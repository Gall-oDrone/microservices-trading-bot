// Package strategies — limit_profit: buy near reference + offset, exit when min profit is achievable.
package strategies

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/logger"

	"github.com/google/uuid"
)

// LimitProfitConfig holds parameters for the limit-profit / scalp-style strategy.
// Exit threshold uses Bitso GET /fees with configurable maker/taker per leg when credentials exist;
// otherwise entry + min_profit + manual fee_addon.
type LimitProfitConfig struct {
	Reference            string  // "last_trade" or "vwap"
	EntryOffset          float64 // absolute quote-currency offset added to reference for BUY limit
	EntryOffsetBPS       float64 // if >0, entry_offset = reference * EntryOffsetBPS / 10000 (takes precedence)
	MinProfit            float64 // absolute quote-currency profit target above break-even
	MinProfitBPS         float64 // if >0, min_profit = entry * MinProfitBPS / 10000 (takes precedence)
	Fee                  float64 // extra margin on top of computed threshold (slippage buffer)
	FeeBPS               float64 // manual mode only
	UseBitsoFees         bool
	BuyLiquidity         string // "maker" | "taker" — expected role for the buy leg (default maker)
	SellLiquidity        string // "maker" | "taker" — expected role for the sell leg (default taker)
	ExitPriceReference   string // "last" | "bid" | "mid" | "min_last_bid" — price vs threshold
	PositionSize         float64
	MinSignalInterval    int
	// PendingBuyTimeoutSeconds: if >0, clear local pending BUY state after this long without a fill (see docs).
	PendingBuyTimeoutSeconds int
	// PendingSellTimeoutSeconds: if >0, attempt to cancel a stale resting SELL and clear pending-sell
	// state after this long without a fill. Position stays open until the next tick re-emits.
	PendingSellTimeoutSeconds int
	// PendingCancelMaxRetries: max cancel attempts before giving up (default 3).
	PendingCancelMaxRetries int
	// MaxPositionHoldSeconds: if >0, emit SELL after position age exceeds this (time stop).
	MaxPositionHoldSeconds int
	// StopLossQuote: if >0, emit SELL when compare price <= entry - StopLossQuote (quote currency per base unit).
	StopLossQuote float64
	// MaxDailyLossQuote: if >0, pause strategy when cumulative daily realized loss exceeds this (circuit breaker).
	MaxDailyLossQuote float64
	// DailyLossResetHourUTC: hour (0-23) when daily loss counter resets (default 0 = midnight UTC).
	DailyLossResetHourUTC int
	// TrailingStopQuote: if >0, once unrealized P&L exceeds TrailingStopActivationQuote, exit if price
	// drops this many quote units below the high-water mark (trailing stop).
	TrailingStopQuote float64
	// TrailingStopActivationQuote: minimum unrealized profit (quote) before trailing stop activates.
	TrailingStopActivationQuote float64
	// MaxPendingOrders: if >0, reject new entry signals if this many pending BUYs are already active.
	MaxPendingOrders int
	// SizingMode: "fixed" (default) or "atr_scaled" for volatility-adjusted sizing.
	SizingMode string
	// TargetRiskQuote: target quote-currency risk per trade (used with atr_scaled sizing).
	TargetRiskQuote float64
	// ATRMultiplier: multiplier for ATR when computing position size (default 1.0).
	ATRMultiplier float64
	// ATRPeriod: period for ATR indicator (default 14).
	ATRPeriod int
	// DryRun: if true, emit signals with metadata.dry_run=true (trading-engine should skip execution).
	DryRun bool
}

// DefaultLimitProfitConfig returns conservative defaults (tune per book / liquidity).
func DefaultLimitProfitConfig() LimitProfitConfig {
	return LimitProfitConfig{
		Reference:               "last_trade",
		EntryOffset:             500,
		EntryOffsetBPS:          0,
		MinProfit:               5000,
		MinProfitBPS:            0,
		PositionSize:            0.001,
		MinSignalInterval:       60,
		BuyLiquidity:            "maker",
		SellLiquidity:           "taker",
		ExitPriceReference:      "last",
		PendingCancelMaxRetries: 3,
		DailyLossResetHourUTC:   0,
		MaxPendingOrders:        1,
		SizingMode:              "fixed",
		ATRMultiplier:           1.0,
		ATRPeriod:               14,
	}
}

// LimitProfitStrategy buys at reference+offset then sells when market shows min profit vs entry.
type LimitProfitStrategy struct {
	*BaseEnhancedStrategy
	lpConfig LimitProfitConfig
	feeRates MakerTakerFeeProvider

	// Filled after BUY fill notification (optional); cleared on exit.
	positionBuyFeeRate   float64 // measured buy fee as decimal of notional; overrides API buy leg when > 0
	positionBuyLiquidity string  // maker|taker from venue when buy fill executed
	// Filled after SELL fill notification (optional); used for realized P&L only (not threshold).
	// See docs/strategy-fee-accuracy/.
	positionSellFeeRate float64

	// Partial fill tracking.
	targetOrderSize    float64 // original order size from entry signal
	cumulativeFilledAmt float64 // sum of partial fills received

	// Trailing stop tracking.
	trailingStopActive bool    // true once profit exceeds activation threshold
	trailingHighWater  float64 // highest compare_price seen since activation

	// Pending order tracking.
	pendingOrderCount int // number of active pending BUY orders

	// Session-level tracking for circuit breaker.
	dailyRealizedLoss     float64   // cumulative realized loss (positive = loss) for current session
	dailyLossResetTime    time.Time // when daily loss was last reset
	circuitBreakerTripped bool      // true = paused due to max daily loss

	mu sync.RWMutex

	rawStateStore LimitProfitRawStateStore

	// Optional: when pending buy times out, request OM cancel before clearing local pending state.
	pendingBuyCancel PendingBuyCancelClient

	// Prometheus metrics (optional, set via SetMetrics).
	metrics *LimitProfitMetrics

	// Structured logger (optional, set via SetLogger).
	log *logger.Logger

	// lastHoldDiagLog rate-limits "why no exit yet" logs while a position is open.
	lastHoldDiagLog time.Time
}

type limitProfitPersisted struct {
	State                 StrategyState `json:"state"`
	PositionBuyFeeRate    float64       `json:"position_buy_fee_rate,omitempty"`
	PositionBuyLiquidity  string        `json:"position_buy_liquidity,omitempty"`
	DailyRealizedLoss     float64       `json:"daily_realized_loss,omitempty"`
	DailyLossResetTime    time.Time     `json:"daily_loss_reset_time,omitempty"`
	CircuitBreakerTripped bool          `json:"circuit_breaker_tripped,omitempty"`
	// Partial fill tracking
	TargetOrderSize     float64 `json:"target_order_size,omitempty"`
	CumulativeFilledAmt float64 `json:"cumulative_filled_amt,omitempty"`
	// Trailing stop tracking
	TrailingStopActive bool    `json:"trailing_stop_active,omitempty"`
	TrailingHighWater  float64 `json:"trailing_high_water,omitempty"`
	// Pending order tracking
	PendingOrderCount int `json:"pending_order_count,omitempty"`
}

// LimitProfitMetrics holds Prometheus metrics for the limit_profit strategy.
type LimitProfitMetrics struct {
	EntrySignals          func(strategy, book string)
	ExitSignals           func(strategy, book, reason string)
	PendingBuyDuration    func(strategy, book string, seconds float64)
	PositionHoldDuration  func(strategy, book string, seconds float64)
	PendingCancelFailures func(strategy, book string)
	DailyRealizedPnL      func(strategy, book string, value float64)
	CircuitBreakerActive  func(strategy, book string, active bool)
}

// NewLimitProfitStrategy constructs a new instance (factory uses this).
func NewLimitProfitStrategy() *LimitProfitStrategy {
	return &LimitProfitStrategy{
		BaseEnhancedStrategy: NewBaseEnhancedStrategy("limit_profit", "1.0.0"),
		lpConfig:             DefaultLimitProfitConfig(),
	}
}

// NewLimitProfitStrategyFactory registers with EnhancedRegistry.
func NewLimitProfitStrategyFactory() func() EnhancedStrategy {
	return func() EnhancedStrategy {
		return NewLimitProfitStrategy()
	}
}

// SetMetrics injects Prometheus metric callbacks (optional).
func (s *LimitProfitStrategy) SetMetrics(m *LimitProfitMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics = m
}

// SetLogger injects a structured logger (optional).
func (s *LimitProfitStrategy) SetLogger(l *logger.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = l
}

// logger returns the strategy's logger with common fields, or a default logger.
func (s *LimitProfitStrategy) logger() *logger.Logger {
	if s.log != nil {
		return s.log.WithStr("strategy", s.Name()).WithStr("book", s.config.Book)
	}
	return logger.NewDefault().WithStr("strategy", s.Name()).WithStr("book", s.config.Book)
}

// Initialize parses StrategyConfig into lpConfig.
func (s *LimitProfitStrategy) Initialize(config StrategyConfig, indicatorSvc *indicators.Service) error {
	if err := s.BaseEnhancedStrategy.Initialize(config, indicatorSvc); err != nil {
		return err
	}
	s.lpConfig = DefaultLimitProfitConfig()
	s.lpConfig.UseBitsoFees = true
	if p := config.Parameters; p != nil {
		if v, ok := p["entry_offset"].(float64); ok {
			s.lpConfig.EntryOffset = v
		}
		if v, ok := p["entry_offset_bps"].(float64); ok {
			s.lpConfig.EntryOffsetBPS = v
		}
		if v, ok := p["min_profit"].(float64); ok {
			s.lpConfig.MinProfit = v
		}
		if v, ok := p["min_profit_bps"].(float64); ok {
			s.lpConfig.MinProfitBPS = v
		}
		if v, ok := p["fee"].(float64); ok {
			s.lpConfig.Fee = v
		}
		if v, ok := p["fee_bps"].(float64); ok {
			s.lpConfig.FeeBPS = v
		}
		if v, ok := p["min_signal_interval"].(float64); ok {
			s.lpConfig.MinSignalInterval = int(v)
		}
		if v, ok := p["reference"].(string); ok {
			s.lpConfig.Reference = strings.ToLower(strings.TrimSpace(v))
		}
		if v, ok := p["use_bitso_fees"].(bool); ok {
			s.lpConfig.UseBitsoFees = v
		}
		if v, ok := p["buy_liquidity"].(string); ok {
			s.lpConfig.BuyLiquidity = strings.ToLower(strings.TrimSpace(v))
		}
		if v, ok := p["sell_liquidity"].(string); ok {
			s.lpConfig.SellLiquidity = strings.ToLower(strings.TrimSpace(v))
		}
		if v, ok := p["exit_price_reference"].(string); ok {
			s.lpConfig.ExitPriceReference = strings.ToLower(strings.TrimSpace(v))
		}
		if v, ok := p["pending_buy_timeout_seconds"].(float64); ok {
			s.lpConfig.PendingBuyTimeoutSeconds = int(v)
		}
		if v, ok := p["pending_sell_timeout_seconds"].(float64); ok {
			s.lpConfig.PendingSellTimeoutSeconds = int(v)
		}
		if v, ok := p["pending_cancel_max_retries"].(float64); ok {
			s.lpConfig.PendingCancelMaxRetries = int(v)
		}
		if v, ok := p["max_position_hold_seconds"].(float64); ok {
			s.lpConfig.MaxPositionHoldSeconds = int(v)
		}
		if v, ok := p["stop_loss_quote"].(float64); ok {
			s.lpConfig.StopLossQuote = v
		}
		if v, ok := p["max_daily_loss_quote"].(float64); ok {
			s.lpConfig.MaxDailyLossQuote = v
		}
		if v, ok := p["daily_loss_reset_hour_utc"].(float64); ok {
			s.lpConfig.DailyLossResetHourUTC = int(v)
		}
		if v, ok := p["trailing_stop_quote"].(float64); ok {
			s.lpConfig.TrailingStopQuote = v
		}
		if v, ok := p["trailing_stop_activation_quote"].(float64); ok {
			s.lpConfig.TrailingStopActivationQuote = v
		}
		if v, ok := p["max_pending_orders"].(float64); ok {
			s.lpConfig.MaxPendingOrders = int(v)
		}
		if v, ok := p["sizing_mode"].(string); ok {
			s.lpConfig.SizingMode = strings.ToLower(strings.TrimSpace(v))
		}
		if v, ok := p["target_risk_quote"].(float64); ok {
			s.lpConfig.TargetRiskQuote = v
		}
		if v, ok := p["atr_multiplier"].(float64); ok {
			s.lpConfig.ATRMultiplier = v
		}
		if v, ok := p["atr_period"].(float64); ok {
			s.lpConfig.ATRPeriod = int(v)
		}
		if v, ok := p["dry_run"].(bool); ok {
			s.lpConfig.DryRun = v
		}
	}
	s.lpConfig.PositionSize = ResolvePositionSize(
		config.Parameters, config.Sizing.MaxPositionSize, s.lpConfig.PositionSize)
	if s.lpConfig.Reference == "" {
		s.lpConfig.Reference = "last_trade"
	}
	if s.lpConfig.PositionSize <= 0 {
		s.lpConfig.PositionSize = DefaultLimitProfitConfig().PositionSize
	}
	if s.lpConfig.BuyLiquidity == "" {
		s.lpConfig.BuyLiquidity = "maker"
	}
	if s.lpConfig.SellLiquidity == "" {
		s.lpConfig.SellLiquidity = "taker"
	}
	if s.lpConfig.ExitPriceReference == "" {
		s.lpConfig.ExitPriceReference = "last"
	}
	if s.lpConfig.PendingCancelMaxRetries <= 0 {
		s.lpConfig.PendingCancelMaxRetries = 3
	}
	return nil
}

// SetLimitProfitRawStateStore enables Redis-backed durable state (optional).
func (s *LimitProfitStrategy) SetLimitProfitRawStateStore(store LimitProfitRawStateStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rawStateStore = store
}

// SetPendingBuyCancelClient registers order-management cancel RPC for pending-buy timeout (optional).
func (s *LimitProfitStrategy) SetPendingBuyCancelClient(c PendingBuyCancelClient) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingBuyCancel = c
}

// Start restores persisted state after the base marks the strategy running.
func (s *LimitProfitStrategy) Start(ctx context.Context) error {
	if err := s.BaseEnhancedStrategy.Start(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rawStateStore == nil {
		return nil
	}
	payload, err := s.rawStateStore.Load(ctx, s.Name())
	if err != nil || len(payload) == 0 {
		return nil
	}
	var p limitProfitPersisted
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil
	}
	s.applyPersistedLocked(&p)
	return nil
}

func (s *LimitProfitStrategy) applyPersistedLocked(p *limitProfitPersisted) {
	s.ApplyPersistedState(p.State)
	// Start() already marked the strategy running; snapshot may have Running=false from last stop.
	s.BaseEnhancedStrategy.running = true
	s.BaseEnhancedStrategy.state.Running = true
	if s.GetState().HasPosition {
		s.UpdateState(func(st *StrategyState) {
			st.PendingBuy = false
			st.PendingEventID = ""
			st.PendingBuySince = time.Time{}
		})
	} else if s.GetState().PendingBuy && s.GetState().PendingBuySince.IsZero() {
		// Older snapshots or migrations: approximate pending start from last signal time.
		st := s.GetState()
		if !st.LastSignalTime.IsZero() {
			s.UpdateState(func(out *StrategyState) {
				out.PendingBuySince = st.LastSignalTime
			})
		} else {
			s.UpdateState(func(out *StrategyState) {
				out.PendingBuySince = time.Now()
			})
		}
	}
	s.positionBuyFeeRate = p.PositionBuyFeeRate
	s.positionBuyLiquidity = p.PositionBuyLiquidity

	// Restore partial fill tracking.
	s.targetOrderSize = p.TargetOrderSize
	s.cumulativeFilledAmt = p.CumulativeFilledAmt

	// Restore trailing stop tracking.
	s.trailingStopActive = p.TrailingStopActive
	s.trailingHighWater = p.TrailingHighWater

	// Restore pending order tracking.
	s.pendingOrderCount = p.PendingOrderCount

	// Restore daily loss state, but check if it should reset based on time.
	s.dailyLossResetTime = p.DailyLossResetTime
	if s.shouldResetDailyLoss() {
		s.dailyRealizedLoss = 0
		s.circuitBreakerTripped = false
		s.dailyLossResetTime = s.nextDailyResetTime()
	} else {
		s.dailyRealizedLoss = p.DailyRealizedLoss
		s.circuitBreakerTripped = p.CircuitBreakerTripped
	}
}

func (s *LimitProfitStrategy) persistLocked(ctx context.Context) {
	if s.rawStateStore == nil {
		return
	}
	p := limitProfitPersisted{
		State:                 s.GetState(),
		PositionBuyFeeRate:    s.positionBuyFeeRate,
		PositionBuyLiquidity:  s.positionBuyLiquidity,
		DailyRealizedLoss:     s.dailyRealizedLoss,
		DailyLossResetTime:    s.dailyLossResetTime,
		CircuitBreakerTripped: s.circuitBreakerTripped,
		TargetOrderSize:       s.targetOrderSize,
		CumulativeFilledAmt:   s.cumulativeFilledAmt,
		TrailingStopActive:    s.trailingStopActive,
		TrailingHighWater:     s.trailingHighWater,
		PendingOrderCount:     s.pendingOrderCount,
	}
	b, err := json.Marshal(&p)
	if err != nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_ = s.rawStateStore.Save(cctx, s.Name(), b)
}

// shouldResetDailyLoss returns true if current time is past the next reset boundary.
func (s *LimitProfitStrategy) shouldResetDailyLoss() bool {
	if s.dailyLossResetTime.IsZero() {
		return true
	}
	return time.Now().UTC().After(s.dailyLossResetTime)
}

// nextDailyResetTime calculates the next reset time based on DailyLossResetHourUTC.
func (s *LimitProfitStrategy) nextDailyResetTime() time.Time {
	now := time.Now().UTC()
	resetHour := s.lpConfig.DailyLossResetHourUTC
	if resetHour < 0 || resetHour > 23 {
		resetHour = 0
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), resetHour, 0, 0, 0, time.UTC)
	if now.After(next) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

// checkAndResetDailyLoss resets counters if past reset time, and updates metrics.
func (s *LimitProfitStrategy) checkAndResetDailyLoss() {
	if s.shouldResetDailyLoss() {
		s.dailyRealizedLoss = 0
		s.circuitBreakerTripped = false
		s.dailyLossResetTime = s.nextDailyResetTime()
		if s.metrics != nil && s.metrics.CircuitBreakerActive != nil {
			s.metrics.CircuitBreakerActive(s.Name(), s.config.Book, false)
		}
		if s.metrics != nil && s.metrics.DailyRealizedPnL != nil {
			s.metrics.DailyRealizedPnL(s.Name(), s.config.Book, 0)
		}
	}
}

// recordRealizedLoss adds a loss to daily total and checks circuit breaker.
func (s *LimitProfitStrategy) recordRealizedLoss(loss float64) {
	if loss <= 0 {
		return
	}
	s.dailyRealizedLoss += loss
	if s.metrics != nil && s.metrics.DailyRealizedPnL != nil {
		s.metrics.DailyRealizedPnL(s.Name(), s.config.Book, -s.dailyRealizedLoss)
	}
	if s.lpConfig.MaxDailyLossQuote > 0 && s.dailyRealizedLoss >= s.lpConfig.MaxDailyLossQuote {
		s.circuitBreakerTripped = true
		if s.metrics != nil && s.metrics.CircuitBreakerActive != nil {
			s.metrics.CircuitBreakerActive(s.Name(), s.config.Book, true)
		}
		s.logger().
			WithFloat64("daily_loss", s.dailyRealizedLoss).
			WithFloat64("limit", s.lpConfig.MaxDailyLossQuote).
			Warn("circuit breaker tripped")
	}
}

// OnTick evaluates latest trade price against entry / exit rules.
func (s *LimitProfitStrategy) OnTick(tick *indicators.Trade) (*Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		return nil, nil
	}
	if tick == nil {
		return nil, nil
	}

	// Check for daily reset and circuit breaker.
	s.checkAndResetDailyLoss()
	if s.circuitBreakerTripped {
		return nil, nil
	}

	state := s.GetState()
	tickPrice := tick.Price
	book := s.config.Book

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !state.HasPosition {
		if state.PendingBuy {
			if s.lpConfig.PendingBuyTimeoutSeconds > 0 {
				since := state.PendingBuySince
				if since.IsZero() {
					since = state.LastSignalTime
				}
				if !since.IsZero() && time.Since(since) >= time.Duration(s.lpConfig.PendingBuyTimeoutSeconds)*time.Second {
					eid := state.PendingEventID
					cancelSuccess := true
					if eid != "" && s.pendingBuyCancel != nil {
						cancelSuccess = s.cancelPendingBuyWithRetry(ctx, eid)
					}
					if cancelSuccess {
						// Record pending buy duration metric.
						if s.metrics != nil && s.metrics.PendingBuyDuration != nil {
							s.metrics.PendingBuyDuration(s.Name(), book, time.Since(since).Seconds())
						}
						s.UpdateState(func(st *StrategyState) {
							st.PendingBuy = false
							st.PendingEventID = ""
							st.PendingBuySince = time.Time{}
						})
						s.pendingOrderCount--
						if s.pendingOrderCount < 0 {
							s.pendingOrderCount = 0
						}
						s.persistLocked(ctx)
					}
					return nil, nil
				}
			}
			return nil, nil
		}
		if !s.canEmitSignal() {
			return nil, nil
		}
		// Check max pending orders limit.
		maxPending := s.lpConfig.MaxPendingOrders
		if maxPending <= 0 {
			maxPending = 1
		}
		if s.pendingOrderCount >= maxPending {
			return nil, nil
		}

		ref := s.referencePrice(ctx, tickPrice)
		entryOffset := s.computeEntryOffset(ref)
		buyPrice := ref + entryOffset

		// Compute position size (fixed or ATR-scaled).
		posSize := s.computePositionSize(ctx, book)

		eventID := uuid.New().String()
		now := time.Now()
		s.RecordSignal()
		s.UpdateState(func(st *StrategyState) {
			st.PendingBuy = true
			st.PendingEventID = eventID
			st.PendingBuySince = now
		})
		s.pendingOrderCount++
		s.targetOrderSize = posSize
		s.cumulativeFilledAmt = 0
		s.persistLocked(ctx)

		// Record entry signal metric.
		if s.metrics != nil && s.metrics.EntrySignals != nil {
			s.metrics.EntrySignals(s.Name(), book)
		}

		meta := map[string]interface{}{
			"signal_type":            "entry_buy",
			"reference":              ref,
			"event_id":               eventID,
			"entry_offset":           entryOffset,
			"entry_offset_bps":       s.lpConfig.EntryOffsetBPS,
			"buy_liquidity_expected": s.lpConfig.BuyLiquidity,
			"fee_model":              s.feeModelLabel(),
			"use_bitso_fees":         s.lpConfig.UseBitsoFees,
			"sizing_mode":            s.lpConfig.SizingMode,
		}
		if s.lpConfig.DryRun {
			meta["dry_run"] = true
		}

		return &Signal{
			Strategy:   s.Name(),
			Book:       book,
			Side:       "BUY",
			Amount:     posSize,
			Price:      buyPrice,
			Confidence: 0.75,
			Reason: fmt.Sprintf(
				"limit_profit entry: ref=%.2f (%s) + offset=%.2f → buy limit %.2f (size=%.6f)",
				ref, s.lpConfig.Reference, entryOffset, buyPrice, posSize,
			),
			Timestamp: time.Now(),
			Metadata:  meta,
		}, nil
	}

	// If a SELL is already resting on the exchange, do NOT emit another signal.
	// This prevents the "stacked orders" bug where the old code cleared the position on
	// signal emission and re-entered on the next tick while the original SELL was still open.
	if state.PendingSell {
		s.handlePendingSellTimeoutLocked(ctx, state)
		return nil, nil
	}

	entry := state.EntryPrice
	comparePrice, refLabel := s.referenceExitPrice(ctx, tickPrice, book)
	threshold, buyR, sellR, feeModel := s.exitPriceThreshold(ctx, entry)
	manualAddon := s.exitFeeAddon(entry)

	// Circuit breaker exit: if tripped mid-position, close immediately.
	if s.circuitBreakerTripped {
		return s.emitPositionExit(ctx, tickPrice, comparePrice, refLabel, entry, threshold, buyR, sellR, feeModel, manualAddon, "circuit_breaker"), nil
	}

	// Stop loss check (hard stop).
	if s.lpConfig.StopLossQuote > 0 && comparePrice <= entry-s.lpConfig.StopLossQuote {
		return s.emitPositionExit(ctx, tickPrice, comparePrice, refLabel, entry, threshold, buyR, sellR, feeModel, manualAddon, "stop_loss"), nil
	}

	// Trailing stop logic.
	if s.lpConfig.TrailingStopQuote > 0 {
		unrealizedPnL := (comparePrice - entry) * state.PositionSize
		activationThreshold := s.lpConfig.TrailingStopActivationQuote
		if activationThreshold <= 0 {
			activationThreshold = s.lpConfig.TrailingStopQuote // Default: activate when profit >= trailing amount
		}
		if !s.trailingStopActive && unrealizedPnL >= activationThreshold {
			s.trailingStopActive = true
			s.trailingHighWater = comparePrice
			s.persistLocked(ctx)
		}
		if s.trailingStopActive {
			if comparePrice > s.trailingHighWater {
				s.trailingHighWater = comparePrice
				s.persistLocked(ctx)
			}
			trailingStopPrice := s.trailingHighWater - s.lpConfig.TrailingStopQuote
			if comparePrice <= trailingStopPrice {
				return s.emitPositionExit(ctx, tickPrice, comparePrice, refLabel, entry, threshold, buyR, sellR, feeModel, manualAddon, "trailing_stop"), nil
			}
		}
	}

	// Max position hold time check.
	if s.lpConfig.MaxPositionHoldSeconds > 0 && !state.EntryTime.IsZero() {
		if time.Since(state.EntryTime) >= time.Duration(s.lpConfig.MaxPositionHoldSeconds)*time.Second {
			return s.emitPositionExit(ctx, tickPrice, comparePrice, refLabel, entry, threshold, buyR, sellR, feeModel, manualAddon, "max_hold"), nil
		}
	}

	// Take profit check.
	if comparePrice >= threshold {
		return s.emitPositionExit(ctx, tickPrice, comparePrice, refLabel, entry, threshold, buyR, sellR, feeModel, manualAddon, "take_profit"), nil
	}

	// Periodic observability: last trade (or chosen exit ref) vs thresholds. No SELL is emitted
	// until take-profit, stop-loss, trailing, max-hold, or circuit-breaker fires — this is expected.
	if s.lastHoldDiagLog.IsZero() || time.Since(s.lastHoldDiagLog) >= 60*time.Second {
		s.lastHoldDiagLog = time.Now()
		logTh := threshold
		if math.IsInf(logTh, 0) {
			logTh = 0
		}
		chained := s.logger().
			WithFloat64("entry", entry).
			WithFloat64("compare_price", comparePrice).
			WithFloat64("take_profit_threshold", logTh).
			WithStr("exit_price_ref", refLabel).
			WithStr("fee_model", feeModel).
			WithFloat64("buy_fee_r", buyR).
			WithFloat64("sell_fee_r", sellR)
		if s.lpConfig.StopLossQuote > 0 {
			chained = chained.WithFloat64("stop_loss_trigger_level", entry-s.lpConfig.StopLossQuote)
		}
		chained.Info("limit_profit holding: exit not triggered (compare vs take_profit_threshold)")
	}

	return nil, nil
}

// emitPositionExit builds a SELL signal and updates strategy state. Must run with s.mu held.
func (s *LimitProfitStrategy) emitPositionExit(
	ctx context.Context,
	tickPrice, comparePrice float64,
	refLabel string,
	entry, threshold, buyR, sellR float64,
	feeModel string,
	manualAddon float64,
	exitReason string,
) *Signal {
	state := s.GetState()
	posSize := state.PositionSize
	book := s.config.Book

	// Determine exit price for P&L and signal.
	// When using bid/mid references, use comparePrice for both calculation and execution.
	exitForPnL := tickPrice
	signalPrice := tickPrice
	switch refLabel {
	case "bid", "mid", "min_last_bid":
		exitForPnL = comparePrice
		signalPrice = comparePrice // Align signal price with compare price for execution consistency.
	}

	gross := (exitForPnL - entry) * posSize
	netQuote := bitso.NetQuotePnLPerBase(entry, exitForPnL, buyR, sellR) * posSize

	profitable := gross > 0
	if feeModel == "bitso_api" {
		profitable = netQuote > 0
	}

	// Record position hold duration metric.
	if s.metrics != nil && s.metrics.PositionHoldDuration != nil && !state.EntryTime.IsZero() {
		s.metrics.PositionHoldDuration(s.Name(), book, time.Since(state.EntryTime).Seconds())
	}

	// Record exit signal metric.
	if s.metrics != nil && s.metrics.ExitSignals != nil {
		s.metrics.ExitSignals(s.Name(), book, exitReason)
	}

	// NOTE: `profitable`, gross and netQuote are estimates for logging/metadata only.
	// Realized P&L (including circuit-breaker bookkeeping and TotalPnL/DailyPnL metric updates)
	// is computed in handleSellFillLocked once the exchange confirms the SELL fill price.
	_ = profitable

	// Record exit-signal timestamp + mark pending SELL so OnTick suppresses further signals
	// until the exchange reports the fill. Position state, trailing stop, buy-fee overrides,
	// and the RecordTradeWithPnL call are deferred to handleSellFillLocked.
	eventID := uuid.New().String()
	now := time.Now()
	s.RecordSignal()
	s.UpdateState(func(st *StrategyState) {
		st.PendingSell = true
		st.PendingSellEventID = eventID
		st.PendingSellSince = now
	})
	s.persistLocked(ctx)

	minProfit := s.computeMinProfit(entry)
	meta := map[string]interface{}{
		"signal_type":             "exit_sell",
		"exit_reason":             exitReason,
		"entry_price":             entry,
		"event_id":                eventID,
		"gross_quote_pnl":         gross,
		"net_quote_pnl_estimate":  netQuote,
		"exit_threshold":          threshold,
		"fee_model":               feeModel,
		"min_profit":              minProfit,
		"min_profit_bps":          s.lpConfig.MinProfitBPS,
		"extra_fee_margin":        s.lpConfig.Fee,
		"exit_price_reference":    refLabel,
		"compare_price":           comparePrice,
		"tick_price":              tickPrice,
		"signal_price":            signalPrice,
		"buy_liquidity_effective": s.effectiveBuyLiquidity(),
		"sell_liquidity":          s.lpConfig.SellLiquidity,
	}
	if feeModel == "bitso_api" {
		meta["net_quote_pnl"] = netQuote
		meta["buy_fee_rate"] = buyR
		meta["sell_fee_rate"] = sellR
	} else {
		meta["fee_addon_manual"] = manualAddon
	}

	// Add trailing stop info to metadata if active.
	if s.trailingStopActive {
		meta["trailing_stop_active"] = true
		meta["trailing_high_water"] = s.trailingHighWater
	}
	if s.lpConfig.DryRun {
		meta["dry_run"] = true
	}

	reason := fmt.Sprintf(
		"limit_profit exit [%s] ref=%s reason=%s: compare=%.2f threshold=%.2f (entry=%.2f min_profit=%.2f)",
		feeModel, refLabel, exitReason, comparePrice, threshold, entry, minProfit,
	)
	switch exitReason {
	case "stop_loss":
		reason = fmt.Sprintf(
			"limit_profit stop_loss [%s] ref=%s: compare=%.2f <= entry-stop=%.2f (entry=%.2f stop_loss_quote=%.2f)",
			feeModel, refLabel, comparePrice, entry-s.lpConfig.StopLossQuote, entry, s.lpConfig.StopLossQuote,
		)
	case "trailing_stop":
		reason = fmt.Sprintf(
			"limit_profit trailing_stop [%s] ref=%s: compare=%.2f <= high_water %.2f - trail %.2f (entry=%.2f)",
			feeModel, refLabel, comparePrice, s.trailingHighWater, s.lpConfig.TrailingStopQuote, entry,
		)
	case "max_hold":
		reason = fmt.Sprintf(
			"limit_profit max_hold [%s] ref=%s: position age >= %ds",
			feeModel, refLabel, s.lpConfig.MaxPositionHoldSeconds,
		)
	case "circuit_breaker":
		reason = fmt.Sprintf(
			"limit_profit circuit_breaker [%s] ref=%s: daily loss %.2f >= limit %.2f",
			feeModel, refLabel, s.dailyRealizedLoss, s.lpConfig.MaxDailyLossQuote,
		)
	}

	return &Signal{
		Strategy:   s.Name(),
		Book:       book,
		Side:       "SELL",
		Amount:     posSize,
		Price:      signalPrice, // Use compare price when exit ref is bid/mid for execution alignment.
		Confidence: 0.85,
		Reason:     reason,
		Timestamp:  time.Now(),
		Metadata:   meta,
	}
}

// OnOrderFilled is the single fill callback for both BUY (entry) and SELL (exit) orders.
// Dispatches by side: BUY fills open/grow the position, SELL fills close it and realize P&L.
func (s *LimitProfitStrategy) OnOrderFilled(fill OrderFill) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		return
	}
	if fill.Book != s.config.Book {
		return
	}
	side := strings.ToLower(strings.TrimSpace(fill.Side))
	switch side {
	case "buy", "purchase":
		s.handleBuyFillLocked(fill)
	case "sell":
		s.handleSellFillLocked(fill)
	}
}

// handleBuyFillLocked opens or grows a LONG position on a BUY fill and clears pending-buy
// state once the target order size has been filled. Must run with s.mu held.
func (s *LimitProfitStrategy) handleBuyFillLocked(fill OrderFill) {
	st := s.GetState()
	if !st.PendingBuy || st.PendingEventID == "" || fill.EventID != st.PendingEventID {
		return
	}
	if fill.AveragePrice <= 0 {
		return
	}
	fillSize := fill.FilledAmount
	if fillSize <= 0 {
		return
	}

	// Prefer realized FeeRate (Bitso UserTrade-derived) over legacy BuyFeeRate pointer; both
	// describe the BUY leg's actual fee rate. See docs/strategy-fee-accuracy/.
	if fill.FeeRate > 0 {
		s.positionBuyFeeRate = fill.FeeRate
	} else if fill.BuyFeeRate != nil && *fill.BuyFeeRate > 0 {
		s.positionBuyFeeRate = *fill.BuyFeeRate
	}
	if fill.Liquidity != "" {
		s.positionBuyLiquidity = normalizeLiquidity(fill.Liquidity)
	}

	// Handle partial fill: accumulate filled amount and compute weighted average entry.
	prevFilled := s.cumulativeFilledAmt
	prevEntry := st.EntryPrice
	newFilled := prevFilled + fillSize
	s.cumulativeFilledAmt = newFilled

	var avgEntry float64
	if prevFilled > 0 && prevEntry > 0 {
		avgEntry = (prevEntry*prevFilled + fill.AveragePrice*fillSize) / newFilled
	} else {
		avgEntry = fill.AveragePrice
	}

	isFullyFilled := s.targetOrderSize > 0 && newFilled >= s.targetOrderSize*0.999 // 0.1% tolerance for rounding
	if s.targetOrderSize <= 0 {
		isFullyFilled = true
	}

	s.SetPosition("LONG", newFilled, avgEntry)

	if isFullyFilled {
		s.UpdateState(func(out *StrategyState) {
			out.PendingBuy = false
			out.PendingEventID = ""
			out.PendingBuySince = time.Time{}
		})
		s.pendingOrderCount--
		if s.pendingOrderCount < 0 {
			s.pendingOrderCount = 0
		}
		if s.metrics != nil && s.metrics.PendingBuyDuration != nil && !st.PendingBuySince.IsZero() {
			s.metrics.PendingBuyDuration(s.Name(), s.config.Book, time.Since(st.PendingBuySince).Seconds())
		}
	}

	s.persistLocked(context.Background())
}

// handleSellFillLocked closes the position on a SELL fill, computes realized P&L using the
// actual exchange fill price, updates TotalPnL/DailyPnL via RecordTradeWithPnL, feeds the
// circuit breaker when appropriate, and clears all position + pending-sell state.
// Must run with s.mu held.
func (s *LimitProfitStrategy) handleSellFillLocked(fill OrderFill) {
	st := s.GetState()
	if !st.PendingSell || st.PendingSellEventID == "" || fill.EventID != st.PendingSellEventID {
		return
	}
	if !st.HasPosition {
		return
	}
	if fill.AveragePrice <= 0 || fill.FilledAmount <= 0 {
		return
	}

	entry := st.EntryPrice
	exitPrice := fill.AveragePrice
	posSize := st.PositionSize
	if posSize <= 0 {
		posSize = fill.FilledAmount
	}
	book := s.config.Book

	// Capture realized SELL fee rate (Bitso UserTrade-derived) for accurate net P&L below.
	// See docs/strategy-fee-accuracy/.
	if fill.FeeRate > 0 {
		s.positionSellFeeRate = fill.FeeRate
	}

	ctx := context.Background()
	_, buyR, sellR, feeModel := s.exitPriceThreshold(ctx, entry)
	// Prefer the realized SELL leg fee rate over the configured assumption (only the SELL
	// leg can be known at this point; the BUY leg was already realized into positionBuyFeeRate).
	if s.positionSellFeeRate > 0 {
		sellR = s.positionSellFeeRate
		feeModel = "bitso_api"
	}

	gross := (exitPrice - entry) * posSize
	netQuote := gross
	if feeModel == "bitso_api" {
		netQuote = bitso.NetQuotePnLPerBase(entry, exitPrice, buyR, sellR) * posSize
	}

	profitable := gross > 0
	if feeModel == "bitso_api" {
		profitable = netQuote > 0
	}
	realizedPnL := gross
	if feeModel == "bitso_api" {
		realizedPnL = netQuote
	}

	s.RecordTradeWithPnL(profitable, realizedPnL)

	if !profitable {
		loss := -realizedPnL
		if loss > 0 {
			s.recordRealizedLoss(loss)
		}
	} else if s.metrics != nil && s.metrics.DailyRealizedPnL != nil {
		currentPnL := -s.dailyRealizedLoss + realizedPnL
		s.metrics.DailyRealizedPnL(s.Name(), book, currentPnL)
	}

	s.logger().
		WithStr("event_id", fill.EventID).
		WithFloat64("entry_price", entry).
		WithFloat64("exit_price", exitPrice).
		WithFloat64("position_size", posSize).
		WithFloat64("gross_pnl", gross).
		WithFloat64("net_pnl", netQuote).
		WithStr("fee_model", feeModel).
		Info("sell fill processed; realized pnl accumulated")

	s.ClearPosition()
	s.resetPositionFeeOverrides()
	s.resetTrailingStop()
	s.targetOrderSize = 0
	s.cumulativeFilledAmt = 0
	s.UpdateState(func(out *StrategyState) {
		out.PendingSell = false
		out.PendingSellEventID = ""
		out.PendingSellSince = time.Time{}
	})
	s.lastHoldDiagLog = time.Time{}
	s.persistLocked(ctx)
}

// handlePendingSellTimeoutLocked cancels a stale resting SELL and clears the pending-sell
// flag so the next tick can re-emit a fresh exit. The position itself stays open; the caller
// still returns nil this tick. Must run with s.mu held.
func (s *LimitProfitStrategy) handlePendingSellTimeoutLocked(ctx context.Context, state StrategyState) {
	if s.lpConfig.PendingSellTimeoutSeconds <= 0 {
		return
	}
	since := state.PendingSellSince
	if since.IsZero() {
		since = state.LastSignalTime
	}
	if since.IsZero() {
		return
	}
	if time.Since(since) < time.Duration(s.lpConfig.PendingSellTimeoutSeconds)*time.Second {
		return
	}

	eid := state.PendingSellEventID
	cancelSuccess := true
	if eid != "" && s.pendingBuyCancel != nil {
		cancelSuccess = s.cancelPendingBuyWithRetry(ctx, eid)
	}
	if !cancelSuccess {
		return
	}
	s.logger().
		WithStr("event_id", eid).
		WithFloat64("age_seconds", time.Since(since).Seconds()).
		Warn("pending sell timed out; cleared local state to allow re-emit")
	s.UpdateState(func(out *StrategyState) {
		out.PendingSell = false
		out.PendingSellEventID = ""
		out.PendingSellSince = time.Time{}
	})
	s.lastHoldDiagLog = time.Time{}
	s.persistLocked(ctx)
}

// SetFeeRatesProvider injects the registry-wide Bitso fee source (optional).
func (s *LimitProfitStrategy) SetFeeRatesProvider(p MakerTakerFeeProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feeRates = p
}

func (s *LimitProfitStrategy) resetPositionFeeOverrides() {
	s.positionBuyFeeRate = 0
	s.positionBuyLiquidity = ""
	s.positionSellFeeRate = 0
}

func (s *LimitProfitStrategy) resetTrailingStop() {
	s.trailingStopActive = false
	s.trailingHighWater = 0
}

// computePositionSize returns the position size based on sizing mode.
func (s *LimitProfitStrategy) computePositionSize(ctx context.Context, book string) float64 {
	if s.lpConfig.SizingMode != "atr_scaled" {
		return s.lpConfig.PositionSize
	}

	// ATR-scaled sizing: position_size = target_risk_quote / (ATR * multiplier)
	if s.lpConfig.TargetRiskQuote <= 0 {
		return s.lpConfig.PositionSize
	}

	ind := s.GetIndicatorService()
	if ind == nil {
		return s.lpConfig.PositionSize
	}

	// Note: ATRPeriod is stored for documentation/future use; current indicator service
	// uses a fixed period configured at service level.
	atr, err := ind.GetATR(ctx, book)
	if err != nil || atr == nil || atr.Value <= 0 {
		return s.lpConfig.PositionSize
	}

	mult := s.lpConfig.ATRMultiplier
	if mult <= 0 {
		mult = 1.0
	}

	computedSize := s.lpConfig.TargetRiskQuote / (atr.Value * mult)
	if computedSize <= 0 {
		return s.lpConfig.PositionSize
	}

	// Cap at configured max position size if set.
	if s.lpConfig.PositionSize > 0 && computedSize > s.lpConfig.PositionSize {
		return s.lpConfig.PositionSize
	}

	return computedSize
}

func normalizeLiquidity(v string) string {
	x := strings.ToLower(strings.TrimSpace(v))
	switch x {
	case "maker", "taker":
		return x
	default:
		return ""
	}
}

func (s *LimitProfitStrategy) effectiveBuyLiquidity() string {
	if s.positionBuyLiquidity != "" {
		return s.positionBuyLiquidity
	}
	if v := normalizeLiquidity(s.lpConfig.BuyLiquidity); v != "" {
		return v
	}
	return "maker"
}

// referenceExitPrice maps tick/trade price to the value compared against the exit threshold.
func (s *LimitProfitStrategy) referenceExitPrice(ctx context.Context, tickPrice float64, book string) (float64, string) {
	ref := strings.ToLower(strings.TrimSpace(s.lpConfig.ExitPriceReference))
	if ref == "" {
		ref = "last"
	}
	switch ref {
	case "bid", "mid", "min_last_bid":
		ind := s.GetIndicatorService()
		if ind == nil {
			return tickPrice, "last_fallback_no_indicator"
		}
		bid, ask, last, ok := ind.GetBookTicker(ctx, book)
		if !ok {
			return tickPrice, "last_fallback_no_ticker"
		}
		_ = last
		switch ref {
		case "bid":
			return bid, "bid"
		case "mid":
			return (bid + ask) / 2, "mid"
		case "min_last_bid":
			if bid < tickPrice {
				return bid, "min_last_bid"
			}
			return tickPrice, "min_last_bid"
		}
	}
	return tickPrice, "last"
}

func (s *LimitProfitStrategy) exitPriceThreshold(ctx context.Context, entry float64) (threshold, buyR, sellR float64, feeModel string) {
	buyR, sellR, feeModel = s.resolveFeeRates(ctx, entry)
	minProfit := s.computeMinProfit(entry)
	if feeModel == "bitso_api" {
		if sellR >= 1 || buyR < 0 || sellR < 0 || buyR >= 1 {
			s.logger().
				WithFloat64("buy_fee_r", buyR).
				WithFloat64("sell_fee_r", sellR).
				Warn("invalid Bitso fee decimals for round-trip; using manual exit threshold")
			manual := s.exitFeeAddon(entry)
			return entry + minProfit + manual, 0, 0, "manual_estimate"
		}
		be := bitso.MinExitPriceAfterRoundTrip(entry, buyR, sellR)
		if math.IsInf(be, 1) || math.IsNaN(be) {
			s.logger().
				WithFloat64("buy_fee_r", buyR).
				WithFloat64("sell_fee_r", sellR).
				WithFloat64("break_even_raw", be).
				Warn("MinExitPriceAfterRoundTrip unusable; using manual exit threshold")
			manual := s.exitFeeAddon(entry)
			return entry + minProfit + manual, 0, 0, "manual_estimate"
		}
		return be + minProfit + s.lpConfig.Fee, buyR, sellR, feeModel
	}
	manual := s.exitFeeAddon(entry)
	return entry + minProfit + manual, buyR, sellR, "manual_estimate"
}

func (s *LimitProfitStrategy) resolveFeeRates(ctx context.Context, entry float64) (buyR, sellR float64, feeModel string) {
	_ = entry
	book := s.config.Book
	buyLiq := s.effectiveBuyLiquidity()
	sellLiq := normalizeLiquidity(s.lpConfig.SellLiquidity)
	if sellLiq == "" {
		sellLiq = "taker"
	}

	if !s.lpConfig.UseBitsoFees || s.feeRates == nil {
		return 0, 0, "manual_estimate"
	}

	if ext, ok := s.feeRates.(BookFeeResolver); ok {
		br, sr, ok2 := ext.FeeDecimalsForLegs(ctx, book, buyLiq, sellLiq)
		if ok2 {
			if s.positionBuyFeeRate > 0 {
				br = s.positionBuyFeeRate
			}
			return br, sr, "bitso_api"
		}
	}

	if m, t, ok := s.feeRates.MakerTakerRatesForBook(ctx, book); ok {
		br, sr := m, t
		if s.positionBuyFeeRate > 0 {
			br = s.positionBuyFeeRate
		}
		return br, sr, "bitso_api"
	}

	return 0, 0, "manual_estimate"
}

// exitFeeAddon returns extra price margin for manual mode: fixed Fee plus symmetric bps on entry.
func (s *LimitProfitStrategy) exitFeeAddon(entryPrice float64) float64 {
	addon := s.lpConfig.Fee
	if s.lpConfig.FeeBPS > 0 && entryPrice > 0 {
		addon += entryPrice * 2.0 * s.lpConfig.FeeBPS / 10000.0
	}
	return addon
}

// computeEntryOffset returns the entry offset, using BPS if configured.
func (s *LimitProfitStrategy) computeEntryOffset(referencePrice float64) float64 {
	if s.lpConfig.EntryOffsetBPS > 0 && referencePrice > 0 {
		return referencePrice * s.lpConfig.EntryOffsetBPS / 10000.0
	}
	return s.lpConfig.EntryOffset
}

// computeMinProfit returns the min profit target, using BPS if configured.
func (s *LimitProfitStrategy) computeMinProfit(entryPrice float64) float64 {
	if s.lpConfig.MinProfitBPS > 0 && entryPrice > 0 {
		return entryPrice * s.lpConfig.MinProfitBPS / 10000.0
	}
	return s.lpConfig.MinProfit
}

// feeModelLabel returns a string describing the current fee model.
func (s *LimitProfitStrategy) feeModelLabel() string {
	if s.lpConfig.UseBitsoFees && s.feeRates != nil {
		return "bitso_api"
	}
	return "manual_estimate"
}

// cancelPendingBuyWithRetry attempts to cancel a pending buy order with retries.
func (s *LimitProfitStrategy) cancelPendingBuyWithRetry(ctx context.Context, eventID string) bool {
	maxRetries := s.lpConfig.PendingCancelMaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}
	book := s.config.Book
	log := s.logger().WithStr("event_id", eventID)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		cctx, ccancel := context.WithTimeout(ctx, 20*time.Second)
		err := s.pendingBuyCancel.CancelOrderBySignalID(cctx, eventID)
		ccancel()

		if err == nil {
			log.WithInt("attempt", attempt).Info("pending buy cancel succeeded")
			return true
		}

		log.WithInt("attempt", attempt).
			WithInt("max_retries", maxRetries).
			WithError(err).
			Warn("pending buy cancel attempt failed")

		if attempt < maxRetries {
			backoff := time.Duration(attempt*attempt) * time.Second
			time.Sleep(backoff)
		}
	}

	// All retries failed — record metric.
	if s.metrics != nil && s.metrics.PendingCancelFailures != nil {
		s.metrics.PendingCancelFailures(s.Name(), book)
	}
	log.Error("pending buy cancel exhausted retries, leaving pending state set")
	return false
}

func (s *LimitProfitStrategy) referencePrice(ctx context.Context, lastTrade float64) float64 {
	switch s.lpConfig.Reference {
	case "vwap":
		ind := s.GetIndicatorService()
		if ind == nil {
			return lastTrade
		}
		v, err := ind.GetVWAP(ctx, s.config.Book)
		if err != nil || v == nil {
			return lastTrade
		}
		return v.Value
	default:
		return lastTrade
	}
}

func (s *LimitProfitStrategy) canEmitSignal() bool {
	state := s.GetState()
	if state.LastSignalTime.IsZero() {
		return true
	}
	if s.lpConfig.MinSignalInterval <= 0 {
		return true
	}
	return time.Since(state.LastSignalTime).Seconds() >= float64(s.lpConfig.MinSignalInterval)
}

// OnBar forwards to OnTick using close as price.
func (s *LimitProfitStrategy) OnBar(bar *indicators.OHLCV) (*Signal, error) {
	t := &indicators.Trade{
		Timestamp: bar.Timestamp,
		Price:     bar.Close,
		Amount:    bar.Volume,
	}
	return s.OnTick(t)
}

// Reset clears mutex-protected state.
func (s *LimitProfitStrategy) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BaseEnhancedStrategy.Reset()
	s.resetPositionFeeOverrides()
	s.resetTrailingStop()
	s.dailyRealizedLoss = 0
	s.dailyLossResetTime = time.Time{}
	s.circuitBreakerTripped = false
	s.targetOrderSize = 0
	s.cumulativeFilledAmt = 0
	s.pendingOrderCount = 0
	s.lastHoldDiagLog = time.Time{}
	if s.metrics != nil && s.metrics.CircuitBreakerActive != nil {
		s.metrics.CircuitBreakerActive(s.Name(), s.config.Book, false)
	}
	if s.rawStateStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.rawStateStore.Delete(ctx, s.Name())
	}
}

// IsCircuitBreakerTripped returns true if the strategy is paused due to daily loss limit.
func (s *LimitProfitStrategy) IsCircuitBreakerTripped() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.circuitBreakerTripped
}

// GetDailyRealizedLoss returns the current session's cumulative realized loss.
func (s *LimitProfitStrategy) GetDailyRealizedLoss() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dailyRealizedLoss
}
