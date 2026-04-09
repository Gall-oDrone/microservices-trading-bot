## Terraform: Development Environment

This stack provisions development AWS infrastructure for the trading platform.

## Prerequisites

- Terraform installed
- AWS credentials configured for the development account
- Remote backend resources already created (S3 bucket + DynamoDB lock table)

## Initialize

```bash
cd infrastructure/terraform/envs/development
terraform init -backend-config=backend.example.hcl
```

If needed, create your own backend config file and pass it to `-backend-config`.

## Plan

```bash
terraform plan
```

## Apply

```bash
terraform apply
```

## Common Variables

Defaults live in `variables.tf` and include:
- `project` (default: `mtb`)
- `env` (default: `development`)
- `aws_region` (default: `us-east-1`)
- `enable_msk` and `enable_redis` toggles
- `github_repo` for CI OIDC role wiring

Override any variable via `-var` or a `.tfvars` file.

## Outputs

After apply, key outputs include:
- `cluster_name`
- `cluster_endpoint`
- `ci_role_arn`

## Destroy (optional)

```bash
terraform destroy
```

Use with care and only in disposable development environments.
