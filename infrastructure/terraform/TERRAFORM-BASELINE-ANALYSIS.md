# Terraform: feat/eks-infrastructure-baseline vs k8s-deployment-manifests

Analysis of the **entire** `infrastructure/terraform` folder on **feat/eks-infrastructure-baseline** to see if the Terraform changes made on **feat/k8s-deployment-manifests** are already implemented.

---

## 1. modules/iam/main.tf

| Change (from k8s branch) | On baseline? | Notes |
|-------------------------|-------------|--------|
| **StringLike** for OIDC `:sub` condition (fix for External Secrets "Request ARN is invalid") | **Yes** | Lines 34–39 use `test = "StringLike"` and `values = ["system:serviceaccount:*:*"]`. |
| **ALB controller EC2 permissions** (extra policy for subnet/sg/tags) | **Yes** | Lines 49–120: `alb_controller_ec2` policy document, policy resource, and attachment when `alb` is in `irsa_policies`. |

**Conclusion:** IAM fixes and ALB extras are **already on baseline**. No Terraform changes needed in this file on baseline.

---

## 2. envs/development/main.tf

| Item | On baseline? | Notes |
|------|-------------|--------|
| **external-secrets** in `irsa_policies` | **Yes** | SecretsManagerReadWrite + AmazonSSMReadOnlyAccess. |
| **external_secrets** Helm release with IRSA | **Yes** | Uses `module.iam_irsa.irsa_role_arns["external-secrets"]`. |
| **time_sleep** for ALB webhook | **Yes** | 90s wait; cert-manager, external_secrets, kube_prometheus_stack depend on it. |
| **Helm values** via `yamlencode` for SA role | **Yes** | Development uses `values = [ yamlencode({ serviceAccount = { annotations = { ... } } })]` for ALB, external-dns, cert-manager, external_secrets. |

**Conclusion:** Development EKS + IRSA + External Secrets + ALB webhook ordering are **already on baseline**. No changes needed.

---

## 3. envs/development/outputs.tf

| Change (from k8s branch) | On baseline? | Notes |
|-------------------------|-------------|--------|
| **external_secrets_role_arn** output | **No** | Baseline only has: `vpc_id`, `private_subnet_ids`, `public_subnet_ids`, `ecr_repositories`. |

**Conclusion:** The only Terraform change from the k8s branch that is **not** on baseline is the **external_secrets_role_arn** output in `envs/development/outputs.tf`. Adding it on baseline would allow CI/Kustomize to use the role ARN without calling `terraform output irsa_role_arns` and parsing.

---

## 4. envs/staging and envs/production

- Both have **external-secrets** in `irsa_policies` and **external_secrets** Helm release.
- Staging/production **outputs.tf** do not expose `external_secrets_role_arn` or `ecr_repositories` (staging/production outputs differ slightly from development).
- IAM module is shared, so **StringLike** and ALB EC2 permissions apply to all envs.

---

## 5. scripts/secrets/setup-secrets.sh

- **Present on baseline** at `infrastructure/terraform/scripts/secrets/setup-secrets.sh`.
- Creates `trading-bot/bitso-api-key`, `trading-bot/bitso-api-secret`, and optionally `trading-bot/redis-password`.
- On the k8s branch we had added logic to **always** create `redis-password` (empty if not provided) so the ExternalSecret sync never fails. Baseline script may or may not have that; worth checking if you want identical behavior.

---

## Summary

| Terraform .tf change on k8s branch | Already on feat/eks-infrastructure-baseline? |
|-----------------------------------|-----------------------------------------------|
| IAM: StringLike for IRSA           | **Yes**                                       |
| IAM: ALB controller EC2 policy    | **Yes**                                       |
| development/main.tf (external-secrets, webhook wait) | **Yes**                             |
| development/outputs.tf: **external_secrets_role_arn** | **No**                              |

**Only missing on baseline:** `external_secrets_role_arn` in `envs/development/outputs.tf`. All other Terraform .tf fixes are already implemented on **feat/eks-infrastructure-baseline**.
