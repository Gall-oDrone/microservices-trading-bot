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

**Update 2026-10-08.** **R4 is built** (§8.12): the Risk page can halt and resume the
daily-executor per ledger through `ui-api`, behind a confirmation dialog, a local operator token and
an append-only audit log. It is localhost-only and off unless `ui-api` gets `-operator-token-file`;
OIDC replaces the token in Phase 4 proper.

**Update 2026-10-08 (later).** **Start/stop and the kill switch are built** (§8.13): a new
`/strategies` page lists every daily-executor ledger with its halt state, has a **halt-all kill
switch**, and starts/stops the intraday strategy-executor's strategies through `ui-api`, with the
same token, confirmation and audit log. A stop is a **hold** that strategy-executor persists
(`STRATEGY_HOLD_FILE`), so it survives restarts and the router cannot undo it. OIDC, TLS and roles
are still deferred.

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
| Components | Own primitives (`components/ui.tsx`) | Radix still deferred: the R4 confirmation is a small own modal (focus trap, Escape, `aria-modal`), §8.12 |
| Live updates | Polling (TanStack Query); **SSE** for live prices (§8.3) and the Market page (§8.11) | One EventSource per page through `ui-api` |
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

### 4.4 Market (built 2026-10-08, §8.11)
- Ticker and spread per book, recent trades, order-book depth, live via SSE through `ui-api`. Source: Bitso's public WebSocket through the `ui-api` live hub, not `market-data` REST (§8.11).
- Daily candles with volume and the 20-day volume ratio. The ratio is already computed by `ui-api` (`volume_ratio_20d`).

### 4.5 Data health (built 2026-10-07, §8.7)
- Collector: last flush age per book and the 24 h flush cadence (from S3). Its WebSocket gap table and Postgres row counts are inside the VPC, so they stay an SSM audit for now.
- S3: latest raw partition per book, compaction status (`trades_compacted/` up to which day, from the latest `_manifest.json`).
- Daily-executor: ledger coverage per book (run status), the last run's exit code and whether it uploaded the ledger to S3 (from `run-*.log`), recent runs.

### 4.6 Controls (Phase 4, gated)
- Strategy list with state; start/stop; a global trading halt; the daily-executor halt.
- Every action needs a confirmation, a reason, and is written to an audit log.
- Only after auth, roles and audit exist.
- **Built 2026-10-08 (§8.12):** the daily-executor halt/resume per ledger, as an *Operator controls*
  card on `/risk`, gated by a local operator token (OIDC later) with an audit log.
- **Built 2026-10-08 (§8.13):** `/strategies`: the strategy list with state, start/stop (a persisted
  hold) for the strategy-executor's strategies, and the global kill switch (halt every local ledger).

---

## 5. Backend for the UI (`services/ui-api`, built)

### 5.1 Endpoints (GET only, except the operator controls of §8.12–8.13; any other method returns 405)

| Endpoint | Source | Notes |
|---|---|---|
| `GET /api/ui/healthz` | process + ledger stat | record count, policy source |
| `GET /api/ui/forward-tests` | latest ledger record per book | + distance to SMA, excess vs hold, milestones, run status, risk counts |
| `GET /api/ui/forward-tests/{book}` | same, one book | `{book}` validated against `^[a-z]{2,6}_[a-z]{2,6}$` and the known books |
| `GET /api/ui/forward-tests/{book}/ledger[?mode=stage]` | all records for a book | + equity series and stage fills with fee/slippage bps |
| `GET /api/ui/forward-tests/{book}/candles?days=180` | latest `<book>_daily_<date>.csv` | + SMA50, `long`, `volume_ratio_20d`; SMA50 matches the ledger to the cent (tested) |
| `GET /api/ui/risk` | ledger + candles + risk policy | policy, exposure, utilization, next-order check, realized cost, findings, halt file |
| `GET /api/ui/ledgers` | configured ledgers (`-ledgers`) | name, path, found, records, default; every endpoint above takes `?ledger=<name>` |
| `GET /api/ui/live?books=[&market=1]` | live hub (Bitso public WS) | snapshot JSON: upstream status, last trade, bid/ask, forming candle, provisional flip level. With `market=1` also `markets[]`: top-20 depth per side, spread, last 50 trades (§8.11). Display only; 503 with `-live=false` |
| `GET /api/ui/stream?books=[&market=1]` | live hub | Server-Sent Events: `snapshot`, `book` (≤1/s per book), `status`, `heartbeat` (15 s); with `market=1` also `market` (after each `book`) |
| `GET /api/ui/research/studies` | `-studies-dir` (default `docs/backtest-readiness`) | metadata per study: kind, date, question, summary, follows, references, evidence dir; empty list if the dir is missing |
| `GET /api/ui/research/studies/{name}` | one study file | sanitized HTML (goldmark, raw HTML dropped), h2/h3 headings, followed-by / referenced-by; 400 bad name, 404 unknown |
| `GET /api/ui/research/runs` | `research-run/v1` JSON in `evidence-<date>/` | summary per report: data, costs (incl. additive `level`/`note`), window headers, per-rule beat-hold scores, citing studies; unreadable reports listed as `skipped` |
| `GET /api/ui/research/runs/{date}/{name}` | one report | the summary plus the report passed through unchanged, so additive fields reach the UI |
| `GET /api/ui/health/data` | `-archive s3://bucket` (list/get only) + ledger dir | collector flushes, compaction, executor coverage, last `run-*.log`, S3 upload; ok/warn/fail checks; S3 listing cached 60 s; `?ledger=` picks the executor section |
| `GET /api/ui/controls` | halt file + `ui-audit.jsonl` next to the ledger | whether halt/resume is enabled for this ledger (and why not), the halt file, the audit log newest first (§8.12) |
| `POST /api/ui/risk/halt`, `POST /api/ui/risk/resume` | writes `risk-state.json` | R4: bearer operator token, loopback `Origin`, JSON `{reason, by, confirm}`; every attempt audited; 401/403/400/409/415 as in §8.12 |
| `GET /api/ui/strategies` | ledgers' halt files + strategy-executor `GET /api/v1/strategies` (`-strategy-executor-url`) | ledgers with halt state, kill-switch state, the executor's strategies (state, exposure, metrics, hold), orphan holds, the strategy audit log (§8.13) |
| `POST /api/ui/strategies/{name}/start`, `…/stop` | strategy-executor start (`release_hold`) / stop (`hold`) | same gates as R4, `confirm` = strategy name, `ack_position` to stop with exposure; 404/409 passed through, 502 if the executor fails (§8.13) |
| `POST /api/ui/risk/halt-all` | writes `risk-state.json` next to every local ledger | the kill switch: `confirm` = `HALT ALL`; one audited halt per ledger under a shared `group` id; already halted ledgers left as they are (§8.13) |

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
  `ui-api -ledgers name=s3://…` reads that copy. ~~**Still open:** a scheduled run that sets it.~~
  **Done:** the 06:15 UTC `ops-run.sh executor` cron job uploads it (first copy 2026-10-08 06:15 UTC).

---

## 6. Risk management (backend + frontend)

### 6.1 What already existed (survey)

| Component | Enforces | Wired? |
|---|---|---|
| `order-management/internal/risk` | position ≤ `MAX_POSITION_SIZE`, open orders, order value, orders/minute; concentration (warn) | **Yes**, on the OM order path (`order_manager.go` → `CheckRisk`). ~~But trading-engine can still place an order after OM rejects it.~~ **Fixed 2026-10-08 (R5a, §6.4.2).** Exposure readable at `GET /api/v1/risk/exposure`. |
| `order-management` `GET /api/v1/risk/session` | daily realized P&L, drawdown % | Read by trading-engine |
| trading-engine `CheckSessionLimits` | `MaxDailyLoss`, `MaxDrawdownPct` | ~~Called, but both are 0, so it never blocks.~~ **Blocks since 2026-10-08 (R5a):** from env, defaults 500 / 10 % |
| strategy-executor `internal/risk` | trade amount, positions, hours | ~~Dead code~~ **Deleted 2026-10-08 (R5a)** with the unused `internal/manager` |
| Strategy params `max_daily_loss_quote` | per-strategy daily-loss pause | limit-profit and momentum only |
| daily-executor | `DAILY_EXECUTOR_DISABLED=1`, `-size` ≤ 0.01 BTC, stale-candle refusal, history-revision refusal, file lock, stage-only URL, balance check before orders; **since 2026-10-05 also `shared/pkg/risk.Check` before every stage order (R1)** | **Yes**, the only path that trades today |
| Global halt / StopAll | ~~none~~ halt files (`risk-state.json`) | ~~**Missing**~~ daily-executor since R2; ui-api HALT ALL since R4; **trading-engine and order-management since R5b (2026-10-08, §6.4.3)** via `TRADING_HALT_FILES` |

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
| **R4 (done 2026-10-08, local token)** Halt from the UI, §8.12 | `POST /api/ui/risk/halt` and `/resume` with a required reason, an audit log (append-only JSONL), a confirmation dialog | Built localhost-only behind a 0600 operator-token file instead of OIDC (decided 2026-10-08); OIDC swaps in with Phase 4; every attempt is audited |
| **R5a (done 2026-10-08)** Platform-wide, §6.4.2 | Load trading-engine `MaxDailyLoss`/`MaxDrawdownPct` from env (non-zero defaults); stop trading-engine from placing orders OM rejected; expose OM `GetCurrentExposure`; delete the dead strategy-executor risk package; live trading-engine fails closed (needs OM, stage only) | The session check actually blocks; a pending or rejected OM row never approves an order |
| **R5b (done 2026-10-08)** Platform-wide, §6.4.3 | Move OM and trading-engine checks onto `shared/pkg/risk` (one policy format across services); trading-engine honours the halt (kill switch) | One policy format across services; HALT ALL also stops trading-engine |
| **R6 (done 2026-10-08)** Trading-risk metrics, §6.4.4 | Production-standard telemetry, alerts, runbook and dashboard for limits, halts, execution quality and reconciliation (§11 Q4) | Every limit, the kill switch and execution quality are visible and alert with a runbook; rules are unit-tested in CI |
| **R5c (done 2026-10-08)** Cluster kill switch, §6.4.5 | ConfigMap `trading-halt` mounted into trading-engine and order-management; `scripts/k8s-halt.sh` with confirmation, audit and read-back | One audited command halts every trading pod in a namespace within seconds; CI proves every overlay is wired |
| **R5d (done 2026-10-08)** OMS limits in the shared policy, §6.4.6 | Order-management's position, order-value, open-order and orders-per-minute limits become one `shared/pkg/risk` policy (per book and firm-wide); the env limits are only its fallback | One limit set per service in one format; a policy file can tighten any book without a redeploy of code |
| **R6b (done 2026-10-08)** Realized execution and portfolio risk, §6.4.7 | Realized slippage per closed order vs its decision price; exposure per book marked to market; 1-day 99 % parametric VaR per quote currency against a limit; Alertmanager routing by severity and team | Implementation shortfall, exposure and VaR are on the dashboard and alert with a runbook; every route is pinned in CI; nothing is sent until receivers are configured |

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

#### 6.4.2 R5a as built (2026-10-08)

- **Session limits block.** `services/trading-engine/internal/guard` loads `TRADING_MAX_DAILY_LOSS`
  (quote currency, default 500) and `TRADING_MAX_DRAWDOWN_PCT` (default 10) into the trading config;
  the executor's `CheckSessionLimits` runs before every order with order-management's session P&L.
  0, negative, NaN, a typo or a drawdown above 100 stops start-up (a typo must not disable a limit).
- **Live mode fails closed.** Without `DRY_RUN` the engine refuses to start without
  `ORDER_MANAGEMENT_URL` (pre-trade validation and session P&L come from it; without it every order
  passed unchecked) and on any Bitso host but `stage.bitso.com` unless
  `TRADING_ENGINE_ALLOW_PRODUCTION=1` (§11 Q3: production later, stage for now). k8s already sets
  both (`k8s/base/trading-engine.yaml`).
- **No order after an OM rejection.** order-management's signal consumer stores the order as
  `pending` *before* it validates and risk-checks it, and trading-engine's `POST
  /api/v1/orders/validate` treated that row as "risk already applied" and approved. A pending row now
  vouches only once it settles: validation waits up to 2 s, approves if it becomes `validated`,
  refuses if it is `rejected` (with the rejection reason) or still pending. Errors from either OM call
  already blocked the order.
- **Exposure.** `GET /api/v1/risk/exposure?book=<book>` on order-management: position size and
  value, open orders, the position limit and utilization, as the risk check sees them.
- **Dead code removed.** strategy-executor `internal/risk` and the unused `internal/manager` that
  was its only importer.
- **CI.** The `platform-risk` job in `operator-ui.yml` runs trading-engine's and order-management's
  tests.
- **Still R5b:** one policy format (`shared/pkg/risk`) across order-management and trading-engine,
  and trading-engine honouring the halt so HALT ALL stops it too. The daily-executor's limits (one
  0.01 BTC leg per book per day) do not fit intraday strategies, so this needs per-service limits in
  the shared policy first.

#### 6.4.3 R5b as built (2026-10-08)

- **One format, per-service limits.** Both services read `TRADING_RISK_POLICY` (a `shared/pkg/risk`
  policy file, the daily-executor's JSON) and `TRADING_HALT_FILES` (comma list of `risk-state.json`).
  Each service keeps its own limits in that one format: trading-engine's built-in
  `trading-engine-default-2026-10-08` keeps its existing per-order caps (btc_mxn 0.1 BTC / 10,000 MXN,
  btc_usd 0.1 BTC / 600 USD, others 0.01 BTC) and adds a 500 bps fat-finger guard against the touch
  mid. Order-management applies a policy only when one is set, on top of its `RiskConfig` limits.
- **trading-engine** (`internal/guard/pretrade.go`): the gate runs after signal price validation and
  before the session and OM checks. A block records `orders_failed{reason="risk_policy"}`. The
  position comes from OM `GET /api/v1/risk/exposure`, so a reducing sell is never trapped; an unknown
  position, an unreadable or invalid halt file all block (fail closed). Start-up logs the policy
  version and any halted or invalid halt file, and warns when live mode has no halt files.
- **order-management** (`internal/risk/shared_policy.go`): `CheckRisk` adds critical violations
  `shared_policy:<rule>` (metric `risk_violations{type="shared_policy"}`), so a halt also stops orders
  that reach OM by another path.
- **Kill switch.** Point both at the halt files ui-api writes (the stage ledger's
  `risk-state.json`, plus any other ledger halt file) and HALT ALL stops the daily-executor,
  trading-engine and order-management. In k8s this needs the halt files on a volume both pods mount;
  ~~until then the env is documented, not set~~ done in R5c (§6.4.5): ConfigMap `trading-halt`.
- **Tests.** `guard/pretrade_test.go`, `execution/exposure_test.go`,
  `risk/shared_policy_test.go`; CI `platform-risk` gofmt-checks them and now vets all of
  order-management (also fixed a context leak on its start-up error paths).
- ~~**Not done.** Order-management's own limits (`MAX_POSITION_SIZE`, open orders, orders/minute)
  still live in `RiskConfig`; moving them into the policy file needs per-minute and open-order
  limits in `shared/pkg/risk` first.~~ Done in R5d (§6.4.6).

#### 6.4.4 Trading-risk operations metrics as built (2026-10-08)

Standard asked for (§11 Q4): what a hedge fund or private bank risk desk monitors. That means the
kill switch and whether it can reach every engine; every limit and how close orders and the session
run to it (warn at 80 %, page at 100 %); every breach attempt, reviewed; fail-closed blocks;
execution quality (price vs arrival mid, signal staleness, tick-to-trade, fill ratio); and
OMS-exchange reconciliation. Every alert has a severity (critical pages, warning is reviewed the same
trading day), an owning team and a runbook entry.

- **New trading-engine series** (`internal/metrics/risk.go`): `trading_halt_active`,
  `trading_halt_files_invalid`, `trading_halt_files_configured`,
  `trading_halt_last_check_timestamp_seconds` (halt files evaluated every 10 s by
  `guard.WatchHalts`, so the state shows without signals); `trading_risk_policy_info{version,source}`;
  `trading_risk_limit{book,limit}` (policy limits plus `book="session"` daily loss and drawdown);
  `pretrade_policy_checks_total{book,result}`, `pretrade_policy_rejections_total{book,rule}`;
  histograms `order_limit_utilization_ratio{book,limit}`, `order_price_deviation_bps{book,side}`,
  `signal_age_at_decision_seconds{book}`, `signal_to_order_latency_seconds{book}`; gauge
  `trading_session_limit_utilization_ratio{limit}`. All labels are bounded (books, rule and limit
  names).
- **Alerts** (`monitoring/prometheus/rules/trading-risk-alerts.yml`, 18 rules in three groups):
  kill switch engaged / halt file invalid / kill switch unreachable on a live engine / watcher stale;
  policy version drift; risk state unknown; limit breach attempt; order-limit utilization p95 > 90 %;
  session limit 80 % (warning) and 100 % (critical); price vs mid p95 > 100 bps; signal age p95 > 30 s;
  tick-to-trade p99 > 10 s; exchange order sync and fill polling stale (> 5 min); OMS reject rate
  > 20 %; fill ratio < 50 %; shared-policy block at the OMS. Unit tests in
  `monitoring/prometheus/tests/`; k8s `PrometheusRule` generated by `scripts/sync-prometheus-rules.sh`.
- **Runbook:** [`docs/runbooks/TRADING-RISK-ALERTS.md`](../runbooks/TRADING-RISK-ALERTS.md).
- **Dashboard:** Trading Risk Operations
  (`monitoring/grafana/dashboards/domain/trading-risk-operations.json`): controls, pre-trade,
  execution quality, reconciliation and freshness.
- **CI:** job `monitoring-rules` runs `promtool check rules`, the rule unit tests, the k8s sync check
  and parses every dashboard PromQL expression.
- ~~**Not covered yet:** realized slippage per fill vs arrival price (needs fill prices joined to the
  decision mid in order-management), VaR / exposure in quote currency across books, and Alertmanager
  routing (on-call receivers are not configured in this repo).~~ Done in R6b (§6.4.7); receivers are
  still a template.

#### 6.4.5 R5c cluster kill switch as built (2026-10-08)

- **Mechanism.** ConfigMap `trading-halt` (`k8s/base/trading-halt.yaml`) holds one
  `risk-state.json` in the `shared/pkg/risk` HaltState format. trading-engine and order-management
  mount it read-only as a directory at `/etc/trading-halt` (no `subPath`, which would never see
  updates) and set `TRADING_HALT_FILES=/etc/trading-halt/risk-state.json`. Both already re-read the
  file before every order (R5b), so a halt needs no restart. A missing ConfigMap keeps the pods from
  starting, and a broken file blocks every order (fail closed).
- **Why a ConfigMap, not a shared volume.** ui-api is not deployed in the cluster yet (Phase 5), so a
  ReadWriteMany volume would have no writer. A ConfigMap needs no storage class, is versioned by the
  API server, and `kubectl apply` replaces it atomically.
- **Operator command.** `scripts/k8s-halt.sh -n <ns> halt|resume --reason ... --confirm <ns>` and
  `status`:
  - requires the namespace (no default), a reason of at least 8 characters, and the namespace typed
    again;
  - writes `{halted, reason, by, at}` plus ConfigMap annotations;
  - annotates the pods so the kubelet refreshes the volume within seconds instead of its ~1–2 min
    sync, then reads the file back from every trading-engine and order-management pod until all
    see the change (default 90 s; exit 1 and `unverified` in the audit if not);
  - appends every attempt (refused, failed, unverified, applied) to
    `~/.trading-ops/k8s-halt-audit.jsonl`.
- **Tests (CI `drill` job).** `scripts/tests/k8s-halt-test.sh` runs the script against a fake
  kubectl: refusals, dry run, propagation lag, stuck pods, an apply failure, status exit codes. The
  files it writes, and the shipped default, are parsed by the real Go parser
  (`shared/pkg/risk/halt_external_test.go`). `scripts/tests/check-k8s-halt-wiring.py` fails any
  rendered overlay (development, staging, production) that does not mount the ConfigMap correctly
  or ships it halted.
- **Also fixed.** The staging and production overlays did not build: their `*-patches.yaml` files
  hold only comments, and kustomize refuses an empty patch. The entries are commented out until a
  real patch exists.
- **Not done.** The Risk page's HALT ALL writes local ledger halt files only; it does not reach the
  cluster. That needs ui-api in the cluster (Phase 5) or an authenticated call to the Kubernetes
  API, so it waits for OIDC and roles.

#### 6.4.6 R5d order-management limits in the shared policy as built (2026-10-08)

- **New in `shared/pkg/risk`.** Per book: `max_open_orders`, `max_orders_per_minute`. Firm-wide:
  an optional `portfolio` section with the same two fields (rules `portfolio_max_open_orders`,
  `portfolio_max_orders_per_minute`). They count orders *before* this one and apply to
  non-reducing orders only, so a reducing sell is never trapped. All fields are `omitempty`; old
  policy files and ledger lines parse unchanged; negative values fail validation.
- **One limit set in order-management.** `CheckRisk` now enforces only the shared policy (plus the
  concentration warning). Without `TRADING_RISK_POLICY` the policy `order-management-env` is built
  from the old env limits with the old meaning: `MAX_POSITION_SIZE` and `MAX_ORDER_VALUE` per book
  (`default`), `MAX_OPEN_ORDERS` and `MAX_ORDERS_PER_MINUTE` firm-wide (`portfolio`). With a file,
  the file is the source of truth; if it has no `portfolio` section the env firm-wide limits are
  kept, so a file written for trading-engine cannot silently drop the runaway guards. Start-up logs
  the effective policy and its source.
- **Counting.** Open orders come from the order repository, excluding the order under check; a
  repository error while an open-order limit is set blocks (fail closed). Orders per minute is a
  60 s sliding window of *accepted* orders, counted once per signal id (else order id), because the
  signal consumer and `POST /api/v1/orders/validate` can both check the same signal. The window is
  in memory per pod: with N replicas the effective firm-wide rate is up to N × the limit
  (order-management runs one replica today).
- **Unchanged.** Metric labels (`risk_violations{type="position_limits"|"order_limits"|
  "rate_limit"|"shared_policy"}`), so the dashboard and `OrderSharedPolicyBlockAtOMS` keep working;
  the validator still enforces `MAX_ORDER_VALUE` separately; `config.Validate` still refuses zero or
  negative env limits.
- **Tests.** `shared/pkg/risk/orderflow_test.go`; order-management `risk_manager_test.go` (sliding
  window and dedup with a fake clock) and `shared_policy_test.go` (env build, file precedence,
  portfolio fallback, per-book open orders). CI `platform-risk` gofmt-checks them.

#### 6.4.7 R6b realized execution, exposure, VaR and alert routing as built (2026-10-08)

- **Realized slippage (implementation shortfall, fees excluded).** When order-management's Bitso
  sync closes an order with fills (filled, or cancelled after a partial fill), the average fill
  price is compared once with the order's own price, the price the signal decided on: histogram
  `order_arrival_slippage_bps{book,side}` (positive = cost), counters
  `order_slippage_cost_quote_total{book,side,direction="adverse"|"improvement"}` (split so both stay
  monotonic) and `order_filled_notional_quote_total{book,side}` for the notional-weighted figure.
  The order's metadata keeps `arrival_slippage_bps` and a flag so a re-sync never counts it twice.
  With trading-engine's `order_price_deviation_bps` (decision price vs the touch mid) this splits
  shortfall into "priced away from the market" and "the fill moved".
- **Exposure and VaR** (`internal/risk/portfolio.go`, every `RISK_PORTFOLIO_INTERVAL`, 30 s):
  each position is marked at market-data's ticker mid (`MARKET_DATA_URL`, set in k8s), else its
  entry price with `position_mark_fallback=1`. Published: `position_exposure_base{book}`,
  `position_exposure_quote{book,currency}`, `portfolio_net_exposure_base{asset}`,
  `portfolio_gross_exposure_quote` / `portfolio_net_exposure_quote{currency}`, and
  `portfolio_var_quote{currency}` = 2.326 × |Σ exposure × daily vol| (1 day, 99 %, books in one
  currency share the base asset so correlation 1). The daily vol is a configured model parameter
  (`RISK_VAR_DAILY_VOL`, default 4 %, above BTC's usual 2.5–3.5 %; per book
  `RISK_VAR_DAILY_VOL_BOOKS`), published as `risk_var_daily_vol_ratio{book}`: the cluster has no
  daily closes for a historical or EWMA estimate yet. `RISK_VAR_LIMITS` (`MXN=20000,USD=1000`)
  publishes `portfolio_var_limit_quote`. VaR is reported, never enforced. A value that does not
  parse stops start-up.
- **Fixed: position sign.** `models.Position` keeps an unsigned size plus a side, and
  order-management passed the unsigned size to the shared check and the exposure endpoint, which
  trading-engine feeds to its own check. A short then looked long: a covering buy could be blocked
  and a sell growing the short passed as "reducing". Both now use the signed position. (Bitso spot
  cannot short, so this needed a tracking anomaly to matter.)
- **Alerts** (5 new, 23 total): `OrderSlippageHigh` (> 50 bps notional-weighted over 6 h),
  `PortfolioVaRLimitWarning` (80 %) / `PortfolioVaRLimitBreached` (100 %), `PortfolioRiskStale`,
  `PositionUnpriced`; promtool unit tests; runbook entries; a "Portfolio risk and realized
  execution" row on the Trading Risk Operations dashboard.
- **Alertmanager** (`monitoring/alertmanager/alertmanager.yml`, which `docker-compose.yml` already
  mounted but did not exist): critical pages the owning team (`risk` / `trading`) with a 1 h
  repeat, warning goes to the team's review queue, alerts without a team go to platform;
  breached-supersedes-warning and halt-supersedes-OMS-block inhibitions. **Template:** receivers
  have no integrations, so nothing is sent; the header shows how to add PagerDuty, Slack or SNS with
  secrets from files. `scripts/tests/check-alertmanager-routes.sh` (CI `monitoring-rules`) runs
  `amtool check-config`, pins every route, fails on a severity or team the routes do not know, on an
  integration committed to the repo, and on an inhibition naming an unknown alert.

### 6.5 First findings from the real stage ledger
- **btc_mxn's first stage leg cost 118 bps against 70 assumed.** The post-only order rested 60 min, filled 0.1%, and fell back to market: taker fee 78 bps + 40 bps above the fill-day open. A stage leg is small and stage liquidity is thin, so this is not yet evidence about production costs. But it is the cost signal to watch: the pre-registration's secondary (taker) scenario is 88 bps per leg, and this leg exceeded both.
- **btc_usd's leg cost 41 bps against 40 assumed** (maker fill, 25 bps fee + 16 bps slippage).
- **At 17:35 Mexico City on 2026-10-03, the 2026-10-02 bar was not yet recorded** for either book. The UI flagged it as `run missed`.

---

## 7. Backend fixes to do before or alongside the UI

1. **Auth that actually validates.** OIDC (e.g. Amazon Cognito or Google) with signed JWT validation in the BFF; drop the accept-anything gateway checks. TLS on the ALB (ACM certificate, HTTP→HTTPS redirect).
2. ~~**CORS** restricted to the UI origin; remove the hard-coded `*` in `market-data` and `backtesting`.~~ **Done 2026-10-08:** new `shared/pkg/httpcors` is the one policy for `market-data`, `backtesting` and `api-gateway`: exact origins from `CORS_ALLOWED_ORIGINS` (comma-separated `scheme://host[:port]`), `*` refused, allowed origin echoed with `Vary: Origin`, foreign preflight 403. Unset or invalid sends no CORS headers (same-origin only). The gateway's own `*`-default middleware (which also had a suffix-match bug) is deleted. (`ui-api` sets no CORS headers at all; the UI is same-origin.)
3. ~~**Do not expose signal-publishing endpoints** (`/strategies/process`, `/test/signals`, order cancel) through any public route.~~ **Done 2026-10-08:** the gateway (the only public route, ingress `/`) is read-only for strategies: `GET /api/v1/strategies`, `/status`, `/{name}`. Sub-paths (`start`, `stop`, `config`) are 404, writes on `/{name}` are 405, and the strategy-executor internals `process`, `order-fill`, `types`, `stats` are 404. `/test/signals` and the order routes were never proxied and stay unreachable; `internal/api/handlers_test.go` pins all of this. Start/stop stays on ui-api (§8.13).
4. ~~**Fix or remove the gateway's dead routes** (orders and positions on order-management, strategy config PUT) and the port defaults.~~ **Done 2026-10-08:** order and position routes removed (order-management serves none of them; every call was a 404), with `order_handlers.go`; strategy start/stop/config handlers removed. Defaults now match the k8s Services: gateway `SERVICE_PORT` 8085, `ORDER_MANAGEMENT_URL` `:8082`, `STRATEGY_EXECUTOR_URL` `:8081` (they were 8080/8081/8082, so order-management and strategy-executor pointed at each other); `TestLoadDefaultPorts` pins them. CI job `edge-services` in `operator-ui.yml`.
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
| **3. Market data** | Market page via BFF proxy and SSE; candles with volume ratio | Live ticker updates within 2 s; no direct browser calls to internal services | **Done**: live-data slice (2026-10-06, §8.3); Market page (2026-10-08, §8.11) |
| **4. Hardening + controls** | OIDC, TLS, CORS, audit log, role-gated controls (halt, kill switch, start/stop), risk **R4** | Security review passes; every control action is audited | **R4 + audit log done** (2026-10-08, local token, §8.12); **kill switch and start/stop done** (2026-10-08, local token, §8.13); OIDC, TLS, roles to do |
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
  (`{path, found, halted, reason, by, at, error}`). Read-only; the file is edited by hand until R4
  (*R4 writes it since 2026-10-08, §8.12*).
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

### 8.11 Market page (as built, 2026-10-08)

- **Source decision.** The plan said "`market-data` REST, live via SSE". `market-data` (8083) is
  not running and not reachable from the workstation, and it would add Kafka and Redis to a
  display-only page. So the page extends the `ui-api` live hub (§8.3), which already holds one
  connection to Bitso's public WebSocket. The browser still calls only `ui-api`. If `market-data`
  comes back, the hub can be swapped behind the same contract.
- **Contract (additive).** `?market=1` on `/api/ui/live` and `/api/ui/stream` adds `markets[]`
  (one per book) and, on the stream, a `market` event after each `book` event. It goes only to
  subscribers that asked for it, so the Forward-tests pages get the same bytes as before. Fields:
  `bid`, `ask`, `mid`, `spread`, `spread_bps`, `bids`/`asks` (`{price, amount}`, best first),
  `depth_at`, `trades` and `tape_seeded`. The levels are merged at equal prices, invalid levels
  are dropped, and each side is capped at 20, which is what the Bitso `orders` channel sends as a
  full snapshot. `trades` holds the newest 50 (`{id, price, amount, side, at}`), deduplicated by
  id.
- **Taker side.** In the WS `trades` channel, `t` = 0 is a taker buy and `t` = 1 is a taker sell.
  This was checked against REST `/api/v3/trades`, whose `maker_side` is always the opposite for
  the same `tid`. The tape colours the taker: a buy lifted the ask, a sell hit the bid.
- **Tape seed.** When the hub connects it fetches the last 50 trades per book from REST
  `/api/v3/trades` (`RESTTapeSeeder`), so the tape is full at once. `tape_seeded` says so. Live
  trades merge in by id. A REST error (e.g. 429) only leaves the tape to fill from the WebSocket.
- **Page (`/market`).**
  - **Tickers.** One card per forward-test book: last trade with tick flash, taker side, bid/ask,
    spread in quote and bps with a gauge. The cards are also the book switcher (`?book=`), and a
    switch keeps the same stream.
  - **Order book.** Top 12 levels per side: asks above, best at the bottom, then a spread/mid
    row, then bids. Cumulative-depth bars share one scale, under a bid/ask share meter.
  - **Recent trades.** Newest first, with taker-buy share and VWAP of the tape. New trades flash
    on arrival.
  - **Daily candles.** SMA50 and volume, highlighting days with `volume_ratio_20d` ≥ 1.5, plus
    today's forming bar. Above the chart: last closed day's volume, the ratio, the implied 20-day
    average and the number of high-volume days in the range.
- **Done-when check.** Bid/ask/spread update on every flush (≤ 1 s per book) and the last price
  updates on every trade, so the 2 s target holds.
  `curl '127.0.0.1:8090/api/ui/live?books=btc_mxn,btc_usd&market=1'` showed spreads of about
  2–6 bps, 14–20 levels per side and 50 seeded trades.
- **Tests.**
  - Go: level parsing and merging, tape order/dedupe/cap, seeded-tape merge, spread maths,
    `market` events only for market subscribers, the REST seeder including a 429, and
    `TestStreamMarket`.
  - Web: the contract of the captured `live-market.json`, the helpers (`lib/market.ts`), page
    render, book switch without reconnect, `market` events, contract errors.
  - Playwright: desktop and phone.

### 8.12 R4 operator controls (as built, 2026-10-08)

- **Decision (2026-10-08).** Build R4 before OIDC, localhost-only, behind a local operator token
  and an audit log; OIDC replaces the token in Phase 4 proper. The executor needed no change: it
  already reads `risk-state.json` before every run and fails closed on an invalid file (§8.2).
- **Off by default.** `ui-api -operator-token-file <file>` (env `UI_API_OPERATOR_TOKEN_FILE`) turns
  the controls on. The file must be mode 0600 or stricter, hold ≥ 32 characters with no
  whitespace, and the flag is refused unless `-addr` is loopback. Without it `GET /api/ui/controls`
  says why (`enabled: false`) and the POSTs are 403.
- **Endpoints.** `POST /api/ui/risk/halt` and `/resume` (`?ledger=`), body
  `{"reason", "by", "confirm"}`. Gates, in order:
  1. controls on, else 403; a ledger read from S3 is excluded (403): its halt file is a copy, so
     writing it would not reach the executor;
  2. `Origin`, when sent, must be loopback, else 403 (a page on another site cannot drive it);
  3. `Authorization: Bearer <token>`, compared in constant time, else 401;
  4. `Content-Type: application/json` (415), body ≤ 4 KB, unknown fields rejected (400);
  5. `reason` 8–500 characters on one line, `by` matching `^[A-Za-z0-9][A-Za-z0-9 ._@-]{0,63}$`,
     `confirm` equal to the ledger name (400);
  6. state: halting a halted ledger, resuming one that is not halted by the file, or resuming over
     an invalid file are 409 (the last stays a hand fix). Halting over an invalid file is allowed.
  Only POST is allowed, and only on these two paths; everything else is still 405.
- **Order of a change.** An audit line `requested` is appended first; if that fails the response
  is 500 and nothing changes. Then the halt file is written atomically: validated with
  `risk.ParseHaltState` (the executor's own rules), written to a temp file, fsynced, renamed, then
  the directory is fsynced. Then `done` or `failed` is appended. A mutex serialises controls.
  Resume writes `{"halted": false, reason, by, at}`, so the file keeps who lifted the halt.
- **Audit log.** `ui-audit.jsonl` next to the ledger, mode 0600, append-only (`O_APPEND`, fsync per
  line). One line per attempt: `id`, `at`, `action`, `outcome` (`requested`, `done`, `failed`,
  `refused`, `denied`), `ledger`, `by`, `reason`, remote address, user agent, the halt state
  `before`/`after`, `error`. Refused and denied attempts are logged too. `GET /api/ui/controls`
  returns it newest first; unreadable lines are skipped and reported.
- **Alerts.** While a ledger is halted by the file, `ui-alerts` (§8.9) raises a warning
  `<ledger>/risk.halted` ("reason (by X at T, halt file)"), so a forgotten halt keeps reminding.
- **UI.** An *Operator controls* card on `/risk`, under the counters:
  - state (no halt / halted by operator / invalid file), with who, when and why;
  - **Halt trading** or **Resume trading**, disabled when the controls are off (the card says why);
  - a confirmation dialog: reason, by (remembered in localStorage), the ledger name typed again,
    and the token (password field, kept in **sessionStorage** for the tab, cleared on a 401).
    Validation mirrors the server for instant feedback; the server decides. Focus is trapped,
    Escape cancels, and a failed submit focuses the first bad field;
  - after a change the card, the halt banner and the forward-test views refresh, and a status line
    names the audit id;
  - the audit log table (time, action, outcome, by, reason or error), 8 rows with "show all".
  The halt banner now points to the card. Long banner text (paths) wraps on phones.
- **Mock mode.** MSW keeps a halt and an audit log in memory (`web/src/mocks/controls.ts`), mirrors
  the server's checks, and overlays the halt on `/risk`. Any non-empty token is accepted there.
- **Checked live** with `-ledgers stage=…,drill=<scratch copy>`: a foreign `Origin` (403), a wrong
  confirm (400), halt (200; `/risk` shows `halt_source: file`), resume (200), each audited; then the
  same through the UI on desktop and phone. The real stage ledger was not touched.
- **Tests.**
  - Go: the audit file (append, tail order, bad lines, permissions); every gate and status above,
    the order of audit lines, atomic write, invalid-file cases, S3 excluded, token-file checks; the
    `risk.halted` alert.
  - Web: the captured `controls.json` contract, `controlProblems`, card on/off, dialog validation,
    halt → audit → resume, a 401 clearing the token, a 409, mock parity.
  - Playwright (desktop and phone): halt and resume through the dialog, no horizontal overflow.
- **Still to do (Phase 4 proper).** OIDC and roles instead of the shared token, TLS. (Start/stop for
  the other strategies and the global kill switch: built, §8.13.)

### 8.13 Strategies page, holds and the kill switch (as built, 2026-10-08)

- **Decision (2026-10-08).** Start/stop next, keeping the shared operator token; OIDC, TLS and roles
  stay deferred. Scope agreed: a `/strategies` page with the ledgers and their halt state, a
  halt-all kill switch, and start/stop of the intraday strategy-executor's strategies (only when
  `ui-api` is given the executor's URL), with stops persisted in strategy-executor.
- **strategy-executor: the hold list.** A stop from an operator is a *hold*:
  - `STRATEGY_HOLD_FILE` (JSON `{"version":1,"holds":{name:{reason,by,at}}}`) is read at boot; a
    missing file is an empty list, a corrupt one stops the boot (fail closed). Writes are atomic
    (temp file, fsync, rename); a failed write leaves the list unchanged and the strategy running.
  - A held strategy refuses `Start` (409 `held`), `StartAll` skips it, and the hold applies again as
    soon as a strategy with that name is registered, so neither strategy-router nor
    `scripts/start-organic-trading.sh` nor a restart can undo it.
  - `POST /api/v1/strategies/{name}/stop` takes an optional body `{by, reason, hold}`; `…/start`
    takes `{release_hold}` (the hold comes back if the start fails). An empty body keeps the old
    behaviour, so the router and scripts are unchanged.
  - Lifecycle errors are JSON `{error, code}`: 404 `not_found`, 409 `already_running`,
    `not_running`, `held` (they were all 500). The strategy list adds `holds`.
- **ui-api.** `-strategy-executor-url` (env `UI_API_STRATEGY_EXECUTOR_URL`; loopback hosts only,
  no credentials in the URL) and `-strategy-audit-file` (env `UI_API_STRATEGY_AUDIT_FILE`; default
  `ui-strategy-audit.jsonl` next to the first local ledger).
  - `GET /api/ui/strategies`: ledgers (halt file, whether controls work, why not), the kill switch
    (targets, already halted), the executor (configured, reachable, holds supported, error), its
    strategies sorted by name with state, exposure, P&L, `dry_run` and hold, holds on names not
    registered, and the strategy audit log.
  - `POST /api/ui/strategies/{name}/start|stop`: the R4 gates (§8.12) with `confirm` = the strategy
    name. Then the state is read from the executor: 404 if unknown; 409 to start a running one or to
    stop one already stopped and held; 409 to stop one with an open position or pending order unless
    `ack_position` (a stopped strategy places no exit orders; the audit `detail` records the
    exposure). Audit `requested` → executor call → `done`/`failed`; an executor 404/409 is passed
    through, anything else is 502. Stop always sends `hold: true` and start `release_hold: true`.
  - `POST /api/ui/risk/halt-all`: `confirm` = `HALT ALL`. Each local ledger gets its own audited halt
    (the §8.12 write path) in its own `ui-audit.jsonl`, sharing a `group` id; ledgers already halted
    are reported `already_halted` and left alone; 500 if any write failed. Ledgers read from S3 are
    not targets.
  - Audit entries gain additive fields: `group`, `strategy`, `executor`, `upstream_status`, `detail`.
- **UI.** `/strategies` (nav "Strategies", after Risk):
  - the **kill switch** card: targets, "N of M to halt", a danger button and a dialog that needs the
    phrase `HALT ALL`; the result names each ledger's outcome and the group id;
  - **ledger cards**: trading allowed / halted (who, when, why) / invalid file / S3 copy, each
    linking to its Risk page to resume;
  - **Intraday strategies**: counters (registered, running, held, open exposure), a table with
    type, version, dry-run, book, status (running / held with reason / stopped), exposure,
    signals/trades, P&L, win rate and **Stop** or **Start**; the stop dialog asks for an
    acknowledgement when the strategy has exposure; banners for no executor, unreachable, read-only
    (no token) and an executor without a hold list; the strategy audit log.
  - The confirmation dialog is shared with the Risk page (`components/ConfirmDialog.tsx`).
- **Mock mode.** `web/src/mocks/strategies.ts` keeps strategies, holds and the audit log in memory,
  mirrors the server's checks, and halts the same in-memory ledgers as the Risk mock. The fixture
  `fixtures/strategies.json` was captured from a real ui-api against the sandboxed executor below.
- **Checked live** against a sandboxed strategy-executor (no Kafka, no Redis, no exchange keys, so
  it cannot trade) with three dry-run demo strategies: stop through ui-api held `mr_btc_demo`; a
  plain executor start then got 409 `held`; after an executor restart and re-registration it was
  still held while the others started. Then stop and start through the UI. The kill switch was
  exercised in tests and mock mode only; the real stage ledger was not halted.
- **Tests.**
  - strategy-executor: the hold file (load, missing, corrupt, atomic write, failed write), the
    registry (held start refused, `StartAll` skips, hold restored on a failed start), the HTTP
    bodies and codes.
  - ui-api: the executor client (loopback only, errors), every gate and state above, audit order,
    position acknowledgement, upstream 409/502, halt-all with an already halted ledger.
  - Web: the fixture contract, the page, stop with confirm and audit, the acknowledgement, start
    releasing a hold, a 409, the kill switch reaching the Risk page, disabled and unreachable
    states, mock parity. Playwright (desktop and phone): stop, start, kill switch, Risk page.

### 8.14 Kill-switch drill (as run, 2026-10-08)

- **Setup.** A second, throwaway `ui-api` that knows **only scratch copies** of the stage ledger
  dir (`alpha`, `beta`, `gamma`) plus the stage ledger's S3 copy (`s3copy`, read-only, to confirm
  remote ledgers are not targets), its own port, live data off, the operator token; a second Vite
  dev server pointed at it (`UI_API_URL=http://127.0.0.1:8093 npx vite --port 5174`). `gamma` was
  halted beforehand.
- **Through the UI** (`/strategies` on that server): "2 of 3 to halt"; the dialog listed `gamma`
  as already halted; after `HALT ALL` the result read `alpha halted · beta halted · gamma already
  halted` in ~130 ms, the button disabled itself ("all halted"), the Risk page showed the halt
  banner, and alpha was resumed from its Risk page. `s3copy` stayed "S3 copy", untouched.
- **Checked beyond the UI:**
  - gates: no/wrong token 401, foreign `Origin` 403, `text/plain` 415, wrong phrase, short reason
    and an extra `ledgers` field 400; no halt file written; every refusal audited in every ledger;
  - `gamma` kept its own halt (reason, who, when); one `group` id in all three audit logs;
  - the **daily-executor itself** (dry run, `-no-record`, no keys) printed `HALTED by …/beta/
    risk-state.json … stage orders will be blocked and recorded`, and refused to run over a
    corrupt halt file ("nothing ran");
  - `ui-alerts -dry-run` raised "Trading halted" for each halted copy;
  - two simultaneous presses: one halted all three, the other reported `already_halted` for each;
  - a press over a corrupt halt file replaced it with a valid halt;
  - the stage ledger dir (ledger checksum, no `risk-state.json`) and its S3 copy were unchanged.
- **Repeatable.** `scripts/kill-switch-drill.sh` does all of the above except the browser part in
  a temp dir with its own token and port and exits non-zero on any failure (38 checks; `--keep`
  keeps the work dir, `--source <dir>` copies another ledger dir, `--offline` points the
  executor's candle fetch at a closed local port, `--no-executor` skips the executor runs). Run it
  after any change to the controls.
- **In CI.** The `drill` job of `.github/workflows/operator-ui.yml` runs it on every push that
  touches ui-api, the daily-executor, the shared risk/ledger packages or the script:
  `--offline --source services/ui-api/internal/api/testdata` (the checked-in fixture ledger), so
  it needs no stage data, no keys, no AWS and no Bitso. On failure it uploads the ui-api log, the
  audit logs and the halt files.
- **Findings, fixed (2026-10-08).**
  - A `GET` on a control path was 404 (it fell through to the `/api/` catch-all). It is now
    **405 with `Allow: POST`**; the drill checks it.
  - The alert e-mail subject named only the first halted ledger. It now names **every halted
    ledger**, halts first (so the 99-character SNS limit cuts the other title, not the list), e.g.
    `[mtb-ops] 2 warning: Trading halted (alpha, beta, gamma)`, including a ledger halted (and
    announced) before the kill switch; the body ends with `Halted ledgers now: …`. A subject
    without halt news does not repeat them; resolved halts are named together the same way.

---

## 9. How to run (local)

```bash
export PATH=$PWD/.tools/node/bin:$PATH              # portable Node 22
(cd services/ui-api && go run ./cmd) &              # 127.0.0.1:8090, stage ledger, live on, studies from docs/backtest-readiness
#   add -archive s3://mtb-development-data-archive-<account> for the Data health archive section
#   the Market page is http://127.0.0.1:5173/market (needs live on, the default)
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
# R4 halt/resume from the Risk page (§8.12): create a 0600 token once, then start ui-api with it:
#   mkdir -p ~/.config/mtb && (umask 077; openssl rand -hex 32 > ~/.config/mtb/operator-token)
#   go run ./cmd -operator-token-file ~/.config/mtb/operator-token
#   to rehearse, add a scratch copy of a ledger dir: -ledgers stage=<path>/ledger.jsonl,drill=<copy>/ledger.jsonl
# Strategies page start/stop (§8.13): run strategy-executor with a hold file, then point ui-api at it:
#   (cd services/strategy-executor && STRATEGY_HOLD_FILE=$HOME/.config/mtb/strategy-holds.json go run ./cmd)
#   go run ./cmd -operator-token-file ~/.config/mtb/operator-token -strategy-executor-url http://127.0.0.1:8081
#   the page is http://127.0.0.1:5173/strategies (the kill switch halts every local ledger: rehearse on a drill copy)
# Kill-switch drill (§8.14): scratch copies, a throwaway ui-api, 38 checks; touches no real ledger:
#   scripts/kill-switch-drill.sh            # --keep, --source <ledger dir>, --offline, --no-executor
#   scripts/kill-switch-drill.sh --offline --source services/ui-api/internal/api/testdata   # as CI runs it
cd web && npm ci && npm run dev                     # http://127.0.0.1:5173
# or, without the backend:
npm run dev:mock
# tests
(cd shared && go test ./pkg/risk ./pkg/dailyledger)
(cd services/ui-api && go test ./...)
(cd services/strategy-executor && go test ./cmd/daily-executor ./internal/strategies ./internal/server)
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
3. ~~Should controls ever reach production trading, or stay limited to stage?~~ **Yes, they will reach production; for now they stay limited to stage** (decided 2026-10-08). Enforced: the daily-executor trades only on the stage URL, and a live trading-engine refuses a non-stage Bitso host unless `TRADING_ENGINE_ALLOW_PRODUCTION=1` (§6.4.2). Going to production needs OIDC, TLS and roles (Phase 4) first.
4. ~~Is Grafana enough for service metrics?~~ **Follow production standards, as hedge funds and private banks run trading operations** (decided 2026-10-08). Proposed reading: Prometheus + Grafana stay the service-metrics stack (the UI links out rather than rebuilding it), with SLOs and alerts on the order path and a trading-operations dashboard set: signal-to-order latency (p50/p99), order reject and error rates by reason, fill ratio and slippage against arrival price (implementation shortfall), exposure and limit utilization per book, intraday P&L and drawdown against limits, position reconciliation breaks (order-management vs exchange), market-data and ledger freshness, kill-switch time-to-halt, and alert acknowledgement time. To be planned as its own step.
5. ~~**R1 policy on a blocked stage order?**~~ **Skip and record, no retry of that order** (decided 2026-10-05). As built (§6.4.1), the next day's run re-plans from the recorded position, so the stage position diverges from paper only until the next run that is not blocked, rather than until the next signal flip. This keeps a blocked *exit* from leaving a long position open through a whole flat period.
6. ~~Are the default limits in §6.2 right?~~ **Keep the defaults** (decided 2026-10-05), version `default-2026-10-03`. Revisit `cost_warn_bps` once more stage legs exist (btc_mxn's first leg already exceeded it).
