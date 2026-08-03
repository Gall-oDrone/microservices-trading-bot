# Data collector — development deployment

Standalone EC2 + S3 (+ optional RDS) path for the Bitso `btc_mxn` trade
archiver. This is **not** deployed to EKS.

## Expected monthly cost (us-east-1, approximate)

| Component | Spec | Rough cost |
|-----------|------|------------|
| EC2 | `t4g.nano` | ~$3 |
| EBS | 8 GB gp3 | ~$0.60 |
| S3 | Parquet + IA after 90d | pennies–few $ |
| RDS (optional) | `db.t4g.micro`, 20 GB gp3, Single-AZ, 7d backups | ~$12–15 |
| **Total** | RDS on | **~$15–20** |
| **Total** | RDS off (`enable_data_collector_rds=false`) | **~$5** |

No NAT Gateway is created for this path — the instance sits in a **public**
subnet with a tight security group (SSH only from your CIDR if set; HTTPS
egress only).

## Prerequisites

- AWS credentials with rights to create EC2, IAM, S3, RDS, Secrets Manager
- Existing development VPC from this repo's Terraform (`module.vpc`)
- Do **not** commit real `.tfvars` with secrets

## Apply sequence (development)

From repo root:

```bash
cd infrastructure/terraform/envs/development

# Optional: override bucket name / SSH / disable RDS
# cat > data-collector.auto.tfvars <<EOF
# data_collector_s3_bucket        = "my-unique-mtb-dev-data-archive"
# data_collector_ssh_cidr_blocks  = ["x.x.x.x/32"]
# data_collector_key_name         = "my-keypair"
# enable_data_collector_rds       = true
# EOF

terraform init
terraform fmt -recursive
terraform validate
terraform plan -out=data-collector.tfplan
# REVIEW the plan, then:
# terraform apply data-collector.tfplan
```

Useful variables (see `variables.tf`):

- `enable_data_collector` (default `true`)
- `enable_data_collector_rds` (default `true`)
- `data_collector_instance_type` (default `t4g.nano`)
- `data_collector_s3_bucket` (default derived from account id)
- `data_collector_rds_instance_class` / `data_collector_rds_storage_gb`
- `data_collector_hot_retention_days` (default `7`)
- `data_collector_bitso_book` (default `btc_mxn`)

## Deploy the binary onto EC2

Terraform provisions the instance, IAM role, systemd unit, and env file.
The binary itself is **not** baked into the AMI — after apply:

1. Cross-compile: `GOOS=linux GOARCH=arm64 go build -o data-collector ./cmd`
2. Copy to `/opt/data-collector/data-collector` on the instance (SCP/SSM)
3. `sudo systemctl start data-collector`

If Postgres is enabled, user-data pulls `POSTGRES_DSN` from Secrets Manager
secret `${project}-${env}-data-collector-rds/postgres` (field `dsn`).

## Verify after deploy

1. **Health**

   ```bash
   curl -s http://<private-or-public-ip>:8085/healthz
   # expect {"status":"healthy",...} within a few minutes on btc_mxn
   ```

2. **Metrics**

   ```bash
   curl -s http://<ip>:8085/metrics | grep data_collector_trades_received
   ```

3. **Hot Postgres**

   ```sql
   SELECT book, tid, price, amount, maker_side, exchange_ts, received_at
   FROM trades
   ORDER BY received_at DESC
   LIMIT 20;

   SELECT * FROM ws_gaps ORDER BY gap_start DESC LIMIT 10;
   ```

4. **S3 archive**

   ```bash
   aws s3 ls s3://<bucket>/trades/book=btc_mxn/ --recursive | tail
   ```

## Teardown

To remove **only** the data-collector (EC2 + S3 archive + optional RDS) without
touching EKS/VPC or the rest of the environment, use the scoped cleanup script:

```bash
cd infrastructure/terraform/scripts/cleanup

# Dry check: resolve names/ids, no deletes
CLEANUP_VALIDATE_ONLY=1 ./cleanup-data-collector.sh development

# Full teardown (prompts before deleting)
./cleanup-data-collector.sh development

# Keep the Parquet archive, remove compute + RDS only
KEEP_S3_ARCHIVE=1 ./cleanup-data-collector.sh development
```

It empties the archive bucket (which has `force_destroy=false`), disables RDS
deletion protection, runs a **targeted** `terraform destroy` of the three
data-collector modules, and falls back to manual AWS deletion for any leftovers.

## Secrets convention

Aligned with the rest of this repo: credentials live in **AWS Secrets Manager**
(not in git). The RDS module creates the DB secret; the EC2 instance role can
read only that secret. No Bitso trading API keys are used or required for the
public trades WebSocket.
