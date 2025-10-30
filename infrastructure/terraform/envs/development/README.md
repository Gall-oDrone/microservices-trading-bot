### Development Environment (Terraform)

Initialize with remote state backend:

```bash
cd infrastructure/terraform/envs/development
terraform init \
  -backend-config=backend.example.hcl
```

Apply (ensure you have AWS credentials for the selected account/region):

```bash
terraform apply
```

Notes
- Replace values in `backend.example.hcl` (bucket, dynamodb_table) or provide your own `-backend-config` file.
- Backend resources (S3 bucket + DynamoDB table) are expected to be bootstrapped outside this stack.
- Providers and module versions are pinned for reproducibility.
