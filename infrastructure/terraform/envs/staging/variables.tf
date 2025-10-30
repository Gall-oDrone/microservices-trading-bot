variable "project" { type = string  default = "microservices-trading-bot" }
variable "env" { type = string  default = "staging" }
variable "aws_region" { type = string  default = "us-east-1" }

variable "vpc_cidr" { type = string  default = "10.1.0.0/16" }
variable "azs" { type = list(string)  default = ["us-east-1a","us-east-1b","us-east-1c"] }
variable "private_subnets" { type = list(string)  default = ["10.1.1.0/24","10.1.2.0/24","10.1.3.0/24"] }
variable "public_subnets" { type = list(string)  default = ["10.1.101.0/24","10.1.102.0/24","10.1.103.0/24"] }

variable "repos" { type = list(string)  default = [
  "api-gateway","market-data","order-management","strategy-executor","trading-engine","backtesting"
] }

variable "enable_msk" { type = bool  default = false }
variable "enable_redis" { type = bool  default = false }
variable "github_repo" { type = string  default = "Gall-oDrone/microservices-trading-bot" }
