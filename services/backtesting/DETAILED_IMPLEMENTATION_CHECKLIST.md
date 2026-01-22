# Backtesting Service - Detailed Implementation Checklist

**Version**: 2.0.0  
**Date**: October 27, 2025  
**Purpose**: File-by-file, method-by-method implementation guide

---

## Implementation Overview

### Totals
- **Total Files**: 72
- **Total Tests**: 20+
- **Estimated LOC**: ~11,000-14,000
- **Implementation Time**: 7 weeks

### Status Legend
- ✅ To Implement
- 🔄 In Progress
- ✔️ Complete
- ⏸️ Blocked
- 🔮 Future Enhancement

---

## PHASE 1: Foundation (Week 1)

### 1.1 `cmd/main.go` ✅

**Purpose**: Application entry point with graceful lifecycle management

**Pattern Reference**: `services/order-management/cmd/main.go`

```go
package main

// Constants
const (
    appName    = "backtesting"
    appVersion = "1.0.0"
    shutdownTimeout = 30 * time.Second
)

// Structs
type Application struct {
    logger           *logger.Logger
    config           *config.Config
    
    // Core components
    healthManager    *health.HealthManager
    metricsCollector *metrics.MetricsCollector
    
    // Business logic
    backtestManager  *manager.BacktestManager
    engine           engine.BacktestEngine
    
    // Data layer
    dataProvider     data.DataProvider
    resultStorage    storage.ResultStorage
    
    // API
    httpServer       *server.HTTPServer
    
    // Context
    ctx              context.Context
    cancel           context.CancelFunc
}

// Functions to implement:
func NewApplication() (*Application, error)
    - Load configuration
    - Initialize logger
    - Create context
    - Initialize metrics collector
    - Initialize health manager
    - Add health checkers (service, redis, market-data)
    - Initialize data provider
    - Initialize result storage
    - Initialize backtest engine
    - Initialize backtest manager
    - Initialize API handlers
    - Initialize HTTP server
    - Return application instance

func (app *Application) Start() error
    - Start metrics collection goroutine
    - Start backtest manager
    - Start HTTP server in goroutine
    - Log startup complete

func (app *Application) Stop() error
    - Log shutdown initiation
    - Create shutdown context with timeout
    - Stop backtest manager
    - Stop HTTP server
    - Close data provider
    - Close result storage
    - Cancel application context
    - Return any errors

func (app *Application) Run() error
    - Call Start()
    - Set up signal handling (SIGINT, SIGTERM)
    - Wait for signal
    - Call Stop()
    - Return result

func (app *Application) startMetricsCollection()
    - Create ticker (30 seconds)
    - Loop: record uptime and health
    - Stop on context cancellation

func main()
    - Create application
    - Run application
    - Handle errors
    - Exit
```

**Dependencies**:
```go
import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"
    
    "bitso-trading-platform/backtesting/internal/api"
    "bitso-trading-platform/backtesting/internal/config"
    "bitso-trading-platform/backtesting/internal/data"
    "bitso-trading-platform/backtesting/internal/engine"
    "bitso-trading-platform/backtesting/internal/logger"
    "bitso-trading-platform/backtesting/internal/manager"
    "bitso-trading-platform/backtesting/internal/metrics"
    "bitso-trading-platform/backtesting/internal/server"
    "bitso-trading-platform/backtesting/internal/storage"
    "bitso-trading-platform/shared/pkg/health"
)
```

---

### 1.2 Configuration (`internal/config/`)

#### 1.2.1 `config.go` ✅

**Purpose**: Configuration structure definitions

**Pattern Reference**: `services/api-gateway/internal/config/config.go`

```go
package config

// Main configuration structure
type Config struct {
    Service        ServiceConfig
    MarketData     MarketDataConfig
    Redis          RedisConfig
    Storage        StorageConfig
    Execution      ExecutionConfig
    Logging        LoggingConfig
    Metrics        MetricsConfig
}

type ServiceConfig struct {
    Name        string
    Version     string
    Host        string
    Port        int
    Environment string
}

type MarketDataConfig struct {
    BaseURL    string
    Timeout    time.Duration
    RetryCount int
    RetryDelay time.Duration
}

type RedisConfig struct {
    Host     string
    Port     int
    Password string
    DB       int
    PoolSize int
}

type StorageConfig struct {
    Type          string  // "redis", "file", "s3"
    Path          string
    RetentionDays int
}

type ExecutionConfig struct {
    MaxConcurrentBacktests int
    DefaultSlippageModel   string
    DefaultSlippageValue   float64
    DefaultCommissionRate  float64
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

// Methods
func (c *Config) Validate() error
func (c *ServiceConfig) GetAddress() string
func (c *RedisConfig) GetAddress() string
```

---

#### 1.2.2 `loader.go` ✅

**Purpose**: Load configuration from environment

```go
package config

// Functions
func Load() (*Config, error)
    - Call loadFromEnv()
    - Set defaults via setDefaults()
    - Validate configuration
    - Return config or error

func loadFromEnv() (*Config, error)
    - Load each config section from environment
    - Use helper functions (getEnv, getEnvAsInt, etc.)
    - Return populated config

func setDefaults(config *Config)
    - Set default values for optional fields
    - Service: port 8084, host 0.0.0.0
    - Redis: localhost:6379
    - Logging: info level, json format
    - Metrics: enabled, port 9094

// Helper functions
func getEnv(key, defaultValue string) string
func getEnvAsInt(key string, defaultValue int) int
func getEnvAsFloat(key string, defaultValue float64) float64
func getEnvAsBool(key string, defaultValue bool) bool
func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration
```

---

#### 1.2.3 `validation.go` ✅

**Purpose**: Configuration validation

```go
package config

// Functions
func (c *Config) Validate() error
    - Validate all sub-configs
    - Return first error encountered

func validateServiceConfig(cfg *ServiceConfig) error
    - Check name not empty
    - Check port in range 1-65535
    - Return error if invalid

func validateMarketDataConfig(cfg *MarketDataConfig) error
    - Check BaseURL not empty
    - Check Timeout positive
    - Check RetryCount non-negative

func validateRedisConfig(cfg *RedisConfig) error
    - Check Host not empty
    - Check Port in range 1-65535

func validateStorageConfig(cfg *StorageConfig) error
    - Check Type is valid ("redis", "file", "s3")
    - Check Path not empty if type is "file"

func validateExecutionConfig(cfg *ExecutionConfig) error
    - Check MaxConcurrentBacktests positive
    - Check slippage value reasonable
    - Check commission rate reasonable

func validateLoggingConfig(cfg *LoggingConfig) error
    - Check Level is valid (trace, debug, info, warn, error, fatal)
    - Check Format is valid (json, console)

func validateMetricsConfig(cfg *MetricsConfig) error
    - Check Port in range if enabled
```

---

#### 1.2.4 `config_test.go` ✅

```go
package config

// Tests
func TestLoad(t *testing.T)
func TestLoadWithEnvVars(t *testing.T)
func TestLoadDefaults(t *testing.T)
func TestValidate(t *testing.T)
func TestValidateInvalidPort(t *testing.T)
func TestValidateInvalidURL(t *testing.T)
func TestGetEnvHelpers(t *testing.T)
func TestGetEnvAsInt(t *testing.T)
func TestGetEnvAsDuration(t *testing.T)
```

---

### 1.3 Logger (`internal/logger/`)

#### 1.3.1 `logger.go` ✅

**Purpose**: Zerolog-based structured logging wrapper

**Pattern Reference**: `services/order-management/internal/logger/logger.go`

```go
package logger

// Interface
type Logger interface {
    Debug(msg string, fields map[string]interface{})
    Info(msg string, fields map[string]interface{})
    Warn(msg string, fields map[string]interface{})
    Error(msg string, fields map[string]interface{})
    Fatal(msg string, fields map[string]interface{})
    With(fields map[string]interface{}) Logger
}

// Config
type Config struct {
    Level  string  // trace, debug, info, warn, error, fatal
    Format string  // json, console
    Output string  // stdout, stderr, file path
}

// Implementation
type ZerologLogger struct {
    logger zerolog.Logger
}

// Functions
func New(cfg *Config) *ZerologLogger
    - Parse log level
    - Set up output writer (console or file)
    - Configure format (json or pretty console)
    - Return logger instance

func (l *ZerologLogger) Debug(msg string, fields map[string]interface{})
func (l *ZerologLogger) Info(msg string, fields map[string]interface{})
func (l *ZerologLogger) Warn(msg string, fields map[string]interface{})
func (l *ZerologLogger) Error(msg string, fields map[string]interface{})
func (l *ZerologLogger) Fatal(msg string, fields map[string]interface{})
func (l *ZerologLogger) With(fields map[string]interface{}) Logger

// Helper
func addFields(event *zerolog.Event, fields map[string]interface{}) *zerolog.Event
```

---

### 1.4 Metrics (`internal/metrics/`)

#### 1.4.1 `prometheus.go` ✅

**Purpose**: Prometheus metrics collector

**Pattern Reference**: `services/order-management/internal/metrics/prometheus.go`

```go
package metrics

// Struct
type MetricsCollector struct {
    // Backtest metrics
    backtestsCreated       prometheus.Counter
    backtestsCompleted     *prometheus.CounterVec
    backtestsFailed        *prometheus.CounterVec
    backtestDuration       *prometheus.HistogramVec
    activeBacktests        prometheus.Gauge
    
    // Data processing metrics
    eventsProcessed        *prometheus.CounterVec
    dataLoadDuration       *prometheus.HistogramVec
    
    // Performance metrics
    metricsCalculationTime *prometheus.HistogramVec
    
    // System metrics
    serviceUptime          prometheus.Gauge
    serviceHealth          *prometheus.GaugeVec
}

// Functions
func NewMetricsCollector(serviceName string) *MetricsCollector
    - Initialize all metrics
    - Register with Prometheus
    - Return collector

// Backtest metrics
func (m *MetricsCollector) RecordBacktestCreated()
func (m *MetricsCollector) RecordBacktestCompleted(status string, duration time.Duration)
func (m *MetricsCollector) RecordBacktestFailed(reason string)
func (m *MetricsCollector) RecordActiveBacktests(count int)

// Data metrics
func (m *MetricsCollector) RecordEventsProcessed(eventType string, count int)
func (m *MetricsCollector) RecordDataLoadDuration(source string, duration time.Duration)

// Performance metrics
func (m *MetricsCollector) RecordMetricsCalculation(metric string, duration time.Duration)

// System metrics
func (m *MetricsCollector) RecordServiceUptime(uptime time.Duration)
func (m *MetricsCollector) RecordServiceHealth(healthy bool)
```

---

#### 1.4.2 `collector.go` ✅

**Purpose**: Custom metrics collection logic

```go
package metrics

// Additional helper functions
func (m *MetricsCollector) StartBacktestTimer() func(status string)
    - Record start time
    - Return completion function that records duration

func (m *MetricsCollector) ObserveBacktestProgress(backtestID string, progress float64)
    - Optional: Record progress for monitoring

func (m *MetricsCollector) RecordAPIRequest(endpoint, method string, duration time.Duration, statusCode int)
    - Record HTTP request metrics
```

---

### 1.5 Models (`internal/models/`)

#### 1.5.1 `backtest.go` ✅

**Purpose**: Backtest domain model

```go
package models

// Types
type BacktestStatus string

const (
    BacktestStatusPending    BacktestStatus = "pending"
    BacktestStatusQueued     BacktestStatus = "queued"
    BacktestStatusRunning    BacktestStatus = "running"
    BacktestStatusCompleted  BacktestStatus = "completed"
    BacktestStatusFailed     BacktestStatus = "failed"
    BacktestStatusCancelled  BacktestStatus = "cancelled"
)

// Struct
type Backtest struct {
    ID          string          `json:"id"`
    Config      *BacktestConfig `json:"config"`
    Status      BacktestStatus  `json:"status"`
    Progress    float64         `json:"progress"`
    Result      *BacktestResult `json:"result,omitempty"`
    Error       string          `json:"error,omitempty"`
    StartedAt   *time.Time      `json:"started_at,omitempty"`
    CompletedAt *time.Time      `json:"completed_at,omitempty"`
    CreatedAt   time.Time       `json:"created_at"`
    UpdatedAt   time.Time       `json:"updated_at"`
}

// Methods
func NewBacktest(config *BacktestConfig) *Backtest
func (b *Backtest) Start()
func (b *Backtest) UpdateProgress(progress float64)
func (b *Backtest) Complete(result *BacktestResult)
func (b *Backtest) Fail(err error)
func (b *Backtest) Cancel()
func (b *Backtest) IsActive() bool
func (b *Backtest) IsCompleted() bool
func (b *Backtest) Duration() time.Duration
func (b *Backtest) ToJSON() ([]byte, error)
func BacktestFromJSON(data []byte) (*Backtest, error)
```

---

#### 1.5.2 `config.go` ✅

**Purpose**: Backtest configuration model

```go
package models

// Struct
type BacktestConfig struct {
    ID              string                 `json:"id"`
    Name            string                 `json:"name"`
    Description     string                 `json:"description,omitempty"`
    
    // Time range
    StartDate       time.Time              `json:"start_date"`
    EndDate         time.Time              `json:"end_date"`
    
    // Trading parameters
    Book            string                 `json:"book"`
    InitialBalance  float64                `json:"initial_balance"`
    
    // Strategy configuration
    Strategy        string                 `json:"strategy"`
    StrategyParams  map[string]interface{} `json:"strategy_params"`
    
    // Execution settings
    SlippageModel   string                 `json:"slippage_model"`   // "none", "fixed", "percentage"
    SlippageValue   float64                `json:"slippage_value"`
    CommissionRate  float64                `json:"commission_rate"`
    
    // Data settings
    DataSource      string                 `json:"data_source"`      // "market-data", "file"
    DataGranularity string                 `json:"data_granularity"` // "tick", "1m", "5m"
    
    // Metadata
    CreatedAt       time.Time              `json:"created_at"`
    CreatedBy       string                 `json:"created_by,omitempty"`
}

type Range struct {
    Min  float64 `json:"min"`
    Max  float64 `json:"max"`
    Step float64 `json:"step"`
}

// Methods
func NewBacktestConfig(name, book string, startDate, endDate time.Time) *BacktestConfig
func (c *BacktestConfig) Validate() error
func (c *BacktestConfig) GetDuration() time.Duration
func (c *BacktestConfig) GetDays() int
func (c *BacktestConfig) Clone() *BacktestConfig
func (c *BacktestConfig) WithStrategy(strategy string, params map[string]interface{}) *BacktestConfig
```

---

#### 1.5.3 `result.go` ✅

**Purpose**: Backtest result models

```go
package models

// Structs
type BacktestResult struct {
    ID            string              `json:"id"`
    BacktestID    string              `json:"backtest_id"`
    ConfigID      string              `json:"config_id"`
    
    Status        string              `json:"status"`
    Progress      float64             `json:"progress"`
    Error         string              `json:"error,omitempty"`
    
    Summary       *PerformanceSummary `json:"summary"`
    Trades        []Trade             `json:"trades"`
    EquityCurve   []EquityPoint       `json:"equity_curve"`
    Positions     []Position          `json:"positions"`
    
    StartedAt     time.Time           `json:"started_at"`
    CompletedAt   *time.Time          `json:"completed_at,omitempty"`
    Duration      time.Duration       `json:"duration"`
    
    Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

type PerformanceSummary struct {
    // Returns
    TotalReturn          float64 `json:"total_return"`
    TotalReturnPercent   float64 `json:"total_return_percent"`
    AnnualizedReturn     float64 `json:"annualized_return"`
    
    // Risk metrics
    Volatility           float64 `json:"volatility"`
    SharpeRatio          float64 `json:"sharpe_ratio"`
    SortinoRatio         float64 `json:"sortino_ratio"`
    MaxDrawdown          float64 `json:"max_drawdown"`
    MaxDrawdownPercent   float64 `json:"max_drawdown_percent"`
    
    // Trade statistics
    TotalTrades          int     `json:"total_trades"`
    WinningTrades        int     `json:"winning_trades"`
    LosingTrades         int     `json:"losing_trades"`
    WinRate              float64 `json:"win_rate"`
    AverageWin           float64 `json:"average_win"`
    AverageLoss          float64 `json:"average_loss"`
    ProfitFactor         float64 `json:"profit_factor"`
    
    // Position metrics
    AverageHoldingTime   time.Duration `json:"average_holding_time"`
    MaxPosition          float64       `json:"max_position"`
    
    // P&L
    GrossProfitLoss      float64 `json:"gross_profit_loss"`
    NetProfitLoss        float64 `json:"net_profit_loss"`
    TotalCommissions     float64 `json:"total_commissions"`
    
    // Portfolio
    FinalBalance         float64 `json:"final_balance"`
    PeakBalance          float64 `json:"peak_balance"`
    InitialBalance       float64 `json:"initial_balance"`
}

type EquityPoint struct {
    Timestamp time.Time `json:"timestamp"`
    Balance   float64   `json:"balance"`
    Equity    float64   `json:"equity"`
    Return    float64   `json:"return"`
    Drawdown  float64   `json:"drawdown"`
}

// Methods
func NewBacktestResult(backtestID, configID string) *BacktestResult
func (r *BacktestResult) AddTrade(trade Trade)
func (r *BacktestResult) AddEquityPoint(point EquityPoint)
func (r *BacktestResult) SetSummary(summary *PerformanceSummary)
func (r *BacktestResult) MarkCompleted()
func (r *BacktestResult) ToJSON() ([]byte, error)
func BacktestResultFromJSON(data []byte) (*BacktestResult, error)
```

---

#### 1.5.4 `trade.go` ✅

**Purpose**: Trade model

```go
package models

type Trade struct {
    ID                string        `json:"id"`
    EntryTime         time.Time     `json:"entry_time"`
    ExitTime          time.Time     `json:"exit_time"`
    Book              string        `json:"book"`
    Side              string        `json:"side"`          // "buy", "sell"
    EntryPrice        float64       `json:"entry_price"`
    ExitPrice         float64       `json:"exit_price"`
    Amount            float64       `json:"amount"`
    ProfitLoss        float64       `json:"profit_loss"`
    ProfitLossPercent float64       `json:"profit_loss_percent"`
    Commission        float64       `json:"commission"`
    Slippage          float64       `json:"slippage"`
    HoldingTime       time.Duration `json:"holding_time"`
    StrategySignal    string        `json:"strategy_signal,omitempty"`
}

// Methods
func NewTrade(side, book string, entryPrice, amount, commission, slippage float64, entryTime time.Time) *Trade
func (t *Trade) Close(exitPrice float64, exitTime time.Time)
func (t *Trade) CalculatePL()
func (t *Trade) IsWinning() bool
func (t *Trade) IsLosing() bool
```

---

#### 1.5.5 `portfolio.go` ✅

**Purpose**: Portfolio and position models

```go
package models

type Position struct {
    Book         string    `json:"book"`
    Size         float64   `json:"size"`          // Positive for long, negative for short
    AveragePrice float64   `json:"average_price"`
    CurrentPrice float64   `json:"current_price"`
    UnrealizedPL float64   `json:"unrealized_pl"`
    CostBasis    float64   `json:"cost_basis"`
    Timestamp    time.Time `json:"timestamp"`
}

// Methods
func NewPosition(book string) *Position
func (p *Position) AddSize(amount, price float64)
func (p *Position) ReduceSize(amount, price float64) float64
func (p *Position) UpdateCurrentPrice(price float64)
func (p *Position) CalculateUnrealizedPL() float64
func (p *Position) IsEmpty() bool
func (p *Position) IsLong() bool
func (p *Position) IsShort() bool
```

---

#### 1.5.6 `event.go` ✅

**Purpose**: Market event wrapper

```go
package models

import (
    "bitso-trading-platform/shared/pkg/bitso"
    "bitso-trading-platform/shared/pkg/models"
)

type MarketEventType string

const (
    EventTypeTrade     MarketEventType = "trade"
    EventTypeTicker    MarketEventType = "ticker"
    EventTypeOrderBook MarketEventType = "orderbook"
)

type MarketEvent struct {
    EventType MarketEventType `json:"event_type"`
    Timestamp time.Time       `json:"timestamp"`
    Book      string          `json:"book"`
    Data      interface{}     `json:"data"`  // *bitso.Trade, *bitso.Ticker, or *bitso.OrderBook
}

// Methods
func NewTradeEvent(trade *bitso.Trade) *MarketEvent
func NewTickerEvent(ticker *bitso.Ticker) *MarketEvent
func NewOrderBookEvent(orderBook interface{}) *MarketEvent

func (e *MarketEvent) GetTrade() (*bitso.Trade, error)
func (e *MarketEvent) GetTicker() (*bitso.Ticker, error)
func (e *MarketEvent) GetOrderBook() (interface{}, error)
func (e *MarketEvent) IsTradeEvent() bool
func (e *MarketEvent) IsTickerEvent() bool
```

---

#### 1.5.7 `validation.go` ✅

**Purpose**: Model validation functions

```go
package models

// Functions
func ValidateBacktestConfig(config *BacktestConfig) error
func ValidateTimeRange(start, end time.Time) error
func ValidateStrategy(strategy string, params map[string]interface{}) error
func ValidateBook(book string) error
func ValidateBalance(balance float64) error
func ValidateSlippageModel(model string) error
func ValidateCommissionRate(rate float64) error
```

---

### 1.6 Tests for Phase 1 ✅

```go
// internal/models/models_test.go
func TestBacktestLifecycle(t *testing.T)
func TestBacktestStatusTransitions(t *testing.T)
func TestBacktestConfigValidation(t *testing.T)
func TestBacktestResultCreation(t *testing.T)
func TestTradeCalculations(t *testing.T)
func TestPositionTracking(t *testing.T)
func TestMarketEventCreation(t *testing.T)
func TestValidationFunctions(t *testing.T)
```

---

## PHASE 2: Data Layer (Week 2)

### 2.1 Data Providers (`internal/data/`)

#### 2.1.1 `provider.go` ✅

```go
package data

// Interface
type DataProvider interface {
    LoadHistoricalData(ctx context.Context, req *DataRequest) ([]models.MarketEvent, error)
    StreamData(ctx context.Context, req *DataRequest) (<-chan models.MarketEvent, error)
    GetDataRange(ctx context.Context, book string) (*DateRange, error)
    Close() error
}

type DataRequest struct {
    Book        string
    StartDate   time.Time
    EndDate     time.Time
    EventTypes  []MarketEventType  // trades, tickers, orderbooks
    Granularity string             // "tick", "1m", "5m"
    Limit       int
}

type DateRange struct {
    FirstDate time.Time
    LastDate  time.Time
}

// Helper functions
func (r *DataRequest) Validate() error
func (r *DataRequest) GetDuration() time.Duration
```

---

#### 2.1.2 `market_data_provider.go` ✅

```go
package data

// Struct
type MarketDataProvider struct {
    baseURL    string
    httpClient *http.Client
    cache      *Cache
    logger     logger.Logger
    retryCount int
    retryDelay time.Duration
}

// Functions
func NewMarketDataProvider(config *config.MarketDataConfig, cache *Cache, logger logger.Logger) *MarketDataProvider

func (p *MarketDataProvider) LoadHistoricalData(ctx context.Context, req *DataRequest) ([]models.MarketEvent, error)
    - Check cache first
    - Make HTTP request(s) with pagination
    - Parse response
    - Convert to MarketEvents
    - Store in cache
    - Return events

func (p *MarketDataProvider) StreamData(ctx context.Context, req *DataRequest) (<-chan models.MarketEvent, error)
    - Create channel
    - Launch goroutine to stream data
    - Handle pagination
    - Send events to channel
    - Close channel when done

func (p *MarketDataProvider) GetDataRange(ctx context.Context, book string) (*DateRange, error)
    - Query market-data service for available date range
    - Return date range

func (p *MarketDataProvider) fetchTrades(ctx context.Context, book string, start, end time.Time, limit int) ([]*bitso.Trade, error)
    - Build URL: GET /api/v1/trades?book={book}&from={start}&to={end}&limit={limit}
    - Make HTTP request with retries
    - Parse JSON response
    - Return trades

func (p *MarketDataProvider) fetchTickers(ctx context.Context, book string, start, end time.Time) ([]*bitso.Ticker, error)
    - Build URL: GET /api/v1/ticker/history?book={book}&from={start}&to={end}
    - Make HTTP request with retries
    - Parse JSON response
    - Return tickers

func (p *MarketDataProvider) makeRequest(ctx context.Context, url string) (*http.Response, error)
    - Create HTTP request
    - Add headers
    - Execute with retries and exponential backoff
    - Return response

func (p *MarketDataProvider) Close() error
```

---

#### 2.1.3 `file_provider.go` ✅

```go
package data

type FileProvider struct {
    basePath string
    logger   logger.Logger
}

func NewFileProvider(basePath string, logger logger.Logger) *FileProvider

func (p *FileProvider) LoadHistoricalData(ctx context.Context, req *DataRequest) ([]models.MarketEvent, error)
    - Build file path based on book and dates
    - Read file
    - Parse JSON
    - Convert to MarketEvents
    - Return events

func (p *FileProvider) StreamData(ctx context.Context, req *DataRequest) (<-chan models.MarketEvent, error)
    - Create channel
    - Launch goroutine to read and stream
    - Close channel when done

func (p *FileProvider) GetDataRange(ctx context.Context, book string) (*DateRange, error)
    - Scan directory for available files
    - Parse file names for dates
    - Return date range

func (p *FileProvider) readDataFile(filePath string) ([]models.MarketEvent, error)
func (p *FileProvider) parseFileName(fileName string) (book string, date time.Time, err error)
func (p *FileProvider) Close() error
```

---

#### 2.1.4 `cache.go` ✅

```go
package data

type Cache struct {
    redis  *redis.Client
    ttl    time.Duration
    logger logger.Logger
}

func NewCache(redisClient *redis.Client, ttl time.Duration, logger logger.Logger) *Cache

func (c *Cache) Get(ctx context.Context, key string) ([]models.MarketEvent, error)
    - Generate Redis key
    - Get from Redis
    - Deserialize
    - Return events or ErrNotFound

func (c *Cache) Set(ctx context.Context, key string, events []models.MarketEvent) error
    - Serialize events
    - Store in Redis with TTL
    - Return error if any

func (c *Cache) Delete(ctx context.Context, key string) error
func (c *Cache) generateKey(book string, start, end time.Time, eventType string) string
func (c *Cache) Close() error
```

---

#### 2.1.5 `provider_test.go` ✅

```go
package data

func TestLoadHistoricalData(t *testing.T)
func TestStreamData(t *testing.T)
func TestGetDataRange(t *testing.T)
func TestFetchTrades(t *testing.T)
func TestFetchTickers(t *testing.T)
func TestRetryLogic(t *testing.T)
func TestCacheHit(t *testing.T)
func TestCacheMiss(t *testing.T)
func TestFileProviderLoad(t *testing.T)
func TestInvalidRequest(t *testing.T)
```

---

### 2.2 Storage (`internal/storage/`)

#### 2.2.1 `storage.go` ✅

```go
package storage

// Interface
type ResultStorage interface {
    Save(ctx context.Context, result *models.BacktestResult) error
    Get(ctx context.Context, backtestID string) (*models.BacktestResult, error)
    List(ctx context.Context, filters *ListFilters) ([]*models.BacktestResult, error)
    Delete(ctx context.Context, backtestID string) error
    UpdateStatus(ctx context.Context, backtestID string, status string, progress float64) error
    Close() error
}

type ListFilters struct {
    Status     string
    Strategy   string
    StartDate  *time.Time
    EndDate    *time.Time
    Limit      int
    Offset     int
}
```

---

#### 2.2.2 `redis_storage.go` ✅

```go
package storage

type RedisStorage struct {
    client *redis.Client
    logger logger.Logger
    ttl    time.Duration
}

func NewRedisStorage(client *redis.Client, logger logger.Logger, ttl time.Duration) *RedisStorage

func (s *RedisStorage) Save(ctx context.Context, result *models.BacktestResult) error
    - Serialize result to JSON
    - Store in Redis with key: "backtest:result:{id}"
    - Add to index sets for filtering
    - Set TTL
    - Return error if any

func (s *RedisStorage) Get(ctx context.Context, backtestID string) (*models.BacktestResult, error)
    - Get from Redis
    - Deserialize
    - Return result or ErrNotFound

func (s *RedisStorage) List(ctx context.Context, filters *ListFilters) ([]*models.BacktestResult, error)
    - Query index sets based on filters
    - Retrieve matching results
    - Apply pagination (limit, offset)
    - Return results

func (s *RedisStorage) Delete(ctx context.Context, backtestID string) error
    - Delete from Redis
    - Remove from index sets
    - Return error if any

func (s *RedisStorage) UpdateStatus(ctx context.Context, backtestID string, status string, progress float64) error
    - Get existing result
    - Update status and progress
    - Save back to Redis
    - Return error if any

func (s *RedisStorage) generateKey(backtestID string) string
func (s *RedisStorage) Close() error
```

---

#### 2.2.3 `file_storage.go` ✅

```go
package storage

type FileStorage struct {
    basePath string
    logger   logger.Logger
}

func NewFileStorage(basePath string, logger logger.Logger) *FileStorage

func (s *FileStorage) Save(ctx context.Context, result *models.BacktestResult) error
    - Generate file path based on ID and date
    - Serialize to JSON
    - Write to file
    - Return error if any

func (s *FileStorage) Get(ctx context.Context, backtestID string) (*models.BacktestResult, error)
    - Build file path
    - Read file
    - Deserialize
    - Return result or error

func (s *FileStorage) List(ctx context.Context, filters *ListFilters) ([]*models.BacktestResult, error)
    - Scan directory
    - Read matching files
    - Apply filters
    - Apply pagination
    - Return results

func (s *FileStorage) Delete(ctx context.Context, backtestID string) error
func (s *FileStorage) UpdateStatus(ctx context.Context, backtestID string, status string, progress float64) error
func (s *FileStorage) generatePath(backtestID string) string
func (s *FileStorage) Close() error
```

---

#### 2.2.4 `storage_test.go` ✅

```go
package storage

func TestSaveResult(t *testing.T)
func TestGetResult(t *testing.T)
func TestGetResultNotFound(t *testing.T)
func TestListResults(t *testing.T)
func TestListWithFilters(t *testing.T)
func TestDeleteResult(t *testing.T)
func TestUpdateStatus(t *testing.T)
func TestRedisTTL(t *testing.T)
func TestFileStoragePersistence(t *testing.T)
```

---

## Summary for Remaining Phases

Due to length constraints, I'll provide a high-level overview for Phases 3-7. Each phase follows similar detailed patterns:

### PHASE 3: Simulation (Week 3)
- Portfolio management (4 files)
- Market simulator (5 files)
- Strategy execution (4 files)
- All with comprehensive tests

### PHASE 4: Engine (Week 4)
- Backtest engine (5 files)
- Backtest manager (4 files)
- Performance analyzer (4 files)
- All with tests

### PHASE 5: API (Week 5)
- HTTP server (3 files)
- API handlers (5 files)
- Integration tests

### PHASE 6: Optimization (Week 6)
- Grid search optimizer (3 files)
- Optimization handlers
- Tests

### PHASE 7: Testing & Documentation (Week 7)
- Integration tests
- Performance tests
- Documentation
- Scripts

---

## Implementation Guidelines

### Code Style
- Follow Go conventions
- Use meaningful variable names
- Add comprehensive comments
- Error handling on every operation

### Testing
- Table-driven tests
- >80% coverage target
- Integration tests for critical flows
- Mock external dependencies

### Dependencies
- Use shared package models
- Follow existing service patterns
- Minimize external dependencies

---

**Total Estimated Lines**: ~11,000-14,000  
**Total Files**: 72  
**Total Tests**: 20+ test files, 65+ test functions  
**Implementation Time**: 7 weeks


