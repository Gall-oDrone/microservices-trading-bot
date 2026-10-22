# Operations Environment Variables

Quick reference for env vars used by the trading bot, especially for **intraday**, **order sync**, and **Bitso testing**. See service-specific READMEs for full config.

---

## Trading-engine

| Variable | Default | Description |
|----------|---------|-------------|
| `BITSO_API_BASE_URL` | `https://stage.bitso.com/api` | Bitso API base. In live mode (no `DRY_RUN`) the engine refuses any host but `stage.bitso.com` unless `TRADING_ENGINE_ALLOW_PRODUCTION=1`. |
| `STAGE_BITSO_API_KEY` | - | Bitso **stage** API key (required for placing orders). |
| `STAGE_BITSO_APISECRET` | - | Bitso **stage** API secret. |
| `DRY_RUN` | - | Set to `true` or `1` to log orders only; no Bitso `PlaceOrder` calls. |
| `ORDER_MANAGEMENT_URL` | - | Base URL of order-management (e.g. `http://order-management:8082`) for pre-trade validation and session risk (daily loss / drawdown). **Required in live mode** (the engine refuses to start without it since R5, 2026-10-08). |
| `TRADING_MAX_DAILY_LOSS` | `500` | Max realized loss per session, in the book's quote currency (MXN for btc_mxn). Must be > 0; a value that does not parse stops start-up. |
| `TRADING_MAX_DRAWDOWN_PCT` | `10` | Max drawdown from peak equity, percent in (0, 100]. |
| `TRADING_ENGINE_ALLOW_PRODUCTION` | - | `1` allows a non-stage `BITSO_API_BASE_URL` in live mode. Leave unset: trading stays on stage until production is approved (decided 2026-10-08). |
| `TRADING_RISK_POLICY` | built-in | Per-order policy file in the shared format (`shared/pkg/risk`, same JSON as the daily-executor). Unset: built-in `trading-engine-default-2026-10-08` (btc_mxn 0.1 BTC / 10,000 MXN per order, 500 bps from the touch mid). A file that is missing, invalid or has unknown fields stops start-up. |
| `TRADING_HALT_FILES` | - | Comma-separated operator halt files (`risk-state.json`). Any one halted, unreadable or invalid blocks every new order (metric reason `risk_policy`). Point it at the ledger halt files ui-api writes so **HALT ALL** stops the engine. Live mode without it logs a warning. |
| `KAFKA_BROKERS` | - | Kafka broker list. |
| `KAFKA_TOPIC_SIGNALS` | `trading.signals` | Topic to consume trade signals from. |
| `KAFKA_TOPIC_ORDERS_PLACED` | `trading.orders.placed` | Topic to publish placed orders to (for order-management). |

---

## Order-management

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_BROKERS` | - | Kafka broker list. |
| `KAFKA_TOPIC_ORDERS_PLACED` | `trading.orders.placed` | Topic to consume placed orders from (must match trading-engine). |
| `KAFKA_CONSUMER_GROUP` | `order-management-group` | Consumer group; orders-placed consumer uses `-orders-placed` suffix. |
| `STAGE_BITSO_API_KEY` | - | Bitso stage API key; set to enable Bitso sync job (poll order status). |
| `STAGE_BITSO_APISECRET` | - | Bitso stage API secret. |
| `BITSO_API_BASE_URL` | `https://stage.bitso.com/api` | Bitso API base for sync job. |
| `MAX_POSITION_SIZE` | `1.0` | BTC per book. Without `TRADING_RISK_POLICY` this becomes `default.max_position_btc` of the env-built policy `order-management-env`. |
| `MAX_ORDER_VALUE` | `100000` | Quote currency per order (`default.max_order_notional`); the validator also enforces it. |
| `MAX_OPEN_ORDERS` | `10` | Firm-wide open orders (`portfolio.max_open_orders`). Kept even with a policy file that has no `portfolio` section. |
| `MAX_ORDERS_PER_MINUTE` | `60` | Firm-wide accepted orders in a 60 s sliding window, once per signal (`portfolio.max_orders_per_minute`). Per pod. Kept like `MAX_OPEN_ORDERS`. |
| `TRADING_RISK_POLICY` | - | Same file and format as trading-engine. Set: it is order-management's limit set (violation `shared_policy:<rule>`); per book it may add `max_open_orders` and `max_orders_per_minute`, and a `portfolio` section sets the firm-wide ones. Unset: the policy is built from the four env limits above (plan §6.4.6). |
| `TRADING_HALT_FILES` | - | Same list as trading-engine. A halted, unreadable or invalid file rejects every new order in `CheckRisk` (defence in depth behind trading-engine). |
| `MARKET_DATA_URL` | - | market-data base URL (k8s: `http://market-data:8083`). Positions are marked at its `/api/v1/ticker` mid for exposure and VaR; unset or unavailable: entry price, flagged `position_mark_fallback`. |
| `RISK_PORTFOLIO_INTERVAL` | `30s` | How often exposure and VaR are recomputed (Go duration, at least 1s). |
| `RISK_VAR_VOL_MODEL` | `estimated` | `estimated`: each book's daily vol is max(RiskMetrics EWMA, 365-day) of Bitso's public daily closes, with a 250-day backtest and a historical-simulation VaR (plan §6.4.8). `fixed`: always `RISK_VAR_DAILY_VOL` (no network). |
| `RISK_VAR_DAILY_VOL` | `0.04` | Daily vol as a ratio in (0, 1]. With `estimated`: the fallback while a book has no fresh estimate (`risk_var_vol_source{source="fallback"}`; alert `VaRVolEstimateFallback`). With `fixed`: the vol. |
| `RISK_VAR_DAILY_VOL_BOOKS` | - | Per-book overrides that win over the estimate, e.g. `btc_usd=0.03,btc_mxn=0.035`. Use to hold a vol above the estimate (e.g. after a red backtest); the estimate stays visible. |
| `RISK_VAR_EWMA_LAMBDA` | `0.94` | EWMA decay in [0.8, 1). Lower reacts faster to new volatility. |
| `RISK_VAR_VOL_REFRESH` | `6h` | How often the closes are re-fetched (at least 1m). Failures retry after 10 min, in the background; a portfolio run never waits for Bitso. |
| `RISK_VAR_VOL_STALE` | `72h` | An estimate whose last daily close is older than this is not used (at least 1h). |
| `RISK_VAR_VOL_SOURCE_URL` | `https://api.bitso.com` | Bitso public OHLC base (`/api/v3/ohlc`, no credentials). The pod needs egress to it; without it every book is on the fallback. |
| `RISK_VAR_LIMITS` | - | VaR limit per quote currency, e.g. `MXN=20000,USD=1000`. Alerts at 80 % / 100 %; nothing is blocked. Any `RISK_*` value that does not parse stops start-up. |
| `RISK_STRESS_SHOCKS` | `-0.5,-0.3,-0.2,-0.1,0.2` | Hypothetical uniform spot moves applied to each currency's net exposure (`portfolio_stress_loss_quote{type="hypothetical"}`). Distinct, non-zero, above −1, at most 10. The 8 historical episodes (plan §6.4.9) always run as well. |
| `RISK_STRESS_LIMITS` | - | Worst-stress-loss limit per quote currency, e.g. `MXN=150000,USD=5000`. `PortfolioStressLimitBreached` fires at 100 % for 15 min; nothing is blocked. Unset: no limit series, alert inactive. |
| `RISK_CAPITAL` | - | Risk capital per quote currency, e.g. `MXN=250000,USD=12000`. Publishes VaR, ES, worst stress and exposure as fractions of it (`portfolio_risk_capital_ratio`) and the reverse-stress move (`portfolio_reverse_stress_move_ratio`); `PortfolioStressExceedsCapital` fires when a stress scenario would lose all of it. Unset: no series. |

---

## Session risk (daily loss / drawdown)

- **Trading-engine:** `ORDER_MANAGEMENT_URL` (required in live mode) lets the engine call `GET /api/v1/risk/session` and `POST /api/v1/orders/validate` before placing orders; an error from either blocks the order.
- **Limits:** `TRADING_MAX_DAILY_LOSS` and `TRADING_MAX_DRAWDOWN_PCT` (defaults 500 and 10%). They can no longer be 0 (disabled).
- **Exposure:** order-management serves `GET /api/v1/risk/exposure?book=btc_mxn` (position, open orders, position limit and utilization), the numbers its risk check uses.
- **Pre-trade race:** if trading-engine validates while order-management's signal consumer is still checking the same signal (row `pending`), validation waits up to 2 s for the outcome and never approves a row that is rejected or still pending.
- **Shared policy and halt (R5b):** both services read `TRADING_RISK_POLICY` and `TRADING_HALT_FILES`. trading-engine checks first (with the touch mid as the fat-finger reference and the position from `/api/v1/risk/exposure`, failing closed if it is unknown); order-management checks again in `CheckRisk`. A sell that only reduces the position is never blocked by a size limit, only by a halt or the price guard.

---

## CORS (market-data, backtesting, api-gateway)

| Variable | Default | Meaning |
|---|---|---|
| `CORS_ALLOWED_ORIGINS` | unset | Comma-separated exact origins, e.g. `https://ops.example.com,http://127.0.0.1:5173`. Each must be `scheme://host[:port]` (http or https, no path). `*` is refused. Unset or invalid: no CORS headers are sent, so browsers reach the service same-origin only (an invalid value is logged as "CORS disabled"). The operator UI uses ui-api, which is same-origin and needs none of this. |

The api-gateway defaults also match the k8s Services: `SERVICE_PORT` 8085, `ORDER_MANAGEMENT_URL` `http://localhost:8082`, `STRATEGY_EXECUTOR_URL` `http://localhost:8081`, `MARKET_DATA_URL` `http://localhost:8083`.

---

## Backtest engine (backtesting, strategy-executor)

`services/backtesting` is the canonical backtest engine (plan §7 item 5).

| Variable | Service | Default | Meaning |
|---|---|---|---|
| `BACKTEST_DAILY_BARS_DIR` | backtesting | unset | Directory holding Bitso daily CSVs (`date,open,high,low,close,...`) for strategy `sma50_daily`, the frozen SMA50 rule on the shared simulator. A run picks a file with `strategy_params.bars_file`, a relative path inside this directory that defaults to `<book>_daily_bitso.csv`. Unset: `sma50_daily` fails with a clear error, and every other strategy is unaffected. |
| `STRATEGY_EXECUTOR_LEGACY_BACKTESTS` | strategy-executor | unset | `1` restores the deprecated in-process `/api/v1/backtests` handler for a transition period. Otherwise those routes answer 410 Gone, pointing to services/backtesting. |

Example `sma50_daily` request to backtesting `POST /api/v1/backtests` at Bitso btc_mxn taker cost (78 bps + 10 bps slippage per leg):

```json
{"name":"sma50 2025","book":"btc_mxn","start_date":"2025-01-01T00:00:00Z","end_date":"2025-12-31T00:00:00Z",
 "initial_balance":100000,"strategy":"sma50_daily","data_source":"file",
 "taker_fee":0.0078,"slippage_model":"percentage","slippage_value":0.001}
```

---

## eToro index-CFD demo executor and console (porting plan P3–P6)

Runbook: `docs/etoro/ETORO-DAILY-EXECUTOR.md`. Everything is **demo only**: `ETORO_ENV=real` is
refused by the executor, by ui-api's live feed and by the console's trade path — with one built,
not-yet-used exception, the measurement canary (runbook §14): a separate ui-api process started
with `-etoro-real-canary` **on the command line** (there is no environment-variable form) that
passes the six-part acknowledgement. Its variables are the last rows of the table.

| Variable | Where | Meaning |
|---|---|---|
| `ETORO_PUBLIC_KEY`, `ETORO_PRIVATE_KEY`, `ETORO_ENV` | `~/.config/microservices-trading-bot/etoro-demo.env` (chmod 600), read by the executor, `etoro-reconcile`, ui-api (`-etoro-env-file`) | API keys (x-api-key / x-user-key) and environment. `ETORO_ENV` must be `demo`. Never committed (`scripts/check-no-etoro-secrets.sh`). In k8s, `trading-config/etoro-env` defaults to `demo` and the keys come from `trading-secrets` (`k8s/EXTERNAL-SECRETS-SETUP.md`). |
| `ETORO_EXECUTOR_BIN`, `ETORO_RECONCILE_BIN`, `ETORO_RISK_REPORT_BIN` | `scripts/etoro-daily-executor-run.sh`, `scripts/ops-run.sh etoro-risk` | Binaries (default `services/strategy-executor/etoro-daily-data/<name>`; built by `scripts/install-ops-cron.sh`, rebuilt from the committed HEAD with `--rebuild-etoro`). |
| `ETORO_EXECUTOR_LEDGER` | wrapper | The ledger (default `etoro-daily-data/demo/ledger.jsonl`); the halt file, intents, run logs, reconcile reports and risk reports live next to it. |
| `ETORO_RECONCILE` | wrapper | `0` skips the read-only reconciliation after the run. |
| `ETORO_EXECUTOR_DISABLED` | cron environment | `1` makes the run exit without trading (one day off). |
| `ETORO_EXECUTOR_PROTECT` | cron environment | Protection phase C (2026-10-19, in force once the binaries are rebuilt): default on, the run sends the disaster stop `max(4 × ATR, 8 %)` with a rule open, ratchets a held stop daily (raise only) and records a venue close from the trading history; the risk policy defaults to `etoro-demo-2026-10-19`. `0` turns the stop and the ratchet off (`-protect=false` does the same; venue-close detection stays on, it only reads). Runbook §9. |
| `ETORO_EXECUTOR_S3_URI` | `ops.env`, wrapper, `ops-run.sh etoro-risk` | Copy of the ledger directory for the off-machine watchdog, `s3://<bucket>/daily-executor/etoro-demo` (written by the installer with `--bucket`). The run log is uploaded last; `upload=ok\|failed` is in the run log and is a ui-alerts warning (`etoro.upload`). `scripts/etoro-daily-executor-run.sh --upload-only` re-sends. |
| `ETORO_RISK_REPORT_YAHOO` | `ops.env`, `ops-run.sh etoro-risk` | Yahoo index histories for the weekly P4 risk report (default `docs/backtest-readiness/evidence-2026-10-10/data`). |
| `UI_API_ETORO_LEDGER`, `UI_API_ETORO_NAME` | ui-api `-etoro-ledger`, `-etoro-name` | Registers eToro ledgers as venue `etoro`: a plain path (named `etoro-demo` by `-etoro-name`) or, since 2026-10-21, a `name=path,name=path` list — the installer writes `etoro-demo`, `etoro-demo-etf` and `etoro-demo-rotation`. `/api/ui/etoro/*` take `?ledger=name` (default: the first); halt/resume and halt-all reach each ledger's halt file; the risk view shows the policy the executor applies to that ledger's product. The live feed and the assisted trades stay on the first eToro ledger. Empty: the console shows Bitso only. |
| `UI_API_ETORO_LIVE`, `UI_API_ETORO_ENV_FILE` | ui-api `-etoro-live` (default on), `-etoro-env-file` | The read-only rates poller (one `GetRates` every 5 s = 12 reads/min) behind `/api/ui/etoro/live` and `/stream`; off when the env file is missing or not `demo`. |
| `UI_API_ETORO_TRADING` | ui-api `-etoro-trading` (default **off**) | Assisted trades from the console (prepare → place). Needs the operator token, the local eToro ledger and the demo env file; a missing prerequisite is fatal at start. |
| `UI_ALERTS_ETORO_LEDGER`, `UI_ALERTS_ETORO_NAME` | ui-alerts `-etoro-ledger`, `-etoro-name` | The 15-minute alerts check these ledger directories (missed or failed 09:35 New York run, drift, unrecorded order, halt, failed upload): one path named by `-etoro-name`, or since 2026-10-21 a `name=path,name=path` list — the installer writes all three eToro cells (`etoro-demo`, `etoro-demo-etf`, `etoro-demo-rotation`); a cell whose ledger does not exist yet is a warning ("no ledger yet") until its first run. |
| `LEDGERS`, `MAX_AGE_HOURS_NYSE` | `infrastructure/lambda/ledger-watchdog` (deploy with `--ledgers …,etoro-demo=daily-executor/etoro-demo:nyse`) | `:nyse` ledgers are stale when no run log follows the last NYSE trading day's 09:35 New York run within `MAX_AGE_HOURS_NYSE` (6 h); weekends and holidays never page. For the canary add `etoro-real-canary=daily-executor/etoro-real-canary` **without** `:nyse` (the observer runs every day): stale after `MAX_AGE_HOURS` (30 h). |
| `ETORO_ENV=real`, `ETORO_PUBLIC_KEY`, `ETORO_PRIVATE_KEY` | `~/.config/microservices-trading-bot/etoro-real-canary.env` (0600; **written by hand**, never by a script), read by the canary ui-api, `etoro-canary-observe`, `etoro-fees-check -real-canary`, `etoro-reconcile -real-canary` | The canary's own key set (2026-10-22). Its public key's first 8–16 characters must match the `key_prefix` line of `~/.config/microservices-trading-bot/REAL-ACCOUNT-ACK` (0600, by hand; the sentence, `key_prefix`, `ledger`, `policy`, `start`, `end`, `name`). Never committed: `check-no-etoro-secrets.sh` scans the tree for both files' values and for a filled-in ack. |
| `UI_API_ADDR=127.0.0.1:8091`, `UI_API_OPERATOR_TOKEN_FILE`, `UI_API_DEMO_OPERATOR_TOKEN_FILE`, `UI_API_ETORO_ENV_FILE`, `UI_API_REAL_ACK_FILE`, `UI_API_ETORO_LEDGER=etoro-real-canary=…/real-canary/ledger.jsonl`, `UI_API_ETORO_TRADING=1`, `UI_API_LIVE=0` | `~/.config/microservices-trading-bot/ui-api-real-canary.env`, the only `EnvironmentFile` of unit `mtb-ui-api-real-canary` (`scripts/install-ui-api-service.sh --real-canary`; `ExecStart=… -etoro-real-canary`) | The canary console's settings: its own operator token (`~/.config/mtb/operator-token-real-canary`, must differ from the demo one by path and content; the demo path is named so the binary can compare), the real env file, the ack file (`-real-ack-file`), exactly one eToro ledger named `etoro-real-canary` under a `real-canary/` directory. The unit does not read `ops.env` or `ui-api.env`. `UI_API_CANARY_REHEARSAL=1` (`-canary-rehearsal`) is the demo rehearsal of the same policy on its own ledger; it excludes `-etoro-real-canary`. |
| `ETORO_CANARY_S3_URI`, `ETORO_CANARY_OBSERVE_BIN` | `ops.env` (written by `install-ops-cron.sh --canary-observe` with `--bucket`), `ops-run.sh etoro-canary-observe` | The observer's off-machine copy, `s3://<bucket>/daily-executor/etoro-real-canary` (its own prefix, never under `etoro-demo*`): `observations.jsonl`, `ledger.jsonl`, `reconcile/`, `intents/`, then `run-<UTC>.log` last with its `exit=` line. Empty: local only. The binary defaults to `services/strategy-executor/etoro-daily-data/etoro-canary-observe` (built by the installer, rebuilt with `--rebuild-etoro`). `MTB_ETORO_REAL_CANARY_ENV` only moves the installer's "env file not there yet" warning. |

Prometheus: the cron executor exposes no metrics; its alerting is ui-alerts (on the workstation) plus
the watchdog (off it). The `trading-engine`'s `BROKER=etoro` path reuses the engine's existing
metrics and rules (`monitoring/prometheus/rules/`), which carry no venue label.

## See also

- **docs/ORDER-FLOW-AND-BITSO-TESTING.md** — when orders hit Bitso testing, how to validate the flow, and Bitso dashboard.
- **INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md** — phases and status.
- **scripts/run-one-backtest.sh** — run one backtest and print the report (Phase 6 workflow).
