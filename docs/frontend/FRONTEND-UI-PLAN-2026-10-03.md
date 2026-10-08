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

What is **not** done yet: there is no halt switch the UI can flip (R4), no alerting (R3), and nothing
is deployed beyond localhost. (The data-health page was built on 2026-10-07, §8.7.)

**Update 2026-10-07.** Research step 4 is complete: both research tools write `research-run/v1`
JSON (`-json`), `ui-api` serves the reports, and the web app has `/research/runs` (list) and
`/research/runs/<date>/<name>` (heat-map matrix, window detail, cost sensitivity and, for 2–3 window
runs, the **development vs holdout comparison**), §8.5.

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
`/forward-tests` (was `/` until the landing page, §8.6): one card per book (`btc_mxn`, `btc_usd`), from the latest ledger record:
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

### 4.3 Research and backtests (Phase 2; study index built 2026-10-06, runs and comparison 2026-10-07)
- **Built:** `/research` lists the studies (`docs/backtest-readiness/*.md`), newest first, with kind, question, summary, lineage chips and evidence count; search and kind filter live in the URL. `/research/<name>` renders the study with a table of contents, what it builds on, what links to it, and its evidence files (§8.4).
- **Built:** strategy comparison view: return, vs hold, max DD, Sharpe, trades and cost, development vs holdout side by side, with beat-hold / lower-DD verdicts per rule and a sort on the holdout (from `weekly-research` / `daily-research` `-json` outputs), §8.5.
- Backtest runs from the `backtesting` service: equity curve, trades, parameters. Pick **one** canonical engine first (see §7).
- The volume-confirmed SMA50 variant, once pre-registered, gets its own forward-test card from its separate dry-run ledger (`ui-api -ledger …`; multi-ledger support needed, §8).

### 4.4 Market (Phase 3)
- Ticker and spread per book, recent trades, order-book depth (from `market-data` REST, live via SSE).
- Daily candles with volume and the 20-day volume ratio. The ratio is already computed by `ui-api` (`volume_ratio_20d`).

### 4.5 Data health (built 2026-10-07, §8.7)
- Collector: last flush age per book and the 24 h flush cadence (from S3). Its WebSocket gap table and Postgres row counts are inside the VPC, so they stay an SSM audit for now.
- S3: latest raw partition per book, compaction status (`trades_compacted/` up to which day, from the latest `_manifest.json`).
- Daily-executor: ledger coverage per book (run status), the last run's exit code and whether it uploaded the ledger to S3 (from `run-*.log`), recent runs.

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
| `GET /api/ui/risk` | ledger + candles + risk policy | policy, exposure, utilization, next-order check, realized cost, findings, halt file |
| `GET /api/ui/ledgers` | configured ledgers (`-ledgers`) | name, path, found, records, default; every endpoint above takes `?ledger=<name>` |
| `GET /api/ui/live?books=` | live hub (Bitso public WS) | snapshot JSON: upstream status, last trade, bid/ask, forming candle, provisional flip level. Display only; 503 with `-live=false` |
| `GET /api/ui/stream?books=` | live hub | Server-Sent Events: `snapshot`, `book` (≤1/s per book), `status`, `heartbeat` (15 s) |
| `GET /api/ui/research/studies` | `-studies-dir` (default `docs/backtest-readiness`) | metadata per study: kind, date, question, summary, follows, references, evidence dir; empty list if the dir is missing |
| `GET /api/ui/research/studies/{name}` | one study file | sanitized HTML (goldmark, raw HTML dropped), h2/h3 headings, followed-by / referenced-by; 400 bad name, 404 unknown |
| `GET /api/ui/research/runs` | `research-run/v1` JSON in `evidence-<date>/` | summary per report: data, costs (incl. additive `level`/`note`), window headers, per-rule beat-hold scores, citing studies; unreadable reports listed as `skipped` |
| `GET /api/ui/research/runs/{date}/{name}` | one report | the summary plus the report passed through unchanged, so additive fields reach the UI |
| `GET /api/ui/health/data` | `-archive s3://bucket` (list/get only) + ledger dir | collector flushes, compaction, executor coverage, last `run-*.log`, S3 upload; ok/warn/fail checks; S3 listing cached 60 s; `?ledger=` picks the executor section |

Files are cached by size and mtime (by ETag for a ledger in S3, §8.8); the ledger is re-read only
when it changes.

Still to build (Phase 2–3): `/market/{book}/…` proxy.

### 5.2 Contracts
- **Go ↔ Go:** `shared/pkg/dailyledger` mirrors the executor's unexported ledger types.
  `services/strategy-executor/cmd/daily-executor/ledger_contract_test.go` marshals a fully populated
  executor record and requires a lossless round trip through the mirror (`DisallowUnknownFields`).
- **Go ↔ TS:** `web/src/test/app.test.tsx` parses every captured `ui-api` response with the zod
  schemas. `npm run fixtures` re-captures them from a running `ui-api`.
- Correction to the earlier draft of this plan: `candles.recent_gaps` is a **string**, not `string[]`.
  `web/src/api/schemas.ts` is now the canonical TypeScript contract.

### 5.3 Prerequisites still open
- ~~**Ledger location.**~~ **Done 2026-10-07 (§8.8):** `scripts/daily-executor-run.sh` uploads the
  ledger to `s3://…/daily-executor/<ledger>/` after each run when `DAILY_EXECUTOR_S3_URI` is set, and
  `ui-api -ledgers name=s3://…` reads that copy. **Still open:** a scheduled run that sets it (none
  of the runs so far did, so the prefix does not exist yet).

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
| **R2 (done 2026-10-06)** Halt file, §8.2 | `risk-state.json` next to the ledger (`{halted, reason, by, at}`), read by the executor alongside `DAILY_EXECUTOR_DISABLED`; `ui-api` shows it read-only | Setting the file halts the next run and the UI shows who set it, when and why |
| **R3 (done 2026-10-07)** Alerts, §8.9 | Alert on a missed or failed run (exit code ≠ 0), a block, or a warning. `ui-alerts` (cron, every 15 min) evaluates ui-api's own views and publishes to SNS (email) | A missed day pages within an hour of 06:00 Mexico City |
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
6. ~~**Machine-readable research output.**~~ **Done 2026-10-07:** `-json` on `daily-research` and `weekly-research` (schema `research-run/v1`), §8.5.
7. **Risk:** the R1–R5 items in §6.4.

---

## 8. Phased plan

| Phase | Scope | Done when | Status |
|---|---|---|---|
| **0. Foundations** | `web/` scaffold (Vite, TS strict, router, Query, tokens, dark theme, layout), `npm run ci`, MSW mocks from real data | `npm run build` passes; Forward tests page renders from mocks | **Done** |
| **1. Forward tests + risk, local** | Forward-tests pages and Risk page against a local `ui-api` reading the local ledger; localhost only | Today's signal, paper vs hold, fills and fees for both books, matching the CLI output | **Done** |
| **1b. Close-out** | GitHub Actions job (`go test` for shared, ui-api, daily-executor; `npm run ci`), Playwright smoke test, multi-ledger support in `ui-api` (stage + dry-run + future volume variant), risk step **R1** | CI runs on every PR; a blocked order is enforced and visible | **Done**: R1 (2026-10-05); CI (`operator-ui.yml`), multi-ledger, Playwright (2026-10-06) |
| **2. Research + data health** | Study index, strategy comparison (needs `-json`), data-health page, ledger read from S3, risk **R2** + **R3** | Holdout vs development tables match the evidence files; a missed run alerts | **R2 done**, **study index done** (2026-10-06), **runs + comparison done**, **data health done**, **S3 ledger done**, **R3 done** (2026-10-07) |
| **3. Market data** | Market page via BFF proxy and SSE; candles with volume ratio | Live ticker updates within 2 s; no direct browser calls to internal services | **Live-data slice done** (2026-10-06, §8.3); Market page still to build |
| **4. Hardening + controls** | OIDC, TLS, CORS, audit log, role-gated controls (halt, kill switch, start/stop), risk **R4** | Security review passes; every control action is audited | |
| **5. Deploy** | Static build behind CloudFront or served by `ui-api`; k8s/compose entries; risk **R5** | Reachable only through auth over HTTPS | |

### 8.1 Next phase plan (decided 2026-10-06)

Order: close out Phase 1b, then R2, then a slim live-data slice, then research views. Data health
and R3 alerts follow.

| Step | Scope | Done when |
|---|---|---|
| **1a. Multi-ledger (done)** | `ui-api -ledgers stage=<path>,dry-run=<path>[,…]` (`-ledger` stays as the `stage` default). Each ledger has its own store, candles dir and halt file. `GET /api/ui/ledgers` lists them; every other endpoint takes `?ledger=<name>` (default: the first). The UI has a ledger picker (URL search param `ledger`) and every card shows its ledger. | Switching the picker shows the dry-run ledger; an unknown name is a 400; fixtures and schema tests cover `/ledgers` |
| **1b. Playwright smoke (done)** | `@playwright/test` against `vite preview` in mock mode (captured fixtures). The 3 pages, a desktop and a phone viewport. Fails on any console error or on a schema mismatch banner. 3rd job in `operator-ui.yml`. | `npm run e2e` passes locally and in CI |
| **2. R2 halt file (done)** | See §8.2. | Setting the file halts the next run (recorded as a block) and `/risk` shows who, when, why |
| **3. Live-data slice (done)** | See §8.3. | Cards and the detail chart update about once a second; a stale feed is visible; nothing live reaches the executor |
| **4. Research views (done)** | **4a (done 2026-10-06):** study index from `docs/backtest-readiness/*.md` (§8.4). **4b (done 2026-10-07):** `-json` on the research tools, `/research/runs`, the comparison view (§8.5) | As in Phase 2 |
| **5. Data health, S3 ledger, R3 (done)** | **5a (done 2026-10-07):** data-health page (§4.5) with `GET /api/ui/health/data`, §8.7. **5b (done 2026-10-07):** ledger read from S3 (§5.3, §8.8). **5c (done 2026-10-07):** **R3** alerts and the daily schedule (§8.9) | A stale collector or a missed run is visible in the UI and pages |

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

**As built (2026-10-06), where it differs from the design above:**

- One `book` event per book (last trade, bid/ask, forming candle and provisional values together,
  at most 1/s) instead of separate `ticker` and `candle` events. The JSON fallback is
  `GET /api/ui/live?books=` (same shape as the `snapshot` event).
- The stale threshold is **25 s** without any event, not 10 s: the heartbeat is every 15 s and a quiet
  market can go longer than 10 s without a trade, so 10 s would raise false alarms. The badge shows
  `Live`, `Stale · N s`, `Reconnecting` (browser or Bitso side), `Live off` (ui-api refused the stream,
  e.g. `-live=false`; retried every 15 s) or `Live error` (an event failed the zod contract).
- Cards also show **"would flip"** when the provisional signal differs from the recorded one.
- The forming candle is drawn translucent, so it never reads as a closed bar.
- Mock mode (`npm run dev:mock`, Playwright) replays the captured `fixtures/live.json` as a real
  `text/event-stream` through MSW, timestamps moved to now.
- Code: `services/ui-api/internal/live`, `internal/api/stream.go`; `web/src/api/live.ts`,
  `web/src/components/live.tsx`, `PriceChart` in `web/src/components/charts.tsx`.

### 8.4 Research study index (as built, 2026-10-06)

- **Source of truth stays the markdown.** `ui-api/internal/research` reads `-studies-dir`
  (env `UI_API_STUDIES_DIR`); `README.md` is skipped. Parsed per file and cached by size and mtime.
- **Metadata.** Title from the `# h1`; date from the `-YYYY-MM-DD` file suffix, overridden by a
  `Date:` / `Date registered:` header line; kind from the name (`PREREGISTRATION` →
  pre-registration, `ASSESSMENT`, `STUDY`/`CHECK` → study, else report); question from the first
  plain blockquote or a `**Question.**` paragraph; summary from the first prose paragraph (after a
  short lead-in such as `**Short answer.**`, the next one); `follows` from links in the `Follows:` /
  `Context:` header lines, `references` from other links to studies; evidence from the
  `evidence-<date>/` dir with the study's date.
- **Safe rendering.** goldmark (GFM) with raw HTML dropped and only http(s)/mailto/relative links
  kept; external links open in a new tab; links to studies become in-app routes; other repo links are
  shown but inert (the UI does not serve repo files). The leading h1 is removed (the page has its
  own). GitHub alerts (`> [!IMPORTANT]`) are styled.
- **Names** are validated (no `/`, no `..`); unknown names are a 404.
- **Fixtures** are captured from the **committed** docs only (`scripts/capture-fixtures.sh`), so
  draft studies never land in `web/src/mocks/research/`.
- **Tests.** Go: metadata, sanitization (script, `javascript:` links), traversal, cache refresh,
  missing dir, handlers. Web: zod contract per fixture, filters in the URL, in-app vs inert links,
  unknown study; Playwright: list → filter → study → follow a lineage link.
- **Next (4b, done 2026-10-07):** see §8.5.

### 8.5 Research runs and strategy comparison (as built, 2026-10-07)

- **One schema, two tools.** `daily-research -json` and `weekly-research -json` both write
  `research-run/v1` (`cmd/*/report.go`; changes are additive only). `weekly-research` writes one file per
  cost level: `<name>.json` (base) and `<name>-stress-<bps>bps.json`. Its additive fields: per rule
  `label`, `trades`, `turnover_x`, `return_zero_cost_pct`, `cagr_pct`, `sharpe`, weekly up/down/flat,
  median and worst week; per window (base report only) `events` (volume-spike event study) and
  `sensitivity` (the post-hoc volume threshold check, flagged `post_hoc`); `costs.level` and
  `costs.note` (one per-leg cost that already includes slippage). `round_trips` counts entries from
  flat; `trades` counts every rebalance.
- **Evidence.** The JSON twins sit next to the text in `evidence-2026-09-27/`, `-09-28/` and
  `-10-03/`. The 2026-10-03 JSON was regenerated with the committed tool and its text output was
  **byte-identical** to the committed `.txt`, so the JSON and the study's numbers agree.
- **ui-api.** `internal/research/runs.go` scans `evidence-<date>/*.json` for the schema prefix,
  builds summaries (per-rule beat-hold counts) and passes each report through unchanged.
- **UI.** `/research/runs` groups reports by evidence folder (cost filter and search in the URL;
  cards show the cost level, the tool and at most six rules). `/research/runs/<date>/<name>`:
  - **Comparison** (2–3 windows): every rule × window with return, vs hold, max DD (teal when below
    holding's), Sharpe and trades when present, costs; then *beat hold* and *lower DD* counts. Sort
    (`?sort=vs_hold|sharpe|max_dd`) ranks on the **holdout** (else the last window), never on
    development.
  - A holdout design opens on the holdout window (`?w=` overrides) and tags the columns
    *development* / *holdout*; daily-research keeps *in-sample*.
  - Matrix, window detail (CAGR, Sharpe, trades, no-cost return when present; the random-baseline
    column only when the report has one), event study, sensitivity, and cost siblings (base ↔ stress
    links) for the selected window.
- **Tests.** Go: report mapping, v1 field contract, stress file naming, round-trip counting; ui-api
  level/note pass-through. Web: zod contract per captured fixture, helpers, comparison and sort in the
  URL, holdout default, base/stress links; Playwright: list → weekly run → sort → detail.

### 8.6 Landing page (as built, 2026-10-07)

- **Route.** `/` is a full-screen landing page outside the console shell; the console starts at
  `/forward-tests` (a pathless layout route holds the sidebar). The sidebar brand links back to `/`.
  Every link keeps `?ledger=`.
- **Sections**, all from existing ui-api endpoints (no new backend):
  - **Hero:** what the rule is, the console call to action, and a card with the live Bitso price
    (SSE, display only), the recorded signal, how long the rule has held it, and a one-year chart:
    daily closes, SMA50 (dashed amber) and the days the rule was long (teal bands). Plain SVG
    (`components/TrendChart.tsx`, geometry in `lib/trend.ts`); hover, touch or arrow keys read a day.
    Below it: price change, share of days long, paper vs buy-and-hold since the forward start.
  - **Today:** one tile per book (distance to SMA50, close/SMA, paper vs hold equity, max drawdown,
    next open's action), linking to the book's detail page.
  - **How it works:** four steps (freeze, decide daily, trade small behind the check, judge against
    holding) and the forward-window timeline per book with the interim tick; links the pre-registration.
  - **Research:** study, pre-registration and run counts and the three latest write-ups.
  - **Guardrails:** enforcement, halt state, stage size, position cap, price guard and realized cost
    per leg vs the pre-registered assumption (amber when above).
- **Offline.** If ui-api is down the copy still renders, with a note in place of the numbers.
- **Tests.** Unit: geometry (bands, SMA gaps, summary), current run, landing render, book switch,
  keyboard read-out, ledger-aware links, offline state, brand link. Playwright (desktop and phone):
  hero with live price, chart hover, book switch, sections, CTA into the console and back.

### 8.7 Data health (as built, 2026-10-07)

- **Backend.** `ui-api -archive s3://<bucket>` (env `UI_API_ARCHIVE`; off by default) lists the
  collector's archive with the default AWS credentials, list/get only (`internal/objstore`, with an
  in-memory store for tests). `internal/datahealth` checks, per book:
  - **Raw flushes** (`trades/book=…/year=…/month=…/day=…/`, today and yesterday, walking back up to
    a week if empty): age of the newest object, flushes in the last 24 h, the longest wait and waits
    over 2 h. The collector flushes about hourly, so **warn after 90 min, fail after 3 h**.
  - **Compaction** (`trades_compacted/book=…/…/_manifest.json`): the latest compacted day against
    yesterday (UTC), rows in vs out, duplicates, when it ran. **Warn at 2 days behind, fail at 4.**
  - **Executor**, per ledger: coverage per book (the forward-test run status: a missed closed day
    fails, before 06:00 Mexico City a missing run is fine), the newest `run-<UTC>.log` next to the
    ledger (exit code; no exit line = running for 90 min, then a warning), and the S3 upload line.
  - The S3 listing is cached for 60 s (15 s after an error); an unreachable bucket shows as unknown.
- **`scripts/daily-executor-run.sh`** runs the executor once (flags pass through) and writes
  `run-<UTC>.log` ending in `exit=<code>`. With `DAILY_EXECUTOR_S3_URI` it then uploads the ledger,
  the halt file (or deletes a stale remote copy), the candle CSVs and the log, and records
  `upload=ok|failed <dest>`. The upload never changes the exit code.
- **UI.** `/data-health` (nav: Operations, badge = failing or warning checks): overall status with a
  tile per area, the checks worst first ("not configured" does not count against the overall
  status), one card per book with the last flush, a 24 h flush timeline (gaps hatched amber) and the
  compaction lag, and the executor card with coverage, missing days, the last run's error lines and
  the recent runs. Ages are measured against the response's `generated_at`, so fixtures read as live.
- **First findings (2026-10-07 21:20 UTC).**
  - The collector is healthy: 25 hourly flushes per book in 24 h, longest wait 63 min.
  - **Compaction stopped**: both books are compacted through 2026-09-30, from a single run on
    2026-10-01 17:48 UTC, so it is **6 days behind**. It looks like a one-off backfill, not a
    scheduled job. *Fixed 2026-10-08 (§8.9): caught up through 2026-10-06 and scheduled daily.*
  - **The stage ledger is missing 2026-10-05 and 2026-10-06** for both books: the last run was
    2026-10-05 16:53 Mexico City. Runs are manual today; R3 alerts and a schedule are the fix.
    *Decided 2026-10-08: left as a gap, not backfilled.* A late run would have recorded the
    2026-10-06 decision at a later price than the pre-registered run time, so the two days stay
    missing: a known gap in the forward-test record. The scheduled run (§8.9) records
    2026-10-07 onward; ledger coverage then measures from that bar, so its alert resolves.
  - Run logs before 2026-10-05 22:53 UTC have no `exit=` line (they predate the wrapper).
- **Tests.** Go: archive thresholds (healthy, stale, compaction lag, gap, missing book, outage),
  run-log parsing, the handler (archive off, missed days and a failed upload, cache, unreachable
  bucket). Web: contract per captured fixture, helpers (ordering, minutes, flush timeline), the
  page, archive off, healthy state, error state; Playwright (desktop and phone): nav → page →
  sections → dry-run ledger.

### 8.8 Ledger read from S3 (as built, 2026-10-07)

- **Layout** (what `scripts/daily-executor-run.sh` uploads with
  `DAILY_EXECUTOR_S3_URI=s3://<bucket>/daily-executor/<ledger>`): `ledger.jsonl`, `risk-state.json`
  (removed remotely when lifted locally), `candles/<book>_daily_<date>.csv`, `run-<UTC>.log`.
- **ui-api.** Any ledger in `-ledger` or `-ledgers` may be `s3://bucket/prefix` (a prefix ending in
  `.jsonl` names the ledger key). `internal/store` now reads through an `FS`: the local disk as
  before, or the S3 prefix through `internal/objstore` (list and get only, default AWS credentials,
  one client per bucket). Every endpoint works the same for both, including the halt file and the
  run logs on the Data health page; paths are shown as `s3://bucket/key`.
- **Caching.** One listing of the prefix serves every read for **30 s** (5 s after an error), and
  bodies are cached by ETag, so polling pages cost at most one LIST per 30 s; a new upload shows
  within 30 s without a restart. An unreachable bucket is an error on the ledger, not an empty ledger.
- **Halt file.** Parsed with the executor's own rules: `shared/pkg/risk.ParseHaltState` (split out
  of `LoadHaltState`, whose behaviour is unchanged; a test requires both to agree on every case).
  Note that the S3 copy is as of the last upload: a halt set locally shows in an S3-backed ui-api only
  after the next run uploads it.
- **Checked live** against a temporary copy of the stage ledger in the real bucket (since deleted):
  the same records, run status, candles and run log as the local ledger.
- **Tests.** Store: the S3 copy and the local disk give the same records, candles, run logs and halt
  file; one LIST per TTL; a new upload and a new or invalid halt file seen after the TTL; a deleted
  ledger is empty; an outage surfaces and is retried. API: an S3 ledger next to a local one through
  `/ledgers`, `/healthz`, `/forward-tests`, candles (incl. the 404 path), `/risk` (halt from S3) and
  `/health/data` (run log and upload from S3); one client per bucket; bad URIs.

### 8.9 R3 alerts and the daily schedule (as built, 2026-10-07)

- **`ui-alerts`** (`services/ui-api/cmd/ui-alerts`, logic in `internal/alerts`) builds the same
  `api.Server` as ui-api and calls `/api/ui/health/data` and `/api/ui/risk` **in process** for every
  ledger, so alerts do not depend on a running ui-api. Same flags and `UI_API_*` variables.
- **What alerts.** Every data-health check that fails (critical) or warns (warning): a missed closed
  day, a run that exited ≠ 0 (incl. a block, exit 1, or a refused run, exit 2), a failed ledger
  upload, a stale collector, a compaction lag. An unreachable archive is a warning; a run still in
  progress and "not configured" are not alerts. From `/risk`: block findings (critical), warn
  findings (`order_blocked`, `data_gap`, `policy_mismatch`, drawdown, cost), and an invalid halt file
  (critical: the executor refuses to run). `run_missed` is left to the coverage check (same days).
  The archive checks are de-duplicated across ledgers.
- **When it sends.** An alert is sent when it first appears or escalates (warning → critical), as a
  reminder every 12 h (`-repeat`) while open, and once when it resolves. A changed message alone
  waits for the reminder. Nothing is sent when nothing changed. Open alerts are kept in
  `~/.local/state/mtb-ui-alerts/state.json`; it is written only after a successful send, so a failed
  send retries 15 min later.
- **Channel.** SNS topic `mtb-operator-alerts` (us-east-1), email subscription. One message per
  run: an ASCII subject ("[mtb-ops] 2 critical, 1 resolved: Ledger coverage - btc_mxn (stage)") and a
  plain-text body by section (escalated, new, reminder, resolved) with links to the Data health and
  Risk pages. `-notify stdout` and `-dry-run` print instead; `-test` sends a test message. SNS drops
  messages to unconfirmed subscriptions, so a topic with no confirmed subscriber is a send error: the
  state is kept and the alerts go out on the first run after the link is clicked.
- **Schedule** (`scripts/install-ops-cron.sh`, a marked block in the user's crontab; the machine is
  on UTC):
  - `15 6 * * *` `scripts/ops-run.sh executor`: `scripts/daily-executor-run.sh -stage` at 00:15
    Mexico City, with `DAILY_EXECUTOR_S3_URI=s3://<bucket>/daily-executor/stage`, so every run leaves
    an exit code and an S3 copy (§8.8).
  - `*/15 * * * *` `scripts/ops-run.sh alerts`. A day still missing at 06:00 Mexico City is reported
    by 06:15; a failed run at 00:15 by about 00:30 (or when the maker timeout ends).
  - `30 2 * * *` `scripts/ops-run.sh compact` (added 2026-10-08): the archive compactor's
    **non-destructive** refresh, `bin/compact-archive -bucket <bucket>`. It writes
    `trades_compacted/` and a manifest per day after a read-back check, skips days whose manifest is
    current, and leaves `trades/` untouched; 02:30 UTC is after its 2 h settle on the UTC day.
    `-cutover` (deletes the small files) is **refused** by `ops-run.sh` and stays a manual, reviewed
    step (`docs/data-collector/S3-COMPACTION-2026-09-22.md`). The compactor is not on this branch:
    the installer builds it from `--compactor-ref` (default `origin/feat/intraday-data-collector`,
    `services/data-collector` + `shared` via `git archive`) when missing or with
    `--rebuild-compactor`, and records the commit in `bin/compact-archive.ref`. `--no-compact` skips it.
  - Settings in `~/.config/microservices-trading-bot/ops.env` (chmod 600: PATH with the AWS CLI,
    topic, bucket, ledger); logs in `~/.local/state/mtb-ops/{executor,alerts,compact}.log`.
- **First catch-up (2026-10-08 00:08 UTC).** 2026-10-01 … 10-06 compacted for both books (12
  partitions, rows match: btc_mxn 26 949 → 49 files in total, btc_usd 149 → 7); the two compaction
  alerts resolved on the next alerts run ("[mtb-ops] 2 resolved").
- **Known limit.** The executor and the alerts run on the same workstation: if it is off, nothing
  runs and nothing alerts. A watchdog outside it (e.g. a scheduled Lambda that checks the S3 ledger's
  age, now that it is uploaded daily) would close that gap. *Done: §8.10.*
- **Tests.** Mapping from health and risk (incl. what does not alert), de-duplication and ordering,
  the new → quiet → escalated → reminder → resolved lifecycle, `repeat 0`, a downgrade, rendering and
  the SNS-safe subject, SNS publish and errors (fake client); in-process evaluation against the test
  ledger and the state round trip.

### 8.10 Off-machine watchdog (as built, 2026-10-08)

- **What.** The Lambda `mtb-ledger-watchdog` (`infrastructure/lambda/ledger-watchdog/`, Python
  3.12). EventBridge runs it once per clock hour (`cron(5 * * * ? *)`). For every ledger prefix
  (default `stage=daily-executor/stage`) it finds the newest `run-*.log` in S3. The run wrapper
  uploads that file last, so it marks the last run. If it is more than **30 h** old, the Lambda
  publishes to `mtb-operator-alerts`. A missed 06:15 UTC run is therefore reported at about 12:05
  UTC the next day, even if the workstation is off.
- **When it sends.** It keeps no state. Each decision uses a 62-minute window around the 30 h
  crossing, then every 12 h while the ledger stays stale. It sends "resolved" once, on the first
  check after a new run log that ends a gap of 30 h or more. A prefix with no run logs is reported
  at 00:05 and 12:05 UTC. A 72 h simulation of hourly runs in the tests proves exactly one first
  alert, then reminders every 12 h.
- **Message.** An ASCII subject, e.g. `[mtb-ops] watchdog: 1 stale - stage: no run for 30 h`. The
  body has the last run's `exit=`/`upload=` lines and the age of `ledger.jsonl`.
- **Access.** `s3:ListBucket` limited to `daily-executor/*`, `s3:GetObject` on
  `daily-executor/*`, and `sns:Publish` on the one topic. Logs are kept 30 days.
- **Deploy.** `deploy.sh --bucket … --topic-arn … [--test]` does four things:
  1. runs the unit tests;
  2. packages `src/` to `s3://<bucket>/lambda-artifacts/ledger-watchdog/`;
  3. deploys the CloudFormation stack `mtb-ledger-watchdog` (function, role, hourly rule, log group);
  4. invokes it once with `{"dry_run": true}`.

  `deploy.sh --delete` removes the stack.
- **Overlap with ui-alerts.** None in practice. ui-alerts reports a missed day by 06:15 Mexico
  City while the workstation is up. The watchdog fires only after 30 h, which happens only when
  the workstation (or its cron) is down.

---

## 9. How to run (local)

```bash
export PATH=$PWD/.tools/node/bin:$PATH              # portable Node 22
(cd services/ui-api && go run ./cmd) &              # 127.0.0.1:8090, stage ledger, live on, studies from docs/backtest-readiness
#   add -archive s3://mtb-development-data-archive-<account> for the Data health archive section
# several ledgers, live data off, another studies dir:
#   go run ./cmd -ledgers stage=<path>/ledger.jsonl,dry-run=<path>/ledger.jsonl -live=false -studies-dir <dir>
# a ledger from its S3 copy (list/get only), next to a local one:
#   go run ./cmd -ledgers stage=s3://<bucket>/daily-executor/stage,local=<path>/ledger.jsonl
# one executor run with a run log (and an S3 copy when DAILY_EXECUTOR_S3_URI is set):
#   scripts/daily-executor-run.sh -stage
# R3 alerts: print what would be sent; install the cron jobs (daily run, compaction refresh, alerts every 15 min):
#   (cd services/ui-api && go run ./cmd/ui-alerts -dry-run)
#   scripts/install-ops-cron.sh --topic-arn <arn> --bucket <bucket>   # --print to preview, --uninstall
#   scripts/ops-run.sh compact -dry-run                              # what the compaction job would do
cd web && npm ci && npm run dev                     # http://127.0.0.1:5173
# or, without the backend:
npm run dev:mock
# tests
(cd shared && go test ./pkg/risk ./pkg/dailyledger)
(cd services/ui-api && go test ./...)
(cd services/strategy-executor && go test ./cmd/daily-executor)
(cd web && npm run ci && npm run e2e)
python3 -m unittest discover -s infrastructure/lambda/ledger-watchdog   # §8.10 watchdog
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
