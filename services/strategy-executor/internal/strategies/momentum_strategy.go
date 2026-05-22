// Package strategies provides trading strategy implementations.
package strategies

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
	"github.com/google/uuid"
)

// MomentumConfig holds configuration for the momentum strategy.
//
// Note on RSIPeriod / EMAPeriod: the strategy reads indicator values via
// indicator service helpers (GetRSI / GetEMA) which currently use the
// service-wide configured periods. The fields below are accepted for
// future per-strategy period selection and surfaced in metadata so config
// drift is auditable.
type MomentumConfig struct {
	RSIPeriod         int     `json:"rsi_period" yaml:"rsi_period"`
	OverboughtLevel   float64 `json:"overbought_level" yaml:"overbought_level"`
	OversoldLevel     float64 `json:"oversold_level" yaml:"oversold_level"`
	EMAPeriod         int     `json:"ema_period" yaml:"ema_period"`
	MinSignalInterval int     `json:"min_signal_interval" yaml:"min_signal_interval"`
	PositionSize      float64 `json:"position_size" yaml:"position_size"`
	MaxPositionValue  float64 `json:"max_position_value" yaml:"max_position_value"`
	// MinConfidence gates signal emission: signals below this score are dropped.
	MinConfidence float64 `json:"min_confidence" yaml:"min_confidence"`
	// DryRun, when true, tags every emitted signal with metadata.dry_run=true so
	// trading-engine should skip execution. Mirrors limit_profit semantics.
	DryRun bool `json:"dry_run" yaml:"dry_run"`
	// Lifecycle controls (0 = disabled) — see docs/LIMIT-PROFIT-ROBUSTNESS.md.
	StopLossQuote           float64 `json:"stop_loss_quote" yaml:"stop_loss_quote"`
	MaxPositionHoldSeconds  int     `json:"max_position_hold_seconds" yaml:"max_position_hold_seconds"`
	MaxDailyLossQuote       float64 `json:"max_daily_loss_quote" yaml:"max_daily_loss_quote"`
	DailyLossResetHourUTC   int     `json:"daily_loss_reset_hour_utc" yaml:"daily_loss_reset_hour_utc"`
}

// DefaultMomentumConfig returns default configuration
func DefaultMomentumConfig() MomentumConfig {
	return MomentumConfig{
		RSIPeriod:         14,
		OverboughtLevel:   70,
		OversoldLevel:     30,
		EMAPeriod:         20,
		MinSignalInterval: 60,
		PositionSize:      0.001,
		MaxPositionValue:  15000,
		MinConfidence:     0.0,
		DryRun:            false,
	}
}

// MomentumStrategy implements an RSI + EMA momentum strategy.
// Implements OrderFillAware for realized-fee P&L (POINT-11).
type MomentumStrategy struct {
	*BaseEnhancedStrategy
	momConfig           MomentumConfig
	fees                PositionFeeRates
	dailyRealizedLoss   float64
	circuitBreakerTripped bool
	lastRSI             float64
	lastEMA             float64
	mu                  sync.RWMutex
}

// NewMomentumStrategy creates a new momentum strategy
func NewMomentumStrategy() *MomentumStrategy {
	return &MomentumStrategy{
		BaseEnhancedStrategy: NewBaseEnhancedStrategy("momentum", "1.0.0"),
		momConfig:            DefaultMomentumConfig(),
	}
}

// NewMomentumStrategyFactory returns a factory function for momentum strategy
func NewMomentumStrategyFactory() func() EnhancedStrategy {
	return func() EnhancedStrategy {
		return NewMomentumStrategy()
	}
}

// Initialize sets up the strategy with configuration
func (s *MomentumStrategy) Initialize(config StrategyConfig, indicatorSvc *indicators.Service) error {
	if err := s.BaseEnhancedStrategy.Initialize(config, indicatorSvc); err != nil {
		return err
	}

	if params := config.Parameters; params != nil {
		if v, ok := params["rsi_period"].(float64); ok {
			s.momConfig.RSIPeriod = int(v)
		}
		if v, ok := params["overbought_level"].(float64); ok {
			s.momConfig.OverboughtLevel = v
		}
		if v, ok := params["oversold_level"].(float64); ok {
			s.momConfig.OversoldLevel = v
		}
		if v, ok := params["ema_period"].(float64); ok {
			s.momConfig.EMAPeriod = int(v)
		}
		if v, ok := params["min_signal_interval"].(float64); ok {
			s.momConfig.MinSignalInterval = int(v)
		}
		if v, ok := params["position_size"].(float64); ok {
			s.momConfig.PositionSize = v
		}
		if v, ok := params["min_confidence"].(float64); ok {
			s.momConfig.MinConfidence = v
		}
		if v, ok := params["dry_run"].(bool); ok {
			s.momConfig.DryRun = v
		}
		if v, ok := params["stop_loss_quote"].(float64); ok {
			s.momConfig.StopLossQuote = v
		}
		if v, ok := params["max_position_hold_seconds"].(float64); ok {
			s.momConfig.MaxPositionHoldSeconds = int(v)
		}
		if v, ok := params["max_daily_loss_quote"].(float64); ok {
			s.momConfig.MaxDailyLossQuote = v
		}
		if v, ok := params["daily_loss_reset_hour_utc"].(float64); ok {
			s.momConfig.DailyLossResetHourUTC = int(v)
		}
	}

	if config.Sizing.MaxPositionSize > 0 {
		s.momConfig.PositionSize = config.Sizing.MaxPositionSize
	}
	if config.Sizing.MaxPositionValue > 0 {
		s.momConfig.MaxPositionValue = config.Sizing.MaxPositionValue
	}

	return nil
}

// OnTick processes a new trade tick and generates signals
func (s *MomentumStrategy) OnTick(tick *indicators.Trade) (*Signal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		return nil, nil
	}

	if !s.IsWithinSchedule() {
		return nil, nil
	}

	indicatorSvc := s.GetIndicatorService()
	if indicatorSvc == nil {
		return nil, fmt.Errorf("indicator service not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rsiVal, err := indicatorSvc.GetRSI(ctx, s.config.Book)
	if err != nil || rsiVal == nil {
		return nil, nil
	}

	emaVal, err := indicatorSvc.GetEMA(ctx, s.config.Book)
	if err != nil || emaVal == nil {
		return nil, nil
	}

	if !s.canGenerateSignal() {
		return nil, nil
	}

	price := tick.Price
	rsi := rsiVal.Value
	ema := emaVal.Value
	state := s.GetState()

	s.lastRSI = rsi
	s.lastEMA = ema

	if state.PendingSell {
		return nil, nil
	}

	if !state.HasPosition {
		if s.circuitBreakerTripped {
			return nil, nil
		}
		return s.generateEntrySignal(price, rsi, ema)
	}

	if s.circuitBreakerTripped {
		return s.emitExit(price, rsi, ema, &state, "circuit_breaker")
	}

	if s.momConfig.StopLossQuote > 0 {
		if s.stopLossTriggered(price, &state) {
			return s.emitExit(price, rsi, ema, &state, "stop_loss")
		}
	}

	if s.momConfig.MaxPositionHoldSeconds > 0 && !state.EntryTime.IsZero() {
		if time.Since(state.EntryTime) >= time.Duration(s.momConfig.MaxPositionHoldSeconds)*time.Second {
			return s.emitExit(price, rsi, ema, &state, "max_hold")
		}
	}

	return s.generateExitSignal(price, rsi, ema, &state)
}

// OnBar processes a new OHLCV bar and generates signals
func (s *MomentumStrategy) OnBar(bar *indicators.OHLCV) (*Signal, error) {
	trade := &indicators.Trade{
		Timestamp: bar.Timestamp,
		Price:     bar.Close,
		Amount:    bar.Volume,
	}
	return s.OnTick(trade)
}

// canGenerateSignal checks if enough time has passed since last signal
func (s *MomentumStrategy) canGenerateSignal() bool {
	state := s.GetState()
	if state.LastSignalTime.IsZero() {
		return true
	}

	elapsed := time.Since(state.LastSignalTime)
	return elapsed.Seconds() >= float64(s.momConfig.MinSignalInterval)
}

// generateEntrySignal generates entry signals based on RSI and price/EMA relationship
func (s *MomentumStrategy) generateEntrySignal(price, rsi, ema float64) (*Signal, error) {
	if rsi < s.momConfig.OversoldLevel && price > ema {
		confidence := s.calculateConfidence(rsi, price, ema, "BUY")
		if confidence < s.momConfig.MinConfidence {
			return nil, nil
		}
		s.RecordSignal()
		eventID := uuid.New().String()
		meta := s.signalMetadata(rsi, ema, "entry_long")
		meta["event_id"] = eventID

		return &Signal{
			Strategy:   s.Name(),
			Book:       s.config.Book,
			Side:       "BUY",
			Amount:     s.momConfig.PositionSize,
			Price:      price,
			Confidence: confidence,
			Reason:     fmt.Sprintf("RSI oversold (%.2f) with price above EMA (%.2f > %.2f)", rsi, price, ema),
			Timestamp:  time.Now(),
			Metadata:   meta,
		}, nil
	}

	if rsi > s.momConfig.OverboughtLevel && price < ema {
		confidence := s.calculateConfidence(rsi, price, ema, "SELL")
		if confidence < s.momConfig.MinConfidence {
			return nil, nil
		}
		s.RecordSignal()
		eventID := uuid.New().String()
		meta := s.signalMetadata(rsi, ema, "entry_short")
		meta["event_id"] = eventID

		return &Signal{
			Strategy:   s.Name(),
			Book:       s.config.Book,
			Side:       "SELL",
			Amount:     s.momConfig.PositionSize,
			Price:      price,
			Confidence: confidence,
			Reason:     fmt.Sprintf("RSI overbought (%.2f) with price below EMA (%.2f < %.2f)", rsi, price, ema),
			Timestamp:  time.Now(),
			Metadata:   meta,
		}, nil
	}

	return nil, nil
}

// signalMetadata builds the metadata payload for a momentum signal. The
// rsi_period / ema_period values are surfaced for audit; dry_run is set when
// the strategy was started in simulation mode.
func (s *MomentumStrategy) signalMetadata(rsi, ema float64, signalType string) map[string]interface{} {
	meta := map[string]interface{}{
		"rsi":         rsi,
		"ema":         ema,
		"signal_type": signalType,
		"rsi_period":  s.momConfig.RSIPeriod,
		"ema_period":  s.momConfig.EMAPeriod,
	}
	if s.momConfig.DryRun {
		meta["dry_run"] = true
	}
	return meta
}

// generateExitSignal emits an exit when RSI returns to neutral.
func (s *MomentumStrategy) generateExitSignal(price, rsi, ema float64, state *StrategyState) (*Signal, error) {
	rsiNeutral := rsi > s.momConfig.OversoldLevel && rsi < s.momConfig.OverboughtLevel
	if !rsiNeutral {
		return nil, nil
	}
	return s.emitExit(price, rsi, ema, state, "take_profit")
}

func (s *MomentumStrategy) emitExit(price, rsi, ema float64, state *StrategyState, exitReason string) (*Signal, error) {
	var side string
	var reason string

	if state.PositionSide == "BUY" || state.PositionSide == "LONG" {
		side = "SELL"
		reason = fmt.Sprintf("momentum %s: closing long (rsi=%.2f)", exitReason, rsi)
	} else {
		side = "BUY"
		reason = fmt.Sprintf("momentum %s: closing short (rsi=%.2f)", exitReason, rsi)
	}

	pnl := s.calculateUnrealizedPnL(price, state)
	eventID := uuid.New().String()
	s.RecordSignal()
	s.UpdateState(func(st *StrategyState) {
		st.PendingSell = true
		st.PendingSellSince = time.Now()
		st.PendingSellEventID = eventID
	})

	meta := map[string]interface{}{
		"rsi":            rsi,
		"ema":            ema,
		"entry_price":    state.EntryPrice,
		"unrealized_pnl": pnl,
		"signal_type":    "exit",
		"exit_reason":    exitReason,
		"rsi_period":     s.momConfig.RSIPeriod,
		"ema_period":     s.momConfig.EMAPeriod,
		"event_id":       eventID,
	}
	if s.momConfig.DryRun {
		meta["dry_run"] = true
	}

	return &Signal{
		Strategy:   s.Name(),
		Book:       s.config.Book,
		Side:       side,
		Amount:     state.PositionSize,
		Price:      price,
		Confidence: 0.75,
		Reason:     reason,
		Timestamp:  time.Now(),
		Metadata:   meta,
	}, nil
}

func (s *MomentumStrategy) stopLossTriggered(price float64, state *StrategyState) bool {
	if state.EntryPrice == 0 {
		return false
	}
	if state.PositionSide == "BUY" || state.PositionSide == "LONG" {
		return price <= state.EntryPrice-s.momConfig.StopLossQuote
	}
	return price >= state.EntryPrice+s.momConfig.StopLossQuote
}

// OnOrderFilled records realized fees and position state from exchange fills.
func (s *MomentumStrategy) OnOrderFilled(fill OrderFill) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.maybeResetDailyLossLocked()

	side := strings.ToLower(strings.TrimSpace(fill.Side))
	switch side {
	case "buy":
		s.handleBuyFillLocked(fill)
	case "sell":
		s.handleSellFillLocked(fill)
	}
}

func (s *MomentumStrategy) handleBuyFillLocked(fill OrderFill) {
	st := s.GetState()
	if st.PendingSell && st.PendingSellEventID != "" && fill.EventID == st.PendingSellEventID {
		s.finalizeExitFillLocked(fill)
		return
	}
	if fill.AveragePrice <= 0 || fill.FilledAmount <= 0 {
		return
	}
	ApplyBuyFillFee(fill, &s.fees)
	s.SetPosition("LONG", fill.FilledAmount, fill.AveragePrice)
}

func (s *MomentumStrategy) handleSellFillLocked(fill OrderFill) {
	st := s.GetState()
	if !st.PendingSell || st.PendingSellEventID == "" || fill.EventID != st.PendingSellEventID {
		return
	}
	s.finalizeExitFillLocked(fill)
}

func (s *MomentumStrategy) finalizeExitFillLocked(fill OrderFill) {
	st := s.GetState()
	if !st.HasPosition || fill.AveragePrice <= 0 {
		return
	}
	ApplySellFillFee(fill, &s.fees)

	entry := st.EntryPrice
	exit := fill.AveragePrice
	size := st.PositionSize
	if size <= 0 {
		size = fill.FilledAmount
	}

	_, net, feeModel := RealizedQuotePnL(entry, exit, size, s.fees.BuyFeeRate, s.fees.SellFeeRate)
	profitable := net > 0
	if s.fees.BuyFeeRate == 0 || s.fees.SellFeeRate == 0 {
		gross := (exit - entry) * size
		if st.PositionSide != "BUY" && st.PositionSide != "LONG" {
			gross = (entry - exit) * size
		}
		profitable = gross > 0
		net = gross
	} else if st.PositionSide != "BUY" && st.PositionSide != "LONG" {
		net = (entry - exit) * size
		if s.fees.BuyFeeRate > 0 && s.fees.SellFeeRate > 0 {
			_, net, _ = RealizedQuotePnL(entry, exit, size, s.fees.SellFeeRate, s.fees.BuyFeeRate)
		}
	}

	s.RecordTradeWithPnL(profitable, net)
	if !profitable && net < 0 {
		s.dailyRealizedLoss += -net
		if s.momConfig.MaxDailyLossQuote > 0 && s.dailyRealizedLoss >= s.momConfig.MaxDailyLossQuote {
			s.circuitBreakerTripped = true
		}
	}

	s.fees = PositionFeeRates{}
	s.ClearPosition()
	s.UpdateState(func(st *StrategyState) {
		st.PendingSell = false
		st.PendingSellEventID = ""
		st.PendingSellSince = time.Time{}
	})
	_ = feeModel
}

func (s *MomentumStrategy) maybeResetDailyLossLocked() {
	if s.momConfig.MaxDailyLossQuote <= 0 {
		return
	}
	now := time.Now().UTC()
	if now.Hour() == s.momConfig.DailyLossResetHourUTC && now.Minute() < 2 {
		s.dailyRealizedLoss = 0
		s.circuitBreakerTripped = false
	}
}

// calculateConfidence calculates signal confidence based on RSI extremity
func (s *MomentumStrategy) calculateConfidence(rsi, price, ema float64, side string) float64 {
	var rsiExtremity float64

	if side == "BUY" {
		rsiExtremity = (s.momConfig.OversoldLevel - rsi) / s.momConfig.OversoldLevel
	} else {
		rsiExtremity = (rsi - s.momConfig.OverboughtLevel) / (100 - s.momConfig.OverboughtLevel)
	}

	priceEMADiff := (price - ema) / ema

	if side == "BUY" && priceEMADiff > 0.01 {
		rsiExtremity += 0.1
	} else if side == "SELL" && priceEMADiff < -0.01 {
		rsiExtremity += 0.1
	}

	confidence := 0.5 + rsiExtremity*0.4
	if confidence > 0.95 {
		confidence = 0.95
	}
	if confidence < 0.5 {
		confidence = 0.5
	}

	return confidence
}

// calculateUnrealizedPnL calculates unrealized P&L for current position
func (s *MomentumStrategy) calculateUnrealizedPnL(currentPrice float64, state *StrategyState) float64 {
	if !state.HasPosition || state.EntryPrice == 0 {
		return 0
	}

	if state.PositionSide == "BUY" || state.PositionSide == "LONG" {
		return (currentPrice - state.EntryPrice) * state.PositionSize
	}

	return (state.EntryPrice - currentPrice) * state.PositionSize
}

// GetMomentumConfig returns the strategy-specific configuration
func (s *MomentumStrategy) GetMomentumConfig() MomentumConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.momConfig
}

// Reset resets the strategy state
func (s *MomentumStrategy) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BaseEnhancedStrategy.Reset()
	s.fees = PositionFeeRates{}
	s.dailyRealizedLoss = 0
	s.circuitBreakerTripped = false
	s.lastRSI = 0
	s.lastEMA = 0
}

// IsWithinSchedule checks if current time is within trading schedule
func (s *MomentumStrategy) IsWithinSchedule() bool {
	schedule := s.config.Schedule
	if schedule.ActiveHours == "" {
		return true
	}

	loc := time.UTC
	if schedule.Timezone != "" {
		if parsedLoc, err := time.LoadLocation(schedule.Timezone); err == nil {
			loc = parsedLoc
		}
	}

	now := time.Now().In(loc)

	if len(schedule.ActiveDays) > 0 {
		dayName := now.Weekday().String()[:3]
		found := false
		for _, d := range schedule.ActiveDays {
			if d == dayName {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if schedule.ActiveHours != "" {
		var startHour, startMin, endHour, endMin int
		_, err := fmt.Sscanf(schedule.ActiveHours, "%d:%d-%d:%d", &startHour, &startMin, &endHour, &endMin)
		if err == nil {
			nowMinutes := now.Hour()*60 + now.Minute()
			startMinutes := startHour*60 + startMin
			endMinutes := endHour*60 + endMin

			if nowMinutes < startMinutes || nowMinutes > endMinutes {
				return false
			}
		}
	}

	return true
}
