package metrics

import (
	"sync"

	"bitso-trading-platform/shared/pkg/indicators"
	"bitso-trading-platform/shared/pkg/models"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	maxPrices     = 500
	maxPriceVolumes = 2000
	rsiPeriod     = 14
	emaShort      = 12
	emaLong       = 26
	macdSignal    = 9
	bollingerPeriod = 20
	bollingerStd   = 2.0
	volatilityPeriod = 20
	volumeSpikeThreshold = 2.0
)

// IndicatorGauges holds Prometheus gauges for financial indicators (per book).
type IndicatorGauges struct {
	// Tier 1
	RSI           *prometheus.GaugeVec
	EMAShort      *prometheus.GaugeVec
	SMAShort      *prometheus.GaugeVec
	VWAP          *prometheus.GaugeVec
	Volume        *prometheus.GaugeVec
	VolumeSpike   *prometheus.GaugeVec // 1 = spike
	Momentum1m    *prometheus.GaugeVec
	Momentum5m    *prometheus.GaugeVec
	Momentum15m   *prometheus.GaugeVec
	Volatility    *prometheus.GaugeVec
	SpreadBps     *prometheus.GaugeVec
	SpreadTight   *prometheus.GaugeVec // 1 = tight
	SpreadWide    *prometheus.GaugeVec // 1 = wide
	// Tier 2
	OrderBookImbalance *prometheus.GaugeVec
	VWAPDeviation      *prometheus.GaugeVec
	// Tier 3
	BollingerUpper *prometheus.GaugeVec
	BollingerMiddle *prometheus.GaugeVec
	BollingerLower *prometheus.GaugeVec
	BollingerWidth *prometheus.GaugeVec
	RSIOversold   *prometheus.GaugeVec // 1 = oversold
	RSIOverbought *prometheus.GaugeVec // 1 = overbought
	// Tier 4
	OrderFlowImbalance *prometheus.GaugeVec
	TradeIntensityPerSec  *prometheus.GaugeVec
	TradeIntensityPerMin  *prometheus.GaugeVec
}

// NewIndicatorGauges creates and registers all indicator gauges.
func NewIndicatorGauges() *IndicatorGauges {
	return &IndicatorGauges{
		RSI: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_rsi",
			Help: "RSI (0-100)",
		}, []string{"book"}),
		EMAShort: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_ema_short",
			Help: "Short-term EMA (12)",
		}, []string{"book"}),
		SMAShort: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_sma_short",
			Help: "Short-term SMA (20)",
		}, []string{"book"}),
		VWAP: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_vwap",
			Help: "Volume Weighted Average Price",
		}, []string{"book"}),
		Volume: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_volume",
			Help: "Cumulative volume (rolling window)",
		}, []string{"book"}),
		VolumeSpike: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_volume_spike",
			Help: "1 if volume spike detected (current > 2x avg)",
		}, []string{"book"}),
		Momentum1m: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_momentum_1m",
			Help: "Short-term momentum (1 period)",
		}, []string{"book"}),
		Momentum5m: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_momentum_5m",
			Help: "Momentum 5 periods",
		}, []string{"book"}),
		Momentum15m: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_momentum_15m",
			Help: "Momentum 15 periods",
		}, []string{"book"}),
		Volatility: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_volatility",
			Help: "Standard deviation of returns",
		}, []string{"book"}),
		SpreadBps: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_spread_bps",
			Help: "Bid-ask spread in basis points",
		}, []string{"book"}),
		SpreadTight: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_spread_tight",
			Help: "1 if spread is tight (<=5 bps)",
		}, []string{"book"}),
		SpreadWide: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_spread_wide",
			Help: "1 if spread is wide (>=50 bps)",
		}, []string{"book"}),
		OrderBookImbalance: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_orderbook_imbalance",
			Help: "Order book imbalance (-1 to 1)",
		}, []string{"book"}),
		VWAPDeviation: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_vwap_deviation",
			Help: "(price - VWAP) / VWAP",
		}, []string{"book"}),
		BollingerUpper: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_bollinger_upper",
			Help: "Bollinger upper band",
		}, []string{"book"}),
		BollingerMiddle: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_bollinger_middle",
			Help: "Bollinger middle (SMA)",
		}, []string{"book"}),
		BollingerLower: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_bollinger_lower",
			Help: "Bollinger lower band",
		}, []string{"book"}),
		BollingerWidth: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_bollinger_width",
			Help: "Bollinger width (upper-lower)/middle",
		}, []string{"book"}),
		RSIOversold: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_rsi_oversold",
			Help: "1 if RSI oversold",
		}, []string{"book"}),
		RSIOverbought: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_rsi_overbought",
			Help: "1 if RSI overbought",
		}, []string{"book"}),
		OrderFlowImbalance: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_order_flow_imbalance",
			Help: "Order flow imbalance (-1 to 1)",
		}, []string{"book"}),
		TradeIntensityPerSec: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_trade_intensity_per_sec",
			Help: "Trades per second (rolling window)",
		}, []string{"book"}),
		TradeIntensityPerMin: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "market_data_indicator_trade_intensity_per_min",
			Help: "Trades per minute (rolling window)",
		}, []string{"book"}),
	}
}

// bookState holds rolling state for one book.
type bookState struct {
	prices    []float64
	pvs       []indicators.PriceVolume
	lastBid   float64
	lastAsk   float64
	lastMid   float64
}

// IndicatorRecorder updates per-book state from trades and records indicator gauges.
type IndicatorRecorder struct {
	gauges *IndicatorGauges
	state  map[string]*bookState
	mu     sync.Mutex
}

// NewIndicatorRecorder creates a recorder that writes to the given gauges.
func NewIndicatorRecorder(gauges *IndicatorGauges) *IndicatorRecorder {
	return &IndicatorRecorder{
		gauges: gauges,
		state:  make(map[string]*bookState),
	}
}

// RecordTrade updates state and records all indicators for the trade's book.
func (r *IndicatorRecorder) RecordTrade(trade *models.TradeEvent) {
	if trade == nil {
		return
	}
	book := trade.Book
	if book == "" {
		book = "unknown"
	}
	r.mu.Lock()
	st, ok := r.state[book]
	if !ok {
		st = &bookState{
			prices: make([]float64, 0, maxPrices),
			pvs:    make([]indicators.PriceVolume, 0, maxPriceVolumes),
		}
		r.state[book] = st
	}
	pv := indicators.PriceVolume{
		Price:     trade.Price,
		Volume:    trade.Amount,
		Timestamp: trade.Timestamp,
		Side:      trade.Side,
	}
	st.pvs = append(st.pvs, pv)
	if len(st.pvs) > maxPriceVolumes {
		st.pvs = st.pvs[len(st.pvs)-maxPriceVolumes:]
	}
	st.prices = append(st.prices, trade.Price)
	if len(st.prices) > maxPrices {
		st.prices = st.prices[len(st.prices)-maxPrices:]
	}
	r.mu.Unlock()

	r.updateGauges(book, st)
}

// RecordBidAsk updates last bid/ask for spread (call when ticker or order book is available).
func (r *IndicatorRecorder) RecordBidAsk(book string, bid, ask float64) {
	if book == "" {
		book = "unknown"
	}
	r.mu.Lock()
	st, ok := r.state[book]
	if !ok {
		st = &bookState{
			prices: make([]float64, 0, maxPrices),
			pvs:    make([]indicators.PriceVolume, 0, maxPriceVolumes),
		}
		r.state[book] = st
	}
	st.lastBid = bid
	st.lastAsk = ask
	st.lastMid = (bid + ask) / 2
	r.mu.Unlock()

	spreadBps := indicators.SpreadBps(bid, ask)
	r.gauges.SpreadBps.WithLabelValues(book).Set(spreadBps)
	tight := 0.0
	if indicators.SpreadStateFromBps(spreadBps, indicators.DefaultSpreadTightBps, indicators.DefaultSpreadWideBps) == indicators.SpreadTight {
		tight = 1
	}
	r.gauges.SpreadTight.WithLabelValues(book).Set(tight)
	wide := 0.0
	if indicators.SpreadStateFromBps(spreadBps, indicators.DefaultSpreadTightBps, indicators.DefaultSpreadWideBps) == indicators.SpreadWide {
		wide = 1
	}
	r.gauges.SpreadWide.WithLabelValues(book).Set(wide)
}

// RecordOrderBookImbalance sets order book imbalance (call when order book snapshot is available).
func (r *IndicatorRecorder) RecordOrderBookImbalance(book string, imbalance float64) {
	if book == "" {
		book = "unknown"
	}
	r.gauges.OrderBookImbalance.WithLabelValues(book).Set(imbalance)
}

func (r *IndicatorRecorder) updateGauges(book string, st *bookState) {
	if len(st.prices) == 0 {
		return
	}
	prices := st.prices
	// RSI
	rsi := indicators.RSI(prices, rsiPeriod)
	r.gauges.RSI.WithLabelValues(book).Set(rsi)
	oversold := 0.0
	if indicators.RSIStateFromLevels(rsi, indicators.DefaultRSIOversold, indicators.DefaultRSIOverbought) == indicators.RSIOversold {
		oversold = 1
	}
	r.gauges.RSIOversold.WithLabelValues(book).Set(oversold)
	overbought := 0.0
	if indicators.RSIStateFromLevels(rsi, indicators.DefaultRSIOversold, indicators.DefaultRSIOverbought) == indicators.RSIOverbought {
		overbought = 1
	}
	r.gauges.RSIOverbought.WithLabelValues(book).Set(overbought)
	// EMA / SMA
	r.gauges.EMAShort.WithLabelValues(book).Set(indicators.EMA(prices, emaShort))
	r.gauges.SMAShort.WithLabelValues(book).Set(indicators.SMA(prices, bollingerPeriod))
	// VWAP
	vwap := indicators.VWAP(st.pvs)
	r.gauges.VWAP.WithLabelValues(book).Set(vwap)
	// Volume
	vol := indicators.Volume(st.pvs)
	r.gauges.Volume.WithLabelValues(book).Set(vol)
	// Volume spike: compare recent half to previous half
	n := len(st.pvs)
	if n >= 4 {
		mid := n / 2
		recentVol := indicators.Volume(st.pvs[mid:])
		prevVol := indicators.Volume(st.pvs[:mid])
		spike := 0.0
		if indicators.VolumeSpike(recentVol, prevVol, volumeSpikeThreshold) {
			spike = 1
		}
		r.gauges.VolumeSpike.WithLabelValues(book).Set(spike)
	}
	// Momentum
	m1, m5, m15 := indicators.MomentumMulti(prices)
	r.gauges.Momentum1m.WithLabelValues(book).Set(m1)
	r.gauges.Momentum5m.WithLabelValues(book).Set(m5)
	r.gauges.Momentum15m.WithLabelValues(book).Set(m15)
	// Volatility
	r.gauges.Volatility.WithLabelValues(book).Set(indicators.Volatility(prices, volatilityPeriod))
	// VWAP deviation
	if vwap > 0 {
		lastPrice := prices[len(prices)-1]
		r.gauges.VWAPDeviation.WithLabelValues(book).Set(indicators.VWAPDeviation(lastPrice, vwap))
	}
	// Bollinger
	bb := indicators.BollingerBands(prices, bollingerPeriod, bollingerStd)
	r.gauges.BollingerUpper.WithLabelValues(book).Set(bb.Upper)
	r.gauges.BollingerMiddle.WithLabelValues(book).Set(bb.Middle)
	r.gauges.BollingerLower.WithLabelValues(book).Set(bb.Lower)
	r.gauges.BollingerWidth.WithLabelValues(book).Set(bb.Width)
	// Order flow imbalance
	ofi := indicators.OrderFlowImbalance(st.pvs)
	r.gauges.OrderFlowImbalance.WithLabelValues(book).Set(ofi)
	// Trade intensity
	cnt, window := indicators.TradeCountAndWindow(st.pvs)
	if window > 0 {
		r.gauges.TradeIntensityPerSec.WithLabelValues(book).Set(indicators.TradeIntensityPerSecond(cnt, window))
		r.gauges.TradeIntensityPerMin.WithLabelValues(book).Set(indicators.TradeIntensityPerMinute(cnt, window))
	}
}
