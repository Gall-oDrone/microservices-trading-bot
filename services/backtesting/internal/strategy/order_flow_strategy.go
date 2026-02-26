package strategy

import (
	"fmt"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/indicators"
)

// OrderFlowStrategy trades on order flow imbalance (buy vs sell volume from trades).
// Buy when OFI > threshold, sell when OFI < -threshold.
const maxPVsOrderFlow = 2000

// OrderFlowStrategy for backtesting.
type OrderFlowStrategy struct {
	name string

	buyThreshold  float64 // e.g. 0.3 = buy when OFI > 0.3
	sellThreshold float64 // e.g. -0.3 = sell when OFI < -0.3
	minInterval   time.Duration

	pvs           []indicators.PriceVolume
	lastSignalTime time.Time
}

// NewOrderFlowStrategy creates a new order flow imbalance strategy.
func NewOrderFlowStrategy(params map[string]interface{}) (Strategy, error) {
	s := &OrderFlowStrategy{
		name:           "order_flow",
		buyThreshold:   0.25,
		sellThreshold:  -0.25,
		minInterval:    20 * time.Second,
		pvs:            make([]indicators.PriceVolume, 0, maxPVsOrderFlow),
	}
	if err := s.Initialize(params); err != nil {
		return nil, err
	}
	return s, nil
}

// Initialize initializes the strategy with parameters.
func (s *OrderFlowStrategy) Initialize(params map[string]interface{}) error {
	if v, ok := params["buy_threshold"]; ok {
		switch x := v.(type) {
		case float64:
			s.buyThreshold = x
		case int:
			s.buyThreshold = float64(x)
		}
	}
	if v, ok := params["sell_threshold"]; ok {
		switch x := v.(type) {
		case float64:
			s.sellThreshold = x
		case int:
			s.sellThreshold = float64(x)
		}
	}
	return nil
}

func (s *OrderFlowStrategy) addPV(price, volume float64, ts time.Time, side string) {
	if side == "none" {
		side = "buy"
	}
	s.pvs = append(s.pvs, indicators.PriceVolume{Price: price, Volume: volume, Timestamp: ts, Side: side})
	if len(s.pvs) > maxPVsOrderFlow {
		s.pvs = s.pvs[len(s.pvs)-maxPVsOrderFlow:]
	}
}

// OnTrade processes a trade event.
func (s *OrderFlowStrategy) OnTrade(trade *bitso.Trade) (*Signal, error) {
	price := trade.Price.Float64()
	amount := trade.Amount.Float64()
	book := trade.Book.String()
	ts := trade.CreatedAt.Time()
	side := trade.MakerSide.String()
	s.addPV(price, amount, ts, side)

	if len(s.pvs) < 20 {
		return NewSignal(SignalHold, book, price, 0), nil
	}
	if time.Since(s.lastSignalTime) < s.minInterval {
		return NewSignal(SignalHold, book, price, 0), nil
	}

	ofi := indicators.OrderFlowImbalance(s.pvs)
	if ofi >= s.buyThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalBuy, book, price, 0.01).
			WithReason(fmt.Sprintf("Order flow imbalance %.2f (buy pressure)", ofi)).
			WithMetadata("order_flow_imbalance", ofi), nil
	}
	if ofi <= s.sellThreshold {
		s.lastSignalTime = ts
		return NewSignal(SignalSell, book, price, 0.01).
			WithReason(fmt.Sprintf("Order flow imbalance %.2f (sell pressure)", ofi)).
			WithMetadata("order_flow_imbalance", ofi), nil
	}
	return NewSignal(SignalHold, book, price, 0).WithMetadata("order_flow_imbalance", ofi), nil
}

// OnTicker: no volume in ticker, use price-only hold or skip.
func (s *OrderFlowStrategy) OnTicker(ticker *bitso.Ticker) (*Signal, error) {
	return NewSignal(SignalHold, ticker.Book.String(), ticker.Last.Float64(), 0), nil
}

// OnOrderBook does not generate signals.
func (s *OrderFlowStrategy) OnOrderBook(orderBook interface{}) (*Signal, error) {
	return NewSignal(SignalHold, "", 0, 0), nil
}

// GetName returns the strategy name.
func (s *OrderFlowStrategy) GetName() string { return s.name }

// Reset resets the strategy state.
func (s *OrderFlowStrategy) Reset() error {
	s.pvs = make([]indicators.PriceVolume, 0, maxPVsOrderFlow)
	s.lastSignalTime = time.Time{}
	return nil
}
