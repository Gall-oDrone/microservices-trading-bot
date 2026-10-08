# ui-api

Backend for the web UI (`web/`), read-only except the opt-in R4 halt/resume controls. It reads the daily-executor's ledger and candle files,
evaluates the risk policy (`shared/pkg/risk`) against them, and serves JSON under `/api/ui/`.
Plan: [`docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md`](../../docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md).

```bash
cd services/ui-api
go run ./cmd                                   # stage ledger, built-in risk policy, 127.0.0.1:8090
go run ./cmd -ledger ../strategy-executor/daily-executor-data/ledger.jsonl   # the dry-run ledger
go run ./cmd -print-default-policy > risk-policy.json   # starting point for a policy file
go run ./cmd -risk-policy risk-policy.json -static ../../web/dist            # also serve the built UI
go run ./cmd -archive s3://mtb-development-data-archive-<account>          # Data health: list the collector's archive
go run ./cmd -ledgers stage=s3://<bucket>/daily-executor/stage,local=../strategy-executor/daily-executor-data/stage/ledger.jsonl
                                               # a ledger from the copy scripts/daily-executor-run.sh uploads
go run ./cmd/ui-alerts -dry-run                # R3 alerts: what would be sent (see cmd/ui-alerts, scripts/install-ops-cron.sh)
go run ./cmd -operator-token-file ~/.config/mtb/operator-token   # R4: halt/resume from the Risk page (0600 file, loopback only)
go test ./...
```

| Flag | Env | Default |
|---|---|---|
| `-addr` | `UI_API_ADDR` | `127.0.0.1:8090` (non-loopback refused unless `UI_API_ALLOW_REMOTE=1`) |
| `-ledger` | `UI_API_LEDGER` | `../strategy-executor/daily-executor-data/stage/ledger.jsonl`; or `s3://bucket/prefix` |
| `-ledgers` | `UI_API_LEDGERS` | none; `name=path,name=s3://bucket/prefix,…` (first is the default; every endpoint takes `?ledger=`) |
| `-candles-dir` | `UI_API_CANDLES_DIR` | `<ledger dir>/candles` (disk only) |
| `-risk-policy` | `UI_API_RISK_POLICY` | built-in `risk.DefaultPolicy()` |
| `-static` | `UI_API_STATIC_DIR` | none |
| `-stage-size` | `UI_API_STAGE_SIZE` | `0.001` (the executor's `-size`) |
| `-archive` | `UI_API_ARCHIVE` | none: the Data health archive section is "not configured" |
| `-operator-token-file` | `UI_API_OPERATOR_TOKEN_FILE` | none: controls off. A 0600 file with a token of ≥ 32 characters (`umask 077; openssl rand -hex 32 > …`); needs a loopback `-addr` |

## Endpoints (GET only, plus the two R4 POSTs; anything else is 405)

| Path | Returns |
|---|---|
| `/api/ui/healthz` | status, ledger path, record count, policy source |
| `/api/ui/forward-tests` | one summary per book: decision, paper vs hold, distance to SMA, milestones, run status, risk counts |
| `/api/ui/forward-tests/{book}` | the same for one book |
| `/api/ui/forward-tests/{book}/ledger[?mode=stage]` | raw records, equity series, stage fills with fee + slippage in bps |
| `/api/ui/forward-tests/{book}/candles?days=180` | daily candles with SMA50, `long`, and 20-day volume ratio |
| `/api/ui/risk` | policy, per-book exposure, limit utilization, next stage order run through `risk.Check`, the executor's last recorded check and blocked days, realized cost, findings |
| `/api/ui/health/data` | data health: the collector's hourly S3 flushes per book (age, 24 h cadence, gaps), daily compaction progress (latest `_manifest.json`), the executor's ledger coverage, its last `run-*.log` (exit code, S3 upload) and an ok/warn/fail check list. The S3 listing is cached 60 s |
| `/api/ui/controls` | R4: whether halt/resume is enabled for the ledger (and why not), its halt file, and the audit log newest first |
| `POST /api/ui/risk/halt`, `POST /api/ui/risk/resume` | R4: write `risk-state.json` next to a **local** ledger. Bearer operator token, loopback `Origin`, JSON `{reason, by, confirm}` (`confirm` = ledger name); every attempt is appended to `ui-audit.jsonl` next to the ledger. Plan §8.12 |

## Guarantees

- **Read-only, except R4.** It never writes the ledger, the candles or the policy, and exposes no
  endpoint that can place or cancel anything. The only writes are the opt-in R4 controls: the halt
  file and its audit log, for local ledgers, with `-operator-token-file` set (off by default). With
  `-archive` or an `s3://` ledger it only lists and reads
  objects (default AWS credential chain); it never writes to S3. `cmd/ui-alerts` is a separate
  command: it reads the same way, writes only its state file and publishes to the SNS topic.
- **S3 ledgers** read the layout `scripts/daily-executor-run.sh` uploads (`ledger.jsonl`,
  `risk-state.json`, `candles/*.csv`, `run-*.log`). One listing per 30 s, bodies cached by ETag; a
  new upload shows within 30 s. The halt file is parsed with the executor's rules
  (`risk.ParseHaltState`) and is as of the last upload.
- **Risk is enforced by the executor, displayed here.** Since 2026-10-05 the daily-executor runs
  `shared/pkg/risk.Check` before every stage order and records it in the ledger (`stage.risk`;
  `stage.action: "blocked"` when stopped). `/risk` says `"enforcement": "enforced"`, shows that last
  check, and warns with `order_blocked` and `policy_mismatch` (executor ran under another policy
  version). Start ui-api with the same `-risk-policy` file as the executor. The next-order preview
  prices at the last close; the executor prices at the live best bid/ask.
- **Contract.** The ledger types come from `shared/pkg/dailyledger`.
  `cmd/daily-executor/ledger_contract_test.go` fails if the executor's JSON and that mirror drift
  apart, and `web/src/test/app.test.tsx` parses captured responses with the UI's zod schemas.
