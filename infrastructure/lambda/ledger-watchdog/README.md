# Ledger watchdog (off-machine executor check)

The daily executor and the R3 alerts run from the operator's workstation
(`scripts/install-ops-cron.sh`). If that machine is off, or its cron stops, nothing runs **and
nothing alerts**. This Lambda closes that gap from AWS. Once an hour it checks the S3 copy that
every run leaves under `s3://<bucket>/daily-executor/<ledger>/`. `scripts/daily-executor-run.sh`
uploads `run-<UTC>.log` last, after its `exit=` line. When the newest run log is older than
**30 h**, the Lambda publishes to the operators' SNS topic (`mtb-operator-alerts`, the same email
as ui-alerts).

Plan: `docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md` §8.10.

## Behaviour

- **Stale**: sent on the first hourly run after the newest run log turns 30 h old, then every 12 h
  while it stays stale. The run is daily at 06:15 UTC, so a missed run is reported at about 12:05
  UTC the next day.
- **Resolved**: sent once, on the first hourly run after a new run log ends a gap of 30 h or more.
- **No run logs** under a prefix: sent at 00:05 and 12:05 UTC.
- Each message adds the last run's `exit=` and `upload=` lines and the age of `ledger.jsonl`.
- **No state is kept.** EventBridge runs it once per clock hour (`cron(5 * * * ? *)`), and each
  decision uses a 62-minute window, so no alert is missed. A rare duplicate is possible.
- **Access**: read-only S3 (`ListBucket` limited to `daily-executor/*`, `GetObject` on
  `daily-executor/*`) and `sns:Publish` on the one topic. Logs are kept 30 days.

## Deploy / remove

```bash
infrastructure/lambda/ledger-watchdog/deploy.sh \
  --bucket mtb-development-data-archive-<account> \
  --topic-arn arn:aws:sns:us-east-1:<account>:mtb-operator-alerts      # add --test for a test email
infrastructure/lambda/ledger-watchdog/deploy.sh --delete
```

`deploy.sh` does four things:

1. Runs the unit tests.
2. Packages `src/` to `s3://<bucket>/lambda-artifacts/ledger-watchdog/`.
3. Deploys the CloudFormation stack `mtb-ledger-watchdog`: the function, the IAM role
   `mtb-ledger-watchdog`, the rule `mtb-ledger-watchdog-hourly` and the log group.
4. Invokes the function once with `{"dry_run": true}`.

Other events: `{}` for a scheduled check, and `{"test": true}` to publish a test message.
Options:
- `--ledgers name=prefix,…` (every prefix under `daily-executor/`)
- `--max-age-hours`
- `--region`

## Tests

```bash
python3 -m unittest discover -s infrastructure/lambda/ledger-watchdog -v
```

The tests use fake S3 and SNS clients, so boto3 is not needed. They cover:
- the decision windows, including a 72 h simulation of hourly runs: exactly one first alert,
  then reminders every 12 h;
- resolution only after a gap;
- no run logs;
- pagination;
- message rendering (ASCII subject under 100 characters);
- dry-run, test and error paths.
