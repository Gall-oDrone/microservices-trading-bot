# Order management: practical next checks (metrics & Bitso sync)

Use this when **`bitso_sync_last_success_timestamp_seconds` is 0**, **`orders_filled_total`** does not increase, or **P&L / trades today** stay flat while Bitso shows fills.

## How to read the current metrics

### `bitso_sync_last_success_timestamp_seconds`

- **0** often means the gauge was **never set**, not necessarily that Bitso is unreachable.
- In the current service code, the timestamp is updated only on a **subset** of successful sync paths (when there are **active** OM orders with `metadata["bitso_order_id"]` to reconcile). If **`ListActiveBitsoOrderIDs`** is empty, the job may still run (`bitso_sync_attempts_total` increases) and **`bitso_sync_errors_total`** may stay 0, while this timestamp remains **0**.
- **Do not** treat **`0` alone** as proof that sync is broken; combine with attempts, errors, and logs below.

### `orders_filled_total` / P&L / trades today

- **`orders_filled_total`** may **not appear** in `/metrics` until the first increment (Prometheus counters with labels).
- **P&L** (`trading_daily_realized_pnl_currency`), **trades today** (`trading_trades_today_total`), **wins/losses** update when **`SyncOrderFromBitso`** applies a **filled** terminal state and **`recordTradeClosedForIntraday`** runs. If fills never reach OM, these stay **0**.

## Commands (cluster)

**Namespace** (adjust if yours differs):

```bash
export NS=bitso-trading-dev
```

### 1. Deployment image and single replica

```bash
kubectl get deploy order-management -n "$NS" -o jsonpath='{.spec.replicas} replicas {.spec.template.spec.containers[0].image}{"\n"}'
```

### 2. Order-management logs (linkage, sync, errors)

```bash
kubectl logs -n "$NS" -l service=order-management --since=30m | grep -iE 'Linked Bitso|SyncOrderFromBitso|LookupOrders|OrderTrades|ListActiveBitsoOrderIDs|Bitso sync|Warn|Error'
```

### 3. Kafka: signals + orders placed (consumer groups)

```bash
NAMESPACE="$NS" ./scripts/debug-kafka-orders-placed.sh
```

Confirm **`trading.signals`** group (`order-management-group-signals`) and **`trading.orders.placed`** group (`order-management-group-orders-placed`) show sane lag after a test.

### 4. Prometheus (port-forward then query)

```bash
kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090 &
```

Examples:

```bash
curl -sG http://127.0.0.1:9090/api/v1/query --data-urlencode 'query=bitso_sync_attempts_total{namespace="'"$NS"'"}' | jq .
curl -sG http://127.0.0.1:9090/api/v1/query --data-urlencode 'query=bitso_sync_errors_total{namespace="'"$NS"'"}' | jq .
curl -sG http://127.0.0.1:9090/api/v1/query --data-urlencode 'query=bitso_sync_last_success_timestamp_seconds{namespace="'"$NS"'"}' | jq .
curl -sG http://127.0.0.1:9090/api/v1/query --data-urlencode 'query=orders_filled_total{namespace="'"$NS"'"}' | jq .
curl -sG http://127.0.0.1:9090/api/v1/query --data-urlencode 'query=trading_trades_today_total{namespace="'"$NS"'"}' | jq .
```

### 5. Scrape OM directly

```bash
kubectl exec -n "$NS" deploy/order-management -- wget -qO- http://127.0.0.1:8082/metrics | grep -E 'bitso_sync_|orders_filled|orders_created|orders_active|trading_trades_today|trading_daily_realized'
```

### 6. End-to-end smoke

```bash
USE_KUBECTL=1 NAMESPACE="$NS" ./scripts/exercise-stage-orders.sh
```

Then re-check logs for **`Linked Bitso order to signal order`** and Grafana after scrape interval.

## Common causes to verify

| Symptom | What to verify |
|--------|----------------|
| No **`Linked Bitso`** logs | **`trading.orders.placed`** not consumed; **signals** consumer not creating rows before placement; wrong **`KAFKA_TOPIC_SIGNALS`** / **`KAFKA_TOPIC_ORDERS_PLACED`**. |
| **`orders_rejected_total` … `risk_check_failed` > 0** | Signals fail risk/validation; fewer orders reach Bitso. |
| **`bitso_sync_last_success_timestamp_seconds` stuck at 0** with **attempts** increasing | Often **empty OID list** (no active OM orders with `bitso_order_id`) or instrumentation only updates on certain paths; confirm **Redis** order rows and Bitso **open orders** vs. OM state. |
| **Grafana doubles active orders** | Multiple replicas scraping the same gauge: prefer **`max()`** over **`sum()`** for gauges (see dashboards). |

## Optional improvement (code)

Consider updating **`bitso_sync_last_success_timestamp_seconds`** whenever a sync tick completes successfully **including** when there is **nothing to reconcile** (e.g. after **`refreshOpenOrdersGauge`** succeeds), so the metric reflects “sync loop healthy” rather than “had OIDs to poll.”
