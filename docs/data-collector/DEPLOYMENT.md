# Data collector — development deployment

Standalone EC2 + S3 (+ optional RDS) path for the Bitso `btc_mxn` trade
archiver. This is **not** deployed to EKS.

## Expected monthly cost (us-east-1, approximate)

| Component | Spec | Rough cost |
|-----------|------|------------|
| EC2 | `t4g.nano` | ~$3 |
| EBS | 30 GB gp3 (AL2023 arm64 AMI minimum) | ~$2.40 |
| S3 | Parquet + IA after 90d | pennies–few $ |
| RDS (optional) | `db.t4g.micro`, 20 GB gp3, Single-AZ, 7d backups | ~$12–15 |
| **Total** | RDS on | **~$15–20** |
| **Total** | RDS off (`enable_data_collector_rds=false`) | **~$5** |

The data-collector **instance** sits in a **public** subnet with a tight
security group (no SSH by default; HTTPS egress only; health/metrics reachable
only from inside the VPC). The collector modules themselves create **no** NAT
Gateway.

> ⚠️ **NAT cost caveat:** if you apply into an environment where the shared
> `module.vpc` does **not** yet exist (i.e. you also create the VPC, as a
> data-collector-only apply does), the shared VPC module creates **one NAT
> Gateway (~$32/mo)** for its private subnets. If the dev VPC already exists
> (e.g. EKS is deployed), the collector reuses it and adds no NAT. The scoped
> cleanup script does **not** delete the shared VPC/NAT — see Teardown.

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

## One-shot automated deploy (recommended)

The end-to-end deploy is scripted. It cross-compiles the binary, runs a
**targeted** apply (VPC + data-collector EC2/S3/RDS only), stages the binary in
S3, pushes it onto the instance via **SSM Run Command** (no SSH), starts the
systemd unit, and verifies the S3 + Postgres sinks:

```bash
cd infrastructure/terraform/scripts/deploy

# Review the plan only
PLAN_ONLY=1 ./deploy-data-collector.sh development

# Full deploy + verify (prompts before the billable apply)
./deploy-data-collector.sh development

# Re-deploy a new binary onto already-running infra (no apply)
SKIP_APPLY=1 ./deploy-data-collector.sh development
```

This relies on the `data-collector-ec2` module being built with
`enable_ssm=true` (default), which attaches `AmazonSSMManagedInstanceCore` and
grants `s3:GetObject` on the `deploy/*` prefix. No inbound ports are opened.

For a fully manual (no-agent) walkthrough of every command, see
[`MANUAL-RUNBOOK-2026-08-03.md`](./MANUAL-RUNBOOK-2026-08-03.md).

## Deploy the binary onto EC2 (manual fallback)

Terraform provisions the instance, IAM role, systemd unit, and env file.
The binary itself is **not** baked into the AMI — after apply:

1. Cross-compile: `GOOS=linux GOARCH=arm64 go build -o data-collector ./cmd`
2. Stage in S3 and pull it via SSM (the instance role can read `deploy/*`):

   ```bash
   BUCKET=$(terraform output -raw data_archive_bucket)
   IID=$(terraform output -raw data_collector_instance_id)
   aws s3 cp data-collector "s3://$BUCKET/deploy/data-collector"
   aws ssm send-command --instance-ids "$IID" \
     --document-name "AWS-RunShellScript" \
     --parameters 'commands=["aws s3 cp s3://'"$BUCKET"'/deploy/data-collector /opt/data-collector/data-collector && chmod +x /opt/data-collector/data-collector && systemctl restart data-collector && systemctl is-active data-collector"]'
   ```

   (Or, if you enabled SSH via `data_collector_ssh_cidr_blocks` + `key_name`,
   `scp` the binary and `sudo systemctl start data-collector`.)

If Postgres is enabled, user-data pulls `POSTGRES_DSN` from Secrets Manager
secret `${project}-${env}-data-collector-rds/postgres` (field `dsn`).

> The SSM document is `AWS-RunShellScript` (not `AWS-RunShellCommand`).

## Verify after deploy

Health/metrics bind to `:8085` but the security group exposes them only
**inside the VPC**, so run the `curl` checks **on the instance via SSM** (the
`deploy-data-collector.sh` script does all of this automatically):

1. **Health + metrics (on the instance via SSM)**

   ```bash
   IID=$(terraform output -raw data_collector_instance_id)
   aws ssm send-command --instance-ids "$IID" \
     --document-name "AWS-RunShellScript" \
     --parameters 'commands=["curl -s localhost:8085/healthz","curl -s localhost:8085/metrics | grep data_collector_trades_received"]'
   # then read the output:
   # aws ssm list-command-invocations --command-id <id> --details
   ```

   Expect `{"status":"healthy",...}` within a few minutes on `btc_mxn`.

2. **S3 archive (from anywhere with creds)**

   ```bash
   BUCKET=$(terraform output -raw data_archive_bucket)
   aws s3 ls "s3://$BUCKET/trades/book=btc_mxn/" --recursive | tail
   ```

3. **Hot Postgres** — RDS is private, so query it from the instance (install
   `psql` first via SSM, DSN comes from Secrets Manager on the box):

   ```sql
   SELECT book, tid, price, amount, maker_side, exchange_ts, received_at
   FROM trades ORDER BY received_at DESC LIMIT 20;

   SELECT * FROM ws_gaps ORDER BY gap_start DESC LIMIT 10;
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

> The scoped destroy intentionally leaves `module.vpc` (and its NAT Gateway)
> in place so it can't tear down a VPC that EKS or other stacks may share. If
> you created the VPC only for this path and want the NAT gone too, destroy the
> whole environment (or manually remove the NAT) — see the manual runbook.

For a copy-paste, no-agent teardown (including the manual AWS CLI fallbacks and
the NAT caveat), see
[`MANUAL-RUNBOOK-2026-08-03.md`](./MANUAL-RUNBOOK-2026-08-03.md).

## Secrets convention

Aligned with the rest of this repo: credentials live in **AWS Secrets Manager**
(not in git). The RDS module creates the DB secret; the EC2 instance role can
read only that secret. No Bitso trading API keys are used or required for the
public trades WebSocket.
