// Package strategies — limit_profit: buy near reference + offset, exit when min profit is achievable.
package strategies

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/strategy-executor/internal/indicators"

	"github.com/google/uuid"
)

// LimitProfitConfig holds parameters for the limit-profit / scalp-style strategy.
// Reference price is either the latest trade (last_trade) or VWAP (vwap).
// Buy limit = reference + entry_offset. Exit when last >= threshold; threshold uses Bitso GET /fees
// when enabled and configured, else entry + min_profit + manual fee_addon (see exitFeeAddon).
type LimitProfitConfig struct {
	Reference         string  // "last_trade" or "vwap"
	EntryOffset       float64 // added to reference for BUY limit price
	MinProfit         float64 // minimum price move above entry before SELL (strategy target, before fees)
	Fee               float64 // extra price margin (same units as book), always added on top of threshold
	FeeBPS            float64 // manual mode only: symmetric bps × entry / 10_000 × 2 (ignored when Bitso fees apply)
	UseBitsoFees      bool    // use MakerTakerFeeProvider (GET /fees) when true and provider returns ok
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
	lpConfig  LimitProfitConfig
	feeRates  MakerTakerFeeProvider
	mu        sync.RWMutex
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
	s.lpConfig.UseBitsoFees = true
	if p := config.Parameters; p != nil {
		if v, ok := p["entry_offset"].(float64); ok {
			s.lpConfig.EntryOffset = v
		}
		if v, ok := p["min_profit"].(float64); ok {
			s.lpConfig.MinProfit = v
		}
		if v, ok := p["fee"].(float64); ok {
			s.lpConfig.Fee = v
		}
		if v, ok := p["fee_bps"].(float64); ok {
			s.lpConfig.FeeBPS = v
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
		if v, ok := p["use_bitso_fees"].(bool); ok {
			s.lpConfig.UseBitsoFees = v
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
		if state.PendingBuy {
			// BUY limit is working at the exchange; wait for fill notification before monitoring profit.
			return nil, nil
		}
		if !s.canEmitSignal() {
			return nil, nil
		}
		ref := s.referencePrice(ctx, price)
		buyPrice := ref + s.lpConfig.EntryOffset

		eventID := uuid.New().String()
		s.RecordSignal()
		s.UpdateState(func(st *StrategyState) {
			st.PendingBuy = true
			st.PendingEventID = eventID
		})

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
				"event_id":    eventID,
			},
		}, nil
	}

	// In position: exit when last >= threshold (Bitso maker/taker from GET /fees when available)
	entry := state.EntryPrice
	threshold, maker, taker, feeModel := s.exitPriceThreshold(ctx, entry)
	manualAddon := s.exitFeeAddon(entry)
	if price >= threshold {
		posSize := state.PositionSize
		gross := (price - entry) * posSize
		netQuote := bitso.NetQuotePnLPerBase(entry, price, maker, taker) * posSize
		profitable := gross > 0
		if feeModel == "bitso_api" {
			profitable = netQuote > 0
		}

		s.RecordSignal()
		s.RecordTrade(profitable)
		s.ClearPosition()

		meta := map[string]interface{}{
			"signal_type":      "exit_sell",
			"entry_price":      entry,
			"gross_quote_pnl":  gross,
			"exit_threshold":   threshold,
			"fee_model":        feeModel,
			"min_profit":       s.lpConfig.MinProfit,
			"extra_fee_margin": s.lpConfig.Fee,
		}
		if feeModel == "bitso_api" {
			meta["net_quote_pnl"] = netQuote
			meta["maker_fee_rate"] = maker
			meta["taker_fee_rate"] = taker
		} else {
			meta["fee_addon_manual"] = manualAddon
		}

		return &Signal{
			Strategy:   s.Name(),
			Book:       book,
			Side:       "SELL",
			Amount:     posSize,
			Price:      price,
			Confidence: 0.85,
			Reason: fmt.Sprintf(
				"limit_profit exit [%s]: last=%.2f >= threshold=%.2f (entry=%.2f min_profit=%.2f)",
				feeModel, price, threshold, entry, s.lpConfig.MinProfit,
			),
			Timestamp: time.Now(),
			Metadata:  meta,
		}, nil
	}

	return nil, nil
}

// OnOrderFilled opens the position when the BUY limit fills at the exchange. Correlates via event_id on the signal.
func (s *LimitProfitStrategy) OnOrderFilled(eventID, book, side string, avgPrice, filledAmount float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		return
	}
	st := s.GetState()
	if !st.PendingBuy || st.PendingEventID == "" || eventID != st.PendingEventID {
		return
	}
	if book != s.config.Book {
		return
	}
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "buy", "purchase":
	default:
		return
	}
	if avgPrice <= 0 {
		return
	}
	size := filledAmount
	if size <= 0 {
		size = s.lpConfig.PositionSize
	}
	s.SetPosition("LONG", size, avgPrice)
	s.UpdateState(func(out *StrategyState) {
		out.PendingBuy = false
		out.PendingEventID = ""
	})
}

// SetFeeRatesProvider injects the registry-wide Bitso fee source (optional).
func (s *LimitProfitStrategy) SetFeeRatesProvider(p MakerTakerFeeProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.feeRates = p
}

// exitPriceThreshold returns the minimum last price to emit SELL. feeModel is "bitso_api" or "manual_estimate".
func (s *LimitProfitStrategy) exitPriceThreshold(ctx context.Context, entry float64) (threshold, maker, taker float64, feeModel string) {
	if s.lpConfig.UseBitsoFees && s.feeRates != nil {
		if m, t, ok := s.feeRates.MakerTakerRatesForBook(ctx, s.config.Book); ok {
			be := bitso.MinExitPriceAfterFees(entry, m, t)
			return be + s.lpConfig.MinProfit + s.lpConfig.Fee, m, t, "bitso_api"
		}
	}
	manual := s.exitFeeAddon(entry)
	return entry + s.lpConfig.MinProfit + manual, 0, 0, "manual_estimate"
}

// exitFeeAddon returns extra price margin for the exit threshold: fixed Fee plus a symmetric bps estimate on entry.
// fee_bps is applied as 2 * entry * (fee_bps/10000), approximating buy+sell commission on notional ≈ entry × size each leg.
func (s *LimitProfitStrategy) exitFeeAddon(entryPrice float64) float64 {
	addon := s.lpConfig.Fee
	if s.lpConfig.FeeBPS > 0 && entryPrice > 0 {
		addon += entryPrice * 2.0 * s.lpConfig.FeeBPS / 10000.0
	}
	return addon
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
