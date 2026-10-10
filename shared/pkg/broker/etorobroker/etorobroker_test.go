package etorobroker

import (
	"context"
	"errors"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoro"
)

// fakeClient models the eToro behaviour verified on demo (2026-10-10): an
// open with a request id that was already used is rejected with HTTP 400
// "ReferenceID ... may already exists"; executed orders appear as positions
// (with their orderID) in the portfolio.
type fakeClient struct {
	usedRefs   map[string]int64 // request id -> order id
	orders     map[int64][]etoro.OrderInfo
	lookups    map[int64]int
	positions  []etoro.Position
	pending    []etoro.OpenOrderState
	openErrs   []error // per call: error returned instead of normal handling
	landOnErr  bool    // an injected error still places the order
	opens      []string
	closeErr   error
	closeInfo  []etoro.CloseOrderInfo
	costs      etoro.CostPreview
	nextID     int64
	clock      *time.Time
	fillStatus string
	// portfolioLag hides positions from the next N GetPortfolio calls (the
	// demo pnl route shows a fill a few seconds late).
	portfolioLag int
	// history pages returned by TradingHistory (page n = history[n-1]).
	history [][]etoro.ClosedTrade
}

func newFake(clock *time.Time) *fakeClient {
	return &fakeClient{usedRefs: map[string]int64{}, orders: map[int64][]etoro.OrderInfo{}, lookups: map[int64]int{},
		nextID: 387753200, clock: clock, fillStatus: "Filled"}
}

func dupErr(ref string, oid int64) error {
	return &etoro.APIError{StatusCode: 400, Message: "Validation failed: \n -- : ReferenceID " + ref + " may already exists for CID 1 and OrderID 999 Severity: Error"}
}

func (f *fakeClient) place(ref string, req etoro.OrderRequest) int64 {
	f.nextID++
	id, pid := f.nextID, f.nextID*10
	f.usedRefs[ref] = id
	units := *req.Amount / 30963
	f.orders[id] = []etoro.OrderInfo{{OrderID: id, Status: etoro.OrderStatus{ID: 3, Name: f.fillStatus},
		Asset: etoro.OrderAsset{InstrumentID: req.InstrumentID, SettlementType: "CFD", Leverage: req.Leverage, Side: "long"},
		PositionExecutions: []etoro.PositionExecution{{PositionID: pid, State: "open", InvestedAmountCurrency: 1,
			InitialExposureAccountCurrency: *req.Amount, MarginAccountCurrency: *req.Amount,
			OpeningData: &etoro.OpeningData{Units: units, AvgPrice: 30963, MarketSpread: 1.49, Markup: 0.06, ExecutionTime: "2026-10-12T13:35:02.223Z"}}}}}
	if f.fillStatus == "Filled" {
		f.positions = append(f.positions, etoro.Position{PositionID: pid, InstrumentID: req.InstrumentID, OrderID: id, IsBuy: true,
			Units: units, OpenRate: 30963, Amount: *req.Amount, Leverage: req.Leverage, OpenDateTime: f.clock.Format(time.RFC3339Nano)})
	} else {
		f.orders[id][0].PositionExecutions = nil // not executed yet
	}
	return id
}

func (f *fakeClient) Rate(ctx context.Context, id int64) (etoro.Rate, error) {
	return etoro.Rate{InstrumentID: id, Bid: 100, Ask: 101, Date: "2026-10-12T13:35:00.000"}, nil
}

func (f *fakeClient) OpenOrder(ctx context.Context, req etoro.OrderRequest, ref string) (etoro.OrderAccepted, error) {
	f.opens = append(f.opens, ref)
	if len(f.openErrs) > 0 {
		e := f.openErrs[0]
		f.openErrs = f.openErrs[1:]
		if e != nil {
			if f.landOnErr {
				if _, used := f.usedRefs[ref]; !used {
					f.place(ref, req)
				}
			}
			return etoro.OrderAccepted{}, e
		}
	}
	if oid, used := f.usedRefs[ref]; used {
		return etoro.OrderAccepted{}, dupErr(ref, oid)
	}
	id := f.place(ref, req)
	return etoro.OrderAccepted{OrderID: id, ReferenceID: ref}, nil
}

func (f *fakeClient) LookupOrder(ctx context.Context, id int64) (etoro.OrderInfo, error) {
	seq := f.orders[id]
	if len(seq) == 0 {
		return etoro.OrderInfo{}, &etoro.APIError{StatusCode: 404}
	}
	i := f.lookups[id]
	if i >= len(seq) {
		i = len(seq) - 1
	}
	f.lookups[id]++
	return seq[i], nil
}

func (f *fakeClient) ClosePosition(ctx context.Context, pid, iid int64, u *float64, rid string) (etoro.CloseAccepted, error) {
	if f.closeErr != nil {
		return etoro.CloseAccepted{}, f.closeErr
	}
	var acc etoro.CloseAccepted
	acc.OrderForClose.OrderID = 387838268
	acc.OrderForClose.PositionID = pid
	return acc, nil
}

func (f *fakeClient) GetCloseOrder(ctx context.Context, id int64) (etoro.CloseOrderInfo, error) {
	if len(f.closeInfo) == 0 {
		return etoro.CloseOrderInfo{OrderID: id}, nil
	}
	info := f.closeInfo[0]
	if len(f.closeInfo) > 1 {
		f.closeInfo = f.closeInfo[1:]
	}
	return info, nil
}

func (f *fakeClient) GetPortfolio(ctx context.Context) (*etoro.Portfolio, error) {
	if f.portfolioLag > 0 {
		f.portfolioLag--
		return &etoro.Portfolio{Credit: 99000, AccountCurrencyID: 1}, nil
	}
	return &etoro.Portfolio{Credit: 99000, UnrealizedPnL: -1.49, AccountCurrencyID: 1, Positions: f.positions, OrdersForOpen: f.pending}, nil
}

func (f *fakeClient) Costs(ctx context.Context, req etoro.OrderRequest) (etoro.CostPreview, error) {
	return f.costs, nil
}

func (f *fakeClient) TradingHistory(ctx context.Context, minDate time.Time, page, pageSize int) ([]etoro.ClosedTrade, error) {
	if page < 1 || page > len(f.history) {
		return nil, nil
	}
	return f.history[page-1], nil
}

// newAdapter uses a fake clock that advances only when the adapter sleeps.
func newAdapter(t *testing.T) (*Adapter, *fakeClient, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 12, 13, 35, 0, 0, time.UTC)
	f := newFake(&now)
	a := New(f)
	a.now = func() time.Time { return now }
	a.sleep = func(ctx context.Context, d time.Duration) error { now = now.Add(d); return ctx.Err() }
	return a, f, &now
}

var nsdq = broker.Instrument{Venue: Venue, Symbol: "NSDQ100", ID: 28}

func openReq(ref string, intent time.Time) broker.OpenRequest {
	return broker.OpenRequest{Instrument: nsdq, Side: broker.Long, Amount: 1000, Leverage: 1, ClientRef: ref, IntentAt: intent}
}

func TestOpenFreshExecutes(t *testing.T) {
	a, f, now := newAdapter(t)
	o, err := a.Open(context.Background(), openReq("sma50-NSDQ100-2026-10-12-open", *now))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.opens) != 1 || f.opens[0] != etoro.RequestIDFor("sma50-NSDQ100-2026-10-12-open") {
		t.Fatalf("opens %v: want one open with the derived reference", f.opens)
	}
	if o.Status != broker.StatusExecuted || o.AvgPrice != 30963 || len(o.PositionIDs) != 1 || o.SpreadCost != 1.55 {
		t.Fatalf("order %+v", o)
	}
	if o.Amount != 1000 {
		t.Fatalf("amount %v: must come from margin, not investedAmountCurrency (reads 1 on demo)", o.Amount)
	}
	if o.At.IsZero() {
		t.Fatal("execution time not mapped")
	}
}

func TestReOpenSameRefResolvesToFirstPosition(t *testing.T) {
	a, f, now := newAdapter(t)
	intent := *now
	first, err := a.Open(context.Background(), openReq("r1", intent))
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(5 * time.Minute) // a re-run later
	again, err := a.Open(context.Background(), openReq("r1", intent))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.positions) != 1 || len(f.opens) != 2 {
		t.Fatalf("positions %d opens %d: the re-run must be rejected as a duplicate, not open", len(f.positions), len(f.opens))
	}
	if again.Status != broker.StatusExecuted || again.OrderID != first.OrderID || again.PositionIDs[0] != first.PositionIDs[0] {
		t.Fatalf("re-run %+v, first %+v", again, first)
	}
}

// Live demo: the re-send is rejected within a second, before the pnl
// portfolio shows the position; the adapter keeps polling.
func TestReOpenResolvesAfterPortfolioLag(t *testing.T) {
	a, f, now := newAdapter(t)
	intent := *now
	first, err := a.Open(context.Background(), openReq("lag", intent))
	if err != nil {
		t.Fatal(err)
	}
	f.portfolioLag = 2
	again, err := a.Open(context.Background(), openReq("lag", intent))
	if err != nil || again.PositionIDs[0] != first.PositionIDs[0] || f.portfolioLag != 0 {
		t.Fatalf("re-run %+v err %v lag left %d", again, err, f.portfolioLag)
	}
}

func TestAmbiguousFailureReSentOnce(t *testing.T) {
	// 503 but the order landed: the re-send is a duplicate -> resolved.
	a, f, now := newAdapter(t)
	f.openErrs = []error{&etoro.APIError{StatusCode: 503}}
	f.landOnErr = true
	o, err := a.Open(context.Background(), openReq("r2", *now))
	if err != nil || o.Status != broker.StatusExecuted || len(f.positions) != 1 || len(f.opens) != 2 {
		t.Fatalf("landed order: %+v %v positions %d opens %d", o, err, len(f.positions), len(f.opens))
	}

	// Transport error, nothing landed: the re-send places it exactly once.
	a2, f2, now2 := newAdapter(t)
	f2.openErrs = []error{&etoro.TransportError{Method: "POST", Path: "/orders", Err: errors.New("reset")}}
	o, err = a2.Open(context.Background(), openReq("r3", *now2))
	if err != nil || o.Status != broker.StatusExecuted || len(f2.positions) != 1 {
		t.Fatalf("not-landed order: %+v %v positions %d", o, err, len(f2.positions))
	}

	// Both attempts fail without an answer: unknown, never a third send.
	a3, f3, now3 := newAdapter(t)
	f3.openErrs = []error{&etoro.TransportError{Err: errors.New("reset")}, &etoro.APIError{StatusCode: 502}}
	o, err = a3.Open(context.Background(), openReq("r4", *now3))
	if !errors.Is(err, broker.ErrOutcomeUnknown) || o.Status != broker.StatusUnknown || len(f3.opens) != 2 {
		t.Fatalf("unresolved: %+v %v opens %d", o, err, len(f3.opens))
	}
}

func TestDuplicateWithoutMatchingPositionIsUnknown(t *testing.T) {
	a, f, now := newAdapter(t)
	ref := etoro.RequestIDFor("r5")
	f.usedRefs[ref] = 1 // used earlier, but its position is gone (closed) or older than the intent
	f.positions = []etoro.Position{{PositionID: 1, InstrumentID: 28, IsBuy: true, OrderID: 1, OpenDateTime: "2026-10-01T13:35:00Z"}}
	o, err := a.Open(context.Background(), openReq("r5", *now))
	if !errors.Is(err, broker.ErrOutcomeUnknown) || o.Status != broker.StatusUnknown {
		t.Fatalf("got %+v %v: an old position must not be taken for this order", o, err)
	}
	f.pending = []etoro.OpenOrderState{{OrderID: 55, InstrumentID: 28, IsBuy: true}}
	o, err = a.Open(context.Background(), openReq("r5", *now))
	if err != nil || o.Status != broker.StatusPending || o.OrderID != "55" {
		t.Fatalf("pending order: %+v %v", o, err)
	}
}

func TestOpenDefiniteRejection(t *testing.T) {
	a, f, now := newAdapter(t)
	f.openErrs = []error{&etoro.APIError{StatusCode: 400, Code: 720, Message: "remaining amount too low"}}
	o, err := a.Open(context.Background(), openReq("r6", *now))
	if err == nil || errors.Is(err, broker.ErrOutcomeUnknown) || len(f.opens) != 1 {
		t.Fatalf("4xx must be a definite error without a re-send, got %v (opens %d)", err, len(f.opens))
	}
	if o.Status != broker.StatusRejected || o.ErrorCode != 720 {
		t.Fatalf("order %+v", o)
	}
}

func TestOpenStillPendingAfterWait(t *testing.T) {
	a, f, now := newAdapter(t)
	f.fillStatus = "Pending"
	a.Wait = 6 * time.Second
	o, err := a.Open(context.Background(), openReq("r7", *now))
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != broker.StatusPending {
		t.Fatalf("order %+v, want pending (never re-send)", o)
	}
	var polls int
	for _, n := range f.lookups {
		polls += n
	}
	if polls < 3 {
		t.Fatalf("polled %d times in 6s at 2s", polls)
	}
}

func TestOpenValidation(t *testing.T) {
	a, _, now := newAdapter(t)
	short := openReq("r8", *now)
	short.Side = broker.Short
	if _, err := a.Open(context.Background(), short); !errors.Is(err, broker.ErrInvalidRequest) {
		t.Fatalf("short without stop-loss: %v", err)
	}
	if _, err := a.Open(context.Background(), openReq("", *now)); !errors.Is(err, broker.ErrInvalidRequest) {
		t.Fatalf("missing client ref: %v", err)
	}
	lev := openReq("r9", *now)
	lev.Leverage = 2
	if _, err := a.Open(context.Background(), lev); !errors.Is(err, broker.ErrInvalidRequest) {
		t.Fatalf("x2 without stop-loss: %v", err)
	}
}

func TestCloseExecutesAndNotOpen(t *testing.T) {
	a, f, _ := newAdapter(t)
	f.closeInfo = []etoro.CloseOrderInfo{{OrderID: 387838268}, {OrderID: 387838268, StatusID: 3, Proceeds: 998.49,
		Positions: []etoro.ClosedFill{{PositionID: 3616261788, Rate: 30917, Units: 0.032296, Amount: 999.98, Occurred: "2026-10-10T02:06:40.497Z"}}}}
	o, err := a.Close(context.Background(), broker.CloseRequest{Instrument: nsdq, PositionID: "3616261788", ClientRef: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != broker.StatusExecuted || o.AvgPrice != 30917 || o.Units != 0.032296 || o.OrderID != "387838268" {
		t.Fatalf("close %+v", o)
	}

	// Demo: re-closing is accepted, then the close order carries 741.
	a2, f2, _ := newAdapter(t)
	f2.closeInfo = []etoro.CloseOrderInfo{{OrderID: 387838269, ErrorCode: 741, ErrorMessage: "Order cannot be placed because the requested position is already closed"}}
	o, err = a2.Close(context.Background(), broker.CloseRequest{Instrument: nsdq, PositionID: "3616261788", ClientRef: "c1"})
	if !errors.Is(err, broker.ErrPositionNotOpen) || o.ErrorCode != 741 {
		t.Fatalf("741: %+v %v, want ErrPositionNotOpen", o, err)
	}

	a3, f3, _ := newAdapter(t)
	f3.closeErr = &etoro.APIError{StatusCode: 400, Code: 632, Message: "position already pending close"}
	if _, err := a3.Close(context.Background(), broker.CloseRequest{Instrument: nsdq, PositionID: "1010", ClientRef: "x"}); !errors.Is(err, broker.ErrPositionNotOpen) {
		t.Fatalf("632: %v, want ErrPositionNotOpen", err)
	}
	if _, err := a.Close(context.Background(), broker.CloseRequest{Instrument: nsdq, PositionID: "abc", ClientRef: "x"}); !errors.Is(err, broker.ErrInvalidRequest) {
		t.Fatalf("bad position id: %v", err)
	}
}

func TestPositionsAccountAndPreview(t *testing.T) {
	a, f, _ := newAdapter(t)
	f.positions = []etoro.Position{
		{PositionID: 1010, InstrumentID: 28, IsBuy: true, Units: 0.03, OpenRate: 30950, Amount: 1000, Leverage: 1, TotalFees: 0.69,
			OpenDateTime: "2026-10-12T13:35:02Z", UnrealizedPnL: etoro.PositionPnL{PnL: -1.49, ExposureInAccountCurrency: 998.51}},
		{PositionID: 2020, InstrumentID: 27, IsBuy: true, Amount: 500, MirrorID: 9},
	}
	f.costs = etoro.CostPreview{InstrumentID: 28, Costs: []etoro.Cost{{Type: "markup", Currency: "USD"}, {Type: "marketSpread", Currency: "USD", Value: 1.52}, {Type: "overnightFee", Currency: "USD", Value: 0.23}}}
	ps, err := a.Positions(context.Background())
	if err != nil || len(ps) != 1 || ps[0].ID != "1010" || ps[0].Side != broker.Long || ps[0].OpenedAt.IsZero() || ps[0].UnrealizedPnL != -1.49 ||
		ps[0].Fees != 0.69 || ps[0].Exposure != 998.51 {
		t.Fatalf("positions %+v %v (copy positions excluded)", ps, err)
	}
	acct, err := a.Account(context.Background())
	if err != nil || acct.Currency != "USD" || acct.Cash != 99000 || acct.Equity != 99000+1500-1.49 {
		t.Fatalf("account %+v %v", acct, err)
	}
	c, err := a.PreviewOpen(context.Background(), broker.OpenRequest{Instrument: nsdq, Side: broker.Long, Amount: 1000, Leverage: 1})
	if err != nil || c.Spread != 1.52 || c.OvernightFee != 0.23 || c.Currency != "USD" {
		t.Fatalf("preview %+v %v", c, err)
	}
	q, err := a.Quote(context.Background(), nsdq)
	if err != nil || q.Mid() != 100.5 || q.At.IsZero() {
		t.Fatalf("quote %+v %v", q, err)
	}
}

// The row is the demo trading-history response of 2026-10-10 (trimmed).
func TestClosedPositionsPages(t *testing.T) {
	a, f, _ := newAdapter(t)
	full := make([]etoro.ClosedTrade, 200)
	for i := range full {
		full[i] = etoro.ClosedTrade{PositionID: int64(5000 + i), InstrumentID: 27, IsBuy: true}
	}
	f.history = [][]etoro.ClosedTrade{full, {{PositionID: 3616261792, InstrumentID: 28, IsBuy: true, Leverage: 1,
		OpenRate: 30963, CloseRate: 30919, OpenTimestamp: "2026-10-10T02:07:34.06Z", CloseTimestamp: "2026-10-10T02:08:16.607Z",
		Units: 0.032296, Investment: 999.98, NetProfit: -1.42, Fees: 0}}}
	cps, err := a.ClosedPositions(context.Background(), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(cps) != 201 {
		t.Fatalf("closed %d %v, want 201 over two pages", len(cps), err)
	}
	last := cps[200]
	if last.ID != "3616261792" || last.Instrument.ID != 28 || last.Side != broker.Long || last.CloseRate != 30919 ||
		last.NetProfit != -1.42 || last.Amount != 999.98 || last.ClosedAt != time.Date(2026, 10, 10, 2, 8, 16, 607e6, time.UTC) {
		t.Fatalf("row %+v", last)
	}
}
