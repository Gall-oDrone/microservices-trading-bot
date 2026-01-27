# Trading Bot Development Roadmap

This document outlines the recommended next steps for the trading bot application development.

## Current State

| Component | Status | Description |
|-----------|--------|-------------|
| **Strategies** | Basic | 3 strategies: basic, trend, arbitrage |
| **Risk Management** | Basic | Trade limits, stop-loss, take-profit |
| **Order Execution** | Basic | Places limit orders on Bitso |
| **Backtesting** | Good | Full framework with optimizer |
| **Monitoring** | Good | Prometheus metrics, Grafana dashboards |
| **Infrastructure** | Complete | EKS deployment with all phases (1-10) completed |

---

## Recommended Next Steps

### Phase 1: Enhanced Trading Strategies

**Priority:** High  
**Effort:** Medium

The current `basic_strategy.go` uses simple spread-based logic. Implement more sophisticated strategies:

- **Moving Averages**: SMA/EMA crossover strategies
- **RSI-based Mean Reversion**: Buy oversold, sell overbought
- **VWAP Strategies**: Volume-weighted average price tracking
- **Order Book Imbalance**: Detect supply/demand imbalances
- **Momentum Strategies**: Trend-following based on price momentum

**Files to modify:**
- `services/strategy-executor/internal/strategies/`

---

### Phase 2: Paper Trading Mode

**Priority:** High  
**Effort:** Low

Add a simulation mode that logs trades without placing real orders. Essential for testing new strategies safely.

**Implementation:**

```go
// In executor.go - add dry-run mode
type ExecutorConfig struct {
    DryRun      bool    // If true, log but don't execute
    SimBalance  float64 // Simulated account balance
}

func (e *BasicExecutor) ExecuteBuySignal(signal TradingSignal) error {
    if e.config.DryRun {
        e.logger.Printf("[DRY-RUN] Would execute BUY: %s at %.8f", signal.Book, signal.Price)
        return nil
    }
    // ... actual execution
}
```

**Files to modify:**
- `services/trading-engine/internal/execution/executor.go`
- `services/trading-engine/internal/config/config.go`

---

### Phase 3: Position Management

**Priority:** High  
**Effort:** Medium

Track open positions and P&L in real-time:

- Current positions per trading pair (book)
- Unrealized P&L calculation
- Position sizing based on account balance
- Position history and trade log

**Features:**
- [ ] Real-time position tracking in Redis
- [ ] P&L calculation service
- [ ] Position size calculator (Kelly criterion, fixed fractional)
- [ ] Trade journal with entry/exit logging

**Files to modify:**
- `services/order-management/internal/repository/position_repository.go`
- `services/order-management/internal/models/position.go`

---

### Phase 4: Enhanced Risk Controls

**Priority:** High  
**Effort:** Medium

Implement advanced risk management features:

| Control | Description |
|---------|-------------|
| **Daily Loss Limit** | Stop trading if daily loss exceeds threshold |
| **Max Drawdown** | Halt trading if drawdown exceeds X% |
| **Position Correlation** | Limit exposure to correlated assets |
| **Volatility Circuit Breaker** | Pause during extreme volatility |
| **Order Rate Limiting** | Prevent excessive order placement |

**Implementation example:**

```go
type RiskLimits struct {
    MaxDailyLoss      float64 // e.g., 500 MXN
    MaxDrawdownPct    float64 // e.g., 10%
    MaxPositionSize   float64 // e.g., 0.1 BTC
    MaxOpenPositions  int     // e.g., 5
    OrderRateLimit    int     // orders per minute
}
```

**Files to modify:**
- `services/strategy-executor/internal/risk/manager.go`
- `services/order-management/internal/risk/risk_manager.go`

---

### Phase 5: API Authentication

**Priority:** Medium  
**Effort:** Medium

Add JWT/API key authentication to the API Gateway for secure external access.

**Features:**
- [ ] JWT token generation and validation
- [ ] API key management
- [ ] Role-based access control (RBAC)
- [ ] Rate limiting per API key

**Files to modify:**
- `services/api-gateway/cmd/main.go`
- Create: `services/api-gateway/internal/auth/`

---

### Phase 6: Trading Dashboard (Frontend)

**Priority:** Medium  
**Effort:** High

Build a web UI for monitoring and management:

**Features:**
- [ ] Live positions and P&L display
- [ ] Strategy performance charts
- [ ] Order history table
- [ ] Strategy parameter configuration
- [ ] Account balance and equity curve
- [ ] Trade alerts and notifications

**Tech Stack Options:**
- React + TypeScript
- Next.js
- Vue.js

**New service:**
- `services/dashboard/` (or separate repo)

---

### Phase 7: Additional Trading Pairs

**Priority:** Medium  
**Effort:** Low

Expand beyond `btc_mxn` to other Bitso trading pairs:

**Available pairs:**
- `eth_mxn` - Ethereum/MXN
- `xrp_mxn` - Ripple/MXN
- `ltc_mxn` - Litecoin/MXN
- `btc_usd` - Bitcoin/USD
- `eth_btc` - Ethereum/Bitcoin

**Files to modify:**
- `shared/pkg/config/config.go`
- `k8s/base/configmap.yaml`

---

### Phase 8: Advanced Alerting

**Priority:** Medium  
**Effort:** Low

Implement trading-specific alerts:

| Alert Type | Trigger |
|------------|---------|
| **P&L Alert** | Daily P&L exceeds threshold |
| **Position Alert** | Position size exceeds limit |
| **Strategy Error** | Strategy execution failure |
| **Order Failure** | Order placement/execution error |
| **Connectivity** | Exchange API disconnection |

**Integration options:**
- Slack webhooks
- Email (AWS SES)
- PagerDuty
- Telegram bot

**Files to modify:**
- `monitoring/prometheus/rules/trading-alerts.yml`
- Create: `services/notification/`

---

## Quick Wins (Immediate Implementation)

These can be implemented quickly with minimal effort:

### 1. Add More Trading Pairs

Update the ConfigMap:

```yaml
# k8s/base/configmap.yaml
data:
  bitso-books: "btc_mxn,eth_mxn,xrp_mxn"
```

### 2. Tune Strategy Parameters

Adjust in `basic_strategy.go`:

```go
func NewBasicStrategy(book *bitso.Book) *BasicStrategy {
    return &BasicStrategy{
        tradeInterval: 5 * time.Minute,  // Adjust frequency
        profitTarget:  0.015,            // 1.5% profit target
        stopLoss:      0.008,            // 0.8% stop loss
    }
}
```

### 3. Enable Dry-Run Mode

Add environment variable:

```yaml
# k8s/base/trading-engine.yaml
env:
  - name: DRY_RUN
    value: "true"
```

---

## Development Timeline Suggestion

| Phase | Priority | Estimated Effort | Dependencies |
|-------|----------|------------------|--------------|
| Phase 1: Strategies | High | 2-3 weeks | None |
| Phase 2: Paper Trading | High | 1 week | None |
| Phase 3: Position Mgmt | High | 2 weeks | Phase 2 |
| Phase 4: Risk Controls | High | 2 weeks | Phase 3 |
| Phase 5: API Auth | Medium | 1-2 weeks | None |
| Phase 6: Dashboard | Medium | 4-6 weeks | Phase 3, 5 |
| Phase 7: Trading Pairs | Medium | 1-2 days | None |
| Phase 8: Alerting | Medium | 1 week | Phase 4 |

---

## Architecture Considerations

### For High-Frequency Trading
If moving towards HFT:
- Consider co-location with exchange
- Optimize network latency
- Use binary protocols instead of JSON
- Implement order book caching

### For Multi-Exchange
If expanding to multiple exchanges:
- Abstract exchange interface
- Implement exchange adapters
- Cross-exchange arbitrage strategies

---

**Document Version:** 1.0  
**Created:** January 27, 2026  
**Status:** Planning
