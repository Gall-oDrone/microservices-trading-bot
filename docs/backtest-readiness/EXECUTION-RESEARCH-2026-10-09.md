# Execution research: maker windows, repricing and slicing (2026-10-09)

**Question.** The stage executor's first btc_mxn leg cost 118 bps against the 70 bps the
pre-registration assumes (plan §6.5). Is that the execution schedule, or the stage book? And how
large can a leg get before the registered cost stops holding?

**Verdict.**

- **The schedule is not the problem.** On the production book, today's schedule costs 66.5 bps per
  btc_mxn leg and 32.7 bps per btc_usd leg at stage size (0.001 BTC). That is inside the primary
  70 / 40 bps. Today's schedule is one post-only order at the best price for 60 minutes, then a
  market order. The 118 bps leg reflects stage's thin book, not the schedule.
- **Re-pegging every 5 minutes is the best candidate change.** It keeps the same 60-minute window
  and re-pegs the resting price to the current best bid or ask.
  - btc_mxn: the maker share rises from 84 % to 98 % and the mean falls from 66.5 to 64.1 bps.
  - btc_mxn tail: the p90 falls from 89.7 to 77.7 bps, and the tracking error against the paper
    price falls from ±22 to ±15 bps.
  - btc_usd: every leg fills as a maker, with mean 32.3 and p90 39.5 bps.
  - **It changes execution, so it needs its own pre-registration.** Nothing was changed.
- **Capacity under today's schedule is about 0.01 BTC per leg on btc_mxn.**
  - At 0.1 BTC, today's schedule averages 90.1 bps, above even the pessimistic 88 bps.
  - The best schedule studied averages 81.6 bps at 0.1 BTC, with a p90 of 151 bps.
  - This agrees with the capacity page's 0.02–0.1 BTC (§6.4.12).
- **Start time.** 06:01 and 06:15 UTC cost the same. Waiting until 14:00 UTC barely changes the
  cost, but the tracking error against the paper account's open grows to about ±150 bps (btc_mxn).
  The executor should stay close to the open.

Reporting only. The live executor, the frozen costs and the rule are unchanged.

---

## 1. Method

`strategy-executor/cmd/exec-research` (plan §6.4.13) replays execution schedules against Bitso's
production public trades from the collector archive (`trades_compacted/`, read-only list and get):

- btc_mxn: 2026-08-19 → 2026-10-08, 111,331 trades.
- btc_usd: 2026-09-30 → 2026-10-08, 19,804 trades.

The simulator is `internal/execsim`.

| Element | Model |
|---|---|
| Day | Bitso's candle day: opens at Mexico City midnight (06:00 UTC). "vs open" is the cost against the paper account's price. |
| Legs | Every archived day, a buy and a sell, at each start time (06:01, 06:15 = today, 14:00 UTC). |
| Best bid / ask | The last print whose maker was a buyer (bid) or seller (ask), if under 10 minutes old; else the last trade ± an assumed half-spread (1.5 / 1.3 bps). |
| Resting order | Post-only at the best bid (buy) or ask (sell). **Through**: filled by trades strictly past it (conservative: the whole queue ahead traded first). **Touch**: filled by trades at it (optimistic). Fills are capped by the traded amount. |
| Market order | The ask (buy) or bid (sell) plus square-root impact Y·σ·√(q/ADV), with Y = 1 (the conservative end), ADV and σ from the archive (btc_mxn 8.89 BTC, 2.15 %). |
| Fees | The registered ones: maker 60 / 30 bps, taker 78 / 36 bps. |
| Cost | Implementation shortfall against the arrival price (the last trade at the start), fees included. |
| Schedules | Market now; maker for 15m–8h, then market; the same with a 5-minute re-peg; TWAP 4 child orders over the window, each resting a quarter and then market for its remainder. |
| Skipped days | Any gap over 30 minutes in the archive between the open and the end of the longest window: 9 btc_mxn days, 1 btc_usd day. |

Evidence: [`exec-research.md`](evidence-2026-10-09/exec-research.md) (all tables) and
[`exec-research.json`](evidence-2026-10-09/exec-research.json). To reproduce:

```bash
cd services/strategy-executor
go run ./cmd/exec-research -bucket mtb-development-data-archive-326105557351 -to 2026-10-08 \
  -out-json exec.json -out-md exec.md
```

## 2. Results at stage size (0.001 BTC, start 06:15 UTC, Through model)

| | btc_mxn mean | p90 | maker share | vs open ± sd | btc_usd mean | p90 | maker share |
|---|---:|---:|---:|---:|---:|---:|---:|
| Pre-registered primary / pessimistic | 70 / 88 | | | | 40 / 46 | | |
| Market now | 80.9 | 81.8 | 0 % | ± 12 | 38.2 | 38.3 | 0 % |
| **Maker 1h, then market (today)** | **66.5** | 89.7 | 84 % | ± 22 | **32.7** | 39.9 | 79 % |
| Maker 4h | 67.4 | 77.0 | 90 % | ± 30 | 35.2 | 48.5 | 85 % |
| Maker 30m, repriced 5m | 66.7 | 84.6 | 84 % | ± 16 | 32.3 | 39.5 | 94 % |
| **Maker 1h, repriced 5m** | **64.1** | **77.7** | 98 % | ± 15 | **32.3** | 39.5 | 100 % |
| Maker 4h, repriced 5m | 63.7 | 77.3 | 100 % | ± 15 | 32.3 | 39.5 | 100 % |
| TWAP 4 × 1h (4h window) | 73.0 | 125.1 | 74 % | ± 46 | 33.8 | 74.8 | 84 % |

How to read it:

- A leg filled as a maker at the arrival price costs exactly the maker fee: 60 / 30 bps, the
  median of most rows.
- A leg that falls back to market costs the taker fee, plus the drift while it rested, plus the
  spread.
- Waiting longer raises the fill rate but adds drift. A market that runs away from a static order
  is the expensive case, and re-pegging removes most of it.
- At stage size the Through and Touch results differ by under 3 bps (btc_mxn) and 0.5 bps
  (btc_usd), so the unknown queue position does not change the ranking. On btc_mxn the gap grows
  to 10–12 bps at 0.1–0.5 BTC: the more volume an order needs, the more its place in the queue
  matters.

## 3. Larger legs (start 06:15 UTC, Through model, mean bps per leg)

| Size | btc_mxn market | today | best schedule (mean, p90) | btc_usd market | today | best schedule (mean, p90) |
|---|---:|---:|---|---:|---:|---|
| 0.001 BTC | 80.9 | 66.5 | maker 4h repriced: 63.7, 77.3 | 38.2 | 32.7 | maker 30m repriced: 32.3, 39.5 |
| 0.01 BTC | 85.8 | 72.7 | maker 8h repriced: 65.3, 85.8 | 41.2 | 35.4 | maker 1h repriced: 33.3, 42.0 |
| 0.1 BTC | 101.4 | 90.1 | maker 8h repriced: 72.5, 131.6 | 50.6 | 44.0 | TWAP 4 × 1h (4h): 38.6, 93.6 |
| 0.5 BTC | 129.6 | 120.5 | maker 8h repriced: 90.6, 199.0 | 67.6 | 59.4 | maker 8h repriced: 43.5, 97.4 |

At 0.1 BTC on btc_mxn only about a third fills as a maker within an hour (the traded amount
through the bid is the limit). The rest pays the taker fee plus about 23 bps of square-root
impact. The longest repriced windows lower the mean, but the p90 above 130 bps and the ±49 bps
tracking error against the paper price show that they trade cost for variance. **Sizing beyond
about 0.01 BTC per btc_mxn leg needs a different execution design, studied with more data,
before any pre-registration.**

## 4. Limits

- **About 40 btc_mxn days and 7 btc_usd days.** Means move by a few bps from one week to the next.
  Re-run monthly; the archive grows every hour.
- **Production prints, not stage.** Stage fills are thinner, as the first leg showed.
- **The book is inferred from prints.** The hourly book sampler (§6.4.13) now measures the real
  spread distribution, so the half-spread fallback can be checked.
- **Impact uses Y = 1** and ignores the order's own effect on later prints.
- **No hidden liquidity or iceberg orders.** Bitso's public trades do not show them.

## 5. Next step, if wanted

Write a pre-registration for "maker 60 minutes, re-peg every 5 minutes, then market". It should
give the hypothesis (the mean cost per leg is not higher than today's schedule's, measured on the
stage ledger), the window, and the decision rule. Only after it is committed should the executor
gain an option for the re-peg. The SMA50 forward tests keep their frozen costs either way.
