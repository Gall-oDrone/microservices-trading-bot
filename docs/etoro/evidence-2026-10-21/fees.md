# eToro demo: overnight fees, venue vs frozen model

Generated 2026-10-21T15:39:40Z · history since 2026-09-21T15:39:40Z · model **2.3 bps per calendar night** on the position amount

**Verdict: `not_charged`**

- 11 position-nights, venue fees 0.00 USD, model 2.78 USD: the demo account charges no overnight financing (or books it where neither the history's fees nor the portfolio's totalFees show it)
- the frozen costs of the pre-registration decide pass/fail regardless of this verdict; this report turns an assumed cost into a measured one (pre-registration §5, execution note)

| Position | Book | State | Opened (UTC) | Closed (UTC) | Nights | Amount USD | Price P&L | Net profit | Venue fees | Model fees | Ratio |
|---|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|
| 3616683146 | nsdq100 | closed | 2026-10-12T13:35:06Z | 2026-10-16T13:35:05Z | 4 | 1099.99 | -34.24 | -34.24 | **0.00** | 1.01 | 0.00 |
| 3616683295 | spx500 | closed | 2026-10-12T13:35:11Z | 2026-10-19T13:35:06Z | 7 | 1099.99 | -16.36 | -16.36 | **0.00** | 1.77 | 0.00 |
| 3622570112 | nsdq100 | closed | 2026-10-19T15:46:35Z | 2026-10-19T18:03:31Z | 0 | 1099.99 | -7.79 | -7.79 | **0.00** | 0.00 | 0.00 |
| 3623460232 | — | closed | 2026-10-21T15:33:51Z | 2026-10-21T15:34:02Z | 0 | 1099.97 | 0.09 | 0.09 | **0.00** | 0.00 | 0.00 |
| **Sum** | | | | | | | | | **0.00** | **2.78** | |
