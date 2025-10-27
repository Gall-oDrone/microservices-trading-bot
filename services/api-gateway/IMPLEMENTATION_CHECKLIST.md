# API Gateway - Complete Implementation Checklist

## Overview
This document provides a detailed checklist of every file, method, test, and component that needs to be implemented for the API Gateway service.

---

## Phase 1: Core Infrastructure

### 1.1 Configuration (`internal/config/`)

#### File: `internal/config/config.go`
- [ ] **Type: Config struct**
  - [ ] Service fields (Name, Version, Host, Port, Environment)
  - [ ] Backend service URLs (MarketDataURL, OrderManagementURL, StrategyExecutorURL)
  - [ ] HTTP client config (Timeout, MaxRetries, RetryDelay, MaxIdleConns, IdleConnTimeout)
  - [ ] Rate limiting config (Enabled, RequestsPerMinute, Burst)
  - [ ] Circuit breaker config (Enabled, Threshold, Timeout, MaxRequests)
  - [ ] Auth config (Enabled, JWTSecret, APIKeyHeader)
  - [ ] Logging config (Level, Format, Output)
  - [ ] Metrics config (Enabled, Port, Path)
  - [ ] TLS config (Enabled, CertFile, KeyFile)

- [ ] **Function: Load() (*Config, error)**
  - [ ] Load from environment variables
  - [ ] Load from .env file (optional)
  - [ ] Set default values
  - [ ] Call Validate()

- [ ] **Method: (c *Config) Validate() error**
  - [ ] Validate service name and port
  - [ ] Validate backend service URLs
  - [ ] Validate timeout values
  - [ ] Validate rate limit settings
  - [ ] Validate circuit breaker settings

- [ ] **Helper Functions**
  - [ ] getEnv(key, defaultValue string) string
  - [ ] getEnvAsInt(key string, defaultValue int) int
  - [ ] getEnvAsBool(key string, defaultValue bool) bool
  - [ ] getEnvAsDuration(key string, defaultValue time.Duration) time.Duration

#### File: `internal/config/config_test.go`
- [ ] **Test: TestLoad**
  - [ ] Test successful config loading
  - [ ] Test with all environment variables set
  - [ ] Test with default values

- [ ] **Test: TestValidate**
  - [ ] Test validation success
  - [ ] Test validation failures (missing required fields)
  - [ ] Test validation failures (invalid values)

- [ ] **Test: TestHelperFunctions**
  - [ ] Test getEnv
  - [ ] Test getEnvAsInt
  - [ ] Test getEnvAsBool
  - [ ] Test getEnvAsDuration

**Estimated Lines**: ~250 lines (config.go: ~180, config_test.go: ~70)

---

### 1.2 Logger (`internal/logger/`)

#### File: `internal/logger/logger.go`
- [ ] **Type: Logger struct**
  - [ ] zerologLogger *zerolog.Logger
  - [ ] level zerolog.Level
  - [ ] format string (json/console)

- [ ] **Type: Config struct**
  - [ ] Level string
  - [ ] Format string
  - [ ] Output string

- [ ] **Function: New(cfg *Config) *Logger**
  - [ ] Initialize zerolog logger
  - [ ] Configure output format
  - [ ] Set log level
  - [ ] Add caller information

- [ ] **Method: (l *Logger) Debug(msg string, fields map[string]interface{})**
- [ ] **Method: (l *Logger) Info(msg string, fields map[string]interface{})**
- [ ] **Method: (l *Logger) Warn(msg string, fields map[string]interface{})**
- [ ] **Method: (l *Logger) Error(msg string, fields map[string]interface{})**
- [ ] **Method: (l *Logger) Fatal(msg string, fields map[string]interface{})**

- [ ] **Method: (l *Logger) WithFields(fields map[string]interface{}) *Logger**
  - [ ] Create logger with additional fields

- [ ] **Method: (l *Logger) WithRequestID(requestID string) *Logger**
  - [ ] Add request ID to logger context

**Estimated Lines**: ~120 lines

---

### 1.3 Metrics (`internal/metrics/`)

#### File: `internal/metrics/prometheus.go`
- [ ] **Type: MetricsCollector struct**
  - [ ] HTTP request metrics (counter, histogram, gauge)
  - [ ] Backend client metrics (counter, histogram)
  - [ ] Circuit breaker metrics (counter, gauge)
  - [ ] Rate limiter metrics (counter)
  - [ ] System metrics (gauge)

- [ ] **Function: NewMetricsCollector(serviceName string) *MetricsCollector**
  - [ ] Initialize all Prometheus metrics
  - [ ] Register metrics with Prometheus

- [ ] **HTTP Metrics Methods**
  - [ ] RecordHTTPRequest(method, path string, statusCode int, duration time.Duration)
  - [ ] RecordHTTPInFlight(path string, delta int)
  - [ ] RecordHTTPRequestSize(path string, size int64)
  - [ ] RecordHTTPResponseSize(path string, size int64)

- [ ] **Backend Client Metrics Methods**
  - [ ] RecordBackendCall(service, endpoint string, statusCode int, duration time.Duration)
  - [ ] RecordBackendError(service, endpoint, errorType string)

- [ ] **Circuit Breaker Metrics Methods**
  - [ ] RecordCircuitBreakerState(service, state string)
  - [ ] RecordCircuitBreakerOperation(service, operation string)

- [ ] **Rate Limiter Metrics Methods**
  - [ ] RecordRateLimitHit(path, clientIP string)
  - [ ] RecordRateLimitAllow(path string)

- [ ] **System Metrics Methods**
  - [ ] RecordServiceUptime(uptime time.Duration)
  - [ ] RecordServiceHealth(healthy bool)
  - [ ] StartSystemMetricsCollection(ctx context.Context)

- [ ] **Function: Handler() http.Handler**
  - [ ] Return Prometheus HTTP handler

**Estimated Lines**: ~300 lines

---

### 1.4 Server (`internal/server/`)

#### File: `internal/server/http_server.go`
- [ ] **Type: HTTPServer struct**
  - [ ] config *config.Config
  - [ ] server *http.Server
  - [ ] logger *logger.Logger
  - [ ] router http.Handler

- [ ] **Function: NewHTTPServer(cfg *config.Config, handler http.Handler, logger *logger.Logger) *HTTPServer**
  - [ ] Create HTTP server with configuration
  - [ ] Set timeouts (read, write, idle)
  - [ ] Configure TLS if enabled

- [ ] **Method: (s *HTTPServer) Start(ctx context.Context) error**
  - [ ] Start HTTP server
  - [ ] Start HTTPS server if TLS enabled
  - [ ] Log startup message
  - [ ] Wait for context cancellation

- [ ] **Method: (s *HTTPServer) Stop(ctx context.Context) error**
  - [ ] Graceful shutdown
  - [ ] Wait for in-flight requests
  - [ ] Log shutdown message

**Estimated Lines**: ~150 lines

---

## Phase 2: Client Layer

### 2.1 Client Types (`internal/client/`)

#### File: `internal/client/types.go`
- [ ] **Type: ClientConfig struct**
  - [ ] BaseURL string
  - [ ] Timeout time.Duration
  - [ ] MaxRetries int
  - [ ] RetryDelay time.Duration
  - [ ] MaxIdleConns int
  - [ ] IdleConnTimeout time.Duration

- [ ] **Type: APIError struct**
  - [ ] StatusCode int
  - [ ] Code string
  - [ ] Message string
  - [ ] Details map[string]interface{}
  - [ ] Err error

- [ ] **Method: (e *APIError) Error() string**
- [ ] **Method: (e *APIError) Unwrap() error**

- [ ] **Type: Response struct**
  - [ ] Success bool
  - [ ] Data json.RawMessage
  - [ ] Meta Metadata
  - [ ] Error *ErrorDetail

- [ ] **Type: Metadata struct**
  - [ ] Timestamp time.Time
  - [ ] RequestID string
  - [ ] Version string

- [ ] **Type: ErrorDetail struct**
  - [ ] Code string
  - [ ] Message string
  - [ ] Details map[string]interface{}

**Estimated Lines**: ~150 lines

---

### 2.2 Market Data Client (`internal/client/`)

#### File: `internal/client/market_data_client.go`
- [ ] **Interface: MarketDataClient**
  - [ ] GetRecentTrades(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error)
  - [ ] GetTrade(ctx context.Context, book string, tradeID uint64) (*models.TradeEvent, error)
  - [ ] GetOrderBook(ctx context.Context, book string) (*bitso.OrderBook, error)
  - [ ] GetTicker(ctx context.Context, book string) (*bitso.Ticker, error)
  - [ ] GetTradeStats(ctx context.Context, book string) (*TradeStats, error)
  - [ ] GetMarketSummary(ctx context.Context) (*MarketSummary, error)
  - [ ] Health(ctx context.Context) error

- [ ] **Type: marketDataClient struct**
  - [ ] baseURL string
  - [ ] httpClient *http.Client
  - [ ] logger *logger.Logger
  - [ ] metrics *metrics.MetricsCollector
  - [ ] config *ClientConfig

- [ ] **Function: NewMarketDataClient(cfg *ClientConfig, logger *logger.Logger, metrics *metrics.MetricsCollector) (MarketDataClient, error)**

- [ ] **Implementation of all interface methods**
  - [ ] GetRecentTrades
  - [ ] GetTrade
  - [ ] GetOrderBook
  - [ ] GetTicker
  - [ ] GetTradeStats
  - [ ] GetMarketSummary
  - [ ] Health

- [ ] **Helper Methods**
  - [ ] doRequest(ctx context.Context, method, path string, body interface{}) (*Response, error)
  - [ ] retryRequest(ctx context.Context, fn func() error) error
  - [ ] parseResponse(resp *http.Response, target interface{}) error

- [ ] **Type: TradeStats struct** (matching market-data service)
- [ ] **Type: MarketSummary struct**

**Estimated Lines**: ~450 lines

---

### 2.3 Order Management Client (`internal/client/`)

#### File: `internal/client/order_management_client.go`
- [ ] **Interface: OrderManagementClient**
  - [ ] ListOrders(ctx context.Context, filters *OrderFilters) (*OrderList, error)
  - [ ] GetOrder(ctx context.Context, orderID string) (*models.Order, error)
  - [ ] CancelOrder(ctx context.Context, orderID string) error
  - [ ] GetActiveOrders(ctx context.Context) ([]*models.Order, error)
  - [ ] GetOrderHistory(ctx context.Context, filters *OrderFilters) ([]*models.Order, error)
  - [ ] ListPositions(ctx context.Context, filters *PositionFilters) ([]*Position, error)
  - [ ] GetPosition(ctx context.Context, book string) (*Position, error)
  - [ ] GetPositionSummary(ctx context.Context) (*PositionSummary, error)
  - [ ] Health(ctx context.Context) error

- [ ] **Type: orderManagementClient struct**
  - [ ] baseURL string
  - [ ] httpClient *http.Client
  - [ ] logger *logger.Logger
  - [ ] metrics *metrics.MetricsCollector
  - [ ] config *ClientConfig

- [ ] **Function: NewOrderManagementClient(cfg *ClientConfig, logger *logger.Logger, metrics *metrics.MetricsCollector) (OrderManagementClient, error)**

- [ ] **Implementation of all interface methods**
  - [ ] ListOrders
  - [ ] GetOrder
  - [ ] CancelOrder
  - [ ] GetActiveOrders
  - [ ] GetOrderHistory
  - [ ] ListPositions
  - [ ] GetPosition
  - [ ] GetPositionSummary
  - [ ] Health

- [ ] **Helper Methods**
  - [ ] doRequest(ctx context.Context, method, path string, body interface{}) (*Response, error)
  - [ ] buildQueryParams(filters interface{}) string

- [ ] **Type: OrderFilters struct**
- [ ] **Type: OrderList struct**
- [ ] **Type: PositionFilters struct**
- [ ] **Type: Position struct**
- [ ] **Type: PositionSummary struct**

**Estimated Lines**: ~500 lines

---

### 2.4 Strategy Executor Client (`internal/client/`)

#### File: `internal/client/strategy_executor_client.go`
- [ ] **Interface: StrategyExecutorClient**
  - [ ] GetStatus(ctx context.Context) (*ServiceStatus, error)
  - [ ] ListStrategies(ctx context.Context) ([]*Strategy, error)
  - [ ] GetStrategy(ctx context.Context, name string) (*Strategy, error)
  - [ ] StartStrategy(ctx context.Context, name string) error
  - [ ] StopStrategy(ctx context.Context, name string) error
  - [ ] UpdateStrategyConfig(ctx context.Context, name string, config map[string]interface{}) error
  - [ ] Health(ctx context.Context) error

- [ ] **Type: strategyExecutorClient struct**
  - [ ] baseURL string
  - [ ] httpClient *http.Client
  - [ ] logger *logger.Logger
  - [ ] metrics *metrics.MetricsCollector
  - [ ] config *ClientConfig

- [ ] **Function: NewStrategyExecutorClient(cfg *ClientConfig, logger *logger.Logger, metrics *metrics.MetricsCollector) (StrategyExecutorClient, error)**

- [ ] **Implementation of all interface methods**
  - [ ] GetStatus
  - [ ] ListStrategies
  - [ ] GetStrategy
  - [ ] StartStrategy
  - [ ] StopStrategy
  - [ ] UpdateStrategyConfig
  - [ ] Health

- [ ] **Helper Methods**
  - [ ] doRequest(ctx context.Context, method, path string, body interface{}) (*Response, error)

- [ ] **Type: ServiceStatus struct**
- [ ] **Type: Strategy struct**

**Estimated Lines**: ~350 lines

---

### 2.5 Client Factory (`internal/client/`)

#### File: `internal/client/client_factory.go`
- [ ] **Type: ClientFactory struct**
  - [ ] config *config.Config
  - [ ] logger *logger.Logger
  - [ ] metrics *metrics.MetricsCollector
  - [ ] httpClient *http.Client

- [ ] **Function: NewClientFactory(cfg *config.Config, logger *logger.Logger, metrics *metrics.MetricsCollector) *ClientFactory**
  - [ ] Initialize shared HTTP client
  - [ ] Configure connection pooling
  - [ ] Set timeouts

- [ ] **Method: (f *ClientFactory) MarketDataClient() (MarketDataClient, error)**
- [ ] **Method: (f *ClientFactory) OrderManagementClient() (OrderManagementClient, error)**
- [ ] **Method: (f *ClientFactory) StrategyExecutorClient() (StrategyExecutorClient, error)**

- [ ] **Method: (f *ClientFactory) createHTTPClient() *http.Client**
  - [ ] Configure transport
  - [ ] Set connection pooling
  - [ ] Set timeouts

**Estimated Lines**: ~150 lines

---

## Phase 3: Middleware Layer

### 3.1 Logging Middleware (`internal/middleware/`)

#### File: `internal/middleware/logging.go`
- [ ] **Type: loggingMiddleware struct**
  - [ ] logger *logger.Logger

- [ ] **Function: NewLoggingMiddleware(logger *logger.Logger) func(http.Handler) http.Handler**

- [ ] **Method: (m *loggingMiddleware) Handler(next http.Handler) http.Handler**
  - [ ] Log request start (method, path, remote addr)
  - [ ] Generate request ID
  - [ ] Inject request ID into context
  - [ ] Wrap response writer to capture status
  - [ ] Log request completion (status, duration, size)
  - [ ] Log errors if any

- [ ] **Type: responseWriter struct** (captures status and size)
  - [ ] http.ResponseWriter
  - [ ] status int
  - [ ] size int
  - [ ] wroteHeader bool

**Estimated Lines**: ~120 lines

---

### 3.2 Metrics Middleware (`internal/middleware/`)

#### File: `internal/middleware/metrics.go`
- [ ] **Type: metricsMiddleware struct**
  - [ ] metrics *metrics.MetricsCollector

- [ ] **Function: NewMetricsMiddleware(metrics *metrics.MetricsCollector) func(http.Handler) http.Handler**

- [ ] **Method: (m *metricsMiddleware) Handler(next http.Handler) http.Handler**
  - [ ] Record request start
  - [ ] Increment in-flight requests
  - [ ] Wrap response writer
  - [ ] Record request completion (status, duration)
  - [ ] Record request/response sizes
  - [ ] Decrement in-flight requests

**Estimated Lines**: ~100 lines

---

### 3.3 Rate Limiter Middleware (`internal/middleware/`)

#### File: `internal/middleware/rate_limiter.go`
- [ ] **Type: rateLimiterMiddleware struct**
  - [ ] limiters map[string]*rate.Limiter (per IP)
  - [ ] mu sync.RWMutex
  - [ ] requestsPerMinute int
  - [ ] burst int
  - [ ] metrics *metrics.MetricsCollector

- [ ] **Function: NewRateLimiterMiddleware(requestsPerMinute, burst int, metrics *metrics.MetricsCollector) func(http.Handler) http.Handler**

- [ ] **Method: (m *rateLimiterMiddleware) Handler(next http.Handler) http.Handler**
  - [ ] Extract client IP
  - [ ] Get or create limiter for IP
  - [ ] Check rate limit
  - [ ] Return 429 if rate limited
  - [ ] Record metrics
  - [ ] Call next handler if allowed

- [ ] **Method: (m *rateLimiterMiddleware) getLimiter(ip string) *rate.Limiter**
  - [ ] Get existing limiter or create new
  - [ ] Clean up old limiters periodically

- [ ] **Method: (m *rateLimiterMiddleware) cleanupLimiters()**
  - [ ] Remove inactive limiters

**Estimated Lines**: ~150 lines

---

### 3.4 Circuit Breaker Middleware (`internal/middleware/`)

#### File: `internal/middleware/circuit_breaker.go`
- [ ] **Type: circuitBreakerMiddleware struct**
  - [ ] breakers map[string]*circuitBreaker (per service)
  - [ ] mu sync.RWMutex
  - [ ] config *CircuitBreakerConfig
  - [ ] metrics *metrics.MetricsCollector

- [ ] **Type: CircuitBreakerConfig struct**
  - [ ] Threshold int
  - [ ] Timeout time.Duration
  - [ ] MaxRequests uint32

- [ ] **Type: circuitBreaker struct**
  - [ ] state string (closed, open, half-open)
  - [ ] failures int
  - [ ] lastFailTime time.Time
  - [ ] mu sync.RWMutex

- [ ] **Function: NewCircuitBreakerMiddleware(cfg *CircuitBreakerConfig, metrics *metrics.MetricsCollector) func(http.Handler) http.Handler**

- [ ] **Method: (m *circuitBreakerMiddleware) Handler(next http.Handler) http.Handler**
  - [ ] Extract service name from path
  - [ ] Get or create circuit breaker
  - [ ] Check circuit state
  - [ ] Return 503 if circuit open
  - [ ] Call next handler
  - [ ] Record success/failure
  - [ ] Update circuit state

- [ ] **Method: (cb *circuitBreaker) canExecute() bool**
- [ ] **Method: (cb *circuitBreaker) recordSuccess()**
- [ ] **Method: (cb *circuitBreaker) recordFailure()**
- [ ] **Method: (cb *circuitBreaker) setState(state string)**

**Estimated Lines**: ~200 lines

---

### 3.5 CORS Middleware (`internal/middleware/`)

#### File: `internal/middleware/cors.go`
- [ ] **Type: corsMiddleware struct**
  - [ ] allowedOrigins []string
  - [ ] allowedMethods []string
  - [ ] allowedHeaders []string
  - [ ] exposeHeaders []string
  - [ ] allowCredentials bool
  - [ ] maxAge int

- [ ] **Function: NewCORSMiddleware(allowedOrigins []string) func(http.Handler) http.Handler**

- [ ] **Method: (m *corsMiddleware) Handler(next http.Handler) http.Handler**
  - [ ] Set CORS headers
  - [ ] Handle preflight requests (OPTIONS)
  - [ ] Validate origin
  - [ ] Call next handler

**Estimated Lines**: ~100 lines

---

### 3.6 Recovery Middleware (`internal/middleware/`)

#### File: `internal/middleware/recovery.go`
- [ ] **Type: recoveryMiddleware struct**
  - [ ] logger *logger.Logger

- [ ] **Function: NewRecoveryMiddleware(logger *logger.Logger) func(http.Handler) http.Handler**

- [ ] **Method: (m *recoveryMiddleware) Handler(next http.Handler) http.Handler**
  - [ ] Defer panic recovery
  - [ ] Log panic with stack trace
  - [ ] Return 500 Internal Server Error
  - [ ] Record metrics

**Estimated Lines**: ~80 lines

---

### 3.7 Timeout Middleware (`internal/middleware/`)

#### File: `internal/middleware/timeout.go`
- [ ] **Type: timeoutMiddleware struct**
  - [ ] timeout time.Duration
  - [ ] logger *logger.Logger

- [ ] **Function: NewTimeoutMiddleware(timeout time.Duration, logger *logger.Logger) func(http.Handler) http.Handler**

- [ ] **Method: (m *timeoutMiddleware) Handler(next http.Handler) http.Handler**
  - [ ] Create context with timeout
  - [ ] Call handler with timeout context
  - [ ] Return 504 Gateway Timeout if timeout exceeded
  - [ ] Record metrics

**Estimated Lines**: ~70 lines

---

### 3.8 Auth Middleware (`internal/middleware/`)

#### File: `internal/middleware/auth.go`
- [ ] **Type: authMiddleware struct**
  - [ ] enabled bool
  - [ ] jwtSecret string
  - [ ] apiKeyHeader string
  - [ ] logger *logger.Logger

- [ ] **Function: NewAuthMiddleware(cfg *config.Config, logger *logger.Logger) func(http.Handler) http.Handler**

- [ ] **Method: (m *authMiddleware) Handler(next http.Handler) http.Handler**
  - [ ] Skip if auth disabled
  - [ ] Extract API key or JWT token
  - [ ] Validate token/key (placeholder)
  - [ ] Inject user context
  - [ ] Return 401 if unauthorized
  - [ ] Call next handler if authenticated

- [ ] **Method: (m *authMiddleware) validateAPIKey(key string) (bool, error)** (placeholder)
- [ ] **Method: (m *authMiddleware) validateJWT(token string) (map[string]interface{}, error)** (placeholder)

**Estimated Lines**: ~120 lines

---

## Phase 4: Handler Layer

### 4.1 Response Helpers (`internal/api/`)

#### File: `internal/api/response.go`
- [ ] **Type: APIResponse struct**
  - [ ] Success bool
  - [ ] Data interface{}
  - [ ] Meta ResponseMetadata
  - [ ] Error *ErrorResponse

- [ ] **Type: ResponseMetadata struct**
  - [ ] Timestamp time.Time
  - [ ] RequestID string
  - [ ] Version string

- [ ] **Type: ErrorResponse struct**
  - [ ] Code string
  - [ ] Message string
  - [ ] Details map[string]interface{}

- [ ] **Function: SuccessResponse(w http.ResponseWriter, requestID string, data interface{})**
  - [ ] Create success response
  - [ ] Set headers
  - [ ] Write JSON

- [ ] **Function: ErrorResponse(w http.ResponseWriter, requestID string, statusCode int, code, message string)**
  - [ ] Create error response
  - [ ] Set headers
  - [ ] Write JSON

- [ ] **Function: InternalErrorResponse(w http.ResponseWriter, requestID string, err error)**
- [ ] **Function: BadRequestResponse(w http.ResponseWriter, requestID string, message string)**
- [ ] **Function: NotFoundResponse(w http.ResponseWriter, requestID string, message string)**
- [ ] **Function: ServiceUnavailableResponse(w http.ResponseWriter, requestID string, service string)**
- [ ] **Function: RateLimitExceededResponse(w http.ResponseWriter, requestID string)**
- [ ] **Function: UnauthorizedResponse(w http.ResponseWriter, requestID string)**

- [ ] **Helper Functions**
  - [ ] getRequestID(r *http.Request) string
  - [ ] writeJSON(w http.ResponseWriter, statusCode int, data interface{}) error

**Estimated Lines**: ~200 lines

---

### 4.2 Market Data Handlers (`internal/api/`)

#### File: `internal/api/market_data_handlers.go`
- [ ] **Type: MarketDataHandler struct**
  - [ ] client client.MarketDataClient
  - [ ] logger *logger.Logger
  - [ ] metrics *metrics.MetricsCollector

- [ ] **Function: NewMarketDataHandler(client client.MarketDataClient, logger *logger.Logger, metrics *metrics.MetricsCollector) *MarketDataHandler**

- [ ] **Handler Methods**
  - [ ] HandleGetTrades(w http.ResponseWriter, r *http.Request)
    - [ ] Parse query parameters (book, limit)
    - [ ] Validate parameters
    - [ ] Call client.GetRecentTrades
    - [ ] Handle errors
    - [ ] Return response

  - [ ] HandleGetTrade(w http.ResponseWriter, r *http.Request)
    - [ ] Parse path parameters (book, tradeID)
    - [ ] Validate parameters
    - [ ] Call client.GetTrade
    - [ ] Handle errors
    - [ ] Return response

  - [ ] HandleGetOrderBook(w http.ResponseWriter, r *http.Request)
    - [ ] Parse query parameters (book)
    - [ ] Call client.GetOrderBook
    - [ ] Return response

  - [ ] HandleGetTicker(w http.ResponseWriter, r *http.Request)
    - [ ] Parse query parameters (book)
    - [ ] Call client.GetTicker
    - [ ] Return response

  - [ ] HandleGetTradeStats(w http.ResponseWriter, r *http.Request)
    - [ ] Parse query parameters (book)
    - [ ] Call client.GetTradeStats
    - [ ] Return response

  - [ ] HandleGetMarketSummary(w http.ResponseWriter, r *http.Request)
    - [ ] Call client.GetMarketSummary
    - [ ] Return response

**Estimated Lines**: ~300 lines

---

### 4.3 Order Management Handlers (`internal/api/`)

#### File: `internal/api/order_handlers.go`
- [ ] **Type: OrderHandler struct**
  - [ ] client client.OrderManagementClient
  - [ ] logger *logger.Logger
  - [ ] metrics *metrics.MetricsCollector

- [ ] **Function: NewOrderHandler(client client.OrderManagementClient, logger *logger.Logger, metrics *metrics.MetricsCollector) *OrderHandler**

- [ ] **Handler Methods**
  - [ ] HandleListOrders(w http.ResponseWriter, r *http.Request)
    - [ ] Parse query parameters (filters)
    - [ ] Validate parameters
    - [ ] Call client.ListOrders
    - [ ] Return response

  - [ ] HandleGetOrder(w http.ResponseWriter, r *http.Request)
    - [ ] Parse path parameter (orderID)
    - [ ] Call client.GetOrder
    - [ ] Return response

  - [ ] HandleCancelOrder(w http.ResponseWriter, r *http.Request)
    - [ ] Parse path parameter (orderID)
    - [ ] Call client.CancelOrder
    - [ ] Return response

  - [ ] HandleGetActiveOrders(w http.ResponseWriter, r *http.Request)
    - [ ] Call client.GetActiveOrders
    - [ ] Return response

  - [ ] HandleGetOrderHistory(w http.ResponseWriter, r *http.Request)
    - [ ] Parse query parameters (filters)
    - [ ] Call client.GetOrderHistory
    - [ ] Return response

  - [ ] HandleListPositions(w http.ResponseWriter, r *http.Request)
    - [ ] Parse query parameters (filters)
    - [ ] Call client.ListPositions
    - [ ] Return response

  - [ ] HandleGetPosition(w http.ResponseWriter, r *http.Request)
    - [ ] Parse path parameter (book)
    - [ ] Call client.GetPosition
    - [ ] Return response

  - [ ] HandleGetPositionSummary(w http.ResponseWriter, r *http.Request)
    - [ ] Call client.GetPositionSummary
    - [ ] Return response

**Estimated Lines**: ~350 lines

---

### 4.4 Strategy Executor Handlers (`internal/api/`)

#### File: `internal/api/strategy_handlers.go`
- [ ] **Type: StrategyHandler struct**
  - [ ] client client.StrategyExecutorClient
  - [ ] logger *logger.Logger
  - [ ] metrics *metrics.MetricsCollector

- [ ] **Function: NewStrategyHandler(client client.StrategyExecutorClient, logger *logger.Logger, metrics *metrics.MetricsCollector) *StrategyHandler**

- [ ] **Handler Methods**
  - [ ] HandleGetStatus(w http.ResponseWriter, r *http.Request)
    - [ ] Call client.GetStatus
    - [ ] Return response

  - [ ] HandleListStrategies(w http.ResponseWriter, r *http.Request)
    - [ ] Call client.ListStrategies
    - [ ] Return response

  - [ ] HandleGetStrategy(w http.ResponseWriter, r *http.Request)
    - [ ] Parse path parameter (name)
    - [ ] Call client.GetStrategy
    - [ ] Return response

  - [ ] HandleStartStrategy(w http.ResponseWriter, r *http.Request)
    - [ ] Parse path parameter (name)
    - [ ] Call client.StartStrategy
    - [ ] Return response

  - [ ] HandleStopStrategy(w http.ResponseWriter, r *http.Request)
    - [ ] Parse path parameter (name)
    - [ ] Call client.StopStrategy
    - [ ] Return response

  - [ ] HandleUpdateStrategyConfig(w http.ResponseWriter, r *http.Request)
    - [ ] Parse path parameter (name)
    - [ ] Parse request body (config)
    - [ ] Validate config
    - [ ] Call client.UpdateStrategyConfig
    - [ ] Return response

**Estimated Lines**: ~250 lines

---

### 4.5 Aggregation Handlers (`internal/api/`)

#### File: `internal/api/aggregation_handlers.go`
- [ ] **Type: AggregationHandler struct**
  - [ ] marketDataClient client.MarketDataClient
  - [ ] orderClient client.OrderManagementClient
  - [ ] strategyClient client.StrategyExecutorClient
  - [ ] logger *logger.Logger
  - [ ] metrics *metrics.MetricsCollector

- [ ] **Function: NewAggregationHandler(marketDataClient, orderClient, strategyClient, logger, metrics) *AggregationHandler**

- [ ] **Handler Methods**
  - [ ] HandleGetDashboard(w http.ResponseWriter, r *http.Request)
    - [ ] Call multiple services concurrently
    - [ ] Aggregate market summary, positions, active orders
    - [ ] Return combined response

  - [ ] HandleGetPortfolio(w http.ResponseWriter, r *http.Request)
    - [ ] Get positions summary
    - [ ] Get active orders
    - [ ] Calculate total values
    - [ ] Return portfolio overview

  - [ ] HandleGetTradingOverview(w http.ResponseWriter, r *http.Request)
    - [ ] Get active strategies
    - [ ] Get recent orders
    - [ ] Get market summary
    - [ ] Return trading overview

  - [ ] HandleGetSystemStatus(w http.ResponseWriter, r *http.Request)
    - [ ] Check health of all backend services
    - [ ] Aggregate status
    - [ ] Return system status

- [ ] **Helper Methods**
  - [ ] callServicesInParallel(calls []func() error) []error
  - [ ] aggregateResults(results ...interface{}) interface{}

**Estimated Lines**: ~350 lines

---

### 4.6 General Handlers (`internal/api/`)

#### File: `internal/api/handlers.go`
- [ ] **Type: Handler struct**
  - [ ] config *config.Config
  - [ ] logger *logger.Logger
  - [ ] metrics *metrics.MetricsCollector
  - [ ] healthManager *health.HealthManager
  - [ ] marketDataHandler *MarketDataHandler
  - [ ] orderHandler *OrderHandler
  - [ ] strategyHandler *StrategyHandler
  - [ ] aggregationHandler *AggregationHandler

- [ ] **Function: NewHandler(cfg, logger, metrics, healthManager, marketDataClient, orderClient, strategyClient) *Handler**

- [ ] **Health Check Methods**
  - [ ] HandleHealth(w http.ResponseWriter, r *http.Request)
    - [ ] Call health manager
    - [ ] Return detailed health status

  - [ ] HandleLiveness(w http.ResponseWriter, r *http.Request)
    - [ ] Return 200 OK

  - [ ] HandleReadiness(w http.ResponseWriter, r *http.Request)
    - [ ] Check backend services
    - [ ] Return 200 if ready, 503 otherwise

  - [ ] HandleServiceStatus(w http.ResponseWriter, r *http.Request)
    - [ ] Return service status info

  - [ ] HandleVersion(w http.ResponseWriter, r *http.Request)
    - [ ] Return API version info

**Estimated Lines**: ~200 lines

---

## Phase 5: Router and Integration

### 5.1 Router (`internal/router/`)

#### File: `internal/router/routes.go`
- [ ] **Type: RouteDefinition struct**
  - [ ] Path string
  - [ ] Method string
  - [ ] Handler http.HandlerFunc
  - [ ] Middleware []func(http.Handler) http.Handler

- [ ] **Function: DefineRoutes(handler *api.Handler) []RouteDefinition**
  - [ ] Health endpoints
    - [ ] GET /health
    - [ ] GET /health/live
    - [ ] GET /health/ready

  - [ ] Service status
    - [ ] GET /api/v1/status
    - [ ] GET /api/v1/version

  - [ ] Market data routes
    - [ ] GET /api/v1/market-data/trades
    - [ ] GET /api/v1/market-data/trades/:id
    - [ ] GET /api/v1/market-data/orderbook
    - [ ] GET /api/v1/market-data/ticker
    - [ ] GET /api/v1/market-data/stats/trades
    - [ ] GET /api/v1/market-data/summary

  - [ ] Order routes
    - [ ] GET /api/v1/orders
    - [ ] GET /api/v1/orders/:id
    - [ ] POST /api/v1/orders/:id/cancel
    - [ ] GET /api/v1/orders/active
    - [ ] GET /api/v1/orders/history

  - [ ] Position routes
    - [ ] GET /api/v1/positions
    - [ ] GET /api/v1/positions/:book
    - [ ] GET /api/v1/positions/summary

  - [ ] Strategy routes
    - [ ] GET /api/v1/strategies
    - [ ] GET /api/v1/strategies/:name
    - [ ] POST /api/v1/strategies/:name/start
    - [ ] POST /api/v1/strategies/:name/stop
    - [ ] PUT /api/v1/strategies/:name/config

  - [ ] Aggregation routes
    - [ ] GET /api/v1/dashboard
    - [ ] GET /api/v1/portfolio
    - [ ] GET /api/v1/trading/overview
    - [ ] GET /api/v1/system/status

  - [ ] Metrics
    - [ ] GET /metrics

**Estimated Lines**: ~200 lines

---

#### File: `internal/router/router.go`
- [ ] **Type: Router struct**
  - [ ] mux *http.ServeMux
  - [ ] middleware []func(http.Handler) http.Handler
  - [ ] logger *logger.Logger

- [ ] **Function: NewRouter(logger *logger.Logger) *Router**

- [ ] **Method: (r *Router) Use(middleware func(http.Handler) http.Handler)**
  - [ ] Add middleware to chain

- [ ] **Method: (r *Router) RegisterRoutes(routes []RouteDefinition)**
  - [ ] Register all routes with middleware

- [ ] **Method: (r *Router) Handler() http.Handler**
  - [ ] Return handler with middleware chain

- [ ] **Helper Methods**
  - [ ] chainMiddleware(h http.Handler, middleware []func(http.Handler) http.Handler) http.Handler
  - [ ] extractPathParams(pattern, path string) map[string]string

**Estimated Lines**: ~150 lines

---

### 5.2 Validation (`internal/validation/`)

#### File: `internal/validation/rules.go`
- [ ] **Validation Functions**
  - [ ] ValidateBook(book string) error
  - [ ] ValidateLimit(limit int) error
  - [ ] ValidateOrderID(orderID string) error
  - [ ] ValidateTradeID(tradeID uint64) error
  - [ ] ValidateStrategyName(name string) error
  - [ ] ValidateTimeRange(start, end time.Time) error
  - [ ] ValidateQueryParams(params map[string][]string, rules map[string]ValidationRule) error

- [ ] **Type: ValidationRule struct**
  - [ ] Required bool
  - [ ] Type string
  - [ ] Min, Max interface{}
  - [ ] Pattern *regexp.Regexp
  - [ ] Validator func(interface{}) error

**Estimated Lines**: ~150 lines

---

#### File: `internal/validation/validator.go`
- [ ] **Type: Validator struct**
  - [ ] logger *logger.Logger

- [ ] **Function: NewValidator(logger *logger.Logger) *Validator**

- [ ] **Method: (v *Validator) ValidateRequest(r *http.Request, rules map[string]ValidationRule) error**
  - [ ] Extract parameters
  - [ ] Validate each parameter
  - [ ] Return validation errors

- [ ] **Method: (v *Validator) ValidateJSON(body []byte, target interface{}) error**
  - [ ] Parse JSON
  - [ ] Validate structure
  - [ ] Return errors

**Estimated Lines**: ~100 lines

---

### 5.3 Main Application (`cmd/`)

#### File: `cmd/main.go`
- [ ] **Type: Application struct**
  - [ ] config *config.Config
  - [ ] logger *logger.Logger
  - [ ] metrics *metrics.MetricsCollector
  - [ ] healthManager *health.HealthManager
  - [ ] clientFactory *client.ClientFactory
  - [ ] server *server.HTTPServer
  - [ ] ctx context.Context
  - [ ] cancel context.CancelFunc

- [ ] **Function: NewApplication() (*Application, error)**
  - [ ] Load configuration
  - [ ] Initialize logger
  - [ ] Initialize metrics
  - [ ] Initialize health manager
  - [ ] Initialize client factory
  - [ ] Initialize handlers
  - [ ] Initialize router
  - [ ] Initialize HTTP server
  - [ ] Wire dependencies

- [ ] **Method: (app *Application) Start() error**
  - [ ] Start metrics collection
  - [ ] Add health checks for backend services
  - [ ] Start HTTP server
  - [ ] Log startup message

- [ ] **Method: (app *Application) Stop() error**
  - [ ] Graceful shutdown
  - [ ] Stop HTTP server
  - [ ] Close clients
  - [ ] Log shutdown message

- [ ] **Method: (app *Application) Run() error**
  - [ ] Start application
  - [ ] Setup signal handling
  - [ ] Wait for shutdown signal
  - [ ] Perform graceful shutdown

- [ ] **Function: main()**
  - [ ] Create application
  - [ ] Run application
  - [ ] Handle errors

**Estimated Lines**: ~350 lines

---

## Phase 6: Testing and Documentation

### 6.1 Unit Tests

#### Configuration Tests (`internal/config/config_test.go`)
- [ ] TestLoad - successful loading
- [ ] TestLoad_WithDefaults
- [ ] TestLoad_WithAllEnvVars
- [ ] TestValidate_Success
- [ ] TestValidate_MissingRequired
- [ ] TestValidate_InvalidValues
- [ ] TestHelperFunctions

**Estimated Lines**: ~150 lines

---

#### Client Tests (`internal/client/*_test.go`)

**Market Data Client Tests**
- [ ] TestNewMarketDataClient
- [ ] TestGetRecentTrades
- [ ] TestGetRecentTrades_Error
- [ ] TestGetTrade
- [ ] TestGetOrderBook
- [ ] TestGetTicker
- [ ] TestGetTradeStats
- [ ] TestGetMarketSummary
- [ ] TestHealth
- [ ] TestRetryLogic
- [ ] TestTimeout

**Estimated Lines**: ~300 lines per client (900 total)

---

#### Middleware Tests (`internal/middleware/*_test.go`)

**Logging Middleware Tests**
- [ ] TestLoggingMiddleware
- [ ] TestLoggingMiddleware_RequestIDGeneration
- [ ] TestLoggingMiddleware_ErrorLogging

**Metrics Middleware Tests**
- [ ] TestMetricsMiddleware
- [ ] TestMetricsMiddleware_InFlightRequests
- [ ] TestMetricsMiddleware_RequestDuration

**Rate Limiter Tests**
- [ ] TestRateLimiter
- [ ] TestRateLimiter_ExceedLimit
- [ ] TestRateLimiter_MultipleClients
- [ ] TestRateLimiter_Cleanup

**Circuit Breaker Tests**
- [ ] TestCircuitBreaker
- [ ] TestCircuitBreaker_OpenState
- [ ] TestCircuitBreaker_HalfOpenState
- [ ] TestCircuitBreaker_Recovery

**Recovery Tests**
- [ ] TestRecoveryMiddleware
- [ ] TestRecoveryMiddleware_Panic

**Estimated Lines**: ~500 lines total

---

#### Handler Tests (`internal/api/*_test.go`)

**Market Data Handler Tests**
- [ ] TestHandleGetTrades
- [ ] TestHandleGetTrades_InvalidParams
- [ ] TestHandleGetTrade
- [ ] TestHandleGetOrderBook
- [ ] TestHandleGetTicker

**Order Handler Tests**
- [ ] TestHandleListOrders
- [ ] TestHandleGetOrder
- [ ] TestHandleCancelOrder
- [ ] TestHandleGetPositions

**Strategy Handler Tests**
- [ ] TestHandleListStrategies
- [ ] TestHandleStartStrategy
- [ ] TestHandleStopStrategy

**Aggregation Handler Tests**
- [ ] TestHandleGetDashboard
- [ ] TestHandleGetPortfolio

**Estimated Lines**: ~600 lines total

---

### 6.2 Integration Tests (`test/integration/`)

#### File: `test/integration/market_data_test.go`
- [ ] TestMarketDataIntegration_GetTrades
- [ ] TestMarketDataIntegration_GetOrderBook
- [ ] TestMarketDataIntegration_GetTicker
- [ ] TestMarketDataIntegration_Errors

**Estimated Lines**: ~200 lines

---

#### File: `test/integration/order_management_test.go`
- [ ] TestOrderManagementIntegration_ListOrders
- [ ] TestOrderManagementIntegration_GetOrder
- [ ] TestOrderManagementIntegration_CancelOrder
- [ ] TestOrderManagementIntegration_GetPositions

**Estimated Lines**: ~200 lines

---

#### File: `test/integration/strategy_executor_test.go`
- [ ] TestStrategyExecutorIntegration_ListStrategies
- [ ] TestStrategyExecutorIntegration_StartStrategy
- [ ] TestStrategyExecutorIntegration_StopStrategy

**Estimated Lines**: ~150 lines

---

#### File: `test/integration/aggregation_test.go`
- [ ] TestAggregation_Dashboard
- [ ] TestAggregation_Portfolio
- [ ] TestAggregation_SystemStatus

**Estimated Lines**: ~150 lines

---

### 6.3 End-to-End Tests (`test/e2e/`)

#### File: `test/e2e/api_gateway_test.go`
- [ ] TestE2E_FullFlow
- [ ] TestE2E_ErrorHandling
- [ ] TestE2E_RateLimiting
- [ ] TestE2E_CircuitBreaker
- [ ] TestE2E_HealthChecks

**Estimated Lines**: ~250 lines

---

### 6.4 Documentation

#### File: `README.md`
- [ ] Service overview
- [ ] Features list
- [ ] Architecture diagram
- [ ] Quick start guide
- [ ] API endpoints documentation
- [ ] Configuration guide
- [ ] Environment variables
- [ ] Running the service
- [ ] Testing guide
- [ ] Deployment guide
- [ ] Monitoring and metrics
- [ ] Troubleshooting
- [ ] Contributing guidelines

**Estimated Lines**: ~600 lines

---

#### File: `TESTING.md`
- [ ] Testing strategy overview
- [ ] Unit testing guide
- [ ] Integration testing guide
- [ ] E2E testing guide
- [ ] Running tests
- [ ] Test coverage
- [ ] Writing new tests
- [ ] Test fixtures and mocks
- [ ] CI/CD integration

**Estimated Lines**: ~300 lines

---

#### File: `run_tests.sh`
- [ ] Unit test runner
- [ ] Integration test runner
- [ ] E2E test runner
- [ ] Coverage report generation
- [ ] Test result formatting
- [ ] Cleanup

**Estimated Lines**: ~100 lines

---

#### File: `Dockerfile`
- [ ] Multi-stage build
- [ ] Go build stage
- [ ] Runtime stage
- [ ] Health check
- [ ] Metadata labels

**Estimated Lines**: ~30 lines

---

## Summary Statistics

### Total Files to Create: 50+
### Total Lines of Code: ~8,500-9,000

### Breakdown by Phase:
1. **Phase 1 (Core Infrastructure)**: ~770 lines
2. **Phase 2 (Client Layer)**: ~1,600 lines
3. **Phase 3 (Middleware Layer)**: ~940 lines
4. **Phase 4 (Handler Layer)**: ~1,850 lines
5. **Phase 5 (Router & Integration)**: ~950 lines
6. **Phase 6 (Testing & Docs)**: ~2,400 lines

### Breakdown by Category:
- **Production Code**: ~6,000 lines
- **Test Code**: ~2,500 lines
- **Documentation**: ~1,000 lines

### Test Coverage Target:
- **Unit Tests**: >80% coverage
- **Integration Tests**: All critical paths
- **E2E Tests**: Happy path + error scenarios

---

## Implementation Order (Recommended)

### Week 1: Foundation (Days 1-5)
1. Day 1: Configuration and Logger
2. Day 2: Metrics and Server
3. Day 3: Client Types and Market Data Client
4. Day 4: Order Management Client
5. Day 5: Strategy Executor Client and Client Factory

### Week 2: Middleware (Days 6-10)
6. Day 6: Logging and Metrics Middleware
7. Day 7: Rate Limiter and Circuit Breaker
8. Day 8: CORS, Recovery, Timeout
9. Day 9: Auth Middleware (placeholder)
10. Day 10: Middleware Integration and Testing

### Week 3: Handlers (Days 11-15)
11. Day 11: Response Helpers and Market Data Handlers
12. Day 12: Order Management Handlers
13. Day 13: Strategy Executor Handlers
14. Day 14: Aggregation Handlers
15. Day 15: General Handlers and Health Checks

### Week 4: Integration & Testing (Days 16-20)
16. Day 16: Router and Validation
17. Day 17: Main Application and Wiring
18. Day 18: Unit Tests (Part 1)
19. Day 19: Integration and E2E Tests
20. Day 20: Documentation and Final Polish

---

**Document Version**: 1.0  
**Last Updated**: October 27, 2025  
**Status**: Ready for Implementation

