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

module "msk" {
  source = "../modules/msk"
  count  = var.enable_msk ? 1 : 0

  region                 = var.aws_region
  cluster_name           = "${local.name}-msk"
  vpc_id                 = module.vpc.vpc_id
  subnet_ids             = module.vpc.private_subnet_ids
  kafka_version          = "3.6.0"
  broker_instance_type   = "kafka.m5.large"
  number_of_broker_nodes = 2
}

module "redis" {
  source = "../modules/redis"
  count  = var.enable_redis ? 1 : 0

  region          = var.aws_region
  name            = "${local.name}-redis"
  vpc_id          = module.vpc.vpc_id
  subnet_ids      = module.vpc.private_subnet_ids
  node_type       = "cache.t3.micro"
  num_cache_clusters = 1
  engine_version  = "7.1"
}

module "iam_irsa" {
  source = "../modules/iam"

  region            = var.aws_region
  cluster_name      = module.eks.cluster_name
  oidc_provider     = module.eks.oidc_provider
  oidc_provider_arn = module.eks.oidc_provider_arn

  irsa_policies = {
    external-dns   = ["arn:aws:iam::aws:policy/AmazonRoute53FullAccess"]
    alb            = ["arn:aws:iam::aws:policy/ElasticLoadBalancingFullAccess"]
    cert-manager   = ["arn:aws:iam::aws:policy/AmazonRoute53FullAccess"]
    external-secrets = [
      "arn:aws:iam::aws:policy/SecretsManagerReadWrite",
      "arn:aws:iam::aws:policy/AmazonSSMReadOnlyAccess"
    ]
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

resource "helm_release" "external_dns" {
  name       = "external-dns"
  repository = "https://kubernetes-sigs.github.io/external-dns/"
  chart      = "external-dns"
  namespace  = "kube-system"
  version    = "1.15.0"

  set {
    name  = "provider"
    value = "aws"
  }

  set {
    name  = "policy"
    value = "upsert-only"
  }

  set {
    name  = "registry"
    value = "txt"
  }

  set {
    name  = "txtOwnerId"
    value = local.name
  }

  set {
    name  = "serviceAccount.create"
    value = true
  }

  set {
    name  = "serviceAccount.annotations.eks\.amazonaws\.com/role-arn"
    value = module.iam_irsa.irsa_role_arns["external-dns"]
  }
}

resource "helm_release" "cert_manager" {
  name       = "cert-manager"
  repository = "https://charts.jetstack.io"
  chart      = "cert-manager"
  namespace  = "cert-manager"
  version    = "v1.15.1"

  create_namespace = true

  set {
    name  = "installCRDs"
    value = true
  }

  set {
    name  = "serviceAccount.create"
    value = true
  }

  set {
    name  = "serviceAccount.annotations.eks\.amazonaws\.com/role-arn"
    value = module.iam_irsa.irsa_role_arns["cert-manager"]
  }
}

resource "helm_release" "external_secrets" {
  name       = "external-secrets"
  repository = "https://charts.external-secrets.io"
  chart      = "external-secrets"
  namespace  = "external-secrets"
  version    = "0.9.14"

  create_namespace = true

  set {
    name  = "serviceAccount.create"
    value = true
  }

  set {
    name  = "serviceAccount.annotations.eks\.amazonaws\.com/role-arn"
    value = module.iam_irsa.irsa_role_arns["external-secrets"]
  }
}

module "ci_github_oidc" {
  source = "../modules/github-oidc"

  region     = var.aws_region
  repo       = var.github_repo
  role_name  = "${local.name}-github-actions"
  permissions = [
    "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryPowerUser",
    "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
  ]
}

output "cluster_name" { value = module.eks.cluster_name }
output "cluster_endpoint" { value = module.eks.cluster_endpoint }
output "ci_role_arn" { value = module.ci_github_oidc.role_arn }
