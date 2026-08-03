# Path A data-collector — end-to-end verification (2026-08-03)

Status snapshot for the standalone Bitso `btc_mxn` trade archiver (EC2 + S3 +
RDS Postgres). This records what was built, the bugs fixed while bringing it up
on live AWS, and the evidence that both sinks work end-to-end.

> Companion docs:
> - [`DEPLOYMENT.md`](./DEPLOYMENT.md) — reference deployment / cost / teardown
> - [`MANUAL-RUNBOOK-2026-08-03.md`](./MANUAL-RUNBOOK-2026-08-03.md) — copy-paste
>   manual apply + cleanup if the agent/scripts are unavailable
> - [`PATH-A-PROMPT-2026-08-02.md`](./PATH-A-PROMPT-2026-08-02.md) — original plan

## Result

**Deployed and verified on live AWS.** The collector runs on a `t4g.nano` EC2
instance, streams Bitso public trades over WebSocket, and persists to both
sinks:

- **S3 Parquet archive** — objects landing on the flush interval, e.g.:

  ```
  trades/book=btc_mxn/year=2026/month=08/day=03/trades-20260803T175938.052.parquet
  trades/book=btc_mxn/year=2026/month=08/day=03/trades-20260803T180038.053.parquet
  trades/book=btc_mxn/year=2026/month=08/day=03/trades-20260803T180138.052.parquet
  ```

- **Postgres hot store** — `trades` populating, `ws_gaps` empty (no disconnects):

  ```
  TRADES_COUNT: 6
  GAPS_COUNT:   0
  ```

- **Health/metrics** — `/healthz` returns `{"status":"healthy",...}` once the
  first trade arrives; `/metrics` exposes `data_collector_trades_received_total`
  and friends.

Deploy + verify is fully scripted (SSH-less, via SSM):
`infrastructure/terraform/scripts/deploy/deploy-data-collector.sh`.

## What was added

| Area | Change |
|------|--------|
| Deploy automation | `scripts/deploy/deploy-data-collector.sh` — build (arm64) → targeted apply → stage binary in S3 → SSM push → start unit → verify S3 + Postgres |
| Teardown automation | `scripts/cleanup/cleanup-data-collector.sh` — targeted destroy of the 3 collector modules + manual AWS fallback |
| SSM support | `data-collector-ec2` module: `enable_ssm` (default true) attaches `AmazonSSMManagedInstanceCore` and grants `s3:GetObject` on `deploy/*` — no inbound SSH needed |

## Bugs fixed during bring-up

1. **Parquet `WriteStop` nil-pointer panic.**
   `xitongsys/parquet-go` v1.6.2 doesn't encode `time.Time` fields tagged as
   `INT64/TIMESTAMP_MILLIS`. Fix: a dedicated `parquetTrade` struct with
   `int64` millisecond timestamps in `services/data-collector/internal/sink/batcher.go`;
   removed the misleading `parquet` tags from `models.Trade`.

2. **WebSocket `maker_side` unmarshal failure (connection drops).**
   Bitso sends the trade `"t"` field as a JSON **number** (`0`/`1`), not a
   string. Fix: custom `UnmarshalJSON` on `WebSocketTradePayload` in
   `shared/pkg/bitso/websocket.go` that accepts number **or** string and
   normalizes to `"0"`/`"1"`.

3. **EC2 `InvalidBlockDeviceMapping` — root volume too small.**
   The AL2023 arm64 AMI requires ≥30 GB. Fix: `root_block_device.volume_size`
   raised 8 → 30 GB in `modules/data-collector-ec2/main.tf` (cost note updated
   in `DEPLOYMENT.md`).

4. **RDS `InvalidParameterCombination` — pinned minor version unavailable.**
   Fix: `engine_version` default `"16.4"` → `"16"` in
   `modules/data-collector-rds/variables.tf` so RDS picks a supported minor.

5. **User-data OOM-kill on `t4g.nano` (512 MB).**
   `dnf install` was OOM-killed before the systemd unit was written. Fix:
   create/enable a 1 GiB swapfile **before** any `dnf`, and slim the install to
   just `jq` (awscli is preinstalled on AL2023) in the user-data.

6. **SSM `InvalidDocument`.**
   Used the wrong document name. Fix: `AWS-RunShellScript` (not
   `AWS-RunShellCommand`) in the deploy script.

## Cost / running-infra reminder

The verified stack is **billable while running** (~$15–20/mo with RDS on, plus
a shared-VPC NAT ~$32/mo if the VPC was created for this path). Tear it down
when not collecting:

```bash
infrastructure/terraform/scripts/cleanup/cleanup-data-collector.sh development
```

See the manual runbook for the no-agent teardown and the NAT caveat.
