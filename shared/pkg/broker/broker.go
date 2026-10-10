// Package broker is the venue-neutral execution seam for position-based
// brokers (eToro CFDs today). Executors talk to a Broker; adapters such as
// broker/etorobroker translate to a venue's API.
//
// Contract every adapter keeps:
//   - Open is idempotent per ClientRef: calling it again with the same
//     reference never places a second order. It returns the order the first
//     call placed when the venue can identify it, otherwise an error wrapping
//     ErrOutcomeUnknown (the order exists but could not be located yet);
//   - an outcome that cannot be established (no response, order still
//     working when the wait ends) is StatusPending with a nil error when the
//     order is known to exist, else ErrOutcomeUnknown: resolve it by calling
//     Open again with the SAME ClientRef (never a new one) or by Positions;
//   - amounts are in the account currency (USD for this repository).
//
// The Bitso spot path keeps its own seam (strategy-executor dailyexec.Exchange):
// post-only maker legs and fee-in-base accounting do not map onto positions.
package broker

import (
	"context"
	"errors"
	"time"
)

// Side is a position direction.
type Side string

const (
	Long  Side = "long"
	Short Side = "short"
)

// Instrument identifies a tradable on a venue.
type Instrument struct {
	Venue  string `json:"venue"`
	Symbol string `json:"symbol"`
	ID     int64  `json:"id"`
}

// Quote is a top-of-book snapshot.
type Quote struct {
	Instrument Instrument `json:"instrument"`
	Bid        float64    `json:"bid"`
	Ask        float64    `json:"ask"`
	At         time.Time  `json:"at"`
}

// Mid is (bid+ask)/2.
func (q Quote) Mid() float64 { return (q.Bid + q.Ask) / 2 }

// SpreadBps is the quoted spread in basis points of the mid.
func (q Quote) SpreadBps() float64 {
	if m := q.Mid(); m > 0 {
		return (q.Ask - q.Bid) / m * 1e4
	}
	return 0
}

// OpenRequest opens a market position sized by cash Amount.
type OpenRequest struct {
	Instrument Instrument `json:"instrument"`
	Side       Side       `json:"side"`
	Amount     float64    `json:"amount"`
	Leverage   int        `json:"leverage"`
	StopLoss   float64    `json:"stop_loss,omitempty"`   // absolute rate, 0 = none
	TakeProfit float64    `json:"take_profit,omitempty"` // absolute rate, 0 = none
	// ClientRef is the caller's idempotency key, e.g.
	// "sma50-NSDQ100-2026-10-12-open". Required.
	ClientRef string `json:"client_ref"`
	// IntentAt is when the caller first recorded this order (persisted with
	// the intent, unchanged on re-runs). When the venue reports the order as
	// a duplicate, the adapter recognises its own position as one opened on
	// the instrument at or after IntentAt. Zero: one hour before the call.
	IntentAt time.Time `json:"intent_at,omitempty"`
}

// CloseRequest closes a position fully (Units 0) or partly.
type CloseRequest struct {
	Instrument Instrument `json:"instrument"`
	PositionID string     `json:"position_id"`
	Units      float64    `json:"units,omitempty"`
	ClientRef  string     `json:"client_ref"`
}

// Status is a broker-neutral order state.
type Status string

const (
	StatusPending   Status = "pending"
	StatusExecuted  Status = "executed"
	StatusRejected  Status = "rejected"
	StatusCancelled Status = "cancelled"
	StatusUnknown   Status = "unknown"
)

// Final reports whether the status will not change any more.
func (s Status) Final() bool {
	return s == StatusExecuted || s == StatusRejected || s == StatusCancelled
}

// Order is the result of Open, Close or LookupOpen.
type Order struct {
	OrderID     string   `json:"order_id"`
	ClientRef   string   `json:"client_ref"`
	Status      Status   `json:"status"`
	PositionIDs []string `json:"position_ids,omitempty"`
	// AvgPrice / Units / Amount describe the fill (zero until executed).
	AvgPrice float64 `json:"avg_price,omitempty"`
	Units    float64 `json:"units,omitempty"`
	Amount   float64 `json:"amount,omitempty"`
	// Fees is explicit fees and taxes; SpreadCost is the spread the venue
	// reports for the fill (both account currency).
	Fees       float64   `json:"fees,omitempty"`
	SpreadCost float64   `json:"spread_cost,omitempty"`
	Error      string    `json:"error,omitempty"`
	ErrorCode  int       `json:"error_code,omitempty"`
	At         time.Time `json:"at,omitempty"`
}

// Position is an open position.
type Position struct {
	ID            string     `json:"id"`
	Instrument    Instrument `json:"instrument"`
	Side          Side       `json:"side"`
	Units         float64    `json:"units"`
	OpenRate      float64    `json:"open_rate"`
	Amount        float64    `json:"amount"`
	Leverage      int        `json:"leverage"`
	OpenedAt      time.Time  `json:"opened_at"`
	UnrealizedPnL float64    `json:"unrealized_pnl"`
}

// Account is the cash and equity of the trading account.
type Account struct {
	Currency      string  `json:"currency"`
	Cash          float64 `json:"cash"`
	Equity        float64 `json:"equity"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
}

// Costs previews the cost of an open order (account currency).
type Costs struct {
	Currency       string  `json:"currency"`
	Spread         float64 `json:"spread"`
	Markup         float64 `json:"markup"`
	TransactionFee float64 `json:"transaction_fee"`
	// OvernightFee is the charge for holding the position one night;
	// WeekendFee the extra over-weekend charge when the venue quotes one.
	OvernightFee float64 `json:"overnight_fee"`
	WeekendFee   float64 `json:"weekend_fee"`
}

// Broker is the execution seam.
type Broker interface {
	Venue() string
	Quote(ctx context.Context, in Instrument) (Quote, error)
	Open(ctx context.Context, req OpenRequest) (Order, error)
	Close(ctx context.Context, req CloseRequest) (Order, error)
	Positions(ctx context.Context) ([]Position, error)
	Account(ctx context.Context) (Account, error)
	PreviewOpen(ctx context.Context, req OpenRequest) (Costs, error)
}

var (
	// ErrOutcomeUnknown: the request may or may not have reached the venue,
	// or it did but the order cannot be located yet. Resolve by calling Open
	// again with the same ClientRef (closes: check Positions).
	ErrOutcomeUnknown = errors.New("broker: order outcome unknown")
	// ErrPositionNotOpen: the position is already closed or being closed.
	ErrPositionNotOpen = errors.New("broker: position is not open")
	// ErrInvalidRequest: rejected locally before reaching the venue.
	ErrInvalidRequest = errors.New("broker: invalid request")
)

// Validate checks the venue-independent rules of an open request.
func (r OpenRequest) Validate() error {
	switch {
	case r.ClientRef == "":
		return errors.Join(ErrInvalidRequest, errors.New("client_ref is required"))
	case r.Instrument.ID <= 0 && r.Instrument.Symbol == "":
		return errors.Join(ErrInvalidRequest, errors.New("instrument is required"))
	case r.Side != Long && r.Side != Short:
		return errors.Join(ErrInvalidRequest, errors.New("side must be long or short"))
	case !(r.Amount > 0):
		return errors.Join(ErrInvalidRequest, errors.New("amount must be positive"))
	case r.Leverage < 1:
		return errors.Join(ErrInvalidRequest, errors.New("leverage must be >= 1"))
	}
	return nil
}
