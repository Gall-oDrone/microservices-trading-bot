## Terraform: Staging Environment

This stack provisions staging infrastructure used for pre-production validation.

## Prerequisites

- Terraform installed
- AWS credentials configured for the staging account
- Remote backend resources (S3 + DynamoDB lock table)

## Initialize

```bash
cd infrastructure/terraform/envs/staging
terraform init -backend-config=backend.example.hcl
```

## Plan

```bash
terraform plan
```

## Apply

```bash
terraform apply
```

## Notes

- Staging should mirror production shape as closely as possible.
- Review changes carefully before apply, especially EKS node scaling and IAM changes.
- Validate cluster access and core releases (ALB controller, cert-manager, external-secrets, monitoring) after deployment.

## Destroy

Staging destroy is possible but typically avoided outside controlled maintenance windows:

```bash
terraform destroy
```
