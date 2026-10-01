package dailyexec

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/strategy-executor/internal/bitsostage"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time        { return c.t }
func (c *fakeClock) Sleep(d time.Duration) { c.t = c.t.Add(d) }

type fakeOrder struct {
	bitsostage.OpenOrder
	req       bitsostage.OrderRequest
	cancelled bool
}

// fakeExchange keeps orders and trades. onPoll lets a test fill orders as
// time passes; rejectPostOnly rejects that many post-only placements.
type fakeExchange struct {
	t              *testing.T
	clk            *fakeClock
	quote          bitsostage.Quote
	bal            map[string]float64
	orders         []*fakeOrder
	trades         []bitsostage.Trade
	rejectPostOnly int
	marketFill     float64 // share of a market order that fills (default 1)
	onPoll         func(f *fakeExchange)
	placed         []bitsostage.OrderRequest
	cancels        int
	nextID         int
	// btcFeeRate > 0 models Bitso as observed on stage: buys pay the fee in
	// BTC out of the amount received, and a market buy for X trades
	// X/(1-fee) gross so that X arrives. Sells pay the fee in the quote.
	btcFeeRate float64
}

func newFake(t *testing.T, clk *fakeClock) *fakeExchange {
	return &fakeExchange{t: t, clk: clk, quote: bitsostage.Quote{Bid: 100, Ask: 101},
		bal: map[string]float64{"btc": 1, "usd": 1000}, marketFill: 1}
}

func (f *fakeExchange) Ticker(string) (bitsostage.Quote, error) { return f.quote, nil }
func (f *fakeExchange) Balances() (map[string]float64, error) {
	out := map[string]float64{}
	for k, v := range f.bal {
		out[k] = v
	}
	return out, nil
}

func (f *fakeExchange) PlaceOrder(o bitsostage.OrderRequest) (string, error) {
	f.placed = append(f.placed, o)
	for _, x := range f.orders {
		if !x.cancelled && x.Unfilled > 0 && x.OriginID == o.OriginID {
			return "", &bitsostage.APIError{HTTPStatus: 400, Code: "dup", Message: "origin_id in use by an active order"}
		}
	}
	if o.TimeInForce == "postonly" && f.rejectPostOnly > 0 {
		f.rejectPostOnly--
		return "", &bitsostage.APIError{HTTPStatus: 400, Code: "0343", Message: "post-only order would cross"}
	}
	f.nextID++
	oid := fmt.Sprintf("oid%d", f.nextID)
	qty := num(o.Major)
	if o.Type == "market" && o.Side == "buy" && f.btcFeeRate > 0 {
		qty = math.Round(qty/(1-f.btcFeeRate)*1e8) / 1e8
	}
	ord := &fakeOrder{OpenOrder: bitsostage.OpenOrder{Oid: oid, OriginID: o.OriginID, Book: o.Book, Side: o.Side,
		Price: num(o.Price), Original: qty, Unfilled: qty, CreatedAt: f.clk.Now()}, req: o}
	f.orders = append(f.orders, ord)
	if o.Type == "market" {
		price := f.quote.Ask
		if o.Side == "sell" {
			price = f.quote.Bid
		}
		f.fill(ord, qty*f.marketFill, price)
	}
	return oid, nil
}

func (f *fakeExchange) fill(o *fakeOrder, qty, price float64) {
	qty = math.Min(qty, o.Unfilled)
	if qty <= 0 {
		return
	}
	o.Unfilled -= qty
	fee, cur := qty*price*0.003, "usd"
	if f.btcFeeRate > 0 && o.Side == "buy" {
		fee, cur = math.Round(qty*f.btcFeeRate*1e8)/1e8, "btc"
	}
	f.trades = append(f.trades, bitsostage.Trade{Oid: o.Oid, OriginID: o.OriginID, Book: o.Book, Side: o.Side,
		Major: qty, Minor: qty * price, Price: price, Fee: fee, FeeCurrency: cur})
}

func (f *fakeExchange) OpenOrders(string) ([]bitsostage.OpenOrder, error) {
	if f.onPoll != nil {
		f.onPoll(f)
	}
	var out []bitsostage.OpenOrder
	for _, o := range f.orders {
		if !o.cancelled && o.Unfilled > 1e-12 && o.req.Type == "limit" {
			out = append(out, o.OpenOrder)
		}
	}
	return out, nil
}

func (f *fakeExchange) CancelOrder(oid string) error {
	f.cancels++
	for _, o := range f.orders {
		if o.Oid == oid {
			o.cancelled = true
			return nil
		}
	}
	return fmt.Errorf("unknown oid %s", oid)
}

func (f *fakeExchange) TradesByOrigin(id string) ([]bitsostage.Trade, error) {
	var out []bitsostage.Trade
	for _, t := range f.trades {
		if t.OriginID == id {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeExchange) openMaker() *fakeOrder {
	for _, o := range f.orders {
		if !o.cancelled && o.Unfilled > 1e-12 && o.req.Type == "limit" {
			return o
		}
	}
	return nil
}

func num(s string) float64 {
	var v float64
	fmt.Sscan(s, &v)
	return v
}

func testCfg() Config {
	c := DefaultConfig()
	c.MinQuoteValue = 0.01 // fake prices are ~100, so 0.001 BTC is worth ~0.1
	return c
}

var t0 = time.Date(2026, 10, 1, 6, 5, 0, 0, time.UTC)

func leg(side string) Leg {
	return Leg{Book: "btc_usd", Side: side, Qty: 0.001, FillDate: "2026-10-01"}
}

func nolog(string, ...any) {}

func TestOriginIDsAreValidAndDistinct(t *testing.T) {
	m, tk := OriginIDs("btc_mxn", "2026-10-01", "sell")
	if m != "sma50-btc_mxn-20261001-s-m" || tk != "sma50-btc_mxn-20261001-s-t" {
		t.Fatalf("got %s %s", m, tk)
	}
	for _, id := range []string{m, tk} {
		if len(id) > 40 || strings.Trim(id, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-") != "" {
			t.Fatalf("invalid origin id %q", id)
		}
	}
}

func TestMakerFillsWithinTimeout(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	ex.onPoll = func(f *fakeExchange) {
		if o := f.openMaker(); o != nil && clk.Now().Sub(o.CreatedAt) >= 10*time.Minute {
			f.fill(o, o.Unfilled, o.Price)
		}
	}
	res, err := Run(ex, clk, testCfg(), leg("buy"), nolog)
	if err != nil {
		t.Fatal(err)
	}
	if res.Filled != 0.001 || res.MakerFilled != 0.001 || res.Fallback || len(ex.placed) != 1 {
		t.Fatalf("want one maker fill: %+v placed=%d", res, len(ex.placed))
	}
	p := ex.placed[0]
	if p.TimeInForce != "postonly" || p.Type != "limit" || p.Price != "100" || p.Major != "0.00100000" || p.OriginID != res.MakerOrigin {
		t.Fatalf("maker order: %+v", p)
	}
	if res.AvgPrice != 100 {
		t.Fatalf("avg price %v", res.AvgPrice)
	}
}

func TestSellRestsAtAsk(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	ex.onPoll = func(f *fakeExchange) {
		if o := f.openMaker(); o != nil {
			f.fill(o, o.Unfilled, o.Price)
		}
	}
	if _, err := Run(ex, clk, testCfg(), leg("sell"), nolog); err != nil {
		t.Fatal(err)
	}
	if ex.placed[0].Price != "101" || ex.placed[0].Side != "sell" {
		t.Fatalf("sell must rest at the ask: %+v", ex.placed[0])
	}
}

// Reproduces the first stage run (btc_mxn, 2026-10-01): a tiny maker fill,
// then a market fallback that Bitso grosses up so the requested amount
// arrives after its BTC fee. The position must follow the BTC actually
// received (BaseDelta), not the gross trade amounts (Filled).
func TestBuyBaseDeltaIsNetOfBTCFees(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	ex.btcFeeRate = 0.0078
	filledOnce := false
	ex.onPoll = func(f *fakeExchange) {
		if o := f.openMaker(); o != nil && !filledOnce {
			f.fill(o, 0.0000012, o.Price)
			filledOnce = true
		}
	}
	res, err := Run(ex, clk, testCfg(), leg("buy"), nolog)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Fallback || len(ex.placed) != 2 || ex.placed[1].Major != "0.00099880" {
		t.Fatalf("want one maker + one market for the remainder: %+v placed=%+v", res, ex.placed)
	}
	if res.Filled <= res.Target {
		t.Fatalf("grossed-up market buy should trade more than the target: %+v", res)
	}
	want := math.Round((res.Filled-res.Fees["btc"])*1e8) / 1e8
	if res.BaseDelta != want || math.Abs(res.BaseDelta-0.00099999) > 2e-8 {
		t.Fatalf("base delta %.8f, want %.8f (~0.00099999); filled %.8f fees %v", res.BaseDelta, want, res.Filled, res.Fees)
	}
}

func TestSellBaseDeltaIsMinusFilled(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	ex.btcFeeRate = 0.0078 // sells pay the fee in the quote currency
	ex.onPoll = func(f *fakeExchange) {
		if o := f.openMaker(); o != nil {
			f.fill(o, o.Unfilled, o.Price)
		}
	}
	res, err := Run(ex, clk, testCfg(), leg("sell"), nolog)
	if err != nil {
		t.Fatal(err)
	}
	if res.BaseDelta != -0.001 || res.Fees["btc"] != 0 {
		t.Fatalf("sell: base delta %.8f fees %v", res.BaseDelta, res.Fees)
	}
}

func TestPartialFillThenMarketFallbackAtTimeout(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	filledOnce := false
	ex.onPoll = func(f *fakeExchange) {
		if o := f.openMaker(); o != nil && !filledOnce {
			f.fill(o, 0.0004, o.Price)
			filledOnce = true
		}
	}
	res, err := Run(ex, clk, testCfg(), leg("buy"), nolog)
	if err != nil {
		t.Fatal(err)
	}
	if ex.cancels != 1 {
		t.Fatalf("maker order must be cancelled at timeout, cancels=%d", ex.cancels)
	}
	last := ex.placed[len(ex.placed)-1]
	if last.Type != "market" || last.Major != "0.00060000" || last.OriginID != res.TakerOrigin {
		t.Fatalf("fallback must be a market order for the remainder: %+v", last)
	}
	if math.Abs(res.Filled-0.001) > 1e-12 || math.Abs(res.MakerFilled-0.0004) > 1e-12 || !res.Fallback {
		t.Fatalf("result: %+v", res)
	}
	if got := res.Finished.Sub(res.Started); got < 60*time.Minute || got > 62*time.Minute {
		t.Fatalf("fallback should happen at the 60-minute timeout, took %s", got)
	}
}

func TestPostOnlyRejectionsRetryThenFallBack(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	ex.rejectPostOnly = 99
	res, err := Run(ex, clk, testCfg(), leg("buy"), nolog)
	if err != nil {
		t.Fatal(err)
	}
	if res.Placements != 5 || !res.Fallback || res.TakerFilled != 0.001 {
		t.Fatalf("want 5 rejected placements then market: %+v", res)
	}
	if clk.Now().Sub(t0) > 10*time.Minute {
		t.Fatal("repeated rejections should fall back quickly, not wait for the timeout")
	}
}

func TestResumeAfterCrashDoesNotDoubleBuy(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	mk, _ := OriginIDs("btc_usd", "2026-10-01", "buy")
	// First run placed a maker order 20 minutes ago that filled 0.0007, then crashed.
	ex.nextID = 1
	ord := &fakeOrder{OpenOrder: bitsostage.OpenOrder{Oid: "oid1", OriginID: mk, Book: "btc_usd", Side: "buy", Price: 100,
		Original: 0.001, Unfilled: 0.001, CreatedAt: t0.Add(-20 * time.Minute)},
		req: bitsostage.OrderRequest{Type: "limit", TimeInForce: "postonly", OriginID: mk}}
	ex.orders = append(ex.orders, ord)
	ex.fill(ord, 0.0007, 100)

	res, err := Run(ex, clk, testCfg(), leg("buy"), nolog)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ex.placed {
		if p.Type == "limit" {
			t.Fatalf("must resume the existing maker order, not place another: %+v", p)
		}
	}
	if last := ex.placed[len(ex.placed)-1]; last.Major != "0.00030000" {
		t.Fatalf("fallback must cover only the remainder: %+v", last)
	}
	if got := res.Finished.Sub(t0); got > 42*time.Minute {
		t.Fatalf("resume must keep the original deadline (40 min left), took %s", got)
	}
	if math.Abs(res.Filled-0.001) > 1e-12 {
		t.Fatalf("filled %v", res.Filled)
	}
}

func TestRerunAfterCompletionPlacesNothing(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	ex.rejectPostOnly = 99
	if _, err := Run(ex, clk, testCfg(), leg("buy"), nolog); err != nil {
		t.Fatal(err)
	}
	n := len(ex.placed)
	res, err := Run(ex, clk, testCfg(), leg("buy"), nolog)
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.placed) != n || res.Filled != 0.001 {
		t.Fatalf("re-run must place nothing: placed %d -> %d, %+v", n, len(ex.placed), res)
	}
}

func TestNeverSendsSecondMarketOrder(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	ex.rejectPostOnly = 99
	ex.marketFill = 0.5 // market order only half fills
	res, err := Run(ex, clk, testCfg(), leg("buy"), nolog)
	if err != nil {
		t.Fatal(err)
	}
	markets := 0
	for _, p := range ex.placed {
		if p.Type == "market" {
			markets++
		}
	}
	if markets != 1 || math.Abs(res.Shortfall-0.0005) > 1e-12 {
		t.Fatalf("want exactly one market order and a recorded shortfall: markets=%d %+v", markets, res)
	}
	if _, err := Run(ex, clk, testCfg(), leg("buy"), nolog); err != nil {
		t.Fatal(err)
	}
	for _, p := range ex.placed[len(ex.placed)-1:] {
		if p.Type == "market" && len(ex.placed) > 6 {
			t.Fatalf("re-run sent another market order: %+v", ex.placed)
		}
	}
}

func TestInsufficientFundsPlacesNothing(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	ex.bal["usd"] = 0.05
	if _, err := Run(ex, clk, testCfg(), leg("buy"), nolog); err == nil || !strings.Contains(err.Error(), "insufficient usd") {
		t.Fatalf("want insufficient funds error, got %v", err)
	}
	if len(ex.placed) != 0 {
		t.Fatalf("no order may be placed without funds: %+v", ex.placed)
	}
}

func TestAuthErrorStopsImmediately(t *testing.T) {
	clk := &fakeClock{t0}
	ex := &authFail{newFake(t, clk)}
	if _, err := Run(ex, clk, testCfg(), leg("buy"), nolog); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("want auth error, got %v", err)
	}
}

type authFail struct{ *fakeExchange }

func (a *authFail) PlaceOrder(bitsostage.OrderRequest) (string, error) {
	return "", &bitsostage.APIError{HTTPStatus: 401, Code: "0201", Message: "invalid nonce or signature"}
}

func TestStageClientRefusesProduction(t *testing.T) {
	for _, u := range []string{"https://api.bitso.com", "https://bitso.com/api", "https://stage.bitso.com/api.evil.com", ""} {
		if _, err := bitsostage.New(u, "k", "s"); err == nil {
			t.Fatalf("base URL %q must be refused", u)
		}
	}
	if _, err := bitsostage.New(bitsostage.StageBaseURL, "k", "s"); err != nil {
		t.Fatal(err)
	}
}

// A fill that lands between the open-orders and trades queries must not
// cause a second order (regression test for the query-order race).
func TestFillBetweenQueriesDoesNotDoubleOrder(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	polls := 0
	// The fill lands just before the open-orders snapshot. With the old
	// query order (trades first), the loop saw no fills AND no open order,
	// and placed a second order.
	ex.onPoll = func(f *fakeExchange) {
		polls++
		if o := f.openMaker(); o != nil && polls == 3 {
			f.fill(o, o.Unfilled, o.Price)
		}
	}
	res, err := Run(ex, clk, testCfg(), leg("buy"), nolog)
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.placed) != 1 || res.Filled != 0.001 {
		t.Fatalf("placed %d orders, want 1: %+v", len(ex.placed), res)
	}
}

func TestBelowMinimumRemainderIsRecordedNotSent(t *testing.T) {
	clk := &fakeClock{t0}
	ex := newFake(t, clk)
	ex.rejectPostOnly = 99
	cfg := testCfg()
	cfg.MinQuoteValue = 10 // 0.001 BTC at ~101 = 0.10 < 10
	res, err := Run(ex, clk, cfg, leg("buy"), nolog)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ex.placed {
		if p.Type == "market" {
			t.Fatalf("a remainder below the minimum value must not be sent: %+v", p)
		}
	}
	if res.Shortfall != 0.001 {
		t.Fatalf("shortfall %v", res.Shortfall)
	}
}
