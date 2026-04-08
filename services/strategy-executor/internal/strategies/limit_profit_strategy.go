// Package strategies — limit_profit: buy near reference + offset, exit when min profit is achievable.
package strategies

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
)

// LimitProfitConfig holds parameters for the limit-profit / scalp-style strategy.
// Reference price is either the latest trade (last_trade) or VWAP (vwap).
// Buy limit = reference + entry_offset. Sell when last trade price >= entry + min_profit.
type LimitProfitConfig struct {
	Reference         string  // "last_trade" or "vwap"
	EntryOffset       float64 // added to reference for BUY limit price
	MinProfit         float64 // minimum (last - entry) required before SELL
	PositionSize      float64
	MinSignalInterval int // seconds between signals (entry or exit)
}

// DefaultLimitProfitConfig returns conservative defaults (tune per book / liquidity).
func DefaultLimitProfitConfig() LimitProfitConfig {
	return LimitProfitConfig{
		Reference:         "last_trade",
		EntryOffset:       500,
		MinProfit:         5000,
		PositionSize:      0.001,
		MinSignalInterval: 60,
	}
}

// LimitProfitStrategy buys at reference+offset then sells when market shows min profit vs entry.
type LimitProfitStrategy struct {
	*BaseEnhancedStrategy
	lpConfig LimitProfitConfig
	mu       sync.RWMutex
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

// Initialize parses StrategyConfig into lpConfig.
func (s *LimitProfitStrategy) Initialize(config StrategyConfig, indicatorSvc *indicators.Service) error {
	if err := s.BaseEnhancedStrategy.Initialize(config, indicatorSvc); err != nil {
		return err
	}
	s.lpConfig = DefaultLimitProfitConfig()
	if p := config.Parameters; p != nil {
		if v, ok := p["entry_offset"].(float64); ok {
			s.lpConfig.EntryOffset = v
		}
		if v, ok := p["min_profit"].(float64); ok {
			s.lpConfig.MinProfit = v
		}
		if v, ok := p["position_size"].(float64); ok {
			s.lpConfig.PositionSize = v
		}
		if v, ok := p["min_signal_interval"].(float64); ok {
			s.lpConfig.MinSignalInterval = int(v)
		}
		if v, ok := p["reference"].(string); ok {
			s.lpConfig.Reference = strings.ToLower(strings.TrimSpace(v))
		}
	}
	if config.Sizing.MaxPositionSize > 0 {
		s.lpConfig.PositionSize = config.Sizing.MaxPositionSize
	}
	if s.lpConfig.Reference == "" {
		s.lpConfig.Reference = "last_trade"
	}
	if s.lpConfig.PositionSize <= 0 {
		s.lpConfig.PositionSize = DefaultLimitProfitConfig().PositionSize
	}
	return nil
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

	state := s.GetState()
	price := tick.Price
	book := s.config.Book

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !state.HasPosition {
		if !s.canEmitSignal() {
			return nil, nil
		}
		ref := s.referencePrice(ctx, price)
		buyPrice := ref + s.lpConfig.EntryOffset

		s.RecordSignal()
		s.SetPosition("LONG", s.lpConfig.PositionSize, buyPrice)

		return &Signal{
			Strategy:   s.Name(),
			Book:       book,
			Side:       "BUY",
			Amount:     s.lpConfig.PositionSize,
			Price:      buyPrice,
			Confidence: 0.75,
			Reason: fmt.Sprintf(
				"limit_profit entry: ref=%.2f (%s) + offset=%.2f → buy limit %.2f",
				ref, s.lpConfig.Reference, s.lpConfig.EntryOffset, buyPrice,
			),
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"signal_type": "entry_buy",
				"reference":   ref,
			},
		}, nil
	}

	// In position: exit when last price shows at least min_profit vs entry (no min-interval gate on exit)
	if price >= state.EntryPrice+s.lpConfig.MinProfit {
		posSize := state.PositionSize
		entry := state.EntryPrice
		pnl := (price - entry) * posSize

		s.RecordSignal()
		s.RecordTrade(pnl > 0)
		s.ClearPosition()

		return &Signal{
			Strategy:   s.Name(),
			Book:       book,
			Side:       "SELL",
			Amount:     posSize,
			Price:      price,
			Confidence: 0.85,
			Reason: fmt.Sprintf(
				"limit_profit exit: last=%.2f >= entry=%.2f + min_profit=%.2f",
				price, entry, s.lpConfig.MinProfit,
			),
			Timestamp: time.Now(),
			Metadata: map[string]interface{}{
				"signal_type": "exit_sell",
				"entry_price": entry,
				"gross_pnl":   pnl,
			},
		}, nil
	}

	return nil, nil
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
}
