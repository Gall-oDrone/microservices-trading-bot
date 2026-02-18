# Prometheus: Trading Engine Available Balance

## What Prometheus scrapes

- **Metric name**: `bitso_available_balance`
- **Type**: Gauge (current value per label set)
- **Labels**: `currency` (e.g. `MXN`, `USD`), plus standard scrape labels: `job`, `instance`, etc.
- **Source**: trading-engine `/metrics` (ServiceMonitor, 15s interval)
- **How it’s set**:
  - **Normal**: Engine’s `fetchAndCacheBalances()` calls Bitso, then `RecordBalances(currencyToAvailable)`.
  - **Test**: If `BALANCE_TEST_MXN` (etc.) are set, main calls `RecordBalances(testBalances)` at startup; engine may also use test balances when Bitso fetch fails.

Example scraped series (with 2 replicas):

```text
bitso_available_balance{currency="MXN", job="trading-engine", instance="10.0.1.5:8080"}  50000
bitso_available_balance{currency="MXN", job="trading-engine", instance="10.0.1.6:8080"}  50000
bitso_available_balance{currency="USD", job="trading-engine", instance="10.0.1.5:8080"}  1000
bitso_available_balance{currency="USD", job="trading-engine", instance="10.0.1.6:8080"}  1000
```

So you get **one time series per (instance, currency)**. With **2 replicas** you see **two lines per currency** in a timeseries panel (e.g. “MXN” and “MXN” for each pod). That’s the “duplicate” data.

## Deduplicating in Grafana

Use one series per currency by aggregating over instances, e.g.:

- **One line per currency (recommended)**  
  `max by (currency) (bitso_available_balance{job=~"trading-engine.*"})`  
  (Both replicas should report the same balance; `max`/`avg`/`last` are equivalent for dedup.)

- **Raw (shows duplicates)**  
  `bitso_available_balance{job=~"trading-engine.*"}`  
  Keeps one line per instance per currency.

## Chart type

- **Available balance is a gauge**: “current balance per currency.” It only changes when the engine re-fetches (e.g. periodically or after trades).
- **Timeseries**: Good if you want “balance over time” (e.g. after executions). Use the aggregated query above so you don’t see duplicate lines per currency.
- **Stat**: Better if you only care about “current balance right now.” Use the same aggregated query; stat will show the latest value per currency (or a single value if you further aggregate).

Summary: the data Prometheus scrapes is correct; the duplicate lines come from multiple trading-engine replicas. Use `max by (currency)(...)` (or similar) in Grafana to get a single series per currency and the right chart type (timeseries for history, stat for current value).
