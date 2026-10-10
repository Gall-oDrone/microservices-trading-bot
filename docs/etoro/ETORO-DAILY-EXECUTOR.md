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

Crontab (the executor skips NYSE holidays itself):

```cron
CRON_TZ=America/New_York
35 9 * * 1-5  /path/to/repo/scripts/ops-run.sh etoro
```

Wiring this into `scripts/install-ops-cron.sh` is planned for phase P6.

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

**P3 exit criterion:** five consecutive trading days with `reconcile=clean`, plus a passing
kill-switch drill (`scripts/kill-switch-drill.sh --offline --source services/ui-api/internal/api/testdata`;
it covers both executors).

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
| `reconcile=drift` | See the findings in the JSON report. | `unfinished_intent`: re-run the executor. Other findings: resolve by hand. |

## 4. eToro behaviours the executor works around

- **Duplicate request id.** eToro rejects a reused `x-request-id`, and `orders:lookup?referenceId=` cannot find API orders. A resent open is therefore resolved to the position opened on the instrument since the intent's timestamp.
- **Portfolio lag.** The portfolio shows fills 1–3 s late. The adapter polls for up to 30 s.
- **Two versions of daily history** (seen 2026-10-10):
  - up to ~1.3 bps on 13–16 recent closes;
  - the executor reads twice and records `candles.revised_closes` / `max_revision_bps`;
  - recorded closes may move by up to 5 bps before the run stops.
- **Weekend and holiday quotes.** These bars are dropped. Orders go only in the cash session.

## 5. Halting and manual resolution

- **Halt now.** Write the halt file next to the ledger:
  ```bash
  printf '{"halted":true,"reason":"<why>","by":"<you>","at":"%s"}\n' "$(date -u +%FT%TZ)" \
    > services/strategy-executor/etoro-daily-data/demo/risk-state.json
  ```
  Until phase P5 adds this ledger to ui-api, the operator UI's **Halt all** button does not reach it.
- **Resume.** Delete the file. The next run trades from the rule's target.
- **Untracked position.** Close it by hand on the demo account (or with the eToro MCP `prepare-close` → `place-close`), then re-run. If the position *is* the executor's, add a note in the forward-test report. Never edit past ledger lines.
- **Disable for a day.** Set `ETORO_EXECUTOR_DISABLED=1` in the cron environment.
