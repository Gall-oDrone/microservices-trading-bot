# Execution Risk Guards on the Daily Executor (2026-10-05)

**What changed.** Starting 2026-10-05, `services/strategy-executor/cmd/daily-executor` runs a pre-trade risk
check (`shared/pkg/risk.Check`) before every **stage** order. This is step R1 of
[`docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md`](../frontend/FRONTEND-UI-PLAN-2026-10-03.md) §6.4.1.

**This is not a rule change.** It affects neither forward test:

- [`FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md`](FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md) (btc_mxn)
- [`FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md`](FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md) (btc_usd)

The signal (close vs SMA50), its timing, the all-in/all-out sizing, the paper account and its leg
costs are computed exactly as before, by the same code, before the check runs. The check only
decides whether the **stage order** for that signal is safe to send. Dry runs place no orders, so
the check does not run on them.

**Limits** (built-in policy `default-2026-10-03`, unchanged from the plan's §6.2):

| Limit | btc_mxn | btc_usd | Effect |
|---|---:|---:|---|
| Max order size | 0.01 BTC | 0.01 BTC | block |
| Max position | 0.01 BTC | 0.01 BTC | block |
| Max order notional | 25,000 MXN | 1,500 USD | block |
| Max legs per day | 1 | 1 | block |
| Max price deviation, touch vs decision close | 1,500 bps | 1,500 bps | block |
| Global halt flag | off | off | block |

Sells that reduce the position skip the size, position, notional and per-day limits. Only the halt
and the price guard apply to them. Drawdown and cost thresholds only warn and never block
(pre-registration §2: stopping a forward test needs a written reason).

**If an order is blocked:**

- No order is sent.
- The day's ledger line has `stage.action = "blocked"` and `stage.risk.allowed = false`, with the findings.
- The executor exits 1.
- The next day's run plans from the unchanged stage position.

The paper account is never affected. The stage position can lag paper for the blocked day or days,
and the UI shows those as `order_blocked`. Each such day is an execution gap, not a deviation from
the frozen rule.
