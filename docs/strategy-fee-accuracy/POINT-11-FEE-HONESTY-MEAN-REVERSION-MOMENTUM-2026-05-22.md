# Point 11 — Realized-Fee P&L for `mean_reversion` and `momentum`

Date: 2026-05-22  
Repository: `microservices-trading-bot`

## 1) Problem

POINT-9 wired Bitso's **actually-realized** fee rates into `limit_profit` only. After POINT-10 Phase 2, the regime router can hand traffic to `mean_reversion` or `momentum` as soon as `DRY_RUN=false` on Stage — but those strategies still treated round-trip P&L as gross quote delta (or exited on signal without waiting for the SELL fill). That reintroduces the same failure mode POINT-9 fixed: the strategy believes a trade was profitable when Bitso's per-leg fees say otherwise.

## 2) Goal

1. Share the POINT-9 fill plumbing (`OrderFill.FeeRate`, `ApplyBuyFillFee` / `ApplySellFillFee`, `RealizedQuotePnL`) across `mean_reversion` and `momentum`.
2. Exit on **confirmed SELL fills** (pending-sell pattern from `limit_profit`) so position state and P&L match the exchange.
3. Give `momentum` the same lifecycle guards as `limit_profit`: `stop_loss_quote`, `max_position_hold_seconds`, `max_daily_loss_quote` (with `exit_reason` metadata for dashboards).

## 3) Data flow

Unchanged from POINT-9 — `order-management` → Kafka `trading.order.fills` → `strategy-executor` `OnOrderFilled`. Only the consumers changed:

```
OrderFillEvent → strategy-executor fills consumer
                      ├─ MeanReversionStrategy.OnOrderFilled
                      └─ MomentumStrategy.OnOrderFilled
                              │
                              ▼
                    fee_aware.go (shared)
                      ApplyBuyFillFee / ApplySellFillFee
                      RealizedQuotePnL → net P&L audit + circuit breaker
```

## 4) Code changes

### 4.1 `services/strategy-executor/internal/strategies/fee_aware.go` (new)

| Symbol | Purpose |
|--------|---------|
| `PositionFeeRates` | Per-leg realized fee rates for the open position |
| `ApplyBuyFillFee` / `ApplySellFillFee` | Store `fill.FeeRate` when present |
| `RealizedQuotePnL` | Gross + net quote P&L via `bitso.NetQuotePnLPerBase` when both legs are known |

### 4.2 `mean_reversion.go`

- Implements `OrderFillAware`.
- BUY fill: sets position from fill price/size, records buy fee.
- SELL signal: sets `PendingSell` + `event_id`; position stays open until matching SELL fill.
- SELL fill: `RealizedQuotePnL` for audit; clears position.

### 4.3 `momentum_strategy.go`

- Same fill-aware exit path as mean reversion.
- Lifecycle: `stop_loss_quote`, `max_position_hold_seconds`, `max_daily_loss_quote`, `daily_loss_reset_hour_utc` (parsed from strategy parameters).
- Exits tag `metadata.exit_reason` (`stop_loss`, `max_hold`, `take_profit`, etc.) for Grafana parity with `limit_profit`.

### 4.4 `scripts/start-organic-trading.sh`

- `ROUTER_MANAGED=true`: registers `mean_reversion,limit_profit,momentum` (override with `STRATEGY_TYPES`) but does **not** call `start` — the in-cluster router picks the active strategy.
- Momentum lifecycle env vars: `MOMENTUM_STOP_LOSS_QUOTE`, `MOMENTUM_MAX_POSITION_HOLD_SEC`, `MOMENTUM_MAX_DAILY_LOSS_QUOTE`, `MOMENTUM_DAILY_LOSS_RESET_HOUR_UTC`.

## 5) Operator notes

- Until both BUY and SELL fills carry `fee_rate`, strategies fall back to gross P&L (same as pre-POINT-11).
- For router-driven Stage runs: `ROUTER_MANAGED=true ./scripts/start-organic-trading.sh` then deploy/start `strategy-router` with matching strategy names in `STRATEGY_ROUTER_*` env.

## 6) Related documents

- [`POINT-9-REALIZED-FEES.md`](POINT-9-REALIZED-FEES.md)
- [`POST-POINT-10-ROADMAP-2026-05-22.md`](POST-POINT-10-ROADMAP-2026-05-22.md) — items 6–7
- [`../MOMENTUM-STRATEGY.md`](../MOMENTUM-STRATEGY.md) §7
