# Manual runbook — data-collector deploy & cleanup (2026-08-03)

Copy-paste, no-agent instructions to bring the Path A data-collector up on AWS
and tear it back down **by hand**. Use this if the Cursor agent or the helper
scripts aren't available. It mirrors exactly what
`scripts/deploy/deploy-data-collector.sh` and
`scripts/cleanup/cleanup-data-collector.sh` do, step by step.

> The scripts are the happy path — try them first:
> ```bash
> infrastructure/terraform/scripts/deploy/deploy-data-collector.sh development
> infrastructure/terraform/scripts/cleanup/cleanup-data-collector.sh development
> ```
> Everything below is the manual fallback.

---

## 0. Conventions used in this doc

```bash
export AWS_REGION=us-east-1
export ENVIRONMENT=development
export BITSO_BOOK=btc_mxn

# Repo-relative dirs (run from repo root)
ENV_DIR=infrastructure/terraform/envs/$ENVIRONMENT
SVC_DIR=services/data-collector
```

Resource naming (from `main.tf`): `local.name = mtb-development`, so the
collector resources are `mtb-development-data-collector*`, RDS is
`mtb-development-data-collector-rds`, and the DB secret is
`mtb-development-data-collector-rds/postgres`.

---

## 1. Prerequisites

```bash
# Tooling
aws --version         # AWS CLI v2
terraform -version    # >= 1.5
go version            # >= 1.22 (for cross-compiling the binary)

# Credentials must resolve
aws sts get-caller-identity
```

You need IAM rights to create EC2, IAM, S3, RDS, Secrets Manager, and to call
SSM. Do **not** commit real `.tfvars` secrets.

---

## 2. Deploy

### 2.1 Cross-compile the binary (linux/arm64)

The binary is **not** baked into the AMI; you ship it after apply.

```bash
cd $SVC_DIR
GOOS=linux GOARCH=arm64 go build -o /tmp/data-collector ./cmd
cd -   # back to repo root
```

### 2.2 Terraform init + targeted plan

Only the VPC + the three collector modules are applied (EKS/MSK/Redis are left
out).

```bash
cd $ENV_DIR
terraform init -input=false

# Optional overrides (bucket name, SSH, disable RDS) — else defaults apply:
# cat > data-collector.auto.tfvars <<'EOF'
# data_collector_s3_bucket       = "my-unique-mtb-dev-data-archive"
# data_collector_ssh_cidr_blocks = ["x.x.x.x/32"]
# data_collector_key_name        = "my-keypair"
# enable_data_collector_rds      = true
# EOF

terraform plan -input=false \
  -target=module.vpc \
  -target=module.data_collector_ec2 \
  -target=module.data_archive_s3 \
  -target=module.data_collector_rds \
  -out=data-collector.tfplan
```

**Review the plan.** Note it may create the shared VPC **NAT Gateway (~$32/mo)**
if the VPC doesn't already exist.

### 2.3 Apply (billable)

```bash
terraform apply data-collector.tfplan   # RDS creation can take 10–15 min
```

### 2.4 Capture outputs

```bash
BUCKET=$(terraform output -raw data_archive_bucket)
IID=$(terraform output -raw data_collector_instance_id)
RDS_SECRET=$(terraform output -raw data_collector_rds_secret_arn 2>/dev/null || echo "")
echo "bucket=$BUCKET instance=$IID secret=$RDS_SECRET"
cd -   # back to repo root
```

### 2.5 Stage the binary in S3 and deploy via SSM (no SSH)

The instance role can read `s3://$BUCKET/deploy/*`.

```bash
# Upload
aws s3 cp /tmp/data-collector "s3://$BUCKET/deploy/data-collector" --region "$AWS_REGION"

# Wait until the instance is registered with SSM
aws ssm describe-instance-information --region "$AWS_REGION" \
  --filters "Key=InstanceIds,Values=$IID" \
  --query 'InstanceInformationList[0].PingStatus' --output text
# ... repeat until it prints: Online

# Pull binary onto the box, (re)start the unit
CID=$(aws ssm send-command --instance-ids "$IID" --region "$AWS_REGION" \
  --document-name "AWS-RunShellScript" \
  --comment "deploy data-collector binary" \
  --parameters 'commands=[
    "mkdir -p /opt/data-collector",
    "aws s3 cp s3://'"$BUCKET"'/deploy/data-collector /opt/data-collector/data-collector --region '"$AWS_REGION"'",
    "chmod +x /opt/data-collector/data-collector",
    "systemctl daemon-reload || true",
    "systemctl enable data-collector.service || true",
    "systemctl restart data-collector.service",
    "sleep 4",
    "systemctl is-active data-collector.service"
  ]' \
  --query 'Command.CommandId' --output text)

# Read the result (repeat until Status is Success/Failed)
aws ssm get-command-invocation --command-id "$CID" --instance-id "$IID" \
  --region "$AWS_REGION" --query '{Status:Status,Out:StandardOutputContent,Err:StandardErrorContent}'
```

> The SSM document is **`AWS-RunShellScript`** (a common gotcha is
> `AWS-RunShellCommand`, which does not exist).

---

## 3. Verify both sinks

### 3.1 Health + metrics (on the instance, via SSM)

`:8085` is only reachable inside the VPC, so check it from the box:

```bash
CID=$(aws ssm send-command --instance-ids "$IID" --region "$AWS_REGION" \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=[
    "curl -s --max-time 5 http://localhost:8085/healthz; echo",
    "curl -s --max-time 5 http://localhost:8085/metrics | grep data_collector_trades_received_total | grep -v ^#"
  ]' --query 'Command.CommandId' --output text)

aws ssm get-command-invocation --command-id "$CID" --instance-id "$IID" \
  --region "$AWS_REGION" --query 'StandardOutputContent' --output text
# expect {"status":"healthy",...} once the first btc_mxn trade arrives
```

### 3.2 S3 archive (from anywhere with creds)

Flush happens every 60s or 500 rows, so wait ~2 min:

```bash
aws s3 ls "s3://$BUCKET/trades/book=$BITSO_BOOK/" --recursive --region "$AWS_REGION" | tail
```

### 3.3 Postgres hot store (via SSM; RDS is private)

```bash
CID=$(aws ssm send-command --instance-ids "$IID" --region "$AWS_REGION" \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=[
    "command -v psql >/dev/null || sudo dnf install -y postgresql15 || sudo dnf install -y postgresql16 || true",
    "DSN=$(aws secretsmanager get-secret-value --secret-id '"$RDS_SECRET"' --region '"$AWS_REGION"' --query SecretString --output text | jq -r .dsn)",
    "psql \"$DSN\" -tAc \"SELECT count(*) FROM trades;\"",
    "psql \"$DSN\" -tAc \"SELECT count(*) FROM ws_gaps;\"",
    "psql \"$DSN\" -c \"SELECT book,tid,price,amount,maker_side,exchange_ts FROM trades ORDER BY received_at DESC LIMIT 5;\""
  ]' --query 'Command.CommandId' --output text)

aws ssm get-command-invocation --command-id "$CID" --instance-id "$IID" \
  --region "$AWS_REGION" --query 'StandardOutputContent' --output text
```

---

## 4. Cleanup (teardown)

### 4.1 Preferred: targeted `terraform destroy`

`terraform destroy` alone can't delete a non-empty S3 bucket
(`force_destroy=false`), so **empty it first**.

```bash
cd $ENV_DIR
BUCKET=$(terraform output -raw data_archive_bucket 2>/dev/null)
RDS_ID=mtb-$ENVIRONMENT-data-collector-rds

# 1. Empty the archive bucket (skip if you want to KEEP the Parquet data)
aws s3 rm "s3://$BUCKET" --recursive --region "$AWS_REGION" || true

# 2. Defensively disable RDS deletion protection (default is off, but be safe)
aws rds modify-db-instance --db-instance-identifier "$RDS_ID" \
  --no-deletion-protection --apply-immediately --region "$AWS_REGION" 2>/dev/null || true

# 3. Targeted destroy of the 3 collector modules ONLY (VPC/EKS untouched)
terraform destroy \
  -target=module.data_collector_rds \
  -target=module.data_archive_s3 \
  -target=module.data_collector_ec2

# If it fails on a stale refresh, retry once:
# terraform destroy -refresh=false -target=module.data_collector_rds \
#   -target=module.data_archive_s3 -target=module.data_collector_ec2
cd -
```

### 4.2 Manual AWS fallback (state lost, or destroy left leftovers)

Run these only for whatever survived. Names follow the convention in §0.

```bash
NAME=mtb-$ENVIRONMENT-data-collector
RDS_ID=$NAME-rds
SECRET=$NAME-rds/postgres

# EC2 (resolve by tag if the id is unknown)
IID=$(aws ec2 describe-instances --region "$AWS_REGION" \
  --filters "Name=tag:Name,Values=$NAME" "Name=instance-state-name,Values=pending,running,stopping,stopped" \
  --query 'Reservations[].Instances[].InstanceId' --output text | head -1)
[ -n "$IID" ] && aws ec2 terminate-instances --instance-ids "$IID" --region "$AWS_REGION" && \
  aws ec2 wait instance-terminated --instance-ids "$IID" --region "$AWS_REGION"

# RDS (skip final snapshot) + subnet group
aws rds delete-db-instance --db-instance-identifier "$RDS_ID" \
  --skip-final-snapshot --delete-automated-backups --region "$AWS_REGION" || true
aws rds wait db-instance-deleted --db-instance-identifier "$RDS_ID" --region "$AWS_REGION" || true
aws rds delete-db-subnet-group --db-subnet-group-name "$RDS_ID-subnets" --region "$AWS_REGION" || true

# Secrets Manager (immediate)
aws secretsmanager delete-secret --secret-id "$SECRET" \
  --force-delete-without-recovery --region "$AWS_REGION" || true

# Security groups (delete RDS SG first — it references the collector SG)
for sg in "$RDS_ID-sg" "$NAME-sg"; do
  ID=$(aws ec2 describe-security-groups --region "$AWS_REGION" \
    --filters "Name=group-name,Values=$sg" --query 'SecurityGroups[0].GroupId' --output text)
  [ "$ID" != "None" ] && aws ec2 delete-security-group --group-id "$ID" --region "$AWS_REGION" || true
done

# IAM instance profile + role (detach/remove first)
aws iam remove-role-from-instance-profile --instance-profile-name "$NAME-profile" --role-name "$NAME-role" 2>/dev/null || true
aws iam delete-instance-profile --instance-profile-name "$NAME-profile" 2>/dev/null || true
aws iam delete-role-policy --role-name "$NAME-role" --policy-name "$NAME-policy" 2>/dev/null || true
# If SSM managed policy was attached, detach it before deleting the role:
aws iam detach-role-policy --role-name "$NAME-role" \
  --policy-arn arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore 2>/dev/null || true
aws iam delete-role --role-name "$NAME-role" 2>/dev/null || true

# CloudWatch log group
aws logs delete-log-group --log-group-name "/mtb/data-collector/$NAME" --region "$AWS_REGION" 2>/dev/null || true

# S3 bucket (must be empty first — see §4.1 step 1)
aws s3api delete-bucket --bucket "$BUCKET" --region "$AWS_REGION" 2>/dev/null || true
```

### 4.3 Verify nothing is left

```bash
aws ec2 describe-instances --region "$AWS_REGION" \
  --filters "Name=tag:Name,Values=$NAME" "Name=instance-state-name,Values=running,stopped" \
  --query 'Reservations[].Instances[].InstanceId' --output text
aws rds describe-db-instances --db-instance-identifier "$RDS_ID" --region "$AWS_REGION" 2>&1 | grep -q DBInstanceNotFound && echo "RDS gone"
aws s3api head-bucket --bucket "$BUCKET" --region "$AWS_REGION" 2>&1 | grep -q 'Not Found' && echo "bucket gone"
```

### 4.4 NAT / VPC caveat

The targeted destroy intentionally leaves `module.vpc` (and its **NAT Gateway,
~$32/mo**) in place so it can't tear down a VPC that EKS or other stacks may
share. If you created the VPC only for this path and want the NAT gone:

```bash
# Option A: destroy the whole environment (only if nothing else uses this VPC!)
cd $ENV_DIR && terraform destroy && cd -

# Option B: delete just the NAT + release its EIP manually
aws ec2 describe-nat-gateways --region "$AWS_REGION" \
  --filter "Name=tag:Name,Values=mtb-$ENVIRONMENT*" \
  --query 'NatGateways[].{Id:NatGatewayId,State:State}' --output table
# aws ec2 delete-nat-gateway --nat-gateway-id <nat-...> --region "$AWS_REGION"
# then release the associated Elastic IP once the NAT is deleted
```

---

## 5. Troubleshooting (gotchas hit during bring-up)

| Symptom | Cause | Fix |
|---------|-------|-----|
| `InvalidBlockDeviceMapping` on apply | AL2023 arm64 AMI needs ≥30 GB root | `volume_size = 30` (already set in the module) |
| RDS `InvalidParameterCombination` | pinned minor `16.4` unavailable | `engine_version = "16"` lets RDS pick a minor |
| Instance never `Online` in SSM; unit missing | `dnf` OOM-killed on 512 MB `t4g.nano` | user-data adds a 1 GiB swapfile before `dnf`; installs only `jq` |
| `aws ssm send-command` → `InvalidDocument` | wrong document name | use `AWS-RunShellScript` |
| `/healthz` = `503 waiting for first trade` | just started / low volume | wait for the first `btc_mxn` trade |
| No Parquet in S3 yet | flush interval not reached | wait 60s+ or 500 rows; re-run the §3.2 `aws s3 ls` |
| `terraform destroy` won't delete bucket | `force_destroy=false` + non-empty | empty it first (§4.1 step 1) |
| State lock error | stale local lock | `terraform force-unlock <ID>` / remove `.terraform.tfstate.lock.info` |
