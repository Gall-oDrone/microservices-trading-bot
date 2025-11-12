terraform {
  required_version = ">= 1.5.0"
}

provider "aws" {
  region = var.region
}

resource "aws_ecr_repository" "this" {
  for_each = toset(var.repositories)
  name                 = each.value
  image_tag_mutability = "MUTABLE"
  image_scanning_configuration { scan_on_push = true }
}

output "repository_urls" {
  value = { for k, r in aws_ecr_repository.this : k => r.repository_url }
}
