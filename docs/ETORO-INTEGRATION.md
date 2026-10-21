# eToro API integration

The trading engine can execute against **Bitso** (default) or the **eToro Public API** by setting `BROKER=etoro`. Branch `feat/etoro` ports the operator-ui work (daily SMA50 executor, risk, operator UI) to eToro **index CFDs**: NSDQ100 (instrument 28) and SPX500 (27), benchmarked against QQQ (3006) and SPY (3000). Plan: porting plan phases P0–P7. Verified API facts: [docs/etoro/evidence-2026-10-10/README.md](etoro/evidence-2026-10-10/README.md).

## Credentials

| Environment variable | eToro header | AWS Secrets Manager key | K8s secret key |
|---------------------|--------------|-------------------------|----------------|
| `ETORO_PUBLIC_KEY` | `x-api-key` | `trading-bot/etoro-public-key` | `etoro-public-key` |
| `ETORO_PRIVATE_KEY` | `x-user-key` | `trading-bot/etoro-private-key` | `etoro-private-key` |

Create secrets with:

```bash
./infrastructure/terraform/scripts/secrets/setup-secrets.sh
```

Or set `ETORO_PUBLIC_KEY` / `ETORO_PRIVATE_KEY` in the environment before running the script.

**Local development:**
- Keep the demo keys in `.env.etoro.local` at the repo root. It is git-ignored by `.env.*`.
- Load them with `set -a; . ./.env.etoro.local; set +a`.
- `scripts/check-no-etoro-secrets.sh` fails if a key value, or anything shaped like an eToro user key, is tracked or staged. Run it before every push; CI runs it too.

## Runtime configuration

| Variable | Description |
|----------|-------------|
| `BROKER` | `bitso` (default) or `etoro` |
| `ETORO_ENV` | `demo` (paper) or `real` — must match the key’s environment |
| `DRY_RUN` | Log orders without calling the broker API |
| `ETORO_DEFAULT_SYMBOL` | Default symbol label for logs when using eToro |

In Kubernetes, set `broker` and `etoro-env` in the `trading-config` ConfigMap (`k8s/base/configmap.yaml`).

**Demo only.** With `BROKER=etoro`, live mode (no `DRY_RUN`) refuses to start unless `ETORO_ENV=demo` (unset means demo). `TRADING_ENGINE_ALLOW_PRODUCTION=1` is the same explicit switch that keeps Bitso on stage (`services/trading-engine/internal/guard`).

## Trade signals (Kafka)

For eToro, set the signal `book` field to the ticker symbol (e.g. `AAPL`, `BTC`). Optional metadata:

```json
{
  "instrument_id": 1234,
  "reason": "strategy signal"
}
```

- **BUY**: opens a long position by cash `amount` at x1 through the v2 route `POST /api/v2/trading/execution[/demo]/orders`.
  - The order's `x-request-id` is derived from the signal's `event_id`, so a redelivered signal is rejected by eToro as a duplicate reference and does not open twice.
- **SELL**: closes the long positions held on the instrument (v1 market-close route).
  - It never opens a short: shorts need a stop-loss and are outside the long/flat policy.

## API facts the code relies on (verified on demo, 2026-10-10 and 2026-10-12)

- **Routes:**
  - v2 opens, `orders:lookup?orderId=`, costs and eligibility; v2 rates.
  - v1 search, candles, pnl portfolio, close and close-orders, trade history.
  - Demo and real paths differ non-uniformly; the full table is in `shared/pkg/etoro/paths.go`.
- **Idempotency:**
  - eToro rejects a reused `x-request-id` with 400 "ReferenceID … may already exists" (`etoro.IsDuplicateReference`).
  - `orders:lookup?referenceId=` does **not** find API-placed orders (404).
  - The broker adapter resolves a duplicate to the position the first attempt opened.
- **Consistency:** the pnl portfolio shows new and closed positions late, and the closed-positions history lags further (2026-10-16: a close gone from the portfolio after 10 s was not in the history yet; listed within 3 h): usually 1–3 s, but on 2026-10-12 a portfolio read 5 s after a filled open (which the adapter had already resolved to its position id) still showed no positions and an unchanged cash balance; 80 minutes later both positions and the margin were there. A position's `unrealizedPnL` is an object. `etoro-reconcile` therefore re-reads the portfolio for up to 60 s after an order the executor finished within the last 10 minutes.
- **Order status:** a filled order reads `{id 3, "Filled"}`. Re-closing a closed position gives error 741.
- **Rate limits:** 60 reads/min and 20 trading writes/min per user key. The client paces at 55/18 and retries reads on 429/5xx (honouring `Retry-After`). It never retries writes.
- **Daily candles come in two versions:** two reads of the live `OneDay` route about an hour apart returned 13–16 recent closes up to 1.3 bps apart (2026-10-10; opens/highs/lows identical). The executor reads twice and acts only when both agree (see the runbook).
- **Spreads depend on the session.** In the weekend session (Saturday 2026-10-10, 02:15 UTC) the cost preview quoted 15.5 bps (NSDQ100) and 14.8 bps (SPX500) of spread per open. In the NYSE cash session (Monday 2026-10-12, 09:35 New York) the live quotes were 0.23 bps and 0.38 bps wide and the previews 0.04–0.06 USD on 1,100 USD (≈0.4–0.5 bps). The pre-registration froze the weekend figures (its §3 fallback), so the frozen cost model is pessimistic for the CFD; the forward-test report must show the measured spreads next to the frozen ones.
- **Stop-loss / take-profit (from the portal's OpenAPI, 2026-10-19; called by ui-api since the same day, not yet by the executor):** an open order takes `stopLossRate` / `takeProfitRate`; an open position is edited with `PATCH /api/v2/trading/{demo|real}/positions/{positionId}` (`stopLossRate`, `takeProfitRate`, `stopLossType: fixed|trailing`, `clearStopLoss`, `clearTakeProfit`; `x-request-id` required; async `202` with `operationId`; `404` unknown, `409` closed; default 60/min quota; the client counts it against the write budget). A position with `stopLossRate 0.01` has no stop (`stops.NoStopRate`; the broker maps it to 0). Eligibility for the x1 long CFDs: `minStopLossPercentage` 1 (NSDQ100) / 0 (SPX500), `maxStopLossPercentage` 100, `maxTakeProfitPercentage` 1000, `allowEditStopLoss` true, **`allowTrailingStopLoss` false** (trailing is emulated by the planned daily ratchet). The first PATCH against the real demo account is still to be evidenced (the adapter is contract-tested against the OpenAPI shapes). Plan and status: [etoro/ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md](etoro/ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md); operator view: runbook §9.

## eToro MCP server (operators and IDE agents only)

- eToro runs an official remote MCP server at `https://mcp.public-api.etoro.com` (streamable HTTP) with a companion skill at `https://mcp.public-api.etoro.com/skill`.
- It exposes route discovery (`get-all-routes`, `get-route-spec`), account views and `prepare-trade` → `place-trade` / `prepare-close` → `place-close` with a confirmation gate.
- Copy [docs/etoro/mcp_config.example.json](etoro/mcp_config.example.json) into your IDE's MCP config (Antigravity: `mcp_config.json`) and fill in the keys locally.
- Use it for ad-hoc demo checks and to confirm route specs. **The bots never use it**: they call the REST API through `shared/pkg/etoro`.

## Code layout

- `shared/pkg/etoro/`: Public API client.
  - v2 orders, lookup, costs, eligibility, close, portfolio, history, candles, history candles; since 2026-10-19 `UpdatePosition` (the PATCH above) and `stopLossRate` / `takeProfitRate` on an open.
  - Read/write pacing, typed errors, deterministic request ids.
  - httptest contract tests use trimmed real demo responses.
- `shared/pkg/broker/`: venue-neutral execution seam. `broker/etorobroker` is the eToro adapter (idempotent `Open`, bounded polling). Since 2026-10-19: `Position.StopLoss / TakeProfit / StopType` (+ `Protected()`), `OpenRequest.StopLoss / TakeProfit`, the `ProtectionEditor` seam (`SetProtection`: PATCH, then poll the portfolio until the rates show; `ErrPositionNotOpen` on 404/409, `ErrOutcomeUnknown` on 5xx/429) and `ProtectionLimitsReader` (the eligibility bounds, cached an hour).
- `shared/pkg/stops/`: pure stop-loss / take-profit model (ATR(14), the regime, the three risk-aversion profiles, the guard chain, `DisasterStopPct`, `Ratchet`), shared by ui-api today and by the executor and reconcile in Phase C.
- `shared/pkg/risk/`: since 2026-10-19 `BookLimits.RequireStopLoss / MinStopLossPct / MaxStopLossPct / MaxLossPctEquity`, `Order.StopLoss / TakeProfit` and the rules `stop_loss_required / invalid / too_tight / too_wide`, `take_profit_invalid`, `max_loss_pct_equity` (exposure-adding orders only). `EtoroDemoPolicy()` (`etoro-demo-2026-10-10`, the executor's) is unchanged; `EtoroDemoProtectedPolicy()` (`etoro-demo-2026-10-19`) adds the stop rules and is what ui-api checks assisted trades with. `EtoroDemoETFPolicy()` (`etoro-demo-etf-2026-10-21`, since 2026-10-21) is the ETF cells' policy: books `qqq` / `spy`, the same per-leg limits, no stop requirement; the executor selects it for any ETF-only run and refuses a run that mixes products.
- `shared/pkg/mktcal/`: NYSE calendar (holidays, early closes, DST sessions, trading-day arithmetic).
- `shared/pkg/etorodaily/`: eToro daily candles turned into one bar per NYSE trading day. Written in the research CSV format, with a freshness check.
- `services/strategy-executor/cmd/etoro-spike/`: demo-only evidence recorder (`docs/etoro/evidence-*`).
- `services/strategy-executor/cmd/etoro-daily-executor/`: the SMA50 index-CFD executor on the demo account (P3). Runbook: [docs/etoro/ETORO-DAILY-EXECUTOR.md](etoro/ETORO-DAILY-EXECUTOR.md).
- `services/strategy-executor/cmd/etoro-reconcile/`: read-only reconciliation of its ledger against the demo account.
- `shared/pkg/etoroledger/`: the executor's ledger schema and intent journal. Since P5d it also holds the console's assisted trades: lines with `source: "ui"` and `operator`, keyed `book|bar_date|client_ref` (the daily lines keep `book|bar_date`), `LastDemoPosition` ordered by `(bar_date, recorded_at)`, `LegsOn` counting both, `UIClientRef`. Since 2026-10-19 (schema still `etoro-ledger/v1`, every addition `omitempty`): `Position.StopLossRate / TakeProfitRate / StopType`, the actions `protect` and `venue_close` (neither is a leg for `LegsOn`), `Demo.Protection` (profile, ATR, the rates before, max loss) and `Demo.VenueClose` (`stopped_out` / `took_profit` / `other`, written by Phase C).
- `services/ui-api/` (operator UI backend, P5a): `internal/api/etoro.go` and `etoro_views.go` serve the ledger under `/api/ui/etoro/` (forward tests, per-book ledger and candles, risk with `risk.EtoroDemoPolicy` and the halt file, health, reconcile reports, the P4 risk report); `internal/etorohealth/` is the file-only health check shared with `ui-alerts`; the ledger is registered with `-etoro-ledger` (`UI_API_ETORO_LEDGER`) as venue `etoro`, so the shared halt/resume and halt-all controls write its `risk-state.json` and audit to `ui-audit.jsonl` next to it. `web/scripts/capture-fixtures.sh` captures the eToro endpoints for the web fixtures. P5c adds `internal/etorolive/` (a rates poller: one `GetRates` for NSDQ100/SPX500 every 5 s = 12 reads/min, display only) feeding a second `live.Hub` (UTC-day bars, mid as price) behind `GET /api/ui/etoro/live` and `GET /api/ui/etoro/stream` (`internal/api/etoro_live.go`; the `/live`/`/stream` shapes plus the NYSE `session`); the demo keys come from `-etoro-env-file` (`UI_API_ETORO_ENV_FILE`, default `~/.config/microservices-trading-bot/etoro-demo.env`, `ETORO_ENV=demo` required, `real` refused), `-etoro-live=false` turns the feed off. P5d adds `internal/api/trades.go`: the assisted trades `POST /api/ui/etoro/trades/prepare` (halt file → `risk.Check` → eligibility → quote → costs → cash; a preview with an HMAC token, 120 s, single use, bound to the operator session) and `/place` (re-checks, the executor's `.lock`, intent first, the `etorobroker` adapter, the ledger line with `source: "ui"`, audit), **off unless `-etoro-trading`** (`UI_API_ETORO_TRADING=1`) with the operator token, the local ledger and the demo env file; `GET /controls` reports `trading`. The web Trade page is `web/src/pages/etoro/EtoroTradePage.tsx` with `src/api/trades.ts` and the MSW mock `src/mocks/trades.ts`. Runbook §8 and the frontend plan §8.16. Protection (2026-10-19) adds `internal/api/protection.go`: `POST /api/ui/etoro/trades/recommend` (the three profiles from the ledger's candles, the live quote, the account and the venue bounds; sends nothing), `profile` / `stop_loss_pct` / `take_profit_pct` on prepare, a stop and optional target on every open, and the `protect` side that edits the held position (phrase `PROTECT <SYMBOL>`, a `protect` ledger line, not a leg); the policy is `EtoroDemoProtectedPolicy`. Web: `components/etoro/protection.tsx`, `lib/protection.ts`. Runbook §9 and the frontend plan §8.17. The same day adds `internal/api/positions.go`: `GET /api/ui/etoro/positions[?book=]`, one row per position paired from the ledger's open / protect / close / venue_close lines (fills, gross P&L, source, operator, who closed it) plus, when the demo broker is present, the account's unrealised P&L and the history's net profit / fees (three reads, cached 45 s per ledger); a ledger position the account no longer holds is classified from the history (stop / target / outside / unknown). Web: `components/etoro/positions.tsx` on the Trade page and the book page. Runbook §8 "Where to see what became of a trade" and the frontend plan §8.18.
- `services/trading-engine/internal/execution/etoro_executor.go`: Kafka-signal execution (`BROKER=etoro`).
- `shared/pkg/config/broker.go`: `BROKER` parsing.

## Agentic research (TradingAgents + financial news)

Cold-path integration (separate namespace `trading-research`):

- `docs/agentic-ai/AGENTIC-AI-ETORO-TRADINGAGENTS-INTEGRATION-PLAN-2026-05-22.md`
- `docs/agentic-ai/AGENTIC-AI-FINANCIAL-NEWS-S3-ETL-2026-05-22.md`
- `services/news-publisher` — S3 ETL → Kafka `news.agentic`
- `services/research-agent` — [TradingAgents](https://github.com/Gall-oDrone/TradingAgents) HTTP wrapper

Deploy: `kubectl apply -k k8s/overlays/research`

**News → signals (optional):** set `NEWS_ENABLED=true` on `strategy-executor` to consume `news.agentic` and gate BUY signals by sentiment.

**Research → signals (operator gate):** set `RESEARCH_API_ENABLED=true` on `api-gateway` to list S3 memos and approve them into `trading.signals` with an `audit_id`. See `docs/agentic-ai/AGENTIC-AI-ETORO-IMPLEMENTATION-STATUS-2026-05-27.md`.

## Operations (P6, 2026-10-17)

- **Cron** (`scripts/install-ops-cron.sh`, managed `mtb-ops` block): the demo run at 09:35 New York on
  weekdays (`ops-run.sh etoro`; two UTC candidates with a New York-hour guard, this host's cron
  ignores `CRON_TZ`), the weekly P4 risk report on Monday 15:00 UTC (`ops-run.sh etoro-risk`), and
  `ui-alerts` every 15 minutes over the eToro ledger directory. `--no-etoro` opts out;
  `--rebuild-etoro` rebuilds the three binaries from the committed HEAD.
- **S3 copy**: `s3://<bucket>/daily-executor/etoro-demo/` holds `ledger.jsonl`, `risk-state.json`
  (while halted), `candles/`, `reconcile/`, `intents/`, `risk/<date>/` and every `run-<UTC>.log`
  (uploaded last). `scripts/etoro-daily-executor-run.sh --upload-only` re-sends.
- **Watchdog** (`infrastructure/lambda/ledger-watchdog`, `etoro-demo=daily-executor/etoro-demo:nyse`):
  pages when no run log follows the last NYSE trading day's 09:35 New York run within 6 h; quiet on
  weekends and holidays (NYSE calendar computed in the function, pinned to `mktcal`).
- **Alerts** (`ui-alerts`, `internal/etorohealth`): coverage, run exit, reconcile (with the re-check
  rule), unrecorded intents, halt, upload. Runbook §3.1.
- **Assisted trades** are off unless ui-api runs with `-etoro-trading` (runbook §8).
- Environment variables: [OPERATIONS-ENV-VARS.md](OPERATIONS-ENV-VARS.md). Weekly summary:
  [OPERATIONS-UPDATE-2026-10-16.md](OPERATIONS-UPDATE-2026-10-16.md).

## Branch

The original integration lives on `feat/etoro-api-integration` (based on `feat/k8s-deployment-manifests`). It was merged into `feat/etoro` together with `feat/operator-ui` on 2026-10-10.
