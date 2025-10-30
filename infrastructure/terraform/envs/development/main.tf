locals {
  name = "${var.project}-${var.env}"
}

module "vpc" {
  source = "../modules/vpc"

  region          = var.aws_region
  name            = local.name
  cidr            = var.vpc_cidr
  azs             = var.azs
  private_subnets = var.private_subnets
  public_subnets  = var.public_subnets
}

module "eks" {
  source = "../modules/eks"

  region             = var.aws_region
  cluster_name       = local.name
  cluster_version    = "1.30"
  vpc_id             = module.vpc.vpc_id
  private_subnet_ids = module.vpc.private_subnet_ids
  public_subnet_ids  = module.vpc.public_subnet_ids

  node_instance_types = ["t3.large"]
  desired_size        = 2
  min_size            = 1
  max_size            = 3
}

module "ecr" {
  source = "../modules/ecr"

  region       = var.aws_region
  repositories = var.repos
}

module "iam_irsa" {
  source = "../modules/iam"

  region            = var.aws_region
  cluster_name      = module.eks.cluster_name
  oidc_provider     = module.eks.oidc_provider
  oidc_provider_arn = module.eks.oidc_provider_arn

  irsa_policies = {
    external-dns = "arn:aws:iam::aws:policy/AmazonRoute53FullAccess"
    alb          = "arn:aws:iam::aws:policy/ElasticLoadBalancingFullAccess"
  }
}

provider "kubernetes" {
  host                   = module.eks.cluster_endpoint
  cluster_ca_certificate = base64decode(module.eks.cluster_ca_data)
  token                  = data.aws_eks_cluster_auth.this.token
}

data "aws_eks_cluster" "this" { name = module.eks.cluster_name }

data "aws_eks_cluster_auth" "this" { name = module.eks.cluster_name }

provider "helm" {
  kubernetes {
    host                   = module.eks.cluster_endpoint
    cluster_ca_certificate = base64decode(module.eks.cluster_ca_data)
    token                  = data.aws_eks_cluster_auth.this.token
  }
}

resource "helm_release" "metrics_server" {
  name       = "metrics-server"
  repository = "https://kubernetes-sigs.github.io/metrics-server/"
  chart      = "metrics-server"
  namespace  = "kube-system"
  version    = "3.12.2"
}

resource "helm_release" "aws_load_balancer_controller" {
  name       = "aws-load-balancer-controller"
  repository = "https://aws.github.io/eks-charts"
  chart      = "aws-load-balancer-controller"
  namespace  = "kube-system"
  version    = "1.8.2"

  set {
    name  = "clusterName"
    value = module.eks.cluster_name
  }

  set {
    name  = "region"
    value = var.aws_region
  }

  set {
    name  = "vpcId"
    value = module.vpc.vpc_id
  }

  # Use IRSA role created above
  set {
    name  = "serviceAccount.create"
    value = true
  }

  set {
    name  = "serviceAccount.annotations.eks\.amazonaws\.com/role-arn"
    value = module.iam_irsa.irsa_role_arns["alb"]
  }
}

output "cluster_name" { value = module.eks.cluster_name }
output "cluster_endpoint" { value = module.eks.cluster_endpoint }
