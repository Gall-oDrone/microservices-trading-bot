variable "region" { type = string }
variable "name" { type = string }
variable "cidr" { type = string }
variable "azs" { type = list(string) }
variable "private_subnets" { type = list(string) }
variable "public_subnets" { type = list(string) }
variable "single_nat_gateway" { 
  type    = bool
  default = true
}
