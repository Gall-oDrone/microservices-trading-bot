package table

import (
	"fmt"
	"time"

	"bitso_trading_bot/pkg/bitso"
)

// TradePnL represents the profit and loss for a single trade
type TradePnL struct {
	Book       *bitso.Book
	Side       bitso.OrderSide
	EntryPrice float64
	ExitPrice  float64
	Amount     float64
	PnL        float64
	PnLPercent float64
	Fees       float64
	NetPnL     float64
	Timestamp  time.Time
}

// AccumulatedPnL represents the accumulated profit and loss statistics
type AccumulatedPnL struct {
	Book           *bitso.Book
	TotalTrades    int
	WinningTrades  int
	LosingTrades   int
	TotalPnL       float64
	TotalFees      float64
	NetPnL         float64
	WinRate        float64
	LastUpdateTime time.Time
}

// PnLManager handles profit and loss tracking
type PnLManager struct {
	trades      []TradePnL
	accumulated map[string]*AccumulatedPnL // key: book.String()
}

// NewPnLManager creates a new PnL manager instance
func NewPnLManager() *PnLManager {
	return &PnLManager{
		trades:      make([]TradePnL, 0),
		accumulated: make(map[string]*AccumulatedPnL),
	}
}

// AddTrade adds a new trade and updates accumulated statistics
func (pm *PnLManager) AddTrade(trade TradePnL) {
	// Calculate PnL
	trade.PnL = pm.calculatePnL(trade)
	trade.PnLPercent = pm.calculatePnLPercent(trade)
	trade.NetPnL = trade.PnL - trade.Fees
	trade.Timestamp = time.Now()

	// Add to trades list
	pm.trades = append(pm.trades, trade)

	// Update accumulated statistics
	pm.updateAccumulatedPnL(trade)
}

// GetTradePnLString returns a formatted string for a single trade PnL
func (pm *PnLManager) GetTradePnLString(trade TradePnL) string {
	return fmt.Sprintf("%s\t%s\t%.8f\t%.8f\t%.8f\t%.8f\t%.2f%%\t%.8f\t%.8f\n",
		trade.Book.String(),
		trade.Side.String(),
		trade.EntryPrice,
		trade.ExitPrice,
		trade.Amount,
		trade.PnL,
		trade.PnLPercent,
		trade.Fees,
		trade.NetPnL,
	)
}

// GetAccumulatedPnLString returns a formatted string for accumulated PnL
func (pm *PnLManager) GetAccumulatedPnLString(book *bitso.Book) string {
	acc := pm.accumulated[book.String()]
	if acc == nil {
		return ""
	}

	return fmt.Sprintf("%s\t%d\t%d\t%d\t%.8f\t%.8f\t%.8f\t%.2f%%\n",
		book.String(),
		acc.TotalTrades,
		acc.WinningTrades,
		acc.LosingTrades,
		acc.TotalPnL,
		acc.TotalFees,
		acc.NetPnL,
		acc.WinRate,
	)
}

// calculatePnL calculates the profit and loss for a trade
func (pm *PnLManager) calculatePnL(trade TradePnL) float64 {
	if trade.Side == bitso.OrderSideBuy {
		return (trade.ExitPrice - trade.EntryPrice) * trade.Amount
	}
	return (trade.EntryPrice - trade.ExitPrice) * trade.Amount
}

// calculatePnLPercent calculates the profit and loss percentage
func (pm *PnLManager) calculatePnLPercent(trade TradePnL) float64 {
	if trade.Side == bitso.OrderSideBuy {
		return ((trade.ExitPrice - trade.EntryPrice) / trade.EntryPrice) * 100
	}
	return ((trade.EntryPrice - trade.ExitPrice) / trade.EntryPrice) * 100
}

// updateAccumulatedPnL updates the accumulated PnL statistics
func (pm *PnLManager) updateAccumulatedPnL(trade TradePnL) {
	bookKey := trade.Book.String()
	acc, exists := pm.accumulated[bookKey]
	if !exists {
		acc = &AccumulatedPnL{
			Book:           trade.Book,
			LastUpdateTime: time.Now(),
		}
		pm.accumulated[bookKey] = acc
	}

	acc.TotalTrades++
	if trade.NetPnL > 0 {
		acc.WinningTrades++
	} else {
		acc.LosingTrades++
	}

	acc.TotalPnL += trade.PnL
	acc.TotalFees += trade.Fees
	acc.NetPnL += trade.NetPnL
	acc.WinRate = float64(acc.WinningTrades) / float64(acc.TotalTrades) * 100
	acc.LastUpdateTime = time.Now()
}

// GetTradeHistory returns the trade history for a specific book
func (pm *PnLManager) GetTradeHistory(book *bitso.Book) []TradePnL {
	var history []TradePnL
	for _, trade := range pm.trades {
		if trade.Book.String() == book.String() {
			history = append(history, trade)
		}
	}
	return history
}

// GetAccumulatedPnL returns the accumulated PnL for a specific book
func (pm *PnLManager) GetAccumulatedPnL(book *bitso.Book) *AccumulatedPnL {
	return pm.accumulated[book.String()]
}
