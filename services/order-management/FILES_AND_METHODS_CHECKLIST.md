# Order Management Service - Files & Methods Checklist

## Overview

This document provides a comprehensive checklist of all files, methods, and tests to be implemented for the Order Management Service.

---

## 📁 File Structure

```
services/order-management/
├── cmd/
│   └── main.go                                 ✅ EXISTS (needs implementation)
├── internal/
│   ├── api/
│   │   ├── handlers.go                         ❌ NEW
│   │   ├── handlers_test.go                    ❌ NEW
│   │   └── responses.go                        ❌ NEW
│   ├── config/
│   │   ├── config.go                           ❌ NEW
│   │   └── config_test.go                      ❌ NEW
│   ├── consumer/
│   │   ├── signal_consumer.go                  ❌ NEW
│   │   └── signal_consumer_test.go             ❌ NEW
│   ├── executor/
│   │   ├── order_executor.go                   ❌ NEW
│   │   ├── order_executor_test.go              ❌ NEW
│   │   └── client.go                           ❌ NEW
│   ├── logger/
│   │   └── logger.go                           ❌ NEW
│   ├── manager/
│   │   ├── order_manager.go                    ❌ NEW
│   │   ├── state_machine.go                    ❌ NEW
│   │   ├── manager_test.go                     ❌ NEW
│   │   └── state_machine_test.go               ❌ NEW
│   ├── metrics/
│   │   ├── prometheus.go                       ❌ NEW
│   │   └── metrics_test.go                     ❌ NEW
│   ├── models/
│   │   ├── order.go                            ❌ NEW
│   │   ├── position.go                         ❌ NEW
│   │   ├── validation.go                       ❌ NEW
│   │   └── filters.go                          ❌ NEW
│   ├── publisher/
│   │   ├── event_publisher.go                  ❌ NEW
│   │   └── event_publisher_test.go             ❌ NEW
│   ├── repository/
│   │   ├── order_repository.go                 ❌ NEW
│   │   ├── position_repository.go              ❌ NEW
│   │   ├── repository_test.go                  ❌ NEW
│   │   └── interfaces.go                       ❌ NEW
│   ├── risk/
│   │   ├── risk_manager.go                     ❌ NEW
│   │   ├── risk_manager_test.go                ❌ NEW
│   │   └── rules.go                            ❌ NEW
│   ├── server/
│   │   ├── http_server.go                      ❌ NEW
│   │   └── http_server_test.go                 ❌ NEW
│   └── validator/
│       ├── order_validator.go                  ❌ NEW
│       ├── validator_test.go                   ❌ NEW
│       └── rules.go                            ❌ NEW
├── Dockerfile                                  ✅ EXISTS
├── go.mod                                      ✅ EXISTS (needs update)
├── go.sum                                      ❌ NEW
├── README.md                                   ❌ NEW
├── TESTING.md                                  ❌ NEW
├── API.md                                      ❌ NEW
└── run_tests.sh                                ❌ NEW
```

**Summary**: 1 partial file, 39 new files to create

---

## 📝 Detailed File Specifications

### 1. `cmd/main.go`

**Purpose**: Application entry point and orchestration

**Type Definitions**:
```go
type Application struct {
    logger           *logger.Logger
    config           *config.Config
    healthManager    *health.HealthManager
    metricsCollector *metrics.MetricsCollector
    
    // Core components
    signalConsumer   *consumer.SignalConsumer
    orderManager     *manager.OrderManager
    orderValidator   *validator.OrderValidator
    riskManager      *risk.RiskManager
    orderRepository  repository.OrderRepository
    positionRepo     repository.PositionRepository
    eventPublisher   *publisher.EventPublisher
    orderExecutor    *executor.OrderExecutor
    httpServer       *server.HTTPServer
    
    // Context
    ctx    context.Context
    cancel context.CancelFunc
}
```

**Functions**:
- `NewApplication() (*Application, error)` - Create and wire dependencies
- `(app *Application) Start() error` - Start all components
- `(app *Application) Stop() error` - Graceful shutdown
- `(app *Application) Run() error` - Main execution loop
- `main()` - Entry point

**Initialization Order**:
1. Load configuration
2. Initialize logger
3. Initialize metrics
4. Initialize health manager
5. Initialize Redis client
6. Initialize repositories
7. Initialize Kafka producer/consumer
8. Initialize validators and risk manager
9. Initialize order manager
10. Initialize HTTP server
11. Start all components
12. Wait for shutdown signal

---

### 2. `internal/config/config.go`

**Type Definitions**:
```go
type Config struct {
    Service      ServiceConfig
    Kafka        KafkaConfig
    Redis        RedisConfig
    TradingEngine TradingEngineConfig
    Risk         RiskConfig
    Logging      LoggingConfig
    Metrics      MetricsConfig
}

type ServiceConfig struct {
    Name        string
    Version     string
    Host        string
    Port        int
    Environment string
}

type KafkaConfig struct {
    Brokers         []string
    ConsumerGroup   string
    TopicSignals    string
    TopicOrders     string
    TopicEvents     string
    BatchSize       int
    BatchTimeout    time.Duration
    AutoOffsetReset string
    CommitInterval  time.Duration
}

type RedisConfig struct {
    Host     string
    Port     int
    Password string
    DB       int
    PoolSize int
}

type TradingEngineConfig struct {
    BaseURL    string
    Timeout    time.Duration
    RetryCount int
    RetryDelay time.Duration
}

type RiskConfig struct {
    MaxOpenOrders        int
    MaxOrderValue        float64
    MinOrderSize         float64
    MaxPositionSize      float64
    EnableDuplicateCheck bool
    MaxOrdersPerMinute   int
}

type LoggingConfig struct {
    Level  string
    Format string
    Output string
}

type MetricsConfig struct {
    Enabled bool
    Path    string
    Port    int
}
```

**Functions**:
- `Load() (*Config, error)` - Load from environment variables
- `(c *Config) Validate() error` - Validate configuration
- `getEnv(key, default string) string` - Helper
- `getEnvAsInt(key string, default int) int` - Helper
- `getEnvAsFloat(key string, default float64) float64` - Helper
- `getEnvAsBool(key string, default bool) bool` - Helper
- `getEnvAsDuration(key string, default time.Duration) time.Duration` - Helper
- `getEnvAsSlice(key string, default []string) []string` - Helper

**Tests** (`config_test.go`):
- `TestLoad()` - Load default config
- `TestLoadWithEnv()` - Load with env vars
- `TestValidate()` - Validation rules
- `TestValidateInvalid()` - Invalid configs

---

### 3. `internal/models/order.go`

**Type Definitions**:
```go
type OrderStatus string
const (
    OrderStatusPending         OrderStatus = "pending"
    OrderStatusValidated       OrderStatus = "validated"
    OrderStatusSubmitted       OrderStatus = "submitted"
    OrderStatusAccepted        OrderStatus = "accepted"
    OrderStatusPartiallyFilled OrderStatus = "partially_filled"
    OrderStatusFilled          OrderStatus = "filled"
    OrderStatusCancelled       OrderStatus = "cancelled"
    OrderStatusRejected        OrderStatus = "rejected"
)

type Order struct {
    ID              string
    ClientOrderID   string
    SignalID        string
    Book            string
    Side            string
    Type            string
    Status          OrderStatus
    Price           float64
    Amount          float64
    FilledAmount    float64
    RemainingAmount float64
    AveragePrice    float64
    Strategy        string
    CreatedAt       time.Time
    UpdatedAt       time.Time
    SubmittedAt     *time.Time
    FilledAt        *time.Time
    CancelledAt     *time.Time
    RejectionReason string
    Metadata        map[string]interface{}
}

type OrderEvent struct {
    EventID   string
    EventType string
    OrderID   string
    Order     *Order
    Timestamp time.Time
    Metadata  map[string]interface{}
}
```

**Methods**:
- `(o *Order) IsActive() bool` - Check if order is active
- `(o *Order) IsClosed() bool` - Check if order is closed
- `(o *Order) UpdateStatus(status OrderStatus)` - Update status
- `(o *Order) RecordFill(amount, price float64)` - Record fill
- `(o *Order) GetFillPercentage() float64` - Calculate fill percentage
- `(o *Order) Validate() error` - Validate order data
- `(o *Order) ToJSON() ([]byte, error)` - Serialize to JSON
- `OrderFromJSON(data []byte) (*Order, error)` - Deserialize from JSON

---

### 4. `internal/models/position.go`

**Type Definitions**:
```go
type Position struct {
    ID              string
    Book            string
    Side            string
    Size            float64
    EntryPrice      float64
    CurrentPrice    float64
    UnrealizedPnL   float64
    RealizedPnL     float64
    OpenOrdersCount int
    OpenedAt        time.Time
    UpdatedAt       time.Time
}
```

**Methods**:
- `(p *Position) UpdatePrice(price float64)` - Update current price
- `(p *Position) CalculatePnL() float64` - Calculate P&L
- `(p *Position) AddOrder(order *Order)` - Add order to position
- `(p *Position) RemoveOrder(orderID string)` - Remove order
- `(p *Position) Close(price float64)` - Close position
- `(p *Position) ToJSON() ([]byte, error)` - Serialize
- `PositionFromJSON(data []byte) (*Position, error)` - Deserialize

---

### 5. `internal/models/validation.go`

**Type Definitions**:
```go
type ValidationError struct {
    Field   string
    Message string
    Code    string
}

type ValidationResult struct {
    Valid  bool
    Errors []ValidationError
}
```

**Methods**:
- `(vr *ValidationResult) AddError(field, message, code string)` - Add error
- `(vr *ValidationResult) HasErrors() bool` - Check for errors
- `(vr *ValidationResult) Error() string` - Get error message

---

### 6. `internal/models/filters.go`

**Type Definitions**:
```go
type OrderFilters struct {
    Book      string
    Status    OrderStatus
    Strategy  string
    Side      string
    FromDate  time.Time
    ToDate    time.Time
    Limit     int
    Offset    int
}
```

**Methods**:
- `(f *OrderFilters) Apply(orders []*Order) []*Order` - Apply filters
- `(f *OrderFilters) Validate() error` - Validate filters
- `(f *OrderFilters) ToQueryParams() map[string]string` - Convert to query params

---

### 7. `internal/consumer/signal_consumer.go`

**Interface**:
```go
type SignalConsumer interface {
    Start(ctx context.Context) error
    Stop() error
    GetSignalStream() <-chan *models.TradeSignalEvent
}
```

**Type Definitions**:
```go
type Consumer struct {
    logger   *logger.Logger
    metrics  *metrics.MetricsCollector
    consumer *kafka.Consumer
    signals  chan *models.TradeSignalEvent
    stopChan chan struct{}
    wg       sync.WaitGroup
}
```

**Functions**:
- `NewSignalConsumer(config *ConsumerConfig, logger *logger.Logger, metrics *metrics.MetricsCollector) (*Consumer, error)`

**Methods**:
- `(c *Consumer) Start(ctx context.Context) error` - Start consuming
- `(c *Consumer) Stop() error` - Stop consumer
- `(c *Consumer) GetSignalStream() <-chan *models.TradeSignalEvent` - Get signal channel
- `(c *Consumer) consumeLoop(ctx context.Context)` - Main consume loop
- `(c *Consumer) processMessage(msg kafka.Message) error` - Process single message
- `(c *Consumer) parseSignal(data []byte) (*models.TradeSignalEvent, error)` - Parse signal

**Tests** (`signal_consumer_test.go`):
- `TestNewSignalConsumer()` - Creation
- `TestConsumerStart()` - Start logic
- `TestConsumerStop()` - Stop logic
- `TestProcessMessage()` - Message processing
- `TestParseSignal()` - Signal parsing
- `TestInvalidSignal()` - Invalid signal handling

---

### 8. `internal/validator/order_validator.go`

**Interface**:
```go
type OrderValidator interface {
    ValidateSignal(signal *models.TradeSignalEvent) error
    ValidateOrder(order *models.Order) error
    ValidateOrderSize(order *models.Order) error
    ValidateOrderValue(order *models.Order) error
}
```

**Type Definitions**:
```go
type Validator struct {
    logger *logger.Logger
    config *config.RiskConfig
    repo   repository.OrderRepository
}
```

**Functions**:
- `NewOrderValidator(config *config.RiskConfig, logger *logger.Logger, repo repository.OrderRepository) *Validator`

**Methods**:
- `(v *Validator) ValidateSignal(signal *models.TradeSignalEvent) error` - Validate signal
- `(v *Validator) ValidateOrder(order *models.Order) error` - Validate order
- `(v *Validator) ValidateOrderSize(order *models.Order) error` - Check size limits
- `(v *Validator) ValidateOrderValue(order *models.Order) error` - Check value limits
- `(v *Validator) ValidateBook(book string) error` - Validate trading book
- `(v *Validator) ValidateSide(side string) error` - Validate order side
- `(v *Validator) ValidateType(orderType string) error` - Validate order type
- `(v *Validator) CheckDuplicates(order *models.Order) error` - Check for duplicates

**Tests** (`validator_test.go`):
- `TestValidateSignal()` - Valid signals
- `TestValidateSignalInvalid()` - Invalid signals
- `TestValidateOrder()` - Valid orders
- `TestValidateOrderSize()` - Size validation
- `TestValidateOrderValue()` - Value validation
- `TestCheckDuplicates()` - Duplicate detection

---

### 9. `internal/risk/risk_manager.go`

**Interface**:
```go
type RiskManager interface {
    CheckRisk(ctx context.Context, order *models.Order) error
    CheckPositionLimits(order *models.Order) error
    CheckOrderLimits(order *models.Order) error
    GetPositionLimits(book string) (*PositionLimits, error)
    GetCurrentExposure(book string) (*Exposure, error)
}
```

**Type Definitions**:
```go
type Manager struct {
    logger      *logger.Logger
    config      *config.RiskConfig
    orderRepo   repository.OrderRepository
    positionRepo repository.PositionRepository
    metrics     *metrics.MetricsCollector
}

type PositionLimits struct {
    MaxSize  float64
    MaxValue float64
}

type Exposure struct {
    TotalSize  float64
    TotalValue float64
    OpenOrders int
}
```

**Functions**:
- `NewRiskManager(config *config.RiskConfig, logger *logger.Logger, orderRepo repository.OrderRepository, positionRepo repository.PositionRepository, metrics *metrics.MetricsCollector) *Manager`

**Methods**:
- `(rm *Manager) CheckRisk(ctx context.Context, order *models.Order) error` - Comprehensive risk check
- `(rm *Manager) CheckPositionLimits(order *models.Order) error` - Position limits
- `(rm *Manager) CheckOrderLimits(order *models.Order) error` - Order limits
- `(rm *Manager) CheckConcentrationRisk(order *models.Order) error` - Concentration risk
- `(rm *Manager) GetPositionLimits(book string) (*PositionLimits, error)` - Get limits
- `(rm *Manager) GetCurrentExposure(book string) (*Exposure, error)` - Get exposure
- `(rm *Manager) CheckRateLimit(ctx context.Context) error` - Rate limiting

**Tests** (`risk_manager_test.go`):
- `TestCheckRisk()` - Risk checks
- `TestCheckPositionLimits()` - Position limits
- `TestCheckOrderLimits()` - Order limits
- `TestConcentrationRisk()` - Concentration checks
- `TestRateLimit()` - Rate limiting

---

### 10. `internal/manager/order_manager.go`

**Interface**:
```go
type OrderManager interface {
    Start(ctx context.Context) error
    Stop() error
    CreateOrder(signal *models.TradeSignalEvent) (*models.Order, error)
    UpdateOrderStatus(orderID string, status models.OrderStatus, metadata map[string]interface{}) error
    CancelOrder(ctx context.Context, orderID string) error
    GetOrder(ctx context.Context, orderID string) (*models.Order, error)
    ListOrders(ctx context.Context, filters *models.OrderFilters) ([]*models.Order, error)
}
```

**Type Definitions**:
```go
type Manager struct {
    logger       *logger.Logger
    config       *config.Config
    validator    validator.OrderValidator
    riskManager  risk.RiskManager
    repository   repository.OrderRepository
    publisher    *publisher.EventPublisher
    executor     *executor.OrderExecutor
    stateMachine *StateMachine
    metrics      *metrics.MetricsCollector
    
    // Internal state
    stopChan     chan struct{}
    wg           sync.WaitGroup
}
```

**Functions**:
- `NewOrderManager(config *config.Config, logger *logger.Logger, validator validator.OrderValidator, riskManager risk.RiskManager, repository repository.OrderRepository, publisher *publisher.EventPublisher, executor *executor.OrderExecutor, metrics *metrics.MetricsCollector) *Manager`

**Methods**:
- `(m *Manager) Start(ctx context.Context) error` - Start manager
- `(m *Manager) Stop() error` - Stop manager
- `(m *Manager) CreateOrder(signal *models.TradeSignalEvent) (*models.Order, error)` - Create order from signal
- `(m *Manager) SubmitOrder(ctx context.Context, order *models.Order) error` - Submit to executor
- `(m *Manager) UpdateOrderStatus(orderID string, status models.OrderStatus, metadata map[string]interface{}) error` - Update status
- `(m *Manager) CancelOrder(ctx context.Context, orderID string) error` - Cancel order
- `(m *Manager) GetOrder(ctx context.Context, orderID string) (*models.Order, error)` - Get order
- `(m *Manager) ListOrders(ctx context.Context, filters *models.OrderFilters) ([]*models.Order, error)` - List orders
- `(m *Manager) handleOrderUpdate(event *models.OrderEvent)` - Handle updates
- `(m *Manager) processSignal(signal *models.TradeSignalEvent) error` - Process signal
- `(m *Manager) generateOrderID() string` - Generate unique ID

**Tests** (`manager_test.go`):
- `TestNewOrderManager()` - Creation
- `TestCreateOrder()` - Order creation
- `TestSubmitOrder()` - Order submission
- `TestUpdateOrderStatus()` - Status updates
- `TestCancelOrder()` - Cancellation
- `TestGetOrder()` - Retrieval
- `TestListOrders()` - Listing with filters

---

### 11. `internal/manager/state_machine.go`

**Type Definitions**:
```go
type StateMachine struct {
    logger             *logger.Logger
    allowedTransitions map[models.OrderStatus][]models.OrderStatus
}

type Transition struct {
    From models.OrderStatus
    To   models.OrderStatus
}
```

**Functions**:
- `NewStateMachine(logger *logger.Logger) *StateMachine`

**Methods**:
- `(sm *StateMachine) CanTransition(from, to models.OrderStatus) bool` - Check if transition is allowed
- `(sm *StateMachine) Transition(order *models.Order, to models.OrderStatus) error` - Execute transition
- `(sm *StateMachine) ValidateTransition(from, to models.OrderStatus) error` - Validate transition
- `(sm *StateMachine) GetAllowedTransitions(status models.OrderStatus) []models.OrderStatus` - Get allowed transitions
- `(sm *StateMachine) initializeTransitions()` - Initialize transition rules

**Tests** (`state_machine_test.go`):
- `TestCanTransition()` - Transition validation
- `TestTransition()` - State changes
- `TestInvalidTransition()` - Invalid transitions
- `TestGetAllowedTransitions()` - Get allowed states

---

### 12. `internal/repository/order_repository.go`

**Interface**:
```go
type OrderRepository interface {
    Create(ctx context.Context, order *models.Order) error
    Update(ctx context.Context, order *models.Order) error
    Get(ctx context.Context, orderID string) (*models.Order, error)
    List(ctx context.Context, filters *models.OrderFilters) ([]*models.Order, error)
    Delete(ctx context.Context, orderID string) error
    GetBySignalID(ctx context.Context, signalID string) (*models.Order, error)
    GetActiveOrders(ctx context.Context) ([]*models.Order, error)
    GetOrdersByBook(ctx context.Context, book string) ([]*models.Order, error)
    GetOrdersByStatus(ctx context.Context, status models.OrderStatus) ([]*models.Order, error)
}
```

**Type Definitions**:
```go
type RedisOrderRepository struct {
    client  *redis.Client
    logger  *logger.Logger
    metrics *metrics.MetricsCollector
}
```

**Functions**:
- `NewRedisOrderRepository(client *redis.Client, logger *logger.Logger, metrics *metrics.MetricsCollector) *RedisOrderRepository`

**Methods**:
- `(r *RedisOrderRepository) Create(ctx context.Context, order *models.Order) error` - Create order
- `(r *RedisOrderRepository) Update(ctx context.Context, order *models.Order) error` - Update order
- `(r *RedisOrderRepository) Get(ctx context.Context, orderID string) (*models.Order, error)` - Get order
- `(r *RedisOrderRepository) List(ctx context.Context, filters *models.OrderFilters) ([]*models.Order, error)` - List orders
- `(r *RedisOrderRepository) Delete(ctx context.Context, orderID string) error` - Delete order
- `(r *RedisOrderRepository) GetBySignalID(ctx context.Context, signalID string) (*models.Order, error)` - Get by signal
- `(r *RedisOrderRepository) GetActiveOrders(ctx context.Context) ([]*models.Order, error)` - Get active
- `(r *RedisOrderRepository) GetOrdersByBook(ctx context.Context, book string) ([]*models.Order, error)` - Get by book
- `(r *RedisOrderRepository) GetOrdersByStatus(ctx context.Context, status models.OrderStatus) ([]*models.Order, error)` - Get by status
- `(r *RedisOrderRepository) addToIndex(ctx context.Context, order *models.Order) error` - Add to indexes
- `(r *RedisOrderRepository) removeFromIndex(ctx context.Context, order *models.Order) error` - Remove from indexes

**Tests** (`repository_test.go`):
- `TestCreate()` - Order creation
- `TestUpdate()` - Order updates
- `TestGet()` - Order retrieval
- `TestList()` - Order listing
- `TestGetBySignalID()` - Signal lookup
- `TestGetActiveOrders()` - Active orders

---

### 13. `internal/repository/position_repository.go`

**Interface**:
```go
type PositionRepository interface {
    Create(ctx context.Context, position *models.Position) error
    Update(ctx context.Context, position *models.Position) error
    Get(ctx context.Context, book string) (*models.Position, error)
    GetAll(ctx context.Context) ([]*models.Position, error)
    Delete(ctx context.Context, book string) error
}
```

**Type Definitions**:
```go
type RedisPositionRepository struct {
    client  *redis.Client
    logger  *logger.Logger
    metrics *metrics.MetricsCollector
}
```

**Functions**:
- `NewRedisPositionRepository(client *redis.Client, logger *logger.Logger, metrics *metrics.MetricsCollector) *RedisPositionRepository`

**Methods**:
- `(r *RedisPositionRepository) Create(ctx context.Context, position *models.Position) error`
- `(r *RedisPositionRepository) Update(ctx context.Context, position *models.Position) error`
- `(r *RedisPositionRepository) Get(ctx context.Context, book string) (*models.Position, error)`
- `(r *RedisPositionRepository) GetAll(ctx context.Context) ([]*models.Position, error)`
- `(r *RedisPositionRepository) Delete(ctx context.Context, book string) error`

---

### 14. `internal/executor/order_executor.go`

**Interface**:
```go
type OrderExecutor interface {
    SubmitOrder(ctx context.Context, order *models.Order) error
    CancelOrder(ctx context.Context, orderID string) error
    GetOrderStatus(ctx context.Context, orderID string) (*models.Order, error)
}
```

**Type Definitions**:
```go
type HTTPExecutor struct {
    logger     *logger.Logger
    config     *config.TradingEngineConfig
    httpClient *http.Client
    metrics    *metrics.MetricsCollector
}
```

**Functions**:
- `NewHTTPExecutor(config *config.TradingEngineConfig, logger *logger.Logger, metrics *metrics.MetricsCollector) *HTTPExecutor`

**Methods**:
- `(e *HTTPExecutor) SubmitOrder(ctx context.Context, order *models.Order) error` - Submit to trading-engine
- `(e *HTTPExecutor) CancelOrder(ctx context.Context, orderID string) error` - Cancel order
- `(e *HTTPExecutor) GetOrderStatus(ctx context.Context, orderID string) (*models.Order, error)` - Get status
- `(e *HTTPExecutor) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error)` - HTTP helper
- `(e *HTTPExecutor) retryRequest(ctx context.Context, fn func() error) error` - Retry logic

**Tests** (`order_executor_test.go`):
- `TestSubmitOrder()` - Order submission
- `TestCancelOrder()` - Order cancellation
- `TestGetOrderStatus()` - Status retrieval
- `TestRetryLogic()` - Retry behavior

---

### 15. `internal/publisher/event_publisher.go`

**Interface**:
```go
type EventPublisher interface {
    Start(ctx context.Context) error
    Stop() error
    PublishOrderCreated(order *models.Order) error
    PublishOrderUpdated(order *models.Order) error
    PublishOrderFilled(order *models.Order) error
    PublishOrderCancelled(order *models.Order) error
    PublishOrderRejected(order *models.Order) error
}
```

**Type Definitions**:
```go
type Publisher struct {
    logger        *logger.Logger
    metrics       *metrics.MetricsCollector
    producer      *kafka.Producer
    eventsTopic   string
    ordersTopic   string
    eventChan     chan *models.OrderEvent
    stopChan      chan struct{}
    wg            sync.WaitGroup
}
```

**Functions**:
- `NewEventPublisher(config *PublisherConfig, logger *logger.Logger, metrics *metrics.MetricsCollector) (*Publisher, error)`

**Methods**:
- `(p *Publisher) Start(ctx context.Context) error` - Start publisher
- `(p *Publisher) Stop() error` - Stop publisher
- `(p *Publisher) PublishOrderCreated(order *models.Order) error` - Publish created event
- `(p *Publisher) PublishOrderUpdated(order *models.Order) error` - Publish updated event
- `(p *Publisher) PublishOrderFilled(order *models.Order) error` - Publish filled event
- `(p *Publisher) PublishOrderCancelled(order *models.Order) error` - Publish cancelled event
- `(p *Publisher) PublishOrderRejected(order *models.Order) error` - Publish rejected event
- `(p *Publisher) publishEvent(event *models.OrderEvent) error` - Generic publish
- `(p *Publisher) processEvents(ctx context.Context)` - Event processing loop

**Tests** (`event_publisher_test.go`):
- `TestPublishOrderCreated()` - Created events
- `TestPublishOrderUpdated()` - Updated events
- `TestPublishOrderFilled()` - Filled events
- `TestPublishOrderCancelled()` - Cancelled events
- `TestPublishOrderRejected()` - Rejected events

---

### 16. `internal/api/handlers.go`

**Type Definitions**:
```go
type Handler struct {
    logger  *logger.Logger
    manager manager.OrderManager
    metrics *metrics.MetricsCollector
}
```

**Functions**:
- `NewHandler(logger *logger.Logger, manager manager.OrderManager, metrics *metrics.MetricsCollector) *Handler`

**Methods**:
- `(h *Handler) GetOrder(w http.ResponseWriter, r *http.Request)` - GET /api/v1/orders/{id}
- `(h *Handler) ListOrders(w http.ResponseWriter, r *http.Request)` - GET /api/v1/orders
- `(h *Handler) CancelOrder(w http.ResponseWriter, r *http.Request)` - POST /api/v1/orders/{id}/cancel
- `(h *Handler) GetActiveOrders(w http.ResponseWriter, r *http.Request)` - GET /api/v1/orders/active
- `(h *Handler) GetOrderHistory(w http.ResponseWriter, r *http.Request)` - GET /api/v1/orders/history
- `(h *Handler) GetPositions(w http.ResponseWriter, r *http.Request)` - GET /api/v1/positions
- `(h *Handler) GetPosition(w http.ResponseWriter, r *http.Request)` - GET /api/v1/positions/{book}
- `(h *Handler) GetPositionSummary(w http.ResponseWriter, r *http.Request)` - GET /api/v1/positions/summary
- `(h *Handler) GetStatus(w http.ResponseWriter, r *http.Request)` - GET /api/v1/status
- `(h *Handler) parseFilters(r *http.Request) (*models.OrderFilters, error)` - Parse query params
- `(h *Handler) respondJSON(w http.ResponseWriter, status int, data interface{})` - JSON response
- `(h *Handler) respondError(w http.ResponseWriter, status int, message string)` - Error response

**Tests** (`handlers_test.go`):
- `TestGetOrder()` - Get order endpoint
- `TestListOrders()` - List orders endpoint
- `TestCancelOrder()` - Cancel order endpoint
- `TestGetActiveOrders()` - Active orders endpoint
- `TestGetPositions()` - Positions endpoint
- `TestErrorHandling()` - Error responses

---

### 17. `internal/server/http_server.go`

**Type Definitions**:
```go
type HTTPServer struct {
    logger        *logger.Logger
    config        *config.ServiceConfig
    handler       *api.Handler
    healthManager *health.HealthManager
    metrics       *metrics.MetricsCollector
    server        *http.Server
    router        *http.ServeMux
}
```

**Functions**:
- `NewHTTPServer(config *config.ServiceConfig, handler *api.Handler, healthManager *health.HealthManager, metrics *metrics.MetricsCollector, logger *logger.Logger) *HTTPServer`

**Methods**:
- `(s *HTTPServer) Start(ctx context.Context) error` - Start server
- `(s *HTTPServer) Stop(ctx context.Context) error` - Graceful shutdown
- `(s *HTTPServer) setupRoutes()` - Configure routes
- `(s *HTTPServer) setupMiddleware()` - Setup middleware

**Routes**:
- `GET /health` - Health check
- `GET /health/live` - Liveness probe
- `GET /health/ready` - Readiness probe
- `GET /metrics` - Prometheus metrics
- `GET /api/v1/status` - Service status
- `GET /api/v1/orders` - List orders
- `GET /api/v1/orders/{id}` - Get order
- `POST /api/v1/orders/{id}/cancel` - Cancel order
- `GET /api/v1/orders/active` - Active orders
- `GET /api/v1/orders/history` - Order history
- `GET /api/v1/positions` - List positions
- `GET /api/v1/positions/{book}` - Get position
- `GET /api/v1/positions/summary` - Position summary

---

### 18. `internal/metrics/prometheus.go`

**Type Definitions**:
```go
type MetricsCollector struct {
    // Order metrics
    ordersCreated    *prometheus.CounterVec
    ordersFilled     *prometheus.CounterVec
    ordersCancelled  *prometheus.CounterVec
    ordersRejected   *prometheus.CounterVec
    ordersActive     *prometheus.GaugeVec
    orderDuration    *prometheus.HistogramVec
    
    // Validation metrics
    validations      *prometheus.CounterVec
    validationDuration *prometheus.HistogramVec
    
    // Risk metrics
    riskChecks       *prometheus.CounterVec
    riskViolations   *prometheus.CounterVec
    
    // Repository metrics
    repoOperations   *prometheus.CounterVec
    repoDuration     *prometheus.HistogramVec
    
    // Publisher metrics
    eventsPublished  *prometheus.CounterVec
    eventsFailed     *prometheus.CounterVec
    
    // System metrics
    serviceUptime    prometheus.Gauge
    serviceHealth    *prometheus.GaugeVec
}
```

**Functions**:
- `NewMetricsCollector(serviceName string) *MetricsCollector`

**Methods**:
- `(mc *MetricsCollector) RecordOrderCreated(book, strategy string)`
- `(mc *MetricsCollector) RecordOrderFilled(book, strategy string)`
- `(mc *MetricsCollector) RecordOrderCancelled(book, strategy, reason string)`
- `(mc *MetricsCollector) RecordOrderRejected(book, strategy, reason string)`
- `(mc *MetricsCollector) RecordOrderDuration(stage string, duration time.Duration)`
- `(mc *MetricsCollector) RecordValidation(validationType, result string)`
- `(mc *MetricsCollector) RecordRiskCheck(checkType, result string)`
- `(mc *MetricsCollector) RecordRiskViolation(violationType string)`
- `(mc *MetricsCollector) RecordRepositoryOperation(operation, result string, duration time.Duration)`
- `(mc *MetricsCollector) RecordEventPublished(eventType string)`
- `(mc *MetricsCollector) RecordEventFailed(eventType, reason string)`
- `(mc *MetricsCollector) RecordServiceHealth(healthy bool)`
- `(mc *MetricsCollector) RecordServiceUptime(uptime time.Duration)`

---

### 19. `internal/logger/logger.go`

**Type Definitions**:
```go
type Logger struct {
    logger *zerolog.Logger
}

type Config struct {
    Level  string
    Format string
    Output string
}
```

**Functions**:
- `New(config *Config) *Logger`
- `DefaultLogger() *Logger`

**Methods**:
- `(l *Logger) Debug(msg string, fields map[string]interface{})`
- `(l *Logger) Info(msg string, fields map[string]interface{})`
- `(l *Logger) Warn(msg string, fields map[string]interface{})`
- `(l *Logger) Error(msg string, fields map[string]interface{})`
- `(l *Logger) Fatal(msg string, fields map[string]interface{})`
- `(l *Logger) Debugf(format string, args ...interface{})`
- `(l *Logger) Infof(format string, args ...interface{})`
- `(l *Logger) Warnf(format string, args ...interface{})`
- `(l *Logger) Errorf(format string, args ...interface{})`
- `(l *Logger) Fatalf(format string, args ...interface{})`

---

## 🧪 Testing Summary

### Unit Tests (23 test files)

1. `config_test.go` - Configuration loading/validation
2. `signal_consumer_test.go` - Signal consumption
3. `order_validator_test.go` - Order validation
4. `risk_manager_test.go` - Risk management
5. `order_manager_test.go` - Order lifecycle
6. `state_machine_test.go` - State transitions
7. `order_repository_test.go` - Order persistence
8. `position_repository_test.go` - Position persistence
9. `order_executor_test.go` - Order execution
10. `event_publisher_test.go` - Event publishing
11. `handlers_test.go` - HTTP handlers
12. `http_server_test.go` - HTTP server
13. `metrics_test.go` - Metrics collection

### Integration Tests

Create `integration_test.go` in project root with:
- End-to-end order flow
- Kafka integration
- Redis persistence
- HTTP API operations

### Performance Tests

Create `benchmark_test.go` with:
- Order creation benchmarks
- Validation benchmarks
- Repository operation benchmarks
- Event publishing benchmarks

---

## 📚 Documentation Files

1. **README.md** - Service overview, features, quick start
2. **TESTING.md** - Testing guide and procedures
3. **API.md** - API documentation with examples
4. **DEPLOYMENT.md** - Deployment instructions
5. **ARCHITECTURE.md** - Architecture and design decisions

---

## ⚙️ Configuration Files

1. **go.mod** - Go module dependencies (UPDATE)
2. **go.sum** - Dependency checksums (GENERATE)
3. **Dockerfile** - Container image (EXISTS)
4. **.env.example** - Example environment variables (NEW)
5. **run_tests.sh** - Test runner script (NEW)
6. **.gitignore** - Git ignore rules (NEW)

---

## 🔌 External Dependencies (shared package)

### Already Available in `shared/pkg/`:
- ✅ `bitso` - Bitso API client
- ✅ `kafka` - Kafka producer/consumer
- ✅ `health` - Health check framework
- ✅ `models` - Common models (TradeEvent, TradeSignalEvent, OrderEvent)
- ✅ `database` - Redis client
- ✅ `config` - Base config utilities
- ✅ `utils` - Rate limiter, utilities

### Need to Use:
- `github.com/google/uuid` - UUID generation
- `github.com/prometheus/client_golang` - Metrics
- `github.com/redis/go-redis/v9` - Redis
- `github.com/segmentio/kafka-go` - Kafka
- `github.com/rs/zerolog` - Logging

---

## 📊 Implementation Progress Tracking

### Phase 1: Core Infrastructure ⬜
- [ ] `cmd/main.go` - Application setup
- [ ] `internal/config/config.go` - Configuration
- [ ] `internal/logger/logger.go` - Logging
- [ ] `internal/metrics/prometheus.go` - Metrics
- [ ] `internal/server/http_server.go` - HTTP server

### Phase 2: Data Layer ⬜
- [ ] `internal/models/order.go` - Order model
- [ ] `internal/models/position.go` - Position model
- [ ] `internal/models/validation.go` - Validation types
- [ ] `internal/models/filters.go` - Filter types
- [ ] `internal/repository/order_repository.go` - Order repo
- [ ] `internal/repository/position_repository.go` - Position repo

### Phase 3: Business Logic ⬜
- [ ] `internal/validator/order_validator.go` - Validation
- [ ] `internal/risk/risk_manager.go` - Risk management
- [ ] `internal/manager/state_machine.go` - State machine
- [ ] `internal/manager/order_manager.go` - Order management

### Phase 4: Integration ⬜
- [ ] `internal/consumer/signal_consumer.go` - Kafka consumer
- [ ] `internal/publisher/event_publisher.go` - Kafka producer
- [ ] `internal/executor/order_executor.go` - Execution client

### Phase 5: API ⬜
- [ ] `internal/api/handlers.go` - HTTP handlers
- [ ] `internal/api/responses.go` - Response types

### Phase 6: Testing ⬜
- [ ] All unit tests
- [ ] Integration tests
- [ ] Performance tests

### Phase 7: Documentation ⬜
- [ ] README.md
- [ ] API.md
- [ ] TESTING.md
- [ ] DEPLOYMENT.md

---

## 🎯 Key Metrics

- **Total Files**: 40+ files
- **Total Lines of Code**: ~5,000-7,000 lines
- **Test Coverage Target**: 80%+
- **Estimated Time**: 12-14 working days
- **Complexity**: Medium-High
- **Priority**: High

---

## 🔗 Service Connections

### Consumes From:
- **strategy-executor**: `strategy-executor.signals` topic

### Produces To:
- **trading-engine**: `order-management.orders` topic
- **monitoring**: `order-management.events` topic

### Queries:
- **Redis**: Order/position persistence
- **trading-engine** (HTTP): Order status queries

### Exposes:
- **HTTP API**: Order management operations
- **Prometheus**: Metrics endpoint
- **Health**: Health check endpoints

---

This checklist provides a comprehensive roadmap for implementing the Order Management Service. Each component should be implemented, tested, and documented following the patterns established in the existing microservices.

