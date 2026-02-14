# Grafana Dashboards – Troubleshooting

## Clarification: Prometheus vs Grafana

- **Prometheus** has **Targets** (Status → Targets) and a **query UI**; it does **not** have “panels” or dashboards. You do **not** need a script to “see panels in Prometheus.”
- **Panels and dashboards** are in **Grafana**. The Trading Engine (and other per-service) dashboards are Grafana dashboards that **query** Prometheus. If a dashboard shows no data, the fix is: ensure Prometheus is scraping the service, and that Grafana’s datasource points to Prometheus.

---

## Trading Engine dashboard shows no data

1. **Apply ServiceMonitors** so Prometheus discovers and scrapes the trading-engine (and other services):
   ```bash
   kubectl apply -f k8s/monitoring/servicemonitors.yaml
   ```
   Wait ~30–60 seconds for Prometheus to pick up new targets.

2. **Grafana datasource** must point to the **in-cluster** Prometheus (Grafana runs in the cluster and queries Prometheus by service name):
   - In Grafana: **Connections** → **Data sources** → **Prometheus**.
   - Set **URL** to: `http://kube-prometheus-stack-prometheus:9090` (or your Prometheus service in the `monitoring` namespace).
   - **If Prometheus is exposed under a path** (e.g. ALB path `/prometheus` and `routePrefix: /prometheus/`), the URL **must** include that path, e.g. `http://kube-prometheus-stack-prometheus:9090/prometheus`. Otherwise Grafana queries get **404** and dashboards show no data.
   - **Save & test**.

3. **Verify scraping** (use port-forward; the Prometheus API via the ALB may return HTML due to path routing):
   ```bash
   kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090
   # In another terminal:
   PROMETHEUS_URL=http://localhost:9090 ./scripts/verify-prometheus-scrape-service.sh trading-engine
   ```
   Or open Prometheus **Status** → **Targets** and confirm `trading-engine` (or `bitso-trading-dev` / trading-engine) is **UP**.

4. **Time range**: In the dashboard, set the time range to when the service was running (e.g. **Last 1 hour**).

5. **Optional – test balance panel without Bitso**: See section 5 below for `BALANCE_TEST_MXN` and related env vars.

---

## Per-service dashboards

In addition to **Trading Platform Metrics**, the repo includes one dashboard per service so you can verify metrics per service before relying on the consolidated view:

| Dashboard JSON | Service |
|----------------|--------|
| `monitoring/grafana/dashboards/trading-engine.json` | Trading Engine |
| `monitoring/grafana/dashboards/order-management.json` | Order Management |
| `monitoring/grafana/dashboards/api-gateway.json` | API Gateway |
| `monitoring/grafana/dashboards/market-data.json` | Market Data |

**Import:** Use the same flow as the main dashboard; point at the JSON file. Example:

```bash
# Port-forward Grafana, then:
GRAFANA_URL=http://localhost:3000 ./scripts/grafana-import-trading-dashboard.sh
# (Change DASHBOARD_JSON in the script to the path of the per-service JSON, or use Grafana UI: Create → Import → Upload JSON.)
```

**Verify Prometheus is scraping a service** (before or after importing the dashboard):

```bash
# Port-forward Prometheus first: kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090
PROMETHEUS_URL=http://localhost:9090 ./scripts/verify-prometheus-scrape-service.sh trading-engine
PROMETHEUS_URL=http://localhost:9090 ./scripts/verify-prometheus-scrape-service.sh order-management
# ... api-gateway, market-data

# Or run all at once:
./scripts/verify-prometheus-scrape-all-services.sh
```

If a service fails, check ServiceMonitors, the service’s `/metrics` endpoint, and Prometheus **Targets**.

---

## Trading Platform Metrics Dashboard – No Data

If the **Trading Platform Metrics** dashboard shows no data, check the following.

## 1. "Targets (scrape status)" panel empty

This panel shows `up` for all Prometheus targets. If it is empty:

- **Grafana is not talking to Prometheus**
  - In Grafana: **Connections** → **Data sources** → **Prometheus**.
  - Set **URL** to your Prometheus endpoint, e.g.:
    - In-cluster (Grafana in same cluster): `http://kube-prometheus-stack-prometheus:9090` (or your Prometheus service name). If Prometheus uses `routePrefix: /prometheus/`, use `http://kube-prometheus-stack-prometheus:9090/prometheus`.
    - Port-forward: `http://localhost:9090` if you ran `kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090` (port-forward bypasses route prefix).
  - **Save & test**; the test should succeed.

- **Prometheus is not scraping any targets**
  - Open Prometheus UI → **Status** → **Targets** (or `/targets`).
  - Ensure the trading services (trading-engine, order-management, market-data, etc.) are in the target list and **UP**.
  - If they are missing, apply the ServiceMonitors in `k8s/monitoring/servicemonitors.yaml` and ensure:
    - Services run in the namespace selected by the ServiceMonitors (e.g. `bitso-trading-dev`).
    - Services have the labels the ServiceMonitors select (e.g. `service: trading-engine`).
    - Prometheus is configured to select those ServiceMonitors (default with kube-prometheus-stack).

## 2. "Targets" has data but other panels are empty

- **Time range**: Use a time range where the services were running (e.g. **Last 1 hour**).
- **Metric names**: The dashboard expects:
  - Trading engine: `orders_executed_total`, `bitso_available_balance`, and `up{job=~"trading-engine|..."}`.
  - Order management / API gateway: `http_request_duration_seconds_bucket`, `trading_*` (e.g. `trading_daily_realized_pnl_currency`).
  - If your Prometheus uses different `job` names (e.g. from ServiceMonitors), the dashboard uses regex so names containing `trading-engine`, `order-management`, `api-gateway` should still match.
- **Services not exposing metrics**: Confirm each service has a `/metrics` endpoint and that Prometheus is scraping it (see **Targets**).

## 3. Provisioning the Prometheus datasource

If you provision Grafana datasources (e.g. via ConfigMap), use a URL that resolves from inside the cluster. For kube-prometheus-stack the Prometheus service is usually:

- `http://kube-prometheus-stack-prometheus:9090` (same namespace as Grafana).
- If Prometheus is configured with `routePrefix: /prometheus/` (e.g. when exposed at ALB path `/prometheus`), the datasource URL must be `http://kube-prometheus-stack-prometheus:9090/prometheus`, otherwise Grafana queries return 404 and dashboards show no data.

Example is in `monitoring/grafana/datasources/prometheus.yml`. Adjust the URL if your Helm release or service name differs.

## 4. Re-importing the dashboard

```bash
# With port-forward (Grafana on localhost:3000):
GRAFANA_URL=http://localhost:3000 ./scripts/grafana-import-trading-dashboard.sh

# With CloudFront / grafana.local:
GRAFANA_URL=http://grafana.local ./scripts/grafana-import-trading-dashboard.sh
```

After import, set the time range (e.g. **Last 1 hour**) and refresh. The top panel **Targets (scrape status)** should show at least one line per scraped target if Prometheus and the datasource are correct.

## 5. Testing the "Available Balance" panel without Bitso

The **Available Balance** panel reads the `bitso_available_balance` metric from the **trading-engine** service. For testing (e.g. without Bitso API credentials), you can set test values via environment variables. The trading-engine will expose these so Grafana shows data:

- `BALANCE_TEST_MXN=50000` – sets available MXN (e.g. for btc_mxn trading)
- `BALANCE_TEST_USD=1000` – optional
- `BALANCE_TEST_BTC=0.01` – optional
- `BALANCE_TEST_ETH=0.1` – optional

Example when running the trading-engine:

```bash
export BALANCE_TEST_MXN=50000
# then start trading-engine (e.g. in Docker, K8s, or locally)
```

The metric is set as soon as the metrics server is up (and again during engine init if Bitso balance fetch fails but test vars are set). Ensure Prometheus is scraping the trading-engine’s `/metrics` and that the dashboard’s time range includes the period when the service was running.
