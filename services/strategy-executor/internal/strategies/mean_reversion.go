// Package strategies provides trading strategy implementations.
package strategies

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/strategy-executor/internal/indicators"
	"github.com/google/uuid"
)

// MeanReversionConfig holds configuration for the mean reversion strategy
type MeanReversionConfig struct {
	LookbackPeriod       int     `json:"lookback_period" yaml:"lookback_period"`
	EntryThreshold       float64 `json:"entry_threshold" yaml:"entry_threshold"`
	ExitThreshold        float64 `json:"exit_threshold" yaml:"exit_threshold"`
	MinSignalInterval    int     `json:"min_signal_interval" yaml:"min_signal_interval"`
	PositionSize         float64 `json:"position_size" yaml:"position_size"`
	MaxPositionValue     float64 `json:"max_position_value" yaml:"max_position_value"`
	MinNetProfitBPS      float64 `json:"min_net_profit_bps" yaml:"min_net_profit_bps"`
	FallbackRoundTripBPS float64 `json:"fallback_round_trip_bps" yaml:"fallback_round_trip_bps"`
	StopLossBPS          float64 `json:"stop_loss_bps" yaml:"stop_loss_bps"`
}

// DefaultMeanReversionConfig returns conservative default configuration
func DefaultMeanReversionConfig() MeanReversionConfig {
	return MeanReversionConfig{
		LookbackPeriod:    20,
		EntryThreshold:    2.0,
		ExitThreshold:     0.5,
		MinSignalInterval: 60,
		PositionSize:      0.001,
		MaxPositionValue:  15000,
	}
}

// MeanReversionStrategy implements a Bollinger Bands mean reversion strategy.
// Implements OrderFillAware for realized-fee P&L (POINT-11).
type MeanReversionStrategy struct {
	*BaseEnhancedStrategy
	mrConfig MeanReversionConfig
	fees     PositionFeeRates
	feeGate  FeeGate
	provider MakerTakerFeeProvider
	mu       sync.RWMutex
}

// NewMeanReversionStrategy creates a new mean reversion strategy
func NewMeanReversionStrategy() *MeanReversionStrategy {
	return &MeanReversionStrategy{
		BaseEnhancedStrategy: NewBaseEnhancedStrategy("mean_reversion", "1.0.0"),
		mrConfig:             DefaultMeanReversionConfig(),
	}
}

// NewMeanReversionStrategyFactory returns a factory function for mean reversion strategy
func NewMeanReversionStrategyFactory() func() EnhancedStrategy {
	return func() EnhancedStrategy {
		return NewMeanReversionStrategy()
	}
}

// Initialize sets up the strategy with configuration
func (s *MeanReversionStrategy) Initialize(config StrategyConfig, indicatorSvc *indicators.Service) error {
	if err := s.BaseEnhancedStrategy.Initialize(config, indicatorSvc); err != nil {
		return err
	}

	if params := config.Parameters; params != nil {
		if v, ok := params["lookback_period"].(float64); ok {
			s.mrConfig.LookbackPeriod = int(v)
		}
		if v, ok := params["entry_threshold"].(float64); ok {
			s.mrConfig.EntryThreshold = v
		}
		if v, ok := params["exit_threshold"].(float64); ok {
			s.mrConfig.ExitThreshold = v
		}
		if v, ok := params["min_signal_interval"].(float64); ok {
			s.mrConfig.MinSignalInterval = int(v)
		}
		if v, ok := params["min_net_profit_bps"].(float64); ok {
			s.mrConfig.MinNetProfitBPS = v
		}
		if v, ok := params["fallback_round_trip_bps"].(float64); ok {
			s.mrConfig.FallbackRoundTripBPS = v
		}
		if v, ok := params["stop_loss_bps"].(float64); ok {
			s.mrConfig.StopLossBPS = v
		}
	}

	s.mrConfig.PositionSize = ResolvePositionSize(
		config.Parameters, config.Sizing.MaxPositionSize, s.mrConfig.PositionSize)
	if config.Sizing.MaxPositionValue > 0 {
		s.mrConfig.MaxPositionValue = config.Sizing.MaxPositionValue
	}

	s.feeGate = NewFeeGate(s.provider, s.mrConfig.FallbackRoundTripBPS)

	return nil
}

// SetFeeRatesProvider sets the fee provider.
func (s *MeanReversionStrategy) SetFeeRatesProvider(provider MakerTakerFeeProvider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.provider = provider
	s.feeGate = NewFeeGate(provider, s.mrConfig.FallbackRoundTripBPS)
}

// entryClearsCost reports whether an expected move from entryPrice to
// expectedExit covers the round-trip fee plus the configured MinNetProfitBPS
// margin.
//
// When neither a fee provider nor a fallback estimate is configured the gate
// is a no-op and returns true. That keeps every existing deployment and test
// on exactly its current behaviour: fee gating only takes effect once it has
// been deliberately switched on with real inputs.
func (s *MeanReversionStrategy) entryClearsCost(dir PositionDirection, entryPrice, expectedExit float64) bool {
	if !s.feeGate.HasRates() {
		return true
	}
	minNet := entryPrice * s.mrConfig.MinNetProfitBPS / 10000.0
	ok, _, _ := s.feeGate.EntryClearsCost(
		context.Background(), s.config.Book, dir, entryPrice, expectedExit, minNet)
	return ok
}

// OnTick processes a new trade tick and generates signals
func (s *MeanReversionStrategy) OnTick(tick *indicators.Trade) (*Signal, error) {
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

	bb, err := indicatorSvc.GetBollinger(ctx, s.config.Book)
	if err != nil {
		return nil, fmt.Errorf("get bollinger bands: %w", err)
	}
	if bb == nil {
		return nil, nil
	}

	if !s.canGenerateSignal() {
		return nil, nil
	}

	price := tick.Price
	state := s.GetState()

	if state.PendingSell {
		return nil, nil
	}

	if !state.HasPosition {
		return s.generateEntrySignal(price, bb)
	}

	return s.generateExitSignal(price, bb, &state)
}

// OnBar processes a new OHLCV bar and generates signals
func (s *MeanReversionStrategy) OnBar(bar *indicators.OHLCV) (*Signal, error) {
	trade := &indicators.Trade{
		Timestamp: bar.Timestamp,
		Price:     bar.Close,
		Amount:    bar.Volume,
	}
	return s.OnTick(trade)
}

// canGenerateSignal checks if enough time has passed since last signal
func (s *MeanReversionStrategy) canGenerateSignal() bool {
	state := s.GetState()
	if state.LastSignalTime.IsZero() {
		return true
	}

	elapsed := time.Since(state.LastSignalTime)
	return elapsed.Seconds() >= float64(s.mrConfig.MinSignalInterval)
}

// generateEntrySignal generates entry signals based on Bollinger Bands
func (s *MeanReversionStrategy) generateEntrySignal(price float64, bb *indicators.BollingerBands) (*Signal, error) {
	if price < bb.Lower {
		// The reversion target is the middle band — that is where
		// generateExitSignal will close. Gate the entry on whether that move
		// actually clears round-trip fees.
		if !s.entryClearsCost(DirectionLong, price, bb.Middle) {
			return nil, nil
		}

		confidence := s.calculateConfidence(price, bb)
		s.RecordSignal()
		eventID := uuid.New().String()

		return &Signal{
			Strategy:   s.Name(),
			Book:       s.config.Book,
			Side:       "BUY",
			Amount:     s.mrConfig.PositionSize,
			Price:      price,
			Confidence: confidence,
			Reason:     fmt.Sprintf("Price %.2f below lower band %.2f (%.2f std devs)", price, bb.Lower, s.getDeviations(price, bb)),
			Timestamp:  time.Now(),
			Metadata: map[string]interface{}{
				"upper_band":  bb.Upper,
				"middle_band": bb.Middle,
				"lower_band":  bb.Lower,
				"std_dev":     bb.StdDev,
				"signal_type": "entry_long",
				"event_id":    eventID,
			},
		}, nil
	}

	if price > bb.Upper {
		// Short entry: sell high now, buy back at the middle band. This is a
		// SHORT round-trip, so it must be evaluated with short fee arithmetic —
		// scoring it as a long would compare a high entry against a lower exit
		// and suppress every short unconditionally.
		if !s.entryClearsCost(DirectionShort, price, bb.Middle) {
			return nil, nil
		}

		confidence := s.calculateConfidence(price, bb)
		s.RecordSignal()
		eventID := uuid.New().String()

		return &Signal{
			Strategy:   s.Name(),
			Book:       s.config.Book,
			Side:       "SELL",
			Amount:     s.mrConfig.PositionSize,
			Price:      price,
			Confidence: confidence,
			Reason:     fmt.Sprintf("Price %.2f above upper band %.2f (%.2f std devs)", price, bb.Upper, s.getDeviations(price, bb)),
			Timestamp:  time.Now(),
			Metadata: map[string]interface{}{
				"upper_band":  bb.Upper,
				"middle_band": bb.Middle,
				"lower_band":  bb.Lower,
				"std_dev":     bb.StdDev,
				"signal_type": "entry_short",
				"event_id":    eventID,
			},
		}, nil
	}

	return nil, nil
}

// generateExitSignal generates exit signals when price returns to mean
func (s *MeanReversionStrategy) generateExitSignal(price float64, bb *indicators.BollingerBands, state *StrategyState) (*Signal, error) {
	deviations := s.getDeviations(price, bb)

	// STOP-LOSS logic
	if state.HasPosition && state.EntryPrice > 0 && s.mrConfig.StopLossBPS > 0 {
		var drawdownBPS float64
		if state.PositionSide == "BUY" || state.PositionSide == "LONG" {
			drawdownBPS = (state.EntryPrice - price) / state.EntryPrice * 10000.0
		} else {
			drawdownBPS = (price - state.EntryPrice) / state.EntryPrice * 10000.0
		}

		if drawdownBPS > s.mrConfig.StopLossBPS {
			var side string
			if state.PositionSide == "BUY" || state.PositionSide == "LONG" {
				side = "SELL"
			} else {
				side = "BUY"
			}
			pnl := s.calculateUnrealizedPnL(price, state)
			eventID := uuid.New().String()
			s.RecordSignal()
			s.UpdateState(func(st *StrategyState) {
				st.PendingSell = true
				st.PendingSellSince = time.Now()
				st.PendingSellEventID = eventID
			})

			return &Signal{
				Strategy:   s.Name(),
				Book:       s.config.Book,
				Side:       side,
				Amount:     state.PositionSize,
				Price:      price,
				Confidence: 0.9,
				Reason:     fmt.Sprintf("Stop-loss triggered (drawdown %.2f BPS > %.2f)", drawdownBPS, s.mrConfig.StopLossBPS),
				Timestamp:  time.Now(),
				Metadata: map[string]interface{}{
					"upper_band":     bb.Upper,
					"middle_band":    bb.Middle,
					"lower_band":     bb.Lower,
					"entry_price":    state.EntryPrice,
					"unrealized_pnl": pnl,
					"signal_type":    "exit",
					"exit_reason":    "stop_loss",
					"event_id":       eventID,
				},
			}, nil
		}
	}

	if math.Abs(deviations) < s.mrConfig.ExitThreshold {
		var side string
		if state.PositionSide == "BUY" || state.PositionSide == "LONG" {
			side = "SELL"
		} else {
			side = "BUY"
		}

		if state.EntryPrice > 0 {
			dir := DirectionLong
			if state.PositionSide != "BUY" && state.PositionSide != "LONG" {
				dir = DirectionShort
			}
			// A voluntary take-profit must not realize a net loss. Stop-loss is
			// evaluated above and returns before reaching here, so this guard
			// cannot block a risk exit.
			if ok, _, _ := s.feeGate.ExitIsNetProfitable(context.Background(), s.config.Book, dir, state.EntryPrice, price); !ok {
				return nil, nil
			}
		}

		pnl := s.calculateUnrealizedPnL(price, state)
		eventID := uuid.New().String()
		s.RecordSignal()
		s.UpdateState(func(st *StrategyState) {
			st.PendingSell = true
			st.PendingSellSince = time.Now()
			st.PendingSellEventID = eventID
		})

		return &Signal{
			Strategy:   s.Name(),
			Book:       s.config.Book,
			Side:       side,
			Amount:     state.PositionSize,
			Price:      price,
			Confidence: 0.8,
			Reason:     fmt.Sprintf("Price returned to mean (%.2f std devs from middle)", deviations),
			Timestamp:  time.Now(),
			Metadata: map[string]interface{}{
				"upper_band":     bb.Upper,
				"middle_band":    bb.Middle,
				"lower_band":     bb.Lower,
				"entry_price":    state.EntryPrice,
				"unrealized_pnl": pnl,
				"signal_type":    "exit",
				"exit_reason":    "take_profit",
				"event_id":       eventID,
			},
		}, nil
	}

	return nil, nil
}

// OnOrderFilled applies realized fees and updates position from exchange fills.
func (s *MeanReversionStrategy) OnOrderFilled(fill OrderFill) {
	s.mu.Lock()
	defer s.mu.Unlock()

	side := strings.ToLower(strings.TrimSpace(fill.Side))
	switch side {
	case "buy":
		s.handleBuyFillLocked(fill)
	case "sell":
		s.handleSellFillLocked(fill)
	}
}

func (s *MeanReversionStrategy) handleBuyFillLocked(fill OrderFill) {
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

func (s *MeanReversionStrategy) handleSellFillLocked(fill OrderFill) {
	st := s.GetState()
	if !st.PendingSell || st.PendingSellEventID == "" || fill.EventID != st.PendingSellEventID {
		return
	}
	s.finalizeExitFillLocked(fill)
}

func (s *MeanReversionStrategy) finalizeExitFillLocked(fill OrderFill) {
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

	_, net, _ := RealizedQuotePnL(entry, exit, size, s.fees.BuyFeeRate, s.fees.SellFeeRate)
	profitable := net > 0
	if s.fees.BuyFeeRate == 0 || s.fees.SellFeeRate == 0 {
		gross := (exit - entry) * size
		if st.PositionSide != "BUY" && st.PositionSide != "LONG" {
			gross = (entry - exit) * size
		}
		profitable = gross > 0
		net = gross
	}

	s.RecordTradeWithPnL(profitable, net)
	s.fees = PositionFeeRates{}
	s.ClearPosition()
	s.UpdateState(func(st *StrategyState) {
		st.PendingSell = false
		st.PendingSellEventID = ""
		st.PendingSellSince = time.Time{}
	})
}

// getDeviations returns how many standard deviations price is from middle band
func (s *MeanReversionStrategy) getDeviations(price float64, bb *indicators.BollingerBands) float64 {
	if bb.StdDev == 0 {
		return 0
	}
	return (price - bb.Middle) / bb.StdDev
}

// calculateConfidence calculates signal confidence based on distance from bands
func (s *MeanReversionStrategy) calculateConfidence(price float64, bb *indicators.BollingerBands) float64 {
	deviations := math.Abs(s.getDeviations(price, bb))

	if deviations >= 3.0 {
		return 0.95
	}
	if deviations >= 2.5 {
		return 0.85
	}
	if deviations >= 2.0 {
		return 0.75
	}

	return 0.5 + (deviations-1.0)*0.25
}

// calculateUnrealizedPnL calculates unrealized P&L for current position
func (s *MeanReversionStrategy) calculateUnrealizedPnL(currentPrice float64, state *StrategyState) float64 {
	if !state.HasPosition || state.EntryPrice == 0 {
		return 0
	}

	if state.PositionSide == "BUY" || state.PositionSide == "LONG" {
		return (currentPrice - state.EntryPrice) * state.PositionSize
	}

	return (state.EntryPrice - currentPrice) * state.PositionSize
}

// GetMeanReversionConfig returns the strategy-specific configuration
func (s *MeanReversionStrategy) GetMeanReversionConfig() MeanReversionConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mrConfig
}

// SetMeanReversionConfig updates the strategy-specific configuration
func (s *MeanReversionStrategy) SetMeanReversionConfig(config MeanReversionConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mrConfig = config
}

// Reset resets the strategy state
func (s *MeanReversionStrategy) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BaseEnhancedStrategy.Reset()
	s.fees = PositionFeeRates{}
}

// IsWithinSchedule checks if current time is within trading schedule
func (s *MeanReversionStrategy) IsWithinSchedule() bool {
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
