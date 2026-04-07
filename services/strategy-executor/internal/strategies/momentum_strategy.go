// Package strategies provides trading strategy implementations.
package strategies

import (
	"context"
	"fmt"
	"sync"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// MomentumConfig holds configuration for the momentum strategy
type MomentumConfig struct {
	RSIPeriod          int     `json:"rsi_period" yaml:"rsi_period"`
	OverboughtLevel    float64 `json:"overbought_level" yaml:"overbought_level"`
	OversoldLevel      float64 `json:"oversold_level" yaml:"oversold_level"`
	EMAPeriod          int     `json:"ema_period" yaml:"ema_period"`
	MinSignalInterval  int     `json:"min_signal_interval" yaml:"min_signal_interval"`
	PositionSize       float64 `json:"position_size" yaml:"position_size"`
	MaxPositionValue   float64 `json:"max_position_value" yaml:"max_position_value"`
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
	}
}

// MomentumStrategy implements an RSI + EMA momentum strategy
type MomentumStrategy struct {
	*BaseEnhancedStrategy
	momConfig MomentumConfig
	lastRSI   float64
	lastEMA   float64
	mu        sync.RWMutex
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

	if !state.HasPosition {
		return s.generateEntrySignal(price, rsi, ema)
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
		s.RecordSignal()

		return &Signal{
			Strategy:   s.Name(),
			Book:       s.config.Book,
			Side:       "BUY",
			Amount:     s.momConfig.PositionSize,
			Price:      price,
			Confidence: confidence,
			Reason:     fmt.Sprintf("RSI oversold (%.2f) with price above EMA (%.2f > %.2f)", rsi, price, ema),
			Timestamp:  time.Now(),
			Metadata: map[string]interface{}{
				"rsi":         rsi,
				"ema":         ema,
				"signal_type": "entry_long",
			},
		}, nil
	}

	if rsi > s.momConfig.OverboughtLevel && price < ema {
		confidence := s.calculateConfidence(rsi, price, ema, "SELL")
		s.RecordSignal()

		return &Signal{
			Strategy:   s.Name(),
			Book:       s.config.Book,
			Side:       "SELL",
			Amount:     s.momConfig.PositionSize,
			Price:      price,
			Confidence: confidence,
			Reason:     fmt.Sprintf("RSI overbought (%.2f) with price below EMA (%.2f < %.2f)", rsi, price, ema),
			Timestamp:  time.Now(),
			Metadata: map[string]interface{}{
				"rsi":         rsi,
				"ema":         ema,
				"signal_type": "entry_short",
			},
		}, nil
	}

	return nil, nil
}

// generateExitSignal generates exit signals when RSI returns to neutral
func (s *MomentumStrategy) generateExitSignal(price, rsi, ema float64, state *StrategyState) (*Signal, error) {
	rsiNeutral := rsi > s.momConfig.OversoldLevel && rsi < s.momConfig.OverboughtLevel

	if !rsiNeutral {
		return nil, nil
	}

	var side string
	var reason string

	if state.PositionSide == "BUY" || state.PositionSide == "LONG" {
		side = "SELL"
		reason = fmt.Sprintf("RSI returned to neutral (%.2f) - closing long", rsi)
	} else {
		side = "BUY"
		reason = fmt.Sprintf("RSI returned to neutral (%.2f) - closing short", rsi)
	}

	pnl := s.calculateUnrealizedPnL(price, state)
	profitable := pnl > 0

	s.RecordSignal()
	s.RecordTrade(profitable)
	s.ClearPosition()

	return &Signal{
		Strategy:   s.Name(),
		Book:       s.config.Book,
		Side:       side,
		Amount:     state.PositionSize,
		Price:      price,
		Confidence: 0.75,
		Reason:     reason,
		Timestamp:  time.Now(),
		Metadata: map[string]interface{}{
			"rsi":            rsi,
			"ema":            ema,
			"entry_price":    state.EntryPrice,
			"unrealized_pnl": pnl,
			"signal_type":    "exit",
		},
	}, nil
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
