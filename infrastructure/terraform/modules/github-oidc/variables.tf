variable "region" { type = string }
variable "repo" { type = string } # e.g., owner/repo
variable "role_name" { type = string  default = "ci-github-actions" }
variable "permissions" { type = list(string)  default = [
  "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryPowerUser",
  "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
] }
