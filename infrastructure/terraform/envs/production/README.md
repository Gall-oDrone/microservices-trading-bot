## Terraform: Production Environment

This stack provisions production infrastructure and should be applied with strict change control.

## Prerequisites

- Terraform installed
- AWS credentials configured for the production account
- Remote backend resources (S3 + DynamoDB lock table)
- Approved maintenance/change window

## Initialize

```bash
cd infrastructure/terraform/envs/production
terraform init -backend-config=backend.example.hcl
```

## Plan (required)

```bash
terraform plan
```

Review all changes before proceeding, especially:
- EKS cluster or node group updates
- IAM policy/role modifications
- networking changes (VPC, subnets, security groups)

## Apply

```bash
terraform apply
```

## Post-Apply Validation

- Confirm EKS cluster health and node readiness.
- Confirm foundational Helm releases are healthy.
- Validate CI role output and deployment permissions.

## Rollback/Recovery Guidance

- Prefer forward-fix when possible.
- For breaking issues, use version-controlled Terraform changes and re-apply.
- Avoid manual console drift to keep state consistent.
