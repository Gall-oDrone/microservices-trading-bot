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
	// Fees is what the venue has charged the position so far (eToro
	// totalFees: overnight financing), account currency. Exposure is the
	// current notional in account currency (0 when the venue does not say).
	Fees     float64 `json:"fees,omitempty"`
	Exposure float64 `json:"exposure,omitempty"`
	// StopLoss / TakeProfit are the resting protective rates the venue
	// holds for the position; 0 means none (adapters map the venue's
	// "no stop" placeholder to 0). StopType is StopFixed or StopTrailing
	// when known. Added 2026-10-19 (protection plan phase A).
	StopLoss   float64 `json:"stop_loss,omitempty"`
	TakeProfit float64 `json:"take_profit,omitempty"`
	StopType   string  `json:"stop_type,omitempty"`
}

// Protected reports whether the position has a stop loss at the venue.
func (p Position) Protected() bool { return p.StopLoss > 0 }

// Stop-loss types.
const (
	StopFixed    = "fixed"
	StopTrailing = "trailing"
)

// ProtectionRequest edits the protective rates of an open position. A zero
// StopLoss / TakeProfit leaves that side unchanged; ClearTakeProfit removes
// the target. A stop is never cleared through this seam (policy: a
// position may gain or move a stop, never lose it). ClientRef is the
// caller's idempotency key, e.g. "ui-nsdq100-2026-10-19-protect-1a2b3c".
type ProtectionRequest struct {
	Instrument      Instrument `json:"instrument"`
	PositionID      string     `json:"position_id"`
	StopLoss        float64    `json:"stop_loss,omitempty"`
	TakeProfit      float64    `json:"take_profit,omitempty"`
	ClearTakeProfit bool       `json:"clear_take_profit,omitempty"`
	StopType        string     `json:"stop_type,omitempty"` // "" = StopFixed
	ClientRef       string     `json:"client_ref"`
}

// Validate checks the venue-independent rules of a protection request.
func (r ProtectionRequest) Validate() error {
	switch {
	case r.ClientRef == "":
		return errors.Join(ErrInvalidRequest, errors.New("client_ref is required"))
	case r.PositionID == "":
		return errors.Join(ErrInvalidRequest, errors.New("position_id is required"))
	case r.StopLoss < 0 || r.TakeProfit < 0:
		return errors.Join(ErrInvalidRequest, errors.New("stop_loss and take_profit must be >= 0"))
	case r.StopLoss == 0 && r.TakeProfit == 0 && !r.ClearTakeProfit:
		return errors.Join(ErrInvalidRequest, errors.New("nothing to change: set stop_loss, take_profit or clear_take_profit"))
	case r.TakeProfit > 0 && r.ClearTakeProfit:
		return errors.Join(ErrInvalidRequest, errors.New("take_profit and clear_take_profit are exclusive"))
	case r.StopType != "" && r.StopType != StopFixed && r.StopType != StopTrailing:
		return errors.Join(ErrInvalidRequest, errors.New("stop_type must be fixed or trailing"))
	}
	return nil
}

// ProtectionResult is the outcome of SetProtection. Status is
// StatusExecuted when the position was read back with the requested rates,
// StatusPending when the venue accepted the edit but the position did not
// show it before the adapter's wait ran out (read Positions later), and
// StatusRejected when the venue refused it. Position is the last state
// read (zero value when none was read).
type ProtectionResult struct {
	PositionID  string    `json:"position_id"`
	ClientRef   string    `json:"client_ref"`
	OperationID string    `json:"operation_id,omitempty"`
	Status      Status    `json:"status"`
	Position    Position  `json:"position"`
	Error       string    `json:"error,omitempty"`
	ErrorCode   int       `json:"error_code,omitempty"`
	At          time.Time `json:"at,omitempty"`
}

// ProtectionEditor is implemented by adapters whose venue can edit the
// stop loss / take profit of an open position (eToro: PATCH position).
// Optional: callers type-assert and refuse to protect when absent.
type ProtectionEditor interface {
	SetProtection(ctx context.Context, req ProtectionRequest) (ProtectionResult, error)
}

// ProtectionLimits are the venue's rules for stops and targets on an
// instrument and direction (eToro: eligibility leverageConfigs[]), as
// fractions of the entry. Zero disables a bound; Tick is the price step.
type ProtectionLimits struct {
	MinStopLossPct   float64 `json:"min_sl_pct"`
	MaxStopLossPct   float64 `json:"max_sl_pct"`
	MaxTakeProfitPct float64 `json:"max_tp_pct"`
	AllowEdit        bool    `json:"allow_edit"`
	AllowTrailing    bool    `json:"allow_trailing"`
	Tick             float64 `json:"tick"`
}

// ProtectionLimitsReader is implemented by adapters that can read those
// rules. Optional.
type ProtectionLimitsReader interface {
	ProtectionLimits(ctx context.Context, in Instrument, side Side, leverage int) (ProtectionLimits, error)
}

// ClosedPosition is one row of the venue's trading history.
type ClosedPosition struct {
	ID         string     `json:"id"`
	Instrument Instrument `json:"instrument"`
	Side       Side       `json:"side"`
	Units      float64    `json:"units"`
	OpenRate   float64    `json:"open_rate"`
	CloseRate  float64    `json:"close_rate"`
	OpenedAt   time.Time  `json:"opened_at"`
	ClosedAt   time.Time  `json:"closed_at"`
	Amount     float64    `json:"amount"`
	// NetProfit is realized P&L before Fees; Fees is everything the venue
	// charged over the position's life (financing included).
	NetProfit float64 `json:"net_profit"`
	Fees      float64 `json:"fees"`
	// StopLoss / TakeProfit are the protective rates the position carried
	// when it closed (0 = none): a close rate at one of them says the venue
	// closed it. Added 2026-10-19.
	StopLoss   float64 `json:"stop_loss,omitempty"`
	TakeProfit float64 `json:"take_profit,omitempty"`
	// Leverage the position carried (0 when the venue did not say; read
	// as x1). Added 2026-10-22 for the real-account canary, whose financing
	// is on Amount x Leverage, not on Amount.
	Leverage int `json:"leverage,omitempty"`
}

// HistoryReader is implemented by adapters that can list closed positions
// (reconciliation). since must be less than one year ago on eToro.
type HistoryReader interface {
	ClosedPositions(ctx context.Context, since time.Time) ([]ClosedPosition, error)
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
