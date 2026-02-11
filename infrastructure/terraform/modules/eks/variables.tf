variable "region" { type = string }
variable "cluster_name" { type = string }
variable "vpc_id" { type = string }
variable "private_subnet_ids" { type = list(string) }
variable "public_subnet_ids" { type = list(string) }
variable "cluster_version" { 
  type    = string
  default = "1.30"
}
variable "endpoint_public_access" {
  type        = bool
  default     = true
  description = "Enable public API server endpoint (keep true to avoid Terraform changing live cluster)."
}
variable "node_instance_types" { 
  type    = list(string)
  default = ["m6i.large"]
}
variable "desired_size" { 
  type    = number
  default = 2
}
variable "min_size" { 
  type    = number
  default = 1
}
variable "max_size" { 
  type    = number
  default = 4
}
