# Financial Strategy Implementation Guide

This document outlines the recommended approach for implementing financial metric and model-based trading strategies, backtesting them using the Bitso Stage API, and validating them in a controlled environment before any production consideration.

**Document Version:** 1.1  
**Date:** April 7, 2026  
**Last Updated:** April 7, 2026  
**Prerequisites:** Phases 1-6 of [INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md](../INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md) complete; P&L fixes from [PNL-DEBUGGING-AND-FIXES.md](PNL-DEBUGGING-AND-FIXES.md) applied.

---

## Implementation Progress

| Phase | Status | Completion Date | Notes |
|-------|--------|-----------------|-------|
| **Phase 1: Foundation Fixes** | ✅ Complete | April 2026 | Pre-trade validation, Redis persistence, integration tests |
| **Phase 2: Indicator Infrastructure** | ✅ Complete | April 2026 | All core indicators implemented (SMA, EMA, RSI, Bollinger, ATR, VWAP) |
| **Phase 3: Strategy Framework** | ✅ Complete | April 7, 2026 | Enhanced strategy interface, registry, mean reversion + momentum strategies |
| **Phase 4: Backtest Integration** | 🔄 Pending | - | Unified code, promotion workflow |
| **Phase 5: Stage Observation** | 🔄 Pending | - | Stage deployment, monitoring |

---

## Table of Contents

1. [Current State Summary](#current-state-summary)
2. [Strategy Development Lifecycle](#strategy-development-lifecycle)
3. [Recommended Strategy Types](#recommended-strategy-types)
4. [Production-Ready Architecture](#production-ready-architecture)
5. [Implementation Phases](#implementation-phases)
6. [First Strategy: Mean Reversion](#first-strategy-mean-reversion)
7. [Stage Observation Protocol](#stage-observation-protocol)
8. [Validation Gates](#validation-gates)
9. [Risk Management](#risk-management)
10. [Timeline Estimate](#timeline-estimate)

---

## Current State Summary

### Infrastructure (Complete)

| Component | Status | Notes |
|-----------|--------|-------|
| Stage/Production URL switching | ✅ Done | `BITSO_API_BASE_URL` env-driven |
| Order/Fill sync | ✅ Done | Kafka + Bitso sync job |
| Redis persistence | ✅ Done | Opt-in via `STORAGE_TYPE=redis` |
| Risk limits | ✅ Done | `MaxDailyLoss`, `MaxDrawdownPct` |
| Intraday metrics | ✅ Done | Prometheus + Grafana dashboards |
| Trades → Redis | ✅ Done | Market-data WebSocket persistence |
| EKS deployment | ✅ Done | Full cluster with monitoring |

### Known Gaps to Address

| Issue | Impact | Priority |
|-------|--------|----------|
| Pre-trade risk alignment | Engine places order before OM can reject | High |
| Redis not default for stage | Position state lost on restarts | High |
| No integration test suite | BUY→SELL→P&L path untested | Medium |
| No indicator infrastructure | Strategies can't access computed metrics | Medium |

---

## Strategy Development Lifecycle

The financial industry follows a rigorous lifecycle for strategy development. This project adopts the same approach:

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│  Research   │───▶│  Backtest   │───▶│   Paper     │───▶│  Shadow     │───▶│ Production  │
│  & Design   │    │  (History)  │    │  Trading    │    │  Trading    │    │  (Live)     │
└─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘    └─────────────┘
     │                   │                  │                  │                  │
     ▼                   ▼                  ▼                  ▼                  ▼
  Hypothesis        Historical         Stage env          Stage env          Production
  + metrics         validation         no real $          parallel run       Bitso API
  definition        (offline)          (Bitso Stage)      + comparison       + real funds
```

### Stage Definitions

| Stage | Environment | Real Money | Purpose |
|-------|-------------|------------|---------|
| **Backtest** | Local/CI | No | Validate hypothesis on historical data |
| **Paper Trading** | Bitso Stage | No | Test order flow, fills, P&L calculation |
| **Shadow Trading** | Bitso Stage | No | Extended observation (2-4 weeks minimum) |
| **Production** | Bitso Production | Yes | Live trading with real funds |

---

## Recommended Strategy Types

Strategies are ordered by implementation complexity. Start with Level 1 and progress as the framework matures.

| Level | Strategy Type | Metrics/Inputs | Complexity | Use Case |
|-------|--------------|----------------|------------|----------|
| 1 | **Mean Reversion** | Price deviation from SMA/EMA, Bollinger Bands | Low | Range-bound markets |
| 2 | **Momentum/Trend** | RSI, MACD, price velocity | Low-Medium | Trending markets |
| 3 | **VWAP-based** | Volume-weighted avg price, time-of-day | Medium | Intraday execution |
| 4 | **Volatility-adjusted** | ATR, realized vol, implied vol proxies | Medium | Position sizing |
| 5 | **Statistical Arbitrage** | Spread z-score, cointegration | Medium-High | Pair trading |
| 6 | **ML-based** | Feature vectors → classification/regression | High | Pattern recognition |

### Indicator Dependencies

```
Level 1-2: SMA, EMA, RSI, Bollinger Bands
Level 3:   VWAP, Volume profile
Level 4:   ATR, Historical volatility, Parkinson estimator
Level 5:   Correlation matrix, Cointegration tests
Level 6:   Feature pipelines, Model serving infrastructure
```

---

## Production-Ready Architecture

### Proposed Directory Structure

```
services/strategy-executor/
├── internal/
│   ├── strategies/           # Strategy implementations
│   │   ├── interface.go      # Common Strategy interface
│   │   ├── mean_reversion.go # Level 1 strategy
│   │   ├── momentum.go       # Level 2 strategy
│   │   ├── vwap.go           # Level 3 strategy
│   │   └── registry.go       # Strategy registration & lifecycle
│   ├── indicators/           # Technical indicators
│   │   ├── interface.go      # Indicator interface
│   │   ├── sma.go            # Simple Moving Average
│   │   ├── ema.go            # Exponential Moving Average
│   │   ├── rsi.go            # Relative Strength Index
│   │   ├── bollinger.go      # Bollinger Bands
│   │   ├── atr.go            # Average True Range
│   │   ├── vwap.go           # Volume Weighted Average Price
│   │   └── store.go          # Indicator storage (Redis)
│   ├── features/             # Feature engineering
│   │   ├── price_features.go # Price-derived features
│   │   ├── volume_features.go# Volume-derived features
│   │   └── market_features.go# Market microstructure
│   └── risk/                 # Strategy-level risk
│       ├── position_sizing.go# Kelly, fixed fraction, etc.
│       └── signal_filters.go # Entry/exit filters
```

### Strategy Interface

```go
// Strategy defines the contract for all trading strategies
type Strategy interface {
    // Metadata
    Name() string
    Version() string
    
    // Lifecycle
    Initialize(config StrategyConfig) error
    Start(ctx context.Context) error
    Stop() error
    Reset()
    
    // Signal generation
    OnTick(tick *models.TradeEvent) (*Signal, error)
    OnBar(bar *models.OHLCV) (*Signal, error)
    
    // State management
    GetState() StrategyState
    GetMetrics() StrategyMetrics
}

// Signal represents a trading signal from a strategy
type Signal struct {
    Strategy    string
    Book        string
    Side        string    // BUY, SELL
    Amount      float64
    Price       float64   // Limit price (0 for market)
    Confidence  float64   // 0-1 signal strength
    Reason      string    // Human-readable explanation
    Timestamp   time.Time
    Metadata    map[string]interface{}
}
```

### Strategy Configuration (Declarative YAML)

```yaml
# config/strategies/mean_reversion_btc_mxn.yaml
strategy:
  name: "mean_reversion_btc_mxn"
  type: "mean_reversion"
  version: "1.0.0"
  enabled: true
  
  # Target market
  book: "btc_mxn"
  
  # Strategy-specific parameters
  parameters:
    lookback_period: 20       # Number of data points for SMA
    entry_threshold: 2.0      # Standard deviations for entry
    exit_threshold: 0.5       # Standard deviations for exit
    min_signal_interval: 60   # Seconds between signals
    
  # Position sizing
  sizing:
    method: "fixed"           # fixed, kelly, volatility_adjusted
    max_position_size: 0.01   # BTC
    max_position_value: 15000 # MXN
    
  # Risk controls (strategy-level)
  risk:
    max_daily_loss: 500       # MXN
    max_drawdown_pct: 5       # Percent
    max_trades_per_day: 50
    max_consecutive_losses: 5
    
  # Schedule
  schedule:
    active_hours: "09:00-17:00"
    timezone: "America/Mexico_City"
    active_days: ["Mon", "Tue", "Wed", "Thu", "Fri"]
    
  # Backtest settings
  backtest:
    slippage_bps: 10          # Basis points
    commission_bps: 25        # Basis points
    fill_probability: 0.95    # For limit orders
```

### Indicator Store (Redis)

```
# Key format: ind:{book}:{indicator}:{period}
# Example keys:
ind:btc_mxn:sma:20          → {"value": 1190000.5, "ts": 1712505600}
ind:btc_mxn:ema:20          → {"value": 1189500.2, "ts": 1712505600}
ind:btc_mxn:rsi:14          → {"value": 45.3, "ts": 1712505600}
ind:btc_mxn:bollinger:20    → {"upper": 1195000, "middle": 1190000, "lower": 1185000, "ts": 1712505600}
ind:btc_mxn:atr:14          → {"value": 2500.5, "ts": 1712505600}

# OHLCV bars (for strategies that need candles)
bar:btc_mxn:1m:{timestamp}  → {"o": 1190000, "h": 1191000, "l": 1189500, "c": 1190500, "v": 1.5}
bar:btc_mxn:5m:{timestamp}  → {"o": 1189000, "h": 1192000, "l": 1188000, "c": 1191000, "v": 8.2}
```

---

## Implementation Phases

### Phase 1: Foundation Fixes (Priority: Critical) ✅ COMPLETE

**Goal:** Ensure infrastructure reliability before any strategy development.

**Status:** Completed - Pre-trade validation endpoint added, Redis persistence configured, integration tests created.

#### 1.1 Pre-Trade Risk Alignment

Fix the race condition where trading-engine places orders on Bitso before order-management can reject them.

**Current flow (problematic):**
```
Engine → Bitso.PlaceOrder() → Success → Publish to Kafka → OM rejects (too late!)
```

**Target flow:**
```
Engine → OM.ValidateOrder() → Approved → Bitso.PlaceOrder() → Publish to Kafka
```

**Implementation:**
- Add `POST /api/v1/orders/validate` endpoint to order-management
- Trading-engine calls validation before `PlaceOrder`
- Only proceed to Bitso if validation passes

#### 1.2 Redis Persistence as Default

Update Kubernetes manifests to use `STORAGE_TYPE=redis` for order-management in stage and production environments.

```yaml
# k8s/development/order-management/deployment.yaml
env:
  - name: STORAGE_TYPE
    value: "redis"
  - name: REDIS_HOST
    value: "redis.default.svc.cluster.local"
```

#### 1.3 Integration Test Suite

Create automated tests for the full trading cycle:

```bash
# tests/integration/trading_cycle_test.go
1. Send BUY signal via Kafka
2. Verify order created in order-management
3. Wait for Bitso fill (stage)
4. Verify position updated
5. Send SELL signal
6. Verify P&L calculated correctly
7. Verify Prometheus metrics updated
```

**Files to create:**
- `tests/integration/trading_cycle_test.go`
- `scripts/run-integration-tests.sh`

---

### Phase 2: Indicator Infrastructure ✅ COMPLETE

**Goal:** Provide computed technical indicators for strategies to consume.

**Status:** Completed - All core indicators implemented with Redis storage and HTTP API.

**Implemented Files:**
- `services/strategy-executor/internal/indicators/interface.go` - Indicator interfaces
- `services/strategy-executor/internal/indicators/sma.go` - Simple Moving Average
- `services/strategy-executor/internal/indicators/ema.go` - Exponential Moving Average
- `services/strategy-executor/internal/indicators/rsi.go` - Relative Strength Index
- `services/strategy-executor/internal/indicators/bollinger.go` - Bollinger Bands
- `services/strategy-executor/internal/indicators/atr.go` - Average True Range
- `services/strategy-executor/internal/indicators/vwap.go` - Volume Weighted Average Price
- `services/strategy-executor/internal/indicators/service.go` - Indicator computation service
- `services/strategy-executor/internal/indicators/store.go` - Redis + in-memory storage
- `services/strategy-executor/internal/indicators/data_provider.go` - Market data provider

#### 2.1 Core Indicators

Implement in `services/strategy-executor/internal/indicators/`:

| Indicator | Formula | Use Case |
|-----------|---------|----------|
| SMA | Sum(prices) / N | Trend identification |
| EMA | α × price + (1-α) × prev_ema | Responsive trend |
| RSI | 100 - (100 / (1 + RS)) | Overbought/oversold |
| Bollinger | SMA ± (k × σ) | Volatility bands |
| ATR | SMA of True Range | Position sizing |
| VWAP | Σ(price × volume) / Σ(volume) | Fair value |

#### 2.2 Indicator Computation Service

```go
// internal/indicators/service.go
type IndicatorService struct {
    redis      *redis.Client
    marketData MarketDataClient
    logger     *logger.Logger
}

func (s *IndicatorService) ComputeAndStore(ctx context.Context, book string) error {
    // Fetch recent trades from market-data
    trades, err := s.marketData.GetTrades(ctx, book, since)
    
    // Compute indicators
    sma := s.computeSMA(trades, 20)
    ema := s.computeEMA(trades, 20)
    rsi := s.computeRSI(trades, 14)
    bb := s.computeBollinger(trades, 20, 2.0)
    
    // Store in Redis with TTL
    s.redis.Set(ctx, fmt.Sprintf("ind:%s:sma:20", book), sma, 5*time.Minute)
    // ...
}
```

#### 2.3 Indicator API

Expose indicators via HTTP for backtesting and external consumers:

```
GET /api/v1/indicators/{book}
GET /api/v1/indicators/{book}/{indicator}?period=20
GET /api/v1/indicators/{book}/snapshot  # All indicators at once
```

---

### Phase 3: Strategy Framework ✅ COMPLETE

**Goal:** Create a pluggable framework for implementing and managing strategies.

**Status:** Completed - Strategy interface, registry, and two initial strategies implemented.

**Implemented Files:**
- `services/strategy-executor/internal/strategies/enhanced_strategy.go` - Enhanced strategy interface and base implementation
- `services/strategy-executor/internal/strategies/enhanced_registry.go` - Strategy registry with lifecycle management
- `services/strategy-executor/internal/strategies/mean_reversion.go` - Mean Reversion strategy (Bollinger Bands)
- `services/strategy-executor/internal/strategies/momentum_strategy.go` - Momentum strategy (RSI + EMA)
- `services/strategy-executor/internal/server/http_server.go` - HTTP API for strategy management
- `services/strategy-executor/cmd/main.go` - Full integration with indicators and strategies

**API Endpoints:**
- `GET /api/v1/strategies` - List all strategies
- `POST /api/v1/strategies` - Create new strategy
- `GET /api/v1/strategies/{name}` - Get strategy info
- `DELETE /api/v1/strategies/{name}` - Remove strategy
- `POST /api/v1/strategies/{name}/start` - Start strategy
- `POST /api/v1/strategies/{name}/stop` - Stop strategy
- `GET /api/v1/strategies/{name}/state` - Get strategy state
- `GET /api/v1/strategies/{name}/metrics` - Get strategy metrics
- `GET /api/v1/strategies/types` - List available strategy types
- `GET /api/v1/strategies/stats` - Get registry statistics
- `GET /api/v1/indicators/{book}/snapshot` - Get all indicators for a book

#### 3.1 Strategy Interface and Registry

```go
// internal/strategies/registry.go
type StrategyRegistry struct {
    strategies map[string]Strategy
    configs    map[string]StrategyConfig
    mu         sync.RWMutex
}

func (r *StrategyRegistry) Register(name string, factory StrategyFactory) error
func (r *StrategyRegistry) Get(name string) (Strategy, error)
func (r *StrategyRegistry) List() []StrategyInfo
func (r *StrategyRegistry) Enable(name string) error
func (r *StrategyRegistry) Disable(name string) error
```

#### 3.2 Mean Reversion Strategy (First Implementation)

```go
// internal/strategies/mean_reversion.go
type MeanReversionStrategy struct {
    config     MeanReversionConfig
    indicators *indicators.Service
    state      *StrategyState
    logger     *logger.Logger
}

func (s *MeanReversionStrategy) OnTick(tick *models.TradeEvent) (*Signal, error) {
    // Get current indicators
    bb, err := s.indicators.GetBollinger(tick.Book, s.config.LookbackPeriod)
    if err != nil {
        return nil, err
    }
    
    price := tick.Price
    
    // Entry logic: price outside bands
    if price < bb.Lower && !s.state.HasPosition {
        return &Signal{
            Side:       "BUY",
            Amount:     s.config.PositionSize,
            Price:      price,
            Reason:     fmt.Sprintf("Price %.2f below lower band %.2f", price, bb.Lower),
            Confidence: s.calculateConfidence(price, bb),
        }, nil
    }
    
    if price > bb.Upper && !s.state.HasPosition {
        return &Signal{
            Side:       "SELL",
            Amount:     s.config.PositionSize,
            Price:      price,
            Reason:     fmt.Sprintf("Price %.2f above upper band %.2f", price, bb.Upper),
            Confidence: s.calculateConfidence(price, bb),
        }, nil
    }
    
    // Exit logic: price returns to mean
    if s.state.HasPosition && math.Abs(price-bb.Middle) < s.config.ExitThreshold*bb.StdDev {
        return &Signal{
            Side:       s.exitSide(),
            Amount:     s.state.PositionSize,
            Price:      price,
            Reason:     "Price returned to mean",
            Confidence: 0.8,
        }, nil
    }
    
    return nil, nil // No signal
}
```

#### 3.3 Strategy Executor Integration

Wire strategies into the existing strategy-executor service:

```go
// cmd/main.go additions
func main() {
    // ... existing setup ...
    
    // Initialize indicator service
    indicatorSvc := indicators.NewService(redisClient, marketDataClient, logger)
    
    // Initialize strategy registry
    registry := strategies.NewRegistry(logger)
    
    // Register strategies
    registry.Register("mean_reversion", strategies.NewMeanReversionFactory(indicatorSvc))
    registry.Register("momentum", strategies.NewMomentumFactory(indicatorSvc))
    
    // Load and enable configured strategies
    for _, cfg := range config.Strategies {
        if err := registry.LoadConfig(cfg); err != nil {
            logger.Error("Failed to load strategy config", "name", cfg.Name, "error", err)
            continue
        }
        if cfg.Enabled {
            registry.Enable(cfg.Name)
        }
    }
    
    // Start strategy loop
    go strategyLoop(ctx, registry, signalPublisher)
}
```

---

### Phase 4: Backtest Integration 🔄 PENDING

**Goal:** Ensure strategy code works identically in backtest and live modes.

#### 4.1 Unified Strategy Code

**Critical principle:** The same strategy implementation must be used for both backtesting and live trading. This prevents the common industry pitfall of backtest/live divergence.

```go
// Strategy interface works for both modes
type DataProvider interface {
    GetTrades(ctx context.Context, book string, from, to time.Time) ([]models.TradeEvent, error)
    GetIndicator(ctx context.Context, book, indicator string, period int) (float64, error)
}

// Live provider
type LiveDataProvider struct {
    marketData MarketDataClient
    indicators *indicators.Service
}

// Backtest provider
type BacktestDataProvider struct {
    trades     []models.TradeEvent
    indicators map[string][]float64
    cursor     int
}
```

#### 4.2 Backtest Workflow Scripts

```bash
#!/bin/bash
# scripts/backtest-strategy.sh
# Usage: ./scripts/backtest-strategy.sh <strategy> <book> <start_date> <end_date>

STRATEGY=$1
BOOK=$2
START_DATE=$3
END_DATE=$4

echo "Running backtest: $STRATEGY on $BOOK from $START_DATE to $END_DATE"

# Create backtest via API
BACKTEST_ID=$(curl -s -X POST http://localhost:8084/api/v1/backtests \
  -H "Content-Type: application/json" \
  -d "{
    \"book\": \"$BOOK\",
    \"strategy\": \"$STRATEGY\",
    \"start_date\": \"$START_DATE\",
    \"end_date\": \"$END_DATE\",
    \"initial_balance\": 100000
  }" | jq -r '.id')

echo "Backtest ID: $BACKTEST_ID"

# Poll until complete
while true; do
  STATUS=$(curl -s "http://localhost:8084/api/v1/backtests/$BACKTEST_ID" | jq -r '.status')
  echo "Status: $STATUS"
  if [ "$STATUS" = "completed" ] || [ "$STATUS" = "failed" ]; then
    break
  fi
  sleep 5
done

# Get results
curl -s "http://localhost:8084/api/v1/backtests/$BACKTEST_ID/report?format=text"
```

#### 4.3 Backtest → Stage Promotion

```bash
#!/bin/bash
# scripts/promote-strategy-to-stage.sh
# Usage: ./scripts/promote-strategy-to-stage.sh <backtest_id> <min_sharpe> <max_dd>

BACKTEST_ID=$1
MIN_SHARPE=${2:-1.0}
MAX_DD=${3:-10}

# Fetch backtest results
RESULTS=$(curl -s "http://localhost:8084/api/v1/backtests/$BACKTEST_ID/results")

SHARPE=$(echo $RESULTS | jq -r '.sharpe_ratio')
MAX_DRAWDOWN=$(echo $RESULTS | jq -r '.max_drawdown_pct')
STRATEGY=$(echo $RESULTS | jq -r '.strategy')

echo "Backtest Results:"
echo "  Sharpe Ratio: $SHARPE (min: $MIN_SHARPE)"
echo "  Max Drawdown: $MAX_DRAWDOWN% (max: $MAX_DD%)"

# Validation gate
if (( $(echo "$SHARPE < $MIN_SHARPE" | bc -l) )); then
  echo "FAILED: Sharpe ratio below minimum"
  exit 1
fi

if (( $(echo "$MAX_DRAWDOWN > $MAX_DD" | bc -l) )); then
  echo "FAILED: Max drawdown exceeds limit"
  exit 1
fi

echo "PASSED: Strategy approved for stage deployment"
echo "Deploy with: kubectl apply -f k8s/development/strategies/$STRATEGY.yaml"
```

---

## First Strategy: Mean Reversion

### Why Mean Reversion First?

| Reason | Benefit |
|--------|---------|
| Simple mathematics | Easy to validate and debug |
| Well-understood behavior | Extensive academic literature |
| Works in range-bound markets | Crypto often trades in ranges intraday |
| Clear entry/exit rules | No ambiguity in signal generation |
| Good baseline | Compare all future strategies against it |

### Strategy Logic

```
Entry Conditions:
  LONG:  Price < Lower Bollinger Band (SMA - k*σ)
  SHORT: Price > Upper Bollinger Band (SMA + k*σ)

Exit Conditions:
  Price returns within ExitThreshold * σ of the SMA

Position Sizing:
  Fixed size for initial implementation
  Later: Kelly criterion or volatility-adjusted
```

### Parameter Grid for Testing

```yaml
# Conservative (start here)
conservative:
  lookback_period: 20
  entry_threshold: 2.5    # Wider bands = fewer trades
  exit_threshold: 0.5
  position_size: 0.001    # Minimum BTC

# Moderate
moderate:
  lookback_period: 20
  entry_threshold: 2.0
  exit_threshold: 0.5
  position_size: 0.005

# Aggressive
aggressive:
  lookback_period: 10     # Faster response
  entry_threshold: 1.5    # Tighter bands = more trades
  exit_threshold: 0.3
  position_size: 0.01
```

### Backtest Checklist

Before promoting to stage, verify:

- [ ] Sharpe Ratio > 1.0
- [ ] Max Drawdown < 10%
- [ ] Win Rate > 45%
- [ ] Profit Factor > 1.2
- [ ] Average trade duration reasonable for intraday
- [ ] No obvious overfitting (test on out-of-sample data)

---

## Stage Observation Protocol

### Minimum Observation Period

**Recommended: 2-4 weeks minimum** before any production consideration.

### Daily Checklist

```markdown
## Daily Stage Observation Report

Date: ____________________
Strategy: ________________
Book: ____________________

### Metrics Comparison

| Metric | Backtest | Stage Today | Stage Cumulative | Drift |
|--------|----------|-------------|------------------|-------|
| Trades | | | | |
| Win Rate | | | | |
| P&L | | | | |
| Max DD | | | | |
| Sharpe | | | | |

### Observations
- Market conditions: _______________
- Any anomalies: __________________
- System issues: __________________

### Action Items
- [ ] Continue observation
- [ ] Adjust parameters
- [ ] Pause strategy
- [ ] Escalate issue
```

### Automated Monitoring

```yaml
# Prometheus alerts for stage observation
groups:
  - name: strategy_observation
    rules:
      - alert: StrategyDrawdownExceeded
        expr: trading_drawdown_percent > 1.5 * on(strategy) group_left backtest_max_drawdown
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Strategy {{ $labels.strategy }} drawdown exceeds 1.5x backtest"
          
      - alert: StrategyDailyLossExceeded
        expr: trading_daily_realized_pnl_currency < -2 * on(strategy) group_left backtest_worst_day
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Strategy {{ $labels.strategy }} daily loss exceeds 2x backtest worst day"
```

---

## Validation Gates

### Gate 1: Backtest Approval

| Metric | Minimum | Target | Reject If |
|--------|---------|--------|-----------|
| Sharpe Ratio | 1.0 | > 1.5 | < 0.5 |
| Max Drawdown | < 15% | < 10% | > 20% |
| Win Rate | > 40% | > 50% | < 35% |
| Profit Factor | > 1.1 | > 1.3 | < 1.0 |
| Total Trades | > 50 | > 200 | < 20 |

### Gate 2: Stage Promotion

| Metric | Acceptable Drift from Backtest |
|--------|-------------------------------|
| Sharpe Ratio | ±30% |
| Max Drawdown | +50% (worse is ok up to limit) |
| Win Rate | ±15% |
| Avg Trade Duration | ±40% |
| Trades per Day | ±30% |

### Gate 3: Production Consideration

- [ ] Minimum 2 weeks on stage with acceptable metrics
- [ ] No circuit breaker triggers
- [ ] Manual review of all edge cases
- [ ] Risk committee approval (if applicable)
- [ ] Documented rollback procedure
- [ ] Monitoring and alerting verified

---

## Risk Management

### Strategy-Level Controls

```yaml
risk:
  # Loss limits
  max_daily_loss: 500           # MXN - stop trading for the day
  max_weekly_loss: 2000         # MXN - pause for review
  max_drawdown_pct: 10          # Percent from peak
  
  # Trade limits
  max_trades_per_day: 50
  max_trades_per_hour: 10
  max_consecutive_losses: 5     # Pause and review
  
  # Position limits
  max_position_size: 0.01       # BTC
  max_position_value: 15000     # MXN
  max_open_positions: 3
  
  # Timing
  min_time_between_trades: 60   # Seconds
  cooldown_after_loss: 300      # Seconds after a losing trade
```

### Kill Switch Implementation

```go
// internal/risk/kill_switch.go
type KillSwitch struct {
    triggered    atomic.Bool
    reason       string
    triggeredAt  time.Time
    mu           sync.Mutex
}

func (k *KillSwitch) Check(metrics StrategyMetrics) bool {
    if k.triggered.Load() {
        return false // Already triggered
    }
    
    // Check conditions
    if metrics.DailyLoss > metrics.MaxDailyLoss {
        k.Trigger("Daily loss limit exceeded")
        return false
    }
    
    if metrics.DrawdownPct > metrics.MaxDrawdownPct {
        k.Trigger("Drawdown limit exceeded")
        return false
    }
    
    if metrics.ConsecutiveLosses > metrics.MaxConsecutiveLosses {
        k.Trigger("Consecutive losses limit exceeded")
        return false
    }
    
    return true // OK to trade
}

func (k *KillSwitch) Trigger(reason string) {
    k.mu.Lock()
    defer k.mu.Unlock()
    
    k.triggered.Store(true)
    k.reason = reason
    k.triggeredAt = time.Now()
    
    // Alert
    alerting.Send(alerting.Critical, "Kill switch triggered", reason)
    
    // Log
    logger.Error("KILL SWITCH TRIGGERED", "reason", reason)
}
```

### Circuit Breakers

| Condition | Action | Auto-Reset |
|-----------|--------|------------|
| Daily loss > limit | Pause trading for day | Next trading day |
| Drawdown > limit | Pause until manual review | Manual only |
| 5 consecutive losses | Pause 1 hour | After cooldown |
| API errors > 5/min | Pause 5 minutes | After cooldown |
| Latency > 5s | Pause until resolved | When latency normal |

---

## Timeline Estimate

| Phase | Duration | Deliverables |
|-------|----------|--------------|
| **Phase 1: Foundation** | 2-3 days | Pre-trade validation, Redis default, integration tests |
| **Phase 2: Indicators** | 3-4 days | Core indicators (SMA, EMA, RSI, Bollinger, ATR), Redis storage, API |
| **Phase 3: Framework** | 4-5 days | Strategy interface, mean reversion impl, registry, config loading |
| **Phase 4: Backtest** | 2-3 days | Unified code, promotion workflow, validation scripts |
| **Phase 5: Observation** | 2-4 weeks | Stage deployment, daily monitoring, tuning |

**Total implementation time:** ~2-3 weeks  
**Total validation time:** ~2-4 weeks additional

---

## Scripts Reference

| Script | Purpose |
|--------|---------|
| `scripts/backtest-strategy.sh` | Run backtest for a strategy |
| `scripts/validate-backtest.sh` | Check if backtest passes gates |
| `scripts/promote-strategy-to-stage.sh` | Deploy approved strategy to stage |
| `scripts/compare-stage-vs-backtest.sh` | Compare stage results to backtest |
| `scripts/strategy-daily-report.sh` | Generate daily observation report |
| `scripts/kill-switch-status.sh` | Check kill switch state |

---

## Next Steps Summary

### Immediate Actions (Testing Phase)

| Step | Status | Command/Action |
|------|--------|----------------|
| **Unit Tests** | ✅ Created | `go test -v ./services/strategy-executor/internal/...` |
| **Integration Tests** | 📋 Existing | `./scripts/run-integration-tests.sh --local` |
| **Strategy Executor Dashboard** | ✅ Created | `./scripts/grafana-import-dashboard.sh strategy-executor` |

### Phase 4 Work (Next Phase)

| Component | Purpose |
|-----------|---------|
| `BacktestDataProvider` | Unified interface for backtest/live modes |
| `scripts/backtest-strategy.sh` | Run backtests via API |
| `scripts/promote-strategy-to-stage.sh` | Validate backtest metrics before promotion |
| Backtest API endpoints | `/api/v1/backtests` CRUD operations |

---

## Grafana Dashboards to Monitor

### 1. Strategy Executor (NEW) - `services/strategy-executor.json`

Import: `./scripts/grafana-import-dashboard.sh strategy-executor`

Key panels:
- Service health & active strategies count
- Signal generation rate by strategy
- Indicator computation health
- Win rate, P&L per strategy
- Consecutive losses (kill switch warning)
- Real-time Bollinger Bands, RSI, EMA/SMA

### 2. Trading Platform Metrics - `domain/trading-metrics.json`

Import: `./scripts/grafana-import-dashboard.sh trading-metrics`

Monitor during testing:
- Daily Realized/Unrealized P&L
- Drawdown % (should stay < 10%)
- Trades today, Win Rate
- Session risk rejections

### 3. Financial Indicators - `domain/financial-indicators.json`

Import: `./scripts/grafana-import-dashboard.sh financial-indicators`

Verify indicator values:
- RSI (oversold/overbought zones)
- Bollinger Bands (upper/middle/lower)
- VWAP deviation
- Volume spikes

### 4. Trading Engine - `services/trading-engine.json`

Import: `./scripts/grafana-import-dashboard.sh trading-engine`

Monitor signal flow:
- Engine state (running)
- Signals processed rate
- Orders failed by reason

---

## Running Tests

```bash
# Unit tests (fast, local)
cd services/strategy-executor
go test -v ./internal/strategies/... ./internal/indicators/...

# Integration tests (requires services running)
./scripts/run-integration-tests.sh --local

# Full test with race detection
go test -race -v ./...
```

---

## Key Metrics to Watch During Testing

| Metric | Target | Alert If |
|--------|--------|----------|
| `strategy_executor_signals_generated_total` | Steady rate | Flat (no signals) |
| `strategy_executor_strategy_win_rate` | > 45% | < 35% |
| `strategy_executor_consecutive_losses` | < 3 | >= 5 (kill switch) |
| `trading_drawdown_percent` | < 5% | > 10% |
| `trading_daily_realized_pnl_currency` | Positive | Large negative |

---

## References

- [INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md](../INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md) - Infrastructure phases
- [PNL-DEBUGGING-AND-FIXES.md](PNL-DEBUGGING-AND-FIXES.md) - P&L calculation fixes
- [BACKTESTING-METRICS-AND-EXPORT.md](../BACKTESTING-METRICS-AND-EXPORT.md) - Backtest export and metrics
- [Bitso API Documentation](https://docs.bitso.com/bitso-api/docs/api-overview)
- [Bitso Testing Environment](https://docs.bitso.com/bitso-api/docs/set-up-your-testing-environment)

---

## Appendix: Quick Start Checklist

```markdown
## Before Starting Strategy Development

- [ ] Phase 1-6 of INTRADAY-STRATEGY-IMPLEMENTATION-PLAN complete
- [ ] P&L fixes from PNL-DEBUGGING-AND-FIXES applied
- [ ] Redis persistence enabled for order-management
- [ ] Market-data collecting stage WebSocket trades
- [ ] Grafana dashboards showing live metrics
- [ ] Integration tests passing

## For Each New Strategy

1. [ ] Define hypothesis and parameters
2. [ ] Implement strategy (reuse indicator infrastructure)
3. [ ] Run backtest on historical data
4. [ ] Validate against gates (Sharpe > 1.0, DD < 10%)
5. [ ] Deploy to stage with conservative parameters
6. [ ] Monitor for 2+ weeks
7. [ ] Compare stage vs backtest metrics
8. [ ] Document findings and adjustments
9. [ ] (Optional) Proceed to production consideration
```
