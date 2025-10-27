# Order Management Service - Architecture Summary

## System Context

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Trading Bot Microservices                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│   ┌───────────────┐       ┌───────────────┐       ┌───────────────┐        │
│   │   Market      │       │   Strategy    │       │    Order      │        │
│   │   Data        │──────▶│   Executor    │──────▶│  Management   │──┐     │
│   │   Service     │       │   Service     │       │   Service     │  │     │
│   └───────────────┘       └───────────────┘       └───────────────┘  │     │
│         │                         │                        │           │     │
│         │ market-data.trades      │ strategy-executor     │ order-    │     │
│         │ market-data.orderbook   │ .signals              │ management│     │
│         │ market-data.ticker      │                       │ .orders   │     │
│         ▼                         ▼                        ▼           │     │
│   ┌─────────────────────────────────────────────────────────────┐    │     │
│   │                      Kafka Topics                            │    │     │
│   └─────────────────────────────────────────────────────────────┘    │     │
│                                                                        │     │
│   ┌───────────────┐                                                   │     │
│   │    Trading    │◀──────────────────────────────────────────────────┘     │
│   │    Engine     │                                                          │
│   │    Service    │                                                          │
│   └───────────────┘                                                          │
│         │                                                                     │
│         ▼                                                                     │
│   ┌───────────────┐                                                          │
│   │  Bitso API    │                                                          │
│   │  (Exchange)   │                                                          │
│   └───────────────┘                                                          │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Order Management Service - Internal Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                        Order Management Service                              │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                               │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                         External Interfaces                          │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                       │   │
│  │  ┌──────────────┐              ┌──────────────┐                     │   │
│  │  │    Kafka     │              │     HTTP     │                     │   │
│  │  │   Consumer   │              │     API      │                     │   │
│  │  │  (Signals)   │              │   (REST)     │                     │   │
│  │  └──────┬───────┘              └──────┬───────┘                     │   │
│  │         │                              │                             │   │
│  └─────────┼──────────────────────────────┼─────────────────────────────┘   │
│            │                              │                                 │
│            ▼                              ▼                                 │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                        Application Layer                             │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                       │   │
│  │  ┌──────────────┐              ┌──────────────┐                     │   │
│  │  │   Signal     │              │     API      │                     │   │
│  │  │   Consumer   │              │   Handlers   │                     │   │
│  │  └──────┬───────┘              └──────┬───────┘                     │   │
│  │         │                              │                             │   │
│  │         └──────────────┬───────────────┘                             │   │
│  │                        ▼                                             │   │
│  │              ┌──────────────────┐                                    │   │
│  │              │  Order Manager   │                                    │   │
│  │              │  (Orchestrator)  │                                    │   │
│  │              └────────┬─────────┘                                    │   │
│  │                       │                                              │   │
│  └───────────────────────┼──────────────────────────────────────────────┘   │
│                          │                                                  │
│                          ▼                                                  │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                        Business Logic Layer                          │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                       │   │
│  │  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐            │   │
│  │  │   Order      │   │     Risk     │   │    State     │            │   │
│  │  │  Validator   │   │   Manager    │   │   Machine    │            │   │
│  │  └──────────────┘   └──────────────┘   └──────────────┘            │   │
│  │                                                                       │   │
│  └───────────────────────────────────────────────────────────────────────┘   │
│                          │                                                  │
│                          ▼                                                  │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                        Data Access Layer                             │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                       │   │
│  │  ┌──────────────┐              ┌──────────────┐                     │   │
│  │  │    Order     │              │   Position   │                     │   │
│  │  │  Repository  │              │  Repository  │                     │   │
│  │  └──────┬───────┘              └──────┬───────┘                     │   │
│  │         │                              │                             │   │
│  └─────────┼──────────────────────────────┼─────────────────────────────┘   │
│            │                              │                                 │
│            ▼                              ▼                                 │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    External Dependencies                             │   │
│  ├─────────────────────────────────────────────────────────────────────┤   │
│  │                                                                       │   │
│  │  ┌──────────────┐              ┌──────────────┐                     │   │
│  │  │    Redis     │              │     Kafka    │                     │   │
│  │  │  (Storage)   │              │  (Publisher) │                     │   │
│  │  └──────────────┘              └──────────────┘                     │   │
│  │                                                                       │   │
│  │  ┌──────────────┐              ┌──────────────┐                     │   │
│  │  │   Trading    │              │ Prometheus   │                     │   │
│  │  │   Engine     │              │  (Metrics)   │                     │   │
│  │  └──────────────┘              └──────────────┘                     │   │
│  │                                                                       │   │
│  └───────────────────────────────────────────────────────────────────────┘   │
│                                                                               │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Data Flow Diagram

### Signal to Order Flow

```
┌────────────────────────────────────────────────────────────────────────────┐
│                         Signal Processing Flow                             │
└────────────────────────────────────────────────────────────────────────────┘

1. Signal Arrival
   ┌──────────────────┐
   │ Strategy-Executor│
   │    (Kafka)       │
   └────────┬─────────┘
            │ TradeSignalEvent
            │ {
            │   signal: "BUY",
            │   book: "btc_mxn",
            │   price: 500000,
            │   amount: 0.01
            │ }
            ▼
   ┌──────────────────┐
   │ Signal Consumer  │──── Deserialize
   └────────┬─────────┘     Parse JSON
            │
            ▼

2. Validation Phase
   ┌──────────────────┐
   │ Order Validator  │──── Check signal format
   │                  │     Check book validity
   │  ✓ Signal Valid  │     Check amount/price
   └────────┬─────────┘
            │
            ▼

3. Order Creation
   ┌──────────────────┐
   │  Order Manager   │──── Generate Order ID
   │                  │     Create Order object
   │  Order Created   │     Set Status: PENDING
   └────────┬─────────┘
            │ Order {
            │   id: "ord-123",
            │   status: "pending",
            │   book: "btc_mxn",
            │   side: "buy"
            │ }
            ▼

4. Risk Checks
   ┌──────────────────┐
   │  Risk Manager    │──── Check position limits
   │                  │     Check order limits
   │  ✓ Risk OK       │     Check exposure
   └────────┬─────────┘
            │
            ▼

5. Order Validation
   ┌──────────────────┐
   │ Order Validator  │──── Validate order size
   │                  │     Validate order value
   │  ✓ Order Valid   │     Check duplicates
   └────────┬─────────┘
            │
            ▼

6. Status Update
   ┌──────────────────┐
   │  State Machine   │──── PENDING → VALIDATED
   └────────┬─────────┘
            │
            ▼

7. Persistence
   ┌──────────────────┐
   │ Order Repository │──── Save to Redis
   │                  │     Update indexes
   │  ✓ Saved         │     Set TTL
   └────────┬─────────┘
            │
            ▼

8. Order Submission
   ┌──────────────────┐
   │ Order Executor   │──── HTTP POST to Trading-Engine
   │                  │     /api/v1/orders
   │  ✓ Submitted     │     Wait for ACK
   └────────┬─────────┘
            │
            ▼

9. Status Update
   ┌──────────────────┐
   │  State Machine   │──── VALIDATED → SUBMITTED
   └────────┬─────────┘
            │
            ▼

10. Event Publishing
   ┌──────────────────┐
   │ Event Publisher  │──── Publish to Kafka
   │                  │     order-management.orders
   │  ✓ Published     │     order-management.events
   └────────┬─────────┘
            │
            ▼

11. Monitoring & Metrics
   ┌──────────────────┐
   │ Metrics Collector│──── orders_created_total++
   │                  │     order_processing_duration
   │  ✓ Recorded      │     Update Prometheus
   └──────────────────┘

Time: ~50-100ms (end-to-end)
```

---

## State Machine Diagram

```
┌────────────────────────────────────────────────────────────────────────────┐
│                          Order State Machine                               │
└────────────────────────────────────────────────────────────────────────────┘

                              ┌─────────────┐
                              │   PENDING   │◀──── Signal Received
                              └──────┬──────┘
                                     │
                          ┌──────────┴──────────┐
                          │                     │
                    ✓ Validated          ✗ Rejected
                          │                     │
                   ┌──────▼──────┐       ┌─────▼─────┐
                   │  VALIDATED  │       │ REJECTED  │ [FINAL]
                   └──────┬──────┘       └───────────┘
                          │
                          │ Submit to Engine
                          │
                   ┌──────▼──────┐
                   │  SUBMITTED  │
                   └──────┬──────┘
                          │
                ┌─────────┴─────────┐
                │                   │
          ✓ Accepted          ✗ Rejected
                │                   │
         ┌──────▼──────┐     ┌─────▼─────┐
         │  ACCEPTED   │     │ REJECTED  │ [FINAL]
         └──────┬──────┘     └───────────┘
                │
        ┌───────┴────────────────┐
        │                        │
   Exchange Fill          User Cancels
        │                        │
        ▼                        ▼
┌────────────────┐      ┌───────────────┐
│ PARTIALLY_FILL │      │   CANCELLED   │ [FINAL]
└────────┬───────┘      └───────────────┘
         │
         │ Complete Fill
         │
   ┌─────▼─────┐
   │  FILLED   │ [FINAL]
   └───────────┘


Valid Transitions:
──────────────────
PENDING          → VALIDATED, REJECTED
VALIDATED        → SUBMITTED, REJECTED
SUBMITTED        → ACCEPTED, REJECTED
ACCEPTED         → PARTIALLY_FILLED, FILLED, CANCELLED
PARTIALLY_FILLED → FILLED, CANCELLED

Invalid Transitions (will be rejected):
────────────────────────────────────
FILLED     → Any (final state)
REJECTED   → Any (final state)
CANCELLED  → Any (final state)
PENDING    → FILLED (must go through validation)
```

---

## Component Interaction Diagram

```
┌────────────────────────────────────────────────────────────────────────────┐
│                      Component Interactions                                │
└────────────────────────────────────────────────────────────────────────────┘

┌─────────────────┐
│  HTTP Client    │
└────────┬────────┘
         │ GET /api/v1/orders
         ▼
┌─────────────────┐
│  API Handlers   │────┐
└────────┬────────┘    │
         │             │ Uses
         │             ▼
         │        ┌────────────────┐
         │        │ Order Manager  │
         │        └────────┬───────┘
         │                 │
         │        ┌────────┼────────────────────┐
         │        │        │                    │
         │        ▼        ▼                    ▼
         │   ┌─────────┐ ┌──────────┐   ┌─────────────┐
         │   │Validator│ │   Risk   │   │    State    │
         │   │         │ │  Manager │   │   Machine   │
         │   └────┬────┘ └────┬─────┘   └──────┬──────┘
         │        │           │                 │
         │        └───────────┼─────────────────┘
         │                    │
         │                    ▼
         │           ┌─────────────────┐
         │           │   Repository    │
         │           └────────┬────────┘
         │                    │
         │                    ▼
         │              ┌──────────┐
         │              │  Redis   │
         │              └──────────┘
         │
         │ Parallel Operations
         │
         ├──────────────┐
         │              │
         ▼              ▼
┌─────────────┐  ┌─────────────┐
│  Publisher  │  │  Executor   │
└──────┬──────┘  └──────┬──────┘
       │                │
       ▼                ▼
  ┌────────┐      ┌──────────────┐
  │ Kafka  │      │ Trading Eng. │
  └────────┘      └──────────────┘
```

---

## Data Model

### Order Entity

```
┌────────────────────────────────────────────────────────────────┐
│                          Order                                  │
├────────────────────────────────────────────────────────────────┤
│ Identification:                                                 │
│   • ID              : string (UUID)                            │
│   • ClientOrderID   : string (UUID)                            │
│   • SignalID        : string (Reference to signal)            │
│                                                                 │
│ Order Details:                                                  │
│   • Book            : string (e.g., "btc_mxn")                │
│   • Side            : string ("buy", "sell")                   │
│   • Type            : string ("market", "limit")              │
│   • Status          : OrderStatus (enum)                       │
│                                                                 │
│ Pricing:                                                        │
│   • Price           : float64 (Limit price)                    │
│   • Amount          : float64 (Total amount)                   │
│   • FilledAmount    : float64 (Filled amount)                 │
│   • RemainingAmount : float64 (Remaining)                      │
│   • AveragePrice    : float64 (Average fill price)            │
│                                                                 │
│ Metadata:                                                       │
│   • Strategy        : string (Strategy name)                   │
│   • CreatedAt       : time.Time                               │
│   • UpdatedAt       : time.Time                               │
│   • SubmittedAt     : *time.Time                              │
│   • FilledAt        : *time.Time                              │
│   • CancelledAt     : *time.Time                              │
│   • RejectionReason : string                                   │
│   • Metadata        : map[string]interface{}                   │
└────────────────────────────────────────────────────────────────┘
```

### Position Entity

```
┌────────────────────────────────────────────────────────────────┐
│                         Position                                │
├────────────────────────────────────────────────────────────────┤
│ • ID              : string (UUID)                              │
│ • Book            : string (e.g., "btc_mxn")                  │
│ • Side            : string ("long", "short")                   │
│ • Size            : float64 (Position size)                    │
│ • EntryPrice      : float64 (Average entry)                   │
│ • CurrentPrice    : float64 (Current market)                  │
│ • UnrealizedPnL   : float64 (Unrealized P&L)                 │
│ • RealizedPnL     : float64 (Realized P&L)                   │
│ • OpenOrdersCount : int (Active orders)                        │
│ • OpenedAt        : time.Time                                 │
│ • UpdatedAt       : time.Time                                 │
└────────────────────────────────────────────────────────────────┘
```

---

## Redis Data Structure

```
┌────────────────────────────────────────────────────────────────┐
│                     Redis Key Design                            │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Orders:                                                         │
│   order:{id}                    → Hash (Order data)            │
│   orders:active                 → Set (Active order IDs)       │
│   orders:book:{book}            → Set (Order IDs by book)      │
│   orders:signal:{signal_id}     → String (Order ID)            │
│   orders:status:{status}        → Set (Order IDs by status)    │
│   orders:strategy:{strategy}    → Set (Order IDs by strategy)  │
│                                                                 │
│ Positions:                                                      │
│   position:{book}               → Hash (Position data)          │
│   positions:all                 → Set (All position books)      │
│                                                                 │
│ Indexes:                                                        │
│   idx:orders:created:{date}     → Sorted Set (by timestamp)    │
│   idx:orders:filled:{date}      → Sorted Set (by fill time)    │
│                                                                 │
│ Rate Limiting:                                                  │
│   ratelimit:orders:{minute}     → Counter (Orders per minute)   │
│                                                                 │
│ TTL Strategy:                                                   │
│   • Active orders: No TTL                                       │
│   • Closed orders: 30 days                                     │
│   • Rate limit keys: 1 minute                                  │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

---

## API Endpoints

```
┌────────────────────────────────────────────────────────────────┐
│                     REST API Endpoints                          │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Health & Status:                                                │
│   GET  /health                  → Full health check            │
│   GET  /health/ready            → Readiness probe              │
│   GET  /health/live             → Liveness probe               │
│   GET  /api/v1/status           → Service status               │
│                                                                 │
│ Orders:                                                         │
│   GET  /api/v1/orders           → List orders (filtered)       │
│        ?book=btc_mxn                                           │
│        &status=active                                           │
│        &strategy=basic                                          │
│        &limit=10                                                │
│        &offset=0                                                │
│                                                                 │
│   GET  /api/v1/orders/{id}      → Get order details            │
│                                                                 │
│   POST /api/v1/orders/{id}/cancel → Cancel order               │
│                                                                 │
│   GET  /api/v1/orders/active    → Get active orders            │
│                                                                 │
│   GET  /api/v1/orders/history   → Get order history            │
│        ?from=2025-10-01                                         │
│        &to=2025-10-24                                           │
│                                                                 │
│ Positions:                                                      │
│   GET  /api/v1/positions        → List all positions           │
│                                                                 │
│   GET  /api/v1/positions/{book} → Get position by book         │
│                                                                 │
│   GET  /api/v1/positions/summary → Position summary            │
│                                                                 │
│ Metrics:                                                        │
│   GET  /metrics                 → Prometheus metrics           │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

---

## Kafka Topics

```
┌────────────────────────────────────────────────────────────────┐
│                        Kafka Topics                             │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Input (Consumer):                                               │
│   • strategy-executor.signals                                   │
│     - Trading signals from strategies                           │
│     - Message: TradeSignalEvent                                │
│     - Partition Key: book                                       │
│                                                                 │
│ Output (Producer):                                              │
│   • order-management.orders                                     │
│     - Order commands for trading-engine                         │
│     - Message: Order                                           │
│     - Partition Key: book                                       │
│                                                                 │
│   • order-management.events                                     │
│     - Order lifecycle events                                    │
│     - Message: OrderEvent                                      │
│     - Partition Key: order_id                                   │
│     - Events: created, validated, submitted,                    │
│               accepted, filled, cancelled, rejected             │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

---

## Monitoring & Observability

```
┌────────────────────────────────────────────────────────────────┐
│                    Metrics & Monitoring                         │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Order Metrics:                                                  │
│   orders_created_total{book, strategy}                         │
│   orders_filled_total{book, strategy}                          │
│   orders_cancelled_total{book, strategy, reason}               │
│   orders_rejected_total{book, strategy, reason}                │
│   orders_active{book}                                           │
│   order_processing_duration_seconds{stage}                     │
│                                                                 │
│ Validation Metrics:                                             │
│   validations_total{type, result}                              │
│   validation_duration_seconds{type}                            │
│                                                                 │
│ Risk Metrics:                                                   │
│   risk_checks_total{check_type, result}                        │
│   risk_violations_total{violation_type}                        │
│                                                                 │
│ Repository Metrics:                                             │
│   repository_operations_total{operation, result}               │
│   repository_duration_seconds{operation}                       │
│                                                                 │
│ Publisher Metrics:                                              │
│   events_published_total{event_type}                           │
│   events_failed_total{event_type, reason}                      │
│                                                                 │
│ System Metrics:                                                 │
│   service_uptime_seconds                                        │
│   service_health{status}                                        │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

---

## Error Handling Strategy

```
┌────────────────────────────────────────────────────────────────┐
│                      Error Categories                           │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│ 1. Validation Errors (400 Bad Request)                         │
│    • Invalid signal format                                      │
│    • Invalid order parameters                                   │
│    • Out of range values                                        │
│    → Response: ValidationError with details                     │
│    → Action: Reject order, log warning                         │
│                                                                 │
│ 2. Business Logic Errors (422 Unprocessable)                   │
│    • Risk limit exceeded                                        │
│    • Position limit exceeded                                    │
│    • Duplicate order                                            │
│    → Response: BusinessError with reason                        │
│    → Action: Reject order, update metrics                      │
│                                                                 │
│ 3. Integration Errors (502/503 Service Unavailable)            │
│    • Trading-engine unavailable                                 │
│    • Kafka connection failure                                   │
│    • Redis connection failure                                   │
│    → Response: ServiceError                                     │
│    → Action: Retry with backoff, alert if persistent           │
│                                                                 │
│ 4. System Errors (500 Internal Server Error)                   │
│    • Unexpected exceptions                                      │
│    • Data corruption                                            │
│    • Resource exhaustion                                        │
│    → Response: InternalError (generic)                          │
│    → Action: Log error, alert, return order to pending         │
│                                                                 │
│ Retry Strategy:                                                 │
│   • Transient errors: Exponential backoff (3 attempts)         │
│   • Network errors: Circuit breaker pattern                     │
│   • Validation errors: No retry (immediate rejection)          │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

---

## Performance Characteristics

```
┌────────────────────────────────────────────────────────────────┐
│                    Performance Targets                          │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Latency:                                                        │
│   • Signal to Order Creation:     <  50ms (P50)               │
│   • Validation:                   <  10ms (P50)               │
│   • Risk Check:                   <  20ms (P50)               │
│   • Repository Operation:         <  10ms (P50)               │
│   • End-to-End Processing:        < 100ms (P50)               │
│   • API Response Time:            <  50ms (P95)               │
│                                                                 │
│ Throughput:                                                     │
│   • Orders Created:               > 100 orders/sec             │
│   • Orders Updated:               > 500 updates/sec            │
│   • API Requests:                 > 1000 req/sec               │
│                                                                 │
│ Reliability:                                                    │
│   • Service Uptime:               99.9%                        │
│   • Data Durability:              100% (Redis persistence)     │
│   • Message Processing:           At-least-once delivery       │
│                                                                 │
│ Scalability:                                                    │
│   • Horizontal Scaling:           Stateless design             │
│   • Kafka Partitions:             Per-book partitioning        │
│   • Redis Sharding:               Ready for sharding           │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

---

## Security Considerations

```
┌────────────────────────────────────────────────────────────────┐
│                    Security Measures                            │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Input Validation:                                               │
│   ✓ Strict type checking                                       │
│   ✓ Range validation                                           │
│   ✓ Format validation                                          │
│   ✓ Sanitization of user input                                │
│                                                                 │
│ Access Control:                                                 │
│   ○ API authentication (planned)                                │
│   ○ Role-based authorization (planned)                          │
│   ✓ Internal service communication (mTLS ready)                │
│                                                                 │
│ Data Protection:                                                │
│   ✓ Sensitive data logging prevention                          │
│   ○ Encryption at rest (planned)                                │
│   ○ Encryption in transit (planned)                             │
│                                                                 │
│ Audit Trail:                                                    │
│   ✓ Complete order lifecycle logging                           │
│   ✓ All state changes recorded                                 │
│   ✓ Event publishing for audit                                │
│                                                                 │
│ Rate Limiting:                                                  │
│   ✓ Order creation limits                                      │
│   ✓ API request limits                                         │
│   ✓ Per-strategy limits                                        │
│                                                                 │
│ Error Handling:                                                 │
│   ✓ No sensitive data in errors                                │
│   ✓ Generic error messages to clients                          │
│   ✓ Detailed logging server-side                              │
│                                                                 │
└────────────────────────────────────────────────────────────────┘

Legend: ✓ Implemented  ○ Planned
```

---

## Deployment Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                   Kubernetes Deployment                         │
├────────────────────────────────────────────────────────────────┤
│                                                                 │
│ Deployment:                                                     │
│   • Replicas: 3 (high availability)                            │
│   • Strategy: RollingUpdate                                     │
│   • Resources:                                                  │
│     - CPU: 500m request, 1000m limit                           │
│     - Memory: 512Mi request, 1Gi limit                         │
│                                                                 │
│ Service:                                                        │
│   • Type: ClusterIP                                            │
│   • Port: 8080 (HTTP)                                          │
│   • Selector: app=order-management                             │
│                                                                 │
│ ConfigMap:                                                      │
│   • Service configuration                                       │
│   • Feature flags                                               │
│   • Non-sensitive settings                                      │
│                                                                 │
│ Secret:                                                         │
│   • Kafka credentials                                           │
│   • Redis password                                              │
│   • API keys                                                    │
│                                                                 │
│ Health Checks:                                                  │
│   • Liveness:  /health/live  (every 10s)                       │
│   • Readiness: /health/ready (every 5s)                        │
│   • Startup:   /health/ready (initial delay 30s)               │
│                                                                 │
│ Monitoring:                                                     │
│   • Prometheus scraping: /metrics                               │
│   • Log aggregation: ELK/Loki                                  │
│   • Distributed tracing: Jaeger (future)                        │
│                                                                 │
└────────────────────────────────────────────────────────────────┘
```

---

## Summary

### Key Characteristics

1. **Stateless**: All state in Redis, enables horizontal scaling
2. **Event-Driven**: Kafka-based communication
3. **Resilient**: Retry logic, circuit breakers, graceful degradation
4. **Observable**: Comprehensive metrics, logging, health checks
5. **Testable**: High test coverage, mocked dependencies
6. **Maintainable**: Clean architecture, SOLID principles

### Integration Points

**Consumes**:
- Kafka: `strategy-executor.signals`

**Produces**:
- Kafka: `order-management.orders`, `order-management.events`

**Stores**:
- Redis: Orders, positions, indexes

**Calls**:
- Trading-Engine API: Order submission, status queries

**Exposes**:
- HTTP API: Order management operations
- Prometheus: Metrics endpoint
- Health: Health check endpoints

### Technology Stack

- **Language**: Go 1.21+
- **Messaging**: Kafka (segmentio/kafka-go)
- **Storage**: Redis (go-redis/v9)
- **Metrics**: Prometheus (client_golang)
- **Logging**: Zerolog
- **HTTP**: Standard library net/http
- **Testing**: Go testing + testify

---

This architecture provides a robust, scalable, and maintainable foundation for the Order Management Service, following industry best practices and aligning with the existing microservices architecture.

