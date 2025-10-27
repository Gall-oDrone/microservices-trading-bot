# Backtesting Service - Complete Implementation Plan

**Version**: 1.0.0  
**Status**: Planning Phase  
**Date**: October 27, 2025

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Architecture Analysis](#architecture-analysis)
3. [Service Architecture](#service-architecture)
4. [Component Design](#component-design)
5. [File Structure](#file-structure)
6. [Implementation Roadmap](#implementation-roadmap)
7. [Integration Points](#integration-points)
8. [Testing Strategy](#testing-strategy)
9. [Appendices](#appendices)

---

## 1. Executive Summary

### 1.1 Purpose

The Backtesting Service is a critical component of the trading platform that enables strategy validation, parameter optimization, and performance analysis using historical market data. It allows traders and developers to:

- Test trading strategies against historical data
- Evaluate strategy performance metrics (P&L, Sharpe ratio, drawdown, etc.)
- Optimize strategy parameters
- Validate risk management rules
- Compare multiple strategies
- Generate comprehensive performance reports

### 1.2 Key Features

- ✅ **Historical Data Replay** - Time-based replay of market data
- ✅ **Virtual Portfolio Management** - Simulated portfolio with realistic execution
- ✅ **Strategy Integration** - Support for all existing strategies
- ✅ **Performance Analytics** - Comprehensive metrics and statistics
- ✅ **Parameter Optimization** - Grid search and optimization algorithms
- ✅ **Result Storage** - Persistent storage of backtest results
- ✅ **HTTP API** - REST API for backtest management
- ✅ **Real-time Progress** - WebSocket-based progress updates
- ✅ **Batch Backtesting** - Run multiple backtests concurrently

### 1.3 Success Criteria

- Process historical data at >10,000 events/second
- Support date ranges from 1 day to 5 years
- Calculate 20+ performance metrics
- Run parameter optimization with 100+ combinations
- API response time <100ms
- Generate reports in <5 seconds

---

## 2. Architecture Analysis

### 2.1 Existing Services Overview

Based on analysis of existing services, the following patterns are identified:

#### Common Architecture Pattern

All microservices follow a consistent structure:

```
cmd/
  └── main.go                 # Application entry point
internal/
  ├── api/                    # HTTP handlers
  ├── config/                 # Configuration management
  ├── logger/                 # Structured logging
  ├── metrics/                # Prometheus metrics
  ├── models/                 # Domain models
  ├── server/                 # HTTP server
  └── [domain-specific]/      # Business logic packages
```

#### Key Observations

1. **Configuration Pattern**: Environment-based config with validation (see `order-management/internal/config/`)
2. **Logging Pattern**: Structured JSON logging with contextual fields (see `shared/pkg/`)
3. **Metrics Pattern**: Prometheus metrics with consistent naming (see `**/internal/metrics/`)
4. **Testing Pattern**: Table-driven tests with comprehensive coverage (see `**/internal/**/*_test.go`)
5. **Graceful Lifecycle**: Signal handling and graceful shutdown (see all `cmd/main.go`)
6. **Health Checks**: Liveness, readiness, and detailed health (see `shared/pkg/health/`)

### 2.2 Shared Package Analysis

The `shared/` package provides:

#### Available Components

```go
// shared/pkg/models/
- TradeSignalEvent     # Trading signals from strategy-executor
- OrderEvent           # Order lifecycle events
- TradeEvent           # Market trade events
- Order                # Order model

// shared/pkg/kafka/
- Producer             # Kafka producer with retries
- Consumer             # Kafka consumer (in consumer.go)

// shared/pkg/config/
- Config               # Base configuration
- LoadConfig()         # Config loading

// shared/pkg/health/
- HealthManager        # Health check management
- HealthChecker        # Health check interface

// shared/pkg/bitso/
- All Bitso API models # Market data structures
```

#### Integration Dependencies

```
┌─────────────────────────────────────────────────────┐
│                 Backtesting Service                  │
├─────────────────────────────────────────────────────┤
│                                                       │
│  Depends on:                                         │
│  ✓ shared/pkg/models     - Event and Order models   │
│  ✓ shared/pkg/kafka      - Kafka integration        │
│  ✓ shared/pkg/config     - Configuration            │
│  ✓ shared/pkg/health     - Health checks            │
│  ✓ shared/pkg/bitso      - Market data models       │
│                                                       │
│  Integrates with:                                    │
│  → market-data           - Historical data source    │
│  → strategy-executor     - Strategy definitions      │
│  → order-management      - Order validation logic    │
│                                                       │
└─────────────────────────────────────────────────────┘
```

### 2.3 Service Integration Analysis

#### Market-Data Service
- **Purpose**: Real-time and historical market data
- **API Endpoints**:
  - `GET /api/v1/trades` - Get historical trades
  - `GET /api/v1/ticker/history` - Get ticker history
  - `GET /api/v1/orderbook/history` - Get order book history
- **Integration**: HTTP client for historical data retrieval
- **Data Format**: `TradeEvent`, ticker, order book

#### Strategy-Executor Service
- **Purpose**: Trading strategy execution
- **Components**:
  - Strategy registry with factory pattern
  - Risk management
  - Signal generation
- **Integration**: 
  - Option 1: Embed strategies directly in backtesting
  - Option 2: Use strategy-executor as library (recommended)
- **Strategies Available**: Basic, Trend Following, Arbitrage

#### Order-Management Service
- **Purpose**: Order lifecycle and validation
- **Components**:
  - Order validator with risk rules
  - Position tracking
  - State machine for order states
- **Integration**: Reuse validation logic (as library or duplicate)
- **Models**: `Order`, `Position`, validation rules

---

## 3. Service Architecture

### 3.1 High-Level Architecture

```
┌─────────────────────────────────────────────────────────────┐
│              Backtesting Service Architecture                │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────┐    ┌───────────┐    ┌──────────────┐         │
│  │   API    │───▶│  Backtest │───▶│   Engine     │         │
│  │ Handlers │    │  Manager  │    │   (Runner)   │         │
│  └──────────┘    └───────────┘    └──────────────┘         │
│       │                │                    │                │
│       │                │                    ▼                │
│       │                │           ┌──────────────┐         │
│       │                │           │  Simulator   │         │
│       │                │           │  (Virtual    │         │
│       │                │           │   Market)    │         │
│       │                │           └──────────────┘         │
│       │                │                    │                │
│       │                │           ┌────────▼───────┐       │
│       │                │           │   Strategy     │       │
│       │                │           │   Executor     │       │
│       │                │           └────────┬───────┘       │
│       │                │                    │                │
│       │                │           ┌────────▼───────┐       │
│       │                │           │   Virtual      │       │
│       │                │           │   Portfolio    │       │
│       │                │           └────────┬───────┘       │
│       │                │                    │                │
│       │                └────────────────────┤                │
│       │                                     ▼                │
│       │                            ┌──────────────┐         │
│       │                            │  Performance │         │
│       │                            │   Analyzer   │         │
│       │                            └──────┬───────┘         │
│       │                                   │                 │
│       └───────────────────────────────────┤                 │
│                                           │                 │
│  ┌────────────┐    ┌──────────────┐     │                │
│  │  Storage   │◀───│   Result     │◀────┘                │
│  │  (Redis/   │    │   Manager    │                       │
│  │   Disk)    │    └──────────────┘                       │
│  └────────────┘                                            │
│                                                             │
└─────────────────────────────────────────────────────────────┘
         │                    │                    │
         ▼                    ▼                    ▼
  ┌────────────┐      ┌────────────┐      ┌────────────┐
  │  Market    │      │  Strategy  │      │   Order    │
  │   Data     │      │  Executor  │      │   Mgmt     │
  │  Service   │      │  (Library) │      │  (Logic)   │
  └────────────┘      └────────────┘      └────────────┘
```

### 3.2 Component Responsibilities

#### API Layer
- **Purpose**: REST API for backtest operations
- **Endpoints**:
  - Create/start backtests
  - Get backtest status
  - Get backtest results
  - List backtests
  - Cancel running backtests
- **Authentication**: Future implementation
- **Rate Limiting**: Standard middleware

#### Backtest Manager
- **Purpose**: Orchestrate backtest lifecycle
- **Responsibilities**:
  - Validate backtest requests
  - Queue backtests
  - Monitor progress
  - Handle cancellation
  - Coordinate with engine
- **State Management**: Track running backtests

#### Engine (Runner)
- **Purpose**: Execute backtest simulation
- **Responsibilities**:
  - Load historical data
  - Initialize simulator
  - Run event loop
  - Coordinate components
  - Generate results
- **Concurrency**: Support parallel backtests

#### Simulator (Virtual Market)
- **Purpose**: Simulate market conditions
- **Responsibilities**:
  - Replay market events in time order
  - Simulate order execution
  - Handle slippage and fees
  - Manage market state (order book, prices)
- **Realism**: Configurable execution models

#### Strategy Executor
- **Purpose**: Execute trading strategies
- **Responsibilities**:
  - Initialize strategies
  - Process market events
  - Generate signals
  - Apply risk rules
- **Reuse**: Use strategy-executor library

#### Virtual Portfolio
- **Purpose**: Track simulated positions and balance
- **Responsibilities**:
  - Manage virtual balance
  - Track positions
  - Calculate P&L
  - Apply transaction costs
- **Accuracy**: Match real trading behavior

#### Performance Analyzer
- **Purpose**: Calculate performance metrics
- **Responsibilities**:
  - Calculate returns
  - Compute risk metrics
  - Generate trade statistics
  - Create equity curves
- **Metrics**: 20+ standard metrics

#### Storage Layer
- **Purpose**: Persist backtest results
- **Options**:
  - Redis for active/recent results
  - Disk/S3 for historical results
- **Format**: JSON or Protocol Buffers

#### Result Manager
- **Purpose**: Manage result lifecycle
- **Responsibilities**:
  - Store results
  - Retrieve results
  - Generate reports
  - Export data
- **Caching**: Redis for fast access

---

## 4. Component Design

### 4.1 Core Models

#### BacktestConfig
```go
type BacktestConfig struct {
    ID              string                 `json:"id"`
    Name            string                 `json:"name"`
    Description     string                 `json:"description"`
    
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
    
    // Optimization (optional)
    OptimizationMode bool                  `json:"optimization_mode"`
    ParamRanges      map[string]Range      `json:"param_ranges"`
    
    // Metadata
    CreatedAt       time.Time              `json:"created_at"`
    CreatedBy       string                 `json:"created_by"`
}

type Range struct {
    Min  float64 `json:"min"`
    Max  float64 `json:"max"`
    Step float64 `json:"step"`
}
```

#### BacktestResult
```go
type BacktestResult struct {
    ID            string                 `json:"id"`
    BacktestID    string                 `json:"backtest_id"`
    ConfigID      string                 `json:"config_id"`
    
    // Status
    Status        string                 `json:"status"` // "running", "completed", "failed", "cancelled"
    Progress      float64                `json:"progress"`
    Error         string                 `json:"error,omitempty"`
    
    // Performance Summary
    Summary       PerformanceSummary     `json:"summary"`
    
    // Detailed Results
    Trades        []Trade                `json:"trades"`
    EquityCurve   []EquityPoint          `json:"equity_curve"`
    Positions     []Position             `json:"positions"`
    
    // Timing
    StartedAt     time.Time              `json:"started_at"`
    CompletedAt   *time.Time             `json:"completed_at,omitempty"`
    Duration      time.Duration          `json:"duration"`
    
    // Metadata
    Metadata      map[string]interface{} `json:"metadata"`
}

type PerformanceSummary struct {
    // Returns
    TotalReturn          float64 `json:"total_return"`
    TotalReturnPercent   float64 `json:"total_return_percent"`
    AnnualizedReturn     float64 `json:"annualized_return"`
    
    // Risk
    Volatility           float64 `json:"volatility"`
    SharpeRatio          float64 `json:"sharpe_ratio"`
    SortinoRatio         float64 `json:"sortino_ratio"`
    MaxDrawdown          float64 `json:"max_drawdown"`
    MaxDrawdownPercent   float64 `json:"max_drawdown_percent"`
    
    // Trade Statistics
    TotalTrades          int     `json:"total_trades"`
    WinningTrades        int     `json:"winning_trades"`
    LosingTrades         int     `json:"losing_trades"`
    WinRate              float64 `json:"win_rate"`
    AverageWin           float64 `json:"average_win"`
    AverageLoss          float64 `json:"average_loss"`
    ProfitFactor         float64 `json:"profit_factor"`
    
    // Position Metrics
    AverageHoldingTime   time.Duration `json:"average_holding_time"`
    MaxPosition          float64       `json:"max_position"`
    
    // P&L
    GrossProfitLoss      float64 `json:"gross_profit_loss"`
    NetProfitLoss        float64 `json:"net_profit_loss"`
    TotalCommissions     float64 `json:"total_commissions"`
    
    // Portfolio
    FinalBalance         float64 `json:"final_balance"`
    PeakBalance          float64 `json:"peak_balance"`
}

type EquityPoint struct {
    Timestamp time.Time `json:"timestamp"`
    Balance   float64   `json:"balance"`
    Equity    float64   `json:"equity"`
    Return    float64   `json:"return"`
}

type Trade struct {
    ID              string    `json:"id"`
    EntryTime       time.Time `json:"entry_time"`
    ExitTime        time.Time `json:"exit_time"`
    Book            string    `json:"book"`
    Side            string    `json:"side"`
    EntryPrice      float64   `json:"entry_price"`
    ExitPrice       float64   `json:"exit_price"`
    Amount          float64   `json:"amount"`
    ProfitLoss      float64   `json:"profit_loss"`
    ProfitLossPercent float64 `json:"profit_loss_percent"`
    Commission      float64   `json:"commission"`
    Slippage        float64   `json:"slippage"`
    HoldingTime     time.Duration `json:"holding_time"`
}

type Position struct {
    Book         string    `json:"book"`
    Size         float64   `json:"size"`
    AveragePrice float64   `json:"average_price"`
    CurrentPrice float64   `json:"current_price"`
    UnrealizedPL float64   `json:"unrealized_pl"`
    Timestamp    time.Time `json:"timestamp"`
}
```

#### VirtualPortfolio
```go
type VirtualPortfolio struct {
    ID              string               `json:"id"`
    InitialBalance  float64              `json:"initial_balance"`
    CurrentBalance  float64              `json:"current_balance"`
    Positions       map[string]*Position `json:"positions"`
    TotalPL         float64              `json:"total_pl"`
    TotalCommissions float64             `json:"total_commissions"`
    
    // Trade history
    Trades          []Trade              `json:"trades"`
    
    // Metrics tracking
    PeakBalance     float64              `json:"peak_balance"`
    DrawdownAmount  float64              `json:"drawdown_amount"`
    
    // State
    mu              sync.RWMutex
}
```

### 4.2 Core Interfaces

```go
// Engine interface
type BacktestEngine interface {
    Run(ctx context.Context, config *BacktestConfig) (*BacktestResult, error)
    Cancel(backtestID string) error
    GetProgress(backtestID string) (float64, error)
}

// Data provider interface
type DataProvider interface {
    LoadHistoricalData(ctx context.Context, book string, startDate, endDate time.Time) ([]MarketEvent, error)
    StreamData(ctx context.Context, book string, startDate, endDate time.Time) (<-chan MarketEvent, error)
}

// Market simulator interface
type MarketSimulator interface {
    Initialize(ctx context.Context, config *SimulatorConfig) error
    ProcessEvent(event MarketEvent) error
    ExecuteOrder(order *Order) (*OrderExecution, error)
    GetCurrentPrice(book string) (float64, error)
    GetOrderBook(book string) (*OrderBook, error)
}

// Strategy interface (reuse from strategy-executor)
type Strategy interface {
    Initialize(config map[string]interface{}) error
    OnTrade(trade *TradeEvent) (*Signal, error)
    OnTicker(ticker *TickerEvent) (*Signal, error)
    OnOrderBook(orderBook *OrderBookEvent) (*Signal, error)
    GetName() string
}

// Portfolio interface
type Portfolio interface {
    GetBalance() float64
    GetPosition(book string) (*Position, error)
    GetAllPositions() []*Position
    ExecuteTrade(trade *Trade) error
    CalculateEquity(prices map[string]float64) float64
    GetTrades() []Trade
}

// Analyzer interface
type PerformanceAnalyzer interface {
    Analyze(portfolio *VirtualPortfolio, trades []Trade, equityCurve []EquityPoint) (*PerformanceSummary, error)
    CalculateMetrics(portfolio *VirtualPortfolio) (*PerformanceSummary, error)
    GenerateReport(result *BacktestResult) (string, error)
}

// Storage interface
type ResultStorage interface {
    Save(ctx context.Context, result *BacktestResult) error
    Get(ctx context.Context, backtestID string) (*BacktestResult, error)
    List(ctx context.Context, filters map[string]interface{}) ([]*BacktestResult, error)
    Delete(ctx context.Context, backtestID string) error
}
```

---

## 5. File Structure

### 5.1 Complete Directory Structure

```
services/backtesting/
├── cmd/
│   └── main.go                         # Application entry point
├── internal/
│   ├── analyzer/
│   │   ├── analyzer.go                 # Performance analyzer implementation
│   │   ├── metrics.go                  # Metric calculations
│   │   ├── report.go                   # Report generation
│   │   └── analyzer_test.go            # Analyzer tests
│   ├── api/
│   │   ├── handlers.go                 # HTTP handlers
│   │   ├── backtest_handlers.go        # Backtest CRUD endpoints
│   │   ├── result_handlers.go          # Result endpoints
│   │   ├── optimization_handlers.go    # Optimization endpoints
│   │   ├── response.go                 # Response utilities
│   │   └── handlers_test.go            # Handler tests
│   ├── config/
│   │   ├── config.go                   # Configuration structure
│   │   ├── loader.go                   # Config loading logic
│   │   ├── validation.go               # Config validation
│   │   └── config_test.go              # Config tests
│   ├── data/
│   │   ├── provider.go                 # Data provider interface
│   │   ├── market_data_provider.go     # Market-data service client
│   │   ├── file_provider.go            # File-based data provider
│   │   ├── cache.go                    # Data caching
│   │   └── provider_test.go            # Provider tests
│   ├── engine/
│   │   ├── engine.go                   # Main backtest engine
│   │   ├── runner.go                   # Backtest runner
│   │   ├── event_loop.go               # Event processing loop
│   │   ├── coordinator.go              # Component coordination
│   │   └── engine_test.go              # Engine tests
│   ├── logger/
│   │   └── logger.go                   # Structured logging
│   ├── manager/
│   │   ├── backtest_manager.go         # Backtest lifecycle management
│   │   ├── queue.go                    # Backtest queue
│   │   ├── state.go                    # State management
│   │   └── manager_test.go             # Manager tests
│   ├── metrics/
│   │   ├── prometheus.go               # Prometheus metrics
│   │   └── collector.go                # Metrics collector
│   ├── models/
│   │   ├── backtest.go                 # Backtest domain models
│   │   ├── config.go                   # Config models
│   │   ├── result.go                   # Result models
│   │   ├── portfolio.go                # Portfolio models
│   │   ├── trade.go                    # Trade models
│   │   ├── event.go                    # Market event models
│   │   └── validation.go               # Model validation
│   ├── optimizer/
│   │   ├── optimizer.go                # Parameter optimizer
│   │   ├── grid_search.go              # Grid search implementation
│   │   ├── genetic.go                  # Genetic algorithm (future)
│   │   └── optimizer_test.go           # Optimizer tests
│   ├── portfolio/
│   │   ├── virtual_portfolio.go        # Virtual portfolio implementation
│   │   ├── position.go                 # Position tracking
│   │   ├── balance.go                  # Balance management
│   │   └── portfolio_test.go           # Portfolio tests
│   ├── server/
│   │   ├── http_server.go              # HTTP server
│   │   ├── routes.go                   # Route definitions
│   │   └── middleware.go               # Middleware
│   ├── simulator/
│   │   ├── simulator.go                # Market simulator
│   │   ├── execution.go                # Order execution simulation
│   │   ├── slippage.go                 # Slippage models
│   │   ├── orderbook.go                # Order book simulation
│   │   └── simulator_test.go           # Simulator tests
│   ├── storage/
│   │   ├── storage.go                  # Storage interface
│   │   ├── redis_storage.go            # Redis implementation
│   │   ├── file_storage.go             # File-based storage
│   │   └── storage_test.go             # Storage tests
│   └── strategy/
│       ├── strategy.go                 # Strategy interface
│       ├── executor.go                 # Strategy executor
│       ├── factory.go                  # Strategy factory
│       └── strategy_test.go            # Strategy tests
├── test/
│   ├── integration/
│   │   ├── backtest_test.go            # Integration tests
│   │   └── api_test.go                 # API integration tests
│   └── fixtures/
│       ├── sample_data.json            # Test data
│       └── expected_results.json       # Expected results
├── scripts/
│   ├── run_backtest.sh                 # Manual backtest script
│   └── import_data.sh                  # Data import script
├── Dockerfile
├── go.mod
├── go.sum
├── README.md
├── BACKTESTING_IMPLEMENTATION_PLAN.md  # This file
└── .env.example                        # Example environment variables
```

### 5.2 File Size Estimates

| Component | Files | Estimated Lines | Complexity |
|-----------|-------|-----------------|------------|
| analyzer/ | 4 | 800-1000 | High |
| api/ | 6 | 600-800 | Medium |
| config/ | 4 | 400-500 | Low |
| data/ | 5 | 600-800 | Medium |
| engine/ | 5 | 800-1000 | High |
| logger/ | 1 | 100-150 | Low |
| manager/ | 4 | 500-700 | Medium |
| metrics/ | 2 | 300-400 | Low |
| models/ | 7 | 700-900 | Medium |
| optimizer/ | 4 | 600-800 | High |
| portfolio/ | 4 | 500-700 | Medium |
| server/ | 3 | 400-500 | Low |
| simulator/ | 5 | 700-900 | High |
| storage/ | 4 | 500-700 | Medium |
| strategy/ | 4 | 400-600 | Medium |
| **Total** | **56** | **7,900-10,550** | **Mixed** |

---

## 6. Implementation Roadmap

### 6.1 Phase 1: Foundation (Week 1)

**Goal**: Set up project structure and core infrastructure

**Tasks**:
1. ✅ Create project structure
2. ✅ Implement configuration management
   - `internal/config/config.go`
   - `internal/config/loader.go`
   - `internal/config/validation.go`
   - Tests: `internal/config/config_test.go`
3. ✅ Set up logging
   - `internal/logger/logger.go`
4. ✅ Implement metrics collection
   - `internal/metrics/prometheus.go`
   - `internal/metrics/collector.go`
5. ✅ Define core models
   - `internal/models/backtest.go`
   - `internal/models/config.go`
   - `internal/models/result.go`
   - `internal/models/portfolio.go`
6. ✅ Set up main application structure
   - `cmd/main.go`

**Deliverables**:
- Project compiles
- Configuration loads correctly
- Logging works
- Metrics exposed

### 6.2 Phase 2: Data Layer (Week 2)

**Goal**: Implement data providers and storage

**Tasks**:
1. ✅ Implement data provider interface
   - `internal/data/provider.go`
2. ✅ Create market-data service client
   - `internal/data/market_data_provider.go`
3. ✅ Implement file-based provider
   - `internal/data/file_provider.go`
4. ✅ Add data caching
   - `internal/data/cache.go`
5. ✅ Implement result storage
   - `internal/storage/storage.go`
   - `internal/storage/redis_storage.go`
   - `internal/storage/file_storage.go`
6. ✅ Write comprehensive tests
   - `internal/data/provider_test.go`
   - `internal/storage/storage_test.go`

**Deliverables**:
- Historical data loading works
- Result persistence works
- All tests pass

### 6.3 Phase 3: Simulation Engine (Week 3)

**Goal**: Implement core simulation components

**Tasks**:
1. ✅ Implement virtual portfolio
   - `internal/portfolio/virtual_portfolio.go`
   - `internal/portfolio/position.go`
   - `internal/portfolio/balance.go`
2. ✅ Create market simulator
   - `internal/simulator/simulator.go`
   - `internal/simulator/execution.go`
   - `internal/simulator/slippage.go`
   - `internal/simulator/orderbook.go`
3. ✅ Implement strategy executor
   - `internal/strategy/strategy.go`
   - `internal/strategy/executor.go`
   - `internal/strategy/factory.go`
4. ✅ Write comprehensive tests
   - `internal/portfolio/portfolio_test.go`
   - `internal/simulator/simulator_test.go`
   - `internal/strategy/strategy_test.go`

**Deliverables**:
- Portfolio management works
- Order execution simulation works
- Strategy execution works
- All tests pass

### 6.4 Phase 4: Backtest Engine (Week 4)

**Goal**: Implement main backtest engine

**Tasks**:
1. ✅ Implement backtest engine
   - `internal/engine/engine.go`
   - `internal/engine/runner.go`
   - `internal/engine/event_loop.go`
   - `internal/engine/coordinator.go`
2. ✅ Create backtest manager
   - `internal/manager/backtest_manager.go`
   - `internal/manager/queue.go`
   - `internal/manager/state.go`
3. ✅ Implement performance analyzer
   - `internal/analyzer/analyzer.go`
   - `internal/analyzer/metrics.go`
   - `internal/analyzer/report.go`
4. ✅ Write comprehensive tests
   - `internal/engine/engine_test.go`
   - `internal/manager/manager_test.go`
   - `internal/analyzer/analyzer_test.go`

**Deliverables**:
- End-to-end backtest works
- Performance metrics calculated
- Results stored correctly
- All tests pass

### 6.5 Phase 5: API Layer (Week 5)

**Goal**: Implement REST API

**Tasks**:
1. ✅ Implement HTTP server
   - `internal/server/http_server.go`
   - `internal/server/routes.go`
   - `internal/server/middleware.go`
2. ✅ Create API handlers
   - `internal/api/handlers.go`
   - `internal/api/backtest_handlers.go`
   - `internal/api/result_handlers.go`
   - `internal/api/response.go`
3. ✅ Add health checks
4. ✅ Write API tests
   - `internal/api/handlers_test.go`
   - `test/integration/api_test.go`

**Deliverables**:
- All API endpoints work
- Health checks work
- API tests pass

### 6.6 Phase 6: Optimization (Week 6)

**Goal**: Implement parameter optimization

**Tasks**:
1. ✅ Implement grid search
   - `internal/optimizer/optimizer.go`
   - `internal/optimizer/grid_search.go`
2. ✅ Add optimization API endpoints
   - `internal/api/optimization_handlers.go`
3. ✅ Implement result comparison
4. ✅ Write optimizer tests
   - `internal/optimizer/optimizer_test.go`

**Deliverables**:
- Parameter optimization works
- Multiple backtests run in parallel
- Results compared correctly
- All tests pass

### 6.7 Phase 7: Testing & Documentation (Week 7)

**Goal**: Comprehensive testing and documentation

**Tasks**:
1. ✅ Write integration tests
   - `test/integration/backtest_test.go`
2. ✅ Achieve >80% code coverage
3. ✅ Write comprehensive README
4. ✅ Create API documentation
5. ✅ Add deployment guide
6. ✅ Create example scripts

**Deliverables**:
- All tests pass
- Coverage >80%
- Documentation complete

---

## 7. Integration Points

### 7.1 Market-Data Service Integration

**Purpose**: Load historical market data

**Integration Method**: HTTP Client

**Endpoints Used**:
```go
// Get historical trades
GET /api/v1/trades?book={book}&from={start}&to={end}&limit=10000

// Get ticker history
GET /api/v1/ticker/history?book={book}&from={start}&to={end}

// Get order book history (if available)
GET /api/v1/orderbook/history?book={book}&from={start}&to={end}
```

**Client Implementation**:
```go
type MarketDataClient struct {
    baseURL    string
    httpClient *http.Client
    logger     logger.Logger
}

func (c *MarketDataClient) GetHistoricalTrades(
    ctx context.Context,
    book string,
    startDate, endDate time.Time,
) ([]TradeEvent, error) {
    // HTTP request to market-data service
}
```

**Error Handling**:
- Retry logic with exponential backoff
- Fallback to file-based provider
- Cache responses

**Data Volume Considerations**:
- Batch requests (max 10,000 events per request)
- Stream large datasets
- Cache frequently accessed data

### 7.2 Strategy-Executor Integration

**Purpose**: Reuse strategy definitions and execution logic

**Integration Method**: Library/Package Import

**Approach**:
```go
import (
    "bitso-trading-platform/strategy-executor/internal/strategies"
    "bitso-trading-platform/strategy-executor/internal/risk"
)

// Use existing strategies
func (e *Engine) initializeStrategy() {
    strategy := strategies.NewBasicStrategy(e.config.StrategyParams)
    e.strategy = strategy
}
```

**Components to Reuse**:
- Strategy interface and implementations
- Strategy factory
- Risk manager (optional)

**Adaptation Needed**:
- Remove Kafka dependencies
- Adapt to synchronous event processing
- Use virtual portfolio instead of real orders

### 7.3 Order-Management Integration

**Purpose**: Reuse order validation and state machine logic

**Integration Method**: Logic Duplication or Library

**Option 1 - Logic Duplication** (Recommended):
```go
// Copy validation logic to backtesting
type OrderValidator struct {
    config *ValidationConfig
}

func (v *OrderValidator) ValidateOrder(order *Order) error {
    // Same logic as order-management
}
```

**Option 2 - Library Import**:
```go
import "bitso-trading-platform/order-management/internal/validator"

// Use existing validator
validator := validator.NewOrderValidator(config, logger)
```

**Components to Consider**:
- Order validator
- Risk manager rules
- State machine (simplified for backtesting)

**Differences in Backtesting**:
- No real exchange communication
- Instant order execution (or simulated delay)
- No order rejection from exchange

### 7.4 Shared Package Integration

**Components Used**:

```go
import (
    "bitso-trading-platform/shared/pkg/config"
    "bitso-trading-platform/shared/pkg/health"
    "bitso-trading-platform/shared/pkg/kafka"    // For future event streaming
    "bitso-trading-platform/shared/pkg/models"
)

// Use shared models
type BacktestEvent struct {
    EventID   string
    Timestamp time.Time
    Trade     *models.TradeEvent
    // ...
}
```

**New Models to Add to Shared**:
- `BacktestConfig`
- `BacktestResult`
- `PerformanceSummary`

---

## 8. Testing Strategy

### 8.1 Unit Testing

**Coverage Target**: >80%

**Test Categories**:

1. **Configuration Tests**
   ```go
   // internal/config/config_test.go
   func TestLoadConfig(t *testing.T)
   func TestValidateConfig(t *testing.T)
   func TestConfigDefaults(t *testing.T)
   ```

2. **Portfolio Tests**
   ```go
   // internal/portfolio/portfolio_test.go
   func TestExecuteTrade(t *testing.T)
   func TestCalculateEquity(t *testing.T)
   func TestPositionTracking(t *testing.T)
   func TestBalanceUpdates(t *testing.T)
   ```

3. **Simulator Tests**
   ```go
   // internal/simulator/simulator_test.go
   func TestOrderExecution(t *testing.T)
   func TestSlippageCalculation(t *testing.T)
   func TestOrderBookSimulation(t *testing.T)
   func TestMarketOrders(t *testing.T)
   func TestLimitOrders(t *testing.T)
   ```

4. **Analyzer Tests**
   ```go
   // internal/analyzer/analyzer_test.go
   func TestCalculateReturns(t *testing.T)
   func TestCalculateSharpeRatio(t *testing.T)
   func TestCalculateDrawdown(t *testing.T)
   func TestCalculateWinRate(t *testing.T)
   ```

5. **Engine Tests**
   ```go
   // internal/engine/engine_test.go
   func TestRunBacktest(t *testing.T)
   func TestEventLoop(t *testing.T)
   func TestCancellation(t *testing.T)
   func TestErrorHandling(t *testing.T)
   ```

### 8.2 Integration Testing

**Test Scenarios**:

1. **End-to-End Backtest**
   ```go
   func TestCompleteBacktest(t *testing.T) {
       // Create config
       // Run backtest
       // Verify results
   }
   ```

2. **API Tests**
   ```go
   func TestCreateBacktest(t *testing.T)
   func TestGetBacktestStatus(t *testing.T)
   func TestGetBacktestResults(t *testing.T)
   func TestListBacktests(t *testing.T)
   ```

3. **Optimization Tests**
   ```go
   func TestGridSearchOptimization(t *testing.T)
   func TestMultipleBacktests(t *testing.T)
   ```

### 8.3 Performance Testing

**Benchmarks**:

```go
func BenchmarkBacktestExecution(b *testing.B)
func BenchmarkDataLoading(b *testing.B)
func BenchmarkMetricsCalculation(b *testing.B)
func BenchmarkPortfolioOperations(b *testing.B)
```

**Performance Targets**:
- Process 10,000 events/second
- Complete 1-year backtest in <30 seconds
- Memory usage <500MB for typical backtest
- Support 10 concurrent backtests

### 8.4 Test Data

**Fixtures**:
```
test/fixtures/
├── sample_data.json          # 1000 sample trade events
├── expected_results.json     # Expected backtest results
├── strategy_configs/
│   ├── basic.json
│   ├── trend.json
│   └── arbitrage.json
└── market_conditions/
    ├── bull_market.json
    ├── bear_market.json
    └── sideways.json
```

**Data Generators**:
```go
func GenerateSyntheticTrades(count int, pattern string) []TradeEvent
func GenerateOrderBook(depth int, spread float64) *OrderBook
```

---

## 9. Appendices

### 9.1 API Endpoint Specifications

#### Create Backtest
```
POST /api/v1/backtests
Content-Type: application/json

{
  "name": "Basic Strategy Test",
  "start_date": "2024-01-01T00:00:00Z",
  "end_date": "2024-12-31T23:59:59Z",
  "book": "btc_mxn",
  "initial_balance": 100000.0,
  "strategy": "basic",
  "strategy_params": {
    "rsi_period": 14,
    "rsi_oversold": 30,
    "rsi_overbought": 70
  },
  "slippage_model": "percentage",
  "slippage_value": 0.001,
  "commission_rate": 0.001
}

Response: 201 Created
{
  "id": "bt-123456",
  "status": "pending",
  "created_at": "2025-10-27T12:00:00Z"
}
```

#### Get Backtest Status
```
GET /api/v1/backtests/{id}

Response: 200 OK
{
  "id": "bt-123456",
  "status": "running",
  "progress": 0.45,
  "started_at": "2025-10-27T12:00:00Z",
  "estimated_completion": "2025-10-27T12:05:00Z"
}
```

#### Get Backtest Results
```
GET /api/v1/backtests/{id}/results

Response: 200 OK
{
  "id": "bt-123456",
  "status": "completed",
  "summary": {
    "total_return": 15234.50,
    "total_return_percent": 15.23,
    "sharpe_ratio": 1.85,
    "max_drawdown": -5432.10,
    "win_rate": 0.65,
    "total_trades": 127
  },
  "trades": [...],
  "equity_curve": [...],
  "completed_at": "2025-10-27T12:04:32Z"
}
```

#### List Backtests
```
GET /api/v1/backtests?status=completed&limit=10

Response: 200 OK
{
  "backtests": [
    {
      "id": "bt-123456",
      "name": "Basic Strategy Test",
      "status": "completed",
      "summary": {...}
    }
  ],
  "total": 45,
  "page": 1,
  "limit": 10
}
```

#### Cancel Backtest
```
POST /api/v1/backtests/{id}/cancel

Response: 200 OK
{
  "id": "bt-123456",
  "status": "cancelled",
  "message": "Backtest cancelled successfully"
}
```

#### Run Optimization
```
POST /api/v1/optimizations
Content-Type: application/json

{
  "name": "RSI Parameter Optimization",
  "start_date": "2024-01-01T00:00:00Z",
  "end_date": "2024-12-31T23:59:59Z",
  "book": "btc_mxn",
  "initial_balance": 100000.0,
  "strategy": "basic",
  "param_ranges": {
    "rsi_period": {"min": 10, "max": 20, "step": 2},
    "rsi_oversold": {"min": 20, "max": 35, "step": 5},
    "rsi_overbought": {"min": 65, "max": 80, "step": 5}
  },
  "optimization_metric": "sharpe_ratio"
}

Response: 201 Created
{
  "id": "opt-789012",
  "total_combinations": 96,
  "status": "running",
  "progress": 0.0
}
```

### 9.2 Configuration Example

```yaml
# .env.example
SERVICE_NAME=backtesting
SERVICE_VERSION=1.0.0
SERVICE_HOST=0.0.0.0
SERVICE_PORT=8084
ENVIRONMENT=development

# Market Data Service
MARKET_DATA_BASE_URL=http://localhost:8083
MARKET_DATA_TIMEOUT=30s
MARKET_DATA_RETRY_COUNT=3

# Redis Configuration
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# Storage Configuration
STORAGE_TYPE=redis  # redis, file, s3
STORAGE_PATH=/var/lib/backtesting/results
STORAGE_RETENTION_DAYS=90

# Execution Settings
MAX_CONCURRENT_BACKTESTS=5
DEFAULT_SLIPPAGE_MODEL=percentage
DEFAULT_SLIPPAGE_VALUE=0.001
DEFAULT_COMMISSION_RATE=0.001

# Logging
LOG_LEVEL=info
LOG_FORMAT=json
LOG_OUTPUT=stdout

# Metrics
METRICS_ENABLED=true
METRICS_PATH=/metrics
METRICS_PORT=9094
```

### 9.3 Performance Metrics Definitions

| Metric | Formula | Description |
|--------|---------|-------------|
| Total Return | Final Balance - Initial Balance | Absolute profit/loss |
| Total Return % | (Final / Initial - 1) × 100 | Percentage return |
| Annualized Return | (1 + Total Return)^(365/days) - 1 | Yearly return rate |
| Volatility | StdDev(daily returns) × √252 | Return volatility |
| Sharpe Ratio | (Return - Risk Free) / Volatility | Risk-adjusted return |
| Sortino Ratio | Return / Downside Volatility | Downside risk-adjusted |
| Max Drawdown | Max(Peak - Trough) | Largest peak-to-trough decline |
| Win Rate | Winning Trades / Total Trades | Percentage of profitable trades |
| Profit Factor | Gross Profit / Gross Loss | Profit to loss ratio |
| Average Win | Sum(Winning P&L) / Winning Trades | Average profitable trade |
| Average Loss | Sum(Losing P&L) / Losing Trades | Average losing trade |

### 9.4 Dependencies

```go
// go.mod
module bitso-trading-platform/backtesting

go 1.21

require (
    bitso-trading-platform/shared v0.0.0
    bitso-trading-platform/strategy-executor v0.0.0  // optional
    
    github.com/gorilla/mux v1.8.0
    github.com/prometheus/client_golang v1.17.0
    github.com/redis/go-redis/v9 v9.5.1
    github.com/rs/zerolog v1.34.0
    github.com/shopspring/decimal v1.3.1
    github.com/stretchr/testify v1.8.4
)
```

---

## Summary

This implementation plan provides a comprehensive roadmap for building the Backtesting Service. The service will:

1. **Integrate seamlessly** with existing microservices (market-data, strategy-executor, order-management)
2. **Follow established patterns** from other services in the platform
3. **Reuse shared components** from the shared package
4. **Provide robust functionality** for strategy validation and optimization
5. **Maintain high code quality** with >80% test coverage
6. **Support production deployment** with proper monitoring and health checks

The estimated implementation timeline is **7 weeks** with clear milestones and deliverables for each phase.

**Next Steps**:
1. Review and approve this plan
2. Set up development environment
3. Begin Phase 1 implementation
4. Regular progress reviews

---

**Document Version**: 1.0.0  
**Last Updated**: October 27, 2025  
**Status**: Ready for Implementation

