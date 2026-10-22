# Real-account canary — evidence (pre-flight 2026-10-22; day 0 pending)

Plan: [ETORO-REAL-CANARY-PLAN-2026-10-21.md](../ETORO-REAL-CANARY-PLAN-2026-10-21.md) (decision B:
NSDQ100 x20, 50 USD invested → 1,000 USD exposure). Procedure: runbook §14. Everything in this
folder was read from the real account by `etoro-canary-observe -capture` (read-only; eligibility,
cost preview at the canary size, rates, account) and is pinned by `SHA256SUMS`. **No order has been
placed.** The measurement table below is filled in as the window runs.

## Pre-flight facts (2026-10-22, 17:44 UTC)

| Item | Value | Source |
|---|---|---|
| Funding | 60 USD deposited, converted from MXN at eToro's **0.0518 USD/MXN** (≈ 1,158.3 MXN) | operator |
| Account at capture | credit (cash) **62.78 USD**, equity 800.49, unrealised −149.79, **5 open positions**, 0 pending | `account.json` |
| Pre-existing holdings | five long-term positions on instruments 6168 (two) and 1137 (three), ≈ 749 USD exposure, opened 2021-12 … 2026-10; outside the canary (**D7**) | `observations.jsonl` 17:22 UTC |
| Eligibility NSDQ100 (28) | `cfd long` leverage [1] and [2, 5, 10, 20] (x20 allowed; **SL ≤ 50 %** of invested at leverage > 1, min 1 %); `minPositionExposure` **1,000 USD**; `maxUnitsPerOrder` 1000 (demo capture of 2026-10-10 said 35); x30/50/100 listed as *potential*; W-8BEN not required | `eligibility.json` |
| Cost preview, 50 USD x20 (stop 28324.72) | **overnight 0.24 USD** per night on 1,000 exposure = **2.4 bps** (frozen model 2.3; demo quoted 0.23); **transaction fee 0.07 USD** (demo quoted 0); spread 0.02; markup 0; no weekend line | `costs.json` |
| Rates at capture | NSDQ100 bid 29035.2 / ask 29036.1 (≈ 0.3 bps) | `rates.json` |
| Observer series | first line 17:20 UTC (401: the env file's two keys were swapped, fixed), 17:22 and 17:44 UTC clean | `../../services/strategy-executor/etoro-daily-data/real-canary/observations.jsonl` (S3 copy under `daily-executor/etoro-real-canary`) |

## Measured vs preview vs frozen (to be filled)

| Line item | Frozen (pre-registration) | Preview (day 0) | Measured | Notes |
|---|---|---|---|---|
| Overnight financing per night, 1,000 USD exposure | 0.23 USD (2.3 bps) | 0.24 USD (2.4 bps) | — | M1; the per-night table of `etoro-fees-check -real-canary` |
| Weekend charge (which night, how much) | 3 × nightly | not quoted | — | M2 |
| Transaction fee per order | 0 (CFD) | 0.07 USD | — | new vs demo |
| Spread at open / close (fill vs mid) | 15.5 bps round trip | 0.02 USD quoted | — | M3 |
| History lag after a close | — | — | — | M4 |
| Stop-out mechanics | — | — | — | M6, if it happens |

## Notes

- The account was not empty when funded (five long-term holdings); the plan's "the account holds
  nothing but the canary" became D7 (per-instrument rule). The balance is therefore not the last
  line of defence; the policy caps are (invested ≤ 60, exposure ≤ 1,200, one order per day, one
  NSDQ100 position, stop 2.3–2.45 %).
- The history already lists hand-placed NSDQ100 trades from before the canary; the observer's
  `history_new` is keyed by position id, so they appear once and are not the canary's.
