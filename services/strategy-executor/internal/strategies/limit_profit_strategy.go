// Package strategies — limit_profit: buy near reference + offset, exit when min profit is achievable.
package strategies

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/strategy-executor/internal/indicators"

	"github.com/google/uuid"
)

// LimitProfitConfig holds parameters for the limit-profit / scalp-style strategy.
// Exit threshold uses Bitso GET /fees with configurable maker/taker per leg when credentials exist;
// otherwise entry + min_profit + manual fee_addon.
type LimitProfitConfig struct {
	Reference            string // "last_trade" or "vwap"
	EntryOffset          float64
	MinProfit            float64
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
	// MaxPositionHoldSeconds: if >0, emit SELL after position age exceeds this (time stop).
	MaxPositionHoldSeconds int
	// StopLossQuote: if >0, emit SELL when compare price <= entry - StopLossQuote (quote currency per base unit).
	StopLossQuote float64
}

// DefaultLimitProfitConfig returns conservative defaults (tune per book / liquidity).
func DefaultLimitProfitConfig() LimitProfitConfig {
	return LimitProfitConfig{
		Reference:          "last_trade",
		EntryOffset:        500,
		MinProfit:          5000,
		PositionSize:       0.001,
		MinSignalInterval:  60,
		BuyLiquidity:       "maker",
		SellLiquidity:      "taker",
		ExitPriceReference: "last",
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

	mu sync.RWMutex

	rawStateStore LimitProfitRawStateStore
}

type limitProfitPersisted struct {
	State                StrategyState `json:"state"`
	PositionBuyFeeRate   float64       `json:"position_buy_fee_rate,omitempty"`
	PositionBuyLiquidity string        `json:"position_buy_liquidity,omitempty"`
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
		if v, ok := p["max_position_hold_seconds"].(float64); ok {
			s.lpConfig.MaxPositionHoldSeconds = int(v)
		}
		if v, ok := p["stop_loss_quote"].(float64); ok {
			s.lpConfig.StopLossQuote = v
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
	if s.lpConfig.BuyLiquidity == "" {
		s.lpConfig.BuyLiquidity = "maker"
	}
	if s.lpConfig.SellLiquidity == "" {
		s.lpConfig.SellLiquidity = "taker"
	}
	if s.lpConfig.ExitPriceReference == "" {
		s.lpConfig.ExitPriceReference = "last"
	}
	return nil
}

// SetLimitProfitRawStateStore enables Redis-backed durable state (optional).
func (s *LimitProfitStrategy) SetLimitProfitRawStateStore(store LimitProfitRawStateStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rawStateStore = store
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
}

func (s *LimitProfitStrategy) persistLocked(ctx context.Context) {
	if s.rawStateStore == nil {
		return
	}
	p := limitProfitPersisted{
		State:                s.GetState(),
		PositionBuyFeeRate:   s.positionBuyFeeRate,
		PositionBuyLiquidity: s.positionBuyLiquidity,
	}
	b, err := json.Marshal(&p)
	if err != nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_ = s.rawStateStore.Save(cctx, s.Name(), b)
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
					s.UpdateState(func(st *StrategyState) {
						st.PendingBuy = false
						st.PendingEventID = ""
						st.PendingBuySince = time.Time{}
					})
					s.persistLocked(ctx)
					return nil, nil
				}
			}
			return nil, nil
		}
		if !s.canEmitSignal() {
			return nil, nil
		}
		ref := s.referencePrice(ctx, tickPrice)
		buyPrice := ref + s.lpConfig.EntryOffset

		eventID := uuid.New().String()
		now := time.Now()
		s.RecordSignal()
		s.UpdateState(func(st *StrategyState) {
			st.PendingBuy = true
			st.PendingEventID = eventID
			st.PendingBuySince = now
		})
		s.persistLocked(ctx)

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

	entry := state.EntryPrice
	comparePrice, refLabel := s.referenceExitPrice(ctx, tickPrice, book)
	threshold, buyR, sellR, feeModel := s.exitPriceThreshold(ctx, entry)
	manualAddon := s.exitFeeAddon(entry)

	if s.lpConfig.StopLossQuote > 0 && comparePrice <= entry-s.lpConfig.StopLossQuote {
		return s.emitPositionExit(ctx, tickPrice, comparePrice, refLabel, entry, threshold, buyR, sellR, feeModel, manualAddon, "stop_loss"), nil
	}
	if s.lpConfig.MaxPositionHoldSeconds > 0 && !state.EntryTime.IsZero() {
		if time.Since(state.EntryTime) >= time.Duration(s.lpConfig.MaxPositionHoldSeconds)*time.Second {
			return s.emitPositionExit(ctx, tickPrice, comparePrice, refLabel, entry, threshold, buyR, sellR, feeModel, manualAddon, "max_hold"), nil
		}
	}
	if comparePrice >= threshold {
		return s.emitPositionExit(ctx, tickPrice, comparePrice, refLabel, entry, threshold, buyR, sellR, feeModel, manualAddon, "take_profit"), nil
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
	exitForPnL := tickPrice
	switch refLabel {
	case "bid", "mid", "min_last_bid":
		exitForPnL = comparePrice
	}
	gross := (exitForPnL - entry) * posSize
	netQuote := bitso.NetQuotePnLPerBase(entry, exitForPnL, buyR, sellR) * posSize

	profitable := gross > 0
	if feeModel == "bitso_api" {
		profitable = netQuote > 0
	}

	s.RecordSignal()
	s.RecordTrade(profitable)
	s.ClearPosition()
	s.resetPositionFeeOverrides()
	s.persistLocked(ctx)

	book := s.config.Book
	meta := map[string]interface{}{
		"signal_type":             "exit_sell",
		"exit_reason":             exitReason,
		"entry_price":             entry,
		"gross_quote_pnl":         gross,
		"exit_threshold":          threshold,
		"fee_model":               feeModel,
		"min_profit":              s.lpConfig.MinProfit,
		"extra_fee_margin":        s.lpConfig.Fee,
		"exit_price_reference":    refLabel,
		"compare_price":           comparePrice,
		"tick_price":              tickPrice,
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

	reason := fmt.Sprintf(
		"limit_profit exit [%s] ref=%s reason=%s: compare=%.2f threshold=%.2f (entry=%.2f min_profit=%.2f)",
		feeModel, refLabel, exitReason, comparePrice, threshold, entry, s.lpConfig.MinProfit,
	)
	switch exitReason {
	case "stop_loss":
		reason = fmt.Sprintf(
			"limit_profit stop_loss [%s] ref=%s: compare=%.2f <= entry-stop=%.2f (entry=%.2f stop_loss_quote=%.2f)",
			feeModel, refLabel, comparePrice, entry-s.lpConfig.StopLossQuote, entry, s.lpConfig.StopLossQuote,
		)
	case "max_hold":
		reason = fmt.Sprintf(
			"limit_profit max_hold [%s] ref=%s: position age >= %ds",
			feeModel, refLabel, s.lpConfig.MaxPositionHoldSeconds,
		)
	}

	return &Signal{
		Strategy:   s.Name(),
		Book:       book,
		Side:       "SELL",
		Amount:     posSize,
		Price:      tickPrice,
		Confidence: 0.85,
		Reason:     reason,
		Timestamp:  time.Now(),
		Metadata:   meta,
	}
}

// OnOrderFilled opens the position when the BUY limit fills at the exchange.
func (s *LimitProfitStrategy) OnOrderFilled(fill OrderFill) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		return
	}
	st := s.GetState()
	if !st.PendingBuy || st.PendingEventID == "" || fill.EventID != st.PendingEventID {
		return
	}
	if fill.Book != s.config.Book {
		return
	}
	switch strings.ToLower(strings.TrimSpace(fill.Side)) {
	case "buy", "purchase":
	default:
		return
	}
	if fill.AveragePrice <= 0 {
		return
	}
	size := fill.FilledAmount
	if size <= 0 {
		size = s.lpConfig.PositionSize
	}

	if fill.BuyFeeRate != nil && *fill.BuyFeeRate > 0 {
		s.positionBuyFeeRate = *fill.BuyFeeRate
	}
	if fill.Liquidity != "" {
		s.positionBuyLiquidity = normalizeLiquidity(fill.Liquidity)
	}

	s.SetPosition("LONG", size, fill.AveragePrice)
	s.UpdateState(func(out *StrategyState) {
		out.PendingBuy = false
		out.PendingEventID = ""
		out.PendingBuySince = time.Time{}
	})
	s.persistLocked(context.Background())
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
	if feeModel == "bitso_api" {
		be := bitso.MinExitPriceAfterRoundTrip(entry, buyR, sellR)
		return be + s.lpConfig.MinProfit + s.lpConfig.Fee, buyR, sellR, feeModel
	}
	manual := s.exitFeeAddon(entry)
	return entry + s.lpConfig.MinProfit + manual, buyR, sellR, "manual_estimate"
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
	if s.rawStateStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.rawStateStore.Delete(ctx, s.Name())
	}
}
