### Staging Environment (Terraform)

Initialize remote state and apply:

```bash
cd infrastructure/terraform/envs/staging
terraform init -backend-config=backend.example.hcl
terraform apply
```
