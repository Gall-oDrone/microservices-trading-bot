provider "aws" {
  region = var.aws_region
}

# Kubernetes and Helm providers will be configured after EKS creation using data sources
