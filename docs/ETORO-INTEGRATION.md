# eToro API integration

The trading engine can execute against **Bitso** (default) or the **eToro Public API** by setting `BROKER=etoro`.

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

## Runtime configuration

| Variable | Description |
|----------|-------------|
| `BROKER` | `bitso` (default) or `etoro` |
| `ETORO_ENV` | `demo` (paper) or `real` — must match the key’s environment |
| `DRY_RUN` | Log orders without calling the broker API |
| `ETORO_DEFAULT_SYMBOL` | Default symbol label for logs when using eToro |

In Kubernetes, set `broker` and `etoro-env` in the `trading-config` ConfigMap (`k8s/base/configmap.yaml`).

## Trade signals (Kafka)

For eToro, set the signal `book` field to the ticker symbol (e.g. `AAPL`, `BTC`). Optional metadata:

```json
{
  "instrument_id": 1234,
  "reason": "strategy signal"
}
```

- **BUY**: opens a market position by cash `amount` via `/trading/execution/{env}/market-open-orders/by-amount`
- **SELL**: closes an existing long position for that instrument, or opens a short if none exists

## Code layout

- `shared/pkg/etoro/` — HTTP client (search, rates, portfolio, open/close)
- `services/trading-engine/internal/execution/etoro_executor.go` — order execution
- `shared/pkg/config/broker.go` — `BROKER` parsing

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

Work for eToro integration lives on `feat/etoro-api-integration`, based on `feat/k8s-deployment-manifests`.
