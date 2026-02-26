package strategy

import (
	"fmt"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/indicators"
)

// VWAPDeviationStrategy trades when price deviates from VWAP (institutional-style).
// Buy when price below VWAP (discount), sell when above (premium). Uses rolling session volume.
const maxPVs = 2000

// VWAPDeviationStrategy implements VWAP deviation intraday strategy.
type VWAPDeviationStrategy struct {
	name string

	// Parameters: deviation threshold in decimal (e.g. -0.002 = buy when 0.2% below VWAP)
	buyThreshold  float64 // negative: price must be below VWAP
	sellThreshold float64 // positive: price must be above VWAP
	minInterval   time.Duration

	// State
	pvs           []indicators.PriceVolume
	lastSignalTime time.Time
}

// NewVWAPDeviationStrategy creates a new VWAP deviation strategy.
func NewVWAPDeviationStrategy(params map[string]interface{}) (Strategy, error) {
	s := &VWAPDeviationStrategy{
		name:           "vwap_deviation",
		buyThreshold:   -0.002, // buy when 0.2% below VWAP
		sellThreshold:  0.002,  // sell when 0.2% above VWAP
		minInterval:    30 * time.Second,
		pvs:            make([]indicators.PriceVolume, 0, maxPVs),
	}
	if err := s.Initialize(params); err != nil {
		return nil, err
	}
	return s, nil
}

// Initialize initializes the strategy with parameters.
func (s *VWAPDeviationStrategy) Initialize(params map[string]interface{}) error {
	if v, ok := params["buy_threshold_pct"]; ok {
		switch x := v.(type) {
		case float64:
			s.buyThreshold = -x / 100
		case int:
			s.buyThreshold = -float64(x) / 100
		}
	}
	if v, ok := params["sell_threshold_pct"]; ok {
		switch x := v.(type) {
		case float64:
			s.sellThreshold = x / 100
		case int:
			s.sellThreshold = float64(x) / 100
		}
	}
	return nil
}

func (s *VWAPDeviationStrategy) addPV(price, volume float64, ts time.Time, side string) {
	s.pvs = append(s.pvs, indicators.PriceVolume{Price: price, Volume: volume, Timestamp: ts, Side: side})
	if len(s.pvs) > maxPVs {
		s.pvs = s.pvs[len(s.pvs)-maxPVs:]
	}
}

// OnTrade processes a trade event.
func (s *VWAPDeviationStrategy) OnTrade(trade *bitso.Trade) (*Signal, error) {
	price := trade.Price.Float64()
	amount := trade.Amount.Float64()
	book := trade.Book.String()
	ts := trade.CreatedAt.Time()
	side := trade.MakerSide.String()
	if side == "none" {
		side = "buy"
	}
	s.addPV(price, amount, ts, side)

	if len(s.pvs) < 10 {
		return NewSignal(SignalHold, book, price, 0), nil
	}
	if time.Since(s.lastSignalTime) < s.minInterval {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	vwap := indicators.VWAP(s.pvs)
	dev := indicators.VWAPDeviation(price, vwap)

	if dev <= s.buyThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("Price %.2f%% below VWAP (dev=%.4f)", dev*100, dev)).
			WithMetadata("vwap", vwap).WithMetadata("deviation", dev), nil
	}
	if dev >= s.sellThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("Price %.2f%% above VWAP (dev=%.4f)", dev*100, dev)).
			WithMetadata("vwap", vwap).WithMetadata("deviation", dev), nil
	}
	return NewSignal(SignalHold, book, price, 0).WithMetadata("vwap", vwap).WithMetadata("deviation", dev), nil
}

// OnTicker processes a ticker (use last as price, no volume — hold unless we have trade history).
func (s *VWAPDeviationStrategy) OnTicker(ticker *bitso.Ticker) (*Signal, error) {
	price := ticker.Last.Float64()
	book := ticker.Book.String()
	if len(s.pvs) < 10 {
		return NewSignal(SignalHold, book, price, 0), nil
	}
	vwap := indicators.VWAP(s.pvs)
	dev := indicators.VWAPDeviation(price, vwap)
	if dev <= s.buyThreshold && time.Since(s.lastSignalTime) >= s.minInterval {
		s.lastSignalTime = ticker.CreatedAt.Time()
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("Price below VWAP (dev=%.4f)", dev)).
			WithMetadata("vwap", vwap).WithMetadata("deviation", dev), nil
	}
	if dev >= s.sellThreshold && time.Since(s.lastSignalTime) >= s.minInterval {
		s.lastSignalTime = ticker.CreatedAt.Time()
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("Price above VWAP (dev=%.4f)", dev)).
			WithMetadata("vwap", vwap).WithMetadata("deviation", dev), nil
	}
	return NewSignal(SignalHold, book, price, 0), nil
}

// OnOrderBook does not generate signals.
func (s *VWAPDeviationStrategy) OnOrderBook(orderBook interface{}) (*Signal, error) {
	return NewSignal(SignalHold, "", 0, 0), nil
}

// GetName returns the strategy name.
func (s *VWAPDeviationStrategy) GetName() string { return s.name }

// Reset resets the strategy state.
func (s *VWAPDeviationStrategy) Reset() error {
	s.pvs = make([]indicators.PriceVolume, 0, maxPVs)
	s.lastSignalTime = time.Time{}
	return nil
}
