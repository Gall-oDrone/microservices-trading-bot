variable "region" { type = string }
variable "repo" { type = string } # e.g., owner/repo
variable "role_name" { 
  type    = string
  default = "ci-github-actions"
}
variable "permissions" { 
  type    = list(string)
  default = [
    "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryPowerUser",
    "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
  ]
}
# Allow GitHub Actions running with environment: X to assume this role (e.g. ["development"])
variable "allowed_environments" {
  type        = list(string)
  default     = []
  description = "GitHub environment names; adds repo:OWNER/REPO:environment:NAME to OIDC trust policy."
}
