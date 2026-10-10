// Package etorobroker adapts the eToro Public API client to broker.Broker.
//
// Idempotency (verified on the demo API, 2026-10-10): every attempt for a
// ClientRef sends x-request-id RequestIDFor(ClientRef), and eToro rejects a
// reused request id with HTTP 400 "ReferenceID ... may already exists". A
// re-run after a crash or timeout therefore cannot open twice; the duplicate
// rejection is resolved to the position the first attempt opened. eToro's
// orders:lookup?referenceId= cannot be used for this: it only finds
// "external operations" and answers 404 for API-placed orders. Closes rely
// on eToro refusing to close a position that is not open (631, 632, 741),
// surfaced as broker.ErrPositionNotOpen.
package etorobroker

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoro"
)

// Venue is the broker.Instrument.Venue of eToro instruments.
const Venue = "etoro"

// eToro error codes for a close of a position that is not open: 631 already
// closed and 632 already pending close (MCP skill "place-close" hints), 741
// "requested position is already closed" (seen on the demo API 2026-10-10,
// returned in the close-order info of an accepted close request).
var notOpenCodes = map[int]bool{631: true, 632: true, 741: true}

// Client is the subset of *etoro.Client the adapter uses.
type Client interface {
	Rate(ctx context.Context, instrumentID int64) (etoro.Rate, error)
	OpenOrder(ctx context.Context, req etoro.OrderRequest, requestID string) (etoro.OrderAccepted, error)
	LookupOrder(ctx context.Context, orderID int64) (etoro.OrderInfo, error)
	ClosePosition(ctx context.Context, positionID, instrumentID int64, unitsToDeduct *float64, requestID string) (etoro.CloseAccepted, error)
	GetCloseOrder(ctx context.Context, orderID int64) (etoro.CloseOrderInfo, error)
	GetPortfolio(ctx context.Context) (*etoro.Portfolio, error)
	Costs(ctx context.Context, req etoro.OrderRequest) (etoro.CostPreview, error)
}

// Adapter implements broker.Broker on eToro.
type Adapter struct {
	c Client
	// Wait bounds how long Open/Close poll for a final state (default 30s,
	// like the MCP place-trade tool); Poll is the interval (default 2s).
	Wait  time.Duration
	Poll  time.Duration
	sleep func(context.Context, time.Duration) error
	now   func() time.Time
}

// New wraps a client.
func New(c Client) *Adapter {
	return &Adapter{c: c, Wait: 30 * time.Second, Poll: 2 * time.Second, sleep: sleepCtx, now: time.Now}
}

var _ broker.Broker = (*Adapter)(nil)

// Venue implements broker.Broker.
func (a *Adapter) Venue() string { return Venue }

// Quote implements broker.Broker.
func (a *Adapter) Quote(ctx context.Context, in broker.Instrument) (broker.Quote, error) {
	if in.ID <= 0 {
		return broker.Quote{}, fmt.Errorf("etorobroker: instrument %q has no id", in.Symbol)
	}
	r, err := a.c.Rate(ctx, in.ID)
	if err != nil {
		return broker.Quote{}, err
	}
	at := r.Time()
	if at.IsZero() {
		at = a.now().UTC()
	}
	return broker.Quote{Instrument: in, Bid: r.Bid, Ask: r.Ask, At: at}, nil
}

func orderRequest(req broker.OpenRequest) etoro.OrderRequest {
	r := etoro.MarketBuyByAmount(req.Instrument.ID, req.Amount, req.Leverage)
	if req.Side == broker.Short {
		r.Transaction = etoro.TxSellShort
	}
	if req.StopLoss > 0 {
		sl := req.StopLoss
		r.StopLossRate = &sl
	}
	if req.TakeProfit > 0 {
		tp := req.TakeProfit
		r.TakeProfitRate = &tp
	}
	return r
}

// Open implements broker.Broker.
//
// Every attempt for a ClientRef uses x-request-id RequestIDFor(ClientRef).
// eToro rejects a reused request id (etoro.IsDuplicateReference), so:
//   - success: this call placed the order; it is polled to a final state;
//   - duplicate reference: an earlier call placed it; it is resolved to the
//     direct position (or pending open order) on the instrument opened at or
//     after IntentAt and returned without placing anything;
//   - no response / 5xx / 429: re-sent ONCE with the same id, which either
//     places it (first attempt never landed) or reports the duplicate;
//   - other 4xx: a definite rejection.
func (a *Adapter) Open(ctx context.Context, req broker.OpenRequest) (broker.Order, error) {
	if err := req.Validate(); err != nil {
		return broker.Order{}, err
	}
	if req.Instrument.ID <= 0 {
		return broker.Order{}, errors.Join(broker.ErrInvalidRequest, errors.New("etoro needs an instrument id"))
	}
	er := orderRequest(req)
	if err := er.Validate(); err != nil {
		return broker.Order{}, errors.Join(broker.ErrInvalidRequest, err)
	}
	ref := etoro.RequestIDFor(req.ClientRef)
	since := req.IntentAt
	if since.IsZero() {
		since = a.now().Add(-time.Hour)
	}

	acc, err := a.c.OpenOrder(ctx, er, ref)
	if err != nil && mayHaveLanded(err) && !etoro.IsDuplicateReference(err) {
		first := err
		acc, err = a.c.OpenOrder(ctx, er, ref)
		if err != nil && mayHaveLanded(err) && !etoro.IsDuplicateReference(err) {
			return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusUnknown}, errors.Join(broker.ErrOutcomeUnknown, first, err)
		}
	}
	if etoro.IsDuplicateReference(err) {
		return a.resolveDuplicate(ctx, req, since, err)
	}
	if err != nil {
		return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusRejected, Error: err.Error(), ErrorCode: apiCode(err)}, err
	}
	info, werr := a.waitOrder(ctx, acc.OrderID)
	if werr != nil {
		// Accepted but not yet readable: it exists, report it pending.
		return broker.Order{OrderID: strconv.FormatInt(acc.OrderID, 10), ClientRef: req.ClientRef, Status: broker.StatusPending}, nil
	}
	return toOrder(info, req.ClientRef), nil
}

// resolveDuplicate finds the order an earlier attempt placed: exactly one
// direct position on the instrument and side opened at or after since (the
// executor is the only actor on its instruments), else one pending open
// order; then reads the order by id for the fill details. The pnl portfolio
// lags a fill by a few seconds (demo, 2026-10-10), so it is polled until
// Wait runs out before the outcome is declared unknown.
func (a *Adapter) resolveDuplicate(ctx context.Context, req broker.OpenRequest, since time.Time, dupErr error) (broker.Order, error) {
	isBuy := req.Side == broker.Long
	deadline := a.now().Add(a.Wait)
	var matches []etoro.Position
	var pending []etoro.OpenOrderState
	for {
		p, err := a.c.GetPortfolio(ctx)
		if err != nil {
			return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusUnknown}, errors.Join(broker.ErrOutcomeUnknown, dupErr, err)
		}
		matches, pending = matches[:0], pending[:0]
		for _, pos := range p.PositionsFor(req.Instrument.ID) {
			if pos.IsBuy == isBuy && !pos.OpenedAt().Before(since.Add(-time.Minute)) {
				matches = append(matches, pos)
			}
		}
		for _, o := range p.OrdersForOpen {
			if o.InstrumentID == req.Instrument.ID && o.IsBuy == isBuy {
				pending = append(pending, o)
			}
		}
		if len(matches) > 0 || len(pending) > 0 || !a.now().Before(deadline) {
			break
		}
		if err := a.sleep(ctx, a.Poll); err != nil {
			break
		}
	}
	if len(matches) == 1 {
		pos := matches[0]
		if pos.OrderID > 0 {
			if info, err := a.c.LookupOrder(ctx, pos.OrderID); err == nil {
				return toOrder(info, req.ClientRef), nil
			}
		}
		return broker.Order{
			OrderID: strconv.FormatInt(pos.OrderID, 10), ClientRef: req.ClientRef, Status: broker.StatusExecuted,
			PositionIDs: []string{strconv.FormatInt(pos.PositionID, 10)}, AvgPrice: pos.OpenRate, Units: pos.Units,
			Amount: pos.Amount, At: pos.OpenedAt(),
		}, nil
	}
	if len(matches) == 0 && len(pending) == 1 {
		return broker.Order{OrderID: strconv.FormatInt(pending[0].OrderID, 10), ClientRef: req.ClientRef, Status: broker.StatusPending}, nil
	}
	return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusUnknown},
		errors.Join(broker.ErrOutcomeUnknown, fmt.Errorf("duplicate reference for %s but %d matching positions and %d pending orders since %s",
			req.ClientRef, len(matches), len(pending), since.UTC().Format(time.RFC3339)), dupErr)
}

func (a *Adapter) waitOrder(ctx context.Context, orderID int64) (etoro.OrderInfo, error) {
	deadline := a.now().Add(a.Wait)
	var last etoro.OrderInfo
	var lastErr error
	for {
		info, err := a.c.LookupOrder(ctx, orderID)
		if err == nil {
			last, lastErr = info, nil
			if oc := info.Outcome(); oc != etoro.OutcomePending && oc != etoro.OutcomeUnknown {
				return info, nil
			}
		} else if !etoro.IsNotFound(err) {
			lastErr = err
		}
		if !a.now().Before(deadline) {
			if last.OrderID != 0 {
				return last, nil
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("order %d not visible after %s", orderID, a.Wait)
			}
			return last, lastErr
		}
		if err := a.sleep(ctx, a.Poll); err != nil {
			if last.OrderID != 0 {
				return last, nil
			}
			return last, err
		}
	}
}

// executionAmount is the cash committed to a position: the margin, else the
// initial exposure divided by leverage. investedAmountCurrency is not used:
// on the demo API it read 1 for a 1000 USD x1 position (2026-10-10).
func executionAmount(pe etoro.PositionExecution, leverage int) float64 {
	if pe.MarginAccountCurrency > 0 {
		return pe.MarginAccountCurrency
	}
	if pe.InitialExposureAccountCurrency > 0 {
		if leverage < 1 {
			leverage = 1
		}
		return pe.InitialExposureAccountCurrency / float64(leverage)
	}
	return pe.InvestedAmountCurrency
}

func toOrder(info etoro.OrderInfo, clientRef string) broker.Order {
	o := broker.Order{
		OrderID:   strconv.FormatInt(info.OrderID, 10),
		ClientRef: clientRef,
		ErrorCode: info.Status.ErrorCode,
		Error:     info.Status.ErrorMessage,
	}
	switch info.Outcome() {
	case etoro.OutcomeExecuted:
		o.Status = broker.StatusExecuted
	case etoro.OutcomeRejected:
		o.Status = broker.StatusRejected
	case etoro.OutcomeCancelled:
		o.Status = broker.StatusCancelled
	case etoro.OutcomePending:
		o.Status = broker.StatusPending
	default:
		o.Status = broker.StatusUnknown
	}
	var notional float64
	for _, pe := range info.PositionExecutions {
		if pe.PositionID > 0 {
			o.PositionIDs = append(o.PositionIDs, strconv.FormatInt(pe.PositionID, 10))
		}
		o.Amount += executionAmount(pe, info.Asset.Leverage)
		if od := pe.OpeningData; od != nil {
			o.Units += od.Units
			notional += od.Units * od.AvgPrice
			o.Fees += od.Fees + od.Taxes
			o.SpreadCost += od.MarketSpread + od.Markup
			if t := parseTime(od.ExecutionTime); !t.IsZero() {
				o.At = t
			}
		}
	}
	if o.Units > 0 {
		o.AvgPrice = notional / o.Units
	}
	return o
}

// Close implements broker.Broker.
func (a *Adapter) Close(ctx context.Context, req broker.CloseRequest) (broker.Order, error) {
	pid, err := strconv.ParseInt(req.PositionID, 10, 64)
	if err != nil || pid <= 0 || req.Instrument.ID <= 0 || req.ClientRef == "" {
		return broker.Order{}, errors.Join(broker.ErrInvalidRequest, fmt.Errorf("close needs position id, instrument id and client_ref (got %+v)", req))
	}
	var units *float64
	if req.Units > 0 {
		u := req.Units
		units = &u
	}
	acc, err := a.c.ClosePosition(ctx, pid, req.Instrument.ID, units, etoro.RequestIDFor(req.ClientRef))
	if err != nil {
		if notOpenCodes[apiCode(err)] {
			return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusRejected, Error: err.Error(), ErrorCode: apiCode(err)},
				errors.Join(broker.ErrPositionNotOpen, err)
		}
		if mayHaveLanded(err) {
			return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusUnknown}, errors.Join(broker.ErrOutcomeUnknown, err)
		}
		return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusRejected, Error: err.Error(), ErrorCode: apiCode(err)}, err
	}
	oid := acc.OrderForClose.OrderID
	o := broker.Order{OrderID: strconv.FormatInt(oid, 10), ClientRef: req.ClientRef, Status: broker.StatusPending,
		PositionIDs: []string{req.PositionID}}
	if oid <= 0 {
		return o, nil
	}
	deadline := a.now().Add(a.Wait)
	for {
		info, err := a.c.GetCloseOrder(ctx, oid)
		if err == nil {
			if info.ErrorCode != 0 {
				o.Status, o.ErrorCode, o.Error = broker.StatusRejected, info.ErrorCode, info.ErrorMessage
				if notOpenCodes[info.ErrorCode] {
					return o, fmt.Errorf("%w: code %d %s", broker.ErrPositionNotOpen, info.ErrorCode, info.ErrorMessage)
				}
				return o, nil
			}
			if len(info.Positions) > 0 {
				o.Status = broker.StatusExecuted
				var notional float64
				for _, f := range info.Positions {
					o.Units += f.Units
					o.Amount += f.Amount
					notional += f.Units * f.Rate
					if t := parseTime(f.Occurred); !t.IsZero() {
						o.At = t
					}
				}
				if o.Units > 0 {
					o.AvgPrice = notional / o.Units
				}
				return o, nil
			}
		}
		if !a.now().Before(deadline) {
			return o, nil
		}
		if err := a.sleep(ctx, a.Poll); err != nil {
			return o, nil
		}
	}
}

// Positions implements broker.Broker: direct (non copy-trading) positions.
func (a *Adapter) Positions(ctx context.Context) ([]broker.Position, error) {
	p, err := a.c.GetPortfolio(ctx)
	if err != nil {
		return nil, err
	}
	var out []broker.Position
	for _, pos := range p.Positions {
		if pos.MirrorID != 0 {
			continue
		}
		side := broker.Long
		if !pos.IsBuy {
			side = broker.Short
		}
		out = append(out, broker.Position{
			ID:            strconv.FormatInt(pos.PositionID, 10),
			Instrument:    broker.Instrument{Venue: Venue, ID: pos.InstrumentID},
			Side:          side,
			Units:         pos.Units,
			OpenRate:      pos.OpenRate,
			Amount:        pos.Amount,
			Leverage:      pos.Leverage,
			OpenedAt:      pos.OpenedAt(),
			UnrealizedPnL: pos.UnrealizedPnL.PnL,
		})
	}
	return out, nil
}

// Account implements broker.Broker.
func (a *Adapter) Account(ctx context.Context) (broker.Account, error) {
	p, err := a.c.GetPortfolio(ctx)
	if err != nil {
		return broker.Account{}, err
	}
	cur := "USD"
	if p.AccountCurrencyID != 0 && p.AccountCurrencyID != 1 {
		cur = "currency#" + strconv.Itoa(p.AccountCurrencyID)
	}
	return broker.Account{Currency: cur, Cash: p.Credit, Equity: p.Equity(), UnrealizedPnL: p.UnrealizedPnL}, nil
}

// PreviewOpen implements broker.Broker.
func (a *Adapter) PreviewOpen(ctx context.Context, req broker.OpenRequest) (broker.Costs, error) {
	if req.ClientRef == "" {
		req.ClientRef = "preview"
	}
	if err := req.Validate(); err != nil {
		return broker.Costs{}, err
	}
	p, err := a.c.Costs(ctx, orderRequest(req))
	if err != nil {
		return broker.Costs{}, err
	}
	cur := "USD"
	if len(p.Costs) > 0 && p.Costs[0].Currency != "" {
		cur = p.Costs[0].Currency
	}
	return broker.Costs{
		Currency:       cur,
		Spread:         p.Get(etoro.CostMarketSpread),
		Markup:         p.Get(etoro.CostMarkup),
		TransactionFee: p.Get(etoro.CostTransactionFee),
		OvernightFee:   p.Get(etoro.CostOvernightFee),
		WeekendFee:     p.Get(etoro.CostOverWeekendFee),
	}, nil
}

// mayHaveLanded: no response, a 5xx or a 429 can hide an accepted order;
// other 4xx are definite rejections.
func mayHaveLanded(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	return etoro.IsRetryable(err)
}

func apiCode(err error) int {
	var ae *etoro.APIError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return 0
}

func parseTime(s string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
