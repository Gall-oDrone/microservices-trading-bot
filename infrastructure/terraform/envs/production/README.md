### Production Environment (Terraform)

Initialize remote state and apply:

```bash
cd infrastructure/terraform/envs/production
terraform init -backend-config=backend.example.hcl
terraform apply
```
