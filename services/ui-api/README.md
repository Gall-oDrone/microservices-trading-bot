# ui-api

Read-only backend for the web UI (`web/`). It reads the daily-executor's ledger and candle files,
evaluates the risk policy (`shared/pkg/risk`) against them, and serves JSON under `/api/ui/`.
Plan: [`docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md`](../../docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md).

```bash
cd services/ui-api
go run ./cmd                                   # stage ledger, built-in risk policy, 127.0.0.1:8090
go run ./cmd -ledger ../strategy-executor/daily-executor-data/ledger.jsonl   # the dry-run ledger
go run ./cmd -print-default-policy > risk-policy.json   # starting point for a policy file
go run ./cmd -risk-policy risk-policy.json -static ../../web/dist            # also serve the built UI
go test ./...
```

| Flag | Env | Default |
|---|---|---|
| `-addr` | `UI_API_ADDR` | `127.0.0.1:8090` (non-loopback refused unless `UI_API_ALLOW_REMOTE=1`) |
| `-ledger` | `UI_API_LEDGER` | `../strategy-executor/daily-executor-data/stage/ledger.jsonl` |
| `-candles-dir` | `UI_API_CANDLES_DIR` | `<ledger dir>/candles` |
| `-risk-policy` | `UI_API_RISK_POLICY` | built-in `risk.DefaultPolicy()` |
| `-static` | `UI_API_STATIC_DIR` | none |
| `-stage-size` | `UI_API_STAGE_SIZE` | `0.001` (the executor's `-size`) |

## Endpoints (GET only; anything else is 405)

| Path | Returns |
|---|---|
| `/api/ui/healthz` | status, ledger path, record count, policy source |
| `/api/ui/forward-tests` | one summary per book: decision, paper vs hold, distance to SMA, milestones, run status, risk counts |
| `/api/ui/forward-tests/{book}` | the same for one book |
| `/api/ui/forward-tests/{book}/ledger[?mode=stage]` | raw records, equity series, stage fills with fee + slippage in bps |
| `/api/ui/forward-tests/{book}/candles?days=180` | daily candles with SMA50, `long`, and 20-day volume ratio |
| `/api/ui/risk` | policy, per-book exposure, limit utilization, next stage order run through `risk.Check`, the executor's last recorded check and blocked days, realized cost, findings |

## Guarantees

- **Read-only.** It never writes the ledger, the candles or the policy, and exposes no endpoint that
  can place, cancel or halt anything.
- **Risk is enforced by the executor, displayed here.** Since 2026-10-05 the daily-executor runs
  `shared/pkg/risk.Check` before every stage order and records it in the ledger (`stage.risk`;
  `stage.action: "blocked"` when stopped). `/risk` says `"enforcement": "enforced"`, shows that last
  check, and warns with `order_blocked` and `policy_mismatch` (executor ran under another policy
  version). Start ui-api with the same `-risk-policy` file as the executor. The next-order preview
  prices at the last close; the executor prices at the live best bid/ask.
- **Contract.** The ledger types come from `shared/pkg/dailyledger`.
  `cmd/daily-executor/ledger_contract_test.go` fails if the executor's JSON and that mirror drift
  apart, and `web/src/test/app.test.tsx` parses captured responses with the UI's zod schemas.
