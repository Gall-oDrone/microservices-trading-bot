variable "region" { type = string }
variable "name" { type = string }
variable "vpc_id" { type = string }
variable "subnet_ids" { type = list(string) }
variable "node_type" { 
  type    = string
  default = "cache.t3.micro"
}
variable "num_cache_clusters" { 
  type    = number
  default = 1
}
variable "engine_version" { 
  type    = string
  default = "7.1"
}
