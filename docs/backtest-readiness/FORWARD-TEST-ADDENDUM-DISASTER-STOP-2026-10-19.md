# Forward-Test Addendum — Disaster Stop on the Demo Executor's Rule Positions

Date: **2026-10-19** · Branch `feat/etoro` · Applies to the forward test registered in
[`FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md`](FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md)
· Design: [`docs/etoro/ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md`](../etoro/ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md)
· Operator view: [`ETORO-DAILY-EXECUTOR.md §9`](../etoro/ETORO-DAILY-EXECUTOR.md#9-protection-stop-loss--take-profit-console-2026-10-19-executor-phase-c-built-2026-10-19)

> [!IMPORTANT]
> **This addendum changes nothing in the pre-registration's §1–§4.** The rule, the window, the
> costs and the hypotheses are as frozen. The paper account the test is judged on **never has a
> stop**: it is long while the close is above its SMA50 and flat otherwise, filled at the next
> bar's open, with the frozen costs. What changes is the **demo account** the P3 executor runs
> next to the paper test, and the pre-registration already says (§5) that the demo's fills are
> reported and do not decide.

## 1. What is added, and where it lives

From the phase C binaries on, the demo executor sends a resting **stop loss** with every rule open
and raises it daily:

| Item | Value | Source |
|---|---|---|
| Stop at open | `max(4 × ATR(14)%, 8 %)` below the ask, through the shared guards (noise floor, 10 % policy cap, 0.5 % of equity, venue bounds, tick), **no take profit** | `shared/pkg/stops.DisasterStopPct`, `cmd/etoro-daily-executor/protect.go` |
| Daily ratchet | at each 09:35 run holding a long with no order planned: `candidate = decision close − 4 × ATR`; sent only if ≥ 0.25 × ATR above the held stop; never lowered | same |
| Where it rests | at eToro, on the position (`stopLossRate`, `stopLossType fixed`); it survives our downtime and covers the overnight gap | `PATCH /api/v2/trading/demo/positions/{id}` |
| Switch | `-protect` (default on), `ETORO_EXECUTOR_PROTECT=0` off | executor flag |
| Policy | `EtoroDemoProtectedPolicy` (`etoro-demo-2026-10-19`: `stop_loss_required`, max 10 %, max 0.5 % of equity) replaces `etoro-demo-2026-10-10` as the executor's default | `shared/pkg/risk` |

At the ATR of 2026-10-19 (NSDQ100 1.75 %, SPX500 0.9 %) the 8 % floor decides on both: about
**88 USD at risk per 1,100 USD leg**, roughly 11 × the frozen round-trip cost.

This is a **risk-policy overlay on the demo account**, in the same family as the halt file, the
one-leg-per-day cap and the price-deviation guard the executor already applies: it bounds what a
single demo position can lose if the executor is down or the market gaps, and nothing more. It is
not part of the rule and it is not a strategy change.

## 2. Why it does not touch the judged series

- **The paper account is computed from bars alone** (`shared/pkg/cfdsim` through the executor's
  `paper` block and `cmd/index-research -forward`). It knows nothing about the demo account's
  positions, stops or fills. H1–H3 are evaluated on it at 2027-10-12 exactly as registered.
- **The demo account was never the judged series.** Its fills are reported "next to the paper
  result" (§5 execution note) to turn assumed costs into measured ones. A stop that fires makes the
  demo diverge from paper until the rule's next signal, which is reported as tracking (risk report
  §4, `tracking_bps`), the same way the one-day-early demo open of 2026-10-12 already is.
- The stop sits **4 ATR (or 8 %) away**. The rule's own exit is a close under the SMA50, which on
  these instruments is typically 1–3 % below the price when long. In almost every path the rule
  closes long before the stop is reached; the stop is for the path where the rule cannot act
  (a gap, a run that did not happen, a multi-day outage).

## 3. How a stop-out is reported

When eToro closes a rule position at the stop:

1. The next 09:35 run finds the ledger long and the account flat, reads the trading history and
   records the day as **`demo.action: venue_close`**, `kind: stopped_out`, with the close rate, the
   gap through the stop in bps, eToro's net profit and fees (C3). The **paper block of the same
   line is the rule's**, unchanged: paper stays long if the close is still above the SMA50.
2. **The demo does not re-enter in that run.** If the rule still says long, the next trading day's
   run opens again from flat, with a fresh stop. The demo is therefore flat for at least one
   session the paper account is long; the tracking difference says so.
3. The weekly risk report lists the stop-out with the **realised loss next to the loss the stop
   modelled** (`units × (open − stop)`), and the worst gap seen (section 6, M15). The forward-test
   execution reports (the first is
   [`FORWARD-TEST-RESULTS-SMA50-INDEX-CFD-2026-10-19.md`](FORWARD-TEST-RESULTS-SMA50-INDEX-CFD-2026-10-19.md))
   carry every stop-out as a dated row: demo fill, paper position that day, gap, cost.
4. A stop-out is **not** a reason to stop the test (§2: no early stopping on results). It is an
   execution event on the demo side.

If the number of stop-outs over the window is material (say, more than one per instrument per
quarter), the evaluation at 2027-10-12 reports the demo equity **with and without** the stops next
to the paper equity, so the reader can see what the overlay cost or saved. The paper equity still
decides.

## 4. What is not covered, deliberately

- **No take profit on rule positions.** The rule's exit is its signal; a target would be a
  strategy change.
- **No intraday ratchet, no local close-at-market loop.** Protection rests at the venue; the
  executor touches it once a day. That is the plan's decision ("where protection lives").
- **Console positions** (assisted trades, runbook §8) carry the operator's chosen profile and are
  ratcheted with that profile's multiple; a custom stop is left alone. They are outside the paper
  account entirely.
- **Dividend adjustments and financing** are as the pre-registration states them (§3): not
  modelled / frozen at 2.3 bps per night; the execution report of 2026-10-19 notes that no overnight
  fee was visible in the demo cash and leaves it pending.

## 5. Evidence to collect (plan phase D)

Under `docs/etoro/evidence-<date>/` when each first happens on the demo account, with
`scripts/check-no-etoro-secrets.sh` run before committing:

- the first rule open that carried a stop: the order request with `stopLossRate`, the position as
  the portfolio shows it (`stopLossRate`, `stopLossType`), the ledger line;
- the first ratchet: the PATCH request and the 202 with `operationId`, the position read back, the
  `protect` ledger line;
- the first stop-out: the trading-history row, the `venue_close` ledger line, the reconcile report
  of that day, the alert e-mail (`etoro.stop_out`), the risk report row.

Until the binaries on the ops host are rebuilt (runbook §9.6) none of this is in force on the live
demo ledger: the executor that ran on 2026-10-19 is the 2026-10-16 build and opens without a stop.
