variable "region" { type = string }
variable "cluster_name" { type = string }
variable "oidc_provider_arn" { type = string }
variable "oidc_provider" { type = string }
variable "irsa_policies" { type = map(string)  default = {} }
