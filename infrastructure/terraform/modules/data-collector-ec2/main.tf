# data-collector-ec2
# Provisions a small EC2 instance in a public subnet to run the Bitso trade collector.

terraform {
  required_version = ">= 1.5.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

data "aws_ami" "amazon_linux_2023_arm" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-arm64"]
  }

  filter {
    name   = "architecture"
    values = ["arm64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }

  filter {
    name   = "state"
    values = ["available"]
  }
}

resource "aws_security_group" "collector" {
  name        = "${var.name}-sg"
  description = "Data collector EC2 - SSH in (optional), HTTPS out"
  vpc_id      = var.vpc_id

  dynamic "ingress" {
    for_each = length(var.ssh_cidr_blocks) > 0 ? [1] : []
    content {
      description = "SSH"
      from_port   = 22
      to_port     = 22
      protocol    = "tcp"
      cidr_blocks = var.ssh_cidr_blocks
    }
  }

  # Health/metrics from within VPC only
  ingress {
    description = "Health and metrics"
    from_port   = var.http_port
    to_port     = var.http_port
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }

  egress {
    description = "HTTPS to Bitso and AWS APIs"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    description = "DNS"
    from_port   = 53
    to_port     = 53
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # Postgres within VPC (CIDR-scoped to avoid a cycle with the RDS module)
  dynamic "egress" {
    for_each = var.enable_postgres ? [1] : []
    content {
      description = "Postgres within VPC"
      from_port   = 5432
      to_port     = 5432
      protocol    = "tcp"
      cidr_blocks = [var.vpc_cidr]
    }
  }

  tags = merge(var.tags, { Name = "${var.name}-sg" })
}

resource "aws_iam_role" "collector" {
  name = "${var.name}-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })

  tags = var.tags
}

resource "aws_iam_role_policy" "collector" {
  name = "${var.name}-policy"
  role = aws_iam_role.collector.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = concat(
      [
        {
          Sid    = "S3WriteOnlyArchive"
          Effect = "Allow"
          Action = [
            "s3:PutObject"
          ]
          Resource = [
            "${var.s3_bucket_arn}/${var.s3_prefix}/*"
          ]
        },
        {
          Sid    = "CloudWatchLogs"
          Effect = "Allow"
          Action = [
            "logs:CreateLogGroup",
            "logs:CreateLogStream",
            "logs:PutLogEvents",
            "logs:DescribeLogStreams"
          ]
          Resource = "arn:aws:logs:*:*:log-group:/mtb/data-collector*"
        }
      ],
      var.postgres_secret_arn_pattern != "" ? [
        {
          Sid      = "ReadPostgresSecret"
          Effect   = "Allow"
          Action   = ["secretsmanager:GetSecretValue", "secretsmanager:DescribeSecret"]
          Resource = [var.postgres_secret_arn_pattern]
        }
      ] : []
    )
  })
}

resource "aws_iam_instance_profile" "collector" {
  name = "${var.name}-profile"
  role = aws_iam_role.collector.name
}

locals {
  user_data = <<-EOF
    #!/bin/bash
    set -euo pipefail
    dnf install -y docker jq awscli
    systemctl enable --now docker

    mkdir -p /opt/data-collector /etc/data-collector /var/log/data-collector

    cat >/etc/data-collector/env <<'ENVEOF'
    SERVICE_NAME=data-collector
    HTTP_PORT=${var.http_port}
    BITSO_WS_URL=${var.bitso_ws_url}
    BITSO_BOOK=${var.bitso_book}
    ENABLE_S3=true
    S3_BUCKET=${var.s3_bucket_name}
    S3_PREFIX=${var.s3_prefix}
    AWS_REGION=${var.region}
    FLUSH_INTERVAL=${var.flush_interval}
    FLUSH_MAX_ROWS=${var.flush_max_rows}
    HOT_RETENTION_DAYS=${var.hot_retention_days}
    HEALTH_STALE_AFTER=${var.health_stale_after}
    ENABLE_POSTGRES=${var.enable_postgres ? "true" : "false"}
    ENVEOF

    %{ if var.postgres_secret_name != "" ~}
    SECRET_JSON=$(aws secretsmanager get-secret-value --secret-id "${var.postgres_secret_name}" --region "${var.region}" --query SecretString --output text)
    POSTGRES_DSN=$(echo "$SECRET_JSON" | jq -r '.dsn')
    echo "POSTGRES_DSN=$POSTGRES_DSN" >> /etc/data-collector/env
    %{ endif ~}

    cat >/etc/systemd/system/data-collector.service <<'UNIT'
    [Unit]
    Description=Bitso BTC-MXN intraday data collector
    After=network-online.target docker.service
    Wants=network-online.target

    [Service]
    Type=simple
    EnvironmentFile=/etc/data-collector/env
    # Binary is expected at /opt/data-collector/data-collector (deploy separately).
    ExecStart=/opt/data-collector/data-collector
    Restart=always
    RestartSec=5
    User=root
    WorkingDirectory=/opt/data-collector
    StandardOutput=append:/var/log/data-collector/stdout.log
    StandardError=append:/var/log/data-collector/stderr.log

    [Install]
    WantedBy=multi-user.target
    UNIT

    systemctl daemon-reload
    # Do not start until the binary is deployed; enable for automatic restart after deploy.
    systemctl enable data-collector.service
  EOF
}

resource "aws_instance" "collector" {
  ami                         = data.aws_ami.amazon_linux_2023_arm.id
  instance_type               = var.instance_type
  subnet_id                   = var.subnet_id
  vpc_security_group_ids      = [aws_security_group.collector.id]
  iam_instance_profile        = aws_iam_instance_profile.collector.name
  associate_public_ip_address = true
  key_name                    = var.key_name != "" ? var.key_name : null
  user_data                   = local.user_data

  root_block_device {
    volume_size = 8
    volume_type = "gp3"
    encrypted   = true
  }

  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  tags = merge(var.tags, { Name = var.name })
}

resource "aws_cloudwatch_log_group" "collector" {
  name              = "/mtb/data-collector/${var.name}"
  retention_in_days = 14
  tags              = var.tags
}
