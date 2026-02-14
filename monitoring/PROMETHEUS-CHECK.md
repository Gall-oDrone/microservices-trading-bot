# Checking Prometheus Logs and Scrape Status

## 1. View Prometheus logs (Kubernetes)

```bash
# List Prometheus pod(s)
kubectl get pods -n monitoring -l app.kubernetes.io/name=prometheus

# Stream logs (main Prometheus container)
kubectl logs -n monitoring prometheus-kube-prometheus-stack-prometheus-0 -c prometheus -f --tail=100
```

**What to look for:**
- `msg="Completed loading of configuration file"` – config loaded OK
- `discovery=kubernetes config=serviceMonitor/monitoring/...` – ServiceMonitors discovered
- `level=error` or `Error scraping` – scrape failures (also visible in Targets UI)
- `v1 Endpoints is deprecated` – harmless k8s client warning, can ignore

## 2. View Prometheus logs (Docker Compose)

```bash
docker compose logs prometheus -f --tail=100
```

## 3. Open Prometheus UI and check Targets

Port-forward then open the UI:

```bash
kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090
# Browser: http://localhost:9090
```

- Go to **Status → Targets** (or http://localhost:9090/targets).
- **UP** = Prometheus is scraping successfully.
- **DOWN** = check the **Last Error** column (e.g. connection refused, 404).

## 4. Quick target summary from the API

From a machine that can `kubectl exec` into the cluster:

```bash
kubectl exec -n monitoring prometheus-kube-prometheus-stack-prometheus-0 -c prometheus -- \
  wget -qO- 'http://127.0.0.1:9090/api/v1/targets' | \
  jq -r '.data.activeTargets[] | "\(.labels.job)\t\(.health)\t\(.lastError // "")"' | sort -u
```

---

## Last run summary (example)

When this was last run, targets looked like this:

| Job                     | Health | Last Error |
|-------------------------|--------|------------|
| api-gateway             | up     | —          |
| backtesting             | up     | —          |
| order-management        | up     | —          |
| strategy-executor       | up     | —          |
| **market-data**         | **down** | server returned HTTP status 404 Not Found |
| **trading-engine**      | **down** | connection refused (dial tcp ... :8080) |

- **trading-engine DOWN** → `bitso_available_balance` (Available Balance in Grafana) will have no data.
  - **Fix applied:** trading-engine now stays up in "metrics-only" mode when `BALANCE_TEST_*` env vars are set even if engine init fails (e.g. Redis/Kafka/Bitso). Set e.g. `BALANCE_TEST_MXN=50000` in the deployment so the pod listens on 8080 and serves `/metrics`.
- **market-data DOWN (404)** → market-data metrics missing.
  - **Fix applied:** market-data HTTP server now registers `/metrics` (Prometheus handler). Rebuild and redeploy the market-data image so the new binary is used.

**Grafana shows no data even when targets are UP:** If Prometheus is served with `routePrefix: /prometheus/` (e.g. ALB path `/prometheus`), Grafana’s Prometheus datasource URL must include the path: `http://kube-prometheus-stack-prometheus.monitoring:9090/prometheus`. Otherwise Grafana gets 404 on API calls and dashboards are empty. See GRAFANA-DASHBOARD-TROUBLESHOOTING.md.

After rebuilding and redeploying both services, re-run the `jq` command above (or check the Targets page) to confirm targets go **UP**.
