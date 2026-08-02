variable "name" {
  type = string
}

variable "vpc_id" {
  type = string
}

variable "subnet_ids" {
  type        = list(string)
  description = "Private subnet IDs for the DB subnet group (Multi-AZ capable layout; instance is Single-AZ)"
}

variable "collector_security_group_id" {
  type        = string
  description = "Security group of the data-collector EC2 instance"
}

variable "instance_class" {
  type    = string
  default = "db.t4g.micro"
}

variable "allocated_storage" {
  type    = number
  default = 20
}

variable "max_allocated_storage" {
  type    = number
  default = 100
}

variable "engine_version" {
  type    = string
  default = "16.4"
}

variable "db_name" {
  type    = string
  default = "trades"
}

variable "master_username" {
  type    = string
  default = "collector"
}

variable "backup_retention_days" {
  type    = number
  default = 7
}

variable "deletion_protection" {
  type    = bool
  default = false
}

variable "tags" {
  type    = map(string)
  default = {}
}
