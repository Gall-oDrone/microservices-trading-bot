# Ledger watchdog (off-machine executor check)

The daily executor and the R3 alerts run from the operator's workstation
(`scripts/install-ops-cron.sh`). If that machine is off, or its cron stops, nothing runs **and
nothing alerts**. This Lambda closes that gap from AWS. Once an hour it checks the S3 copy that
every run leaves under `s3://<bucket>/daily-executor/<ledger>/`. `scripts/daily-executor-run.sh`
and `scripts/etoro-daily-executor-run.sh` upload `run-<UTC>.log` last, after its `exit=` line.
When a ledger is stale, the Lambda publishes to the operators' SNS topic (`mtb-operator-alerts`,
the same email as ui-alerts). Two calendars:

- **daily** (`name=prefix`): the Bitso stage run, every day at 06:15 UTC; stale when the newest run
  log is older than **30 h** (`MAX_AGE_HOURS`).
- **nyse** (`name=prefix:nyse`, added 2026-10-17 for the eToro demo executor): the run happens at
  **09:35 New York on NYSE trading days only**; stale when no run log follows the last trading
  day's run within **6 h** (`MAX_AGE_HOURS_NYSE`). Weekends and holidays never page; a missed
  Monday run pages at about 15:35 New York. The NYSE calendar is computed in the function (no
  dependencies): weekends, the ten scheduled holidays with their observance rules, Good Friday via
  the Gregorian Easter algorithm, mirroring `shared/pkg/mktcal`; the holiday lists for 2026 and
  2027 are pinned in the tests against `mktcal`'s.

Plan: `docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md` §8.10; eToro runbook
`docs/etoro/ETORO-DAILY-EXECUTOR.md` §3.1.

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
  --topic-arn arn:aws:sns:us-east-1:<account>:mtb-operator-alerts \
  --ledgers "stage=daily-executor/stage,etoro-demo=daily-executor/etoro-demo:nyse,etoro-demo-etf=daily-executor/etoro-demo-etf:nyse,etoro-demo-rotation=daily-executor/etoro-demo-rotation:nyse"
  # the four ledgers deployed on 2026-10-21 (stage, and the three eToro cells); add --test for a test email
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
