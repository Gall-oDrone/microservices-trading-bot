variable "bucket_name" {
  type        = string
  description = "Globally unique S3 bucket name for the trade archive"
}

variable "prefix" {
  type    = string
  default = "trades"
}

variable "collector_role_arn" {
  type        = string
  description = "IAM role ARN of the data-collector EC2 instance (write-only principal)"
}

variable "ia_transition_days" {
  type    = number
  default = 90
}

variable "force_destroy" {
  type    = bool
  default = false
}

variable "tags" {
  type    = map(string)
  default = {}
}
