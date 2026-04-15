# Limit Profit Strategy - Operational Runbook

## Overview

This runbook covers operational procedures for the `limit_profit` strategy running on Bitso Stage/Production environments.

---

## Alert Response Procedures

### LimitProfitCircuitBreakerTripped

**Severity:** Critical  
**Impact:** Trading paused for this strategy

**Investigation:**
1. Check Grafana dashboard "Strategy Executor" → "Limit Profit Strategy" section
2. Review `limit_profit_daily_realized_pnl_quote` to see cumulative loss
3. Check exit reasons: `sum(limit_profit_exit_signals_total) by (reason)`

**Resolution:**
- **If expected (market conditions):** Wait for daily reset at `daily_loss_reset_hour_utc`
- **If unexpected (bug/configuration):**
  1. Review recent trades in order-management logs
  2. Check if stop_loss or max_hold exits are triggering frequently
  3. Consider adjusting `max_daily_loss_quote` or `stop_loss_quote` parameters

**Manual Reset (if needed):**
```bash
# Delete strategy and recreate to reset circuit breaker
curl -X DELETE http://localhost:8082/api/v1/strategies/<strategy-name>

# Recreate with adjusted parameters
curl -X POST http://localhost:8082/api/v1/strategies \
  -H "Content-Type: application/json" \
  -d '{"name":"limit_profit_btc_mxn","type":"limit_profit","book":"btc_mxn",...}'
```

---

### LimitProfitPendingCancelFailures

**Severity:** Warning  
**Impact:** Pending orders may not be cancelled, potential stuck state

**Investigation:**
1. Check order-management service health: `curl http://order-management:8086/health`
2. Verify Kafka connectivity between strategy-executor and order-management
3. Check order-management logs for cancel request failures

**Resolution:**
1. If order-management is down, restart the service
2. If Kafka issues, check broker health
3. Manually cancel stuck orders via Bitso API if needed:
```bash
# List pending orders
curl -X GET "https://api.bitso.com/v3/open_orders?book=btc_mxn" \
  -H "Authorization: Bitso ${API_KEY}:${NONCE}:${SIGNATURE}"

# Cancel specific order
curl -X DELETE "https://api.bitso.com/v3/orders/${OID}" \
  -H "Authorization: Bitso ${API_KEY}:${NONCE}:${SIGNATURE}"
```

---

### LimitProfitNoEntrySignals

**Severity:** Warning  
**Impact:** Strategy not trading despite being active

**Investigation:**
1. Check if circuit breaker is active: `limit_profit_circuit_breaker_active`
2. Verify indicator service is running: `strategy_executor_indicators_healthy`
3. Check market-data service for trade feed: `curl http://market-data:8083/api/v1/health`
4. Review strategy parameters (entry_offset may be too far from market)

**Resolution:**
- If circuit breaker active → see circuit breaker procedure
- If indicators stale → restart indicator service or check Redis
- If market-data down → restart market-data service
- If parameters issue → adjust `entry_offset` or `entry_offset_bps`

---

### RedisDown

**Severity:** Critical  
**Impact:** Strategy state persistence lost, indicators may fail

**Investigation:**
1. Check Redis container: `docker ps | grep redis`
2. Check Redis logs: `docker logs redis`
3. Verify Redis exporter: `curl http://localhost:9121/metrics | grep redis_up`

**Resolution:**
1. Restart Redis: `docker-compose restart redis`
2. If data corruption, restore from RDB backup
3. Strategy will reload state on next start (if state was persisted)

**Note:** Strategy-executor falls back to in-memory storage if Redis unavailable, but state won't persist across restarts.

---

## Routine Operations

### Starting the Strategy

```bash
# Using organic startup script
STRATEGY_TYPE=limit_profit \
BOOK=btc_mxn \
DRY_RUN=false \
MIN_PROFIT_LP=5000 \
STOP_LOSS_QUOTE=10000 \
MAX_DAILY_LOSS_QUOTE=50000 \
./scripts/start-organic-trading.sh
```

### Stopping the Strategy

```bash
# Graceful stop (completes current position if held)
curl -X POST http://localhost:8082/api/v1/strategies/limit_profit_btc_mxn/stop

# Delete strategy (clears state)
curl -X DELETE http://localhost:8082/api/v1/strategies/limit_profit_btc_mxn
```

### Checking Strategy Status

```bash
# Get strategy info
curl http://localhost:8082/api/v1/strategies/limit_profit_btc_mxn

# Get all strategies
curl http://localhost:8082/api/v1/strategies
```

### Adjusting Parameters at Runtime

```bash
# Stop, update config, restart
curl -X POST http://localhost:8082/api/v1/strategies/limit_profit_btc_mxn/stop

curl -X PUT http://localhost:8082/api/v1/strategies/limit_profit_btc_mxn \
  -H "Content-Type: application/json" \
  -d '{"parameters":{"min_profit":6000,"stop_loss_quote":8000}}'

curl -X POST http://localhost:8082/api/v1/strategies/limit_profit_btc_mxn/start
```

---

## Monitoring Dashboards

| Dashboard | URL | Purpose |
|-----------|-----|---------|
| Strategy Executor | Grafana → Strategy Executor | Main strategy metrics |
| Trading Metrics | Grafana → Trading Metrics | P&L, trades |
| Redis | Grafana → Redis | Infrastructure health |
| Kafka | Grafana → Kafka | Message flow |

### Key Metrics to Watch

| Metric | Normal Range | Alert Threshold |
|--------|--------------|-----------------|
| `limit_profit_circuit_breaker_active` | 0 | 1 |
| `limit_profit_daily_realized_pnl_quote` | > -max_daily_loss | < -5000 |
| `limit_profit_pending_cancel_failures_total` | 0 | > 2 in 15m |
| Avg position hold duration | < 300s | > 600s |

---

## Escalation

| Level | Contact | When |
|-------|---------|------|
| L1 | On-call engineer | First response, restart services |
| L2 | Trading team lead | Configuration changes, parameter tuning |
| L3 | Platform team | Infrastructure issues, code bugs |

---

## Recovery Checklist

After any incident:

1. [ ] Verify strategy is running: `strategy_executor_strategy_running`
2. [ ] Verify indicators healthy: `strategy_executor_indicators_healthy`
3. [ ] Check Redis connection: `redis_up == 1`
4. [ ] Check recent P&L: `limit_profit_daily_realized_pnl_quote`
5. [ ] Verify no stuck positions (check Bitso open orders)
6. [ ] Review last 10 trades for correct execution
7. [ ] Document incident and root cause
