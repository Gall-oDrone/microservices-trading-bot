package models

import "time"

// FillLedgerEntry is one append-only record for a fully filled order (audit / FIFO future use).
type FillLedgerEntry struct {
	ID             string    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	OrderID        string    `json:"order_id"`
	BitsoOrderID   string    `json:"bitso_order_id,omitempty"`
	Book           string    `json:"book"`
	Strategy       string    `json:"strategy"`
	Side           string    `json:"side"`
	Amount         float64   `json:"amount"`
	AvgPrice       float64   `json:"avg_price"`
	FeesQuote      float64   `json:"fees_quote,omitempty"`
	RealizedPnLMXN float64   `json:"realized_pnl_mxn"`
}
