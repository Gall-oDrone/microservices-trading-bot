# Backtesting dashboard – runbook for future runnings

This document describes how to get the **Backtesting** Grafana dashboard to show non-zero values (Backtests created, Backtests completed, Events processed, etc.) and how to verify the full path from backtesting service → Prometheus → Grafana. Use it for future runs after deployment or when the dashboard shows 0.

---

## 1. Why metrics show 0

The dashboard shows data from **whatever Prometheus Grafana’s datasource points to**. That Prometheus only has metrics from the **backtesting instance(s) it scrapes**.

| Where you view Grafana | Which Prometheus it uses | Which backtesting it scrapes |
|------------------------|--------------------------|------------------------------|
| **Docker** (`http://localhost:3000`) | Docker Prometheus (`http://prometheus:9090`) | Docker backtesting container (`backtesting:8084`) |
| **Kubernetes** (cluster Grafana) | Cluster Prometheus (e.g. `kube-prometheus-stack-prometheus:9090`) | Cluster backtesting pods (ServiceMonitor) |

If you run backtests only against **Docker** backtesting (`http://localhost:8084`) but view **cluster** Grafana, the cluster Prometheus has no data from that Docker container, so the dashboard shows 0. To see non-zero values in **cluster** Grafana, you must run at least one backtest against the **cluster** backtesting API.

---

## 2. One-shot: run all recommended actions

From the repo root:

```bash
# Docker Compose (Grafana at localhost:3000, backtesting at 8084, Prometheus at 9090)
./scripts/backtesting-dashboard-recommended-actions.sh

# Kubernetes (script port-forwards Prometheus and backtesting, then verifies and runs one backtest)
./scripts/backtesting-dashboard-recommended-actions.sh --k8s

# Custom URLs (e.g. you already have port-forwards in other terminals)
PROMETHEUS_URL=http://localhost:9091 BACKTEST_URL=http://localhost:8085 ./scripts/backtesting-dashboard-recommended-actions.sh
```

The script will:

1. **Verify** – Check backtesting `/metrics` and Prometheus query; print whether Prometheus has backtesting data.
2. **Run one backtest** – If Prometheus has no data (or backtesting has 0 created), run `run-one-backtest.sh` against the given Backtesting URL so the scraped instance gets metrics.
3. **Print Grafana checks** – Remind you to check datasource URL, time range, and refresh the dashboard.

---

## 3. Step-by-step for future runnings

### Option A: Docker Compose

1. Start the stack (including backtesting and Prometheus):
   ```bash
   docker compose up -d redis kafka market-data backtesting prometheus grafana
   # Or: ./scripts/start-backtest-with-stage.sh
   ```
2. Run at least one backtest against Docker backtesting:
   ```bash
   ./scripts/run-one-backtest.sh http://localhost:8084
   ```
3. Open Grafana at `http://localhost:3000`, go to the **Backtesting** dashboard (folder **services**), set time range e.g. **Last 1 hour**, refresh. You should see non-zero values for Backtests created, Backtests completed, and related panels.

### Option B: Kubernetes (cluster Grafana)

1. Ensure backtesting and Prometheus are running in the cluster (e.g. after deployment). ServiceMonitor for backtesting must be applied so cluster Prometheus scrapes the backtesting pods.
2. Port-forward the cluster backtesting service (so you can call it from your machine):
   ```bash
   kubectl port-forward -n bitso-trading-dev svc/backtesting 8085:8084
   ```
   Leave this running in a terminal.
3. In another terminal, run one backtest against the cluster backtesting:
   ```bash
   ./scripts/run-one-backtest.sh http://localhost:8085
   ```
4. Wait ~15–30 seconds for Prometheus to perform its next scrape (e.g. 15s interval).
5. Open **cluster** Grafana (e.g. via ingress or `kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80`). Go to the **Backtesting** dashboard, set time range **Last 1 hour**, refresh. You should see non-zero values.

---

## 4. Verify only (no backtest)

To confirm whether a given Prometheus has backtesting metrics (e.g. after changing environment or datasource):

```bash
# Default: backtesting at localhost:8084, Prometheus at localhost:9090
./scripts/verify-backtesting-prometheus.sh

# After port-forwarding cluster Prometheus to 9091
PROMETHEUS_URL=http://localhost:9091 ./scripts/verify-backtesting-prometheus.sh

# After port-forwarding cluster backtesting to 8085
BACKTEST_URL=http://localhost:8085 PROMETHEUS_URL=http://localhost:9091 ./scripts/verify-backtesting-prometheus.sh
```

The script prints: (1) what the backtesting service exposes, (2) Prometheus backtesting target health, (3) the result of `sum(backtesting_backtests_created_total)`, (4) a short conclusion and fix hints.

---

## 5. Grafana datasource check

If Prometheus has the data but the dashboard still shows 0:

- In Grafana: **Connections** → **Data sources** → **Prometheus**.
- **URL** must be the Prometheus that scrapes the backtesting instance you ran backtests against:
  - Docker: `http://prometheus:9090`
  - Kubernetes: `http://kube-prometheus-stack-prometheus:9090` (or with path prefix, e.g. `.../prometheus`, if configured).
- **Save & test**.
- Ensure the Backtesting dashboard panels use a datasource with **uid** `prometheus` (provisioned datasource usually has this).

---

## 6. Related docs

- **BACKTESTING-DASHBOARD-METRICS.md** – Panel list, metric names, and “why 0 / why only Scrape and Health”.
- **monitoring/grafana/GRAFANA-DASHBOARD-TROUBLESHOOTING.md** – Root cause, verification script, and recommended-actions script usage.
