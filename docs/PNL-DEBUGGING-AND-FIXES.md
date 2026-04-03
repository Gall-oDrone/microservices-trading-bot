# P&L Metrics Debugging and Fixes

This document details the debugging process, root causes, and fixes implemented to resolve the P&L (Profit & Loss) metrics calculation issues in the order-management service.

## Problem Statement

The P&L metrics (`trading_daily_realized_pnl_currency`, `orders_filled_total`) were not updating in Grafana dashboards after orders were filled on Bitso. Despite orders being placed and filled successfully, the metrics remained at zero or didn't reflect actual trading activity.

## Investigation Timeline

### Phase 1: Initial Analysis

**Symptoms Observed:**
- Grafana P&L metrics showing 0 after BUY/SELL trades
- `orders_filled_total` counter not incrementing
- Orders appearing as filled in Bitso Stage dashboard but not reflected in order-management

**Initial Hypothesis:**
The system wasn't continuously monitoring user trades and order states from Bitso API.

### Phase 2: Data Flow Tracing

Traced the data flow through the system:

```
Trading Signal (Kafka) 
    → Trading Engine (places order on Bitso)
    → Bitso API (fills order)
    → Order Management (should detect fill and calculate P&L)
    → Prometheus Metrics (should be updated)
```

**Components Involved:**
1. `BitsoSyncJob` - Polls `/v3/orders` for order status updates
2. `UserTradesPoller` - Polls `/v3/user_trades` for fill discovery
3. `OrderManager.SyncOrderFromBitso()` - Processes order updates
4. `OrderManager.SyncOrderFromBitsoTrades()` - Processes fill data
5. `Position.ApplyFill()` - Calculates realized P&L

### Phase 3: Root Cause Identification

#### Root Cause 1: In-Memory Position Repository

**Problem:** The `InMemoryPositionRepository` was being used, causing position state to be lost on pod restarts.

```go
// Before: Position state lost on restart
positionRepo = repository.NewInMemoryPositionRepository(appLogger, metricsCollector)
```

**Impact:** 
- When a BUY order filled and created a `long` position, this state was stored in memory
- On pod restart, the position was lost
- When a subsequent SELL order filled, there was no position to close
- Result: P&L calculation returned 0 (new short position opened instead of closing long)

#### Root Cause 2: Stale Orders Causing Batch Lookup Failures

**Problem:** The `BitsoSyncJob` used batch lookups (`/v3/orders/oid1,oid2,...`) which failed entirely when any order ID was no longer on Bitso.

```
{"level":"error","endpoint":"/orders/cTkqrcmRyIYHVVXu,DggxcDzSZyZRmkRm,...","status":404}
{"level":"info","message":"Batch LookupOrders failed, falling back to individual lookups"}
```

**Impact:**
- Old completed orders remained in the repository
- Batch API calls returned 404/HTML errors
- Sync job couldn't update order states

#### Root Cause 3: Rejected Orders Still Placed on Bitso

**Problem:** When order-management rejected an order due to risk check failures, the trading-engine had already placed the order on Bitso.

```
{"level":"warn","error":"risk check failed: max open orders limit reached: 10/10"}
{"level":"info","message":"Linked Bitso order to signal order"}  // Order still placed!
```

**Impact:**
- Order status set to `rejected` in order-management
- Order actually filled on Bitso
- `UserTradesPoller` skipped processing because `order.IsClosed()` returned true for rejected orders
- Fills never processed, P&L never calculated

## Fixes Implemented

### Fix 1: Redis-Backed Position Repository

**File:** `services/order-management/internal/repository/redis_position_repository.go`

Created a new Redis position repository that persists position state across pod restarts:

```go
type RedisPositionRepository struct {
    client  *redis.Client
    logger  *logger.Logger
    metrics *metrics.MetricsCollector
}

func positionKey(book string) string { return positionKeyPrefix + book }
func positionBooksKey() string       { return positionKeyPrefix + "books" }
```

**Key Features:**
- Positions stored with keys like `om:position:btc_mxn`
- All position books tracked in `om:position:books` set
- Full CRUD operations with atomic Redis transactions

### Fix 2: Stale Order Cleanup Mechanism

**File:** `services/order-management/internal/sync/bitso_sync.go`

Added retry tracking and automatic cleanup of stale orders:

```go
const maxStaleRetries = 3

type BitsoSyncJob struct {
    // ... existing fields ...
    staleOrderMu      sync.Mutex
    staleOrderRetries map[string]int
}

func (j *BitsoSyncJob) syncOnce(ctx context.Context) {
    // Try batch lookup first; if it fails, fall back to individual lookups
    orders, batchErr := j.bitsoClient.LookupOrders(oids)
    
    if batchErr != nil {
        j.log.Info("Batch LookupOrders failed, falling back to individual lookups", ...)
    }
    
    // For orders not found, track retries and mark stale after max retries
    for _, oid := range oids {
        if _, ok := seen[oid]; ok {
            continue
        }
        
        trades, err := j.bitsoClient.OrderTrades(oid, nil)
        if err != nil {
            retries := j.incrementStaleRetry(oid)
            if retries >= maxStaleRetries {
                j.orderManager.MarkOrderStale(ctx, oid)
                j.clearStaleRetry(oid)
            }
            continue
        }
        // Process trades...
    }
}
```

**Added Method in OrderManager:**

```go
func (m *Manager) MarkOrderStale(ctx context.Context, bitsoOrderID string) error {
    // Marks order as cancelled with stale_reason metadata
    order.Metadata["stale_reason"] = "missing_from_bitso_after_retries"
    order.Metadata["stale_at"] = time.Now().UTC().Format(time.RFC3339)
    m.stateMachine.Transition(order, models.OrderStatusCancelled)
    // ...
}
```

### Fix 3: Rejected Order Recovery

**Files:** 
- `services/order-management/internal/sync/user_trades_poller.go`
- `services/order-management/internal/manager/order_manager.go`
- `services/order-management/internal/manager/state_machine.go`

#### UserTradesPoller Update:

```go
func (p *UserTradesPoller) handleTrade(ctx context.Context, t *bitso.UserTrade) {
    // Skip if order is truly closed (filled, cancelled) but NOT if rejected.
    // Rejected orders may have been placed on Bitso by trading-engine despite risk check failures.
    if order.IsClosed() && order.Status != models.OrderStatusRejected {
        p.log.Info("handleTrade: order already closed", ...)
        return
    }

    // If rejected, we'll try to recover and process the fill
    if order.Status == models.OrderStatusRejected {
        p.log.Info("handleTrade: processing fill for rejected order", ...)
    }
    // ... continue processing
}
```

#### SyncOrderFromBitsoTrades Update:

```go
func (m *Manager) SyncOrderFromBitsoTrades(...) error {
    // Skip truly closed orders but NOT rejected
    if order.IsClosed() && order.Status != models.OrderStatusRejected {
        return nil
    }

    // If order was rejected but has fills, we need to recover it
    wasRejected := order.Status == models.OrderStatusRejected
    if wasRejected {
        m.logger.Info("SyncOrderFromBitsoTrades: recovering rejected order with Bitso fills", ...)
    }
    // ... continue processing
}
```

#### State Machine Update:

```go
var allowedTransitions = map[models.OrderStatus][]models.OrderStatus{
    // ... other transitions ...
    
    // Rejected orders can transition to filled if they were actually placed on Bitso
    models.OrderStatusRejected: {
        models.OrderStatusFilled,
        models.OrderStatusPartiallyFilled,
    },
}
```

## Errors Encountered During Debugging

### Error 1: Invalid Signal Timestamp Format

```
{"level":"warn","error":"json: cannot unmarshal string into Go struct field TradeSignalEvent.timestamp of type int64"}
```

**Cause:** Signal payload had timestamp as ISO string instead of Unix epoch (int64)

**Fix:** Use Unix timestamp in signal JSON:
```json
{"event_id":"...","timestamp":1775183344,...}  // Correct
{"event_id":"...","timestamp":"2026-04-03T...",...}  // Wrong
```

### Error 2: Missing Required Signal Fields

```
{"level":"warn","message":"Trading signal missing required fields"}
```

**Cause:** Signal used `side` instead of `signal` field

**Fix:** Use correct field names per `TradeSignalEvent` struct:
```go
type TradeSignalEvent struct {
    EventID  string  `json:"event_id"`   // not signal_id
    Signal   string  `json:"signal"`     // BUY/SELL, not side
    // ...
}
```

### Error 3: Price Below Minimum

```
{"level":"error","message":"api error: Incorrect price 400 is below the minimum of 100000"}
```

**Cause:** Test signal used unrealistic BTC/MXN price (400 instead of ~1,190,000)

**Fix:** Use market-appropriate prices for the trading pair

### Error 4: Order Size Below Minimum

```
{"level":"warn","error":"order validation failed: order size 0.000100 below minimum 0.001000"}
```

**Cause:** Test order amount too small

**Fix:** Use amounts >= 0.001 BTC for btc_mxn pair

## Verification

### Test Sequence

1. **BUY Signal:**
   ```json
   {"event_id":"test-buy-final-1775183344","book":"btc_mxn","signal":"BUY","amount":0.001,"price":1194000}
   ```
   - Order created and filled at 1,189,010 MXN
   - Position opened: long 0.001 BTC
   - P&L: 0 (opening position)

2. **SELL Signal:**
   ```json
   {"event_id":"test-sell-final-1775183387","book":"btc_mxn","signal":"SELL","amount":0.001,"price":1184000}
   ```
   - Order created and filled at 1,184,000 MXN
   - Position closed
   - P&L: -6.91 MXN (realized loss)

### Metrics Verification

```
orders_filled_total{book="btc_mxn",strategy="e2e-pnl-test"} 2
trading_daily_realized_pnl_currency{currency="MXN"} -6.907777777777752
```

### Redis Position State

```json
{
  "id": "pos-btc_mxn-1775179338023083098",
  "book": "btc_mxn",
  "side": "long",
  "size": 0.008,
  "entry_price": 1190907.7777777778,
  "realized_pnl": -6.907777777777752,
  "updated_at": "2026-04-03T02:29:58.126676196Z"
}
```

## Architecture Improvements

### Before

```
┌─────────────────┐     ┌─────────────────┐
│ Order Manager   │────▶│ In-Memory Repo  │  ← State lost on restart
└─────────────────┘     └─────────────────┘
         │
         ▼
┌─────────────────┐
│ BitsoSyncJob    │────▶ Batch lookup fails if any order stale
└─────────────────┘
         │
         ▼
┌─────────────────┐
│ UserTradesPoller│────▶ Skips rejected orders (even if filled on Bitso)
└─────────────────┘
```

### After

```
┌─────────────────┐     ┌─────────────────┐
│ Order Manager   │────▶│ Redis Repo      │  ← State persisted
└─────────────────┘     └─────────────────┘
         │
         ▼
┌─────────────────┐
│ BitsoSyncJob    │────▶ Fallback to individual lookups + stale cleanup
└─────────────────┘
         │
         ▼
┌─────────────────┐
│ UserTradesPoller│────▶ Recovers rejected orders with Bitso fills
└─────────────────┘
```

## Recommendations for Future

1. **Synchronize Risk Checks:** Consider having trading-engine check with order-management before placing orders to avoid rejected orders being placed on Bitso.

2. **Add Integration Tests:** Create automated tests that verify the full BUY→SELL→P&L flow.

3. **Monitor Stale Orders:** Add Prometheus metrics for stale order counts to detect sync issues early.

4. **Position Recovery on Startup:** Consider adding a startup routine that reconciles positions from order history.

## Files Modified

| File | Change |
|------|--------|
| `internal/repository/redis_position_repository.go` | New file - Redis position persistence |
| `internal/sync/bitso_sync.go` | Added stale order tracking and cleanup |
| `internal/sync/user_trades_poller.go` | Handle rejected orders with fills |
| `internal/manager/order_manager.go` | Added MarkOrderStale, updated sync methods |
| `internal/manager/state_machine.go` | Allow rejected → filled transitions |

## Date

April 3, 2026
