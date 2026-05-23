package execution

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/models"
)

// EtoroExecutor places orders on eToro via the Public API (market by amount / close position).
type EtoroExecutor struct {
	client *etoro.Client
	config *models.TradingConfig
	logger *log.Logger
	dryRun bool

	instrumentMu sync.RWMutex
	instrumentID map[string]int64 // symbol -> instrumentId cache
}

// NewEtoroExecutor creates an executor for BROKER=etoro.
func NewEtoroExecutor(client *etoro.Client, config *models.TradingConfig, dryRun bool) *EtoroExecutor {
	logger := log.New(log.Writer(), "[ETORO-EXECUTOR] ", log.LstdFlags|log.Lshortfile)
	return &EtoroExecutor{
		client:       client,
		config:       config,
		logger:       logger,
		dryRun:       dryRun,
		instrumentID: make(map[string]int64),
	}
}

// CheckSessionLimits implements Executor (same limits as Bitso path).
func (e *EtoroExecutor) CheckSessionLimits(dailyRealizedPnL, drawdownPct float64) error {
	if e.config == nil {
		return nil
	}
	if e.config.MaxDailyLoss > 0 && dailyRealizedPnL <= -e.config.MaxDailyLoss {
		return fmt.Errorf("daily loss limit exceeded: realized P&L %.2f <= -%.2f", dailyRealizedPnL, e.config.MaxDailyLoss)
	}
	if e.config.MaxDrawdownPct > 0 && drawdownPct >= e.config.MaxDrawdownPct {
		return fmt.Errorf("max drawdown exceeded: %.2f%% >= %.2f%%", drawdownPct, e.config.MaxDrawdownPct)
	}
	return nil
}

// ExecuteBuySignal opens a long position by cash amount (signal.Amount).
func (e *EtoroExecutor) ExecuteBuySignal(signal TradingSignal) (string, error) {
	return e.openPosition(signal, true)
}

// ExecuteSellSignal closes an existing long or opens a short depending on portfolio state.
func (e *EtoroExecutor) ExecuteSellSignal(signal TradingSignal) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	symbol := signal.Symbol
	if symbol == "" && signal.Book != nil {
		symbol = signal.Book.String()
	}
	instrumentID, err := e.resolveInstrumentID(ctx, symbol, signal.InstrumentID)
	if err != nil {
		return "", err
	}

	portfolio, err := e.client.GetPortfolio(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch portfolio: %w", err)
	}
	pos := portfolio.FindPositionByInstrument(instrumentID, true)
	if pos == nil {
		return e.openPosition(signal, false)
	}

	if e.dryRun {
		e.logger.Printf("[DRY-RUN] Would close position %d for %s", pos.PositionID, symbol)
		return "dry-run", nil
	}

	orderID, err := e.client.ClosePosition(ctx, pos.PositionID, nil)
	if err != nil {
		return "", fmt.Errorf("close position: %w", err)
	}
	e.logger.Printf("Closed eToro position %d (order %s) for %s", pos.PositionID, etoro.FormatOrderID(orderID), symbol)
	return etoro.FormatOrderID(orderID), nil
}

func (e *EtoroExecutor) openPosition(signal TradingSignal, isBuy bool) (string, error) {
	symbol := signal.Symbol
	if symbol == "" && signal.Book != nil {
		symbol = signal.Book.String()
	}
	symbol = normalizeEtoroSymbol(symbol)
	if signal.Amount <= 0 {
		return "", fmt.Errorf("amount must be positive for eToro market order")
	}

	if e.dryRun {
		side := "SELL"
		if isBuy {
			side = "BUY"
		}
		e.logger.Printf("[DRY-RUN] Would place %s market order for %s amount %.2f (no eToro API call)", side, symbol, signal.Amount)
		return "dry-run", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	instrumentID, err := e.resolveInstrumentID(ctx, symbol, signal.InstrumentID)
	if err != nil {
		return "", err
	}

	refRate := signal.Price
	if refRate <= 0 {
		rates, rErr := e.client.GetRates(ctx, instrumentID)
		if rErr == nil && len(rates) > 0 {
			refRate = rates[0].Ask
			if !isBuy {
				refRate = rates[0].Bid
			}
		}
	}
	sl, tp := etoro.DefaultSLTP(refRate, isBuy)

	orderID, err := e.client.OpenMarketOrderByAmount(ctx, etoro.OpenByAmountRequest{
		InstrumentID:   instrumentID,
		Amount:         signal.Amount,
		Leverage:       1,
		IsBuy:          isBuy,
		StopLossRate:   sl,
		TakeProfitRate: tp,
	})
	if err != nil {
		return "", fmt.Errorf("open market order: %w", err)
	}
	side := "BUY"
	if !isBuy {
		side = "SELL"
	}
	e.logger.Printf("Placed eToro %s order %s for %s amount %.2f", side, etoro.FormatOrderID(orderID), symbol, signal.Amount)
	return etoro.FormatOrderID(orderID), nil
}

func (e *EtoroExecutor) resolveInstrumentID(ctx context.Context, symbol string, explicitID int64) (int64, error) {
	if explicitID > 0 {
		return explicitID, nil
	}
	symbol = normalizeEtoroSymbol(symbol)
	if symbol == "" {
		return 0, fmt.Errorf("instrument symbol is required for eToro (set signal book to ticker e.g. AAPL)")
	}
	e.instrumentMu.RLock()
	if id, ok := e.instrumentID[symbol]; ok {
		e.instrumentMu.RUnlock()
		return id, nil
	}
	e.instrumentMu.RUnlock()

	result, err := e.client.SearchInstrument(ctx, symbol)
	if err != nil {
		return 0, fmt.Errorf("resolve instrument %s: %w", symbol, err)
	}
	e.instrumentMu.Lock()
	e.instrumentID[symbol] = result.InstrumentID
	e.instrumentMu.Unlock()
	return result.InstrumentID, nil
}

// normalizeEtoroSymbol maps book-style names (btc_mxn) to a search symbol when possible.
func normalizeEtoroSymbol(symbol string) string {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return ""
	}
	if strings.Contains(symbol, "_") {
		parts := strings.Split(strings.ToLower(symbol), "_")
		if len(parts) >= 1 && parts[0] != "" {
			return strings.ToUpper(parts[0])
		}
	}
	return strings.ToUpper(symbol)
}

// InstrumentIDFromMetadata reads instrument_id from a trade signal metadata map.
func InstrumentIDFromMetadata(metadata map[string]interface{}) int64 {
	if metadata == nil {
		return 0
	}
	raw, ok := metadata["instrument_id"]
	if !ok {
		raw, ok = metadata["instrumentId"]
	}
	if !ok {
		raw, ok = metadata["instrumentID"]
	}
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		id, _ := strconv.ParseInt(v, 10, 64)
		return id
	default:
		return 0
	}
}
