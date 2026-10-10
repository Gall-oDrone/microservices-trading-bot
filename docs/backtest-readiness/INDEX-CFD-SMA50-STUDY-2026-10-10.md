# Index-CFD SMA50 Study: NSDQ100 and SPX500 on eToro, against holding the CFD and holding QQQ/SPY

Date: **2026-10-10** · Branch: `feat/etoro` · Porting plan phase **P2**
Evidence: [`evidence-2026-10-10/`](evidence-2026-10-10/) (`report.txt`, `results.json`, input data with SHA-256 in `results.json.params.data_sha256`) · eToro facts: [`docs/etoro/evidence-2026-10-10/README.md`](../etoro/evidence-2026-10-10/README.md)
Engine: [`cmd/index-research`](../../services/strategy-executor/cmd/index-research/main.go) on [`shared/pkg/cfdsim`](../../shared/pkg/cfdsim/cfdsim.go). With financing and fees set to zero, cfdsim reproduces the frozen `dailyrule.Simulate` bit for bit (`TestMatchesDailyrule`).

> [!CAUTION]
> **Verdict: the evidence does not support SMA50 on the index CFDs as a way to make money.**
> - Measured against the honest alternative, an x1 holding of **QQQ / SPY** (a real ETF: dividends, no financing), the trend-following CFD loses in **79 % (NSDQ100) and 88 % (SPX500) of rolling one-year windows**. Over the full ETF history it loses by an order of magnitude.
> - It also loses to simply holding the same CFD in about 70–77 % of rolling years.
> - Its one robust property is **lower drawdown**: lower than holding the CFD in **≈70 %** of rolling years, and large protection in 2000–02, 2008 and 2022.
>
> The pre-registered forward test (§6) is still worth running on **demo money**. It tests the rule and the whole execution pipeline on data nobody has seen. Its expected outcome, given these base rates, is: H1 likely to pass, H2b likely to fail.

> [!NOTE]
> Costs here are **provisional**: Saturday (weekend-quote) capture, 2026-10-10. They are re-measured inside the NYSE cash session on Monday 2026-10-12 and frozen into the pre-registration. Sensitivity (§5) shows that halving or doubling the spread does not change any conclusion.

---

## 1. Question

**D3 of the porting plan** reuses the frozen SMA50 rule as it is. For each instrument:
- **Signal**: long while the close is strictly above the 50-day SMA of closes, otherwise flat.
- **Timing**: decide at close *t*, fill at open *t+1*.
- **Size**: x1, all-in / all-out.

The rule is applied to the eToro index CFDs **NSDQ100 (id 28)** and **SPX500 (id 27)**.

Does that beat:
- (a) **holding the same CFD**, and
- (b) **holding the ETF** an eToro user could buy instead (**QQQ**, **SPY**; x1 long settles as the real asset)?

## 2. Data and cost model

| Input | Source | Span |
|---|---|---|
| CFD proxy for long history | Yahoo `^NDX`, `^GSPC` official daily bars | 1985-10 / 1970-01 → 2026-10-09 |
| eToro CFD bars | live `OneDay` route, UTC-day buckets, NYSE trading days only ([etorodaily](../../shared/pkg/etorodaily/etorodaily.go)) | 2022-12 → 2026-10-09 |
| ETF benchmark | Yahoo QQQ / SPY, **dividend-adjusted** OHLC | 1999-03 / 1993-01 → |
| Financing reference | Yahoo `^IRX` (13-week T-bill) | 1970 → |

**How well the proxy matches.** On the 955/959-day overlap, the eToro CFD and the official index have:
- close ratio 0.99996 / 0.99993;
- daily-return correlation 0.963 / 0.972 (the eToro bar closes at 00:00 UTC, the index at 16:00 New York time);
- **SMA50 position agreement 98.8 % / 99.2 %**.

On the same dates, the rule earned 4.2 / 4.6 pp less on eToro bars than on index bars (`report.txt`, "bar source effect"). The proxy is therefore slightly *favourable* to the rule.

**Costs.** All are provisional and come from the demo capture.
- **CFD spread**: round trip **15.5 bps** (NSDQ100) / **14.8 bps** (SPX500), half charged on each leg.
- **CFD financing**: eToro charges **2.3 bps per night** on a 1,000 USD x1 position today, i.e. 8.4 %/yr over 365 nights.
  - History uses **(^IRX + 4.338 %)/365** per calendar night. The markup is calibrated so that today's rate equals the measured fee, so 1990s and 2000s nights cost more and 2010s nights less.
  - Weekends count three nights; holidays add theirs.
- **ETF**: **1.50 USD per order** on the **1,100 USD** forward-test size (13.6 bps per leg), plus a 0.1–0.3 bps spread, no financing, dividends included.
- **Random baseline**: 2,000 strategies with the same number of trips, seed 1, as in every earlier pre-registration.

> [!WARNING]
> **Dividends on index CFDs are not modelled.** Whether eToro credits dividend adjustments to long index CFD positions is unverified. If it does, CFD returns are understated by roughly the dividend yield while held: about 0.6–1 %/yr for NDX and 1.3–2 %/yr for SPX. The "financing markup ×0.5" row in §5 bounds that effect. It narrows the gaps but changes no verdict.

## 3. Results by window

Returns are % over the window, starting from equity 1.0. Margin close-outs ("CO") are explained in §3.3.

### 3.1 NSDQ100 (index proxy ^NDX, benchmark QQQ)

| Window | CFD hold | **CFD trend** | QQQ hold | QQQ trend | CFD trend max DD | CFD hold max DD | Trend beats random |
|---|---|---|---|---|---|---|---|
| Since QQQ inception (1999-06 →) | 768.6 % (CO) | **139.0 %** | 1591.3 % | 558.8 % | 49.3 % | 107.9 % | 86.8 % |
| 2000–2009 | −85.6 % (CO) | **−10.5 %** | −51.2 % | 17.7 % | 49.2 % | 104.7 % | 92.0 % |
| 2010–2019 | 244.4 % | **5.9 %** | 408.1 % | 45.1 % | 23.2 % | 31.5 % | 8.9 % |
| 2020–2026 | 152.7 % | **61.9 %** | 263.4 % | 120.3 % | 22.9 % | 43.5 % | 75.2 % |
| Last 10 y | 328.9 % | **88.0 %** | 573.3 % | 193.8 % | 22.9 % | 47.4 % | 69.8 % |
| Last 3 y | 69.0 % | **27.5 %** | 110.3 % | 51.7 % | 15.6 % | 27.7 % | 63.4 % |
| eToro bars 2023-03 → | 94.1 % | **37.0 %** | 149.5 % | 64.9 % | 15.3 % | 28.8 % | 68.0 % |

### 3.2 SPX500 (index proxy ^GSPC, benchmark SPY)

| Window | CFD hold | **CFD trend** | SPY hold | SPY trend | CFD trend max DD | CFD hold max DD | Trend beats random |
|---|---|---|---|---|---|---|---|
| Since SPY inception (1993-04 →) | 584.2 % (CO) | **−44.0 %** | 3117.3 % | 187.1 % | 66.2 % | 161.8 % | 17.1 % |
| 2000–2009 | −82.9 % (CO) | **−52.7 %** | −10.7 % | −17.7 % | 61.7 % | 110.1 % | 32.1 % |
| 2010–2019 | 99.7 % | **−19.4 %** | 248.9 % | 15.1 % | 34.2 % | 30.6 % | 4.4 % |
| 2020–2026 | 64.4 % | **16.8 %** | 163.9 % | 65.9 % | 25.8 % | 34.6 % | 58.1 % |
| Last 10 y | 123.3 % | **14.2 %** | 320.6 % | 88.5 % | 25.8 % | 40.8 % | 40.4 % |
| Last 3 y | 45.3 % | **22.3 %** | 88.3 % | 50.4 % | 11.6 % | 23.1 % | 73.0 % |
| eToro bars 2023-03 → | 54.9 % | **19.9 %** | 113.2 % | 56.8 % | 11.8 % | 24.2 % | 62.2 % |

### 3.3 Why "CO" matters: an x1 CFD is not an ETF

- Holding a CFD for years pays financing on the **full notional every night**. Since 1999 that is about 575 % (NSDQ100) and 1,097 % (SPX500) of starting equity.
- The cumulative debit eventually exceeds half the margin, and an ESMA account is **closed out** (`MarginCloseOut`). The CFD "hold" figures on long windows are therefore not achievable as written.
- The point stands either way: **a multi-year index exposure belongs in the ETF, not in a CFD.**
- The trend rule is never closed out: it is flat about 30 % of the time and pays only 30–50 % of the hold's financing.

## 4. Base rates for the pre-registration

### 4.1 Calendar years since the ETF exists

Rate-linked financing, index bars. Full table in `report.txt`.

| | Years | **H1** trend DD < CFD hold DD | **H2** trend > CFD hold | **H2b** trend > ETF hold | **H3** beats ≥ 95 % random |
|---|---|---|---|---|---|
| NSDQ100 | 2000–2026 | **18 / 27** | 8 / 27 | **5 / 27** | 0 / 27 |
| SPX500 | 1994–2026 | **21 / 33** | 7 / 33 | **3 / 33** | 1 / 33 |

- H2 and H2b pass almost only in **bear years** (2000–02, 2008, 2022; also 2018 for NSDQ100, and 2020 and 2025 for SPX500 on H2).
- Every bull year is lost to holding.

### 4.2 Rolling 252-day windows, step 21 days

| | Windows | H1 | H2 | H2b | QQQ/SPY trend > ETF hold | Median trend − CFD hold | Median trend − ETF hold |
|---|---|---|---|---|---|---|---|
| NSDQ100 | 316 | **70.3 %** | 30.7 % | 20.9 % | 24.1 % | −7.6 pp | −14.8 pp |
| SPX500 | 389 | **69.9 %** | 23.1 % | 11.6 % | 15.7 % | −5.1 pp | −14.4 pp |

Applying the rule to the ETF itself (QQQ/SPY trend) instead of the CFD removes financing, but it still trails ETF holding in 76–84 % of years. **The rule's cost is mostly the bull-market returns it misses, not only the CFD's carry.**

## 5. Cost sensitivity

Returns in %. The spread and financing scenarios change only the CFD columns.

| Scenario | NSDQ100 trend, last 10 y | NSDQ100 hold | QQQ hold | SPX500 trend, last 10 y | SPX500 hold | SPY hold |
|---|---|---|---|---|---|---|
| Base | 88.0 | 328.9 | 573.3 | 14.2 | 123.3 | 320.6 |
| Spread ×0.5 | 100.0 | 329.3 | 573.3 | 21.0 | 123.5 | 320.6 |
| Spread ×2 | 66.0 | 328.0 | 573.3 | 1.7 | 122.9 | 320.6 |
| Financing markup ×0.5 (≈ dividend credit) | 119.6 | 389.2 | 573.3 | 34.1 | 165.2 | 320.6 |
| Financing markup ×1.5 | 60.7 | 268.5 | 573.3 | −2.8 | 81.5 | 320.6 |
| Financing fixed at today's 2.3 bps | 67.0 | 297.2 | 573.3 | 1.0 | 99.0 | 320.6 |
| **No financing at all** | 204.4 | 530.9 | 573.3 | 87.4 | 261.1 | 320.6 |

Even with **zero financing and half the spread**, the CFD trend stays far below holding the ETF in every window tested. No plausible cost revision reverses the verdict.

## 6. What happens next

1. **Freeze the pre-registration**
   ([`FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md`](FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md)).
   - The rule stays as decided (D3). The question is whether data nobody has seen agrees with these base rates.
   - The cost block is frozen after the weekday re-measure on 2026-10-12, before the first forward decision.
2. **P3 (demo executor) still has value.**
   - It exercises the whole pipeline on demo money: idempotent orders, reconciliation, risk caps, halts.
   - That pipeline serves the assisted mode (D2) whatever rule runs on it.
3. **Do not plan a real-account version of this rule unless the forward test passes H2b.**
   - Per §4, the prior that it passes is low (≈ 10–20 %).
   - If the goal is index exposure with lower drawdowns, the honest comparison for the operator is "hold QQQ/SPY" against "the rule's drawdown savings". That trade-off is shown on the forward-test page.

## 7. Reproduce

```bash
cd services/strategy-executor
E=../../docs/backtest-readiness/evidence-2026-10-10
go run ./cmd/index-research -fetch -data $E/data -etoro ../../docs/etoro/evidence-2026-10-10 -out $E
```

- `-fetch` re-downloads the Yahoo series. Leave it out to reuse the committed CSVs, which reproduces `report.txt` exactly (SHA-256 of the inputs are in `results.json`).
- Yahoo is a research-only input: no live decision depends on it.
