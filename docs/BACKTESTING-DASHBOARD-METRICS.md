# Backtesting dashboard metrics and Redis trades

## Latest trades from Redis

Trades are stored by the **market-data** service in two places:

| Store | Redis DB | Keys | Description |
|-------|----------|------|-------------|
| **Cache** | 0 | `trade:{book}:{tradeID}`, list `recent_trades:{book}` | Recent trades (newest first); used by `GET /api/v1/trades?book=...&limit=N`. |
| **Historical storage** | 1 | `trade:{book}:{timestamp_unix}:{tradeID}` | Time-range queries for backtesting. |

### Option 1: Script (redis-cli)

```bash
# Latest 5 trades (default)
./scripts/show-latest-trades-redis.sh

# Latest 10 trades
./scripts/show-latest-trades-redis.sh 10

# Custom Redis
REDIS_HOST=myredis REDIS_PORT=6379 ./scripts/show-latest-trades-redis.sh 5
```

Requires `redis-cli`. If Redis is empty (market-data not running or no WebSocket traffic), output will be empty.

### Option 2: Market-data HTTP API

If market-data is running and has received trades:

```bash
# Recent trades (from cache; limit 10)
curl -s "http://localhost:8083/api/v1/trades?book=btc_mxn&limit=10"
```

For a time range (from historical storage, used by backtesting):

```bash
curl -s "http://localhost:8083/api/v1/trades?book=btc_mxn&from=2026-02-23T00:00:00Z&to=2026-02-24T00:00:00Z"
```

---

## Backtesting Grafana dashboard – which metrics show data?

When Prometheus scrapes the **backtesting** service and at least one backtest has been run, these panels should show **non-zero or non-empty** data (assuming the service is healthy and scraped):

| Panel | Metric(s) | What you should see |
|-------|-----------|----------------------|
| **Scrape status (up)** | `up{job=~"backtesting.*"}` | **1** when Prometheus is scraping the backtesting target. |
| **Backtests created (total)** | `backtesting_backtests_created_total` | **≥ 1** after creating at least one backtest (e.g. via `run-one-backtest.sh` or API). |
| **Backtests completed (total)** | `backtesting_backtests_completed_total` | **≥ 1** after at least one backtest has **completed** (status=completed). |
| **Active backtests** | `backtesting_active_backtests` | **1** while a backtest is running; **0** when none are running. |
| **Completed by status** | `backtesting_backtests_completed_total` by `status` | Time series with **completed** (and optionally **failed**) once backtests finish. |
| **Backtest duration (p95, seconds)** | `backtesting_backtest_duration_seconds` histogram | Non-empty after completed backtests (p95 of duration). |
| **Events processed (total by type)** | `backtesting_events_processed_total` by `event_type` | **trade** (and optionally ticker/orderbook) count > 0 when the backtest loaded and processed events. |
| **Data load duration (p95, seconds)** | `backtesting_data_load_duration_seconds` | Non-empty after backtests that loaded data from market-data (or file). |
| **Uptime (seconds)** | `backtesting_uptime_seconds` | Increases over time while the backtesting service is up. |
| **Health** | `backtesting_health{component="service"}` | **1** when the service reports healthy. |

Panels that may stay **0 or empty** until something goes wrong or you use certain features:

| Panel | When it shows data |
|-------|---------------------|
| **Failed by reason** | Only when a backtest **fails** (e.g. `backtesting_backtests_failed_total` by reason). |
| **Data fetch errors (total/rate)** | Only when the backtesting service fails to fetch data from market-data (or file). |

So: after running one or more backtests successfully, you should see non-zero data in **Backtests created**, **Backtests completed**, **Events processed**, **Backtest duration**, **Data load duration**, **Uptime**, and **Health**, and **Scrape status** = 1 if Prometheus is scraping the backtesting target.

### Why do Backtests created / completed (and related) show 0 in Grafana?

**Root cause:** The Grafana you are viewing uses a **different Prometheus** than the one that scrapes the backtesting instance you ran backtests against.

- **Docker:** Grafana at `http://localhost:3000` (Docker) uses Docker Prometheus, which scrapes the Docker backtesting container. Run backtests against `http://localhost:8084`; the dashboard should then show non-zero values.
- **Kubernetes:** Grafana in the cluster uses **cluster** Prometheus, which only scrapes **cluster** backtesting pods. Run at least one backtest against the cluster backtesting (e.g. `kubectl port-forward -n bitso-trading-dev svc/backtesting 8085:8084` then `./scripts/run-one-backtest.sh http://localhost:8085`). After ~15–30s, cluster Grafana Backtesting dashboard should show non-zero values.

### Why do I only see "1" for Scrape and Health?

- **Scrape status (up)** and **Health** showing **1** is correct: it means Prometheus is scraping the backtesting service and the service reports healthy.
- If other panels (Backtests created, Events processed, etc.) show 0 or “No data”, run at least one backtest (e.g. `./scripts/run-one-backtest.sh` or `./scripts/start-backtest-with-stage.sh`) so those metrics are emitted.
- If **Scrape status** is 0, Prometheus cannot reach the backtesting target. With Docker Compose, ensure backtesting and Prometheus are on the same network and both running (`docker compose up -d backtesting prometheus`). With Kubernetes, ensure ServiceMonitor or scrape config targets the backtesting service.
