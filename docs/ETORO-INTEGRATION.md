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

## API facts the code relies on (verified on demo, 2026-10-10)

- **Routes:**
  - v2 opens, `orders:lookup?orderId=`, costs and eligibility; v2 rates.
  - v1 search, candles, pnl portfolio, close and close-orders, trade history.
  - Demo and real paths differ non-uniformly; the full table is in `shared/pkg/etoro/paths.go`.
- **Idempotency:**
  - eToro rejects a reused `x-request-id` with 400 "ReferenceID … may already exists" (`etoro.IsDuplicateReference`).
  - `orders:lookup?referenceId=` does **not** find API-placed orders (404).
  - The broker adapter resolves a duplicate to the position the first attempt opened.
- **Consistency:** the pnl portfolio shows new and closed positions 1–3 s late. A position's `unrealizedPnL` is an object.
- **Order status:** a filled order reads `{id 3, "Filled"}`. Re-closing a closed position gives error 741.
- **Rate limits:** 60 reads/min and 20 trading writes/min per user key. The client paces at 55/18 and retries reads on 429/5xx (honouring `Retry-After`). It never retries writes.

## eToro MCP server (operators and IDE agents only)

- eToro runs an official remote MCP server at `https://mcp.public-api.etoro.com` (streamable HTTP) with a companion skill at `https://mcp.public-api.etoro.com/skill`.
- It exposes route discovery (`get-all-routes`, `get-route-spec`), account views and `prepare-trade` → `place-trade` / `prepare-close` → `place-close` with a confirmation gate.
- Copy [docs/etoro/mcp_config.example.json](etoro/mcp_config.example.json) into your IDE's MCP config (Antigravity: `mcp_config.json`) and fill in the keys locally.
- Use it for ad-hoc demo checks and to confirm route specs. **The bots never use it**: they call the REST API through `shared/pkg/etoro`.

## Code layout

- `shared/pkg/etoro/`: Public API client.
  - v2 orders, lookup, costs, eligibility, close, portfolio, history, candles, history candles.
  - Read/write pacing, typed errors, deterministic request ids.
  - httptest contract tests use trimmed real demo responses.
- `shared/pkg/broker/`: venue-neutral execution seam. `broker/etorobroker` is the eToro adapter (idempotent `Open`, bounded polling).
- `shared/pkg/mktcal/`: NYSE calendar (holidays, early closes, DST sessions, trading-day arithmetic).
- `shared/pkg/etorodaily/`: eToro daily candles turned into one bar per NYSE trading day. Written in the research CSV format, with a freshness check.
- `services/strategy-executor/cmd/etoro-spike/`: demo-only evidence recorder (`docs/etoro/evidence-*`).
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

## Branch

The original integration lives on `feat/etoro-api-integration` (based on `feat/k8s-deployment-manifests`). It was merged into `feat/etoro` together with `feat/operator-ui` on 2026-10-10.
