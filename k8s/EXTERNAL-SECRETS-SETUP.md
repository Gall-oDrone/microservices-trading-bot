# External Secrets setup

Application secrets are synced from **AWS Secrets Manager** into Kubernetes via **External Secrets Operator** (ESO).

## 1. IAM (Terraform)

The ESO controller needs an IRSA role that can read Secrets Manager. This repo:

- **`infrastructure/terraform/modules/iam/main.tf`**  
  Uses `StringLike` for the OIDC `:sub` condition so `system:serviceaccount:*:*` matches any namespace (e.g. `bitso-trading-dev:external-secrets`). If you previously had `StringEquals`, the store will show **ValidationFailed** until the role is updated.

- **`infrastructure/terraform/envs/development/main.tf`**  
  Defines `external-secrets` in `irsa_policies` with `SecretsManagerReadWrite` (and SSM if needed).

Apply the role (and any IAM changes):

```bash
cd infrastructure/terraform/envs/development
terraform init
terraform apply
```

Optional: use the role ARN in Kustomize or CI:

```bash
terraform output external_secrets_role_arn
```

## 2. Secrets in AWS

Create the secrets that ESO syncs (names must match `k8s/base/external-secret.yaml`):

- `trading-bot/bitso-api-key`
- `trading-bot/bitso-api-secret`
- `trading-bot/redis-password` (required for sync; may be empty)
- `trading-bot/etoro-public-key` → `ETORO_PUBLIC_KEY` (x-api-key)
- `trading-bot/etoro-private-key` → `ETORO_PRIVATE_KEY` (x-user-key)

Run the script (after Terraform and AWS CLI are configured):

```bash
./infrastructure/terraform/scripts/secrets/setup-secrets.sh
```

It will create or update all three secrets; `redis-password` is always created so the ExternalSecret can sync even if you don’t use Redis auth.

## 3. Kubernetes

- **Namespace**  
  Each overlay creates its own namespace (`k8s/overlays/<env>/namespace.yaml`), so `kubectl apply -k k8s/overlays/development` creates `bitso-trading-dev` if needed.

- **ServiceAccount**  
  The overlay patches the `external-secrets` ServiceAccount with the IRSA role ARN (e.g. in `development-patches.yaml`). The ESO SecretStore uses this SA to call AWS.

- **SecretStore / ExternalSecret**  
  In `k8s/base/`: `SecretStore` (AWS Secrets Manager) and `ExternalSecret` (`trading-secrets`) are applied with the rest of the overlay.

## 4. Verify

```bash
kubectl get secretstore,externalsecret -n bitso-trading-dev
kubectl get secret trading-secrets -n bitso-trading-dev
```

If SecretStore shows **ValidationFailed** with “Request ARN is invalid” or “failed to retrieve credentials”, re-apply Terraform (IAM trust policy with `StringLike`) and ensure the `external-secrets` ServiceAccount in the app namespace has the correct `eks.amazonaws.com/role-arn` annotation.
