# eToro real-account canary: measurement only (plan, 2026-10-21)

**Status (2026-10-22): built, not yet used.** C1–C10 are on branch `feat/etoro-real-canary`
(§7 lists the commits). The executor still refuses the real account; `ui-api` builds a real broker
only under `-etoro-real-canary` with the six-part acknowledgement (§5.2). **No key, no ack file, no
unit and no order exist yet**; the operating procedure as built is runbook §14 and the rehearsal
gate (§7) comes before any real key. Where the build departed from this text, the paragraph says
so with the date; the original reasoning is kept.

**One sentence.** Open one small, hand-placed position on the **real** eToro account, next to the
demo cells and never sharing a process, key, port, ledger or policy with them, to **measure the
things the demo account cannot show** (overnight financing as charged, fill quality, the history's
lag and field semantics, the venue's behaviour outside the `/demo/` routes) and then close it,
revoke the key and write the numbers down.

**Not** a go-live, **not** a strategy test, **not** a smaller copy of the forward test. The
pre-registered forward tests (CFD cell to 2027-10-21, ETF cell from 2026-10-22) keep running on
demo untouched; the canary's numbers inform the *real* go-live plan that does not exist yet.

---

## 0. The constraint you set, and what it allows

You said **60 USD at most, on the NASDAQ-100**. eToro's minimum for the index CFDs is **1,000 USD
of exposure** per position ([`evidence-2026-10-10`](evidence-2026-10-10/), eligibility table;
a 1,000 USD order executed as 999.99). So 60 USD cannot open NSDQ100 at x1. There are three
honest ways to read "60 USD on the NASDAQ-100", and they measure different things:

| | **A. NSDQ100 x1** (the cell as pre-registered) | **B. NSDQ100 x20, 50 USD margin** ("financing probe") | **C. QQQ x1, 60 USD** (the same index as an ETF) |
|---|---|---|---|
| Settlement | CFD | CFD | real (the ETF itself) |
| Invested / exposure | **1,000–1,100 / 1,000–1,100 USD** | **50 / 1,000 USD** | **60 / 60 USD** |
| Fits "60 USD at most" | **No** (needs ~1,100 of capital; ~90–100 at risk with the 8 % disaster stop) | **Yes** (50 USD invested; 10 USD spare) | **Yes** |
| Loss cap | disaster stop 8 % ≈ 88 USD on 1,100, plus a gap | eToro caps the stop at **50 % of the invested amount** at x20 → **25 USD**, reached by a **2.5 % adverse move** in the index (NSDQ100's daily ATR is ≈ 1.5 %: expect it within days to a few weeks) | the 60 USD; no stop overlay in the ETF cell |
| Measures CFD **overnight financing** (the main unknown; demo charges none) | yes | **yes** — eToro's overnight fee on indices is per unit held, so 1,000 USD of exposure pays the same nightly fee at x20 as at x1 (to be confirmed by the cost preview on day 0; see M1) | **no** (an ETF at x1 has no financing; it has a commission) |
| Measures fill quality, history lag, real-route semantics | yes | yes | yes |
| Measures stop-out / venue_close mechanics | only if it happens | **very likely** (and that is a measurement too) | no |
| Policy fit | the existing `EtoroDemoProtectedPolicy` shape, real sizes | **needs an exception**: `MaxLeverage 20`, invested ≤ 60, exposure ≤ 1,000, one position; a dedicated `EtoroRealCanaryPolicy` | `MaxLeverage 1`, invested ≤ 60; a dedicated policy |
| Measures the strategy | partially (one position) | **no** | no |

**Recommendation.** If 60 USD is a **capital** cap, **B is the only way to measure NSDQ100's
financing on the real account**, and it is acceptable *as a probe* precisely because it is not a
trade: the loss is capped by eToro at 25 USD and by the budget at 60, there is no re-entry
without a human, and the leverage is a means to reach the venue's exposure floor, not a bet. If
60 USD is a **risk** cap and you can park ~1,100 USD of capital, **A** measures the same things on
the position the cell would actually hold and is the cleaner evidence. **C** is the fallback if
you do not want a leveraged CFD on the real account at all; it measures the ETF cell's commission
and fills but says nothing about the CFD cell's financing.

The rest of this plan is written for **B**, with the differences for A and C called out where
they exist (§6 policy, §9 budget). **Decision D1 (§12) picks one.**

---

## 1. Goals and non-goals

### 1.1 Goals (each is a measurement with a number at the end)

1. Overnight financing per night on a real index-CFD position: what eToro charges vs what its
   cost preview says vs the frozen model (2.3 bps per calendar night on the position amount).
2. Weekend financing (which night carries the triple charge for indices, and how much).
3. Fill quality on the real routes: fill vs mid and vs the previous close at open and at close,
   against the spread the rates feed showed.
4. The trading history's behaviour after a close on the real account: how long until the closed
   position appears, which fields carry fees / net profit / close rate, whether `totalFees` on the
   portfolio agrees with the history's `fees`.
5. Real-route semantics the demo could not exercise: the `/api/v2/trading/execution/orders` path
   (no `/demo/`), duplicate-reference resolution of the adapter after a confirmed fill, error
   codes (e.g. the demo's `741` re-close refusal), eligibility (minimum stop distance, `SL ≤ 50 %`
   at x20) and rate limits.
6. If a stop-out happens (likely under B): the venue's close rate vs the stop, the gap, when the
   history reported it, and whether `etoro-reconcile` and the Positions view classify it as
   `stopped_out` from the history alone.

### 1.2 Non-goals

- No automatic trading on the real account: **no cron run places or edits orders**, no ratchet,
  no re-entry. Every order is prepared and placed by hand in the console with the real phrase.
- No change to the demo cells, their ledgers, policies, cron lines or verdicts.
- No change to the frozen cost model of either pre-registration. The frozen costs decide pass or
  fail; the canary adds a "measured" column, it does not move the goalposts.
- Not a path to leverage in the strategy. `MaxLeverage 20` exists in the canary policy only, and
  the canary policy is loaded only by the canary process.
- Not a performance test. One position of 50–60 USD cannot say anything about the rule.

---

## 2. What the demo could not tell us (why this is worth 60 USD)

Measured on demo and documented, so there is no doubt about the gap:

- **Financing:** 11 position-nights across three CFD positions, venue fees **0.00 USD** vs the
  model's 2.78 USD; verdict `not_charged` ([`evidence-2026-10-21/fees.md`](evidence-2026-10-21/fees.md)).
- **Commission (ETF):** eToro previewed 1.65 USD on a 1,100 USD QQQ order; the demo history
  shows `fees 0.00` on the round trip.
- **Fills:** demo fills at the displayed rate; `fill_vs_mid_bps` has been noise.
- **Cash:** reconciles to the cent every run, which a real account with financing will not.

The paper ledger already charges the frozen costs, so the *verdicts* are honest. What is missing
is the evidence that the frozen costs are the real costs, and how the real account behaves when
things happen outside our control (a stop hit at 03:00 New York, a history that lags by hours).

---

## 3. Architecture: strictly parallel to demo, never in the same process

### 3.1 Why a separate process and port (your question)

Yes: **a second `ui-api` process on its own loopback port is the right shape**, not a flag on the
existing one. The reasons are structural, not aesthetic:

- `Server.Trading` holds **one** broker, built from **one** env file. The single most likely way
  to send a real order by mistake is to have the demo key and the real key loaded by the same
  process and pick the wrong one on one code path. Two processes make that impossible without
  intent.
- The operator token gates every control. A **different token file** for the canary means a tab
  authorised for demo cannot place on real, and vice versa, even if someone pastes the wrong URL.
- The halt file, the audit log, the intents dir and the ledger lock are **per ledger directory**.
  A separate directory keeps the canary's halt from touching demo and the reconcile reports apart.
- Ports are visible: `:8090` demo, `:8091` real. The web app shows which one it talks to (§3.4).

The alternative (one process, two brokers keyed by ledger) would need a broker registry, per-ledger
tokens and per-ledger policies in `ui-api`, and the UI would show demo and real side by side in
the same ledger picker. More code, more ways to mix them, for a canary that lives a few weeks.
**Not recommended.** If a unified read-only view is wanted later, a merge of the two ledgers in
the demo `ui-api` (read-only, no trading) is a separate, smaller task.

### 3.2 Process and file map

| | Demo (unchanged) | **Real canary** |
|---|---|---|
| `ui-api` address | `127.0.0.1:8090` (`mtb-ui-api` user service, runbook §13) | **`127.0.0.1:8091`**, its own unit **`mtb-ui-api-real-canary`** with its own `ui-api-real-canary.env` (`-addr`, token file, env file, ledger, the real flag) |
| Env file | `~/.config/microservices-trading-bot/etoro-demo.env` (`ETORO_ENV=demo`) | **`~/.config/microservices-trading-bot/etoro-real-canary.env`** (`ETORO_ENV=real`, **its own API key**, 0600) |
| Operator token | `~/.config/mtb/operator-token` | **`~/.config/mtb/operator-token-real-canary`** (different value) |
| Acknowledgement | — | **`~/.config/microservices-trading-bot/REAL-ACCOUNT-ACK`** (§5.2) |
| Ledger | `services/strategy-executor/etoro-daily-data/demo/` (and `demo-etf/`, `demo-rotation/`) | **`services/strategy-executor/etoro-daily-data/real-canary/`** (ledger.jsonl, intents/, candles/, risk-state.json, ui-audit.jsonl, **observations.jsonl**) |
| Ledger name (`?ledger=`) | `etoro-demo` | **`etoro-real-canary`** (also the halt phrase) |
| Policy | `EtoroDemoProtectedPolicy` | **`EtoroRealCanaryPolicy`** (§6) |
| Executor cron | 09:35 / 09:38 / 09:41 NY | **none that trades**; one read-only `etoro-canary-observe` at 18:00 NY daily (§3.5) |
| S3 prefix | `daily-executor/etoro-demo*` | **`daily-executor/etoro-real-canary`** (observations + ledger, optional; D4) |
| Watchdog | staleness on the demo prefixes | **none** (no daily decision to be late) or observations staleness only (D4). *As built:* the observer's `run-<UTC>.log` follows the executors' convention, so the existing Lambda watches `etoro-real-canary=daily-executor/etoro-real-canary` (plain daily calendar) with no code change |
| Web | vite `:5173` → `:8090`, or `-static` | **vite `:5174` with `UI_API_URL=http://127.0.0.1:8091`** (`npm run dev:real-canary`), or the canary `ui-api` with `-static ../../web/dist` |

Nothing under `real-canary/` is ever read by a demo process and nothing under `demo*/` by the
canary process. `install-ops-cron.sh` does not learn about the canary's trading; it may learn the
observe job (D3).

### 3.3 Start lines (target shape)

```bash
# Demo, as today.
go run ./cmd -operator-token-file ~/.config/mtb/operator-token \
  -etoro-ledger ../strategy-executor/etoro-daily-data/demo/ledger.jsonl -etoro-trading

# Real canary: a different port, env file, token, ledger, and the explicit real flag.
go run ./cmd -addr 127.0.0.1:8091 \
  -operator-token-file ~/.config/mtb/operator-token-real-canary \
  -etoro-env-file ~/.config/microservices-trading-bot/etoro-real-canary.env \
  -etoro-ledger etoro-real-canary=../strategy-executor/etoro-daily-data/real-canary/ledger.jsonl \
  -etoro-trading -etoro-real-canary
```

Without `-etoro-real-canary` an env file with `ETORO_ENV=real` is **fatal**, as today. With it,
every item in §5.2 must hold or the start is fatal (never a silent read-only start). *As built
(2026-10-22):* the flag has no environment-variable form; the ack file and the demo token file
default to `~/.config/microservices-trading-bot/REAL-ACCOUNT-ACK` and `~/.config/mtb/operator-token`
(`-real-ack-file`, `-demo-operator-token-file`); the canary process must register exactly one
eToro ledger and it must be named `etoro-real-canary`.

In operation the canary runs as a second systemd user unit, `mtb-ui-api-real-canary`, installed by
`scripts/install-ui-api-service.sh --real-canary` next to `mtb-ui-api` (runbook §13): same binary,
a separate `~/.config/microservices-trading-bot/ui-api-real-canary.env` carrying the five settings
above, `Wants=mtb-ui-api.service` so the canary never runs without the demo console beside it, and
`systemctl --user status mtb-ui-api-real-canary` as the first thing to check. The two units share
nothing but the binary.

### 3.4 The web app must make the account unmistakable

- `GET /api/ui/controls` (and `/ledgers`) report `environment: "demo" | "real"` per ledger. The
  app renders a **persistent red top bar** on a real ledger: "REAL ACCOUNT · canary · 127.0.0.1:8091
  · policy etoro-real-canary-…", on every page, not dismissable; the document title is prefixed
  `REAL ·`; the venue accent switches from the eToro violet to a red variant; the favicon gets a
  red dot. The sidebar ledger badge reads `etoro-real-canary · REAL`.
- Confirmation phrases contain the word **REAL**: `PLACE REAL NSDQ100`, `CLOSE REAL NSDQ100`,
  `PROTECT REAL NSDQ100`, halt `etoro-real-canary`. The preview card states invested, exposure,
  leverage and the **maximum loss in USD** in the first line.
- The Trade page's amount bounds, leverage options and default come from `TradingInfo` (today
  they are constants 1,000 / 1,500 / 1,100 at x1): the canary reports `min 50 / max 60 / default
  50`, `leverage [20]` (B) or `[1]` (A, C).
- Mock mode gets a `?mock=real-canary` flag and fixtures so the banner, the phrases and the bounds
  are testable without any key.

### 3.5 Daily observation without trading authority

A new small command, `cmd/etoro-canary-observe`, read-only, run by cron at **18:00 New York every
day including weekends** (eToro books overnight fees at 17:00 New York; weekend fees land on one
weekday night for indices, which M2 identifies):

- reads the account, the open positions (with `totalFees`), the trading history since the
  canary's first open, and the cost preview for the held instrument at the held size;
- appends one line to `real-canary/observations.jsonl`: timestamp, account cash/equity, per
  position `fees_to_date`, `unrealised`, the preview's `overnightFee` / `weekendFee`, the day's
  delta in fees (the measurement), and the history rows that appeared since the last observation;
- never sends anything; exit 1 only when a read failed (the alerts job picks it up), 0 otherwise.

This is the measurement instrument. The console's Positions view shows the same numbers live,
but the series must exist even on days nobody opens the console.

---

## 4. Measurements: question → method → record → done when

| # | Question | Method | Recorded in | Done when |
|---|---|---|---|---|
| **M1** | Financing per night as charged vs preview vs frozen 2.3 bps | `observations.jsonl` daily delta of `fees_to_date`; day-0 cost preview (`overnightFee` for 1,000 USD exposure at the canary leverage) | observations; `etoro-fees-check -ledger real-canary` report | ≥ **10 weeknights** measured, or the position is closed by the venue |
| **M2** | Weekend charge: which night, how much | the observation whose delta ≈ 3× the weekday delta | observations | ≥ **2 weekends** |
| **M3** | Fill vs mid / vs prior close at open and close | the place response (`fill_vs_mid_bps`, `fill_vs_close_bps`) and the rates feed's bid/ask at the time | ledger line, audit | every open and close (≥ 1 round trip) |
| **M4** | History lag and field semantics after a close | observations' `history_rows_new` with timestamps; manual notes on `fees`, `netProfit`, `closeRate` | observations; evidence `history.md` | first close reported |
| **M5** | Real-route behaviour: paths, error codes, duplicate reference, eligibility, limits | the adapter's logs under `-etoro-real-canary` (request ids, codes); eligibility and costs captured on day 0 like `etoro-spike` does | evidence `routes.md`, `eligibility.json`, `costs.json` | day 0 + first close |
| **M6** | Stop-out mechanics (B) | `etoro-reconcile -ledger real-canary` classification (`stopped_out`, gap bps); Positions view; observation timestamps | ledger `venue_close` line (hand-run reconcile, since no executor run writes it) | if it happens |
| **M7** | Cash/equity vs ledger with real fees | the reconcile's account section | reconcile report | each observation |

Deliverable: `docs/etoro/evidence-real-canary-<start>/` with the captured JSON, the fee report,
a `SUMMARY.md` ("measured vs frozen" per line item), and a short addendum appended to both
pre-registration documents under "Measured on the real account, reported, does not decide".

---

## 5. Safety model for the real account

### 5.1 What stays refused

- `etoro-daily-executor` keeps refusing `ETORO_ENV=real` unconditionally. The canary never gets
  a trading cron. (The observe command is a different binary with read scopes only.)
- The demo `ui-api` keeps refusing a real env file. Only a process started with
  `-etoro-real-canary` **and** every item of §5.2 may build a real broker.
- The console's `recommend` and `prepare` run on real with the canary policy; nothing else does.

### 5.2 The acknowledgement (all of these, or the process does not start)

1. `-etoro-real-canary` on the command line (not an env var: it must be visible in `ps`).
2. The env file names `ETORO_ENV=real` and a key whose **public-key prefix** is listed in the ack
   file (so a demo key cannot be promoted by renaming a file).
3. The ack file `REAL-ACCOUNT-ACK`, mode 0600, contains exactly: the sentence
   `I accept orders on the eToro REAL account for the measurement canary only`, the public-key
   prefix, the ledger path, the policy version, a start date and an **end date** (after which the
   start is refused again), your name.
4. The ledger path is under a directory named `real-canary` and is **not** any demo ledger.
5. The policy is `EtoroRealCanaryPolicy` (the version string is checked, not just the shape).
6. The operator token file differs from the demo one (hash compared at start if both are readable).

Every refusal names the missing item. The start logs one line with all six facts.

### 5.3 Per-order guards (prepare and place, as today plus)

- Policy (§6): one position account-wide, invested and exposure caps, leverage set, the stop
  required and inside the venue's bounds, one order per day, the session, price deviation.
- **The account must hold nothing but the canary**: `Positions()` returning anything the canary
  ledger does not know blocks the open (same eligibility rule as today, now with real money).
  *Superseded by D7 (2026-10-22):* the funded real account turned out to hold five long-term
  positions on two other instruments (6168, 1137; ≈ 749 USD of exposure, equity ≈ 802 USD) and a
  hand-trading history on NSDQ100. The rule is now **per instrument**: any NSDQ100 position the
  ledger does not know blocks the open (and `MaxPositions 1` counts NSDQ100 positions); holdings
  on other instruments are a REAL warning on every preview, named, never touched by the console.
  The consequence for §5.4 is honest: the account's buying power is no longer "the 60 USD" — the
  policy caps (invested ≤ 60, exposure ≤ 1,200, one order per day, one NSDQ100 position) are the
  last line of defence, not the balance.
- The phrase includes REAL; the dialog is red; the preview states the maximum loss.
- Intent first, audit `requested`, send, ledger line, audit `done` — unchanged. A failed place
  leaves the intent on disk; `etoro-reconcile -ledger real-canary` reports it until resolved.
- The ledger lock is taken as today (no executor competes, but the observe job reads the ledger
  and must see whole lines).

### 5.4 Key hygiene

- A **new API key** created for the canary, with trading scope, never pasted anywhere but the
  0600 env file; 2FA on the account; the key is **revoked at teardown** (§8.5).
- `scripts/check-no-etoro-secrets.sh` learns the real key's pattern and the ack file's sentence
  (neither may enter the repo); it keeps running before every commit.
- Deposit the 60 USD (or 1,100 for A) **and nothing more**; the account's buying power is the
  last line of defence and it should be small.

---

## 6. `EtoroRealCanaryPolicy` (shared/pkg/risk)

Version `etoro-real-canary-2026-10-<dd>`. Values for B; A and C in brackets. *As built
(2026-10-22): version `etoro-real-canary-2026-10-22`, nsdq100 only; the two rows marked ⚠ differ
from the draft.*

| Field | B | notes |
|---|---|---|
| `MaxLeverage` | **20** [A: 1, C: 1] | x20 is the only leverage that reaches 1,000 USD exposure from ≤ 60 USD invested. *Built:* also `Leverages: [20]`, the list the Trade page offers |
| `MaxOrderNotional` (exposure) | ⚠ **1,200** (draft said 1,000) [A: 1,100, C: 60] | the venue minimum is a floor, not the cap: 60 USD invested at x20 is 1,200 of exposure, and the draft's own `MaxInvestedUSD 60` allows it |
| **`MaxInvestedUSD`** (new field) | **60** [A: 1,100, C: 60] | invested = exposure / leverage; the cap you set. *Built:* with `MinInvestedUSD 50` and `DefaultInvestedUSD 50`, which `TradingInfo` reports as the Trade page's bounds |
| `MaxPositions` (new) | **1** | the account holds the canary and nothing else. *Built:* `State.OpenPositions` from the account's `Positions()`. ⚠ *D7 (2026-10-22):* counts the positions **on the order's instrument**, not account-wide (the real account holds long-term positions elsewhere); same values, same version |
| `MaxOrdersPerDay` | 1 | unchanged |
| `RequireStopLoss` | true | at x20 eToro caps it at 50 % of invested → the draft required **exactly the venue maximum distance** (25 USD). ⚠ *Built:* a **range, 2.3–2.45 % of the price** (23–24.5 USD): a market fill a few bps above the ask would push an exactly-2.5 % stop past the venue's cap and the order would be refused. A: the disaster stop; C: none |
| `MaxExposurePctEquity` | **off** | equity *is* the canary; the cap would block every order. *Built:* `MaxLossPctEquity` off for the same reason |
| `MaxPriceDeviationBps` | 1000 | unchanged |
| Market hours | NYSE cash session for opens and closes | unchanged |
| Halt file | honoured | `etoro-real-canary` phrase |

`TradeMinAmount` / `TradeMaxAmount` / `TradeDefaultAmount` move from constants in `trades.go`
into the policy (or `TradingInfo` derives them from it), so the demo keeps 1,000 / 1,500 / 1,100
and the canary gets 50 / 60 / 50 at x20 without touching demo code paths. *Built:* `TradingInfo`
derives them from the policy (`min_amount_usd`, `max_amount_usd`, `default_amount_usd`,
`leverage_options`, `default_leverage`, `policy_version`, `books`, `environment`); the demo
values are unchanged. A second policy, `etoro-canary-rehearsal-2026-10-22`, is the same shape on
the demo account for the rehearsal (nsdq100 + spx500, no `MaxPositions`: the demo account holds
the cells' positions).

---

## 7. Code changes (scope for the implementation thread)

Ordered so each step is testable before the next, and the real account is touched last.

| # | Where | Change | Tests | As built (2026-10-22, branch `feat/etoro-real-canary`) |
|---|---|---|---|---|
| C1 | `shared/pkg/etoro` | `Client.Env()`; route table already real-aware — add a table test asserting every real path has no `/demo/` segment and matches the OpenAPI capture of 2026-10-10 | unit | `b2aa64a`. `Client.Environment()` already existed; `paths_test.go` pins every real path |
| C2 | `shared/pkg/etoro` (new `realack.go`) | `LoadRealAck(path)` + `CheckRealAck(ack, envFile, ledgerPath, policyVersion, now)`: the six conditions of §5.2, each with its own error | unit: one test per missing condition | `ceeafeb`. `LoadRealAck`, `CheckRealAck(ack, RealAckCheck)`, sentinel errors `ErrRealAck{Flag,Env,KeyPrefix,File,Window,Ledger,Policy,Token}`, `Facts()` for the start line. File: the sentence, then `key_prefix` (8–16 chars), `ledger`, `policy`, `start`, `end`, `name`; 0600 |
| C3 | `shared/pkg/risk` | `EtoroRealCanaryPolicy()`; new fields `MaxInvestedUSD`, `MaxPositions`; `Check` enforces them; `Order.Leverage` already exists | unit + golden JSON | `2774782`. Also `MinInvestedUSD`, `DefaultInvestedUSD`, `Leverages`, `State.OpenPositions`; rules `max_invested_usd`, `max_positions`, `leverage_not_allowed`; the rehearsal policy; golden `testdata/etoro-real-canary-policy.json`. Deviations: §6 ⚠ rows. `934f871` fixes `shared/pkg/stops` rounding a clamped stop below the policy floor |
| C4 | `shared/pkg/etoroledger` | `ModeReal = "real"`; `Demo.Env` may be `"real"` (the JSON key stays `demo` for schema v1 stability — documented in the schema comment and the runbook); `UIClientRef` prefix `ui-real-…` so a canary ref can never collide with a demo ref | unit | `8f62d1e`. `ModeReal`, `IsAccountMode`, `Record.IsAccount()` (reconcile and `etorohealth` use it), `UIRealClientRef` / `IsRealRef`; `observation.go` (the observer's schema, `NightsHeld` on 17:00 NY boundaries, `FeeDeltas`); later `venueclose.go` (C7) |
| C5 | `services/ui-api` | `-etoro-real-canary` flag; `NewEtoroTrading` accepts real only with the ack; `TradingInfo` gains `environment`, `leverage_options`, bounds from policy; prepare/place accept `leverage` from the options (today hard-coded 1); phrases with REAL; `/ledgers` and `/controls` report `environment` | Go: gate refusals, phrase, bounds, a full prepare→place→cancel against the fake broker under the canary policy; the demo tests unchanged | `9ef452b`. `NewEtoroRealCanary(cfg, ack, chk)`, `NewEtoroCanaryRehearsal`; flags `-etoro-real-canary`, `-real-ack-file`, `-demo-operator-token-file`, `-canary-rehearsal`; units = invested × leverage / ask; preview `leverage`, `exposure_usd`, `max_loss_usd`, `environment`; trade tokens bound to env and leverage; the real account blocks an open when it holds any position; `protection.go` leverage-aware. Tests `internal/api/canary_test.go`; demo suites unchanged |
| C6 | `services/strategy-executor/cmd/etoro-canary-observe` (new) | §3.5; `-ledger`, `-env-file`, `-out observations.jsonl`, `-dry` (print only) | unit on the delta computation with fixtures | `546c960`. Plus `-allow-demo` (rehearsal), `-capture <dir>` (day-0 JSON + `SHA256SUMS`), `-preview-*`, `-since-days`; exit 1 writes the line with its errors |
| C7 | `cmd/etoro-fees-check`, `cmd/etoro-reconcile` | accept the canary ledger; the fees check gains a "per night, as observed" section from `observations.jsonl` and a `measured vs preview vs frozen` table | unit on fixtures | `1d953e2`. Both take `-real-canary`; the fees model is priced on exposure; reconcile gained **`-record-venue-close -by -reason`** (a reconcile is read-only, so the one write of §4 M6 is a named flag) and the venue-close classifier moved to `etoroledger/venueclose.go`, shared with the executor |
| C8 | `web` | env banner + red accent + title prefix; `TradingInfo` bounds and leverage; REAL phrases; `?mock=real-canary` fixtures; Positions/Today label the ledger `REAL`; the Trade page shows invested / exposure / max loss | vitest: banner on real, absent on demo; bounds; phrases; a11y of the banner | `05ddd13`. `lib/env.ts`, `RealBar` (role=alert, names the ui-api it talks to via `__UI_API__`), `data-env` on `<html>` switching the accent, `REAL ·` title + favicon dot, `· REAL` badge, leverage field, sizing line, red dialog, `npm run dev:real-canary`; `src/test/realCanary.test.tsx` |
| C9 | `scripts` | `check-no-etoro-secrets.sh` patterns; `install-ui-api-service.sh --real-canary` writes `ui-api-real-canary.env` and the `mtb-ui-api-real-canary` unit (`Wants=mtb-ui-api.service`, refuses to install if the demo unit is absent); `install-ops-cron.sh --canary-observe` (optional, D3) adds only the 18:00 NY observe line | shell test of the refusals | `151e649`. Also `ops-run.sh etoro-canary-observe` (run log last, for the watchdog), `ETORO_CANARY_S3_URI`; `scripts/tests/real-canary-scripts-test.sh` (48 checks, no systemd/go/aws) |
| C10 | docs | runbook §14 "Real canary" (operating procedure, §8 below), integration map, frontend plan §8.24, this plan marked "as built" | — | this commit |

Rehearsal before any real order: run the **canary binary against the demo account** with the
canary policy (`ETORO_ENV=demo`, `-etoro-real-canary` refused → a `-canary-rehearsal` flag that
loads the canary policy on demo). Open and close one x20 position on demo with the REAL-less
phrases, confirm the observe job writes deltas, the fees check reads them, the reconcile
classifies a hand-closed position. Only then the real key. *As built:* runbook §14.7; the
rehearsal refuses a ledger named `etoro-demo*` and runs on its own port.

Estimate: **1.5–2.5 days** of implementation and tests, half a day of rehearsal, then the
measurement window. *Actual:* one day of implementation (2026-10-22); the rehearsal is pending.

---

## 8. Operating procedure (runbook draft)

*As built (2026-10-22): the procedure with the exact commands, file formats and console steps
is runbook [ETORO-DAILY-EXECUTOR.md §14](ETORO-DAILY-EXECUTOR.md); what follows is the draft it
was written from. Where they differ, the runbook is right.*

### 8.1 Pre-flight (once)

- [ ] eToro real account funded with **the canary budget only** (60 USD; A: 1,100). Check the
  current terms for your country first: minimum first deposit (50–200 USD by region), deposit
  currency conversion fee if not USD, withdrawal fee (5 USD) and minimum withdrawal (30 USD),
  inactivity fee after 12 months. Deposit in USD if the account allows it.
- [ ] New API key for the canary; 2FA on. The key goes into the 0600 env file and nowhere else.
- [ ] `REAL-ACCOUNT-ACK` written by hand with an end date ≤ 8 weeks out.
- [ ] Rehearsal on demo passed (§7). `check-no-etoro-secrets.sh` green.
- [ ] `real-canary/` directory created; halt file **present and halted** until day 0 (start
  halted, resume on purpose).
- [ ] The canary `ui-api` starts on `:8091` and the start log shows the six ack facts; the web app
  on `:5174` shows the red bar; `GET /controls` shows `environment: real`, trading enabled, bounds
  50/60 at x20.
- [ ] Day-0 captures taken read-only: `eligibility.json`, `costs.json` (1,000 USD exposure at the
  canary leverage), `rates.json`, `account.json` → the evidence folder.

### 8.2 Day 0: open (by hand, cash session, not in the first or last 15 minutes)

1. Resume the halt (`etoro-real-canary` phrase, reason "canary day 0").
2. Trade page → Manual → NSDQ100, open, 50 USD, x20, stop at the venue maximum distance →
   **Prepare**. Read the preview: invested 50, exposure ≈ 1,000, stop ≈ 2.5 % below, **max loss
   25 USD**, costs (overnight, weekend), the warnings.
3. **Place · PLACE REAL NSDQ100**, reason, your name. Read the outcome: fill, `fill_vs_mid_bps`,
   position id, stop as eToro holds it.
4. Record the fill and the day-0 preview in `SUMMARY.md`. Confirm the first observation at 18:00
   NY shows the position with `fees_to_date 0`.

### 8.3 Daily

- Nothing to do. The observe job runs at 18:00 NY. Glance at the Positions card (`:5174`) or the
  observations file. Every Monday: `etoro-fees-check -ledger real-canary` and read the table.
- If the observe job fails two days running, fix it the same day; a gap in the series is a gap in
  the measurement.

### 8.4 Events

- **Stopped out (B, likely):** do nothing at the venue. Run `etoro-reconcile -ledger real-canary`
  by hand until the history shows the close; it writes the `venue_close` line (`stopped_out`,
  gap). Record the timestamps (stop time per eToro, time the history showed it). Decide whether
  to re-open **once** (M1 may need more nights); never more than **three** opens in the window.
- **Halt needed:** the Risk page, phrase `etoro-real-canary`. The console refuses prepares; a
  held position stays until you close it by hand.
- **Anything unexplained** (an order you did not send, a fee you cannot attribute, a position the
  ledger does not know): halt, close by hand in the console if needed, stop the canary `ui-api`,
  revoke the key. Write it down first, investigate second.

### 8.5 Close-out and teardown

1. When M1–M4 are done (or the end date arrives): **Close · CLOSE REAL NSDQ100** in the cash
   session; wait for the history; run the reconcile and the fees check one last time.
2. Copy `observations.jsonl`, the ledger, the audit log, the reconcile and fees reports and the
   day-0 captures into `docs/etoro/evidence-real-canary-<start>/`; write `SUMMARY.md` (measured vs
   preview vs frozen, per line item; lags; fills; anything surprising).
3. **Revoke the API key.** Delete the ack file. Stop the `:8091` process and remove the observe
   cron line. Withdraw the balance (or leave it; note the inactivity fee).
4. Append the "measured on the real account" addendum to both pre-registrations. Commit, push.

---

## 9. Budget (B; A and C in brackets)

| | B | notes |
|---|---|---|
| Capital | 50 USD invested, 10 spare | [A: 1,100; C: 60] |
| Maximum loss on the position | **25 USD** (venue stop cap) + a gap | [A: ≈ 88 USD + gap; C: 60] |
| Financing, expected | ≈ 0.23 USD / night on 1,000 USD exposure (2.3 bps model; the measurement is whether this holds) → ≈ 5 USD over 20 nights incl. weekends | [A: same; C: none] |
| Spread | ≈ 0.10–0.20 USD per round trip at 1,000 exposure (15 bps RT) | [C: ≈ 0.01] |
| Commission | none (CFD) | [C: ≈ 0.09 per order] |
| Account frictions | deposit conversion if not USD; 5 USD withdrawal; nothing else in the window | check the terms. *Measured 2026-10-22:* the account was funded with **60 USD** from MXN at eToro's conversion rate of **0.0518 USD per MXN** (≈ 1,158.3 MXN for the 60 USD; the spread against the interbank rate of the day is the first friction to write into `SUMMARY.md`) |
| Expected total cost of the exercise | **≈ 5–8 USD in fees, up to 25 USD if stopped out, 60 USD worst case** | |

---

## 10. Risk register

| Risk | Likelihood | Mitigation |
|---|---|---|
| An order reaches the real account from a demo workflow | low once built; the whole point of §3 and §5 | separate process/port/key/token/ledger/policy; six-part ack; phrases with REAL; red UI; the executor keeps refusing real |
| A real order is sent twice | low | intent first; adapter resolves a duplicate reference by the same `x-request-id`; one order per day in policy; the canary tests it against the fake broker first |
| The key leaks | low | 0600 files only; secrets check; a dedicated key revoked at teardown; the account holds 60 USD |
| Margin close-out instead of the stop (B) | medium | the stop at the venue's maximum distance is tighter than the close-out level; both are a measurement if they happen |
| The history lags for hours after a stop-out | expected | nothing to do; it is M4 |
| The financing measurement is confounded by a gap night / holiday | medium | observations are daily and timestamped; the fees check reports per night, not an average |
| eToro's terms on API trading or leverage differ for your country | unknown | read the current terms before funding; eligibility captured day 0 decides what the venue allows |
| Tax / reporting | certain, trivial | real gains and losses are reportable even at this size; keep the evidence folder |
| Scope creep ("while it's open, let's also…") | high | the ack has an end date; the policy allows one position; this document's non-goals |

---

## 11. Timeline

| Week | What |
|---|---|
| 0 | Decisions D1–D5. Implementation C1–C10 in a separate branch off `feat/etoro`, CI green, reviewed. |
| 1 | Rehearsal on demo with the canary binary and policy. Pre-flight checklist. |
| 1, day 0 | Open. First observations. |
| 1–4 | Daily observations; weekly fees check; stop-out handling if it happens (re-open at most twice more). |
| 4–6 | M1–M4 complete → close, teardown, evidence folder, addenda. |

---

## 12. Decisions needed before implementation

*Taken 2026-10-22, before implementation: **D1 = B**, **D2** = second process on `:8091` (as a
systemd user unit), **D3** = cron via `install-ops-cron.sh --canary-observe`, **D4** = yes, push
to `s3://…/daily-executor/etoro-real-canary` and watch the observations' staleness, **D5** = the
sentence verbatim as in §5.2 with the end date = the key's expiry minus one day, **D6** = at most
three opens in the window.*

- **D1 — Which position.** A (NSDQ100 x1, ~1,100 USD capital, ~90 at risk), **B (NSDQ100 x20,
  50 USD invested, 25 USD max loss; recommended if 60 USD is a capital cap)**, or C (QQQ x1,
  60 USD; no financing to measure).
- **D2 — Topology.** Second `ui-api` process on `127.0.0.1:8091` with its own web on `:5174`
  (**recommended**), vs one process with two brokers (not recommended, §3.1).
- **D3 — Observe job.** Cron at 18:00 NY daily via `install-ops-cron.sh --canary-observe`
  (recommended), or run by hand.
- **D4 — Off-machine copy.** Push `real-canary/` to `s3://…/daily-executor/etoro-real-canary`
  and watch the observations' staleness (recommended: yes, it is the evidence), or keep it local.
- **D5 — The ack sentence and the end date** (≤ 8 weeks from day 0 suggested).
- **D6 — Re-open policy on stop-out.** At most three opens in the window (suggested), or one.
- **D7 (2026-10-22, after the first real read) — Pre-existing positions on the real account.**
  The account was not empty (five long-term positions on other instruments). Decided: the
  one-position rule is scoped to the canary's instrument; other holdings are warned about, not
  blocked on, and never touched. Alternatives were closing them or using an empty account.

---

## Appendix A. Facts this plan relies on (with sources)

- Index CFDs: long x1 is `cfd` settlement; other long leverage 2/5/10/20 with `SL ≤ 50 %`;
  **minimum position exposure 1,000 USD**; max units per order 35 (NSDQ100) —
  [`evidence-2026-10-10`](evidence-2026-10-10/).
- ETFs QQQ (3006) / SPY (3000): `real` settlement at x1, minimum exposure 10 USD, stop 0–100 %
  allowed, trailing allowed — [`evidence-2026-10-21/eligibility.json`](evidence-2026-10-21/eligibility.json).
- Demo charged **no** financing over 11 position-nights and **no** commission on the ETF round
  trip — [`evidence-2026-10-21/fees.md`](evidence-2026-10-21/fees.md).
- Frozen costs: CFD 15.5 / 14.8 bps spread RT + 2.3 bps per night; ETF 15 bps per order —
  the pre-registrations in `docs/backtest-readiness/`.
- Route table demo vs real (`/demo/` segments on v2, `/demo`↔`/real` on v1 pnl, history path) —
  [`shared/pkg/etoro/paths.go`](../../shared/pkg/etoro/paths.go).
- Refusals of the real account today: executor `main.go` (`ETORO_ENV` must be demo, client
  forced to demo), `ui-api` `NewEtoroTrading`, `ui-api/cmd/main.go`.
- Policy already has `MaxLeverage`, `MaxOrderNotional`, `MaxOrdersPerDay`,
  `MaxPriceDeviationBps`, `MaxExposurePctEquity` — [`shared/pkg/risk/policy.go`](../../shared/pkg/risk/policy.go).

## Appendix B. Port and name map

| Thing | Demo | Real canary |
|---|---|---|
| `ui-api` | 127.0.0.1:**8090** | 127.0.0.1:**8091** |
| web dev | 127.0.0.1:**5173** | 127.0.0.1:**5174** (`UI_API_URL=http://127.0.0.1:8091`) |
| ledger name / halt phrase | `etoro-demo` | `etoro-real-canary` |
| trade phrases | `PLACE NSDQ100` … | `PLACE REAL NSDQ100` … |
| client_ref prefix | `ui-nsdq100-…` | `ui-real-nsdq100-…` |
| policy version | `etoro-demo-2026-10-19` | `etoro-real-canary-2026-10-22` (rehearsal on demo: `etoro-canary-rehearsal-2026-10-22`) |
| S3 prefix | `daily-executor/etoro-demo` | `daily-executor/etoro-real-canary` |
| systemd unit | `mtb-ui-api` | `mtb-ui-api-real-canary` |
| cron job | `ops-run.sh etoro` 09:35 NY weekdays | `ops-run.sh etoro-canary-observe` 18:00 NY daily (read-only) |
