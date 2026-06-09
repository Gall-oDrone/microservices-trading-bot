# Stage Soak Verification Report — Classification PASS

Date: **2026-06-07** (verification run)  
Repository: `microservices-trading-bot`  
Related: [`STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md`](STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md), [`STAGE-SOAK-VERIFICATION-2026-06-03.md`](STAGE-SOAK-VERIFICATION-2026-06-03.md) (superseded interim report), [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md)

---

## Timeline

| Milestone | Timestamp (UTC) |
|-----------|-----------------|
| Bar-first indicator deploy (fresh window) | 2026-06-05T00:59:42Z |
| Aligned agreement sampling begins | 2026-06-05T01:04:49Z |
| Short-soak time gate (≥ 24 h) met | 2026-06-06T00:59:42Z |
| Short-soak upper window (48 h) exceeded | 2026-06-07T00:59:42Z |
| **Verification / PASS declared** | **2026-06-07T22:45:08Z** |
| **Elapsed at verification** | **~69.8 hours** (~2.9 days) |
| **Current date** | **2026-06-07** |

Redeploy ref for this window: `194cdd55fb84062bca4e4aba696bae6e55159b1b` (bar-first indicators + aligned `POST /router/run` sampling).

Pre-window samples archived to `tmp/stage-soak/agreement-samples.pre-window-2026-06-05.jsonl`. Pre-align-fix samples archived to `tmp/stage-soak/agreement-samples.pre-align-fix-2026-06-05.jsonl`.

---

## Executive summary

| Gate | Status | Notes |
|------|--------|-------|
| Soak duration (≥ 24 h) | ✅ **Met** | ~69.8 h elapsed at verification |
| Short-soak window (24–48 h) | ✅ **Exceeded** | Continued sampling while green |
| `DRY_RUN=true` (Go router) | ✅ | Confirmed at verification |
| Running strategies | ✅ | None (expected during classification soak) |
| ATR + bars (market-data) | ✅ | Non-null ATR in snapshot (~0.23% at verification) |
| Bash classifier self-consistency | ✅ | 8,422/8,422 comparable cycles (100%) |
| Bash ↔ Go live agreement | ✅ | **140/140 samples (100%)** |
| Short soak **PASS** | ✅ **Declared PASS** | `live_agreement_rate = 1.0` |
| Extended 7-day classification gate | 📋 **In progress** | Short soak passed; continue periodic sampling |

**Proceed to Stage execution soak Phase 2** (router lifecycle, engine still dry). See [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md) and `./scripts/run-stage-execution-soak-2026-06-04.sh phase2-start`.

---

## What was run (2026-06-07)

```bash
./scripts/run-stage-soak-2026-06-02.sh status
./scripts/run-stage-soak-2026-06-02.sh report
./scripts/stage-soak-sample-loop.sh status
```

Machine-readable artifacts:

| File | Purpose |
|------|---------|
| `tmp/stage-soak/soak-window.json` | Window start + pass criterion |
| `tmp/stage-soak/soak-verification-report.json` | Full bash/Go audit + live agreement |
| `tmp/stage-soak/agreement-samples.jsonl` | 140 paired bash vs Go samples (in-window) |
| `tmp/stage-soak/go-router-audit.jsonl` | Pulled Go audit (8,574 lines) |

---

## Live agreement (primary gate)

| Metric | Value |
|--------|-------|
| Samples (in-window) | 140 |
| Matches | 140 |
| **Agreement rate** | **1.0000 (100%)** |
| Pass threshold | ≥ 0.99 |
| Sample interval | 30 min (`stage-soak-sample-loop.sh`) |
| First in-window sample | 2026-06-05T01:04:49Z |
| Last sample at verification | 2026-06-07T22:28:09Z |
| Mismatches | **None** |

In-window regime distribution (live samples): `low_vol_range` 129, `trending_up` 6, `trending_down` 2, `neutral` 3.

Report output:

```json
{
  "live_samples": 140,
  "live_matches": 140,
  "live_agreement_rate": 1.0,
  "live_pass_99pct": true
}
```

---

## Log analysis results

### Bash leg (`tmp/stage-soak/bash-router-soak.log`)

| Metric | Value |
|--------|-------|
| Log lines | 25,860 |
| `snapshot fetch failed` | 4 |
| Comparable cycles (price > 0) | 8,422 |
| Stated vs recomputed regime match | **100%** (8,422 / 8,422) |
| Regime distribution | `low_vol_range`: 8,268; `neutral`: 63; `trending_down`: 48; `trending_up`: 43 |

### Go leg (`/tmp/strategy-regime-router.log` in pod → `go-router-audit.jsonl`)

| Metric | Value |
|--------|-------|
| Audit lines | 8,574 |
| Comparable cycles (`Price` > 0) | 8,574 |
| Missing-price cycles | 0 |
| First timestamp | 2026-06-05T00:29:08.731758314Z |
| Last timestamp | 2026-06-07T22:45:08.816042617Z |
| Regime distribution | `low_vol_range`: 8,103; `trending_up`: 173; `trending_down`: 190; `neutral`: 106; `high_vol`: 2 |

Go audit spans the full window without pod restart data loss during this period.

### Cluster state at verification

- Go router: `dry_run=true`, regime `low_vol_range`, ATR ~0.23%
- Bash reference router + port-forward: running (stopped before execution soak Phase 2)
- Sample loop: running (PID 5845)
- Active strategies: `[]`

---

## Verdict

### Short soak (24–48 h) — **PASS**

```text
live_agreement_rate = 140 / 140 = 1.0 ≥ 0.99  ✅
elapsed_hours       = ~69.8 ≥ 24               ✅
```

### Next steps (execution soak)

1. ~~Stop classification bash leg~~ — done before Phase 2
2. ~~Start execution soak Phase 2~~ — **PASS** 2026-06-09 (26.2 h) — see [`STAGE-EXECUTION-SOAK-VERIFICATION-2026-06-09.md`](STAGE-EXECUTION-SOAK-VERIFICATION-2026-06-09.md)
3. **Phase 3 in progress** (started 2026-06-09T01:06:52Z) — `./scripts/run-stage-execution-soak-2026-06-04.sh phase3-status`
4. Await first Stage round-trip + fee honesty spot-check
5. Continue periodic `./scripts/run-stage-soak-2026-06-02.sh sample` during execution phases to detect classifier drift
6. Extended 7-day classification milestone: keep sampling until 2026-06-12+ while execution phases run

### Operational hygiene (unchanged)

- **Pull Go audit before pod restarts:** `./scripts/analyze-stage-soak-agreement.sh pull-go`
- **Router health:** confirm `strategy_router_evaluation_errors_total` flat in Grafana

---

## Related documents

- [`STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md`](STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md)
- [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md)
- [`STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md`](STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md)
- [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md)
