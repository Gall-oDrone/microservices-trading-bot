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

Crontab (the executor skips NYSE holidays itself). Since P6 (2026-10-17) `scripts/install-ops-cron.sh`
installs the entry inside its managed `mtb-ops` block, builds the three binaries above (plus
`etoro-risk-report`; `--rebuild-etoro` rebuilds them from the committed HEAD and keeps `.prev-<rev>`
copies), schedules the weekly risk report (§7) and writes `ETORO_EXECUTOR_S3_URI`,
`UI_API_ETORO_LEDGER`, `UI_ALERTS_ETORO_LEDGER` and `ETORO_RISK_REPORT_YAHOO` to `ops.env`
(`--no-etoro` opts out). It also removes the hand-installed `mtb-etoro` block of P3. By hand, the
job must run at 09:35 New York time; two forms, depending on the host's cron:

```cron
# cron with CRON_TZ support (cronie, systemd-cron):
CRON_TZ=America/New_York
35 9 * * 1-5  /path/to/repo/scripts/ops-run.sh etoro

# Debian/Ubuntu vixie cron (this ops host; what the installer writes): CRON_TZ is silently ignored,
# so list both UTC candidates and let only the one in the 09:xx New York hour through (correct across DST).
35 13,14 * * 1-5 [ "$(TZ=America/New_York date +\%H)" = 09 ] && /path/to/repo/scripts/ops-run.sh etoro
```

> [!WARNING]
> Check which form your cron honours (`crontab -l` after a day: the run log must show a
> 09:35 New York start). With the first form on a cron that ignores `CRON_TZ`, the job runs
> at 09:35 **UTC**, outside the cash session, and every day ends `needs the NYSE cash session`.

With `ETORO_EXECUTOR_S3_URI` set, every run copies the ledger directory to
`s3://<bucket>/daily-executor/etoro-demo/` (run log last) for the off-machine watchdog (§3.1);
`scripts/etoro-daily-executor-run.sh --upload-only` sends it by hand (first upload, or after an
`upload=failed`).

## 2. What a run does

`scripts/etoro-daily-executor-run.sh -demo` runs these steps for each instrument:

1. **Halt check.** It reads `<ledger dir>/risk-state.json`. A corrupt halt file exits 2 before anything else runs.
2. **Bars.** It reads eToro's live daily candles **twice** and keeps NYSE trading days only.
   - The last closed bar must be the latest closed trading day. Otherwise the run stops on stale data.
   - The two reads must agree on the signal and be within the product's tolerance of each other: **5 bps** for the index CFDs (frozen with the pre-registration), **15 bps** for the ETF bars since 2026-10-25 (see §4).
3. **Decision and paper account.** It applies the frozen rule and runs the paper account with the pre-registered costs (`cfdsim`).
4. **Reconcile before acting.** The ledger's position must match the account.
   - Any other position on the instrument stops the run. Nothing is sent. Since 2026-10-25 a position that a **sibling cell's** ledger holds (`-sibling-ledgers` / `ETORO_SIBLING_LEDGERS`, set by `ops-run.sh` for the three cells) is that cell's: left alone, noted on the day's line, never a reason to stop (§4, "Three cells, one account").
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
| `portfolio_lag` / `history_lag` warnings in a CLEAN report | eToro's portfolio or history caught up inside the reconcile's own settle wait (3 reads / 10 s and 10 reads / 45 s seen 2026-10-26). | Nothing. Since 2026-10-26 `ui-alerts` treats both as normal (they stay in the JSON report). |
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
| eToro ledger coverage | critical | From **10:00 New York** on a trading day, a book has no ledger line with that fill date (the run was missed, or stopped before recording: stale bars, outside the session, unknown order outcome). Looks back 3 trading days. | The day is recorded (a re-run in session). A gap that **cannot** be filled any more (the run's day is over: the executor never backfills) is acknowledged in `<ledger dir>/coverage-ack.json` (since 2026-10-26: `{"acknowledged":[{"book","fill_date","reason","by","at","ref"}]}`, `ref` = the pre-registration's §6 incident note). Acknowledged days are named in an OK status instead of paging every 12 h until they age out; a malformed file fails the check. |
| eToro executor run | critical | The newest `run-*.log` ended `exit≠0`, or has no `exit=` line 15 min after it started. | A later run ends `exit=0`. |
| eToro reconcile | critical | The newest `reconcile/<date>.json` has drift, and no clean `<date>.recheck.json` exists. | A clean re-check is saved, or the next day's report is clean. |
| eToro reconcile | warning | The report is clean but carries warnings other than `portfolio_lag`; or the day's run ended `exit=0` and left no report (the reconcile step failed). | — |
| eToro order sent but not recorded | critical | An intent is on disk for more than 10 min with no ledger line for its reference (the run died between the order and the ledger). | The executor is re-run and records it. |
| eToro trading halted | warning | `risk-state.json` says halted. Named together with any Bitso halt in the subject ("Trading halted (etoro-demo, stage)"). | The file is removed. |
| eToro halt file | critical | `risk-state.json` exists but is invalid (the executor exits 2 before anything runs). | Fixed or removed. |
| eToro ledger copy to S3 | warning | The newest run log says `upload=failed` (the off-machine watchdog cannot see that run). | The next run uploads, or `--upload-only` re-sends. |

A missed 09:35 run is therefore in your inbox by about 10:15 New York. **When the workstation is
off**, the ledger watchdog (`infrastructure/lambda/ledger-watchdog`, since P6 with
`etoro-demo=daily-executor/etoro-demo:nyse`) checks the S3 copy every hour: it pages when no run
log follows the last NYSE trading day's 09:35 New York run within 6 h (a missed Monday run pages
at about 15:35 New York), never on weekends or holidays, and once more when runs resume.

```bash
# what would be sent right now, without sending or saving state
cd services/ui-api && ./bin/ui-alerts -dry-run -etoro-ledger ../strategy-executor/etoro-daily-data/demo/ledger.jsonl
```

## 4. eToro behaviours the executor works around

- **Duplicate request id.** eToro rejects a reused `x-request-id`, and `orders:lookup?referenceId=` cannot find API orders. A resent open is therefore resolved to the position opened on the instrument since the intent's timestamp.
- **Portfolio lag.** The portfolio shows fills late: usually 1–3 s, but on 2026-10-12 it still showed no positions and the pre-trade cash balance 5 s after the second fill, and both positions 80 minutes later. The adapter polls for up to 30 s to resolve the fill; the reconcile waits for up to 60 s more (§2). The ledger's `demo.account` snapshot is the **pre-order** balance (`exposure` 0 before the first open); `position_after` is post-fill.
- **Trading-history lag.** The closed-positions history lists a close later than the portfolio drops it: on 2026-10-16 the portfolio no longer held the closed NSDQ100 position 10 s after the close, but the history did not list it yet (3 h later it did). The reconcile waits for the history too (§2).
- **Two versions of daily history** (seen 2026-10-10):
  - up to ~1.3 bps on 13–16 recent closes of the index CFDs, on every run since;
  - the ETF bars sit further apart: 9 SPY closes at 4.68 bps (10-22, 10-23) and QQQ at 6.4 bps
    (10-23, which stopped the rotation's run);
  - the executor reads twice and records `candles.revised_closes` / `max_revision_bps`;
  - recorded closes may move by up to the product's tolerance before the run stops: **5 bps for
    the CFDs** (frozen), **15 bps for the ETFs** (`revisionTolBpsETF`, 2026-10-25); the two reads
    must agree on the signal regardless, so the rule never acts on bars whose versions disagree
    about the position.
- **Three cells, one account** (2026-10-25). The CFD cell, the SMA50 ETF cell and the dual-momentum
  cell share the demo account, and the last two trade the same ETFs. Each cell's run and the
  reconcile after it get the other cells' ledgers in `ETORO_SIBLING_LEDGERS` (set per job in
  `scripts/ops-run.sh`; the flag is `-sibling-ledgers` on both binaries, read only): a position a
  sibling's ledger currently holds is reported as the sibling's (`demo.notes` on the line,
  `sibling_positions` / `sibling_note` in the reconcile JSON), never as an orphan on this cell's
  instrument nor as `other_instruments`. A position **no** ledger holds is still `resolve by hand`.
  Before this, 2026-10-22 the rotation refused to run because the ETF cell held SPY, and every
  reconcile warned about the other cells' positions twice a day through `ui-alerts`.
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

**P6 installed 2026-10-18 01:50 UTC.** `install-ops-cron.sh` now owns the eToro cron entry (the
hand-installed block is gone; schedule and guard unchanged) and the Monday risk-report job; the
ledger directory's first copy went to `s3://…/daily-executor/etoro-demo/` (`--upload-only`), the
watchdog Lambda was redeployed with `etoro-demo=…:nyse` (dry run: quiet, newest run Friday 13:35,
due Friday), `ui-alerts` gained the `etoro.upload` check. The eToro binaries were not rebuilt. The
`.prev` copies of the executor and reconcile are retired (deleted) by the operator after one clean
week on the 2026-10-16 binaries, i.e. after Friday 2026-10-23's run.

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
`risk.md` (also printed). `scripts/ops-run.sh etoro-risk` runs it every **Monday 15:00 UTC**
(installed by `install-ops-cron.sh`): after the week's first run, so the newest candle file ends on
Friday and the report covers the whole previous week; the output directory is copied to
`$ETORO_EXECUTOR_S3_URI/risk/<date>/`, log in `~/.local/state/mtb-ops/etoro-risk.log`. How to read
it, and the first run's numbers, are in
[`docs/risk/MODEL-VALIDATION-ADDENDUM-ETORO-2026-10-13.md`](../risk/MODEL-VALIDATION-ADDENDUM-ETORO-2026-10-13.md).

## 8. Assisted trades from the console (P5d, 2026-10-16)

The operator UI's **Trade** page (`web/`, route `/trade?ledger=etoro-demo`) can open or close a
demo position by hand through `ui-api`: `POST /api/ui/etoro/trades/prepare` runs the checks and
returns a preview with a two-minute token, `POST /api/ui/etoro/trades/place` sends it. It is the
porting plan's §4.4 "assisted mode" (prepare → place, as the eToro MCP does), demo only. Since
2026-10-19 an open **carries a stop loss** (and an optional take profit) and a third side,
`protect`, edits the stop / target of the ledger's held position; §9 has the model and the levels,
this section the flow they share.

**How it is enabled.** Off by default. `ui-api` needs all of `-etoro-trading`
(`UI_API_ETORO_TRADING=1`), `-operator-token-file` (the same 0600 token the halt controls use),
`-etoro-ledger` (this ledger, a local file) and `-etoro-env-file` naming the demo env file with
`ETORO_ENV=demo`; a missing prerequisite is fatal at start, and `ETORO_ENV=real` is refused in
code. `GET /api/ui/controls?ledger=etoro-demo` reports `trading: {enabled, disabled_reason}`, and
the Trade page shows the reason when it is off. The same gates as the halt controls apply to every
call: loopback `Origin`, `Authorization: Bearer <operator token>`, a JSON body (unknown fields
rejected, 4 KB cap), and an audit line for every attempt.

**The checks (prepare), in order, each a reason in the block verdict:** the halt file
(`risk-state.json`; an invalid file blocks too, as it stops the executor) → the protection levels
(§9; an infeasible level is a reason) → `risk.Check` with `risk.EtoroDemoProtectedPolicy`
(`etoro-demo-2026-10-19`: the executor's policy plus `stop_loss_required`, a stop between the noise
floor and 10 %, at most 0.5 % of equity at risk) on the same order and state the executor builds
(units from the amount and the live ask, the last recorded close as the price-deviation reference,
legs already sent today from the ledger, equity and exposure from the account, the NYSE cash
session; a `protect` is not an order, so only the halt applies to it) → eligibility (an open needs
the ledger flat and no untracked position on the instrument; a close or a protect needs the
ledger's position id and the account still holding it) → the live quote → the cost preview (opens)
→ available cash ≥ amount (opens). Warnings never block: a trade against the rule's current signal
warns that the executor will undo it (below); a protect whose stop sits above the rule's SMA50, or
below the stop the position already has, warns too. A block verdict still returns the preview,
never a token.

**The token.** HMAC-SHA256 over the preview (client ref, request id, book, side, amount or
position, units, bid/ask, the stop and target rates and the profile, expiry) with a per-process
random secret keyed by SHA-256 of the operator token: valid for this `ui-api` process and this
operator session only, 120 s to live, accepted once. `client_ref` is
`ui-<book>-<NY date>-<open|close|protect>-<6 hex>`; the `x-request-id` sent to eToro is
`UUIDv5(client_ref)` as the executor's.

**Cancel (2026-10-21).** `POST /api/ui/etoro/trades/cancel` with `{token, reason?, by?}` burns a
prepared trade's token before it expires: same gates as place, under the controls' mutex, the
token id goes into the same single-use set place consults (so a cancel and a place of one token
cannot both succeed; the loser is 409 "already used: it was placed or cancelled"). Nothing reaches
eToro and the ledger is unchanged; one audit line `trade.cancel` / `done` with `ref` =
`client_ref` and `detail` "… -> cancelled: token burned, nothing sent". An expired token answers
200 `status: "expired"` (nothing to burn, still audited). No phrase: a cancel changes nothing at
the venue. In the console: the **Cancel · nothing sent** button on a live preview, or **Escape**
while the place dialog is closed.

**Place.** Verifies the token, the TTL, the confirmation phrase (`PLACE NSDQ100` / `CLOSE NSDQ100`
/ `PROTECT NSDQ100`), reason and who (the halt controls' rules), then under the controls' mutex and
the executor's own `ledger.jsonl.lock` (a running executor makes place answer 409; a console trade
never interleaves with a run) re-checks the halt file, the session, the ledger and the account;
writes the **intent first** (`intents/<client_ref>.json`, fsync), audits `trade.place requested`,
sends through the same `etorobroker` adapter (market, amount USD, x1, with `stopLossRate` /
`takeProfitRate` on an open; a close is the whole position; a protect is one
`PATCH /positions/{id}` and the adapter polls the portfolio until the rates show; the adapter polls
up to 30 s and resolves a duplicate reference), appends the ledger line, audits `done`. Errors: 401
token, 410 expired, 409 reused or lock held, 422 refused at the re-check (halt, session closed,
ledger changed), 502 eToro did not execute, 501 the broker cannot edit a stop.

**The ledger line.** The executor's record shape with `source: "ui"` and
`operator: {by, reason}`; `decision` carries `bar_date = fill_date =` the New York date of the
trade, `signal: "manual"`, `prev_signal` = the rule's latest signal, `close` = the reference close
the check used; `paper` and `candles` are zero (the trade is outside the pre-registered paper
account); `demo` is what the executor writes (position before/after, account, quote, costs, the
risk check, the order with `fill_vs_mid_bps` and `fill_vs_close_bps`). Since 2026-10-19
`position_after` carries `stop_loss_rate` / `take_profit_rate` / `stop_type` and an open or a
protect adds `demo.protection` (§9). It is keyed `book|bar_date|client_ref`, so it never collides
with the executor's `book|bar_date` line for the same bar (written the next morning);
`Ledger.Get(book, bar_date)` still returns the executor's line only. The web ledger table shows it
as `manual` with the operator.

**Where to see what became of a trade (2026-10-19).** Start with the console's **Today** page
(`/today?ledger=etoro-demo`, 2026-10-20): the run per book, every open position against its stop and
target (distance in % and ATR), the most that can be lost at the stops, and what needs you. The
detail is the **Positions & round trips** card on the
Trade page (every book) and on a book's forward-test page (that book), behind
`GET /api/ui/etoro/positions?ledger=etoro-demo[&book=…]`: one row per position id, paired from the
ledger's open / protect / close / `venue_close` lines whoever wrote them. A row is `open` (opened
NY time, open fill, units, amount, stop / target, source `rule` or `manual` with the operator and
reason, days held) or `closed` (close fill, who closed it: `rule`, `operator`, `eToro · stop` /
`eToro · target`, `outside the ledger`). Two layers, named on the card: the **ledger** gives the
fills, the gross P&L `units × (close − open)` and the financing eToro had charged at the last run;
the **account** (only when ui-api runs with `-etoro-trading`, so it has the demo broker; three
reads, cached 45 s per ledger) gives the open rows' unrealised P&L and fees to date (the mark shown
is implied from it), the account's cash / equity, and for closed rows eToro's trading history:
`net profit` (price P&L, spread included) and `fees` (financing), `net = net profit − fees`, marked
*eToro* when confirmed, *gross, fills* until the history lists it (it lags minutes to hours). A
ledger position the account no longer holds is looked up in the history: at the stop or the
target it reads `closed · unrecorded` by eToro, elsewhere `outside the ledger`, not listed yet
`unknown` — all three are the §9.4 by-hand cases until Phase C. Click a row for the fills' quality
(bps vs mid), the cost preview, both protection views (ledger and account; `!` when they differ),
the client refs and the notes. The audit log on the same page is the attempts, not the trade: it
never shows prices or P&L.

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

## 9. Protection: stop-loss / take-profit (console 2026-10-19; executor phase C built 2026-10-19)

Design and status: [ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md](ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md)
(phases A, B and C landed 2026-10-19; D is this section, the forward-test addendum and the
evidence that comes with the first live PATCH). The short version for the operator:

**What holds.** A position opened from the console carries a resting stop loss at eToro (and a
take profit when one was chosen); a position the console `protect`ed does too. **With the phase C
binaries, the executor owns protection** (`-protect`, on by default): a rule open carries the
disaster stop `max(4 × ATR %, 8 %)`, every 09:35 run with a held long raises the stop when the
close minus k × ATR has moved up by at least 0.25 × ATR (never down, never the target), and a
position eToro closed on its own is read from the trading history and recorded as a `venue_close`
line instead of stopping the run. **Until the binaries on the ops host are rebuilt (§9.6) the
running executor is the 2026-10-16 build:** rule positions carry no stop and a venue close is
still the `resolve by hand` case of §3 and §5. Nothing in this section changes what §2–§5
describe about the rule, the paper account or the pre-registration.

### 9.1 The levels (shared/pkg/stops)

- **Volatility unit:** ATR(14), Wilder smoothing, on the closed daily bars the executor saves under
  `candles/` (`<book>_daily_<date>.csv`). The regime label compares today's ATR % with its 252-day
  median (`calm` < 0.75×, `normal`, `elevated` ≥ 1.25×, `extreme` ≥ 1.75×; `unknown` on short
  history); it is context, it changes no level.
- **Risk-aversion profiles**, as ATR multiples of stop / target distance: **Conservative 1.5 /
  2.25**, **Moderate 2.0 / 4.0 (default)**, **Aggressive 3.0 / 6.0**; **Custom** takes typed
  percentages. Levels are measured from the ask on an open and from the position's open rate on a
  protect; the mark (bid) must be above the stop and below the target or the level is infeasible
  (a block reason).
- **Guards, in order, each one reported in `clamps` so the operator sees what moved and why:**
  (1) noise floor: the stop is never tighter than `max(venue minimum, 2 × spread + 0.25 %, policy
  minimum)`; (2) policy caps: never wider than 10 % of the entry nor more than **0.5 % of equity**
  at risk; (3) venue bounds: eToro's maximum stop (100 %) and target (1000 %) distances; (4) tick
  rounding, the stop **down** and the target **up** to 0.01. A floor above the ceiling is infeasible.
- **Venue bounds** come from eToro's eligibility (cached an hour); when it cannot be read the
  NSDQ100 x1 long figures of 2026-10-10 apply (minimum 1 %) with a warning (`bounds_source:
  fallback`). Without daily bars (no ATR) a named profile becomes a **fixed 8 % stop and no
  target**, with a warning.
- A take profit is optional. An open that names no profile gets **Moderate** and a warning says so;
  the policy refuses an open without a stop (`stop_loss_required`).

### 9.2 Console flow

- `POST /api/ui/etoro/trades/recommend` `{book, side: open|protect, amount_usd | position_id}`
  answers the three profiles with the ATR, the regime, the bounds and their source, the policy
  caps, the rule's SMA50 and, for a protect, the position's current rates (`before`). It sends
  nothing and shares the trade gates (trading on, loopback Origin, bearer operator token).
- `prepare` takes `profile` (`conservative|moderate|aggressive|custom`) and, for custom,
  `stop_loss_pct` / `take_profit_pct` (fractions of the entry); the preview's `protection` block is
  what `place` will send, bound into the token.
- The Trade page shows a **Protection** panel in the manual form (profile picker, the stop / target
  / max loss / gain / R:R rows with their ATR distances, the volatility badge, the clamp notes), a
  `protect` side, a `no stop` / `protected` badge on each proposal with a *Prepare protection*
  button for a held position without a stop, the protection facts on the preview, and the stop /
  target on the outcome card.

### 9.3 What the 09:35 run does with stops (phase C, `cmd/etoro-daily-executor/protect.go`)

- **C1, a rule open carries the disaster stop.** `SL % = max(4 × ATR %, 8 %)` of the ask, through
  the same guards as the console (noise floor, the policy's 10 % cap and 0.5 % of equity, the
  venue's bounds or the 2026-10-10 fallback, tick rounding down), no take profit: the rule exits on
  its signal. The open is risk-checked **with** the stop under `EtoroDemoProtectedPolicy`
  (`etoro-demo-2026-10-19`, now the executor's default policy); an infeasible stop leaves the order
  bare and `stop_loss_required` blocks the day (recorded as `blocked`). At the 2026-10-19 ATR
  (1.7 % NSDQ100, 0.9 % SPX500) the 8 % floor decides: about 88 USD at risk per 1,100 USD leg.
- **C2, the daily ratchet.** At a run that holds a long and plans no order, the account's resting
  rates are first copied onto the ledger (a self-heal: a stop the ledger never learned, or lost, is
  noted). Then `candidate = decision close − k × ATR`, k = 4 for a rule position (also for a
  position opened before phase C: it gets its first stop this way, `profile: disaster`) and the
  recorded profile's multiple for a console position (1.5 / 2.0 / 3.0; a **custom** console stop
  is left as the operator set it). The candidate is kept below the bid by the noise floor and
  rounded to the tick; it is sent (`PATCH`, ref `sma50-<book>-<fill date>-protect`) only when it is
  **≥ 0.25 × ATR above the held stop**. Never lower, never the take profit. The day is recorded as
  `action: protect` (not a leg). A refused or unconfirmed edit is recorded with the error, the
  position keeps its previous stop, and the run exits 1 so `etoro.run` fails until the next clean
  run.
- **C3, a venue close.** When the ledger says long and the account does not hold the position, the
  run reads the trading history. Listed: the day is recorded as `action: venue_close`, position
  flat, with the history's fill, P&L and fees and the kind: close rate at or **through** the stop
  → `stopped_out` (the gap in bps is noted), at or beyond the target → `took_profit`, strictly
  between the two → `venue_close` (closed in the app, or a margin close-out). The history's rates
  win over the ledger's. Not listed yet (eToro lagged a close by up to 3 h on 2026-10-16): nothing
  is recorded, the run exits 1 with `re-run later today`, and the wrapper's next attempt or a
  by-hand `ops-run.sh etoro` records it. **No re-entry in the same run** (a deviation from the plan,
  stated in `protect.go`): one line and one action per decision day; if the rule still says long
  the next trading day's run opens from flat, with a stop.
- **Reconcile (C4)** verifies a `venue_close` line against the history like a close of ours and
  adds `protection_drift` (drift: the ledger recorded a stop, the account holds none) and
  `protection_unrecorded` (warn: the account's stop differs from or is missing in the ledger; the
  next run copies it). **Alerts (C5)**: `etoro.protection` warns when a held position has carried
  no stop on the ledger for more than 30 minutes (since 2026-10-25 a position of an ETF cell,
  `paper.product` `etf`, is listed as "no stop overlay by design" and does not warn: §10 runs
  without the overlay); `etoro.stop_out` reports a venue close of the
  last two trading days (informational). **Risk report (C6)**: section 6 lists held stops against
  the mark and the ATR, every edit, every venue close with the realised result next to the loss
  the stop modelled.

### 9.4 Reading a protection line

- An **open** line (`demo.action: open`) now has `position_after.stop_loss_rate` (and
  `take_profit_rate`, `stop_type: fixed`) and `demo.protection: {profile, atr, atr_pct,
  max_loss_usd, reason}`; `demo.risk.order` carries `stop_loss` / `take_profit` and the policy
  version `etoro-demo-2026-10-19`. The executor's opens say `profile: disaster`.
- A **protect** line (`demo.action: protect`, `demo.order.kind: protect`, `status:
  executed|pending`, `order_id` = eToro's `operationId`) is **not a leg**: `LegsOn` and the
  one-leg-per-day cap ignore it; `position_before` has the rates the account showed,
  `position_after` the new ones (eToro's when the portfolio showed them; the ones sent when it was
  still `pending` after the adapter's wait, with a note); `demo.protection.stop_loss_before` /
  `take_profit_before` keep the old rates. The executor's say `profile: ratchet` (or `disaster`
  for a first stop) and `reason: daily ratchet: close C − k × ATR A`. Units and amount are
  unchanged. A console protect has `source: "ui"` and the operator.
- A **venue_close** line (`demo.action: venue_close`, `demo.order.kind: venue_close`, no client
  ref of ours, `avg_price` = the history's close rate, `fees` = the history's) has
  `position_after: flat` and `demo.venue_close: {kind, stop_loss_rate, take_profit_rate,
  close_rate, gap_bps, closed_at, net_profit, fees}`; `demo.notes` says in words why it was
  classified as it was. `decision` is the day's rule decision as usual; the paper block is
  untouched (the paper account never had a stop).
- A **hold** line on a protected position now carries the account's current rates on
  `position_after` and, when the ratchet did not move, a note saying how far the candidate was.
- `demo.protection.max_loss_usd` is `units × (open rate − stop)`: the most the position can lose at
  the stop before gap and fees.

### 9.5 Still by hand

| What you see | What happened | What to do |
|---|---|---|
| Run log `… the trading history does not list it yet …: re-run later today`, exit 1, no line for the day | eToro closed the position and its history lags (minutes to hours) | Nothing until the afternoon: `scripts/ops-run.sh etoro` again (the wrapper's second UTC slot already does this on some days); it records the `venue_close` line. Still absent by the close: check the portal's history and add the line by hand from it (never edit past lines) |
| Run log `account holds position(s) … besides the executor's position …` | A second position on the instrument the ledger does not know (opened in the app) | Close it on the portal or record it; the executor never touches an untracked position |
| `protection_drift` in reconcile, or `etoro.protection` warning on a position the ledger shows protected | The venue dropped or never applied the stop | The next 09:35 run sets it again (ratchet from `current = none`); to protect it now, the console's Trade page |
| Run log `protect … not confirmed … (recorded; the position keeps its previous stop …)`, exit 1 | eToro refused or did not confirm the PATCH | Read the error in the day's line (`demo.order.error`); the position still has its previous stop (or none). Re-run today to retry, or protect from the console |
| `prepare` blocks with `the account no longer holds position …` on a protect or close | eToro closed it and the executor has not run yet | Wait for the 09:35 run (or run it by hand); it records the venue close |

### 9.6 Switching phase C on (operator)

The cron executor runs the binaries under `etoro-daily-data/`; code changes reach it only when they
are rebuilt. Phase C is on by default in the code (`-protect`, `ETORO_EXECUTOR_PROTECT=0` or
`-protect=false` turns it off; `ETORO_EXECUTOR_DISABLED=1` still stops the run entirely).

```bash
# ops host: rebuild etoro-daily-executor, etoro-reconcile and etoro-risk-report from the committed
# HEAD (previous binaries kept as .prev-<rev>); the installer rebuilds ui-alerts (the C5 checks) too
scripts/install-ops-cron.sh --rebuild-etoro
# dry run of the new executor against the live ledger: nothing recorded, no order, prints "protect on"
(cd services/strategy-executor && etoro-daily-data/etoro-daily-executor -no-record)
```

Before the first run on the new binaries read the forward-test addendum
([FORWARD-TEST-ADDENDUM-DISASTER-STOP-2026-10-19.md](../backtest-readiness/FORWARD-TEST-ADDENDUM-DISASTER-STOP-2026-10-19.md)):
the disaster stop is a risk-policy overlay on the demo account, outside the pre-registered rule;
the paper account stays the judged series. Evidence to keep when it happens (plan phase D):
the first PATCH request and response on demo, the first ratchet line, the first stop-out, under
`docs/etoro/evidence-<date>/`.


## 10. The ETF cell: QQQ / SPY at x1 (frozen 2026-10-21)

The same binary runs a second, independent forward test,
[`FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md`](../backtest-readiness/FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md):
the frozen SMA50 rule on the ETFs **QQQ (3006)** and **SPY (3000)**, x1, settlement *real*, 1,100 USD
each, window 2026-10-22 → 2027-10-21. What differs from the CFD cell:

| | CFD cell (§1–9) | ETF cell |
|---|---|---|
| Instruments | `-instruments NSDQ100,SPX500` (default) | `-instruments QQQ,SPY` |
| Ledger | `etoro-daily-data/demo/` | **`etoro-daily-data/demo-etf/`** — never the same directory; `LegsOn`, the halt file, intents, candles and reconcile reports are per ledger |
| Paper costs (`paper.product`) | `cfd`: spread 15.5 / 14.8 bps RT, 2.3 bps per night | `etf`: **15 bps of notional per order** (`paper.fee_bps_per_order`), 0.28 / 0.26 bps bid/ask, no financing |
| Protection | disaster stop + daily ratchet (§9) | **none**: the executor skips `disasterStop` and `ratchet` for `Kind == etf` until a stop overlay is decided in its own addendum |
| Bars | 0 missing trading days | **28 missing trading days per ETF** in the live route (mostly Fridays); each gap is shorter than `dailyrule.MaxGapDays`, so the SMA never restarts. `candles.missing_trading_days` records it every run |
| Demo fees | financing previewed, **not charged** on demo | commission previewed (1.65 USD), **not charged** on demo ([`evidence-2026-10-21/fees.md`](evidence-2026-10-21/fees.md)) |

Cron: `scripts/install-ops-cron.sh` schedules `scripts/ops-run.sh etoro-etf` at **09:38 New York** on
weekdays (three minutes after the CFD run, so the two never share a minute of eToro's write budget),
with the same UTC guard as the CFD line, and writes `ETORO_ETF_EXECUTOR_S3_URI`
(`s3://<bucket>/daily-executor/etoro-demo-etf`) to `ops.env`. The job sets
`ETORO_EXECUTOR_LEDGER=etoro-daily-data/demo-etf/ledger.jsonl` and passes `-demo -instruments QQQ,SPY`
to the same wrapper. Log: `~/.local/state/mtb-ops/etoro-etf.log`. The off-machine watchdog
(`infrastructure/lambda/ledger-watchdog`) watches the ETF and rotation prefixes since its redeploy of
2026-10-21 (`--ledgers … ,etoro-demo-etf=daily-executor/etoro-demo-etf:nyse,etoro-demo-rotation=daily-executor/etoro-demo-rotation:nyse`),
and `ui-alerts` checks all three eToro ledgers (`UI_ALERTS_ETORO_LEDGER` as a `name=path` list).

By hand: `go run ./cmd/etoro-daily-executor -demo -instruments QQQ,SPY -ledger ./etoro-daily-data/demo-etf/ledger.jsonl`.
The reconcile, risk report and fees check take the ETF ledger with `-ledger`. The weekly execution
report for this cell is a separate dated file that links the pre-registration, as for the CFD cell.

**Draft specs.** An instrument whose pre-registration is not frozen can be added to `draftSpecs`
(`decide.go`) and decided in dry-run mode with `-allow-draft`; `-demo` refuses it in two places. The
ETF cell lived there for one dry run on 2026-10-21 (ledger `etoro-daily-data/draft-etf/`, two lines,
not continued) before its freeze moved it to `frozenSpecs`.

## 11. The dual-momentum cell: `-strategy dm_12_1` (frozen 2026-10-21, executor built the same day)

[`FORWARD-TEST-PREREGISTRATION-DUAL-MOMENTUM-ETF-2026-10-21.md`](../backtest-readiness/FORWARD-TEST-PREREGISTRATION-DUAL-MOMENTUM-ETF-2026-10-21.md):
one position at a time across **QQQ / SPY / cash**, decided on **month ends only** (12-1 momentum,
0 % hurdle), window 2026-11-02 → 2027-10-29. `-strategy dm_12_1` makes the executor take the two
books **together** (`rotation.go`):

| Step | What differs from the daily rule |
|---|---|
| Decide | both books' bars are read twice, aligned on common dates (`rotation.Align`), checked fresh **on each book**, and the latest aligned bar must be every book's latest bar; `rotation.Evaluate` gives one joint target. Both reads must agree on target and previous target |
| Per-book signal | `long` for the target book, `flat` for the others; `Decision.rule = "dm_12_1"`, `decision_day`, `momentum` (per symbol, at the latest decision) and `target` are recorded on every book's line |
| Order of books | the book the rule leaves (sell) first, holds next, the buy last, so a switch frees the cash before it is reused; one failed book does not stop the others and the next run re-plans each from its ledger position (the target is idempotent, which is also how a missed day is caught up) |
| Before the window | until the fill date reaches `ForwardStart` every book's signal is flat (pre-registration §2: the paper account is in cash until its first in-window fill), so `-demo` from day one opens nothing before **2026-11-02** |
| Paper | the **joint** account from `cfdsim.SimulateRotation` with the ETF costs, against holding SPY (the H2 benchmark); identical on both books' lines; `position` is the asset held through the last close, `pending_action` is `buy X` / `sell X` / `switch X -> Y` / `hold` |
| Risk policy | `EtoroDemoETFPolicy` (`etoro-demo-etf-2026-10-21`): books `qqq` / `spy`, 1,500 USD per leg, one leg per book per day, no stop requirement. **Selected automatically for any ETF-only run** (the ETF cell too); the CFD policies are unchanged and a run mixing products is refused |
| Ledger | **`etoro-daily-data/demo-rotation/`**, never shared: a book under another pre-registration in the same ledger is refused (`checkLedgerPrereg`), both ways |
| Protection | none (ETF) |

Cron: `install-ops-cron.sh` schedules `ops-run.sh etoro-rotation` at **09:41 New York** (log
`~/.local/state/mtb-ops/etoro-rotation.log`, upload `ETORO_ROTATION_EXECUTOR_S3_URI`). By hand:
`go run ./cmd/etoro-daily-executor -demo -strategy dm_12_1 -ledger ./etoro-daily-data/demo-rotation/ledger.jsonl`
(`-instruments` is ignored: the books are the pre-registration's). Expected first order: **2026-11-02**,
from the 2026-10-30 decision; the state at registration has QQQ in force.

> [!NOTE]
> The same day's dry run against live bars read `target QQQ (was QQQ) | momentum QQQ +25.50% SPY +19.08%`,
> matching the pre-registration's state at registration, and the ETF-policy fix it exposed
> (`qqq` / `spy` fell to the refuse-everything default of the CFD policy) applies to the ETF cell's first
> demo run as well.

## 12. Weekly verdicts: `scripts/etoro-prereg-evaluation.sh` (2026-10-21)

The eToro sibling of `scripts/prereg-evaluation.sh`: it runs the procedure of each of the three
pre-registrations (§5 of each file) on the **bars the executors saved** (the newest
`<book>_daily_<date>.csv` per book under `etoro-daily-data/{demo,demo-etf,demo-rotation}/candles/`,
the judged series), fetching the Yahoo benchmarks into its output unless `--no-fetch`, with every
cost flag as frozen, and writes `cfd/`, `etf/` and `rotation/` (`forward.txt`, `forward.json`, the
rotation's `state.json`), the bars used and `SHA256SUMS`. No eToro key; nothing written to a
ledger. `ops-run.sh etoro-prereg` runs it on Mondays at 15:10 UTC into
`etoro-daily-data/prereg/<date>-<phase>` (eight newest as-of reports kept; the interim run once
on/after 2027-04-12 and the final once on/after 2027-10-29 are never pruned). The interim and
primary dates per cell are in the pre-registrations; an as-of report after a cell's window end
already carries that cell's complete figures. Cells whose executor has not run yet are skipped
with a note.

## 13. The console's API as a service: `scripts/install-ui-api-service.sh` (2026-10-21)

Until now `ui-api` was started by hand (`go run ./cmd -operator-token-file … -etoro-ledger …
-etoro-trading`) and died with the shell. The installer makes it a `systemd --user` unit,
`mtb-ui-api.service`, so it survives a logout, a crash (`Restart=on-failure`) and a reboot
(linger is on for this user; the script says so if it is not):

```
scripts/install-ui-api-service.sh              # go build -> services/ui-api/bin/ui-api, write, enable --now
scripts/install-ui-api-service.sh --print      # show the unit and the env file, change nothing
scripts/install-ui-api-service.sh --no-trading # read-only console
scripts/install-ui-api-service.sh --uninstall
systemctl --user status mtb-ui-api | journalctl --user -u mtb-ui-api -f
```

Settings come from two files under `~/.config/microservices-trading-bot/`: `ops.env`, which
`install-ops-cron.sh` keeps current (`UI_API_LEDGER`, `UI_API_ARCHIVE` and `UI_API_ETORO_LEDGER`
= `etoro-demo`, `etoro-demo-etf`, `etoro-demo-rotation` as `name=path`), and `ui-api.env`
(written once, 0600: `UI_API_ADDR=127.0.0.1:8090`, `UI_API_OPERATOR_TOKEN_FILE`,
`UI_API_ETORO_TRADING=1`). The eToro keys stay in `etoro-demo.env`, read by the binary itself
(`ETORO_ENV=demo` enforced). The first eToro ledger (`etoro-demo`) is the default of the eToro
routes, the rates feed's closes and the assisted trades; the ETF cells are reached with
`?ledger=`. After a `git pull`, re-run the installer to rebuild; a plain
`systemctl --user restart mtb-ui-api` restarts the binary already built. The web app itself is
still Vite (`cd web && npm run dev`), proxying `/api` to `:8090`.

## 14. Real-account canary, measurement only (built 2026-10-22; nothing placed yet)

The plan is [ETORO-REAL-CANARY-PLAN-2026-10-21.md](ETORO-REAL-CANARY-PLAN-2026-10-21.md)
(decisions D1–D6 taken 2026-10-22: **B**, NSDQ100 x20, 50 USD invested → 1,000 USD exposure, max
loss at the stop 23–24.5 USD). This section is the operating procedure, as built. Everything the
plan's §5 promised is in code; the real account has **not** been touched: no key exists yet, no
acknowledgement file, no unit installed, no order. The demo cells, their unit (`mtb-ui-api`,
§13), their cron lines and their ledgers are unchanged.

**What stays refused.** `etoro-daily-executor` still refuses `ETORO_ENV=real`. The demo `ui-api`
still refuses a real env file. Only a `ui-api` started with `-etoro-real-canary` **on the command
line** that passes the six-part acknowledgement builds a real broker, and the only thing a cron
ever runs against the real account is the read-only observer (`ops-run.sh etoro-canary-observe`).

### 14.1 The two consoles side by side

| | Demo (unchanged) | **Real canary** |
|---|---|---|
| `ui-api` | `127.0.0.1:8090`, unit `mtb-ui-api` | **`127.0.0.1:8091`**, unit **`mtb-ui-api-real-canary`** (same binary, `ExecStart=… -etoro-real-canary`, `Wants=mtb-ui-api.service`) |
| unit env file | `ops.env` + `ui-api.env` | **`~/.config/microservices-trading-bot/ui-api-real-canary.env`** only |
| eToro keys | `etoro-demo.env` (`ETORO_ENV=demo`) | **`etoro-real-canary.env`** (`ETORO_ENV=real`, its own key, 0600) |
| operator token | `~/.config/mtb/operator-token` | **`~/.config/mtb/operator-token-real-canary`** (a different value; the start compares hashes) |
| acknowledgement | — | **`~/.config/microservices-trading-bot/REAL-ACCOUNT-ACK`** (0600; §14.2) |
| ledger | `etoro-daily-data/demo/` | **`etoro-daily-data/real-canary/`**: `ledger.jsonl`, `intents/`, `risk-state.json`, `ui-audit.jsonl`, **`observations.jsonl`**, `run-<UTC>.log`, `candles/` (written by the observer, not an executor) |
| ledger name = halt phrase | `etoro-demo` | **`etoro-real-canary`** |
| policy | `etoro-demo-2026-10-19` | **`etoro-real-canary-2026-10-22`**: nsdq100 only, x20 only, invested 50–60 (default 50), exposure ≤ 1,200, one NSDQ100 position (D7: other instruments' holdings are warned about, never touched), stop required at 2.3–2.45 % of the price (the venue caps it at 2.5 % = 50 % of invested at x20; the policy stays just inside so a fill above the ask cannot push it past), one order per day, cash session, equity caps off |
| phrases | `PLACE NSDQ100` … | **`PLACE REAL NSDQ100`**, **`CLOSE REAL NSDQ100`**, **`PROTECT REAL NSDQ100`** |
| client_ref | `ui-nsdq100-…` | **`ui-real-nsdq100-…`** |
| web | `:5173` → `:8090` | **`npm run dev:real-canary`** = `:5174` → `:8091`; red top bar, `REAL ·` title, red accent, `· REAL` on the ledger badge; the Trade page shows the live quote with its age |
| rates poll | 5 s (12 reads/min) | **2 s** (`UI_API_ETORO_LIVE_INTERVAL=2s`, the floor: 30 reads/min leaves the key room for prepare under eToro's 60/min; the Public API has no tick stream) |
| cron | 09:35 / 09:38 / 09:41 NY executors | **none that trades**; `etoro-canary-observe` at 18:00 NY every day (opt-in, `install-ops-cron.sh --canary-observe`) |
| S3 | `daily-executor/etoro-demo*` | **`daily-executor/etoro-real-canary`** (observer's copy; D4) |
| watchdog | `:nyse` calendar | the same Lambda, plain daily calendar on the observer's run logs (no code change; §14.5) |

The ledger lines keep schema `etoro-ledger/v1`: the JSON key is still `demo`, with `"env":
"real"` inside it (`etoroledger.ModeReal`; `Record.IsAccount()` is what readers test).

### 14.2 Pre-flight (once; what only you can do is marked **you**)

1. **you** — Fund the real account with the canary budget only (60 USD; check your region's
   minimum first deposit, conversion and withdrawal fees first). 2FA on. *Done 2026-10-22: 60 USD,
   converted from MXN at eToro's 0.0518 USD/MXN (≈ 1,158 MXN); note the spread vs the interbank
   rate in `SUMMARY.md`.*
2. **you** — Create a **new API key** for the canary with trading scope. Write the env file,
   0600, and paste the key **nowhere else** (not in a chat, not in a shell history: use an editor).
   *2026-10-22: a key pair was pasted into the implementation chat by mistake; it is to be treated
   as compromised, revoked at eToro, and never used by the canary. The chat transcript is stored on
   disk; nothing in the repo or in `~/.config` received it.* Give the new key the longest expiry
   eToro allows: the ack's end date must stay ≤ expiry − 1 day and M1/M2 need weeks, not days:
   ```
   umask 077
   $EDITOR ~/.config/microservices-trading-bot/etoro-real-canary.env
     ETORO_ENV=real
     ETORO_PUBLIC_KEY=<the x-api-key>
     ETORO_PRIVATE_KEY=<the x-user-key>
   ```
3. **you** — A second operator token, different from the demo one:
   `umask 077; openssl rand -hex 32 > ~/.config/mtb/operator-token-real-canary`.
4. **you** — The acknowledgement file, 0600. The first non-comment line is the sentence,
   verbatim; then `key: value` lines; dates `YYYY-MM-DD`, the end date inclusive and ≤ the key's
   expiry minus one day (D5); `key_prefix` is the first 8–16 characters of the **public** key
   (`cut -c1-12`), never the key:
   ```
   umask 077
   $EDITOR ~/.config/microservices-trading-bot/REAL-ACCOUNT-ACK
     I accept orders on the eToro REAL account for the measurement canary only
     key_prefix: <first 8-16 chars of ETORO_PUBLIC_KEY>
     ledger: /abs/path/to/services/strategy-executor/etoro-daily-data/real-canary/ledger.jsonl
     policy: etoro-real-canary-2026-10-22
     start: 2026-11-03
     end: 2026-12-12
     name: <your name>
   ```
5. `scripts/check-no-etoro-secrets.sh` — green. It now also compares the values in both env
   files against the tree (never printing them) and fails on a filled-in ack file.
6. Rehearsal on demo passed (§14.7).
7. Install the unit: `scripts/install-ui-api-service.sh --real-canary` (`--print` first to read
   what it writes). It refuses when the demo unit is absent, when the token file is missing, the
   demo's path or the demo's content, when the env file is missing, not 0600 or not
   `ETORO_ENV=real`. The binary then checks the six conditions at start and **dies naming the
   failing one**; the installer prints the journal hint when `/healthz` does not answer:
   ```
   systemctl --user status mtb-ui-api-real-canary
   journalctl --user -u mtb-ui-api-real-canary -n 30
   journalctl --user -u mtb-ui-api-real-canary | grep 'REAL CANARY ACK OK' | tail -1   # the six facts
   ```
   The start line reads `REAL CANARY ACK OK: flag -etoro-real-canary; ETORO_ENV=real, key prefix
   matched (12 chars); ack <path> by <name>, <start>..<end>; ledger <path>; policy
   etoro-real-canary-2026-10-22; token file differs from the demo one (paths and hashes)`,
   followed by `REAL ACCOUNT CANARY ENABLED on ledger etoro-real-canary …`.
8. **Start halted.** Open `http://127.0.0.1:5174` (`cd web && npm run dev:real-canary`): the red
   bar must be there on every page. Risk page → Halt, phrase `etoro-real-canary`, reason
   "canary pre-flight: halted until day 0". `GET http://127.0.0.1:8091/api/ui/controls?ledger=etoro-real-canary`
   must show `environment: "real"`, `trading.enabled`, `policy_version
   etoro-real-canary-2026-10-22`, `min_amount_usd 50 / max 60 / default 50`,
   `leverage_options [20]`.
9. Cron and S3 (D3/D4): re-run the ops installer **with** the flag, and keep passing it on every
   later re-run (it is never implied):
   `scripts/install-ops-cron.sh --topic-arn … --bucket … --canary-observe`. It adds one line
   (`0 22,23 * * *` with the New York `= 18` guard → 18:00 NY every day), builds
   `etoro-daily-data/etoro-canary-observe`, writes `ETORO_CANARY_S3_URI`. Then, once:
   `scripts/ops-run.sh etoro-canary-observe` by hand and read
   `~/.local/state/mtb-ops/etoro-canary-observe.log` and `real-canary/observations.jsonl`
   (an observation with no positions and the 50 USD x20 preview; exit 0).
10. Day-0 captures, read-only (eligibility, the cost preview at 50 USD x20, rates, account, with
    `SHA256SUMS`): `cd services/strategy-executor && ./etoro-daily-data/etoro-canary-observe
    -capture ../../docs/etoro/evidence-real-canary-<start>` (without `-dry`, which writes nothing
    at all; the observation line this also appends is the series' first point: no position yet).

### 14.3 Day 0: open (by hand, cash session, not in the first or last 15 minutes)

1. Risk page → Resume, phrase `etoro-real-canary`, reason "canary day 0".
2. Trade page → NSDQ100 · Open. **Invested** 50 USD, leverage x20 (the only option), the stop
   the recommend step proposes (the policy forces 2.3–2.45 %) → **Prepare**. The card is headed
   `Preview · open REAL NSDQ100` and its first line reads `50 USD invested · x20 · exposure
   1,000 USD · max loss 24.50 USD`; below it the venue's costs (overnight, weekend) and the
   warnings. The prepare blocks when the account holds an NSDQ100 position the ledger does not know
   (`max_positions`, D7), when the session is closed, when the day's order was already sent, and
   when the price moved more than 10 % from the quote. The account's long-term holdings on other
   instruments appear as a REAL warning naming them; they are outside the canary.
3. **Place · `PLACE REAL NSDQ100`**, a reason, your name. The dialog is red and repeats the
   sizing line. Read the outcome: fill, `fill_vs_mid_bps`, position id, the stop as eToro holds
   it. The ledger line carries `source: "ui"`, `client_ref ui-real-nsdq100-…`, `env: real`.
4. Note the fill and the day-0 preview in `evidence-real-canary-<start>/SUMMARY.md`. The 18:00 NY
   observation must show the position with `fees_to_date 0` (the first night's fee posts the
   next day).

### 14.4 Daily

Nothing to do. The observer runs at 18:00 NY (weekends included; eToro posts overnight fees at
17:00 NY), refreshes `real-canary/candles/nsdq100_daily_<date>.csv` (the console's chart and the
stop profiles' ATR; one read, the executor's format — the canary has no executor run to do it;
`-candles-only` is the backfill, `-no-candles` skips it) and appends one line: cash and equity, per position `fees_to_date` and the day's
**delta** (M1), nights held since the last line (17:00 NY boundaries), the history rows that
appeared since (M4), the cost preview at the held size. Glance at the Positions card on `:5174`
or `tail -1 real-canary/observations.jsonl | jq`. Every Monday:

```
cd services/strategy-executor
go run ./cmd/etoro-fees-check -real-canary -ledger ./etoro-daily-data/real-canary/ledger.jsonl \
  -env-file ~/.config/microservices-trading-bot/etoro-real-canary.env \
  -out ../../docs/etoro/evidence-real-canary-<start>/fees
```

The report prices the model on **exposure** (amount × leverage) and adds a "Per night, as
observed" section from `observations.jsonl`: measured vs preview vs frozen, one row per night,
the weekend night (M2) visible as the ≈ 3× delta. If the observer exits 1 two days running (a
read failed; the line is still written, with the error), fix it the same day: a gap in the
series is a gap in the measurement.

### 14.5 Events

- **Stopped out (likely under B).** Do nothing at the venue. When the history shows the close:
  ```
  go run ./cmd/etoro-reconcile -real-canary -ledger ./etoro-daily-data/real-canary/ledger.jsonl \
    -env-file ~/.config/microservices-trading-bot/etoro-real-canary.env -record-venue-close -by <you>
  ```
  `-record-venue-close` is the one write this command makes: an `action: venue_close` line
  (`stopped_out` / `took_profit` / `other` with the gap in bps, classified by the shared rule the
  executor uses) for a position the ledger holds, the account does not, and the history lists.
  Record the venue's stop time and the time the history first showed it (M4, M6). Re-open **once
  or twice** if M1 needs more nights; never more than **three** opens in the window (D6).
- **Halt.** Risk page, phrase `etoro-real-canary`. Prepares are refused; a held position stays
  until you close it by hand.
- **Watchdog.** The observer's run log lands last at
  `s3://<bucket>/daily-executor/etoro-real-canary/run-<UTC>.log` with an `exit=` line, the same
  convention the ledger-watchdog reads, so one deploy-time parameter watches it:
  `LEDGERS=…,etoro-real-canary=daily-executor/etoro-real-canary` (plain daily calendar: stale
  after 30 h). The 15-minute `ui-alerts` job is **not** pointed at the canary on purpose
  (measurement only; the observer's exit code, its log and the unit's journal are the signal).
- **Anything unexplained** (a position the ledger does not know, a fee you cannot attribute, an
  order you did not send): halt, close by hand in the console if needed,
  `systemctl --user stop mtb-ui-api-real-canary`, revoke the key. Write it down first.

### 14.6 Close-out and teardown

1. When M1–M4 are done or the ack's end date arrives (after it the unit refuses to start):
   **Close · `CLOSE REAL NSDQ100`** in the cash session; wait for the history; run the reconcile
   and the fees check once more.
2. Copy `observations.jsonl`, the ledger, `ui-audit.jsonl`, the reconcile and fees reports and
   the day-0 captures into `docs/etoro/evidence-real-canary-<start>/`; write `SUMMARY.md`
   (measured vs preview vs frozen per line item; lags; fills; surprises).
3. **you** — Revoke the API key at eToro. Delete the ack file and the env file.
   `scripts/install-ui-api-service.sh --real-canary --uninstall`; re-run
   `scripts/install-ops-cron.sh` **without** `--canary-observe` (the line and the S3 URI go away).
   Withdraw the balance or note the inactivity fee.
4. Append the "measured on the real account, reported, does not decide" addendum to both
   pre-registrations. Commit, push.

### 14.7 Rehearsal on demo (before any real key; the gate)

The same binary runs the canary policy on the **demo** account with `-canary-rehearsal`
(`-etoro-real-canary` and `-canary-rehearsal` exclude each other; the rehearsal refuses a ledger
named `etoro-demo*`, so it cannot write the cells' ledgers). Phrases carry no REAL; the policy is
`etoro-canary-rehearsal-2026-10-22` (nsdq100 and spx500, no `max_positions`, because the demo
account holds the cells' positions; a same-instrument demo position is a warning, not a block):

```
cd services/ui-api
go run ./cmd -addr 127.0.0.1:8092 -operator-token-file ~/.config/mtb/operator-token \
  -etoro-ledger etoro-canary-rehearsal=../strategy-executor/etoro-daily-data/rehearsal-canary/ledger.jsonl \
  -etoro-trading -canary-rehearsal
# web: UI_API_URL=http://127.0.0.1:8092 UI_PORT=5175 npm run dev
```

Pass when, in one cash session: one x20 open at 50 USD and its close went through the console
(`PLACE NSDQ100` / `CLOSE NSDQ100`, the sizing line present, `env: demo` on the lines); the
observer wrote a line for it (`./etoro-daily-data/etoro-canary-observe -allow-demo -env-file
~/.config/microservices-trading-bot/etoro-demo.env -ledger ./etoro-daily-data/rehearsal-canary/ledger.jsonl
-ledger-name etoro-canary-rehearsal`); the fees check read the observations
(`etoro-fees-check -ledger ./etoro-daily-data/rehearsal-canary/ledger.jsonl`, demo env); and the
reconcile classified a position closed by hand at the venue (`etoro-reconcile -ledger … -record-venue-close -by <you>`).
The port `8090` unit is not involved and is not restarted.

**Run 2026-10-22, 12:05–12:16 New York, passed.** `ui-api` on `:8092` with `-canary-rehearsal`
reported `policy_version etoro-canary-rehearsal-2026-10-22`, bounds 50 / 60 / 50, `leverage_options
[20]`. Prepare at 50 USD x20: verdict allow, units 0.03428 = 1,000 USD exposure, stop 28457.19
(2.45 %), max loss 24.50 USD, cost preview overnight **0.23 USD** per night on 1,000 of exposure
(= the frozen 2.3 bps; weekend quoted 0). `PLACE NSDQ100` → executed, position 3623996972, fill
29165.20 (−2.1 bps vs mid), stop held by eToro. The observer (`-allow-demo`) listed the position
at exposure 1,000.03 with `fees_to_date 0`, `in_ledger true`, and previewed it with its held
stop; `CLOSE NSDQ100` → executed at 29171.50 (−0.1 bps vs mid), net profit 0.22, fees 0; the next
observation 30 s later had the position gone and the close in `history_new` (M4 lag on demo:
under a minute). `etoro-fees-check` read the ledger (row `x20`, exposure 1,000) and the
observations' per-night section. `etoro-reconcile` on the ledger: CLEAN; on a copy with the
close line removed (the stop-out shape): DRIFT `missing_position`, then `-record-venue-close -by`
wrote one `venue_close` line (close rate between the stop and the target → `venue_close`, net
profit 0.22, fees 0) and the next reconcile was CLEAN. A second open the same day was refused by
`max_orders_per_day`, as it should be. The demo cells' ledgers were not touched; the rehearsal
ledger lives in `etoro-daily-data/rehearsal-canary/` (git-ignored).

**What the rehearsal caught.** The first prepare was **blocked** with `cost preview failed:
stopLossRate is required for leverage > 1 or sellShort`: eToro's costs route (and our
`etoro.OrderRequest.Validate`) need a stop once leverage > 1, and prepare built the preview
without the stop it had already computed; the observer's what-if previews had the same gap. Fixed
the same day (prepare passes the order's stop and target to the preview; the observer previews a
held position with its own stop and the planned order with a stop 2.45 % below the ask), and the
fakes in both test suites now apply the rule, so the suites would catch it. Every unit test had
passed before: the fakes did not know the venue's rule. This is why the gate exists.

### 14.7a The first real round trip (2026-10-26) and what it changed

Day 0 happened on 2026-10-26, 10:49 New York: **`PLACE REAL NSDQ100`**, 60 USD x20 (0.040925 u at
29323.40, +3.8 bps vs mid, fee **0.08 USD** on the fill, stop 28594.45; position 3615242259). At
10:52 the operator pressed **Close at market** on the Positions card: the shortcut prepared the
close (twice in one second — a double click), the green `Place · CLOSE REAL NSDQ100` card sat in
the Manual section below the fold, and at 10:53:51 an **Escape** — bound to "cancel the prepared
trade" while the dialog is closed — burned it (`trade.cancel`, audited). The position was then
closed **in the eToro app** at 10:55:19 (29338.00, **net +0.60, fees 0.00** per the history) and
booked with `etoro-reconcile -real-canary -record-venue-close -by "Diego Gallo"`. At 11:02 a new
NSDQ100 position (3615264562, 65 USD x20, with a take-profit) was **opened in the app**: not the
canary's, outside the policy's 60 USD cap; the console refuses to open while it exists (D7) and
cannot close it; the observer measures its financing like any position. Cash afterwards:
**0.11 USD**. The operator was told, in these words, that nothing in the console can protect an
account with no cash buffer.

Changed the same day: (1) **live mark** on the Positions card — every open row shows the live bid
and `units × (bid − open rate)` gross from the 2 s quote stream, with eToro's net figure and its
timestamp beside it (the portfolio read stays cached 45 s / refetched 60 s on purpose: the key's
60 reads/min); (2) **Close at market on a REAL ledger opens the confirmation dialog directly**,
one click to the phrase, and the shortcut prepares once; (3) **Escape no longer cancels on REAL**
— only the Cancel button does. The fee question ("charged 0.80 to close"): the API shows 0.08 on
the open and 0.00 on the close; the figure was most likely the 0.08 misread, to be confirmed
against the app.

### 14.8 What the plan got wrong, found while building

- `MaxOrderNotional` is **1,200**, not 1,000: the exposure is invested × leverage = 1,000 at
  50 USD, and the policy allows up to 60 USD invested (D1), i.e. 1,200; the venue minimum is a
  floor, not the cap.
- The stop is a **range** (2.3–2.45 %), not "exactly the venue maximum": a market fill a few
  bps above the ask would push a 2.5 % stop past eToro's cap and the order would be refused.
  Found with it: `shared/pkg/stops` floored a clamped stop below the policy minimum; it now
  rounds up at the ceiling.
- `etoro-reconcile` gained `-record-venue-close` (the plan said "hand-run reconcile writes the
  `venue_close` line"; a reconcile is read-only, so the write is a separate, named flag).
- The real account was to block an open when it held **any** position. The first real read
  (2026-10-22 17:22 UTC, the observer) showed five long-term positions on two other instruments
  (≈ 749 USD exposure, equity ≈ 802 USD, cash 62.78) and a hand-trading history on NSDQ100: the
  rule would have blocked the canary forever. **D7:** scoped to the instrument (any NSDQ100
  position the ledger does not know blocks; `max_positions` counts NSDQ100 positions; other
  holdings are a named REAL warning). Honest consequence: the balance is no longer the last line
  of defence; the policy caps are.
- The ack's `key_prefix` is a weak identifier for eToro keys: the x-api-key is a 63-character
  opaque string (fine), but the 260-character `eyJ…` token is the **x-user-key** and its first
  ~60 characters encode a client id that persists across regenerations. The env file's fields
  were also swapped once (401 on every route; the demo file is the reference: public 63 chars,
  private 260). A `key_sha256:` line in the ack would be stronger; not built yet.
- `Client.Environment()` already existed (the plan said `Env()`).
- The watchdog needed no change: the observer's run log follows the executors' convention.
- The console's chart and the ATR profiles read `<ledger dir>/candles/`, which only the executor
  wrote: the canary's chart was empty ("no candle file for nsdq100") and the recommend step fell
  back to the 8 % stop. Since 2026-10-22 the observer keeps the file (backfilled: 951 bars from
  2023-01-06).
- "Real-time" prices: eToro's Public API is REST-only (no WebSocket in the OpenAPI capture), 60
  reads/min per key shared with the trade path. The canary polls at the 2 s floor and the Trade
  page shows the quote's age; the Prepare quote, fetched when you press Prepare, is the price a
  trade is checked against, and the fill is at market either way.

