# Backtesting Service - Files, Methods & Tests Checklist

**Version**: 1.0.0  
**Date**: October 27, 2025  
**Status**: Implementation Checklist

## Overview

This document provides a detailed checklist of all files, methods, interfaces, and tests required for the Backtesting Service implementation.

---

## Phase 1: Foundation

### Configuration (`internal/config/`)

#### ✅ `config.go`
```go
// Structs
type Config struct {
    Service      ServiceConfig
    MarketData   MarketDataConfig
    Redis        RedisConfig
    Storage      StorageConfig
    Execution    ExecutionConfig
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
    Type           string  // "redis", "file", "s3"
    Path           string
    RetentionDays  int
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

// Functions
func Load() (*Config, error)
func (c *Config) Validate() error
```

**Tests** (`config_test.go`):
- [ ] `TestLoad()`
- [ ] `TestLoadWithEnv()`
- [ ] `TestValidate()`
- [ ] `TestValidateInvalid()`
- [ ] `TestGetEnvHelpers()`

---

#### ✅ `loader.go`
```go
// Functions
func loadFromEnv() (*Config, error)
func loadFromFile(path string) (*Config, error)
func setDefaults(config *Config)
func getEnv(key, defaultValue string) string
func getEnvAsInt(key string, defaultValue int) int
func getEnvAsFloat(key string, defaultValue float64) float64
func getEnvAsBool(key string, defaultValue bool) bool
func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration
```

---

#### ✅ `validation.go`
```go
// Functions
func validateServiceConfig(cfg *ServiceConfig) error
func validateMarketDataConfig(cfg *MarketDataConfig) error
func validateRedisConfig(cfg *RedisConfig) error
func validateStorageConfig(cfg *StorageConfig) error
func validateExecutionConfig(cfg *ExecutionConfig) error
```

---

### Logger (`internal/logger/`)

#### ✅ `logger.go`
```go
// Interface
type Logger interface {
    Debug(msg string, fields map[string]interface{})
    Info(msg string, fields map[string]interface{})
    Warn(msg string, fields map[string]interface{})
    Error(msg string, fields map[string]interface{})
    Fatal(msg string, fields map[string]interface{})
}

// Implementation
type ZerologLogger struct {
    logger zerolog.Logger
}

// Functions
func New(config *Config) Logger
func (l *ZerologLogger) Debug(msg string, fields map[string]interface{})
func (l *ZerologLogger) Info(msg string, fields map[string]interface{})
func (l *ZerologLogger) Warn(msg string, fields map[string]interface{})
func (l *ZerologLogger) Error(msg string, fields map[string]interface{})
func (l *ZerologLogger) Fatal(msg string, fields map[string]interface{})
```

---

### Metrics (`internal/metrics/`)

#### ✅ `prometheus.go`
```go
// Struct
type MetricsCollector struct {
    // Backtest metrics
    backtestsCreated       prometheus.Counter
    backtestsCompleted     *prometheus.CounterVec
    backtestsFailed        *prometheus.CounterVec
    backtestDuration       *prometheus.HistogramVec
    activeBacktests        prometheus.Gauge
    
    // Performance metrics
    eventsProcessed        *prometheus.CounterVec
    processingDuration     *prometheus.HistogramVec
    
    // System metrics
    serviceUptime          prometheus.Gauge
    serviceHealth          *prometheus.GaugeVec
}

// Functions
func NewMetricsCollector(serviceName string) *MetricsCollector
func (m *MetricsCollector) RecordBacktestCreated()
func (m *MetricsCollector) RecordBacktestCompleted(status string, duration time.Duration)
func (m *MetricsCollector) RecordBacktestFailed(reason string)
func (m *MetricsCollector) RecordActiveBacktests(count int)
func (m *MetricsCollector) RecordEventsProcessed(eventType string, count int)
func (m *MetricsCollector) RecordProcessingDuration(operation string, duration time.Duration)
func (m *MetricsCollector) RecordServiceUptime(uptime time.Duration)
func (m *MetricsCollector) RecordServiceHealth(healthy bool)
```

---

### Models (`internal/models/`)

#### ✅ `backtest.go`
```go
// Types
type BacktestStatus string

const (
    BacktestStatusPending    BacktestStatus = "pending"
    BacktestStatusRunning    BacktestStatus = "running"
    BacktestStatusCompleted  BacktestStatus = "completed"
    BacktestStatusFailed     BacktestStatus = "failed"
    BacktestStatusCancelled  BacktestStatus = "cancelled"
)

// Structs
type Backtest struct {
    ID          string
    Config      *BacktestConfig
    Status      BacktestStatus
    Progress    float64
    Result      *BacktestResult
    Error       string
    StartedAt   time.Time
    CompletedAt *time.Time
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// Functions
func NewBacktest(config *BacktestConfig) *Backtest
func (b *Backtest) Start()
func (b *Backtest) UpdateProgress(progress float64)
func (b *Backtest) Complete(result *BacktestResult)
func (b *Backtest) Fail(err error)
func (b *Backtest) Cancel()
func (b *Backtest) IsActive() bool
func (b *Backtest) IsCompleted() bool
func (b *Backtest) ToJSON() ([]byte, error)
func BacktestFromJSON(data []byte) (*Backtest, error)
```

---

#### ✅ `config.go`
```go
// Structs
type BacktestConfig struct {
    ID              string
    Name            string
    Description     string
    
    // Time range
    StartDate       time.Time
    EndDate         time.Time
    
    // Trading parameters
    Book            string
    InitialBalance  float64
    
    // Strategy configuration
    Strategy        string
    StrategyParams  map[string]interface{}
    
    // Execution settings
    SlippageModel   string
    SlippageValue   float64
    CommissionRate  float64
    
    // Data settings
    DataSource      string
    DataGranularity string
    
    // Optimization
    OptimizationMode bool
    ParamRanges      map[string]Range
    
    // Metadata
    CreatedAt       time.Time
    CreatedBy       string
}

type Range struct {
    Min  float64
    Max  float64
    Step float64
}

// Functions
func NewBacktestConfig() *BacktestConfig
func (c *BacktestConfig) Validate() error
func (c *BacktestConfig) GetDuration() time.Duration
func (c *BacktestConfig) Clone() *BacktestConfig
```

---

#### ✅ `result.go`
```go
// Structs
type BacktestResult struct {
    ID            string
    BacktestID    string
    ConfigID      string
    
    Status        string
    Progress      float64
    Error         string
    
    Summary       *PerformanceSummary
    Trades        []Trade
    EquityCurve   []EquityPoint
    Positions     []Position
    
    StartedAt     time.Time
    CompletedAt   *time.Time
    Duration      time.Duration
    
    Metadata      map[string]interface{}
}

type PerformanceSummary struct {
    // Returns
    TotalReturn          float64
    TotalReturnPercent   float64
    AnnualizedReturn     float64
    
    // Risk metrics
    Volatility           float64
    SharpeRatio          float64
    SortinoRatio         float64
    MaxDrawdown          float64
    MaxDrawdownPercent   float64
    
    // Trade statistics
    TotalTrades          int
    WinningTrades        int
    LosingTrades         int
    WinRate              float64
    AverageWin           float64
    AverageLoss          float64
    ProfitFactor         float64
    
    // Position metrics
    AverageHoldingTime   time.Duration
    MaxPosition          float64
    
    // P&L
    GrossProfitLoss      float64
    NetProfitLoss        float64
    TotalCommissions     float64
    
    // Portfolio
    FinalBalance         float64
    PeakBalance          float64
}

type EquityPoint struct {
    Timestamp time.Time
    Balance   float64
    Equity    float64
    Return    float64
}

type Trade struct {
    ID                string
    EntryTime         time.Time
    ExitTime          time.Time
    Book              string
    Side              string
    EntryPrice        float64
    ExitPrice         float64
    Amount            float64
    ProfitLoss        float64
    ProfitLossPercent float64
    Commission        float64
    Slippage          float64
    HoldingTime       time.Duration
}

// Functions
func NewBacktestResult(backtestID, configID string) *BacktestResult
func (r *BacktestResult) AddTrade(trade Trade)
func (r *BacktestResult) AddEquityPoint(point EquityPoint)
func (r *BacktestResult) SetSummary(summary *PerformanceSummary)
func (r *BacktestResult) ToJSON() ([]byte, error)
```

---

#### ✅ `portfolio.go`
```go
// Structs
type Position struct {
    Book         string
    Size         float64
    AveragePrice float64
    CurrentPrice float64
    UnrealizedPL float64
    Timestamp    time.Time
}

// Functions
func NewPosition(book string) *Position
func (p *Position) AddSize(amount, price float64)
func (p *Position) ReduceSize(amount, price float64) float64
func (p *Position) UpdateCurrentPrice(price float64)
func (p *Position) CalculateUnrealizedPL() float64
func (p *Position) IsEmpty() bool
```

---

#### ✅ `event.go`
```go
// Structs
type MarketEvent struct {
    EventType string      // "trade", "ticker", "orderbook"
    Timestamp time.Time
    Book      string
    Data      interface{} // *TradeEvent, *TickerEvent, or *OrderBookEvent
}

// Functions
func NewTradeEvent(trade *models.TradeEvent) *MarketEvent
func NewTickerEvent(ticker interface{}) *MarketEvent
func (e *MarketEvent) GetTrade() *models.TradeEvent
func (e *MarketEvent) IsTradeEvent() bool
```

---

#### ✅ `validation.go`
```go
// Functions
func ValidateBacktestConfig(config *BacktestConfig) error
func ValidateTimeRange(start, end time.Time) error
func ValidateStrategy(strategy string, params map[string]interface{}) error
func ValidateBook(book string) error
func ValidateBalance(balance float64) error
```

**Tests** (`models_test.go`):
- [ ] `TestBacktestLifecycle()`
- [ ] `TestBacktestConfigValidation()`
- [ ] `TestBacktestResultCreation()`
- [ ] `TestPositionTracking()`
- [ ] `TestMarketEventCreation()`
- [ ] `TestValidationFunctions()`

---

### Main Application (`cmd/`)

#### ✅ `main.go`
```go
// Structs
type Application struct {
    logger           logger.Logger
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

// Functions
func NewApplication() (*Application, error)
func (app *Application) Start() error
func (app *Application) Stop() error
func (app *Application) Run() error
func main()
```

---

## Phase 2: Data Layer

### Data Providers (`internal/data/`)

#### ✅ `provider.go`
```go
// Interface
type DataProvider interface {
    LoadHistoricalData(ctx context.Context, req *DataRequest) ([]MarketEvent, error)
    StreamData(ctx context.Context, req *DataRequest) (<-chan MarketEvent, error)
    GetDataRange(ctx context.Context, book string) (*DateRange, error)
    Close() error
}

type DataRequest struct {
    Book        string
    StartDate   time.Time
    EndDate     time.Time
    EventTypes  []string  // ["trade", "ticker", "orderbook"]
    Granularity string    // "tick", "1m", "5m"
    Limit       int
}

type DateRange struct {
    FirstDate time.Time
    LastDate  time.Time
}
```

---

#### ✅ `market_data_provider.go`
```go
// Struct
type MarketDataProvider struct {
    baseURL    string
    httpClient *http.Client
    cache      *Cache
    logger     logger.Logger
}

// Functions
func NewMarketDataProvider(baseURL string, logger logger.Logger) *MarketDataProvider
func (p *MarketDataProvider) LoadHistoricalData(ctx context.Context, req *DataRequest) ([]MarketEvent, error)
func (p *MarketDataProvider) StreamData(ctx context.Context, req *DataRequest) (<-chan MarketEvent, error)
func (p *MarketDataProvider) GetDataRange(ctx context.Context, book string) (*DateRange, error)
func (p *MarketDataProvider) fetchTrades(ctx context.Context, book string, start, end time.Time, limit int) ([]models.TradeEvent, error)
func (p *MarketDataProvider) fetchTickers(ctx context.Context, book string, start, end time.Time) ([]interface{}, error)
func (p *MarketDataProvider) Close() error
```

**Tests** (`provider_test.go`):
- [ ] `TestLoadHistoricalData()`
- [ ] `TestStreamData()`
- [ ] `TestGetDataRange()`
- [ ] `TestFetchTrades()`
- [ ] `TestErrorHandling()`
- [ ] `TestRetryLogic()`

---

#### ✅ `file_provider.go`
```go
// Struct
type FileProvider struct {
    basePath string
    logger   logger.Logger
}

// Functions
func NewFileProvider(basePath string, logger logger.Logger) *FileProvider
func (p *FileProvider) LoadHistoricalData(ctx context.Context, req *DataRequest) ([]MarketEvent, error)
func (p *FileProvider) StreamData(ctx context.Context, req *DataRequest) (<-chan MarketEvent, error)
func (p *FileProvider) GetDataRange(ctx context.Context, book string) (*DateRange, error)
func (p *FileProvider) readDataFile(filePath string) ([]MarketEvent, error)
func (p *FileProvider) Close() error
```

**Tests** (`file_provider_test.go`):
- [ ] `TestLoadFromFile()`
- [ ] `TestStreamFromFile()`
- [ ] `TestInvalidFile()`

---

#### ✅ `cache.go`
```go
// Struct
type Cache struct {
    redis      *redis.Client
    ttl        time.Duration
    logger     logger.Logger
}

// Functions
func NewCache(redisClient *redis.Client, ttl time.Duration, logger logger.Logger) *Cache
func (c *Cache) Get(ctx context.Context, key string) ([]MarketEvent, error)
func (c *Cache) Set(ctx context.Context, key string, events []MarketEvent) error
func (c *Cache) Delete(ctx context.Context, key string) error
func (c *Cache) generateKey(book string, start, end time.Time) string
```

---

### Storage (`internal/storage/`)

#### ✅ `storage.go`
```go
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

#### ✅ `redis_storage.go`
```go
// Struct
type RedisStorage struct {
    client *redis.Client
    logger logger.Logger
}

// Functions
func NewRedisStorage(client *redis.Client, logger logger.Logger) *RedisStorage
func (s *RedisStorage) Save(ctx context.Context, result *models.BacktestResult) error
func (s *RedisStorage) Get(ctx context.Context, backtestID string) (*models.BacktestResult, error)
func (s *RedisStorage) List(ctx context.Context, filters *ListFilters) ([]*models.BacktestResult, error)
func (s *RedisStorage) Delete(ctx context.Context, backtestID string) error
func (s *RedisStorage) UpdateStatus(ctx context.Context, backtestID string, status string, progress float64) error
func (s *RedisStorage) Close() error
func (s *RedisStorage) generateKey(backtestID string) string
```

**Tests** (`storage_test.go`):
- [ ] `TestSaveResult()`
- [ ] `TestGetResult()`
- [ ] `TestListResults()`
- [ ] `TestDeleteResult()`
- [ ] `TestUpdateStatus()`

---

#### ✅ `file_storage.go`
```go
// Struct
type FileStorage struct {
    basePath string
    logger   logger.Logger
}

// Functions
func NewFileStorage(basePath string, logger logger.Logger) *FileStorage
func (s *FileStorage) Save(ctx context.Context, result *models.BacktestResult) error
func (s *FileStorage) Get(ctx context.Context, backtestID string) (*models.BacktestResult, error)
func (s *FileStorage) List(ctx context.Context, filters *ListFilters) ([]*models.BacktestResult, error)
func (s *FileStorage) Delete(ctx context.Context, backtestID string) error
func (s *FileStorage) UpdateStatus(ctx context.Context, backtestID string, status string, progress float64) error
func (s *FileStorage) Close() error
func (s *FileStorage) generatePath(backtestID string) string
```

---

## Phase 3: Simulation Engine

### Portfolio (`internal/portfolio/`)

#### ✅ `virtual_portfolio.go`
```go
// Struct
type VirtualPortfolio struct {
    ID               string
    InitialBalance   float64
    CurrentBalance   float64
    Positions        map[string]*models.Position
    TotalPL          float64
    TotalCommissions float64
    Trades           []models.Trade
    PeakBalance      float64
    DrawdownAmount   float64
    mu               sync.RWMutex
}

// Functions
func NewVirtualPortfolio(id string, initialBalance float64) *VirtualPortfolio
func (p *VirtualPortfolio) GetBalance() float64
func (p *VirtualPortfolio) GetPosition(book string) (*models.Position, error)
func (p *VirtualPortfolio) GetAllPositions() []*models.Position
func (p *VirtualPortfolio) ExecuteTrade(trade *models.Trade) error
func (p *VirtualPortfolio) CalculateEquity(prices map[string]float64) float64
func (p *VirtualPortfolio) GetTrades() []models.Trade
func (p *VirtualPortfolio) GetSummary() *PortfolioSummary
func (p *VirtualPortfolio) Clone() *VirtualPortfolio
```

**Tests** (`portfolio_test.go`):
- [ ] `TestNewPortfolio()`
- [ ] `TestExecuteBuyTrade()`
- [ ] `TestExecuteSellTrade()`
- [ ] `TestPositionTracking()`
- [ ] `TestEquityCalculation()`
- [ ] `TestCommissions()`
- [ ] `TestDrawdownTracking()`
- [ ] `TestConcurrentAccess()`

---

#### ✅ `position.go`
```go
// Functions
func openPosition(portfolio *VirtualPortfolio, book string, side string, amount, price, commission float64) error
func closePosition(portfolio *VirtualPortfolio, book string, side string, amount, price, commission float64) error
func updatePosition(portfolio *VirtualPortfolio, book string, amount, price float64) error
func calculatePositionPL(position *models.Position, exitPrice float64) float64
```

---

#### ✅ `balance.go`
```go
// Functions
func updateBalance(portfolio *VirtualPortfolio, amount float64)
func recordCommission(portfolio *VirtualPortfolio, commission float64)
func updatePeakBalance(portfolio *VirtualPortfolio)
func calculateDrawdown(portfolio *VirtualPortfolio) float64
```

---

### Simulator (`internal/simulator/`)

#### ✅ `simulator.go`
```go
// Interface
type MarketSimulator interface {
    Initialize(ctx context.Context, config *SimulatorConfig) error
    ProcessEvent(event *models.MarketEvent) error
    ExecuteOrder(order *models.Order) (*OrderExecution, error)
    GetCurrentPrice(book string) (float64, error)
    GetOrderBook(book string) (*OrderBook, error)
    GetState() *MarketState
    Reset() error
}

// Struct
type Simulator struct {
    config       *SimulatorConfig
    currentPrices map[string]float64
    orderBooks    map[string]*OrderBook
    lastUpdate    time.Time
    logger        logger.Logger
    mu            sync.RWMutex
}

type SimulatorConfig struct {
    SlippageModel   string
    SlippageValue   float64
    CommissionRate  float64
    EnableOrderBook bool
}

type OrderExecution struct {
    OrderID       string
    ExecutedPrice float64
    ExecutedAmount float64
    Commission    float64
    Slippage      float64
    Timestamp     time.Time
    Success       bool
    Error         string
}

type MarketState struct {
    Timestamp     time.Time
    Prices        map[string]float64
    OrderBooks    map[string]*OrderBook
}

// Functions
func NewSimulator(config *SimulatorConfig, logger logger.Logger) *Simulator
func (s *Simulator) Initialize(ctx context.Context, config *SimulatorConfig) error
func (s *Simulator) ProcessEvent(event *models.MarketEvent) error
func (s *Simulator) ExecuteOrder(order *models.Order) (*OrderExecution, error)
func (s *Simulator) GetCurrentPrice(book string) (float64, error)
func (s *Simulator) GetOrderBook(book string) (*OrderBook, error)
func (s *Simulator) GetState() *MarketState
func (s *Simulator) Reset() error
```

**Tests** (`simulator_test.go`):
- [ ] `TestNewSimulator()`
- [ ] `TestProcessTradeEvent()`
- [ ] `TestExecuteMarketOrder()`
- [ ] `TestExecuteLimitOrder()`
- [ ] `TestSlippageCalculation()`
- [ ] `TestCommissionCalculation()`
- [ ] `TestOrderBookUpdates()`
- [ ] `TestGetCurrentPrice()`

---

#### ✅ `execution.go`
```go
// Functions
func executeMarketOrder(sim *Simulator, order *models.Order) (*OrderExecution, error)
func executeLimitOrder(sim *Simulator, order *models.Order) (*OrderExecution, error)
func calculateExecutionPrice(basePrice float64, side string, slippage float64) float64
func calculateCommission(amount, price, rate float64) float64
func validateOrder(order *models.Order) error
```

---

#### ✅ `slippage.go`
```go
// Types
type SlippageModel interface {
    Calculate(order *models.Order, marketPrice float64) float64
}

// Implementations
type NoSlippage struct{}
type FixedSlippage struct{ Value float64 }
type PercentageSlippage struct{ Percentage float64 }
type VolumeBasedSlippage struct{ VolumeMap map[float64]float64 }

// Functions
func NewSlippageModel(modelType string, value float64) SlippageModel
func (n *NoSlippage) Calculate(order *models.Order, marketPrice float64) float64
func (f *FixedSlippage) Calculate(order *models.Order, marketPrice float64) float64
func (p *PercentageSlippage) Calculate(order *models.Order, marketPrice float64) float64
func (v *VolumeBasedSlippage) Calculate(order *models.Order, marketPrice float64) float64
```

**Tests** (`slippage_test.go`):
- [ ] `TestNoSlippage()`
- [ ] `TestFixedSlippage()`
- [ ] `TestPercentageSlippage()`
- [ ] `TestVolumeBasedSlippage()`

---

#### ✅ `orderbook.go`
```go
// Structs
type OrderBook struct {
    Book      string
    Timestamp time.Time
    Bids      []PriceLevel
    Asks      []PriceLevel
    mu        sync.RWMutex
}

type PriceLevel struct {
    Price  float64
    Amount float64
}

// Functions
func NewOrderBook(book string) *OrderBook
func (ob *OrderBook) Update(bids, asks []PriceLevel, timestamp time.Time)
func (ob *OrderBook) GetBestBid() (float64, float64, error)
func (ob *OrderBook) GetBestAsk() (float64, float64, error)
func (ob *OrderBook) GetMidPrice() (float64, error)
func (ob *OrderBook) GetSpread() float64
func (ob *OrderBook) CanFillOrder(side string, amount float64) bool
```

---

### Strategy (`internal/strategy/`)

#### ✅ `strategy.go`
```go
// Interface (mirrors strategy-executor)
type Strategy interface {
    Initialize(config map[string]interface{}) error
    OnTrade(trade *models.TradeEvent) (*Signal, error)
    OnTicker(ticker interface{}) (*Signal, error)
    OnOrderBook(orderBook interface{}) (*Signal, error)
    GetName() string
    Reset() error
}

type Signal struct {
    Type      string  // "BUY", "SELL", "HOLD"
    Book      string
    Price     float64
    Amount    float64
    Confidence float64
    Metadata  map[string]interface{}
    Timestamp time.Time
}
```

---

#### ✅ `executor.go`
```go
// Struct
type StrategyExecutor struct {
    strategy Strategy
    logger   logger.Logger
}

// Functions
func NewStrategyExecutor(strategy Strategy, logger logger.Logger) *StrategyExecutor
func (e *StrategyExecutor) ProcessEvent(event *models.MarketEvent) (*Signal, error)
func (e *StrategyExecutor) Reset() error
```

**Tests** (`strategy_test.go`):
- [ ] `TestStrategyInitialization()`
- [ ] `TestProcessTradeEvent()`
- [ ] `TestSignalGeneration()`
- [ ] `TestStrategyReset()`

---

#### ✅ `factory.go`
```go
// Functions
func CreateStrategy(strategyType string, params map[string]interface{}) (Strategy, error)
func RegisterStrategy(name string, constructor func(params map[string]interface{}) (Strategy, error))
func GetAvailableStrategies() []string
```

---

## Phase 4: Backtest Engine

### Engine (`internal/engine/`)

#### ✅ `engine.go`
```go
// Interface
type BacktestEngine interface {
    Run(ctx context.Context, config *models.BacktestConfig) (*models.BacktestResult, error)
    Cancel(backtestID string) error
    GetProgress(backtestID string) (float64, error)
}

// Implementation
type Engine struct {
    dataProvider     data.DataProvider
    resultStorage    storage.ResultStorage
    logger           logger.Logger
    metricsCollector *metrics.MetricsCollector
    
    runningBacktests map[string]*runningBacktest
    mu               sync.RWMutex
}

type runningBacktest struct {
    ID       string
    Config   *models.BacktestConfig
    Progress float64
    Cancel   context.CancelFunc
}

// Functions
func NewEngine(
    dataProvider data.DataProvider,
    resultStorage storage.ResultStorage,
    logger logger.Logger,
    metricsCollector *metrics.MetricsCollector,
) *Engine

func (e *Engine) Run(ctx context.Context, config *models.BacktestConfig) (*models.BacktestResult, error)
func (e *Engine) Cancel(backtestID string) error
func (e *Engine) GetProgress(backtestID string) (float64, error)
```

**Tests** (`engine_test.go`):
- [ ] `TestRunBacktest()`
- [ ] `TestCancelBacktest()`
- [ ] `TestGetProgress()`
- [ ] `TestConcurrentBacktests()`
- [ ] `TestErrorHandling()`

---

#### ✅ `runner.go`
```go
// Struct
type BacktestRunner struct {
    config           *models.BacktestConfig
    dataProvider     data.DataProvider
    simulator        simulator.MarketSimulator
    strategyExecutor *strategy.StrategyExecutor
    portfolio        *portfolio.VirtualPortfolio
    analyzer         *analyzer.PerformanceAnalyzer
    logger           logger.Logger
    
    progressCallback func(float64)
    cancelChan       <-chan struct{}
}

// Functions
func NewBacktestRunner(
    config *models.BacktestConfig,
    dataProvider data.DataProvider,
    logger logger.Logger,
) (*BacktestRunner, error)

func (r *BacktestRunner) Initialize(ctx context.Context) error
func (r *BacktestRunner) Execute(ctx context.Context) (*models.BacktestResult, error)
func (r *BacktestRunner) SetProgressCallback(callback func(float64))
func (r *BacktestRunner) cleanup()
```

---

#### ✅ `event_loop.go`
```go
// Functions
func (r *BacktestRunner) runEventLoop(ctx context.Context, events []models.MarketEvent) error
func (r *BacktestRunner) processEvent(event *models.MarketEvent) error
func (r *BacktestRunner) handleSignal(signal *strategy.Signal) error
func (r *BacktestRunner) executeOrder(signal *strategy.Signal) error
func (r *BacktestRunner) updatePortfolio(execution *simulator.OrderExecution) error
func (r *BacktestRunner) recordTrade(execution *simulator.OrderExecution) error
func (r *BacktestRunner) recordEquityPoint(timestamp time.Time) error
```

---

#### ✅ `coordinator.go`
```go
// Functions
func coordinateComponents(runner *BacktestRunner) error
func synchronizeState(runner *BacktestRunner) error
func validateState(runner *BacktestRunner) error
func handleError(runner *BacktestRunner, err error) error
```

---

### Manager (`internal/manager/`)

#### ✅ `backtest_manager.go`
```go
// Struct
type BacktestManager struct {
    engine           engine.BacktestEngine
    queue            *BacktestQueue
    storage          storage.ResultStorage
    logger           logger.Logger
    metricsCollector *metrics.MetricsCollector
    
    maxConcurrent    int
    runningBacktests map[string]*models.Backtest
    mu               sync.RWMutex
}

// Functions
func NewBacktestManager(
    engine engine.BacktestEngine,
    storage storage.ResultStorage,
    maxConcurrent int,
    logger logger.Logger,
    metricsCollector *metrics.MetricsCollector,
) *BacktestManager

func (m *BacktestManager) CreateBacktest(config *models.BacktestConfig) (*models.Backtest, error)
func (m *BacktestManager) StartBacktest(backtestID string) error
func (m *BacktestManager) CancelBacktest(backtestID string) error
func (m *BacktestManager) GetBacktest(backtestID string) (*models.Backtest, error)
func (m *BacktestManager) ListBacktests(filters *storage.ListFilters) ([]*models.Backtest, error)
func (m *BacktestManager) GetBacktestResult(backtestID string) (*models.BacktestResult, error)
func (m *BacktestManager) Start(ctx context.Context) error
func (m *BacktestManager) Stop() error
```

**Tests** (`manager_test.go`):
- [ ] `TestCreateBacktest()`
- [ ] `TestStartBacktest()`
- [ ] `TestCancelBacktest()`
- [ ] `TestGetBacktest()`
- [ ] `TestListBacktests()`
- [ ] `TestMaxConcurrent()`

---

#### ✅ `queue.go`
```go
// Struct
type BacktestQueue struct {
    queue    []string  // Backtest IDs
    capacity int
    mu       sync.RWMutex
}

// Functions
func NewBacktestQueue(capacity int) *BacktestQueue
func (q *BacktestQueue) Enqueue(backtestID string) error
func (q *BacktestQueue) Dequeue() (string, error)
func (q *BacktestQueue) Size() int
func (q *BacktestQueue) IsFull() bool
func (q *BacktestQueue) IsEmpty() bool
func (q *BacktestQueue) Remove(backtestID string) bool
```

---

#### ✅ `state.go`
```go
// Functions
func (m *BacktestManager) trackBacktest(backtest *models.Backtest)
func (m *BacktestManager) untrackBacktest(backtestID string)
func (m *BacktestManager) getTrackedBacktest(backtestID string) (*models.Backtest, error)
func (m *BacktestManager) getRunningCount() int
func (m *BacktestManager) canStartNewBacktest() bool
```

---

### Analyzer (`internal/analyzer/`)

#### ✅ `analyzer.go`
```go
// Interface
type PerformanceAnalyzer interface {
    Analyze(portfolio *portfolio.VirtualPortfolio, trades []models.Trade, equityCurve []models.EquityPoint) (*models.PerformanceSummary, error)
    CalculateMetrics(portfolio *portfolio.VirtualPortfolio) (*models.PerformanceSummary, error)
    GenerateReport(result *models.BacktestResult) (string, error)
}

// Implementation
type Analyzer struct {
    logger logger.Logger
}

// Functions
func NewAnalyzer(logger logger.Logger) *Analyzer
func (a *Analyzer) Analyze(portfolio *portfolio.VirtualPortfolio, trades []models.Trade, equityCurve []models.EquityPoint) (*models.PerformanceSummary, error)
func (a *Analyzer) CalculateMetrics(portfolio *portfolio.VirtualPortfolio) (*models.PerformanceSummary, error)
func (a *Analyzer) GenerateReport(result *models.BacktestResult) (string, error)
```

**Tests** (`analyzer_test.go`):
- [ ] `TestCalculateReturns()`
- [ ] `TestCalculateSharpeRatio()`
- [ ] `TestCalculateSortinoRatio()`
- [ ] `TestCalculateMaxDrawdown()`
- [ ] `TestCalculateWinRate()`
- [ ] `TestCalculateProfitFactor()`
- [ ] `TestGenerateReport()`

---

#### ✅ `metrics.go`
```go
// Functions
func calculateReturns(initialBalance, finalBalance float64, days int) (total, annualized float64)
func calculateVolatility(returns []float64) float64
func calculateSharpeRatio(returns []float64, riskFreeRate float64) float64
func calculateSortinoRatio(returns []float64, targetReturn float64) float64
func calculateMaxDrawdown(equityCurve []models.EquityPoint) (float64, float64)
func calculateTradeStatistics(trades []models.Trade) map[string]float64
func calculateWinRate(trades []models.Trade) float64
func calculateProfitFactor(trades []models.Trade) float64
func calculateAverageHoldingTime(trades []models.Trade) time.Duration
```

**Tests** (`metrics_test.go`):
- [ ] `TestCalculateReturns()`
- [ ] `TestCalculateVolatility()`
- [ ] `TestCalculateSharpeRatio()`
- [ ] `TestCalculateSortinoRatio()`
- [ ] `TestCalculateMaxDrawdown()`
- [ ] `TestCalculateWinRate()`
- [ ] `TestCalculateProfitFactor()`

---

#### ✅ `report.go`
```go
// Functions
func generateTextReport(result *models.BacktestResult) string
func generateJSONReport(result *models.BacktestResult) ([]byte, error)
func generateHTMLReport(result *models.BacktestResult) string
func formatMetric(name string, value interface{}) string
func createEquityCurveChart(equityCurve []models.EquityPoint) string
func createTradeDistributionChart(trades []models.Trade) string
```

---

## Phase 5: API Layer

### Server (`internal/server/`)

#### ✅ `http_server.go`
```go
// Struct
type HTTPServer struct {
    config           *config.ServiceConfig
    router           *mux.Router
    server           *http.Server
    healthManager    *health.HealthManager
    metricsCollector *metrics.MetricsCollector
    logger           logger.Logger
}

// Functions
func NewHTTPServer(
    config *config.ServiceConfig,
    handler *api.Handler,
    healthManager *health.HealthManager,
    metricsCollector *metrics.MetricsCollector,
    logger logger.Logger,
) *HTTPServer

func (s *HTTPServer) Start(ctx context.Context) error
func (s *HTTPServer) Stop(ctx context.Context) error
func (s *HTTPServer) setupRoutes(handler *api.Handler)
func (s *HTTPServer) setupMiddleware()
```

---

#### ✅ `routes.go`
```go
// Functions
func (s *HTTPServer) registerHealthRoutes()
func (s *HTTPServer) registerBacktestRoutes(handler *api.Handler)
func (s *HTTPServer) registerResultRoutes(handler *api.Handler)
func (s *HTTPServer) registerOptimizationRoutes(handler *api.Handler)
func (s *HTTPServer) registerMetricsRoutes()
```

---

#### ✅ `middleware.go`
```go
// Functions
func loggingMiddleware(logger logger.Logger) mux.MiddlewareFunc
func metricsMiddleware(collector *metrics.MetricsCollector) mux.MiddlewareFunc
func recoveryMiddleware(logger logger.Logger) mux.MiddlewareFunc
func corsMiddleware() mux.MiddlewareFunc
func timeoutMiddleware(timeout time.Duration) mux.MiddlewareFunc
```

---

### API Handlers (`internal/api/`)

#### ✅ `handlers.go`
```go
// Struct
type Handler struct {
    manager          *manager.BacktestManager
    logger           logger.Logger
    metricsCollector *metrics.MetricsCollector
}

// Functions
func NewHandler(
    manager *manager.BacktestManager,
    logger logger.Logger,
    metricsCollector *metrics.MetricsCollector,
) *Handler

func (h *Handler) HealthCheck(w http.ResponseWriter, r *http.Request)
func (h *Handler) GetStatus(w http.ResponseWriter, r *http.Request)
```

---

#### ✅ `backtest_handlers.go`
```go
// Functions
func (h *Handler) CreateBacktest(w http.ResponseWriter, r *http.Request)
func (h *Handler) GetBacktest(w http.ResponseWriter, r *http.Request)
func (h *Handler) ListBacktests(w http.ResponseWriter, r *http.Request)
func (h *Handler) StartBacktest(w http.ResponseWriter, r *http.Request)
func (h *Handler) CancelBacktest(w http.ResponseWriter, r *http.Request)
func (h *Handler) DeleteBacktest(w http.ResponseWriter, r *http.Request)

// Request/Response structs
type CreateBacktestRequest struct {
    Name            string
    Description     string
    StartDate       string
    EndDate         string
    Book            string
    InitialBalance  float64
    Strategy        string
    StrategyParams  map[string]interface{}
    SlippageModel   string
    SlippageValue   float64
    CommissionRate  float64
}

type BacktestResponse struct {
    ID          string
    Name        string
    Status      string
    Progress    float64
    CreatedAt   time.Time
    StartedAt   *time.Time
    CompletedAt *time.Time
}
```

**Tests** (`handlers_test.go`):
- [ ] `TestCreateBacktest()`
- [ ] `TestGetBacktest()`
- [ ] `TestListBacktests()`
- [ ] `TestStartBacktest()`
- [ ] `TestCancelBacktest()`
- [ ] `TestInvalidRequests()`

---

#### ✅ `result_handlers.go`
```go
// Functions
func (h *Handler) GetBacktestResult(w http.ResponseWriter, r *http.Request)
func (h *Handler) GetBacktestSummary(w http.ResponseWriter, r *http.Request)
func (h *Handler) GetBacktestTrades(w http.ResponseWriter, r *http.Request)
func (h *Handler) GetBacktestEquityCurve(w http.ResponseWriter, r *http.Request)
func (h *Handler) DownloadBacktestReport(w http.ResponseWriter, r *http.Request)
func (h *Handler) ExportBacktestData(w http.ResponseWriter, r *http.Request)
```

---

#### ✅ `optimization_handlers.go`
```go
// Functions
func (h *Handler) CreateOptimization(w http.ResponseWriter, r *http.Request)
func (h *Handler) GetOptimization(w http.ResponseWriter, r *http.Request)
func (h *Handler) GetOptimizationResults(w http.ResponseWriter, r *http.Request)
func (h *Handler) CancelOptimization(w http.ResponseWriter, r *http.Request)

// Request struct
type CreateOptimizationRequest struct {
    Name            string
    StartDate       string
    EndDate         string
    Book            string
    InitialBalance  float64
    Strategy        string
    ParamRanges     map[string]models.Range
    OptimizationMetric string  // "sharpe_ratio", "total_return", etc.
}
```

---

#### ✅ `response.go`
```go
// Types
type APIResponse struct {
    Success bool        `json:"success"`
    Data    interface{} `json:"data,omitempty"`
    Error   *APIError   `json:"error,omitempty"`
}

type APIError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Details string `json:"details,omitempty"`
}

// Functions
func sendJSON(w http.ResponseWriter, statusCode int, data interface{})
func sendError(w http.ResponseWriter, statusCode int, code, message string)
func sendSuccess(w http.ResponseWriter, data interface{})
func parseRequest(r *http.Request, v interface{}) error
```

---

## Phase 6: Optimization

### Optimizer (`internal/optimizer/`)

#### ✅ `optimizer.go`
```go
// Interface
type Optimizer interface {
    Optimize(ctx context.Context, config *OptimizationConfig) (*OptimizationResult, error)
    Cancel(optimizationID string) error
    GetProgress(optimizationID string) (float64, error)
}

// Structs
type OptimizationConfig struct {
    ID              string
    Name            string
    BacktestConfig  *models.BacktestConfig
    ParamRanges     map[string]models.Range
    OptimizationMetric string
    MaxWorkers      int
}

type OptimizationResult struct {
    ID              string
    ConfigID        string
    Status          string
    Progress        float64
    BestResult      *models.BacktestResult
    BestParams      map[string]interface{}
    AllResults      []*models.BacktestResult
    CompletedAt     *time.Time
}
```

---

#### ✅ `grid_search.go`
```go
// Struct
type GridSearchOptimizer struct {
    engine           engine.BacktestEngine
    logger           logger.Logger
    metricsCollector *metrics.MetricsCollector
    
    runningOpts      map[string]*runningOptimization
    mu               sync.RWMutex
}

type runningOptimization struct {
    ID       string
    Config   *OptimizationConfig
    Progress float64
    Cancel   context.CancelFunc
}

// Functions
func NewGridSearchOptimizer(
    engine engine.BacktestEngine,
    logger logger.Logger,
    metricsCollector *metrics.MetricsCollector,
) *GridSearchOptimizer

func (o *GridSearchOptimizer) Optimize(ctx context.Context, config *OptimizationConfig) (*OptimizationResult, error)
func (o *GridSearchOptimizer) Cancel(optimizationID string) error
func (o *GridSearchOptimizer) GetProgress(optimizationID string) (float64, error)
func (o *GridSearchOptimizer) generateParameterCombinations(ranges map[string]models.Range) []map[string]interface{}
func (o *GridSearchOptimizer) runParameterCombination(ctx context.Context, params map[string]interface{}, baseConfig *models.BacktestConfig) (*models.BacktestResult, error)
func (o *GridSearchOptimizer) selectBestResult(results []*models.BacktestResult, metric string) *models.BacktestResult
```

**Tests** (`optimizer_test.go`):
- [ ] `TestGenerateParameterCombinations()`
- [ ] `TestOptimizeSingleParameter()`
- [ ] `TestOptimizeMultipleParameters()`
- [ ] `TestSelectBestResult()`
- [ ] `TestCancelOptimization()`
- [ ] `TestConcurrentOptimizations()`

---

#### ✅ `genetic.go` (Future implementation)
```go
// Struct
type GeneticOptimizer struct {
    // Future implementation
}

// Functions
func NewGeneticOptimizer(...) *GeneticOptimizer
func (o *GeneticOptimizer) Optimize(ctx context.Context, config *OptimizationConfig) (*OptimizationResult, error)
```

---

## Phase 7: Testing

### Integration Tests (`test/integration/`)

#### ✅ `backtest_test.go`
```go
// Tests
func TestCompleteBacktestFlow(t *testing.T)
func TestBacktestWithDifferentStrategies(t *testing.T)
func TestBacktestWithHistoricalData(t *testing.T)
func TestBacktestCancellation(t *testing.T)
func TestConcurrentBacktests(t *testing.T)
func TestBacktestResultPersistence(t *testing.T)
```

---

#### ✅ `api_test.go`
```go
// Tests
func TestAPICreateBacktest(t *testing.T)
func TestAPIGetBacktest(t *testing.T)
func TestAPIListBacktests(t *testing.T)
func TestAPIStartBacktest(t *testing.T)
func TestAPICancelBacktest(t *testing.T)
func TestAPIGetResult(t *testing.T)
func TestAPIOptimization(t *testing.T)
func TestAPIErrorHandling(t *testing.T)
```

---

### Test Fixtures (`test/fixtures/`)

#### Files needed:
- [ ] `sample_data.json` - Sample market data (1000 trade events)
- [ ] `expected_results.json` - Expected backtest results for validation
- [ ] `strategy_configs/basic.json` - Basic strategy config
- [ ] `strategy_configs/trend.json` - Trend strategy config
- [ ] `strategy_configs/arbitrage.json` - Arbitrage strategy config
- [ ] `market_conditions/bull_market.json` - Bull market data
- [ ] `market_conditions/bear_market.json` - Bear market data
- [ ] `market_conditions/sideways.json` - Sideways market data

---

## Summary Statistics

### Implementation Totals

| Component | Files | Estimated Lines | Tests |
|-----------|-------|-----------------|-------|
| **Phase 1: Foundation** | 15 | 2,000-2,500 | 12 |
| **Phase 2: Data Layer** | 10 | 1,500-2,000 | 8 |
| **Phase 3: Simulation** | 13 | 2,000-2,500 | 10 |
| **Phase 4: Engine** | 11 | 1,800-2,200 | 8 |
| **Phase 5: API** | 9 | 1,200-1,500 | 6 |
| **Phase 6: Optimization** | 3 | 800-1,000 | 6 |
| **Phase 7: Testing** | 10 | 1,500-2,000 | 15 |
| **Total** | **71** | **10,800-13,700** | **65** |

### Test Coverage Targets

- **Unit Tests**: >80% coverage
- **Integration Tests**: All critical flows
- **Benchmark Tests**: Performance validation
- **Total Test Files**: 20+
- **Total Test Functions**: 65+

---

**Document Version**: 1.0.0  
**Last Updated**: October 27, 2025  
**Status**: Ready for Implementation

