package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// PrometheusMetrics holds Prometheus-compatible metrics for the strategy executor
type PrometheusMetrics struct {
	mu sync.RWMutex

	// Service metrics
	serviceUp            float64
	activeStrategies     float64
	indicatorsHealthy    float64
	indicatorsLastUpdate float64

	// Strategy metrics (keyed by strategy name)
	strategyRunning          map[string]float64
	strategyWinRate          map[string]float64
	strategyPnLTotal         map[string]float64
	strategyConsecutiveLoss  map[string]float64
	strategySignalsGenerated map[string]map[string]float64 // strategy -> side -> count

	// Indicator metrics (keyed by book)
	indicatorSMA             map[string]float64
	indicatorEMA             map[string]float64
	indicatorRSI             map[string]float64
	indicatorBollingerUpper  map[string]float64
	indicatorBollingerMiddle map[string]float64
	indicatorBollingerLower  map[string]float64
	indicatorATR             map[string]float64
	indicatorVWAP            map[string]float64

	// limit_profit strategy metrics (keyed by "strategy:book")
	lpEntrySignals          map[string]float64            // counter
	lpExitSignals           map[string]map[string]float64 // key -> reason -> count
	lpPendingBuyDuration    map[string][]float64          // histogram buckets
	lpPositionHoldDuration  map[string][]float64          // histogram buckets
	lpPendingCancelFailures map[string]float64            // counter
	lpDailyRealizedPnL      map[string]float64            // gauge
	lpCircuitBreakerActive  map[string]float64            // gauge (0 or 1)

	// momentum strategy metrics (keyed by "strategy:book")
	momEntrySignals         map[string]map[string]float64 // key -> side -> count
	momExitSignals          map[string]map[string]float64 // key -> reason -> count
	momPositionHoldDuration map[string][]float64          // observations
	momDailyRealizedPnL     map[string]float64            // gauge
	momCircuitBreakerActive map[string]float64            // gauge (0 or 1)

	// Fee honesty metrics (POINT-9 §7) — realized vs assumed fee per book/side/liquidity.
	// Keyed by "book|side|liquidity" unless noted.
	feeRealizedSum   map[string]float64 // sum of realized fee rates (for summary avg)
	feeRealizedCount map[string]float64 // count of realized fee observations
	feeRealizedLast  map[string]float64 // last realized fee rate
	feeAssumedRate   map[string]float64 // assumed fee rate, keyed by "book|liquidity"
	feeDriftRatio    map[string]float64 // realized/assumed, keyed by "book|side|liquidity"
}

// NewPrometheusMetrics creates a new PrometheusMetrics instance
func NewPrometheusMetrics() *PrometheusMetrics {
	return &PrometheusMetrics{
		serviceUp:                1,
		strategyRunning:          make(map[string]float64),
		strategyWinRate:          make(map[string]float64),
		strategyPnLTotal:         make(map[string]float64),
		strategyConsecutiveLoss:  make(map[string]float64),
		strategySignalsGenerated: make(map[string]map[string]float64),
		indicatorSMA:             make(map[string]float64),
		indicatorEMA:             make(map[string]float64),
		indicatorRSI:             make(map[string]float64),
		indicatorBollingerUpper:  make(map[string]float64),
		indicatorBollingerMiddle: make(map[string]float64),
		indicatorBollingerLower:  make(map[string]float64),
		indicatorATR:             make(map[string]float64),
		indicatorVWAP:            make(map[string]float64),
		// limit_profit metrics
		lpEntrySignals:          make(map[string]float64),
		lpExitSignals:           make(map[string]map[string]float64),
		lpPendingBuyDuration:    make(map[string][]float64),
		lpPositionHoldDuration:  make(map[string][]float64),
		lpPendingCancelFailures: make(map[string]float64),
		lpDailyRealizedPnL:      make(map[string]float64),
		lpCircuitBreakerActive:  make(map[string]float64),
		// momentum metrics
		momEntrySignals:         make(map[string]map[string]float64),
		momExitSignals:          make(map[string]map[string]float64),
		momPositionHoldDuration: make(map[string][]float64),
		momDailyRealizedPnL:     make(map[string]float64),
		momCircuitBreakerActive: make(map[string]float64),
		// fee honesty metrics
		feeRealizedSum:   make(map[string]float64),
		feeRealizedCount: make(map[string]float64),
		feeRealizedLast:  make(map[string]float64),
		feeAssumedRate:   make(map[string]float64),
		feeDriftRatio:    make(map[string]float64),
	}
}

// SetServiceUp sets the service up status
func (m *PrometheusMetrics) SetServiceUp(up bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if up {
		m.serviceUp = 1
	} else {
		m.serviceUp = 0
	}
}

// SetActiveStrategies sets the number of active strategies
func (m *PrometheusMetrics) SetActiveStrategies(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeStrategies = float64(count)
}

// SetIndicatorsHealthy sets the indicators health status
func (m *PrometheusMetrics) SetIndicatorsHealthy(healthy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if healthy {
		m.indicatorsHealthy = 1
	} else {
		m.indicatorsHealthy = 0
	}
	m.indicatorsLastUpdate = float64(time.Now().Unix())
}

// SetStrategyRunning sets whether a strategy is running
func (m *PrometheusMetrics) SetStrategyRunning(strategy string, running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if running {
		m.strategyRunning[strategy] = 1
	} else {
		m.strategyRunning[strategy] = 0
	}
}

// SetStrategyWinRate sets the win rate for a strategy
func (m *PrometheusMetrics) SetStrategyWinRate(strategy string, rate float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.strategyWinRate[strategy] = rate
}

// SetStrategyPnL sets the total P&L for a strategy
func (m *PrometheusMetrics) SetStrategyPnL(strategy string, pnl float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.strategyPnLTotal[strategy] = pnl
}

// SetStrategyConsecutiveLosses sets the consecutive losses for a strategy
func (m *PrometheusMetrics) SetStrategyConsecutiveLosses(strategy string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.strategyConsecutiveLoss[strategy] = float64(count)
}

// IncSignalsGenerated increments the signals generated counter
func (m *PrometheusMetrics) IncSignalsGenerated(strategy, side string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.strategySignalsGenerated[strategy] == nil {
		m.strategySignalsGenerated[strategy] = make(map[string]float64)
	}
	m.strategySignalsGenerated[strategy][side]++
}

// SetIndicatorSMA sets the SMA indicator value
func (m *PrometheusMetrics) SetIndicatorSMA(book string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indicatorSMA[book] = value
}

// SetIndicatorEMA sets the EMA indicator value
func (m *PrometheusMetrics) SetIndicatorEMA(book string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indicatorEMA[book] = value
}

// SetIndicatorRSI sets the RSI indicator value
func (m *PrometheusMetrics) SetIndicatorRSI(book string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indicatorRSI[book] = value
}

// SetIndicatorBollinger sets the Bollinger Bands indicator values
func (m *PrometheusMetrics) SetIndicatorBollinger(book string, upper, middle, lower float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indicatorBollingerUpper[book] = upper
	m.indicatorBollingerMiddle[book] = middle
	m.indicatorBollingerLower[book] = lower
}

// SetIndicatorATR sets the ATR indicator value
func (m *PrometheusMetrics) SetIndicatorATR(book string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indicatorATR[book] = value
}

// SetIndicatorVWAP sets the VWAP indicator value
func (m *PrometheusMetrics) SetIndicatorVWAP(book string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indicatorVWAP[book] = value
}

// RemoveStrategy removes all metrics for a strategy
func (m *PrometheusMetrics) RemoveStrategy(strategy string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.strategyRunning, strategy)
	delete(m.strategyWinRate, strategy)
	delete(m.strategyPnLTotal, strategy)
	delete(m.strategyConsecutiveLoss, strategy)
	delete(m.strategySignalsGenerated, strategy)
}

// limit_profit strategy metric methods

func lpKey(strategy, book string) string {
	return strategy + ":" + book
}

// IncLimitProfitEntrySignals increments the entry signals counter
func (m *PrometheusMetrics) IncLimitProfitEntrySignals(strategy, book string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := lpKey(strategy, book)
	m.lpEntrySignals[key]++
}

// IncLimitProfitExitSignals increments the exit signals counter by reason
func (m *PrometheusMetrics) IncLimitProfitExitSignals(strategy, book, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := lpKey(strategy, book)
	if m.lpExitSignals[key] == nil {
		m.lpExitSignals[key] = make(map[string]float64)
	}
	m.lpExitSignals[key][reason]++
}

// RecordLimitProfitPendingBuyDuration records the pending buy duration
func (m *PrometheusMetrics) RecordLimitProfitPendingBuyDuration(strategy, book string, seconds float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := lpKey(strategy, book)
	m.lpPendingBuyDuration[key] = append(m.lpPendingBuyDuration[key], seconds)
	// Keep last 1000 observations
	if len(m.lpPendingBuyDuration[key]) > 1000 {
		m.lpPendingBuyDuration[key] = m.lpPendingBuyDuration[key][1:]
	}
}

// RecordLimitProfitPositionHoldDuration records the position hold duration
func (m *PrometheusMetrics) RecordLimitProfitPositionHoldDuration(strategy, book string, seconds float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := lpKey(strategy, book)
	m.lpPositionHoldDuration[key] = append(m.lpPositionHoldDuration[key], seconds)
	// Keep last 1000 observations
	if len(m.lpPositionHoldDuration[key]) > 1000 {
		m.lpPositionHoldDuration[key] = m.lpPositionHoldDuration[key][1:]
	}
}

// IncLimitProfitPendingCancelFailures increments the pending cancel failures counter
func (m *PrometheusMetrics) IncLimitProfitPendingCancelFailures(strategy, book string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := lpKey(strategy, book)
	m.lpPendingCancelFailures[key]++
}

// SetLimitProfitDailyRealizedPnL sets the daily realized P&L gauge
func (m *PrometheusMetrics) SetLimitProfitDailyRealizedPnL(strategy, book string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := lpKey(strategy, book)
	m.lpDailyRealizedPnL[key] = value
}

// SetLimitProfitCircuitBreakerActive sets the circuit breaker active gauge
func (m *PrometheusMetrics) SetLimitProfitCircuitBreakerActive(strategy, book string, active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := lpKey(strategy, book)
	if active {
		m.lpCircuitBreakerActive[key] = 1
	} else {
		m.lpCircuitBreakerActive[key] = 0
	}
}

// momentum strategy metric methods

// IncMomentumEntrySignals increments the momentum entry signals counter by side.
func (m *PrometheusMetrics) IncMomentumEntrySignals(strategy, book, side string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := lpKey(strategy, book)
	if m.momEntrySignals[key] == nil {
		m.momEntrySignals[key] = make(map[string]float64)
	}
	m.momEntrySignals[key][side]++
}

// IncMomentumExitSignals increments the momentum exit signals counter by reason.
func (m *PrometheusMetrics) IncMomentumExitSignals(strategy, book, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := lpKey(strategy, book)
	if m.momExitSignals[key] == nil {
		m.momExitSignals[key] = make(map[string]float64)
	}
	m.momExitSignals[key][reason]++
}

// RecordMomentumPositionHoldDuration records a momentum position hold duration.
func (m *PrometheusMetrics) RecordMomentumPositionHoldDuration(strategy, book string, seconds float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := lpKey(strategy, book)
	m.momPositionHoldDuration[key] = append(m.momPositionHoldDuration[key], seconds)
	if len(m.momPositionHoldDuration[key]) > 1000 {
		m.momPositionHoldDuration[key] = m.momPositionHoldDuration[key][1:]
	}
}

// SetMomentumDailyRealizedPnL sets the momentum session realized P&L gauge.
func (m *PrometheusMetrics) SetMomentumDailyRealizedPnL(strategy, book string, value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.momDailyRealizedPnL[lpKey(strategy, book)] = value
}

// SetMomentumCircuitBreakerActive sets the momentum circuit breaker gauge.
func (m *PrometheusMetrics) SetMomentumCircuitBreakerActive(strategy, book string, active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := lpKey(strategy, book)
	if active {
		m.momCircuitBreakerActive[key] = 1
	} else {
		m.momCircuitBreakerActive[key] = 0
	}
}

// Fee honesty metric methods (POINT-9 §7)

func feeKey(book, side, liquidity string) string {
	return book + "|" + side + "|" + liquidity
}

// RecordRealizedFeeRate records a realized fee rate observation from a fill.
func (m *PrometheusMetrics) RecordRealizedFeeRate(book, side, liquidity string, rate float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := feeKey(book, side, liquidity)
	m.feeRealizedSum[key] += rate
	m.feeRealizedCount[key]++
	m.feeRealizedLast[key] = rate
}

// SetAssumedFeeRate sets the configured/assumed fee rate for a book + liquidity role.
func (m *PrometheusMetrics) SetAssumedFeeRate(book, liquidity string, rate float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.feeAssumedRate[book+"|"+liquidity] = rate
}

// SetFeeDriftRatio sets the realized/assumed fee drift ratio for a book/side/liquidity.
func (m *PrometheusMetrics) SetFeeDriftRatio(book, side, liquidity string, ratio float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.feeDriftRatio[feeKey(book, side, liquidity)] = ratio
}

// Handler returns an HTTP handler for the /metrics endpoint
func (m *PrometheusMetrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Write([]byte(m.Export()))
	}
}

// Export returns the metrics in Prometheus text format
func (m *PrometheusMetrics) Export() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sb strings.Builder

	// Service metrics
	sb.WriteString("# HELP strategy_executor_up Whether the strategy executor service is up\n")
	sb.WriteString("# TYPE strategy_executor_up gauge\n")
	sb.WriteString(fmt.Sprintf("strategy_executor_up %g\n", m.serviceUp))

	sb.WriteString("# HELP strategy_executor_active_strategies Number of currently active strategies\n")
	sb.WriteString("# TYPE strategy_executor_active_strategies gauge\n")
	sb.WriteString(fmt.Sprintf("strategy_executor_active_strategies %g\n", m.activeStrategies))

	sb.WriteString("# HELP strategy_executor_indicators_healthy Whether indicators are computing normally\n")
	sb.WriteString("# TYPE strategy_executor_indicators_healthy gauge\n")
	sb.WriteString(fmt.Sprintf("strategy_executor_indicators_healthy %g\n", m.indicatorsHealthy))

	sb.WriteString("# HELP strategy_executor_indicators_last_update_timestamp Unix timestamp of last indicator update\n")
	sb.WriteString("# TYPE strategy_executor_indicators_last_update_timestamp gauge\n")
	sb.WriteString(fmt.Sprintf("strategy_executor_indicators_last_update_timestamp %g\n", m.indicatorsLastUpdate))

	// Strategy running state
	if len(m.strategyRunning) > 0 {
		sb.WriteString("# HELP strategy_executor_strategy_running Whether a strategy is currently running (1=yes, 0=no)\n")
		sb.WriteString("# TYPE strategy_executor_strategy_running gauge\n")
		for strategy, value := range m.strategyRunning {
			sb.WriteString(fmt.Sprintf("strategy_executor_strategy_running{strategy=\"%s\"} %g\n", strategy, value))
		}
	}

	// Strategy win rate
	if len(m.strategyWinRate) > 0 {
		sb.WriteString("# HELP strategy_executor_strategy_win_rate Win rate of a strategy (0-1)\n")
		sb.WriteString("# TYPE strategy_executor_strategy_win_rate gauge\n")
		for strategy, value := range m.strategyWinRate {
			sb.WriteString(fmt.Sprintf("strategy_executor_strategy_win_rate{strategy=\"%s\"} %g\n", strategy, value))
		}
	}

	// Strategy P&L
	if len(m.strategyPnLTotal) > 0 {
		sb.WriteString("# HELP strategy_executor_strategy_pnl_total Total P&L for a strategy in MXN\n")
		sb.WriteString("# TYPE strategy_executor_strategy_pnl_total gauge\n")
		for strategy, value := range m.strategyPnLTotal {
			sb.WriteString(fmt.Sprintf("strategy_executor_strategy_pnl_total{strategy=\"%s\"} %g\n", strategy, value))
		}
	}

	// Consecutive losses
	if len(m.strategyConsecutiveLoss) > 0 {
		sb.WriteString("# HELP strategy_executor_consecutive_losses Number of consecutive losing trades\n")
		sb.WriteString("# TYPE strategy_executor_consecutive_losses gauge\n")
		for strategy, value := range m.strategyConsecutiveLoss {
			sb.WriteString(fmt.Sprintf("strategy_executor_consecutive_losses{strategy=\"%s\"} %g\n", strategy, value))
		}
	}

	// Signals generated
	if len(m.strategySignalsGenerated) > 0 {
		sb.WriteString("# HELP strategy_executor_signals_generated_total Total number of signals generated\n")
		sb.WriteString("# TYPE strategy_executor_signals_generated_total counter\n")
		for strategy, sides := range m.strategySignalsGenerated {
			// Sort sides for consistent output
			sideKeys := make([]string, 0, len(sides))
			for side := range sides {
				sideKeys = append(sideKeys, side)
			}
			sort.Strings(sideKeys)
			for _, side := range sideKeys {
				sb.WriteString(fmt.Sprintf("strategy_executor_signals_generated_total{strategy=\"%s\",side=\"%s\"} %g\n", strategy, side, sides[side]))
			}
		}
	}

	// Indicator metrics - SMA
	if len(m.indicatorSMA) > 0 {
		sb.WriteString("# HELP strategy_executor_indicator_sma Simple Moving Average value\n")
		sb.WriteString("# TYPE strategy_executor_indicator_sma gauge\n")
		for book, value := range m.indicatorSMA {
			sb.WriteString(fmt.Sprintf("strategy_executor_indicator_sma{book=\"%s\"} %g\n", book, value))
		}
	}

	// Indicator metrics - EMA
	if len(m.indicatorEMA) > 0 {
		sb.WriteString("# HELP strategy_executor_indicator_ema Exponential Moving Average value\n")
		sb.WriteString("# TYPE strategy_executor_indicator_ema gauge\n")
		for book, value := range m.indicatorEMA {
			sb.WriteString(fmt.Sprintf("strategy_executor_indicator_ema{book=\"%s\"} %g\n", book, value))
		}
	}

	// Indicator metrics - RSI
	if len(m.indicatorRSI) > 0 {
		sb.WriteString("# HELP strategy_executor_indicator_rsi Relative Strength Index value (0-100)\n")
		sb.WriteString("# TYPE strategy_executor_indicator_rsi gauge\n")
		for book, value := range m.indicatorRSI {
			sb.WriteString(fmt.Sprintf("strategy_executor_indicator_rsi{book=\"%s\"} %g\n", book, value))
		}
	}

	// Indicator metrics - Bollinger Bands
	if len(m.indicatorBollingerUpper) > 0 {
		sb.WriteString("# HELP strategy_executor_indicator_bollinger_upper Bollinger Band upper value\n")
		sb.WriteString("# TYPE strategy_executor_indicator_bollinger_upper gauge\n")
		for book, value := range m.indicatorBollingerUpper {
			sb.WriteString(fmt.Sprintf("strategy_executor_indicator_bollinger_upper{book=\"%s\"} %g\n", book, value))
		}
	}

	if len(m.indicatorBollingerMiddle) > 0 {
		sb.WriteString("# HELP strategy_executor_indicator_bollinger_middle Bollinger Band middle (SMA) value\n")
		sb.WriteString("# TYPE strategy_executor_indicator_bollinger_middle gauge\n")
		for book, value := range m.indicatorBollingerMiddle {
			sb.WriteString(fmt.Sprintf("strategy_executor_indicator_bollinger_middle{book=\"%s\"} %g\n", book, value))
		}
	}

	if len(m.indicatorBollingerLower) > 0 {
		sb.WriteString("# HELP strategy_executor_indicator_bollinger_lower Bollinger Band lower value\n")
		sb.WriteString("# TYPE strategy_executor_indicator_bollinger_lower gauge\n")
		for book, value := range m.indicatorBollingerLower {
			sb.WriteString(fmt.Sprintf("strategy_executor_indicator_bollinger_lower{book=\"%s\"} %g\n", book, value))
		}
	}

	// Indicator metrics - ATR
	if len(m.indicatorATR) > 0 {
		sb.WriteString("# HELP strategy_executor_indicator_atr Average True Range value\n")
		sb.WriteString("# TYPE strategy_executor_indicator_atr gauge\n")
		for book, value := range m.indicatorATR {
			sb.WriteString(fmt.Sprintf("strategy_executor_indicator_atr{book=\"%s\"} %g\n", book, value))
		}
	}

	// Indicator metrics - VWAP
	if len(m.indicatorVWAP) > 0 {
		sb.WriteString("# HELP strategy_executor_indicator_vwap Volume Weighted Average Price value\n")
		sb.WriteString("# TYPE strategy_executor_indicator_vwap gauge\n")
		for book, value := range m.indicatorVWAP {
			sb.WriteString(fmt.Sprintf("strategy_executor_indicator_vwap{book=\"%s\"} %g\n", book, value))
		}
	}

	// limit_profit strategy metrics
	if len(m.lpEntrySignals) > 0 {
		sb.WriteString("# HELP limit_profit_entry_signals_total Total entry (BUY) signals emitted\n")
		sb.WriteString("# TYPE limit_profit_entry_signals_total counter\n")
		for key, value := range m.lpEntrySignals {
			strategy, book := parseLpKey(key)
			sb.WriteString(fmt.Sprintf("limit_profit_entry_signals_total{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, value))
		}
	}

	if len(m.lpExitSignals) > 0 {
		sb.WriteString("# HELP limit_profit_exit_signals_total Total exit (SELL) signals by reason\n")
		sb.WriteString("# TYPE limit_profit_exit_signals_total counter\n")
		for key, reasons := range m.lpExitSignals {
			strategy, book := parseLpKey(key)
			reasonKeys := make([]string, 0, len(reasons))
			for reason := range reasons {
				reasonKeys = append(reasonKeys, reason)
			}
			sort.Strings(reasonKeys)
			for _, reason := range reasonKeys {
				sb.WriteString(fmt.Sprintf("limit_profit_exit_signals_total{strategy=\"%s\",book=\"%s\",reason=\"%s\"} %g\n", strategy, book, reason, reasons[reason]))
			}
		}
	}

	if len(m.lpPendingBuyDuration) > 0 {
		sb.WriteString("# HELP limit_profit_pending_buy_duration_seconds Time from entry signal to fill/timeout\n")
		sb.WriteString("# TYPE limit_profit_pending_buy_duration_seconds summary\n")
		for key, values := range m.lpPendingBuyDuration {
			strategy, book := parseLpKey(key)
			if len(values) > 0 {
				sum, count := 0.0, float64(len(values))
				for _, v := range values {
					sum += v
				}
				sb.WriteString(fmt.Sprintf("limit_profit_pending_buy_duration_seconds_sum{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, sum))
				sb.WriteString(fmt.Sprintf("limit_profit_pending_buy_duration_seconds_count{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, count))
			}
		}
	}

	if len(m.lpPositionHoldDuration) > 0 {
		sb.WriteString("# HELP limit_profit_position_hold_duration_seconds Time from fill to exit\n")
		sb.WriteString("# TYPE limit_profit_position_hold_duration_seconds summary\n")
		for key, values := range m.lpPositionHoldDuration {
			strategy, book := parseLpKey(key)
			if len(values) > 0 {
				sum, count := 0.0, float64(len(values))
				for _, v := range values {
					sum += v
				}
				sb.WriteString(fmt.Sprintf("limit_profit_position_hold_duration_seconds_sum{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, sum))
				sb.WriteString(fmt.Sprintf("limit_profit_position_hold_duration_seconds_count{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, count))
			}
		}
	}

	if len(m.lpPendingCancelFailures) > 0 {
		sb.WriteString("# HELP limit_profit_pending_cancel_failures_total Failed cancel attempts after retries exhausted\n")
		sb.WriteString("# TYPE limit_profit_pending_cancel_failures_total counter\n")
		for key, value := range m.lpPendingCancelFailures {
			strategy, book := parseLpKey(key)
			sb.WriteString(fmt.Sprintf("limit_profit_pending_cancel_failures_total{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, value))
		}
	}

	if len(m.lpDailyRealizedPnL) > 0 {
		sb.WriteString("# HELP limit_profit_daily_realized_pnl_quote Session P&L in quote currency (negative = loss)\n")
		sb.WriteString("# TYPE limit_profit_daily_realized_pnl_quote gauge\n")
		for key, value := range m.lpDailyRealizedPnL {
			strategy, book := parseLpKey(key)
			sb.WriteString(fmt.Sprintf("limit_profit_daily_realized_pnl_quote{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, value))
		}
	}

	if len(m.lpCircuitBreakerActive) > 0 {
		sb.WriteString("# HELP limit_profit_circuit_breaker_active Whether circuit breaker is active (1=tripped, 0=normal)\n")
		sb.WriteString("# TYPE limit_profit_circuit_breaker_active gauge\n")
		for key, value := range m.lpCircuitBreakerActive {
			strategy, book := parseLpKey(key)
			sb.WriteString(fmt.Sprintf("limit_profit_circuit_breaker_active{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, value))
		}
	}

	// momentum strategy metrics
	if len(m.momEntrySignals) > 0 {
		sb.WriteString("# HELP momentum_entry_signals_total Total momentum entry signals by side\n")
		sb.WriteString("# TYPE momentum_entry_signals_total counter\n")
		for key, sides := range m.momEntrySignals {
			strategy, book := parseLpKey(key)
			sideKeys := make([]string, 0, len(sides))
			for side := range sides {
				sideKeys = append(sideKeys, side)
			}
			sort.Strings(sideKeys)
			for _, side := range sideKeys {
				sb.WriteString(fmt.Sprintf("momentum_entry_signals_total{strategy=\"%s\",book=\"%s\",side=\"%s\"} %g\n", strategy, book, side, sides[side]))
			}
		}
	}

	if len(m.momExitSignals) > 0 {
		sb.WriteString("# HELP momentum_exit_signals_total Total momentum exit signals by reason\n")
		sb.WriteString("# TYPE momentum_exit_signals_total counter\n")
		for key, reasons := range m.momExitSignals {
			strategy, book := parseLpKey(key)
			reasonKeys := make([]string, 0, len(reasons))
			for reason := range reasons {
				reasonKeys = append(reasonKeys, reason)
			}
			sort.Strings(reasonKeys)
			for _, reason := range reasonKeys {
				sb.WriteString(fmt.Sprintf("momentum_exit_signals_total{strategy=\"%s\",book=\"%s\",reason=\"%s\"} %g\n", strategy, book, reason, reasons[reason]))
			}
		}
	}

	if len(m.momPositionHoldDuration) > 0 {
		sb.WriteString("# HELP momentum_position_hold_duration_seconds Time from fill to exit\n")
		sb.WriteString("# TYPE momentum_position_hold_duration_seconds summary\n")
		for key, values := range m.momPositionHoldDuration {
			strategy, book := parseLpKey(key)
			if len(values) > 0 {
				sum := 0.0
				for _, v := range values {
					sum += v
				}
				sb.WriteString(fmt.Sprintf("momentum_position_hold_duration_seconds_sum{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, sum))
				sb.WriteString(fmt.Sprintf("momentum_position_hold_duration_seconds_count{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, float64(len(values))))
			}
		}
	}

	if len(m.momDailyRealizedPnL) > 0 {
		sb.WriteString("# HELP momentum_daily_realized_pnl_quote Session P&L in quote currency (negative = loss)\n")
		sb.WriteString("# TYPE momentum_daily_realized_pnl_quote gauge\n")
		for key, value := range m.momDailyRealizedPnL {
			strategy, book := parseLpKey(key)
			sb.WriteString(fmt.Sprintf("momentum_daily_realized_pnl_quote{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, value))
		}
	}

	if len(m.momCircuitBreakerActive) > 0 {
		sb.WriteString("# HELP momentum_circuit_breaker_active Whether circuit breaker is active (1=tripped, 0=normal)\n")
		sb.WriteString("# TYPE momentum_circuit_breaker_active gauge\n")
		for key, value := range m.momCircuitBreakerActive {
			strategy, book := parseLpKey(key)
			sb.WriteString(fmt.Sprintf("momentum_circuit_breaker_active{strategy=\"%s\",book=\"%s\"} %g\n", strategy, book, value))
		}
	}

	// Fee honesty metrics (POINT-9 §7)
	if len(m.feeRealizedLast) > 0 {
		sb.WriteString("# HELP strategy_executor_realized_fee_rate Last realized fee rate (decimal fraction of notional) by book/side/liquidity\n")
		sb.WriteString("# TYPE strategy_executor_realized_fee_rate gauge\n")
		for key, value := range m.feeRealizedLast {
			book, side, liq := parseFeeKey(key)
			sb.WriteString(fmt.Sprintf("strategy_executor_realized_fee_rate{book=\"%s\",side=\"%s\",liquidity=\"%s\"} %g\n", book, side, liq, value))
		}
		sb.WriteString("# HELP strategy_executor_realized_fee_rate_observations Realized fee rate summary (sum/count) by book/side/liquidity\n")
		sb.WriteString("# TYPE strategy_executor_realized_fee_rate_observations summary\n")
		for key := range m.feeRealizedLast {
			book, side, liq := parseFeeKey(key)
			sb.WriteString(fmt.Sprintf("strategy_executor_realized_fee_rate_observations_sum{book=\"%s\",side=\"%s\",liquidity=\"%s\"} %g\n", book, side, liq, m.feeRealizedSum[key]))
			sb.WriteString(fmt.Sprintf("strategy_executor_realized_fee_rate_observations_count{book=\"%s\",side=\"%s\",liquidity=\"%s\"} %g\n", book, side, liq, m.feeRealizedCount[key]))
		}
	}

	if len(m.feeAssumedRate) > 0 {
		sb.WriteString("# HELP strategy_executor_assumed_fee_rate Configured/assumed fee rate (decimal fraction) by book/liquidity\n")
		sb.WriteString("# TYPE strategy_executor_assumed_fee_rate gauge\n")
		for key, value := range m.feeAssumedRate {
			parts := strings.SplitN(key, "|", 2)
			book, liq := key, ""
			if len(parts) == 2 {
				book, liq = parts[0], parts[1]
			}
			sb.WriteString(fmt.Sprintf("strategy_executor_assumed_fee_rate{book=\"%s\",liquidity=\"%s\"} %g\n", book, liq, value))
		}
	}

	if len(m.feeDriftRatio) > 0 {
		sb.WriteString("# HELP strategy_executor_fee_drift_ratio Realized/assumed fee ratio (1.0 = match, >1 = paying more than assumed) by book/side/liquidity\n")
		sb.WriteString("# TYPE strategy_executor_fee_drift_ratio gauge\n")
		for key, value := range m.feeDriftRatio {
			book, side, liq := parseFeeKey(key)
			sb.WriteString(fmt.Sprintf("strategy_executor_fee_drift_ratio{book=\"%s\",side=\"%s\",liquidity=\"%s\"} %g\n", book, side, liq, value))
		}
	}

	return sb.String()
}

func parseFeeKey(key string) (book, side, liquidity string) {
	parts := strings.SplitN(key, "|", 3)
	if len(parts) == 3 {
		return parts[0], parts[1], parts[2]
	}
	return key, "", ""
}

func parseLpKey(key string) (strategy, book string) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return key, ""
}

// Global instance for convenience
var globalPromMetrics *PrometheusMetrics
var promOnce sync.Once

// GetPrometheusMetrics returns the global PrometheusMetrics instance
func GetPrometheusMetrics() *PrometheusMetrics {
	promOnce.Do(func() {
		globalPromMetrics = NewPrometheusMetrics()
	})
	return globalPromMetrics
}
