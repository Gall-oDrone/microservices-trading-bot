locals {
  name = "${var.project}-${var.env}"
}

module "vpc" {
  source = "../../modules/vpc"

  region          = var.aws_region
  name            = local.name
  cidr            = var.vpc_cidr
  azs             = var.azs
  private_subnets = var.private_subnets
  public_subnets  = var.public_subnets
}

module "eks" {
  source = "../../modules/eks"

  region             = var.aws_region
  cluster_name       = local.name
  cluster_version    = "1.33" # match live cluster to avoid downgrade
  vpc_id             = module.vpc.vpc_id
  private_subnet_ids = module.vpc.private_subnet_ids
  public_subnet_ids  = module.vpc.public_subnet_ids

  node_instance_types = ["t3.large"]
  desired_size        = 2
  min_size            = 1
  max_size            = 3
}

module "ecr" {
  source = "../../modules/ecr"

  region       = var.aws_region
  repositories = var.repos
}

module "msk" {
  source = "../../modules/msk"
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
  source = "../../modules/redis"
  count  = var.enable_redis ? 1 : 0

  region          = var.aws_region
  name            = "${local.name}-redis"
  vpc_id          = module.vpc.vpc_id
  subnet_ids      = module.vpc.private_subnet_ids
  node_type       = "cache.t3.micro"
  num_cache_clusters = 1
  engine_version  = "7.1"
}

# AWS Load Balancer Controller requires EC2 (e.g. DescribeAvailabilityZones, DescribeSubnets) and ELB actions.
# ElasticLoadBalancingFullAccess alone does not include EC2; use the official controller IAM policy.
resource "aws_iam_policy" "alb_controller" {
  name        = "${local.name}-alb-controller"
  description = "IAM policy for AWS Load Balancer Controller (EC2 + ELB permissions)"
  policy      = file("${path.module}/policies/alb-controller.json")
}

module "iam_irsa" {
  source = "../../modules/iam"

  region            = var.aws_region
  cluster_name      = module.eks.cluster_name
  oidc_provider     = module.eks.oidc_provider
  oidc_provider_arn = module.eks.oidc_provider_arn

  irsa_policies = {
    external-dns   = ["arn:aws:iam::aws:policy/AmazonRoute53FullAccess"]
    alb            = [aws_iam_policy.alb_controller.arn]
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

  values = [
    <<-EOT
    serviceAccount:
      create: true
      annotations:
        "eks.amazonaws.com/role-arn": "${module.iam_irsa.irsa_role_arns["alb"]}"
    EOT
  ]
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

  values = [
    <<-EOT
    serviceAccount:
      create: true
      annotations:
        "eks.amazonaws.com/role-arn": "${module.iam_irsa.irsa_role_arns["external-dns"]}"
    EOT
  ]
}

resource "helm_release" "cert_manager" {
  name       = "cert-manager"
  repository = "https://charts.jetstack.io"
  chart      = "cert-manager"
  namespace  = "cert-manager"
  version    = "v1.15.1"

  create_namespace = true
  depends_on        = [helm_release.aws_load_balancer_controller]

  set {
    name  = "installCRDs"
    value = true
  }

  values = [
    <<-EOT
    serviceAccount:
      create: true
      annotations:
        "eks.amazonaws.com/role-arn": "${module.iam_irsa.irsa_role_arns["cert-manager"]}"
    EOT
  ]
}

resource "helm_release" "external_secrets" {
  name       = "external-secrets"
  repository = "https://charts.external-secrets.io"
  chart      = "external-secrets"
  namespace  = "external-secrets"
  version    = "0.9.14"

  create_namespace = true
  depends_on       = [helm_release.aws_load_balancer_controller]

  values = [
    <<-EOT
    serviceAccount:
      create: true
      annotations:
        "eks.amazonaws.com/role-arn": "${module.iam_irsa.irsa_role_arns["external-secrets"]}"
    EOT
  ]
}

module "ci_github_oidc" {
  source = "../../modules/github-oidc"

  region     = var.aws_region
  repo       = var.github_repo
  role_name  = "${local.name}-github-actions"
  permissions = [
    "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryPowerUser",
    "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
  ]
}

resource "helm_release" "kube_prometheus_stack" {
  name       = "kube-prometheus-stack"
  repository = "https://prometheus-community.github.io/helm-charts"
  chart      = "kube-prometheus-stack"
  namespace  = "monitoring"
  version    = "58.3.2"

  create_namespace = true

  values = [
    yamlencode({
      grafana = {
        adminPassword = "admin"
        service       = { type = "ClusterIP" }
        # Explicitly enable Ingress so Helm does not remove it (chart default is disabled)
        ingress = {
          enabled        = true
          ingressClassName = "alb"
          hosts          = ["grafana.local"]
          annotations = {
            "alb.ingress.kubernetes.io/scheme"       = "internet-facing"
            "alb.ingress.kubernetes.io/target-type" = "ip"
            "alb.ingress.kubernetes.io/listen-ports" = "[{\"HTTP\": 80}]"
          }
        }
        # Provision Trading Platform Metrics dashboard (Intraday / P&L row)
        dashboardProviders = {
          "trading-provider.yaml" = {
            apiVersion = 1
            providers = [
              {
                name             = "trading"
                orgId            = 1
                folder           = "Trading"
                type             = "file"
                disableDeletion  = false
                editable         = false
                options = {
                  path = "/var/lib/grafana/dashboards/trading"
                }
              }
            ]
          }
        }
        dashboards = {
          trading = {
            "trading-metrics" = {
              # File provisioning expects the dashboard object only (title at top level), not the API wrapper
              json = jsonencode(jsondecode(file("${path.module}/../../../../monitoring/grafana/dashboards/trading-metrics.json")).dashboard)
            }
          }
        }
      }
      prometheus = {
        service = { type = "ClusterIP" }
      }
      alertmanager = {
        enabled = false
      }
    })
  ]
}

output "cluster_name" { value = module.eks.cluster_name }
output "cluster_endpoint" { value = module.eks.cluster_endpoint }
output "aws_region" { value = var.aws_region }
output "ci_role_arn" { value = module.ci_github_oidc.role_arn }
