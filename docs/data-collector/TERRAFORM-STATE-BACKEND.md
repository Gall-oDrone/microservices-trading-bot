# Terraform State Backend (data-collector / development)

**Status:** Active since 2026-08-19
**Environment:** `infrastructure/terraform/envs/development`
**AWS account:** `326105557351` · **Region:** `us-east-1`

## TL;DR for future agents

The `development` Terraform state is now stored **remotely in S3** (not on the IDE
EC2 instance's local disk). This means the data-collector infrastructure (EC2
`t4g.nano`, RDS Postgres hot store, S3 Parquet archive, and the standalone VPC)
can be torn down and re-deployed — or the IDE instance itself deleted — without
losing Terraform state.

- **State bucket:** `s3://mtb-tfstate-326105557351-us-east-1`
- **State key:** `microservices-trading-bot/development/terraform.tfstate`
- **Lock table (DynamoDB):** `mtb-tflock`
- **Encryption:** SSE-S3 (AES256), bucket versioning ON, all public access blocked

If you are analyzing collected data or infra, trust the S3 state as the source of
truth. Do **not** re-create these backend resources — they already exist.

## Why this exists

Previously `envs/development/backend.tf` used a **local** backend, so
`terraform.tfstate` lived only on the IDE EC2 instance's EBS volume. That volume
is set to `DeleteOnTermination: true` (see
`infrastructure/cloudformation/trading-bot-ide-cfn.yaml`), so running
`infrastructure/cloudformation/cleanup-ide.sh` — or otherwise deleting the IDE
instance — would have **permanently destroyed the Terraform state** and orphaned
the live data-collector/RDS resources in AWS.

Migrating the state to S3 decouples state durability from the IDE instance
lifecycle and lets a future instance manage the same infrastructure.

## What was provisioned (one-time, done manually via AWS CLI)

1. **S3 bucket** `mtb-tfstate-326105557351-us-east-1`
   - Versioning enabled (recover from bad writes / accidental deletes)
   - Default encryption SSE-S3 (AES256), bucket keys enabled
   - Public access fully blocked
2. **DynamoDB table** `mtb-tflock` (`LockID` hash key, `PAY_PER_REQUEST`) for state
   locking. Terraform 1.9.8 is in use here, which predates S3-native locking
   (`use_lockfile`, added in TF 1.10), so a lock table is required.
3. **Manual state backup** of the pre-migration local state uploaded to:
   `s3://mtb-tfstate-326105557351-us-east-1/backups/development/terraform.tfstate.<UTC-timestamp>`
   (first backup: `...terraform.tfstate.20260819T165946Z`, serial 43,
   lineage `3c2a32fa-1f4b-d43b-ce25-483ce554d49b`).

## What changed in the repo

- `envs/development/backend.tf` — switched from `backend "local"` to `backend "s3"`
  with the concrete bucket/key/region/dynamodb_table hardcoded. The config is
  hardcoded (not a partial `backend "s3" {}` like staging/production) because
  `deploy-data-collector.sh` and `cleanup-data-collector.sh` call
  `terraform init` **without** `-backend-config`, so they must work with zero
  extra flags.
- `envs/development/backend.example.hcl` — updated to the real reference values.

The local `terraform.tfstate` file left in the env dir after migration is stale
and unused (it is gitignored via `*.tfstate`). It can be kept as an extra local
backup or deleted; the S3 copy is authoritative.

## How the migration was performed (already done — do not repeat)

```bash
# 1. Backup current local state to S3
aws s3 cp infrastructure/terraform/envs/development/terraform.tfstate \
  s3://mtb-tfstate-326105557351-us-east-1/backups/development/terraform.tfstate.$(date -u +%Y%m%dT%H%M%SZ)

# 2. Point backend.tf at S3 (see the file), then migrate the state
cd infrastructure/terraform/envs/development
terraform init -migrate-state -force-copy -input=false
```

A post-migration `terraform state list` confirmed all resources
(`module.vpc.*`, `module.data_collector_ec2[0].*`, `module.data_collector_rds[0].*`,
`module.data_archive_s3[0].*`) are tracked in the remote state. A targeted
`terraform plan` showed `0 to add, 0 to destroy` and only one pre-existing
in-place drift: `aws_instance.collector` `user_data` hash differs from the
current template. This is a benign in-place update (not a replacement) and was
present before the migration; it is reconciled harmlessly on the next deploy.

## Operational notes / gotchas

- **`deploy-data-collector.sh` and `cleanup-data-collector.sh` now use S3 state
  automatically.** No flags or env vars needed — `terraform init` reads the
  backend from `backend.tf`.
- **Tearing down + re-deploying** the data-collector no longer risks state loss:
  `cleanup-data-collector.sh development` records the destroy in the S3 state, and
  a later `deploy-data-collector.sh development` reads/writes the same S3 state.
- **State recovery:** if state is ever corrupted, restore from a backup:
  `aws s3 cp s3://mtb-tfstate-326105557351-us-east-1/backups/development/terraform.tfstate.<ts> ./terraform.tfstate`
  then `terraform state push` (with care), or use S3 object versioning on the
  live key.
- **Do NOT delete** the `mtb-tfstate-...` bucket or `mtb-tflock` table while any
  environment relies on them. They are intentionally NOT managed by this
  Terraform config (avoids the chicken-and-egg of a backend managing itself).
- The `data-archive-s3` bucket (Parquet trades) is a **separate** bucket from the
  state bucket; don't conflate them.

## Current live infra as of this doc (development)

- EC2 data-collector: `t4g.nano` (public IP was `3.91.0.132` at migration time)
- RDS: `db.t4g.micro` Postgres hot store (kept running for query verification)
- S3 archive: `mtb-development-data-archive-<account_id>` (Parquet trade data)
- VPC: standalone `mtb-development` (NAT Gateway + EIP) owned by the collector
