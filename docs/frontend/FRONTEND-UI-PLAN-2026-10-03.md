# Frontend UI Plan: React + TypeScript (2026-10-03)

**Goal.** A web UI that lets one operator see, at a glance, what the trading bot is doing and whether
it is working: forward-test status, execution risk, research and backtest results, market data,
data-pipeline health and, later, gated controls.

**Status (updated 2026-10-05).** **Phase 0 and Phase 1 (local) are built**, plus a risk-management
layer shared by the backend and the UI. Since 2026-10-05 the daily-executor **enforces** it before
every stage order (step R1, §6.4):

| Piece | Where | State |
|---|---|---|
| Web app | [`web/`](../../web/README.md) | Forward tests, forward-test detail, Risk pages; `npm run ci` green (typecheck, lint, prettier, 17 tests, build) |
| UI backend (BFF) | [`services/ui-api/`](../../services/ui-api/README.md) | Read-only, localhost-only Go service reading the daily-executor ledger and candles; 9 handler tests |
| Risk library | [`shared/pkg/risk`](../../shared/pkg/risk/) | Policy, pure pre-trade `Check`, drawdown and cost warnings; table tests |
| Risk enforcement | [`cmd/daily-executor/risk.go`](../../services/strategy-executor/cmd/daily-executor/risk.go) | `risk.Check` before every stage order; blocked orders are skipped and recorded; tests with a fake exchange |
| Ledger contract | [`shared/pkg/dailyledger`](../../shared/pkg/dailyledger/) | Importable mirror of the executor's JSONL; contract test in `cmd/daily-executor` |

CI runs in [`.github/workflows/operator-ui.yml`](../../.github/workflows/operator-ui.yml) (Go 1.22 vet + `-race`
tests for shared, daily-executor and ui-api; `npm ci && npm run ci` on Node 22).

What is **not** done yet: there is no halt switch the UI can flip (R2/R4), no alerting (R3), and
nothing is deployed beyond localhost.

---

## 1. What exists today (survey of the repo, 2026-10-03)

| Area | State |
|---|---|
| Web UI | **`web/`** (new, this plan). Monitoring of services stays in Grafana (`monitoring/grafana/dashboards/`: per-service, trading-metrics, data-pipeline, Kafka, Redis). |
| UI backend | **`services/ui-api`** (new): `GET`-only JSON under `/api/ui/`, binds `127.0.0.1:8090`, refuses other addresses unless `UI_API_ALLOW_REMOTE=1`. |
| APIs | REST/JSON only. **No WebSocket, SSE or gRPC servers.** |
| `market-data` (8083) | `/api/v1/ticker`, `/orderbook`, `/trades`, `/bars`, `/market/summary`, `/stats/volume`, … CORS `*`. An `X-API-Key` middleware exists but is not wired. |
| `strategy-executor` (8082) | Strategy CRUD and start/stop, indicators (`/api/v1/indicators/{book}/…`), backtests (`POST/GET /api/v1/backtests`). Backtest jobs live **in memory**: lost on restart, not shared between replicas. |
| `backtesting` (8084) | Separate engine with persisted results (Redis/file/S3), `equity_curve`, HTML/JSON reports, optimizations. |
| `api-gateway` (8085) | Aggregator and the only Ingress target. **Several routes proxy to endpoints that do not exist** (orders and positions on order-management, `PUT /strategies/{name}/config`). Default ports drift from compose/k8s. |
| `daily-executor` | **No HTTP surface.** A CLI that writes an append-only JSONL ledger (`daily-executor-data/stage/ledger.jsonl`) and candle CSVs on the machine that runs it. `ui-api` reads both. |
| Data collector | Writes trades to Postgres (7-day hot store) and S3 (`trades/`, `trades_compacted/`). Has `/healthz` and `/metrics` on the instance; not reachable from outside. |
| Live data | Kafka `market-data.trades/orderbook/ticker`, `trading.signals`, `trading.order.fills`, …; Redis `ticker:{book}`, `orderbook:{book}`, `recent_trades:{book}`. |

> [!WARNING]
> **Security must come first.** Gateway auth is off by default, and when enabled it accepts **any
> non-empty** API key or JWT (`api-gateway/internal/middleware/auth.go`). The ALB ingress is
> internet-facing over plain HTTP. Endpoints that can cause trades
> (`POST /api/v1/strategies/process`, `/test/signals`, strategy start/stop, order cancel) are
> reachable by anyone who can reach the services. **A UI must not be exposed until this is fixed.**
> `ui-api` therefore listens on loopback only and has no write endpoints.

---

## 2. Principles

1. **Read-only first.** The first releases only display data. Anything that changes trading (start/stop, kill switch) comes later, behind real auth, roles and an audit log.
2. **No exchange keys in the browser, ever.** The UI talks only to our own backend.
3. **One backend for the UI.** A small "backend for frontend" (BFF) service, in Go like the rest of the repo, aggregates the services and data stores. The browser never calls `market-data`, `strategy-executor` or S3 directly. This hides the port drift and the broken gateway routes, and keeps CORS and auth in one place.
4. **Typed contracts end to end.** Go structs → JSON → zod schemas (`web/src/api/schemas.ts`), validated at runtime. Two contract tests guard drift (§5.2).
5. **Show honesty, not hype.** Every performance chart shows the buy-and-hold benchmark and the costs. Paper, stage and backtest numbers are always labelled.
6. **Risk controls guard execution, never signals.** The pre-registrations freeze the SMA50 rule (§1–4 of each file). Risk limits may stop an *order* (size, notional, price, count, halt); they may never change a *signal*. Drawdown levels only flag a review, because stopping a forward test on bad results needs a written reason (pre-registration §2).

---

## 3. Stack (as built)

| Concern | Choice | Notes |
|---|---|---|
| Build | **Vite 7 + React 19 + TypeScript 5.8** (strict) | Template default is React 19, not 18 |
| Routing | **React Router 7** (library mode) | Simpler than TanStack Router for three routes; revisit if search-param state grows |
| Server state | **TanStack Query 5** | 60 s refresh for dashboards, 5 min stale time for candles |
| API types | **zod** schemas, hand-written to mirror the Go view models | `openapi-typescript` deferred until `ui-api` has an OpenAPI spec (Phase 2) |
| Charts | **TradingView Lightweight Charts 5** for both candles and equity | ECharts deferred until a chart needs it (heatmaps, histograms in Phase 2) |
| Tables | Plain semantic tables | TanStack Table deferred until a table needs sorting or virtualization |
| Styling | **Global design tokens** (`web/src/index.css`, CSS variables), dark theme | One stylesheet instead of CSS Modules while the app is small |
| Components | Own primitives (`components/ui.tsx`) | Radix deferred until dialogs are needed (Phase 4 confirmations) |
| Live updates | Polling (TanStack Query) | SSE in Phase 3 with the market page |
| Testing | **Vitest + Testing Library + MSW** | MSW serves fixtures **captured from the real `ui-api`** (`npm run fixtures`); Playwright deferred |
| Quality | ESLint (typescript-eslint), Prettier, `tsc -b` | `npm run ci` runs all of them plus the build |
| Node | Portable Node 22 in `.tools/node` (gitignored) | No system Node on the dev box |

Location: `web/` at the repo root, served by Vite in development (proxying `/api` to `ui-api`) and by
`ui-api -static web/dist` for a single-process local deploy.

---

## 4. Pages

### 4.1 Forward tests (built)
`/`: one card per book (`btc_mxn`, `btc_usd`), from the latest ledger record:
- Signal pill, close vs SMA50, paper equity vs buy-and-hold **after the pre-registered leg cost**, max drawdown, days and fills.
- A plain-language line: "the close is 14.9% above its 50-day average; it flips to flat if the price falls about that much", plus the next open's action.
- Forward-window progress, with a tick at the interim look (2027-03-26) and the evaluation date (2027-09-26).
- Badges: run status (`up to date` / `run pending` / `run missed`), stage holdings, risk warnings and blocks, candle gaps.
- A banner when the executor has missed a closed day (Mexico City calendar). Before 06:00 Mexico City, a missing run shows as *pending*.

`/forward-tests/:book` (built):
- Headline stats; **daily candles** with SMA50, volume (bars ≥ 1.5× the 20-day average highlighted, matching the volume study), markers on signal flips and stage fills; 90/180/365-day range.
- **Paper equity vs buy-and-hold** (dashed benchmark), interim/evaluation countdown, stage holdings.
- **Stage execution table**: maker/taker split, average price, fill-day open, fee bps, slippage bps vs that open, total cost **vs the pre-registered assumption** (red when above), market fallback, duration.
- **Ledger viewer**: every record, candle fingerprint, raw JSON of the latest record.

### 4.2 Risk (built; enforced by the executor since 2026-10-05)
`/risk`:
- Banners for **halt** (from the policy) and **enforcement mode** (`enforced`).
- Counters: halt state, blocks, warnings, policy version and source.
- Per book: stage position and its notional, **realized cost per leg** (fee + slippage) vs assumed, paper drawdown vs review level, limit meters (position, entry order size, entry notional, drawdown), **the next stage order run through `risk.Check`** ("would pass" / "would be blocked" with reasons), **the executor's last recorded check** (touch price vs close, policy version, sent / blocked, blocked days), and findings.
- Policy table: every limit per book, with its type (block or warn).

### 4.3 Research and backtests (Phase 2)
- List of studies (`docs/backtest-readiness/*.md`) with their verdicts, linked to evidence files.
- Strategy comparison view: return, max DD, Sharpe, trades and cost drag, development vs holdout side by side (from `weekly-research` / `daily-research` outputs, exported as JSON).
- Backtest runs from the `backtesting` service: equity curve, trades, parameters. Pick **one** canonical engine first (see §7).
- The volume-confirmed SMA50 variant, once pre-registered, gets its own forward-test card from its separate dry-run ledger (`ui-api -ledger …`; multi-ledger support needed, §8).

### 4.4 Market (Phase 3)
- Ticker and spread per book, recent trades, order-book depth (from `market-data` REST, live via SSE).
- Daily candles with volume and the 20-day volume ratio. The ratio is already computed by `ui-api` (`volume_ratio_20d`).

### 4.5 Data health (Phase 2)
- Collector: last trade age per book, WebSocket gaps, Postgres row counts.
- S3: latest raw partition per book, compaction status (`trades_compacted/` up to which day).
- Daily-executor: last successful run (already shown as run status), ledger uploaded to S3 or not, last exit code.

### 4.6 Controls (Phase 4, gated)
- Strategy list with state; start/stop; a global trading halt; the daily-executor halt.
- Every action needs a confirmation, a reason, and is written to an audit log.
- Only after auth, roles and audit exist.

---

## 5. Backend for the UI (`services/ui-api`, built)

### 5.1 Endpoints (GET only; any other method returns 405)

| Endpoint | Source | Notes |
|---|---|---|
| `GET /api/ui/healthz` | process + ledger stat | record count, policy source |
| `GET /api/ui/forward-tests` | latest ledger record per book | + distance to SMA, excess vs hold, milestones, run status, risk counts |
| `GET /api/ui/forward-tests/{book}` | same, one book | `{book}` validated against `^[a-z]{2,6}_[a-z]{2,6}$` and the known books |
| `GET /api/ui/forward-tests/{book}/ledger[?mode=stage]` | all records for a book | + equity series and stage fills with fee/slippage bps |
| `GET /api/ui/forward-tests/{book}/candles?days=180` | latest `<book>_daily_<date>.csv` | + SMA50, `long`, `volume_ratio_20d`; SMA50 matches the ledger to the cent (tested) |
| `GET /api/ui/risk` | ledger + candles + risk policy | policy, exposure, utilization, next-order check, realized cost, findings |

Files are cached by mtime; the ledger is re-read only when it changes.

Still to build (Phase 2–3): `/research/studies`, `/research/runs/{id}` (needs `-json` on the research
tools), `/market/{book}/…` proxy, `/market/stream` SSE, `/health/data`.

### 5.2 Contracts
- **Go ↔ Go:** `shared/pkg/dailyledger` mirrors the executor's unexported ledger types.
  `services/strategy-executor/cmd/daily-executor/ledger_contract_test.go` marshals a fully populated
  executor record and requires a lossless round trip through the mirror (`DisallowUnknownFields`).
- **Go ↔ TS:** `web/src/test/app.test.tsx` parses every captured `ui-api` response with the zod
  schemas. `npm run fixtures` re-captures them from a running `ui-api`.
- Correction to the earlier draft of this plan: `candles.recent_gaps` is a **string**, not `string[]`.
  `web/src/api/schemas.ts` is now the canonical TypeScript contract.

### 5.3 Prerequisites still open
- **Ledger location.** The ledger is a local file on whichever machine runs the executor. Upload it to `s3://mtb-development-data-archive-…/daily-executor/` after each run (planned as phase 3 of the executor), then add an S3 reader to `ui-api/internal/store`.

---

## 6. Risk management (backend + frontend)

### 6.1 What already existed (survey)

| Component | Enforces | Wired? |
|---|---|---|
| `order-management/internal/risk` | position ≤ `MAX_POSITION_SIZE`, open orders, order value, orders/minute; concentration (warn) | **Yes**, on the OM order path (`order_manager.go` → `CheckRisk`). But trading-engine can still place an order after OM rejects it (comments at `order_manager.go` L456/648). |
| `order-management` `GET /api/v1/risk/session` | daily realized P&L, drawdown % | Read by trading-engine |
| trading-engine `CheckSessionLimits` | `MaxDailyLoss`, `MaxDrawdownPct` | Called, but **both are 0** in `createTradingConfig()`, so it never blocks |
| strategy-executor `internal/risk` | trade amount, positions, hours | **Dead code** (only used by an unused manager) |
| Strategy params `max_daily_loss_quote` | per-strategy daily-loss pause | limit-profit and momentum only |
| daily-executor | `DAILY_EXECUTOR_DISABLED=1`, `-size` ≤ 0.01 BTC, stale-candle refusal, history-revision refusal, file lock, stage-only URL, balance check before orders; **since 2026-10-05 also `shared/pkg/risk.Check` before every stage order (R1)** | **Yes**, the only path that trades today |
| Global halt / StopAll | none | **Missing** |

### 6.2 New: `shared/pkg/risk` (stdlib only, Go 1.21)

`Policy` = global `halted` + `halt_reason`, per-book `BookLimits`, and a `default` for other books.
`LoadPolicy(path)` rejects unknown fields and negative limits; `ui-api -print-default-policy` dumps a
starting file.

| Limit | Type | btc_mxn | btc_usd | Why this default |
|---|---|---:|---:|---|
| `max_order_btc` | block | 0.01 | 0.01 | = the executor's hard `maxSize` |
| `max_position_btc` | block | 0.01 | 0.01 | one entry at the max size |
| `max_order_notional` | block | 25,000 MXN | 1,500 USD | ≈ 1.6× a max-size order at today's price |
| `max_orders_per_day` | block | 1 | 1 | one leg per book per Mexico City day, as the executor does |
| `max_price_deviation_bps` | block | 1,500 | 1,500 | fat-finger / bad-data guard vs the decision close |
| `drawdown_warn` | warn | 25% | 25% | review level only (principle 6) |
| `cost_warn_bps` | warn | 140 | 80 | 2× the pre-registered leg cost |

`Check(policy, order, state) → {allowed, findings[]}` is pure (no I/O, no clock). **Sells that reduce
the position skip the size, position, notional and per-day limits**, so tightening a limit can never
trap the executor in a position; the halt and the price guard still apply to them. `AssessDrawdown`
and `AssessCost` return warnings only.

### 6.3 New: risk in `ui-api` and the UI
`ui-api` rebuilds, for each book, the order the next stage run would send (it mirrors the executor's
`planAction`), runs `risk.Check` on it, and adds operational warnings: `run_missed` (a closed day with
no ledger record), `data_gap` (candle gaps in the last 60 bars), `order_blocked` (a day the executor
blocked) and `policy_mismatch` (the executor's last check ran under another policy version). It also
returns the executor's last recorded check. The Risk page shows all of it.

### 6.4 Next steps

| Step | Scope | Done when |
|---|---|---|
| **R0 (done)** | `shared/pkg/risk`, `shared/pkg/dailyledger`, `/api/ui/risk`, Risk page | Risk page shows limits, exposure, realized cost and the next-order check from the real ledger |
| **R1 (done 2026-10-05)** Enforce in the daily-executor | See §6.4.1 | A blocked order leaves a ledger line with `risk.allowed=false` and no exchange order (tested with a fake exchange); `/risk` reports `enforcement: "enforced"` |
| **R2** Halt file (next, §8.1) | `risk-state.json` next to the ledger (`{halted, reason, by, at}`), read by the executor alongside `DAILY_EXECUTOR_DISABLED`; `ui-api` shows it read-only | Setting the file halts the next run and the UI shows who set it, when and why |
| **R3** Alerts | Alert on a missed or failed run (exit code ≠ 0), a block, or a warning. Use the existing Grafana/Alertmanager stack, or a sidecar that polls `/api/ui/risk` | A missed day pages within an hour of 06:00 Mexico City |
| **R4** Halt from the UI | `POST /api/ui/risk/halt` and `/resume` with a required reason, an audit log (append-only JSONL), a confirmation dialog | Needs Phase 4 auth (OIDC) first; every action is audited |
| **R5** Platform-wide | Load trading-engine `MaxDailyLoss`/`MaxDrawdownPct` from env (non-zero defaults); stop trading-engine from placing orders OM rejected; expose OM `GetCurrentExposure`; move OM and trading-engine checks onto `shared/pkg/risk`; delete the dead strategy-executor risk package | One policy format across services; the session check actually blocks |

#### 6.4.1 R1 as built

- **Where.** `finishBook` in `cmd/daily-executor/main.go` calls `checkRisk` (`risk.go`) after
  `planAction` and before `dailyexec.Run`, only with `-stage` and only when an order is planned.
- **Policy.** `-risk-policy <file>`; empty means the built-in `risk.DefaultPolicy()` (limits kept as
  in §6.2, decided 2026-10-05). An invalid file exits 2 before anything runs. The run header prints
  the policy version.
- **Inputs.** Price = the stage touch the order would trade against (best ask for a buy, best bid
  for a sell); reference = the decision bar's production close; position = the executor's own
  tracked stage position; orders today = stage legs already recorded for that fill date.
- **Ledger.** Additive `stage.risk = {policy_version, order, state, allowed, findings}` whenever an
  order was planned. Old lines parse unchanged. Mirrored in `shared/pkg/dailyledger.RiskCheck` and
  `web/src/api/schemas.ts`; the contract test covers it.
- **On a block.** No order is sent. The day is recorded with `stage.action = "blocked"`, the position
  unchanged and the findings, and the run exits 1. A same-day re-run finds the day recorded and does
  nothing. **The next day's run plans from the recorded position as usual**, so a blocked entry is
  attempted again while the signal still asks for it. A blocked exit is retried the same way. Paper
  accounting is never affected.
- **Safety exceptions (nothing recorded, exit 1, re-run can retry):** no ticker or no usable
  bid/ask; or a block when an earlier crashed run already left an open order or fills under this
  leg's client ids, because recording "blocked" would hide those fills from the position tracking.
  That last case needs a person to resolve it.
- **Unchanged:** `DAILY_EXECUTOR_DISABLED=1` still exits before anything runs; the policy's
  `halted` flag is an additional block that is recorded.
- Also fixed: `lastStagePosition` now takes the ledger lock (books finish in parallel goroutines).
- Dated note for the forward tests:
  [`EXECUTION-RISK-GUARDS-2026-10-05.md`](../backtest-readiness/EXECUTION-RISK-GUARDS-2026-10-05.md).

### 6.5 First findings from the real stage ledger
- **btc_mxn's first stage leg cost 118 bps against 70 assumed.** The post-only order rested 60 min, filled 0.1%, and fell back to market: taker fee 78 bps + 40 bps above the fill-day open. A stage leg is small and stage liquidity is thin, so this is not yet evidence about production costs. But it is the cost signal to watch: the pre-registration's secondary (taker) scenario is 88 bps per leg, and this leg exceeded both.
- **btc_usd's leg cost 41 bps against 40 assumed** (maker fill, 25 bps fee + 16 bps slippage).
- **At 17:35 Mexico City on 2026-10-03, the 2026-10-02 bar was not yet recorded** for either book. The UI flagged it as `run missed`.

---

## 7. Backend fixes to do before or alongside the UI

1. **Auth that actually validates.** OIDC (e.g. Amazon Cognito or Google) with signed JWT validation in the BFF; drop the accept-anything gateway checks. TLS on the ALB (ACM certificate, HTTP→HTTPS redirect).
2. **CORS** restricted to the UI origin; remove the hard-coded `*` in `market-data` and `backtesting`. (`ui-api` sets no CORS headers at all; the UI is same-origin.)
3. **Do not expose signal-publishing endpoints** (`/strategies/process`, `/test/signals`, order cancel) through any public route.
4. **Fix or remove the gateway's dead routes** (orders and positions on order-management, strategy config PUT) and the port defaults.
5. **One backtest engine.** Either persist `strategy-executor` jobs and add an equity curve, or route everything to `backtesting`. Two result shapes double the UI work.
6. **Machine-readable research output.** Add `-json` to `weekly-research` / `daily-research` so the UI renders tables without parsing text.
7. **Risk:** the R1–R5 items in §6.4.

---

## 8. Phased plan

| Phase | Scope | Done when | Status |
|---|---|---|---|
| **0. Foundations** | `web/` scaffold (Vite, TS strict, router, Query, tokens, dark theme, layout), `npm run ci`, MSW mocks from real data | `npm run build` passes; Forward tests page renders from mocks | **Done** |
| **1. Forward tests + risk, local** | Forward-tests pages and Risk page against a local `ui-api` reading the local ledger; localhost only | Today's signal, paper vs hold, fills and fees for both books, matching the CLI output | **Done** |
| **1b. Close-out** | GitHub Actions job (`go test` for shared, ui-api, daily-executor; `npm run ci`), Playwright smoke test, multi-ledger support in `ui-api` (stage + dry-run + future volume variant), risk step **R1** | CI runs on every PR; a blocked order is enforced and visible | **R1 done** (2026-10-05); **CI done** (`operator-ui.yml`, 2026-10-06); Playwright and multi-ledger in progress (§8.1) |
| **2. Research + data health** | Study index, strategy comparison (needs `-json`), data-health page, ledger read from S3, risk **R2** + **R3** | Holdout vs development tables match the evidence files; a missed run alerts | |
| **3. Market data** | Market page via BFF proxy and SSE; candles with volume ratio | Live ticker updates within 2 s; no direct browser calls to internal services | A slim **live-data slice** is pulled forward (§8.1, step 3) |
| **4. Hardening + controls** | OIDC, TLS, CORS, audit log, role-gated controls (halt, kill switch, start/stop), risk **R4** | Security review passes; every control action is audited | |
| **5. Deploy** | Static build behind CloudFront or served by `ui-api`; k8s/compose entries; risk **R5** | Reachable only through auth over HTTPS | |

### 8.1 Next phase plan (decided 2026-10-06)

Order: close out Phase 1b, then R2, then a slim live-data slice, then research views. Data health
and R3 alerts follow.

| Step | Scope | Done when |
|---|---|---|
| **1a. Multi-ledger** | `ui-api -ledgers stage=<path>,dry-run=<path>[,…]` (`-ledger` stays as the `stage` default). Each ledger has its own store, candles dir and halt file. `GET /api/ui/ledgers` lists them; every other endpoint takes `?ledger=<name>` (default: the first). The UI has a ledger picker (URL search param `ledger`) and every card shows its ledger. | Switching the picker shows the dry-run ledger; an unknown name is a 400; fixtures and schema tests cover `/ledgers` |
| **1b. Playwright smoke** | `@playwright/test` against `vite preview` in mock mode (captured fixtures). The 3 pages, a desktop and a phone viewport. Fails on any console error or on a schema mismatch banner. 3rd job in `operator-ui.yml`. | `npm run e2e` passes locally and in CI |
| **2. R2 halt file** | See §8.2. | Setting the file halts the next run (recorded as a block) and `/risk` shows who, when, why |
| **3. Live-data slice** | See §8.3. | Cards and the detail chart update about once a second; a stale feed is visible; nothing live reaches the executor |
| **4. Research views** | Study index from `docs/backtest-readiness/*.md`, then `-json` on the research tools and the comparison view | As in Phase 2 |

### 8.2 R2 halt file (design)

- **File.** `risk-state.json` in the ledger's directory:
  `{"halted": true, "reason": "…", "by": "diego", "at": "2026-10-06T01:00:00Z"}`. Parsed by
  `shared/pkg/risk.LoadHaltState` (unknown fields rejected; `halted=true` requires `reason`, `by` and
  `at`). A missing file means not halted.
- **Executor.** Read at start of every run, after `DAILY_EXECUTOR_DISABLED`. An unreadable or invalid
  file exits 2 (fail closed: nothing runs). A halt is merged into the policy's halt
  (`risk.ApplyHalt`), so a planned stage order is **blocked and recorded** like any other block
  (§6.4.1); days with no order are recorded as usual, so the forward test keeps its paper record.
  Recorded `stage.risk` gets an additive `halt` object (`{reason, by, at}`).
- **ui-api.** `/api/ui/risk` adds `halt_source` (`none`, `policy`, `file`, `both`) and `halt_file`
  (`{path, found, halted, reason, by, at, error}`). Read-only; the file is edited by hand until R4.
- **UI.** The halt banner names the source and shows who, when and why. An invalid file is shown as an
  error (the executor would refuse to run).

### 8.3 Live-data slice (design)

Standard practice for trading UIs: one server-side market-data connection fans out to browsers;
updates are throttled for display; staleness is always visible; and anything computed from an
unfinished bar is labelled provisional. The decision path never reads live data.

- **Upstream.** `ui-api/internal/live` holds **one** connection to Bitso's production public
  WebSocket `wss://ws.bitso.com` (no keys), subscribed to `trades` and `orders` for the forward-test
  books. Keep-alives (`{"type":"ka"}`) and a read deadline detect a dead socket; reconnect with
  exponential backoff and jitter (1 s → 30 s cap). Off by default in tests; `-live=false` disables it.
- **Forming daily candle.** Seeded from REST `GET /api/v3/ohlc?book=&time_bucket=86400` for the
  current Mexico City day (buckets start 00:00 Mexico City = 06:00 UTC), then updated from trades
  (`r` rate, `a` amount, `x` ms). On reconnect it is re-seeded from REST, so trades missed during
  the gap are not lost. A new day starts a new candle.
- **Provisional SMA50 flip level.** With the last 49 *closed* closes `S49` (from the executor's
  candle CSV), the SMA50 including a provisional close `c` is `(S49 + c) / 50`, and
  `c > (S49 + c)/50  ⇔  c > S49 / 49`. So the **flip level is the mean of the last 49 closed closes**,
  shown as "provisional: if today closed now". It is withheld (null) when the CSV's last bar is not
  yesterday's Mexico City day, because the level would then be wrong.
- **Browser API.** `GET /api/ui/stream?books=btc_mxn,btc_usd` (Server-Sent Events, GET-only, same
  origin). Events: `snapshot` (on connect), `ticker` (last trade, best bid/ask), `candle` (forming
  bar + flip level), `status` (upstream connected/reconnecting, last message age) and `heartbeat`
  every 15 s. At most 1 `ticker` and 1 `candle` per book per second (latest value wins). `GET
  /api/ui/live/{book}` returns the same snapshot as JSON (REST fallback). The SSE handler clears the
  server's 30 s write deadline per stream (`http.ResponseController`).
- **UI.** `useLiveStream` (EventSource; the browser reconnects; the hook tracks the last event time).
  Forward-test cards: live price, live distance to the flip level, live stage-position value. Detail
  chart: today's candle updates in place (`series.update`) and a dashed flip line, both labelled
  provisional. A badge shows "live · N s ago"; after 10 s with no event the badge turns amber and
  the live overlay greys out.
- **Tests.** Go: a fake WebSocket server (subscribe ack, trades, keep-alives, a dropped connection,
  a reconnect), the candle aggregator across a day boundary, the throttle, SSE framing. Web: a mocked
  EventSource (live values render; staleness after 10 s with fake timers).
- **Not in this slice.** Order-book depth, recent-trades tape, intraday candles; Market page (Phase 3).

---

## 9. How to run (local)

```bash
export PATH=$PWD/.tools/node/bin:$PATH              # portable Node 22
(cd services/ui-api && go run ./cmd) &              # 127.0.0.1:8090, stage ledger
cd web && npm ci && npm run dev                     # http://127.0.0.1:5173
# or, without the backend:
npm run dev:mock
# tests
(cd shared && go test ./pkg/risk ./pkg/dailyledger)
(cd services/ui-api && go test ./...)
(cd services/strategy-executor && go test ./cmd/daily-executor)
(cd web && npm run ci)
```

---

## 10. Design notes

- **Dark theme**, high-contrast numbers, tabular mono figures for prices. Palette: long = teal, flat = slate, loss/block = coral, warn = amber, benchmark = neutral grey dashed line.
- Every performance number shows **what it is compared with** (buy-and-hold) and **after which costs**.
- Times in **America/Mexico_City** with UTC on hover, because the daily bars are Mexico City days.
- Empty and error states explain the cause ("no run today yet: the executor runs after 00:05 Mexico City"; schema drift names the failing field).
- Responsive down to a phone (sidebar becomes a top bar, cards stack); motion respects `prefers-reduced-motion`.

## 11. Open questions

1. ~~Who uses it?~~ One operator for now. Phase 1 is localhost-only with no auth (decided 2026-10-03).
2. ~~Hosting?~~ Local first (decided). AWS when Phase 4 auth exists.
3. Should controls ever reach production trading, or stay limited to stage?
4. Is Grafana enough for service metrics, so the UI covers only trading and research views? (Recommended: yes, and link out to Grafana.)
5. ~~**R1 policy on a blocked stage order?**~~ **Skip and record, no retry of that order** (decided 2026-10-05). As built (§6.4.1), the next day's run re-plans from the recorded position, so the stage position diverges from paper only until the next run that is not blocked, rather than until the next signal flip. This keeps a blocked *exit* from leaving a long position open through a whole flat period.
6. ~~Are the default limits in §6.2 right?~~ **Keep the defaults** (decided 2026-10-05), version `default-2026-10-03`. Revisit `cost_warn_bps` once more stage legs exist (btc_mxn's first leg already exceeded it).
