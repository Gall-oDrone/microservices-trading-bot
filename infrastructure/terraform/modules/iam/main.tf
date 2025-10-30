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
      test     = "StringEquals"
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

output "irsa_role_arns" {
  value = { for k, r in aws_iam_role.irsa : k => r.arn }
}