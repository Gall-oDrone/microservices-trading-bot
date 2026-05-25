# Organic trading startup

**Organic** means the strategy-executor **poll loop** reads **live trades** from market-data (HTTP), updates indicators (Redis when connected), runs **started** strategies, and publishes to **`trading.signals`** when rules fire—**without** calling `/api/v1/strategies/process` (synthetic ticks).

See also: `scripts/start-organic-trading.sh`.

---

## Prerequisites (cluster)

| Area | What to verify |
|------|----------------|
| **Namespace** | Pods in `bitso-trading-dev` (or your env): `market-data`, `strategy-executor`, `kafka`, `redis`, `trading-engine`, `order-management`. |
| **Books** | `trading-config` → `bitso-books` includes your book (e.g. `btc_mxn`). |
| **Kafka** | Topics exist: at least `trading.signals`; market-data topics if you rely on Kafka publishing from market-data. |
| **Redis** | strategy-executor logs show **`Connected to Redis`** (not in-memory fallback). |
| **Stage + execution** | trading-engine: `DRY_RUN` unset; `BITSO_API_BASE_URL` Stage; API keys in secrets; OM reachable for pre-trade validation. |
| **Order size** | Strategy `position_size` (and OM limits) meet **minimum order size** (e.g. ≥ 0.001 BTC for `btc_mxn` in dev). |

---

## Ordered steps

1. **Confirm kubectl** points at the correct cluster (`kubectl config current-context`).
2. **Confirm workloads** are Running (`kubectl get pods -n <ns>`).
3. **Confirm Redis** for strategy-executor: `kubectl logs deploy/strategy-executor -n <ns> | grep -i redis` → prefer `Connected to Redis`.
4. **Confirm market-data** is ingesting trades for your book (logs / metrics).
5. **Create and start a strategy** via strategy-executor HTTP API (`POST /api/v1/strategies`, `POST .../start`) with conservative parameters for organic observation.
6. **Do not** use `POST /api/v1/strategies/process` for organic-only validation (that injects a price).
7. **Observe**: strategy-executor logs (published signals), trading-engine logs/metrics, Grafana, Bitso Stage UI for orders.
8. **Multi-replica trading-engine**: Service `/metrics` may not show `signals_received_total` on every replica (Kafka partition assignment). Use Grafana, scale to one replica for debugging, or inspect the consumer pod from `kafka-consumer-groups.sh --describe`.

---

## Tunables (optional)

| Variable / setting | Role |
|--------------------|------|
| `INDICATORS_UPDATE_INTERVAL` | How often the organic loop fetches latest trade(s) and runs strategies (default often 30s in strategy-executor). |
| `INDICATORS_BOOKS` | Books polled by the loop (default often `btc_mxn`). |
| Strategy parameters | `lookback_period`, `entry_threshold`, etc.—tighter = more frequent signals (and more noise). |

---

## Script automation

Run from repo root:

```bash
./scripts/start-organic-trading.sh
```

Environment overrides:

| Variable | Default | Description |
|----------|---------|-------------|
| `NAMESPACE` | `bitso-trading-dev` | Kubernetes namespace |
| `BOOK` | `btc_mxn` | Trading book |
| `STRATEGY_TYPE` | `mean_reversion` | Single strategy: `mean_reversion`, `momentum`, or `limit_profit` |
| `STRATEGY_TYPES` | (unset) | Comma-separated list for multi-strategy registration |
| `STRATEGY_NAME` | `organic_<type>_<timestamp>` | Strategy name (single-type mode) |
| `ROUTER_MANAGED` | `false` | When `true`, register strategies but do **not** start — `strategy-router` picks the active one |
| `STRATEGY_EXECUTOR_LOCAL_PORT` | `8084` | Local port for port-forward |
| `POSITION_SIZE` | `0.001` | Must meet OM / exchange minimums |
| `DRY_RUN` | `false` | Tag signals with `metadata.dry_run=true` (engine should skip execution) |

Strategy-specific variables are documented in [LIMIT-PROFIT-STRATEGY.md](LIMIT-PROFIT-STRATEGY.md), [MOMENTUM-STRATEGY.md](MOMENTUM-STRATEGY.md), and [strategy-fee-accuracy/POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md](strategy-fee-accuracy/POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md).

### Router-managed organic trading

When using the in-cluster regime router ([`strategy-fee-accuracy/STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md`](strategy-fee-accuracy/STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md)):

```bash
ROUTER_MANAGED=true BOOK=btc_mxn ./scripts/start-organic-trading.sh
# Then ensure strategy-router is running in the cluster (DRY_RUN=true for first soak).
```

The script verifies pods, checks Redis in logs, port-forwards strategy-executor, creates (and usually starts) strategies, then prints follow-up commands.

---

## Troubleshooting

- **`Insufficient trades for btc_mxn: got N, need 20`** in strategy-executor logs: the indicator service needs enough recent trades from market-data before bands/SMA stabilize; wait for **N ≥ 20** or reduce indicator window in config if your deployment exposes it.
- **GET `/api/v1/strategies/{name}`** should return the **instance** `name` (the id you passed at create) and **`parameters`** from the stored config. If you still see stale behavior, ensure the running image includes the strategy-executor registry fix (instance name + parameters on `StrategyInfo`).
