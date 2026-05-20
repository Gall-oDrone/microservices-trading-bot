terraform {
  required_version = ">= 1.5.0"
}

provider "aws" {
  region = var.region
}

data "aws_caller_identity" "current" {}

# EKS CreateCluster requires kms:CreateGrant on the CMK for the cluster IAM role and
# eks.amazonaws.com. The upstream module only adds the role to key_users (encrypt/decrypt),
# not key_service_users (CreateGrant). GrantIsForAWSResource must not be used for CreateCluster.
data "aws_iam_policy_document" "eks_kms_extra" {
  statement {
    sid    = "AllowEKSClusterRoleKMSGrants"
    effect = "Allow"
    principals {
      type        = "AWS"
      identifiers = ["*"]
    }
    actions = [
      "kms:CreateGrant",
      "kms:ListGrants",
      "kms:RevokeGrant",
      "kms:DescribeKey",
      "kms:Encrypt",
      "kms:Decrypt",
    ]
    resources = ["*"]
    condition {
      test     = "StringLike"
      variable = "aws:PrincipalArn"
      values = [
        "arn:aws:iam::${data.aws_caller_identity.current.account_id}:role/${var.cluster_name}-cluster-*",
      ]
    }
  }

  statement {
    sid    = "AllowEKSServiceKMS"
    effect = "Allow"
    principals {
      type        = "Service"
      identifiers = ["eks.amazonaws.com"]
    }
    actions = [
      "kms:CreateGrant",
      "kms:DescribeKey",
      "kms:Decrypt",
    ]
    resources = ["*"]
  }
}

module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 20.8"

  cluster_name    = var.cluster_name
  cluster_version = var.cluster_version

  vpc_id                   = var.vpc_id
  subnet_ids               = var.private_subnet_ids
  control_plane_subnet_ids = var.private_subnet_ids

  enable_irsa = true
  
  # Enable public endpoint access for development environments
  cluster_endpoint_public_access  = true
  cluster_endpoint_private_access = true
  
  # Enable cluster creator admin permissions for development environments
  # This allows the IAM role/user that created the cluster to access it
  enable_cluster_creator_admin_permissions = true

  # KMS key is created by default for cluster encryption
  # The deletion window (7 days) is set when scheduling deletion via cleanup script
  kms_key_source_policy_documents = [data.aws_iam_policy_document.eks_kms_extra.json]

  eks_managed_node_groups = {
    default = {
      instance_types = var.node_instance_types
      min_size       = var.min_size
      max_size       = var.max_size
      desired_size   = var.desired_size
      subnet_ids     = var.private_subnet_ids
    }
  }

  cluster_addons = {
    coredns    = { most_recent = true }
    kube-proxy = { most_recent = true }
    vpc-cni    = { most_recent = true }
    # EBS CSI driver addon is created separately below to avoid circular dependency
  }

  access_entries = var.access_entries
}

# IAM role for EBS CSI driver service account
# Extract OIDC provider from ARN for condition key
locals {
  oidc_provider = replace(module.eks.oidc_provider_arn, "/^(.*provider/)/", "")
}

data "aws_iam_policy_document" "ebs_csi_driver" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    effect  = "Allow"

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_provider}:sub"
      values   = ["system:serviceaccount:kube-system:ebs-csi-controller-sa"]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.oidc_provider}:aud"
      values   = ["sts.amazonaws.com"]
    }

    principals {
      identifiers = [module.eks.oidc_provider_arn]
      type        = "Federated"
    }
  }
}

resource "aws_iam_role" "ebs_csi_driver" {
  name               = "${var.cluster_name}-ebs-csi-driver"
  assume_role_policy = data.aws_iam_policy_document.ebs_csi_driver.json

  tags = {
    "ServiceAccountName"      = "ebs-csi-controller-sa"
    "ServiceAccountNamespace" = "kube-system"
  }

  # Explicit dependency to ensure OIDC provider exists before creating this role
  depends_on = [module.eks]
}

resource "aws_iam_role_policy_attachment" "ebs_csi_driver" {
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonEBSCSIDriverPolicy"
  role       = aws_iam_role.ebs_csi_driver.name
}

# EBS CSI driver addon created separately to avoid circular dependency
# This must be created after the cluster and IAM role exist
data "aws_eks_addon_version" "ebs_csi_driver" {
  addon_name         = "aws-ebs-csi-driver"
  kubernetes_version = var.cluster_version
  most_recent        = true
}

resource "aws_eks_addon" "ebs_csi_driver" {
  cluster_name             = module.eks.cluster_name
  addon_name               = "aws-ebs-csi-driver"
  addon_version            = data.aws_eks_addon_version.ebs_csi_driver.version
  service_account_role_arn = aws_iam_role.ebs_csi_driver.arn
  resolve_conflicts_on_create = "OVERWRITE"
  resolve_conflicts_on_update  = "OVERWRITE"

  depends_on = [
    module.eks,
    aws_iam_role.ebs_csi_driver,
    aws_iam_role_policy_attachment.ebs_csi_driver
  ]
}

output "cluster_name" { value = module.eks.cluster_name }
output "cluster_endpoint" { value = module.eks.cluster_endpoint }
output "cluster_ca_data" { value = module.eks.cluster_certificate_authority_data }
output "oidc_provider_arn" { value = module.eks.oidc_provider_arn }
output "oidc_provider" { value = module.eks.oidc_provider }
