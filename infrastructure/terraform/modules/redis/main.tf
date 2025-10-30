terraform {
  required_version = ">= 1.5.0"
}

provider "aws" { region = var.region }

resource "aws_security_group" "redis" {
  name        = "${var.name}-redis-sg"
  description = "Security group for Redis"
  vpc_id      = var.vpc_id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_elasticache_subnet_group" "this" {
  name       = "${var.name}-redis-subnets"
  subnet_ids = var.subnet_ids
}

resource "aws_elasticache_replication_group" "this" {
  replication_group_id          = var.name
  description                   = "Redis for ${var.name}"
  engine                        = "redis"
  engine_version                = var.engine_version
  node_type                     = var.node_type
  number_cache_clusters         = var.num_cache_clusters
  automatic_failover_enabled    = false
  transit_encryption_enabled    = true
  at_rest_encryption_enabled    = true
  subnet_group_name             = aws_elasticache_subnet_group.this.name
  security_group_ids            = [aws_security_group.redis.id]
}

output "primary_endpoint" { value = aws_elasticache_replication_group.this.primary_endpoint_address }
