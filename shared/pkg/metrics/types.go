package metrics

import (
	"github.com/shopspring/decimal"
)

// MonetaryAmount represents a financial amount with its currency.
// Immutable value type for production-grade financial calculations.
// Use decimal to avoid float rounding errors in P&L and balances.
type MonetaryAmount struct {
	amount   decimal.Decimal
	currency string
}

// NewMonetaryAmount creates a new MonetaryAmount. Currency must be non-empty.
func NewMonetaryAmount(amount decimal.Decimal, currency string) MonetaryAmount {
	if currency == "" {
		currency = "MXN" // default for Bitso MXN books
	}
	return MonetaryAmount{amount: amount, currency: currency}
}

// NewMonetaryAmountFromFloat creates a MonetaryAmount from float64.
// Prefer NewMonetaryAmount with decimal for accuracy; use this only at boundaries (e.g. API input).
func NewMonetaryAmountFromFloat(amount float64, currency string) MonetaryAmount {
	return NewMonetaryAmount(decimal.NewFromFloat(amount), currency)
}

// Amount returns the amount as decimal (immutable).
func (m MonetaryAmount) Amount() decimal.Decimal {
	return m.amount
}

// Currency returns the currency code (e.g. "MXN", "USD").
func (m MonetaryAmount) Currency() string {
	return m.currency
}

// Float64 returns the amount as float64 for exporters (e.g. Prometheus).
// Loss of precision is documented; use for observation only, not accumulation.
func (m MonetaryAmount) Float64() float64 {
	f, _ := m.amount.Float64()
	return f
}

// Add returns a new MonetaryAmount; panics if currency differs.
func (m MonetaryAmount) Add(other MonetaryAmount) MonetaryAmount {
	if m.currency != other.currency {
		panic("metrics: cannot add amounts with different currencies")
	}
	return MonetaryAmount{amount: m.amount.Add(other.amount), currency: m.currency}
}

// PnLSnapshot is an immutable snapshot of profit/loss at a point in time.
// Used when recording or querying P&L for metrics and risk.
type PnLSnapshot struct {
	RealizedPnL   MonetaryAmount
	UnrealizedPnL MonetaryAmount
	Currency      string
}

// TotalPnL returns realized + unrealized as a new MonetaryAmount.
func (p PnLSnapshot) TotalPnL() MonetaryAmount {
	return p.RealizedPnL.Add(p.UnrealizedPnL)
}

// TradeOutcome represents the result of a single closed trade for metrics.
type TradeOutcome struct {
	Book        string
	Strategy    string
	Currency    string
	RealizedPnL MonetaryAmount
	IsWin       bool // true if realized P&L > 0
}
