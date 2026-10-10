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

// EtoroExecutor places orders on eToro via the Public API: a BUY opens a
// long position by cash amount at x1 (v2 orders route); a SELL closes the
// long positions held on the instrument. It never opens a short: shorts
// need a stop-loss and are outside the long/flat policy of this engine.
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
	return e.openLong(signal)
}

// ExecuteSellSignal closes the long positions held on the instrument.
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
	var longs []etoro.Position
	for _, p := range portfolio.PositionsFor(instrumentID) {
		if p.IsBuy {
			longs = append(longs, p)
		}
	}
	if len(longs) == 0 {
		return "", fmt.Errorf("no long eToro position on %s to close; shorts are not opened by this engine", symbol)
	}

	if e.dryRun {
		e.logger.Printf("[DRY-RUN] Would close %d position(s) for %s", len(longs), symbol)
		return "dry-run", nil
	}

	var ids []string
	for i, pos := range longs {
		rid := ""
		if signal.ClientRef != "" {
			rid = etoro.RequestIDFor(fmt.Sprintf("signal-%s-close-%d", signal.ClientRef, i))
		}
		acc, err := e.client.ClosePosition(ctx, pos.PositionID, instrumentID, nil, rid)
		if err != nil {
			return strings.Join(ids, ","), fmt.Errorf("close position %d: %w", pos.PositionID, err)
		}
		oid := strconv.FormatInt(acc.OrderForClose.OrderID, 10)
		ids = append(ids, oid)
		e.logger.Printf("Closed eToro position %d (order %s) for %s", pos.PositionID, oid, symbol)
	}
	return strings.Join(ids, ","), nil
}

func (e *EtoroExecutor) openLong(signal TradingSignal) (string, error) {
	symbol := signal.Symbol
	if symbol == "" && signal.Book != nil {
		symbol = signal.Book.String()
	}
	symbol = normalizeEtoroSymbol(symbol)
	if signal.Amount <= 0 {
		return "", fmt.Errorf("amount must be positive for eToro market order")
	}

	if e.dryRun {
		e.logger.Printf("[DRY-RUN] Would place BUY market order for %s amount %.2f (no eToro API call)", symbol, signal.Amount)
		return "dry-run", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	instrumentID, err := e.resolveInstrumentID(ctx, symbol, signal.InstrumentID)
	if err != nil {
		return "", err
	}

	// Idempotency: the signal's event id fixes the x-request-id. eToro
	// rejects a reused request id (HTTP 400 "ReferenceID ... may already
	// exists"), so a redelivered signal cannot open a second position.
	rid := etoro.NewRequestID()
	if signal.ClientRef != "" {
		rid = etoro.RequestIDFor("signal-" + signal.ClientRef + "-open")
	}

	acc, err := e.client.OpenOrder(ctx, etoro.MarketBuyByAmount(instrumentID, signal.Amount, 1), rid)
	if etoro.IsDuplicateReference(err) {
		// The first delivery placed it and published its order-placed event;
		// an empty id keeps the engine from publishing a second one.
		e.logger.Printf("Signal %s was already placed on eToro (duplicate reference %s); not re-sending", signal.ClientRef, rid)
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("open market order: %w", err)
	}
	e.logger.Printf("Placed eToro BUY order %d for %s amount %.2f (ref %s)", acc.OrderID, symbol, signal.Amount, acc.ReferenceID)
	return strconv.FormatInt(acc.OrderID, 10), nil
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

	result, err := e.client.ResolveSymbol(ctx, symbol)
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
