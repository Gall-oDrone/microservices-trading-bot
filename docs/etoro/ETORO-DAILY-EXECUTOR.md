# eToro daily executor (index CFDs, demo account): operator runbook

Porting plan phase **P3** · branch `feat/etoro` · forward test:
[`FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md`](../backtest-readiness/FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md)

The executor applies the frozen SMA50 rule to eToro's own NSDQ100 (id 28) and SPX500 (id 27)
daily bars. It holds the **demo** account's position at the rule's target: x1 long of 1,100 USD,
or flat. The forward test is judged on paper (`cmd/index-research -forward`). The demo fills
exercise the order pipeline and turn assumed costs into measured ones; they do not decide pass or
fail.

> [!IMPORTANT]
> Demo only. The executor refuses `ETORO_ENV=real` unconditionally. Nothing in this runbook places
> a real-money order.

## 1. One-time setup on the ops host

```bash
# Credentials: never committed (scripts/check-no-etoro-secrets.sh guards the tree).
install -m 600 /dev/null ~/.config/microservices-trading-bot/etoro-demo.env
$EDITOR ~/.config/microservices-trading-bot/etoro-demo.env   # ETORO_PUBLIC_KEY=… ETORO_PRIVATE_KEY=… ETORO_ENV=demo

cd services/strategy-executor
go build -o etoro-daily-data/etoro-daily-executor ./cmd/etoro-daily-executor
go build -o etoro-daily-data/etoro-reconcile      ./cmd/etoro-reconcile
```

Crontab (the executor skips NYSE holidays itself). The job must run at 09:35 New York time; two
forms, depending on the host's cron:

```cron
# cron with CRON_TZ support (cronie, systemd-cron):
CRON_TZ=America/New_York
35 9 * * 1-5  /path/to/repo/scripts/ops-run.sh etoro

# Debian/Ubuntu vixie cron (this ops host): CRON_TZ is silently ignored, so list both UTC
# candidates and let only the one in the 09:xx New York hour through (correct across DST).
# BEGIN mtb-etoro
35 13,14 * * 1-5 [ "$(TZ=America/New_York date +\%H)" = 09 ] && /path/to/repo/scripts/ops-run.sh etoro
# END mtb-etoro
```

> [!WARNING]
> Check which form your cron honours (`crontab -l` after a day: the run log must show a
> 09:35 New York start). With the first form on a cron that ignores `CRON_TZ`, the job runs
> at 09:35 **UTC**, outside the cash session, and every day ends `needs the NYSE cash session`.

Wiring the cron entry into `scripts/install-ops-cron.sh` is planned for phase P6. The **alerts**
side is already wired: the installer writes `UI_ALERTS_ETORO_LEDGER` to `ops.env`, and the
15-minute `ui-alerts` job checks this ledger directory (§3.1).

## 2. What a run does

`scripts/etoro-daily-executor-run.sh -demo` runs these steps for each instrument:

1. **Halt check.** It reads `<ledger dir>/risk-state.json`. A corrupt halt file exits 2 before anything else runs.
2. **Bars.** It reads eToro's live daily candles **twice** and keeps NYSE trading days only.
   - The last closed bar must be the latest closed trading day. Otherwise the run stops on stale data.
   - The two reads must agree on the signal and be within 5 bps of each other (see §4).
3. **Decision and paper account.** It applies the frozen rule and runs the paper account with the pre-registered costs (`cfdsim`).
4. **Reconcile before acting.** The ledger's position must match the account.
   - Any other position on the instrument stops the run. Nothing is sent.
   - The exception is an interrupted leg of this executor (an intent exists for it), which is resumed.
5. **Risk check** (`risk.EtoroDemoPolicy`):
   - x1 only;
   - at most 1,500 USD per order and one leg per instrument per day;
   - total exposure at most 5 % of equity;
   - the quote within 10 % of the decision close;
   - **the NYSE cash session must be open**;
   - the halt file in force.
6. **Order.**
   - An intent is written (fsync) first.
   - Then a market open of 1,100 USD x1, or a full close of the recorded position.
   - The order carries `x-request-id = UUIDv5(ClientRef)`, with `ClientRef = sma50-<book>-<fill date>-open|close`.
7. **Ledger.** It appends one line per instrument per decision day to `etoro-daily-data/demo/ledger.jsonl`. Each line carries:
   - the decision and paper account;
   - the quote, cost preview, risk check and order;
   - fill vs mid and fill vs decision close;
   - financing to date.

The wrapper then runs `etoro-reconcile` and writes `etoro-daily-data/demo/reconcile/<New York date>.json`.
The reconcile is read-only. When the ledger's newest order finished within the last 10 minutes and
eToro's views do not show it yet, it re-reads every 5 s for up to 60 s in total before judging: the
portfolio (a fresh open must be listed, a fresh close must be gone) and the trading history (a
fresh close must be listed). A wait that was needed appears as a `portfolio_lag` / `history_lag`
warning and as `position_reads` / `history_reads` / `settle_waited_s` in the report. Warnings never
make a report `drift`.

**P3 exit criterion:** five consecutive trading days with `reconcile=clean`, plus a passing
kill-switch drill (`scripts/kill-switch-drill.sh --offline --source services/ui-api/internal/api/testdata`;
it covers both executors). A day whose only drift is `missing_position`, `orphan_position` or
`close_not_in_history` reported within the settle window of the executor's own fill or close,
followed by a clean read-only re-check saved as `reconcile/<date>.recheck.json` and a dated operator
note in the run log, counts as clean (precedents: 2026-10-12, portfolio lag on an open, before the
reconcile waited for the portfolio; 2026-10-16, history lag on a close, before it waited for the
history). The original report is never edited.

## 3. Reading the outcome

| Run log says | Meaning | Do |
|---|---|---|
| `exit=0`, `reconcile=clean` | Normal. | Nothing. |
| `already recorded … nothing to do` | Holiday, or a re-run on the same day. | Nothing. |
| `needs the NYSE cash session` | The run happened outside 09:30–16:00 New York time. Nothing was recorded. | Re-run in session. |
| `… (nothing recorded; re-run today to retry or resume)` | Quote, account or order outcome unknown. If an order was sent, its intent is on disk. | Re-run. It resends the **same** ref, so it cannot open twice. |
| `blocked by risk policy … recorded as blocked` | A limit or the halt stopped the order. The day is recorded with the position unchanged. | Review the findings. The next trading day plans from the target again. |
| `rejected … (recorded)` | eToro refused the order outright. | Read `demo.order.error`. The next day retries with a new ref. |
| `ledger says flat but the account holds …` / `… no longer holds it` | Someone traded the instrument by hand, or eToro closed the position (e.g. a margin close-out). | Resolve by hand (see §5). Never edit past ledger lines. |
| `two reads of eToro's bars disagree` | eToro served inconsistent history. | Re-run later. If it persists, record it for the forward-test report. |
| `reconcile=drift` with `missing_position`, `orphan_position` or `close_not_in_history` **on the same run that just filled or closed** that position | eToro's portfolio or trading history had not caught up after the 60 s settle wait. | Re-run `etoro-reconcile` by hand a few minutes later (`-json etoro-daily-data/demo/reconcile/<date>.recheck.json`). Clean: add a dated note to the run log; the day counts as clean. Still drift: resolve by hand (§5). |
| `reconcile=drift` (other) | See the findings in the JSON report. | `unfinished_intent`: re-run the executor. Other findings: resolve by hand. |
| `portfolio_lag` / `history_lag` warning, `reconcile=clean` | The portfolio or the history needed re-reads before it showed the order. Normal. | Nothing. The forward-test report lists how often it happened. |

### 3.1 What reaches you without reading the log

`ui-alerts` (R3, `services/ui-api/internal/etorohealth`) evaluates this ledger directory every
15 minutes from the files alone, with no eToro call, and sends through the same SNS topic and
lifecycle as the Bitso alerts: once when a condition appears, again when it escalates, a reminder
every 12 h while it stays open, once when it resolves. Alerts are named `(etoro-demo)`.

| Alert | Severity | Fires when | Clears when |
|---|---|---|---|
| eToro ledger coverage | critical | From **10:00 New York** on a trading day, a book has no ledger line with that fill date (the run was missed, or stopped before recording: stale bars, outside the session, unknown order outcome). Looks back 3 trading days. | The day is recorded (a re-run in session). |
| eToro executor run | critical | The newest `run-*.log` ended `exit≠0`, or has no `exit=` line 15 min after it started. | A later run ends `exit=0`. |
| eToro reconcile | critical | The newest `reconcile/<date>.json` has drift, and no clean `<date>.recheck.json` exists. | A clean re-check is saved, or the next day's report is clean. |
| eToro reconcile | warning | The report is clean but carries warnings other than `portfolio_lag`; or the day's run ended `exit=0` and left no report (the reconcile step failed). | — |
| eToro order sent but not recorded | critical | An intent is on disk for more than 10 min with no ledger line for its reference (the run died between the order and the ledger). | The executor is re-run and records it. |
| eToro trading halted | warning | `risk-state.json` says halted. Named together with any Bitso halt in the subject ("Trading halted (etoro-demo, stage)"). | The file is removed. |
| eToro halt file | critical | `risk-state.json` exists but is invalid (the executor exits 2 before anything runs). | Fixed or removed. |

A missed 09:35 run is therefore in your inbox by about 10:15 New York. Not covered: the workstation
being off (the Lambda watchdog covers only the Bitso ledger until P6 uploads this one to S3).

```bash
# what would be sent right now, without sending or saving state
cd services/ui-api && ./bin/ui-alerts -dry-run -etoro-ledger ../strategy-executor/etoro-daily-data/demo/ledger.jsonl
```

## 4. eToro behaviours the executor works around

- **Duplicate request id.** eToro rejects a reused `x-request-id`, and `orders:lookup?referenceId=` cannot find API orders. A resent open is therefore resolved to the position opened on the instrument since the intent's timestamp.
- **Portfolio lag.** The portfolio shows fills late: usually 1–3 s, but on 2026-10-12 it still showed no positions and the pre-trade cash balance 5 s after the second fill, and both positions 80 minutes later. The adapter polls for up to 30 s to resolve the fill; the reconcile waits for up to 60 s more (§2). The ledger's `demo.account` snapshot is the **pre-order** balance (`exposure` 0 before the first open); `position_after` is post-fill.
- **Trading-history lag.** The closed-positions history lists a close later than the portfolio drops it: on 2026-10-16 the portfolio no longer held the closed NSDQ100 position 10 s after the close, but the history did not list it yet (3 h later it did). The reconcile waits for the history too (§2).
- **Two versions of daily history** (seen 2026-10-10):
  - up to ~1.3 bps on 13–16 recent closes;
  - the executor reads twice and records `candles.revised_closes` / `max_revision_bps`;
  - recorded closes may move by up to 5 bps before the run stops.
- **Weekend and holiday quotes.** These bars are dropped. Orders go only in the cash session.
- **Session-dependent spreads.** Weekend-session previews quoted ~15 bps of spread per open; in the cash session the fills on 2026-10-12 paid 0.2–0.4 bps (fill vs mid −5.2 bps NSDQ100, −1.7 bps SPX500, both inside the quoted spread). The pre-registration's frozen costs are the weekend figures; report both.

## 5. Halting and manual resolution

- **Halt now.** Write the halt file next to the ledger:
  ```bash
  printf '{"halted":true,"reason":"<why>","by":"<you>","at":"%s"}\n' "$(date -u +%FT%TZ)" \
    > services/strategy-executor/etoro-daily-data/demo/risk-state.json
  ```
  With `ui-api -etoro-ledger <this ledger>` (`UI_API_ETORO_LEDGER` in `ops.env`; P5a, 2026-10-16) the operator UI reaches
  it too: **Halt all** writes this halt file along with the Bitso ones, and the ledger's own halt/resume
  (`POST /api/ui/risk/halt|resume?ledger=etoro-demo`, confirm `etoro-demo`) writes it alone. Every attempt is
  audited in `ui-audit.jsonl` next to the ledger.
  The halt itself does reach you: `ui-alerts` reports it as a warning within 15 minutes (§3.1).
- **Resume.** Delete the file. The next run trades from the rule's target.
- **Untracked position.** Close it by hand on the demo account (or with the eToro MCP `prepare-close` → `place-close`), then re-run. If the position *is* the executor's, add a note in the forward-test report. Never edit past ledger lines.
- **Disable for a day.** Set `ETORO_EXECUTOR_DISABLED=1` in the cron environment.

## 6. P3 soak log

Five consecutive clean trading days are needed. Started 2026-10-12 (first fills). Earliest exit:
Friday 2026-10-16.

| NY date | Run | Reconcile | Note |
|---|---|---|---|
| 2026-10-09 (Fri, dry) | manual smoke run at install time (03:18 UTC, 23:18 NY), outside session: nothing recorded | clean | Not a soak day. |
| 2026-10-12 (Mon) | opened NSDQ100 (3616683146) and SPX500 (3616683295), 1,100 USD x1 each | drift → **clean** on re-check | `missing_position` ×2 at 13:35:14Z, portfolio lag; re-check 14:56:22Z clean (`2026-10-12.recheck.json`). Day 1. |
| 2026-10-13 (Tue) | hold, nothing to do | clean | Day 2. Reconcile rebuilt with the settle wait after this run. |
| 2026-10-14 (Wed) | hold, nothing to do | clean (1 portfolio read) | Day 3. First run with the settle-aware reconcile. eToro alerts switched on in `ui-alerts` the same day. |
| 2026-10-15 (Thu) | hold, nothing to do | clean | Day 4. |
| 2026-10-16 (Fri) | **closed NSDQ100** (order 389021477, 0.035799 u at 29770.50; rule flat: 10-15 close 29868.70 vs SMA50 29871.96); SPX500 hold | drift → **clean** on re-check | Portfolio settled after 3 reads (10 s, as designed); `close_not_in_history` because the history lagged further; re-check 16:43:43Z clean (`2026-10-16.recheck.json`). First alert e-mail of the soak, 13:45 UTC, resolved by the re-check. Day 5. Reconcile rebuilt to wait for the history too. |

**P3 exit criterion met on 2026-10-16** under the re-check rule above (five consecutive trading
days whose books and records agree; two of them needed a clean re-check for a lag the reconcile now
waits out), with the kill-switch drill passed (40/40). Both lags are fixed in the reconcile that
runs from Monday 2026-10-19; the soak log continues through P5 as a plain record.

**Binaries rebuilt 2026-10-16 after the Friday run (P5d).** `etoro-daily-executor` and
`etoro-reconcile` were rebuilt from `feat/etoro` (previous copies kept as `*.prev` next to them)
for two changes that do not touch the rule: the ledger gained console trades (§8: lines with
`source: "ui"`, keyed by `book|bar_date|client_ref` so they never collide with the daily line, and
`LastDemoPosition` now orders by `(bar_date, recorded_at)`), and the paper block's `costs_status`
label changed from `provisional (weekend capture 2026-10-10)` to `FROZEN (pre-registration §3: the
provisional values of 2026-10-10 became final on 2026-10-12)`, the wording `etoro-risk-report`
already used; the frozen numbers themselves are unchanged. The rebuilt reconcile was run once,
read-only, against the live ledger at 19:20:57Z: **CLEAN** (cash 98,859.70, equity 99,936.94;
nsdq100 flat, spx500 long 3616683295). The executor was not run; Monday 2026-10-19 09:35 is its
first run with the new binary.

## 7. Risk report

`cmd/etoro-risk-report` (phase P4) reads the ledger, the saved candles, the Yahoo index histories
and the reconcile reports, and writes the risk and analytics report: trading-day VaR/ES and
backtest per book and for the portfolio (with the NSDQ100/SPX500 correlation), index stress
episodes, a CFD-priced Monte Carlo of the rule against the CFD and ETF holds, P&L attribution with
modelled and charged financing, and fill quality against the frozen costs. Reporting only; it
never writes the ledger and makes no eToro call.

```bash
cd services/strategy-executor
go build -o etoro-daily-data/etoro-risk-report ./cmd/etoro-risk-report
etoro-daily-data/etoro-risk-report -ledger ./etoro-daily-data/demo/ledger.jsonl \
  -candles ./etoro-daily-data/demo/candles \
  -yahoo ../../docs/backtest-readiness/evidence-2026-10-10/data \
  -reconcile ./etoro-daily-data/demo/reconcile \
  -size 1100 -capital 100000 -paths 2000 -seed 1 \
  -out ./etoro-daily-data/demo/risk/$(TZ=America/New_York date +%F)
```

Output: `etoro-daily-data/demo/risk/<NY date>/risk.json` (schema `cfdrisk-report/v1`) and
`risk.md` (also printed). Run it after each soak week (P6 schedules it after the Friday run). How
to read it, and the first run's numbers, are in
[`docs/risk/MODEL-VALIDATION-ADDENDUM-ETORO-2026-10-13.md`](../risk/MODEL-VALIDATION-ADDENDUM-ETORO-2026-10-13.md).

## 8. Assisted trades from the console (P5d, 2026-10-16)

The operator UI's **Trade** page (`web/`, route `/trade?ledger=etoro-demo`) can open or close a
demo position by hand through `ui-api`: `POST /api/ui/etoro/trades/prepare` runs the checks and
returns a preview with a two-minute token, `POST /api/ui/etoro/trades/place` sends it. It is the
porting plan's §4.4 "assisted mode" (prepare → place, as the eToro MCP does), demo only.

**How it is enabled.** Off by default. `ui-api` needs all of `-etoro-trading`
(`UI_API_ETORO_TRADING=1`), `-operator-token-file` (the same 0600 token the halt controls use),
`-etoro-ledger` (this ledger, a local file) and `-etoro-env-file` naming the demo env file with
`ETORO_ENV=demo`; a missing prerequisite is fatal at start, and `ETORO_ENV=real` is refused in
code. `GET /api/ui/controls?ledger=etoro-demo` reports `trading: {enabled, disabled_reason}`, and
the Trade page shows the reason when it is off. The same gates as the halt controls apply to every
call: loopback `Origin`, `Authorization: Bearer <operator token>`, a JSON body (unknown fields
rejected, 4 KB cap), and an audit line for every attempt.

**The checks (prepare), in order, each a reason in the block verdict:** the halt file
(`risk-state.json`; an invalid file blocks too, as it stops the executor) → `risk.Check` with
`risk.EtoroDemoPolicy` on the same order and state the executor builds (units from the amount and
the live ask, the last recorded close as the price-deviation reference, legs already sent today
from the ledger, equity and exposure from the account, the NYSE cash session) → eligibility (an
open needs the ledger flat and no untracked position on the instrument; a close needs the ledger's
position id and the account still holding it) → the live quote → the cost preview (opens) →
available cash ≥ amount (opens). Warnings never block: a trade against the rule's current signal
warns that the executor will undo it (below). A block verdict still returns the preview, never a
token.

**The token.** HMAC-SHA256 over the preview (client ref, request id, book, side, amount or
position, units, bid/ask, expiry) with a per-process random secret keyed by SHA-256 of the
operator token: valid for this `ui-api` process and this operator session only, 120 s to live,
accepted once. `client_ref` is `ui-<book>-<NY date>-<open|close>-<6 hex>`; the `x-request-id`
sent to eToro is `UUIDv5(client_ref)` as the executor's.

**Place.** Verifies the token, the TTL, the confirmation phrase (`PLACE NSDQ100` / `CLOSE NSDQ100`),
reason and who (the halt controls' rules), then under the controls' mutex and the executor's own
`ledger.jsonl.lock` (a running executor makes place answer 409; a console trade never interleaves
with a run) re-checks the halt file, the session, the ledger and the account; writes the
**intent first** (`intents/<client_ref>.json`, fsync), audits `trade.place requested`, sends
through the same `etorobroker` adapter (market, amount USD, x1; a close is the whole position;
the adapter polls up to 30 s and resolves a duplicate reference), appends the ledger line, audits
`done`. Errors: 401 token, 410 expired, 409 reused or lock held, 422 refused at the re-check
(halt, session closed, ledger changed), 502 eToro did not execute.

**The ledger line.** The executor's record shape with `source: "ui"` and
`operator: {by, reason}`; `decision` carries `bar_date = fill_date =` the New York date of the
trade, `signal: "manual"`, `prev_signal` = the rule's latest signal, `close` = the reference close
the check used; `paper` and `candles` are zero (the trade is outside the pre-registered paper
account); `demo` is what the executor writes (position before/after, account, quote, costs, the
risk check, the order with `fill_vs_mid_bps` and `fill_vs_close_bps`). It is keyed
`book|bar_date|client_ref`, so it never collides with the executor's `book|bar_date` line for the
same bar (written the next morning); `Ledger.Get(book, bar_date)` still returns the executor's
line only. The web ledger table shows it as `manual` with the operator.

**What the executor does next.** It plans from `LastDemoPosition`, which now orders by
`(bar_date, recorded_at)`: the console line of day D (recorded at D 11:00) is newer than the
run's bar D-1 line (recorded D 09:35) and older than its bar D line (recorded D+1 09:35). So a
manual position against the rule is **brought back to the rule's target at the next 09:35 run**
(closed when the rule says flat; re-opened when it says long and the operator closed) unless the
ledger is halted. The data-integrity check on recorded closes and the run coverage ignore console
lines (their `close` is a reference price, not a bar); `reconcile` and `etorohealth` count them
(positions, intents). **One leg per instrument per trading day is policy, whoever sends it**:
after the run's leg a console open on the same book is blocked for the day
(`max_orders_per_day`), and after a console leg the executor's planned order for that book is
blocked and recorded the next morning only if it falls on the same NY date (it never does: the run
is the next trading day). A close is a reducing order and is never trapped by the cap.

**Reading `ui-audit.jsonl`.** Next to the ledger, one JSON line per attempt, newest last.
`action` is `trade.prepare` or `trade.place`; `outcome` is `done` (prepare: allow; place:
executed), `refused` (prepare: block, with `error: "blocked: <reasons>"`; place: TTL, phrase,
re-check), `denied` (token or origin), `requested` (place, before the order) or `failed` (place,
after the order: `error` says what eToro answered). `ref` is the `client_ref`, the same on the
prepare line, the place lines, the intent file and the ledger line; `detail` is the trade in words
and, on `done`, the fill. The Trade page shows the `trade.*` tail; the Risk page's audit log shows
all of it.

**When a place fails after the intent.** The intent stays on disk and no ledger line is written:
`etoro-reconcile` reports `unfinished_intent` and the data-health page fails `etoro.intents` after
10 minutes. The executor resumes only its own `sma50-*` intents; a console intent is resolved by
hand: check the demo account (and the trading history), close an orphan position if one was
opened, then move the intent file out of `intents/` (keep it with the day's notes). If the order
did execute, add the ledger line by hand from the audit `failed` line and the position (never edit
past lines), or close the position and leave the ledger flat. The Trade page's outcome card says
which case it is.
