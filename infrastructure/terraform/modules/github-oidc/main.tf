terraform { required_version = ">= 1.5.0" }
provider "aws" { region = var.region }

data "aws_caller_identity" "current" {}

data "aws_iam_policy_document" "gh_oidc_trust" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }
    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }
    # Allow any context from this repo: ref (branches), pull_request, environment (e.g. development)
    condition {
      test     = "StringLike"
      variable = "token.actions.githubusercontent.com:sub"
      values   = ["repo:${var.repo}:*"]
    }
  }
}

resource "aws_iam_openid_connect_provider" "github" {
  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1"]
}

resource "aws_iam_role" "ci" {
  name               = var.role_name
  assume_role_policy = data.aws_iam_policy_document.gh_oidc_trust.json
}

resource "aws_iam_role_policy_attachment" "attach" {
  for_each   = toset(var.permissions)
  role       = aws_iam_role.ci.name
  policy_arn = each.value
}

# Allow GitHub Actions to run aws eks update-kubeconfig and kubectl (DescribeCluster required)
data "aws_iam_policy_document" "eks_describe" {
  statement {
    sid    = "EKSDescribeCluster"
    effect = "Allow"
    actions = [
      "eks:DescribeCluster",
      "eks:ListClusters"
    ]
    resources = [
      "arn:aws:eks:${var.region}:${data.aws_caller_identity.current.account_id}:cluster/*"
    ]
  }
}

resource "aws_iam_role_policy" "eks_describe" {
  name   = "${var.role_name}-eks-describe"
  role   = aws_iam_role.ci.id
  policy = data.aws_iam_policy_document.eks_describe.json
}

output "role_arn" { value = aws_iam_role.ci.arn }
