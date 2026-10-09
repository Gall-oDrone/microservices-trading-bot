package dailyledger

// PreregCost is a book's frozen per-leg cost assumptions, copied from its
// pre-registration in docs/backtest-readiness/. The daily-executor's paper
// account uses PrimaryLegBps (Paper.LegCostBps in every record);
// cmd/daily-executor/ledger_contract_test.go fails if the two drift apart.
// SecondaryLegBps is the registered pessimistic (taker) scenario.
type PreregCost struct {
	Prereg          string  // pre-registration file name
	PrimaryLegBps   float64 // maker commission + 10 bps slippage
	SecondaryLegBps float64 // taker commission + 10 bps slippage
}

// PreregCosts must not change while the forward tests run (both end on
// 2027-09-26). Fees are Bitso's published
// lowest-volume tier as registered; the frozen fees decide pass or fail.
var PreregCosts = map[string]PreregCost{
	"btc_mxn": {Prereg: "FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md", PrimaryLegBps: 60 + 10, SecondaryLegBps: 78 + 10},
	"btc_usd": {Prereg: "FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md", PrimaryLegBps: 30 + 10, SecondaryLegBps: 36 + 10},
}

// LegCost is one filled leg's realized cost, in the book's quote currency.
// Slippage is measured against the fill day's open (the paper account's
// price); SlippageKnown is false when that open is not available.
type LegCost struct {
	Notional      float64
	FeeQuote      float64
	SlippageBps   float64 // + is adverse
	SlippageKnown bool
}

// CostBudget compares realized execution cost with the pre-registered
// assumptions, the way a desk's transaction-cost analysis compares
// implementation shortfall with its budget: notional-weighted, in money.
type CostBudget struct {
	Legs            int     `json:"legs"`
	LegsWithoutRef  int     `json:"legs_without_ref"` // slippage unknown: cost is fees only
	Notional        float64 `json:"notional"`         // Σ filled notional, quote
	CostQuote       float64 `json:"cost_quote"`       // Σ fees + Σ slippage, quote
	WeightedBps     float64 `json:"weighted_bps"`     // CostQuote / Notional
	PrimaryLegBps   float64 `json:"primary_leg_bps"`
	SecondaryLegBps float64 `json:"secondary_leg_bps"`
	BudgetQuote     float64 `json:"budget_quote"`     // Notional × primary
	ExcessQuote     float64 `json:"excess_quote"`     // CostQuote − BudgetQuote (+ is over budget)
	BudgetUsed      float64 `json:"budget_used"`      // CostQuote / BudgetQuote (0 without legs)
	Prereg          string  `json:"prereg"`           // "" for a book without a pre-registration
	OverPessimistic bool    `json:"over_pessimistic"` // WeightedBps > SecondaryLegBps
}

// CostBudgetMinLegs is the number of legs before a weighted cost above the
// pessimistic scenario is a finding; a single leg is already checked against
// the policy's cost_warn_bps, and one leg is too noisy to call a trend.
const CostBudgetMinLegs = 3

// BudgetFor sums legs against book's pre-registered costs. A book without a
// pre-registration has zero budget lines and never sets OverPessimistic.
func BudgetFor(book string, legs []LegCost) CostBudget {
	pc, ok := PreregCosts[book]
	b := CostBudget{Legs: len(legs)}
	if ok {
		b.Prereg, b.PrimaryLegBps, b.SecondaryLegBps = pc.Prereg, pc.PrimaryLegBps, pc.SecondaryLegBps
	}
	for _, l := range legs {
		if l.Notional <= 0 {
			continue
		}
		b.Notional += l.Notional
		b.CostQuote += l.FeeQuote
		if l.SlippageKnown {
			b.CostQuote += l.SlippageBps / 1e4 * l.Notional
		} else {
			b.LegsWithoutRef++
		}
	}
	if b.Notional > 0 {
		b.WeightedBps = b.CostQuote / b.Notional * 1e4
	}
	b.BudgetQuote = b.Notional * b.PrimaryLegBps / 1e4
	b.ExcessQuote = b.CostQuote - b.BudgetQuote
	if b.BudgetQuote > 0 {
		b.BudgetUsed = b.CostQuote / b.BudgetQuote
	}
	b.OverPessimistic = ok && b.Notional > 0 && b.WeightedBps > b.SecondaryLegBps
	return b
}
