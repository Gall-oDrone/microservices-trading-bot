variable "name" {
  type        = string
  description = "Resource name prefix (e.g. mtb-development-data-collector)"
}

variable "region" {
  type = string
}

variable "vpc_id" {
  type = string
}

variable "vpc_cidr" {
  type        = string
  description = "VPC CIDR allowed to reach health/metrics port"
}

variable "subnet_id" {
  type        = string
  description = "Public subnet ID for the collector instance"
}

variable "instance_type" {
  type    = string
  default = "t4g.nano"
}

variable "ssh_cidr_blocks" {
  type        = list(string)
  description = "CIDRs allowed to SSH; empty disables SSH ingress"
  default     = []
}

variable "key_name" {
  type        = string
  description = "Optional EC2 key pair name"
  default     = ""
}

variable "http_port" {
  type    = number
  default = 8085
}

variable "s3_bucket_name" {
  type = string
}

variable "s3_bucket_arn" {
  type = string
}

variable "s3_prefix" {
  type    = string
  default = "trades"
}

variable "deploy_prefix" {
  type        = string
  description = "S3 key prefix the instance may read the binary from (deploy staging)"
  default     = "deploy"
}

variable "enable_ssm" {
  type        = bool
  description = "Attach AmazonSSMManagedInstanceCore so the binary can be deployed and the box managed via SSM (no SSH needed)"
  default     = true
}

variable "bitso_ws_url" {
  type    = string
  default = "wss://ws.bitso.com"
}

variable "bitso_book" {
  type    = string
  default = "btc_mxn"
}

variable "flush_interval" {
  type    = string
  default = "60s"
}

variable "flush_max_rows" {
  type    = number
  default = 500
}

variable "hot_retention_days" {
  type    = number
  default = 7
}

variable "health_stale_after" {
  type    = string
  default = "5m"
}

variable "enable_postgres" {
  type    = bool
  default = true
}

variable "postgres_secret_name" {
  type        = string
  description = "Secrets Manager secret name/id containing {\"dsn\": \"...\"}; empty if Postgres disabled"
  default     = ""
}

variable "postgres_secret_arn_pattern" {
  type        = string
  description = "IAM resource ARN pattern for the Postgres secret (may use *)"
  default     = ""
}

variable "tags" {
  type    = map(string)
  default = {}
}
