variable "project" {
  type    = string
  default = "mtb"
}
variable "env" {
  type    = string
  default = "development"
}
variable "aws_region" {
  type    = string
  default = "us-east-1"
}

variable "vpc_cidr" {
  type    = string
  default = "10.0.0.0/16"
}
variable "azs" {
  type    = list(string)
  default = ["us-east-1a", "us-east-1b", "us-east-1c"]
}
variable "private_subnets" {
  type    = list(string)
  default = ["10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"]
}
variable "public_subnets" {
  type    = list(string)
  default = ["10.0.101.0/24", "10.0.102.0/24", "10.0.103.0/24"]
}

variable "repos" {
  type = list(string)
  default = [
    "api-gateway", "market-data", "order-management", "strategy-executor", "strategy-router", "trading-engine", "backtesting"
  ]
}

variable "enable_msk" {
  type    = bool
  default = false
}
variable "enable_redis" {
  type    = bool
  default = false
}

variable "github_repo" {
  type    = string
  default = "Gall-oDrone/microservices-trading-bot"
}

# Path A: standalone data-collector (EC2 + S3 + optional RDS)
variable "enable_data_collector" {
  type    = bool
  default = true
}

variable "enable_data_collector_rds" {
  type    = bool
  default = true
}

variable "data_collector_instance_type" {
  type    = string
  default = "t4g.nano"
}

variable "data_collector_s3_bucket" {
  type        = string
  description = "Globally unique S3 bucket name for Parquet trade archive"
  default     = ""
}

variable "data_collector_rds_instance_class" {
  type    = string
  default = "db.t4g.micro"
}

variable "data_collector_rds_storage_gb" {
  type    = number
  default = 20
}

variable "data_collector_hot_retention_days" {
  type    = number
  default = 7
}

variable "data_collector_bitso_book" {
  type    = string
  default = "btc_mxn"
}

variable "data_collector_ssh_cidr_blocks" {
  type        = list(string)
  description = "CIDRs allowed to SSH to the collector; empty disables SSH"
  default     = []
}

variable "data_collector_key_name" {
  type        = string
  description = "Optional EC2 key pair name"
  default     = ""
}
