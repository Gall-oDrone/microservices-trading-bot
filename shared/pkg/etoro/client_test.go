package etoro

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fixtures in testdata/ are trimmed real demo responses captured on
// 2026-10-10 (see docs/etoro/evidence-2026-10-10); order/close shapes are the
// portal's OpenAPI examples (no real order had been placed when written).

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type recorded struct {
	method, path, rawQuery string
	header                 http.Header
	body                   []byte
}

// newTestClient serves handler and returns a demo client whose sleeps are
// instant and whose pacing never waits.
func newTestClient(t *testing.T, env Environment, handler func(w http.ResponseWriter, r *http.Request, rec recorded)) (*Client, *[]recorded) {
	t.Helper()
	var calls []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec := recorded{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), body}
		calls = append(calls, rec)
		handler(w, r, rec)
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(Config{PublicKey: "pub", PrivateKey: "priv", Env: env, BaseURL: srv.URL,
		ReadsPerMinute: 6000, WritesPerMinute: 6000})
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(ctx context.Context, d time.Duration) error { return ctx.Err() }
	return c, &calls
}

func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func TestNewClientValidation(t *testing.T) {
	if _, err := NewClient(Config{PublicKey: "a"}); err == nil {
		t.Fatal("missing user key accepted")
	}
	if _, err := NewClient(Config{PublicKey: "a", PrivateKey: "b", Env: "paper"}); err == nil {
		t.Fatal("unknown env accepted")
	}
	c, err := NewClient(Config{PublicKey: "a", PrivateKey: "b"})
	if err != nil || c.Environment() != EnvDemo {
		t.Fatalf("default env = %v, %v; want demo", c.Environment(), err)
	}
}

func TestResolveSymbolExactAndHeaders(t *testing.T) {
	c, calls := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		writeJSON(w, 200, fixture(t, "search_nsdq100.json"))
	})
	in, err := c.ResolveSymbol(context.Background(), "nsdq100")
	if err != nil {
		t.Fatal(err)
	}
	if in.InstrumentID != 28 || in.Symbol != "NSDQ100" || in.Exchange != "CFD" || in.AssetClass != "Indices" {
		t.Fatalf("resolved %+v, want NSDQ100 id 28 (CFD, Indices)", in)
	}
	got := (*calls)[0]
	if got.path != "/api/v1/market-data/search" || got.rawQuery != "internalSymbolFull=nsdq100" {
		t.Fatalf("request %s?%s", got.path, got.rawQuery)
	}
	if got.header.Get("x-api-key") != "pub" || got.header.Get("x-user-key") != "priv" {
		t.Fatal("auth headers missing")
	}
	if len(got.header.Get("x-request-id")) != 36 {
		t.Fatalf("x-request-id %q is not a UUID", got.header.Get("x-request-id"))
	}
	if _, err := c.ResolveSymbol(context.Background(), "NSDQ"); err == nil {
		t.Fatal("loose match resolved; must be exact")
	}
}

func TestGetRatesCommaQuery(t *testing.T) {
	c, calls := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		writeJSON(w, 200, fixture(t, "rates_v2.json"))
	})
	rs, err := c.GetRates(context.Background(), 27, 28, 3000, 3006)
	if err != nil {
		t.Fatal(err)
	}
	if q := (*calls)[0].rawQuery; q != "instrumentIds=27,28,3000,3006" {
		t.Fatalf("query %q: commas must stay literal", q)
	}
	if (*calls)[0].path != "/api/v2/market-data/rates" {
		t.Fatalf("path %s", (*calls)[0].path)
	}
	if len(rs) != 4 || rs[1].InstrumentID != 28 || rs[1].Bid != 30914 || rs[1].Ask != 30962 {
		t.Fatalf("rates %+v", rs)
	}
	if rs[1].Mid() != 30938 || rs[1].Spread() != 48 {
		t.Fatalf("mid %v spread %v", rs[1].Mid(), rs[1].Spread())
	}
	if want := time.Date(2026, 10, 10, 1, 45, 59, 923e6, time.UTC); !rs[1].Time().Equal(want) {
		t.Fatalf("time %v, want %v", rs[1].Time(), want)
	}
	r, err := c.Rate(context.Background(), 28)
	if err != nil || r.InstrumentID != 28 {
		t.Fatalf("Rate(28) = %+v, %v", r, err)
	}
}

func TestCandlesOldestFirst(t *testing.T) {
	c, calls := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		writeJSON(w, 200, fixture(t, "candles_28_oneday.json"))
	})
	cs, err := c.Candles(context.Background(), 28, OneDay, 10)
	if err != nil {
		t.Fatal(err)
	}
	if p := (*calls)[0].path; p != "/api/v1/market-data/instruments/28/history/candles/desc/OneDay/10" {
		t.Fatalf("path %s", p)
	}
	if len(cs) != 10 {
		t.Fatalf("%d candles", len(cs))
	}
	for i := 1; i < len(cs); i++ {
		if !cs[i-1].FromDate.Before(cs[i].FromDate) {
			t.Fatalf("not oldest first at %d: %v then %v", i, cs[i-1].FromDate, cs[i].FromDate)
		}
	}
	last := cs[len(cs)-1]
	if last.FromDate.Format("2006-01-02") != "2026-10-10" || last.Close != 30915 {
		t.Fatalf("newest %+v", last)
	}
	if _, err := c.Candles(context.Background(), 28, OneDay, 1001); err == nil {
		t.Fatal("count above MaxCandles accepted")
	}
}

func TestHistoryCandlesPaging(t *testing.T) {
	page := 0
	c, calls := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		page++
		if page == 1 {
			writeJSON(w, 200, []byte(`{"instrumentId":28,"interval":"1d","pagination":{"limit":2,"hasNext":true,"nextCursor":"CUR1"},
				"results":[{"time":"2026-09-02T21:00:00Z","open":"3","high":"4","low":"2","close":"3.5"},
				           {"time":"2026-09-01T21:00:00Z","open":2,"high":3,"low":1,"close":2.5}]}`))
			return
		}
		writeJSON(w, 200, []byte(`{"instrumentId":28,"interval":"1d","pagination":{"limit":2,"hasNext":false,"nextCursor":null},
			"results":[{"time":"2026-09-01T21:00:00Z","open":2,"high":3,"low":1,"close":2.5},
			           {"time":"2026-08-31T21:00:00Z","open":"1","high":"2","low":"0.5","close":"1.5"}]}`))
	})
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	hs, err := c.HistoryCandles(context.Background(), 28, "1d", from, time.Time{}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 2 || !strings.Contains((*calls)[1].rawQuery, "cursor=CUR1") {
		t.Fatalf("paging calls %+v", *calls)
	}
	if !strings.Contains((*calls)[0].rawQuery, "from=2026-08-01T00%3A00%3A00Z") || !strings.Contains((*calls)[0].rawQuery, "interval=1d") {
		t.Fatalf("query %s", (*calls)[0].rawQuery)
	}
	if len(hs) != 3 || hs[0].Close.Float() != 1.5 || hs[2].Close.Float() != 3.5 {
		t.Fatalf("history %+v (want 3 de-duplicated bars, oldest first, string and number prices)", hs)
	}
}

func TestRealHistoryFixtureDecodes(t *testing.T) {
	var r histResponse
	if err := json.Unmarshal(fixture(t, "history_28_1h.json"), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Results) != 4 || r.Results[0].Open.Float() <= 0 || r.Symbol != "NSDQ100/USD" {
		t.Fatalf("decoded %+v", r)
	}
}

func TestEligibilityLeverage(t *testing.T) {
	c, calls := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		writeJSON(w, 200, fixture(t, "eligibility.json"))
	})
	els, err := c.Eligibility(context.Background(), []int64{27, 28, 3000, 3006})
	if err != nil {
		t.Fatal(err)
	}
	got := (*calls)[0]
	if got.method != "POST" || got.path != "/api/v2/trading/info/demo/eligibility" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	if !strings.Contains(string(got.body), `"instrumentIds":[27,28,3000,3006]`) {
		t.Fatalf("body %s", got.body)
	}
	var nsdq Eligibility
	for _, e := range els {
		if e.Symbol == "NSDQ100" {
			nsdq = e
		}
	}
	if nsdq.InstrumentID != 28 || nsdq.MinPositionExposure != 1000 || !nsdq.AllowOpenPosition {
		t.Fatalf("NSDQ100 eligibility %+v", nsdq)
	}
	lc, ok := nsdq.Config("long", 1)
	if !ok || lc.SettlementType != SettlementCFD || lc.MaxStopLossPercentage != 100 {
		t.Fatalf("long x1 config %+v %v: want cfd with SL up to 100%%", lc, ok)
	}
	if _, ok := nsdq.Config("long", 3); ok {
		t.Fatal("x3 is not an allowed leverage")
	}
	if _, ok := nsdq.Config("short", 20); !ok {
		t.Fatal("short x20 should be listed")
	}
}

func TestCostsPreview(t *testing.T) {
	c, calls := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		writeJSON(w, 200, fixture(t, "costs_open_nsdq100.json"))
	})
	p, err := c.Costs(context.Background(), MarketBuyByAmount(28, 1000, 1))
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].path != "/api/v2/trading/info/demo/costs" {
		t.Fatalf("path %s", (*calls)[0].path)
	}
	if p.Get(CostMarketSpread) != 1.52 || p.Get(CostOvernightFee) != 0.23 || p.Get(CostTransactionFee) != 0 {
		t.Fatalf("costs %+v", p)
	}
}

func TestPortfolioEmptyDemo(t *testing.T) {
	c, calls := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		writeJSON(w, 200, fixture(t, "pnl_demo_empty.json"))
	})
	p, err := c.GetPortfolio(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].path != "/api/v1/trading/info/demo/pnl" {
		t.Fatalf("path %s", (*calls)[0].path)
	}
	if p.Credit != 100000 || len(p.Positions) != 0 || p.Equity() != 100000 {
		t.Fatalf("portfolio %+v", p)
	}
}

func TestPortfolioPositionsDecodeEitherCase(t *testing.T) {
	var r pnlResponse
	// First position: the live demo shape (2026-10-10, CID removed), where a
	// position's unrealizedPnL is an object.
	body := `{"clientPortfolio":{"credit":10,"unrealizedPnL":5,"positions":[
		{"unrealizedPnL":{"pnL":-1.49,"pnlAssetCurrency":-1.49,"exposureInAccountCurrency":998.5,"marginInAccountCurrency":999.98,"closeRate":30917.0,"timestamp":"2026-10-10T02:08:07.3128381Z"},
		 "positionID":3616261792,"openDateTime":"2026-10-10T02:07:34.06Z","openRate":30963.0,"instrumentID":28,"isBuy":true,
		 "takeProfitRate":0.0,"stopLossRate":0.01,"mirrorID":0,"amount":999.98,"leverage":1,"orderID":387753204,"orderType":17,
		 "units":0.032296,"totalFees":0.0,"initialAmountInDollars":999.98,"isNoTakeProfit":true,"isNoStopLoss":true},
		{"positionId":9003,"instrumentId":28,"isBuy":true,"amount":500,"mirrorId":7}]}}`
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatal(err)
	}
	p := &r.ClientPortfolio
	got := p.PositionsFor(28)
	if len(got) != 1 || got[0].PositionID != 3616261792 || got[0].OrderID != 387753204 || got[0].UnrealizedPnL.PnL != -1.49 {
		t.Fatalf("PositionsFor(28) = %+v (copy-trading position must be excluded)", got)
	}
	if p.PositionByID(9003) == nil || p.Equity() != 10+999.98+500+5 {
		t.Fatalf("equity %v", p.Equity())
	}
	if got := p.Positions[0].OpenedAt(); !got.Equal(time.Date(2026, 10, 10, 2, 7, 34, 60e6, time.UTC)) {
		t.Fatalf("openDateTime parsed as %v", got)
	}
}

func TestIsDuplicateReference(t *testing.T) {
	// Verbatim (CID redacted) from a demo re-send with the same x-request-id.
	msg := "Validation failed: \n -- : ReferenceID 107352f1-169d-5225-8b88-ba500aac1abc may already exists for CID 0 and OrderID 387739382 Severity: Error"
	if !IsDuplicateReference(&APIError{StatusCode: 400, Message: msg}) {
		t.Fatal("duplicate reference not recognised")
	}
	if IsDuplicateReference(&APIError{StatusCode: 400, Message: "Validation failed: amount too low"}) || IsDuplicateReference(&APIError{StatusCode: 500, Message: msg}) {
		t.Fatal("false positive")
	}
	if IsRetryable(&APIError{StatusCode: 400, Message: msg}) {
		t.Fatal("a duplicate reference is not retryable")
	}
}

func TestOpenOrderSendsReferenceAndIsNeverRetried(t *testing.T) {
	ref := RequestIDFor("sma50-NSDQ100-2026-10-12-open")
	status := 200
	c, calls := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		if status != 200 {
			writeJSON(w, status, []byte(`{"title":"Service Unavailable","status":503}`))
			return
		}
		writeJSON(w, 200, []byte(`{"token":"066faaee-e1e9-49d2-a568-c6e1cc336ad8","orderId":13902598,"referenceId":"`+rec.header.Get("x-request-id")+`"}`))
	})
	acc, err := c.OpenOrder(context.Background(), MarketBuyByAmount(28, 1000, 1), ref)
	if err != nil {
		t.Fatal(err)
	}
	got := (*calls)[0]
	if got.method != "POST" || got.path != "/api/v2/trading/execution/demo/orders" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	if got.header.Get("x-request-id") != ref || acc.ReferenceID != ref || acc.OrderID != 13902598 {
		t.Fatalf("reference not round-tripped: header %q accepted %+v", got.header.Get("x-request-id"), acc)
	}
	var body map[string]any
	_ = json.Unmarshal(got.body, &body)
	if body["action"] != "open" || body["transaction"] != "buy" || body["instrumentId"] != float64(28) ||
		body["amount"] != float64(1000) || body["leverage"] != float64(1) || body["orderType"] != "mkt" {
		t.Fatalf("body %s", got.body)
	}
	if _, has := body["stopLossRate"]; has {
		t.Fatalf("x1 buy must not send a placeholder stop-loss: %s", got.body)
	}

	status = 503
	*calls = nil
	_, err = c.OpenOrder(context.Background(), MarketBuyByAmount(28, 1000, 1), ref)
	if err == nil || len(*calls) != 1 {
		t.Fatalf("write retried or succeeded: err %v, calls %d (want 1 call, error)", err, len(*calls))
	}
	if !IsRetryable(err) {
		t.Fatalf("503 should classify as retryable (caller resolves by reference): %v", err)
	}
}

func TestOrderRequestValidate(t *testing.T) {
	sl := 25000.0
	amt, units := 1000.0, 0.03
	cases := []struct {
		name string
		r    OrderRequest
		ok   bool
	}{
		{"x1 market buy", MarketBuyByAmount(28, 1000, 1), true},
		{"x2 without stop-loss", MarketBuyByAmount(28, 1000, 2), false},
		{"x2 with stop-loss", func() OrderRequest { r := MarketBuyByAmount(28, 1000, 2); r.StopLossRate = &sl; return r }(), true},
		{"short without stop-loss", OrderRequest{Action: "open", Transaction: TxSellShort, InstrumentID: 28, Leverage: 1, Amount: &amt}, false},
		{"amount and units", OrderRequest{Action: "open", Transaction: TxBuy, InstrumentID: 28, Leverage: 1, Amount: &amt, Units: &units}, false},
		{"no size", OrderRequest{Action: "open", Transaction: TxBuy, InstrumentID: 28, Leverage: 1}, false},
		{"close via v2", OrderRequest{Action: "close", Transaction: TxBuy, InstrumentID: 28, Leverage: 1, Amount: &amt}, false},
		{"no instrument", MarketBuyByAmount(0, 1000, 1), false},
		{"mit without trigger", func() OrderRequest { r := MarketBuyByAmount(28, 1000, 1); r.OrderType = OrderMIT; return r }(), false},
	}
	for _, tc := range cases {
		if err := tc.r.Validate(); (err == nil) != tc.ok {
			t.Errorf("%s: err %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func TestReadsRetryButStopOnClientErrors(t *testing.T) {
	var n int32
	c, _ := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Retry-After", "3")
			writeJSON(w, 429, []byte(`{"error":"Too Many Requests"}`))
			return
		}
		writeJSON(w, 200, fixture(t, "rates_v2.json"))
	})
	var waited []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error { waited = append(waited, d); return nil }
	if _, err := c.GetRates(context.Background(), 28); err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(waited) == 0 || waited[len(waited)-1] != 3*time.Second {
		t.Fatalf("calls %d waits %v: want one retry after the 3s Retry-After", n, waited)
	}

	atomic.StoreInt32(&n, 0)
	c2, _ := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		atomic.AddInt32(&n, 1)
		writeJSON(w, 403, []byte(`{"title":"Forbidden","detail":"InsufficientPermissions: key is for the real environment"}`))
	})
	_, err := c2.GetPortfolio(context.Background())
	if n != 1 || !IsInsufficientPermissions(err) || IsRetryable(err) {
		t.Fatalf("calls %d err %v: a 403 must not be retried", n, err)
	}
}

func TestAPIErrorParsing(t *testing.T) {
	h := http.Header{}
	h.Set("RateLimit-Reset", "17")
	e := newAPIError(429, h, []byte(`{}`), "GET", "/x", "rid")
	if e.RetryAfter != 17*time.Second || !IsRetryable(e) {
		t.Fatalf("429 reset not read: %+v", e)
	}
	e = newAPIError(400, http.Header{}, []byte(`{"errorCode":623,"errorMessage":"User is blocked from trading"}`), "POST", "/o", "rid")
	if e.Code != 623 || e.Message != "User is blocked from trading" || IsRetryable(e) {
		t.Fatalf("errorCode not parsed: %+v", e)
	}
	e = newAPIError(400, http.Header{}, []byte(`{"errorCode":"632","errorMessage":"pending close"}`), "POST", "/c", "rid")
	if e.Code != 632 {
		t.Fatalf("string errorCode: %+v", e)
	}
	if !strings.Contains(e.Error(), "code 632") || !strings.Contains(e.Error(), "POST /c") {
		t.Fatalf("Error() = %q", e.Error())
	}
	var te error = &TransportError{Method: "POST", Path: "/o", RequestID: "r", Err: errors.New("reset")}
	if !IsRetryable(te) {
		t.Fatal("transport errors are retryable (outcome unknown)")
	}
}

func TestLookupOrderByReference(t *testing.T) {
	ref := RequestIDFor("sma50-SPX500-2026-10-12-open")
	found := false
	c, calls := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		if !found {
			writeJSON(w, 404, []byte(`{"title":"Not Found","status":404,"detail":"Order not found"}`))
			return
		}
		writeJSON(w, 200, []byte(`{"orderId":13902598,"action":"open","transaction":"buy","type":"mkt",
			"status":{"id":3,"name":"Executed","errorCode":0,"errorMessage":""},
			"asset":{"symbol":"SPX500","instrumentId":27,"currency":"USD","settlementType":"cfd","leverage":1,"side":"long"},
			"orderCurrency":"usd","requestedAmount":1000,"totalCosts":0.72,
			"positionExecutions":[{"positionId":2001,"state":"open","investedAmountCurrency":1000,
				"openingData":{"orderId":13902598,"units":0.1279,"avgPrice":7818.5,"marketSpread":0.72,"fees":0}}]}`))
	})
	_, ok, err := c.LookupOrderByReference(context.Background(), ref)
	if err != nil || ok {
		t.Fatalf("404 must read as not found: ok %v err %v", ok, err)
	}
	if (*calls)[0].path != "/api/v2/trading/info/demo/orders:lookup" || (*calls)[0].rawQuery != "referenceId="+ref {
		t.Fatalf("request %s?%s", (*calls)[0].path, (*calls)[0].rawQuery)
	}
	found = true
	info, ok, err := c.LookupOrderByReference(context.Background(), ref)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if info.Outcome() != OutcomeExecuted || len(info.PositionIDs()) != 1 || info.PositionIDs()[0] != 2001 {
		t.Fatalf("info %+v", info)
	}
	if od := info.PositionExecutions[0].OpeningData; od == nil || od.AvgPrice != 7818.5 || od.Units != 0.1279 {
		t.Fatalf("opening data %+v", od)
	}
	if info.Asset.SettlementType != SettlementCFD || info.Asset.Leverage != 1 {
		t.Fatalf("asset %+v", info.Asset)
	}
}

func TestOutcomeClassification(t *testing.T) {
	cases := []struct {
		o    OrderInfo
		want Outcome
	}{
		{OrderInfo{Status: OrderStatus{Name: "Executed"}}, OutcomeExecuted},
		{OrderInfo{Status: OrderStatus{Name: "Pending"}}, OutcomePending},
		{OrderInfo{Status: OrderStatus{Name: "Waiting for market"}}, OutcomePending},
		{OrderInfo{Status: OrderStatus{Name: "Rejected", ErrorCode: 720}}, OutcomeRejected},
		{OrderInfo{Status: OrderStatus{Name: "Pending", ErrorCode: 623}}, OutcomeRejected},
		{OrderInfo{Status: OrderStatus{Name: "Cancelled"}}, OutcomeCancelled},
		{OrderInfo{Status: OrderStatus{Name: "Expired"}}, OutcomeCancelled},
		{OrderInfo{}, OutcomeUnknown},
		{OrderInfo{Status: OrderStatus{Name: "InProcess"}, PositionExecutions: []PositionExecution{{PositionID: 1}}}, OutcomeExecuted},
	}
	for _, tc := range cases {
		if got := tc.o.Outcome(); got != tc.want {
			t.Errorf("%+v: %s, want %s", tc.o.Status, got, tc.want)
		}
	}
}

func TestClosePositionV1Route(t *testing.T) {
	c, calls := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		writeJSON(w, 200, []byte(`{"orderForClose":{"positionID":2001,"instrumentID":27,"unitsToDeduct":0,"orderID":555,"orderType":17,"statusID":1}}`))
	})
	acc, err := c.ClosePosition(context.Background(), 2001, 27, nil, RequestIDFor("sma50-SPX500-2026-10-20-close"))
	if err != nil {
		t.Fatal(err)
	}
	got := (*calls)[0]
	if got.method != "POST" || got.path != "/api/v1/trading/execution/demo/market-close-orders/positions/2001" {
		t.Fatalf("%s %s", got.method, got.path)
	}
	if string(got.body) != `{"InstrumentID":27,"UnitsToDeduct":null}` {
		t.Fatalf("body %s (full close sends UnitsToDeduct null)", got.body)
	}
	if acc.OrderForClose.OrderID != 555 || acc.OrderForClose.PositionID != 2001 {
		t.Fatalf("accepted %+v", acc)
	}
	zero := 0.0
	if _, err := c.ClosePosition(context.Background(), 2001, 27, &zero, ""); err == nil {
		t.Fatal("zero partial close accepted")
	}
}

func TestRoutesDemoVsReal(t *testing.T) {
	demo := &Client{env: EnvDemo}
	real := &Client{env: EnvReal}
	cases := []struct{ name, demo, real string }{
		{"orders", demo.ordersPath(), real.ordersPath()},
		{"lookup", demo.orderLookupPath(), real.orderLookupPath()},
		{"costs", demo.costsPath(), real.costsPath()},
		{"eligibility", demo.eligibilityPath(), real.eligibilityPath()},
		{"close", demo.closePositionPath(7), real.closePositionPath(7)},
		{"close-info", demo.closeOrderInfoPath(9), real.closeOrderInfoPath(9)},
		{"pnl", demo.pnlPath(), real.pnlPath()},
		{"history", demo.historyPath(), real.historyPath()},
	}
	want := map[string][2]string{
		"orders":      {"/api/v2/trading/execution/demo/orders", "/api/v2/trading/execution/orders"},
		"lookup":      {"/api/v2/trading/info/demo/orders:lookup", "/api/v2/trading/info/orders:lookup"},
		"costs":       {"/api/v2/trading/info/demo/costs", "/api/v2/trading/info/costs"},
		"eligibility": {"/api/v2/trading/info/demo/eligibility", "/api/v2/trading/info/eligibility"},
		"close":       {"/api/v1/trading/execution/demo/market-close-orders/positions/7", "/api/v1/trading/execution/market-close-orders/positions/7"},
		"close-info":  {"/api/v1/trading/info/demo/close-orders/9", "/api/v1/trading/info/real/close-orders/9"},
		"pnl":         {"/api/v1/trading/info/demo/pnl", "/api/v1/trading/info/real/pnl"},
		"history":     {"/api/v1/trading/info/trade/demo/history", "/api/v1/trading/info/trade/history"},
	}
	for _, tc := range cases {
		if w := want[tc.name]; tc.demo != w[0] || tc.real != w[1] {
			t.Errorf("%s: demo %s real %s, want %v", tc.name, tc.demo, tc.real, w)
		}
	}
}

func TestTradingHistoryWindow(t *testing.T) {
	c, calls := newTestClient(t, EnvDemo, func(w http.ResponseWriter, r *http.Request, rec recorded) {
		writeJSON(w, 200, []byte(`[{"positionId":2001,"instrumentId":27,"isBuy":true,"leverage":1,"openRate":7818.5,"closeRate":7900,"netProfit":10.42,"fees":0.5,"units":0.1279,"investment":1000}]`))
	})
	rows, err := c.TradingHistory(context.Background(), time.Now().AddDate(0, -1, 0), 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].path != "/api/v1/trading/info/trade/demo/history" || !strings.Contains((*calls)[0].rawQuery, "pageSize=50") {
		t.Fatalf("request %s?%s", (*calls)[0].path, (*calls)[0].rawQuery)
	}
	if len(rows) != 1 || rows[0].NetProfit != 10.42 || rows[0].Fees != 0.5 {
		t.Fatalf("rows %+v", rows)
	}
	if _, err := c.TradingHistory(context.Background(), time.Now().AddDate(-1, 0, -1), 1, 50); err == nil {
		t.Fatal("window over one year accepted")
	}
}

func TestRequestIDs(t *testing.T) {
	// RFC 4122 appendix vector: UUIDv5(DNS namespace, "www.example.com").
	if got := uuidV5(mustUUID("6ba7b810-9dad-11d1-80b4-00c04fd430c8"), "www.example.com"); got != "2ed6657d-e927-568b-95e1-2665a8aea6a2" {
		t.Fatalf("uuidV5 = %s", got)
	}
	a, b := RequestIDFor("sma50-NSDQ100-2026-10-12-open"), RequestIDFor("sma50-NSDQ100-2026-10-12-open")
	if a != b || a == RequestIDFor("sma50-NSDQ100-2026-10-13-open") || a[14] != '5' {
		t.Fatalf("RequestIDFor not deterministic v5: %s %s", a, b)
	}
	if r1, r2 := NewRequestID(), NewRequestID(); r1 == r2 || r1[14] != '4' {
		t.Fatalf("NewRequestID %s %s", r1, r2)
	}
}

func TestLimiterPacing(t *testing.T) {
	now := time.Date(2026, 10, 12, 13, 35, 0, 0, time.UTC)
	l := newLimiter(3, func() time.Time { return now })
	for i := 0; i < 3; i++ {
		if d := l.reserve(); d != 0 {
			t.Fatalf("burst call %d waited %v", i, d)
		}
	}
	if d := l.reserve(); d != 20*time.Second {
		t.Fatalf("4th call waits %v, want 20s at 3/min", d)
	}
	if d := l.reserve(); d != 40*time.Second {
		t.Fatalf("5th call waits %v, want 40s (queued behind the 4th)", d)
	}
	now = now.Add(2 * time.Minute)
	if d := l.reserve(); d != 0 {
		t.Fatalf("after refill waited %v", d)
	}
}
