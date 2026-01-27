terraform {
  required_version = ">= 1.5.0"
}

provider "aws" {
  region = var.region
}

locals {
  irsa_map = var.irsa_policies
  policy_attachments = {
    for pair in flatten([
      for role, arns in var.irsa_policies : [for arn in arns : { role = role, arn = arn }]
    ]) : "${pair.role}|${pair.arn}" => pair
  }
}

resource "aws_iam_role" "irsa" {
  for_each = local.irsa_map

  name               = "${var.cluster_name}-${each.key}"
  assume_role_policy = data.aws_iam_policy_document.irsa[each.key].json
}

data "aws_iam_policy_document" "irsa" {
  for_each = local.irsa_map

  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [var.oidc_provider_arn]
    }
    condition {
      test     = "StringLike"
      variable = "${var.oidc_provider}:sub"
      values   = [
        "system:serviceaccount:*:*"
      ]
    }
  }
}

resource "aws_iam_role_policy_attachment" "attach" {
  for_each   = local.policy_attachments
  role       = aws_iam_role.irsa[each.value.role].name
  policy_arn = each.value.arn
}

# =============================================================================
# AWS Load Balancer Controller - Additional EC2 Permissions
# The ElasticLoadBalancingFullAccess managed policy does not include EC2
# permissions required by the ALB controller for subnet/AZ discovery and
# security group management.
# =============================================================================

data "aws_iam_policy_document" "alb_controller_ec2" {
  count = contains(keys(local.irsa_map), "alb") ? 1 : 0

  statement {
    sid    = "EC2DescribePermissions"
    effect = "Allow"
    actions = [
      "ec2:DescribeAccountAttributes",
      "ec2:DescribeAddresses",
      "ec2:DescribeAvailabilityZones",
      "ec2:DescribeInternetGateways",
      "ec2:DescribeVpcs",
      "ec2:DescribeVpcPeeringConnections",
      "ec2:DescribeSubnets",
      "ec2:DescribeSecurityGroups",
      "ec2:DescribeInstances",
      "ec2:DescribeNetworkInterfaces",
      "ec2:DescribeTags",
      "ec2:DescribeCoipPools",
      "ec2:GetCoipPoolUsage",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "EC2SecurityGroupPermissions"
    effect = "Allow"
    actions = [
      "ec2:CreateSecurityGroup",
      "ec2:DeleteSecurityGroup",
      "ec2:AuthorizeSecurityGroupIngress",
      "ec2:RevokeSecurityGroupIngress",
      "ec2:AuthorizeSecurityGroupEgress",
      "ec2:RevokeSecurityGroupEgress",
    ]
    resources = ["*"]
  }

  statement {
    sid    = "EC2TagPermissions"
    effect = "Allow"
    actions = [
      "ec2:CreateTags",
      "ec2:DeleteTags",
    ]
    resources = [
      "arn:aws:ec2:*:*:security-group/*",
      "arn:aws:ec2:*:*:security-group-rule/*",
    ]
  }
}

resource "aws_iam_policy" "alb_controller_ec2" {
  count = contains(keys(local.irsa_map), "alb") ? 1 : 0

  name        = "${var.cluster_name}-alb-controller-ec2-policy"
  description = "Additional EC2 permissions for AWS Load Balancer Controller"
  policy      = data.aws_iam_policy_document.alb_controller_ec2[0].json
}

resource "aws_iam_role_policy_attachment" "alb_controller_ec2" {
  count = contains(keys(local.irsa_map), "alb") ? 1 : 0

  role       = aws_iam_role.irsa["alb"].name
  policy_arn = aws_iam_policy.alb_controller_ec2[0].arn
}

output "irsa_role_arns" {
  value = { for k, r in aws_iam_role.irsa : k => r.arn }
}