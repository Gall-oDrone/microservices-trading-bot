# eToro demo evidence: 2026-10-10 (porting plan, phase P1)

Captured with `services/strategy-executor/cmd/etoro-spike` against the eToro Public API demo (Virtual) account. Command:

```
set -a; . ./.env.etoro.local; set +a      # git-ignored, never committed
cd services/strategy-executor
go run ./cmd/etoro-spike -out ../../docs/etoro/evidence-2026-10-10 -roundtrip -roundtrip-symbol NSDQ100 -amount 1000
```

The files are verbatim tool output. `SHA256SUMS` covers every file, including this README. No credentials or account ids are recorded; eToro error texts are redacted (`CID <redacted>`).

> [!WARNING]
> Captured on a **Saturday, 02:05–02:16 UTC**. The index CFDs were quoting their weekend session. Weekday cash-session spreads have still to be measured: re-run the spike on a weekday between 14:00 and 20:00 UTC before freezing the cost model (P2).

## 1. Instruments (`instruments.json`)

| Symbol | instrumentId | Class / exchange | Role |
|---|---|---|---|
| NSDQ100 | **28** | Indices / CFD ("NASDAQ100 Index (Non Expiry)") | traded |
| SPX500 | **27** | Indices / CFD ("SPX500 Index (Non Expiry)") | traded |
| QQQ | 3006 | ETF / Nasdaq | benchmark |
| SPY | 3000 | ETF / NYSE | benchmark |

- The search route matches loosely. NSDQ100 also returns `NSDQ100.24-7` (686) and `NSDQ100.FUT` (255).
- The client resolves exact symbols only (`etoro.ResolveSymbol`).
- The search route's id field is `internalInstrumentId`. The previous client decoded `instrumentId`, which always came back as 0.

## 2. Eligibility for this account (`eligibility.json`)

| | NSDQ100 | SPX500 | QQQ / SPY |
|---|---|---|---|
| Long x1 | yes, settlement **cfd** | yes, **cfd** | yes, settlement **real** (the ETF itself) |
| Other long leverage | 2, 5, 10, 20 (SL ≤ 50 %) | 2, 5, 10, 20 | 2, 5, 10, 20 (CFD) |
| Short | 1, 2, 5, 10, 20 (CFD, SL ≤ 50 %) | same | same (CFD) |
| Min position exposure | **1000 USD** | **1000 USD** | 10 USD |
| Max units per order | 35 | 130 | 1406 / 1305 |

## 3. Costs at x1 for 1,000 USD (`costs.json`, `rates.json`)

These are weekend quotes; see the warning above.

| | Spread cost per open | Overnight fee | Transaction fee |
|---|---|---|---|
| NSDQ100 | 1.55 USD (15.5 bps; earlier runs that night 1.49–1.52) | 0.23 USD/night = **2.3 bps/night ≈ 8.4 %/yr** (365 nights) | 0 |
| SPX500 | 1.48 USD (14.8 bps) | 0.23 USD/night ≈ 8.4 %/yr | 0 |
| QQQ | 0.01 USD (0.1 bps) | 0 | **1.50 USD per order** |
| SPY | 0.03 USD (0.3 bps) | 0 | 1.50 USD per order |

- The executed round trip confirms that the quoted spread is paid once per round trip. Example: opened at 30970, closed at 30921, `netProfit` −1.58 USD on 999.99 USD.
- `openingData` also reports a markup of 0.06–0.15 USD.

> [!IMPORTANT]
> SMA50 holds positions for weeks, so financing dominates the cost: about 20 trading days ≈ 28 nights ≈ 64 bps, against a one-off 15 bps spread. An x1 QQQ/SPY holding is a real ETF with no overnight fee and 2 × 1.50 USD of fees per round trip (30 bps on 1,000 USD, 6 bps on 5,000 USD). This is why the pre-registration (P2) must benchmark the CFD against QQQ/SPY x1.

## 4. Daily bars (`daily_*.csv`, `daily_*_report.json`)

- The live route `…/history/candles/desc/OneDay/1000` buckets **UTC days**: bar D closes at D+1 00:00Z, i.e. 20:00 New York time (EDT).
- It returns 1000 bars, back to 2022-12-19 (NSDQ100) and 2022-12-13 (SPX500).
- After dropping the in-progress bucket, weekends and NYSE holidays, **every NYSE trading day is present**: 0 missing, with 955 and 959 bars.
- Dropped bars:
  - NYSE holidays where the CFD still printed a bar: 30. The underlying futures trade short sessions on those days, e.g. Good Friday, July 4, Thanksgiving.
  - Weekend bars: 14 and 10. They begin on **2026-08-22**: eToro now quotes these CFDs at weekends, at synthetic prices.
- The CSVs use the research format (`bitsodaily.WriteCSV`) that daily-research and the Monte Carlo tools read.

## 5. Hourly coverage (`hourly_*_coverage.json`)

The last 1000 hourly bars include about 120–150 Saturday and Sunday bars. Weekend quoting is continuous, not occasional.

## 6. DataPlatform history route (`history_*_coverage.json`)

- `/api/v1/data/instruments/{id}/candles?interval=1d` has data from **2018-12-23/27**.
- Its daily bars are futures-style sessions opening at 21:00/22:00 UTC (17:00 New York time).
- It **lags the market by about 12 days**: the last bar was 2026-09-27 at capture time.
- Use it for research history only, never for a live decision.

## 7. Order behaviour (`roundtrip.json`, via `broker/etorobroker`)

| Step | What happened |
|---|---|
| Open | `POST /api/v2/trading/execution/demo/orders` (x1, `amount` 1000) returned `orderId` and `referenceId` (= the sent `x-request-id`). `orders:lookup?orderId=` returned status **`{id 3, name "Filled"}`**, `asset.settlementType "CFD"`, and a fill of 0.032289 units at 30970 in `positionExecutions[0].openingData`. |
| Quirks | `investedAmountCurrency` reads **1**, so the amount comes from `marginAccountCurrency` (999.99). `stopLossRate` defaults to 0.01, which means no stop. |
| Reference lookup | `orders:lookup?referenceId=` answers **404 "No external operation was found"** for API-placed orders (checked over 40 s in a separate diagnostic). |
| Re-open, same `x-request-id` | **400 "ReferenceID … may already exists for CID … and OrderID …"**, so eToro de-duplicates request ids. The adapter recognises this and resolves it to the first position: same position id, no second position. |
| Portfolio | `/api/v1/trading/info/demo/pnl` shows a new position, or drops a closed one, **1–3 s late**. A position's `unrealizedPnL` is an **object** (`{pnL, exposureInAccountCurrency, …}`), not a number. |
| Close | v2 does not support `action: close` yet, so closes use `POST /api/v1/trading/execution/demo/market-close-orders/positions/{id}`. `close-orders/{orderId}` returned `statusID 3`, proceeds and the fill rate. |
| Re-close | Accepted, then the close order carries **errorCode 741** ("requested position is already closed"). Mapped to `broker.ErrPositionNotOpen`. |
| History | `trade/demo/history` lists the closed trade a few seconds later (`netProfit` excludes `fees`). |

Demo account: 100,000 USD virtual. About 6 USD was spent on spreads across three round trips and one diagnostic. The account was confirmed flat afterwards.

## 8. Consequences for the next phases

1. **Bars (P2 pre-registration):** use one bar per NYSE trading day from the live UTC-day route. Long history comes from Yahoo `^NDX`/`^GSPC`, cross-checked against the overlapping eToro bars.
2. **Execution timing (P3):** act only inside the NYSE cash session on NYSE trading days (`mktcal`). Never at weekends, when prices are synthetic and spreads possibly wider.
3. **Idempotency (P3):**
   - The ledger stores `client_ref` and `intent_at` **before** sending.
   - A re-run calls `Open` again with the same reference.
   - Closes are safe to repeat (741 means "already closed").
   - Reconciliation reads `orders:lookup?orderId=` and trading history, and treats the portfolio as eventually consistent.
4. **Size:** the minimum exposure is 1000 USD, and a 1000 USD order executed as 999.99. Use **≥ 1,100 USD** per instrument (the plan's 1,000 USD default sits on the minimum).
5. **Costs (P2):** model about 15 bps spread per round trip plus 2.3 bps per held night, with ±50 % sensitivity, after a weekday re-measure.
