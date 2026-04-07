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

	return sb.String()
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
