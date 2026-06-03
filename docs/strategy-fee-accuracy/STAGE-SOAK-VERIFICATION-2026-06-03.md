# Stage Soak Verification Report

Date: 2026-06-03  
Repository: `microservices-trading-bot`  
Related: [`STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md`](STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md), [`STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md`](STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md)

Operator verification run after the bash soak leg exceeded **24 hours**. ATR/`/api/v1/bars` was implemented in-repo on 2026-06-03 (see market-data observations doc; deploy work may have been done from another session).

---

## Executive summary

| Gate | Status | Notes |
|------|--------|-------|
| Soak duration (≥ 24 h) | ✅ Met | Bash router ~1d 3h+ elapsed at verification time |
| `DRY_RUN=true` (Go router) | ✅ | Confirmed via `check` |
| Running strategies | ✅ | None (expected) |
| ATR + bars (market-data) | ✅ | `GET /api/v1/bars` returns candles; snapshot has non-null `atr` |
| Bash classifier self-consistency | ✅ | 1176/1176 comparable cycles (100%) |
| Bash ↔ Go regime agreement (24–48 h) | ⚠️ **Incomplete** | Go audit log wiped on `strategy-router` pod restart; use live samples going forward |
| Short soak **PASS** | ❌ **Not yet** | Need ≥ 99% bash↔Go on comparable cycles over aligned window |
| Extended 7-day gate | 📋 Pending | After short soak passes |

**Do not** set `strategy-router` `DRY_RUN=false` until bash↔Go agreement is demonstrated.

---

## What was run (2026-06-03)

```bash
./scripts/run-stage-soak-2026-06-02.sh check      # prerequisites + spot regime compare
./scripts/run-stage-soak-2026-06-02.sh report     # pull Go audit + bash log analysis
./scripts/run-stage-soak-2026-06-02.sh sample     # live paired samples (run periodically)
```

New tooling:

| Script | Purpose |
|--------|---------|
| [`scripts/analyze-stage-soak-agreement.sh`](../../scripts/analyze-stage-soak-agreement.sh) | `report`, `sample`, `pull-go` |
| `tmp/stage-soak/soak-verification-report.json` | Machine-readable summary |
| `tmp/stage-soak/agreement-samples.jsonl` | Append-only live bash vs Go pairs |

---

## ATR / indicators (post–bars fix)

Verified in-cluster:

```bash
# Bars API (market-data :8083)
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- 'http://market-data:8083/api/v1/bars?book=btc_mxn&interval=1m&limit=3'

# Snapshot includes ATR after indicator refresh
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot | jq '{price:.bollinger.middle_band, atr:.atr.value}'
```

At verification: **ATR present** (~3138), price from `middle_band`, `check` reported `go_regime=low_vol_range bash_regime=low_vol_range`.

---

## Log analysis results

### Bash leg (`tmp/stage-soak/bash-router-soak.log`)

| Metric | Value |
|--------|-------|
| Log lines | ~7,921 |
| `snapshot fetch failed` | 123 |
| Comparable cycles (price > 0 in `regime inputs`) | 1,176 |
| Stated vs recomputed regime match | **100%** (1,176 / 1,176) |
| Regime distribution | `low_vol_range`: 1,176 (ATR% often 0 in log during pre-bars / cold periods) |

Bash classifier logic is internally consistent. Failures are mostly port-forward drops (see operator guide §8).

### Go leg (`/tmp/strategy-regime-router.log` in pod)

| Metric | Value |
|--------|-------|
| Lines at pull time | 14 (pod **restarted** 2026-06-03T22:43:26Z) |
| Comparable cycles (`Price` > 0) | 5 |
| Missing-price cycles | 9 |

**Critical:** Go audit is **ephemeral**. The prior ~3,300-line history from the first ~25 h soak was **lost** when `strategy-router` was redeployed. Offline bash↔Go agreement for that window **cannot** be reconstructed from Go JSONL alone.

### Live paired samples (`agreement-samples.jsonl`)

First verification batch: **3/3 match** (`low_vol_range`). Too few samples for the 99% gate; use ongoing collection (below).

---

## Code / script fixes applied (2026-06-03)

1. **`scripts/analyze-stage-soak-agreement.sh`** — `report`, `sample`, `pull-go`.
2. **`scripts/run-stage-soak-2026-06-02.sh`** — `report` and `sample` commands; `check` uses `bollinger.middle_band` for price.
3. **`scripts/strategy-regime-router.sh`** — price fallback `middle_band`; UTC timestamp prefix on regime log lines for future alignment.

---

## Verdict and next steps

### Short soak (24–48 h) — not declared PASS yet

Required:

```text
agreement_rate = matching_regimes / comparable_cycles ≥ 0.99
```

Because Go history was lost, use **live sample collection** for the bash↔Go gate:

```bash
# Every 30 minutes during soak (cron example)
*/30 * * * * cd /path/to/microservices-trading-bot && ./scripts/run-stage-soak-2026-06-02.sh sample >>/tmp/soak-sample-cron.log 2>&1

# Or manual spot checks
./scripts/run-stage-soak-2026-06-02.sh sample
./scripts/run-stage-soak-2026-06-02.sh report
```

Target: **≥ 288 samples** over 24 h at 30 min spacing (or denser sampling). Pass when `live_agreement_rate ≥ 0.99` in report output.

Optional: continue bash soak until **48 h** from `start-bash` for the upper bound of the short-soak window.

### After short soak PASS

1. `./scripts/run-stage-soak-2026-06-02.sh stop-bash`
2. Set `DRY_RUN=false` on **Go router only** (not trading-engine).
3. Fee-honesty spot-check on first live round-trip ([`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md)).
4. Maintain `DRY_RUN=true` logging for **≥ 7 days** (extended milestone gate).

### Operational hygiene

- **Pull Go audit before pod restarts:** `./scripts/analyze-stage-soak-agreement.sh pull-go`
- **Recover bash leg after executor restart:** `./scripts/run-stage-soak-2026-06-02.sh stop-bash && ./scripts/run-stage-soak-2026-06-02.sh start-bash`
- **Router health:** confirm `strategy_router_evaluation_errors_total` flat and evaluation rate ≈ 1/`ROUTER_INTERVAL_SEC` in Grafana.

---

## Related documents

- [`STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md`](STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md)
- [`STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md`](STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md)
- [`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md)
