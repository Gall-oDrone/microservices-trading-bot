# Path A — Intraday BTC-MXN Data Collector (2026-08-02)

Status note for the Cursor Agent / EC2 follow-up. This document captures the
prompt that drove Path A and what remains before the collector is considered
fully verified.

## Prompt summary (what Path A is)

Standalone **data-collection** service (not trading) that:

1. Connects to Bitso’s public WebSocket and archives every `btc_mxn` trade
   (book configurable via env, default `btc_mxn`).
2. Writes to **two sinks**: S3 Parquet (durable archive, partitioned
   `book/year/month/day`) and Postgres (hot store, last N days, default 7).
3. Reconnects with backoff; records structured WS gap windows.
4. Exposes `/healthz` dead-man’s switch (stale if no trade in X minutes,
   default 5) and Prometheus `/metrics`.
5. Ships with Terraform modules for a cheap standalone EC2 + S3 + optional
   RDS path under `infrastructure/terraform/` (development first).
6. Must **not** place orders, use trading API keys, touch trading services,
   or depend on Kafka/Redis.

Full original agent prompt lived in the Cursor chat that created branch
`feat/intraday-data-collector`. Guardrails from that prompt still apply:
no `terraform apply` without explicit review; no secrets in git; no changes
to trading services or `k8s/`.

## What is deployed on this branch (code only)

As of **2026-08-02**, the following is on `feat/intraday-data-collector`:

| Area | Path |
|------|------|
| Service | `services/data-collector/` (`cmd/`, `internal/`, unit tests, README) |
| Shared WS URL helper | `shared/pkg/bitso/websocket.go` (`NewWebSocketConnWithURL`) |
| TF modules | `modules/data-collector-ec2`, `data-archive-s3`, `data-collector-rds` |
| Dev wiring | `infrastructure/terraform/envs/development/{main,variables,outputs,versions}.tf` |
| Ops docs | `docs/data-collector/DEPLOYMENT.md`, `services/data-collector/README.md` |

Unit tests included (not executed on this machine):

- `internal/sink/batcher_test.go` — time- and count-based Parquet flush
- `internal/gap/detector_test.go` — gap records from connect/disconnect
- `internal/health/checker_test.go` — `/healthz` staleness with fake clock

## Explicitly deferred — run on the EC2 Cursor Agent

This pass **did not** install Go (or any other toolchain) and **did not**
run tests or `terraform apply`. On the IDE / collector EC2, the follow-up
agent should:

```bash
# From repo root on the feature branch
cd services/data-collector
go mod tidy          # generate go.sum (currently missing)
go build ./...
go vet ./...
go test ./...

# Optional infra review (plan only unless explicitly approved)
cd ../../infrastructure/terraform/envs/development
terraform init
terraform fmt -recursive
terraform validate
terraform plan
```

Also verify after a real deploy (see `DEPLOYMENT.md`):

- `curl http://<ip>:8085/healthz`
- Prometheus series under `/metrics`
- Hot Postgres `trades` / `ws_gaps`
- S3 keys under `trades/book=btc_mxn/...`

## Skipped / unsure (do not silently assume)

- **CI**: no `.github/workflows/` per-service pattern found → Path A CI step
  skipped (do not invent a new convention here).
- **`go.sum`**: absent until `go mod tidy` runs where Go is installed.
- **`terraform plan` / `apply`**: not run in this pass; apply remains blocked
  pending human review.
- **Binary onto collector EC2**: Terraform provisions instance + systemd;
  binary still needs cross-compile + copy (documented in `DEPLOYMENT.md`).
- **Gaps with Postgres disabled**: gap rows only persist when Postgres is on;
  otherwise they are logged only.
- Unrelated IDE deploy script CRLF/WSL hardening may be on the same branch
  (`infrastructure/cloudformation/deploy-ide.sh`).

## Next owner

Start a Cursor Agent **on the EC2 instance** with Go available, pull this
branch, run the commands above, then proceed to reviewed `terraform plan`
(and only then apply if approved).
