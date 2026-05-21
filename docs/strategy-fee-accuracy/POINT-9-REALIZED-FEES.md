# Point 9 — Use Bitso's Actual Realized Fees, Not Assumptions

Date: 2026-05-21  
Repository: `microservices-trading-bot`

## 1) Problem

The `limit_profit` strategy decides whether a position has cleared its profit threshold by computing:

\[
\text{exit\_threshold} = \frac{\text{entry} \cdot (1 + \text{buy\_fee\_rate}) + \text{min\_profit/qty}}{1 - \text{sell\_fee\_rate}}
\]

Both fee rates were taken from one of two sources:

1. A **cached Bitso `/v3/fees` lookup** (`shared/pkg/bitso.FeeDecimalForLiquidity`) keyed by the strategy's *configured* `buy_liquidity` and `sell_liquidity`.
2. A **manual** `fee` / `fee_bps` configuration.

Both paths use *assumptions* about which side of each leg is maker vs taker. The May 2026 Stage run (`organic_lp_pnl_1779300471`) demonstrated the failure mode:

| Leg | Strategy assumption | Reality on Bitso Stage | Configured fee | Actual fee |
|------|--------------------|------------------------|---------------|-----------|
| BUY  | maker (0.57%)      | **taker (0.741%)**     | 7.65 MXN      | **9.93 MXN** |
| SELL | taker (0.741%)     | **maker (0.57%)**      | 9.93 MXN      | **7.64 MXN** |

The round-trip fee was correctly modeled in aggregate (so net P&L was mathematically consistent), but the **per-leg attribution** that drove the SELL threshold was wrong. The strategy lifted SELL orders that priced in a taker SELL fee while the order was actually resting as maker — and the BUY-side assumption of maker also made the entry threshold optimistic.

For small notionals on a high-fee venue, this gap is the difference between a winning scalp and a losing one.

## 2) Goal

Propagate the **actually-realized** maker/taker role and decimal fee rate of each fill from `order-management` to `strategy-executor` so the `limit_profit` strategy:

1. Overrides its configured BUY fee with the realized BUY fee when computing future exit thresholds within an open position.
2. Overrides its configured SELL fee with the realized SELL fee when computing realized P&L on a closed position.
3. Records the realized values in the Prometheus / log audit trail.

## 3) Data Flow

```
Bitso /v3/user_trades  ──►  UserTradesPoller.handleTrade
                                  │
                                  │  buildFillObservation(t, majorAbs)
                                  ▼
                  models.FillObservation { Liquidity, FeeRate, FeeAmount, FeeCurrency }
                                  │
                                  │  OrderManagerUserTrades.RecordFillObservation(ctx, oid, obs)
                                  ▼
              order.Metadata["fill_liquidity" | "fill_fee_rate" | "fill_fee_amount" | "fill_fee_currency"]
                                  │
                                  │  Manager.SyncOrderFromBitsoTrades → status = filled
                                  ▼
                       Manager.maybePublishOrderFill
                                  │
                                  ▼
            shared/pkg/models.OrderFillEvent (Kafka: trading.order.fills)
                  Liquidity / FeeRate / FeeAmount / FeeCurrency populated
                                  │
                                  ▼
            services/strategy-executor/cmd/main.go (fills consumer)
                                  │
                                  │  strategies.OrderFill { Liquidity, FeeRate, BuyFeeRate? }
                                  ▼
                LimitProfitStrategy.OnOrderFilled
                  ├─ BUY  fill: s.positionBuyFeeRate  = fill.FeeRate (used in exitPriceThreshold)
                  └─ SELL fill: s.positionSellFeeRate = fill.FeeRate (used in realized P&L)
```

## 4) Code Changes

All code changes are additive — existing consumers that did not know about the new fields keep working with their previous defaults.

### 4.1 `shared/pkg/bitso/fee_compute.go` (new helpers + tests)

| Symbol | Purpose |
|--------|---------|
| `DeriveFillLiquidity(side, makerSide OrderSide) string` | Returns `"maker"` when our side matches Bitso's `maker_side`, `"taker"` otherwise. Returns `""` if either side is unknown (caller falls back to the configured assumption). |
| `DeriveFillFeeRate(feesAmount, majorAbs, minorAbs float64, feeIsBase bool) float64` | Decimal fraction of notional. When the fee currency equals the **base** (BUY case), `rate = feesAmount / majorAbs`. When the fee currency equals the **quote** (SELL case), `rate = feesAmount / minorAbs`. |
| `IsBaseCurrencyForBook(feeCurrency Currency, book string) bool` | True when `feeCurrency` matches the first segment of `<base>_<quote>`. |

Unit tests in `shared/pkg/bitso/fee_compute_test.go` validate against the **actual** Stage fills:
- BUY taker: `0.00000741 BTC / 0.001 BTC = 0.00741 (= 0.741%)`.
- SELL maker: `7.643814 MXN / 1341.02 MXN = 0.00570 (= 0.570%)`.

### 4.2 `shared/pkg/models/events.go`

`OrderFillEvent` gains three optional fields:

| Field | Type | Meaning |
|-------|------|---------|
| `FeeRate` | `float64` | Decimal fraction of notional Bitso charged for this leg. Omitted when undeterminable. |
| `FeeAmount` | `float64` | Raw fee Bitso billed in `FeeCurrency`. |
| `FeeCurrency` | `string` | Currency Bitso billed the fee in (base for BUY, quote for SELL on Bitso). |

`Liquidity` already existed but was rarely populated; it is now reliably set by the poller.

### 4.3 `services/order-management/internal/models/fill_observation.go` (new)

`FillObservation` is the in-process struct that carries the derived values from the poller to the manager. Lives in `models/` (lower layer than both `manager/` and `sync/`) to avoid a circular import.

### 4.4 `services/order-management/internal/sync/user_trades_poller.go`

- `OrderManagerUserTrades` interface gains `RecordFillObservation(ctx, bitsoOrderID, obs)`.
- `handleTrade` calls `buildFillObservation(t, majorFloat)` **before** the conversion to `UserOrderTrade` drops `MakerSide`, and persists the observation via `RecordFillObservation`.
- `buildFillObservation` is the private adapter that combines `DeriveFillLiquidity`, `DeriveFillFeeRate`, and `IsBaseCurrencyForBook`.

### 4.5 `services/order-management/internal/manager/order_manager.go`

- New method: `(*Manager).RecordFillObservation(ctx, bitsoOrderID, obs)`. Idempotent; updates `order.Metadata["fill_liquidity"|"fill_fee_rate"|"fill_fee_amount"|"fill_fee_currency"]`.
- `maybePublishOrderFill` now reads those metadata fields and copies them onto `OrderFillEvent.{Liquidity, FeeRate, FeeAmount, FeeCurrency}` before publishing.

### 4.6 `services/strategy-executor/cmd/main.go`

The Kafka order-fills consumer copies the new fields onto `strategies.OrderFill`:

```go
fill := strategies.OrderFill{
    EventID:      ev.EventID,
    Book:         ev.Book,
    Side:         ev.Side,
    AveragePrice: ev.AveragePrice,
    FilledAmount: ev.FilledAmount,
    Liquidity:    ev.Liquidity,
    FeeRate:      ev.FeeRate,
}
if ev.FeeRate > 0 && ev.Side == "buy" {
    rate := ev.FeeRate
    fill.BuyFeeRate = &rate  // back-compat for callers that read BuyFeeRate
}
```

### 4.7 `services/strategy-executor/internal/strategies/enhanced_strategy.go`

`OrderFill` gains `FeeRate float64`. The existing `*BuyFeeRate` pointer is retained for back-compat and now documented as "legacy BUY-leg override".

### 4.8 `services/strategy-executor/internal/strategies/limit_profit_strategy.go`

- New per-position field: `positionSellFeeRate float64`. Cleared in `resetPositionFeeOverrides`.
- `handleBuyFillLocked`: prefers `fill.FeeRate` over the legacy `fill.BuyFeeRate` pointer when both are present (both describe the BUY leg's actual fee rate).
- `handleSellFillLocked`: when `fill.FeeRate > 0`, overrides `sellR` and forces the `feeModel == "bitso_api"` branch — realized P&L is now computed against the actual SELL fee Bitso billed.

## 5) Backwards Compatibility

- All new fields on `OrderFillEvent` and `OrderFill` are JSON-omitempty. Older consumers continue to work.
- `RecordFillObservation` is part of a **new** method on the `OrderManagerUserTrades` interface. The only implementation today is `manager.Manager`, which the poller already wires up. Any future implementation will need to add this method (lint will catch it).
- The strategy still respects its configured `buy_liquidity` / `sell_liquidity` parameters when no realized values are known (e.g. when Bitso returns a fill without `maker_side`, which never happens in practice but is defended against).

## 6) Verification

```bash
# Unit tests
( cd shared && go test ./pkg/bitso/... )
( cd services/order-management && go test ./internal/sync/... -run TestBuildFillObservation -v )
( cd services/order-management && go test ./... )
( cd services/strategy-executor && go test ./internal/strategies/... )
```

Manual end-to-end check on a real fill:

1. Start `limit_profit` via `scripts/start-organic-trading.sh` with `USE_BITSO_FEES_LP=true`.
2. Tail the `OrderFillEvent` Kafka topic (`trading.order.fills`). Each event should now carry `liquidity`, `fee_rate`, `fee_amount`, `fee_currency`.
3. In strategy-executor logs, look for the `handleBuyFillLocked` / `handleSellFillLocked` lines — the post-fix `fee_model` should be `bitso_api` and the implied SELL fee rate should match the value seen on the fill event.
4. Reconcile the strategy's reported realized P&L against the manual calculation from Bitso's CSV / `/v3/order_trades` response (matches to within FP rounding).

## 7) Future Work

- Expose a Prometheus histogram of `actual_fee_rate / assumed_fee_rate` per book to alert when reality drifts from the configured assumption for an extended period.
- Use the same hook in `LIMIT-PROFIT-IMPROVEMENTS.md` item 9 to feed the cached `BitsoFees` provider with a "preferred liquidity" hint per book based on the last N realized fills.
- Apply the same fee-honesty pattern to `mean_reversion` and `momentum` strategies that today assume a single round-trip fee constant.
