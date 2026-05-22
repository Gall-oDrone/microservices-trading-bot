# Strategy Fee Accuracy & Regime-Driven Strategy Routing

Date: 2026-05-21  
Repository: `microservices-trading-bot`

This folder documents two related improvements to the live trading pipeline that came out of the post-mortem on the `organic_lp_pnl_1779300471` run on Bitso Stage (May 2026, net P&L −17.28 MXN on a 1340 MXN round-trip).

The two documents below cover the work-streams independently — each is shippable on its own — but together they answer the same question: *"why did this scalp lose money, and how do we stop the next one from losing it for the same reasons?"*

| File | What it covers |
|------|----------------|
| `POINT-9-REALIZED-FEES.md` | Wiring Bitso's **actually-realized** fee and maker/taker liquidity from `UserTrade` payloads through to the `limit_profit` strategy so the SELL-leg threshold and realized P&L use the fee Bitso truly charged — not the strategy's configured assumption. |
| `POINT-10-STRATEGY-REGIME-ROUTER.md` | An external orchestrator that picks the right strategy (`mean_reversion`, `momentum`, `limit_profit`, or pause) for the current market regime so we stop using `limit_profit` on the high-fee / wide-spread regime where it is structurally a loser. Phase 1 — bash script. |
| `STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md` | Phase 2 of POINT-10: in-cluster Go service (`services/strategy-router/`) that re-implements the same routing logic with Prometheus metrics, an HTTP control surface (`GET /api/v1/router/state`, `POST /api/v1/router/run`), and a Kubernetes manifest. |

## TL;DR

1. **Fees now follow the truth, not the config.**  
   `services/order-management/internal/sync/user_trades_poller.go` derives `liquidity` (maker/taker) and `fee_rate` (decimal fraction of notional) from each Bitso `UserTrade` and stamps them onto the order metadata. `OrderFillEvent` carries the realized values to `strategy-executor`, where `limit_profit` overrides its configured assumption for both the exit-threshold calculation (BUY leg) and the realized-P&L calculation (SELL leg). See `docs/strategy-fee-accuracy/POINT-9-REALIZED-FEES.md`.

2. **Strategy choice now follows the market regime, not a single deploy decision.**  
   `scripts/strategy-regime-router.sh` (Phase 1) and the in-cluster Go service `services/strategy-router/` (Phase 2, 2026-05-22) poll `GET /api/v1/indicators/{book}/snapshot`, classify the current regime from ATR, RSI, EMA distance, and Bollinger %B, and use the existing `POST /api/v1/strategies/{name}/start|stop` endpoints to converge on the right strategy. Both refuse to switch when the active strategy still holds a position. See `docs/strategy-fee-accuracy/POINT-10-STRATEGY-REGIME-ROUTER.md` for the Phase 1 design and `docs/strategy-fee-accuracy/STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md` for the Phase 2 service.

## Why both at once

The fee-accuracy fix is what makes the regime router safe: if we route to `limit_profit` whenever the market is calm enough, we still need the strategy's threshold to be computed against the **actual** Bitso fee — otherwise we'd just lose money slower. Conversely, the regime router is what lets us *avoid* `limit_profit` when fees and spread make scalping structurally unprofitable.

## Related documents

- `docs/LIMIT-PROFIT-STRATEGY.md` — strategy behavior, parameters, organic startup.
- `docs/LIMIT-PROFIT-IMPROVEMENTS.md` — tracks production-readiness improvements (P9 and P10 are now ticked in there too).
- `docs/FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md` — strategy framework and risk controls.
- `docs/ORGANIC-TRADING-STARTUP.md` — how to start organic trading runs (the router script piggybacks on the same port-forward + endpoints).
