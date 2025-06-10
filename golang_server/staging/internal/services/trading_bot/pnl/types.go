package pnl

import (
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
