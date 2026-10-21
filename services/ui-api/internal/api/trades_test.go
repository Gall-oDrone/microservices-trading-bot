package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/audit"
	"bitso-trading-platform/ui-api/internal/etorohealth"
	"bitso-trading-platform/ui-api/internal/store"
)

// tradeNow is Monday 2026-10-19 11:00 New York: the cash session is open,
// no leg was sent today, the testdata ledger holds NSDQ100 flat (last close
// 29868.7, rule flat) and SPX500 long 0.140735 u (position 3616683295, last
// close 7738.57, rule long).
var tradeNow = time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC)

// fakeTradeBroker is the eToro adapter's contract without eToro: quotes per
// instrument, one account, positions that appear on open and vanish on
// close, Open idempotent per ClientRef. It is the only broker these tests
// ever construct; the real client is never built here.
type fakeTradeBroker struct {
	quotes    map[int64][2]float64 // id -> bid, ask
	cash      float64
	equity    float64
	positions []broker.Position
	byRef     map[string]broker.Order
	opens     []broker.OpenRequest
	closes    []broker.CloseRequest
	previews  int
	nextID    int
	openErr   error
	closeErr  error
	quoteErr  error
	costsErr  error
	// Protection (phase B): protects records SetProtection calls;
	// protectErr is returned instead; protectPending answers pending
	// without applying; limitsErr fails ProtectionLimits; noEditor hides
	// the ProtectionEditor interface (see protectBroker).
	protects       []broker.ProtectionRequest
	protectErr     error
	protectPending bool
	limitsErr      error
	limitsCalls    int
	// History (positions view): closed is what ClosedPositions answers;
	// histErr fails it; histCalls counts the reads; posErr fails Positions.
	closed    []broker.ClosedPosition
	histErr   error
	histCalls int
	posErr    error
}

func newFakeTradeBroker() *fakeTradeBroker {
	return &fakeTradeBroker{
		quotes: map[int64][2]float64{28: {29850.0, 29851.0}, 27: {7700.0, 7700.3}},
		cash:   97793.95, equity: 99949.9,
		positions: []broker.Position{{ID: "3616683295", Instrument: broker.Instrument{Venue: "etoro", ID: 27}, Side: broker.Long,
			Units: 0.140735, OpenRate: 7816.07, Amount: 1099.99, Leverage: 1, Exposure: 1083.66, Fees: 0.75}},
		byRef: map[string]broker.Order{}, nextID: 3616700000,
	}
}

func (f *fakeTradeBroker) Venue() string { return "etoro" }
func (f *fakeTradeBroker) Quote(ctx context.Context, in broker.Instrument) (broker.Quote, error) {
	if f.quoteErr != nil {
		return broker.Quote{}, f.quoteErr
	}
	q := f.quotes[in.ID]
	return broker.Quote{Instrument: in, Bid: q[0], Ask: q[1], At: tradeNow.Add(-2 * time.Second)}, nil
}
func (f *fakeTradeBroker) Open(ctx context.Context, req broker.OpenRequest) (broker.Order, error) {
	f.opens = append(f.opens, req)
	if o, ok := f.byRef[req.ClientRef]; ok {
		return o, nil
	}
	if f.openErr != nil {
		return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusUnknown}, f.openErr
	}
	f.nextID++
	id := strconv.Itoa(f.nextID)
	ask := f.quotes[req.Instrument.ID][1]
	units := req.Amount / ask
	o := broker.Order{OrderID: "o" + id, ClientRef: req.ClientRef, Status: broker.StatusExecuted, PositionIDs: []string{id},
		AvgPrice: ask + 0.5, Units: units, Amount: req.Amount, SpreadCost: 0.04, At: tradeNow.Add(3 * time.Second)}
	f.byRef[req.ClientRef] = o
	p := broker.Position{ID: id, Instrument: req.Instrument, Side: broker.Long, Units: units,
		OpenRate: o.AvgPrice, Amount: req.Amount, Leverage: 1, Exposure: req.Amount}
	if req.StopLoss > 0 {
		p.StopLoss, p.StopType = req.StopLoss, broker.StopFixed
	}
	p.TakeProfit = req.TakeProfit
	f.positions = append(f.positions, p)
	f.cash -= req.Amount
	return o, nil
}

// SetProtection implements broker.ProtectionEditor: applies the rates to
// the held position (or answers pending / an error when told to).
func (f *fakeTradeBroker) SetProtection(ctx context.Context, req broker.ProtectionRequest) (broker.ProtectionResult, error) {
	f.protects = append(f.protects, req)
	res := broker.ProtectionResult{PositionID: req.PositionID, ClientRef: req.ClientRef, Status: broker.StatusUnknown, OperationID: "op-" + req.ClientRef}
	if err := req.Validate(); err != nil {
		return res, err
	}
	if f.protectErr != nil {
		res.Status = broker.StatusRejected
		return res, f.protectErr
	}
	for i := range f.positions {
		if f.positions[i].ID != req.PositionID {
			continue
		}
		if f.protectPending {
			res.Status, res.Position = broker.StatusPending, f.positions[i]
			return res, nil
		}
		if req.StopLoss > 0 {
			f.positions[i].StopLoss, f.positions[i].StopType = req.StopLoss, broker.StopFixed
		}
		if req.TakeProfit > 0 {
			f.positions[i].TakeProfit = req.TakeProfit
		}
		if req.ClearTakeProfit {
			f.positions[i].TakeProfit = 0
		}
		res.Status, res.Position, res.At = broker.StatusExecuted, f.positions[i], tradeNow.Add(2*time.Second)
		return res, nil
	}
	res.Status = broker.StatusRejected
	return res, broker.ErrPositionNotOpen
}

// ProtectionLimits implements broker.ProtectionLimitsReader with the
// evidence of 2026-10-10: NSDQ100 needs 1 %, SPX500 0 %.
func (f *fakeTradeBroker) ProtectionLimits(ctx context.Context, in broker.Instrument, side broker.Side, leverage int) (broker.ProtectionLimits, error) {
	f.limitsCalls++
	if f.limitsErr != nil {
		return broker.ProtectionLimits{}, f.limitsErr
	}
	min := 0.0
	if in.ID == 28 {
		min = 0.01
	}
	return broker.ProtectionLimits{MinStopLossPct: min, MaxStopLossPct: 1, MaxTakeProfitPct: 10, AllowEdit: true, Tick: 0.01}, nil
}
func (f *fakeTradeBroker) Close(ctx context.Context, req broker.CloseRequest) (broker.Order, error) {
	f.closes = append(f.closes, req)
	if f.closeErr != nil {
		return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusUnknown}, f.closeErr
	}
	for i, p := range f.positions {
		if p.ID == req.PositionID {
			f.positions = append(f.positions[:i], f.positions[i+1:]...)
			bid := f.quotes[req.Instrument.ID][0]
			f.cash += p.Amount
			return broker.Order{OrderID: "c" + p.ID, ClientRef: req.ClientRef, Status: broker.StatusExecuted, PositionIDs: []string{p.ID},
				AvgPrice: bid - 0.2, Units: p.Units, Amount: p.Amount, At: tradeNow.Add(3 * time.Second)}, nil
		}
	}
	return broker.Order{ClientRef: req.ClientRef, Status: broker.StatusRejected, ErrorCode: 741}, broker.ErrPositionNotOpen
}
func (f *fakeTradeBroker) Positions(ctx context.Context) ([]broker.Position, error) {
	if f.posErr != nil {
		return nil, f.posErr
	}
	return append([]broker.Position(nil), f.positions...), nil
}

// ClosedPositions implements broker.HistoryReader.
func (f *fakeTradeBroker) ClosedPositions(ctx context.Context, since time.Time) ([]broker.ClosedPosition, error) {
	f.histCalls++
	if f.histErr != nil {
		return nil, f.histErr
	}
	var out []broker.ClosedPosition
	for _, c := range f.closed {
		if !c.ClosedAt.Before(since) {
			out = append(out, c)
		}
	}
	return out, nil
}
func (f *fakeTradeBroker) Account(ctx context.Context) (broker.Account, error) {
	return broker.Account{Currency: "USD", Cash: f.cash, Equity: f.equity}, nil
}
func (f *fakeTradeBroker) PreviewOpen(ctx context.Context, req broker.OpenRequest) (broker.Costs, error) {
	f.previews++
	if f.costsErr != nil {
		return broker.Costs{}, f.costsErr
	}
	return broker.Costs{Currency: "USD", Spread: 0.04, OvernightFee: 0.26}, nil
}

type tradeHarness struct {
	ts    *httptest.Server
	srv   *Server
	fb    *fakeTradeBroker
	dir   string // the copied eToro ledger dir
	now   time.Time
	token string
}

// tradeServer serves a copy of the eToro testdata ledger as "etoro-demo"
// with controls and assisted trading on (a fake broker), and a copy of the
// Bitso test ledger as "stage" (halt-all writes next to both), at now
// (movable: h.now).
func tradeServer(t *testing.T, now time.Time) *tradeHarness {
	t.Helper()
	h := &tradeHarness{fb: newFakeTradeBroker(), dir: copyEtoroLedger(t), now: now, token: testToken}
	tr, err := newEtoroTrading(h.fb)
	if err != nil {
		t.Fatal(err)
	}
	bitso := t.TempDir()
	src, err := os.ReadFile("testdata/ledger.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bitso, "ledger.jsonl"), string(src))
	stage := store.New(filepath.Join(bitso, "ledger.jsonl"), "testdata/candles")
	h.srv = &Server{
		Ledgers: []Ledger{
			{Name: "stage", Store: stage},
			{Name: "etoro-demo", Store: store.New(filepath.Join(h.dir, "ledger.jsonl"), filepath.Join(etoroTestdata, "candles")), Venue: VenueEtoro},
		},
		Store: stage, Policy: risk.DefaultPolicy(), PolicySrc: "test", Version: "test",
		Now: func() time.Time { return h.now }, Log: log.New(io.Discard, "", 0), OperatorToken: testToken, Trading: tr,
	}
	h.ts = httptest.NewServer(h.srv.Handler())
	t.Cleanup(h.ts.Close)
	return h
}

func prepareBody(book, side string, amount float64, positionID string) string {
	b, _ := json.Marshal(map[string]any{"book": book, "side": side, "amount_usd": amount, "position_id": positionID, "leverage": 1})
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	if amount == 0 {
		delete(m, "amount_usd")
	}
	if positionID == "" {
		delete(m, "position_id")
	}
	b, _ = json.Marshal(m)
	return string(b)
}

func (h *tradeHarness) prepare(t *testing.T, body string) (int, TradePreview, []byte) {
	t.Helper()
	code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/prepare?ledger=etoro-demo", token: h.token, origin: "http://127.0.0.1:5173", body: body})
	var pv TradePreview
	if code == http.StatusOK {
		if err := json.Unmarshal(b, &pv); err != nil {
			t.Fatalf("prepare body: %v: %s", err, b)
		}
	}
	return code, pv, b
}

func placeBody(token, confirm, reason, by string) string {
	b, _ := json.Marshal(TradePlaceRequest{Token: token, Confirm: confirm, Reason: reason, By: by})
	return string(b)
}

func (h *tradeHarness) place(t *testing.T, body string) (int, TradePlaceResponse, []byte) {
	t.Helper()
	code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/place?ledger=etoro-demo", token: h.token, origin: "http://127.0.0.1:5173", body: body})
	var resp TradePlaceResponse
	_ = json.Unmarshal(b, &resp)
	return code, resp, b
}

func (h *tradeHarness) audit(t *testing.T) []audit.Entry {
	t.Helper()
	return readAudit(t, h.dir) // newest first
}

func (h *tradeHarness) records(t *testing.T) []etoroledger.Record {
	t.Helper()
	recs, err := etoroledger.ReadFile(filepath.Join(h.dir, "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

func hasReason(rs []string, sub string) bool {
	for _, r := range rs {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

func TestTradingOffByDefault(t *testing.T) {
	h := tradeServer(t, tradeNow)
	h.srv.Trading = nil
	ci := get[ControlsInfo](t, h.ts, "/api/ui/controls?ledger=etoro-demo", 200)
	if ci.Trading.Enabled || !strings.Contains(ci.Trading.DisabledReason, "-etoro-trading") || ci.Trading.TokenTTLSeconds != 120 ||
		ci.Trading.MaxAmountUSD != 1500 || ci.Trading.DefaultAmount != 1100 {
		t.Fatalf("trading info %+v", ci.Trading)
	}
	code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/prepare?ledger=etoro-demo", token: testToken, body: prepareBody("nsdq100", "open", 1100, "")})
	if code != http.StatusForbidden || !strings.Contains(string(b), "-etoro-trading") {
		t.Fatalf("prepare with trading off: %d %s", code, b)
	}
	code, b = post(t, h.ts, ctl{path: "/api/ui/etoro/trades/place?ledger=etoro-demo", token: testToken, body: placeBody("x.y", "PLACE NSDQ100", "trading off test", "diego")})
	if code != http.StatusForbidden {
		t.Fatalf("place with trading off: %d %s", code, b)
	}
	if len(h.fb.opens) != 0 || len(h.fb.closes) != 0 || h.fb.previews != 0 {
		t.Fatal("the broker must not be touched with trading off")
	}
	// A Bitso ledger never trades here, and GET on the trade routes is 405.
	if ci := get[ControlsInfo](t, h.ts, "/api/ui/controls?ledger=stage", 200); ci.Trading.Enabled || !strings.Contains(ci.Trading.DisabledReason, "eToro ledgers only") {
		t.Fatalf("bitso trading info %+v", ci.Trading)
	}
	get[errorBody](t, h.ts, "/api/ui/etoro/trades/prepare", 405)
	// Without an operator token the controls are off, so is trading.
	h.srv.Trading, h.srv.OperatorToken = nil, ""
	if ci := get[ControlsInfo](t, h.ts, "/api/ui/controls?ledger=etoro-demo", 200); ci.Trading.Enabled || !strings.Contains(ci.Trading.DisabledReason, "-operator-token-file") {
		t.Fatalf("no token: %+v", ci.Trading)
	}
	// The constructor refuses anything but demo and anything but eToro.
	if _, err := NewEtoroTrading(etoro.Config{PublicKey: "k", PrivateKey: "s", Env: etoro.EnvReal}); err == nil || !strings.Contains(err.Error(), "demo") {
		t.Fatalf("real env accepted: %v", err)
	}
	if _, err := NewEtoroTrading(etoro.Config{PublicKey: "k", PrivateKey: "s"}); err == nil {
		t.Fatal("an unset env must be refused (it would default to demo silently)")
	}
	if _, err := newEtoroTrading(nil); err == nil {
		t.Fatal("nil broker accepted")
	}
}

func TestTradePrepareGatesAndValidation(t *testing.T) {
	h := tradeServer(t, tradeNow)
	good := prepareBody("nsdq100", "open", 1100, "")
	for _, c := range []struct {
		name    string
		ctl     ctl
		code    int
		outcome string
		msg     string
	}{
		{"no token", ctl{body: good}, 401, audit.Denied, "operator token"},
		{"foreign origin", ctl{token: testToken, origin: "https://evil.example", body: good}, 403, audit.Denied, "origin"},
		{"form post", ctl{token: testToken, ctype: "text/plain", body: good}, 415, audit.Refused, "application/json"},
		{"unknown field", ctl{token: testToken, body: `{"book":"nsdq100","side":"open","amount_usd":1100,"leverage":1,"stop_loss":1}`}, 400, audit.Refused, "unknown field"},
		{"unknown book", ctl{token: testToken, body: prepareBody("ger40", "open", 1100, "")}, 400, audit.Refused, "book"},
		{"leverage", ctl{token: testToken, body: `{"book":"nsdq100","side":"open","amount_usd":1100,"leverage":2}`}, 400, audit.Refused, "leverage"},
		{"amount low", ctl{token: testToken, body: prepareBody("nsdq100", "open", 500, "")}, 400, audit.Refused, "amount_usd"},
		{"amount high", ctl{token: testToken, body: prepareBody("nsdq100", "open", 1501, "")}, 400, audit.Refused, "amount_usd"},
		{"close needs position", ctl{token: testToken, body: `{"book":"spx500","side":"close","leverage":1}`}, 400, audit.Refused, "position_id"},
		{"close with amount", ctl{token: testToken, body: prepareBody("spx500", "close", 1100, "3616683295")}, 400, audit.Refused, "amount_usd"},
		{"side", ctl{token: testToken, body: `{"book":"nsdq100","side":"short","amount_usd":1100,"leverage":1}`}, 400, audit.Refused, "side"},
		{"bitso ledger", ctl{token: testToken, body: good}, 400, "", "venue bitso"},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.ctl.path = "/api/ui/etoro/trades/prepare?ledger=etoro-demo"
			if c.name == "bitso ledger" {
				c.ctl.path = "/api/ui/etoro/trades/prepare?ledger=stage"
			}
			code, b := post(t, h.ts, c.ctl)
			if code != c.code || !strings.Contains(string(b), c.msg) {
				t.Fatalf("%d %s, want %d containing %q", code, b, c.code, c.msg)
			}
			if c.outcome != "" {
				if e := h.audit(t); len(e) == 0 || e[0].Outcome != c.outcome || e[0].Action != "trade.prepare" {
					t.Fatalf("audit %+v, want %s", e, c.outcome)
				}
			}
		})
	}
	if len(h.fb.opens) != 0 || h.fb.previews != 0 {
		t.Fatal("a refused prepare must not reach the broker")
	}
}

// The checks, in the plan's order, each blocking with its own reason; a
// block still returns the preview but never a token.
func TestTradePrepareChecksInOrder(t *testing.T) {
	t.Run("halt file first", func(t *testing.T) {
		h := tradeServer(t, tradeNow)
		writeFile(t, filepath.Join(h.dir, "risk-state.json"), `{"halted":true,"reason":"eToro demo incident","by":"diego","at":"2026-10-19T14:00:00Z"}`)
		code, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
		if code != 200 || pv.Verdict != VerdictBlock || pv.Token != "" || pv.ExpiresAt != "" || len(pv.Reasons) == 0 ||
			!strings.HasPrefix(pv.Reasons[0], "halted: trading is halted: eToro demo incident") {
			t.Fatalf("%d %+v", code, pv)
		}
		// The preview is still there: quote, costs, account, the check as recorded.
		if pv.Quote == nil || pv.Quote.Ask != 29851 || pv.Costs == nil || pv.Account == nil || pv.Risk == nil || pv.Risk.Allowed || pv.Risk.Halt == nil ||
			pv.Confirmation != "PLACE NSDQ100" || !strings.HasPrefix(pv.ClientRef, "ui-nsdq100-2026-10-19-open-") {
			t.Fatalf("preview %+v", pv)
		}
		if e := h.audit(t); e[0].Outcome != audit.Refused || !strings.Contains(e[0].Error, "blocked: halted") || e[0].Ref != pv.ClientRef {
			t.Fatalf("audit %+v", e[0])
		}
		// An invalid halt file blocks too (the executor would refuse to run).
		writeFile(t, filepath.Join(h.dir, "risk-state.json"), `{"halted":true}`)
		if _, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, "")); pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, "halt file invalid") {
			t.Fatalf("invalid halt file %+v", pv.Reasons)
		}
	})
	t.Run("orders today", func(t *testing.T) {
		// Friday 10-16 13:00 NY: the 09:35 run closed NSDQ100, one leg today.
		h := tradeServer(t, etoroNow)
		_, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
		if pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, risk.RuleMaxOrdersPerDay) || pv.Risk.State.OrdersToday != 1 || pv.Token != "" {
			t.Fatalf("%+v", pv)
		}
		// A close is a reducing order: the per-day cap does not trap a position.
		if _, pv, _ := h.prepare(t, prepareBody("spx500", "close", 0, "3616683295")); pv.Verdict != VerdictAllow || pv.Confirmation != "CLOSE SPX500" {
			t.Fatalf("close on a day with the executor's leg on another book: %+v", pv.Reasons)
		}
	})
	t.Run("exposure cap", func(t *testing.T) {
		h := tradeServer(t, tradeNow)
		h.fb.equity = 20000 // 5 % = 1000 < 1083.66 + 1100
		_, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
		if pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, risk.RuleMaxExposurePctEquity) || pv.Token != "" {
			t.Fatalf("%+v", pv.Reasons)
		}
	})
	t.Run("market closed", func(t *testing.T) {
		h := tradeServer(t, time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)) // Saturday
		_, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
		if pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, risk.RuleMarketClosed) || !pv.Risk.State.MarketClosed {
			t.Fatalf("%+v", pv.Reasons)
		}
		// Closes need the session too (the policy requires it).
		if _, pv, _ := h.prepare(t, prepareBody("spx500", "close", 0, "3616683295")); pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, risk.RuleMarketClosed) {
			t.Fatalf("close on a saturday %+v", pv.Reasons)
		}
	})
	t.Run("price deviation", func(t *testing.T) {
		h := tradeServer(t, tradeNow)
		h.fb.quotes[28] = [2]float64{34000, 34001} // 14 % off the recorded close
		_, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
		if pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, risk.RulePriceDeviation) || pv.RefPrice != 29868.7 {
			t.Fatalf("%+v ref %v", pv.Reasons, pv.RefPrice)
		}
	})
	t.Run("cash short", func(t *testing.T) {
		h := tradeServer(t, tradeNow)
		h.fb.cash = 500
		_, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
		if pv.Verdict != VerdictBlock || len(pv.Reasons) != 1 || !strings.Contains(pv.Reasons[0], "available cash 500.00 USD is below the 1100 USD amount") || pv.Costs == nil {
			t.Fatalf("%+v", pv.Reasons)
		}
	})
	t.Run("cost preview failed", func(t *testing.T) {
		h := tradeServer(t, tradeNow)
		h.fb.costsErr = errors.New("costs route 503")
		if _, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, "")); pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, "cost preview failed") || pv.Costs != nil {
			t.Fatalf("%+v", pv.Reasons)
		}
	})
	t.Run("eligibility", func(t *testing.T) {
		h := tradeServer(t, tradeNow)
		// Closing a position the ledger does not hold.
		if _, pv, _ := h.prepare(t, prepareBody("spx500", "close", 0, "999")); pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, "position 999 is not the ledger's position on SPX500 (3616683295)") {
			t.Fatalf("unknown position %+v", pv.Reasons)
		}
		if _, pv, _ := h.prepare(t, prepareBody("nsdq100", "close", 0, "123")); pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, "holds no position on NSDQ100") {
			t.Fatalf("close flat %+v", pv.Reasons)
		}
		// Opening while the ledger is long.
		if _, pv, _ := h.prepare(t, prepareBody("spx500", "open", 1100, "")); pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, "already holds position 3616683295") {
			t.Fatalf("open while long %+v", pv.Reasons)
		}
		// The account holds something the ledger does not (resolve by hand).
		h.fb.positions = append(h.fb.positions, broker.Position{ID: "777", Instrument: broker.Instrument{ID: 28}, Side: broker.Long, Units: 0.01, Amount: 300})
		if _, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, "")); pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, "account holds position(s) 777 on NSDQ100") {
			t.Fatalf("untracked %+v", pv.Reasons)
		}
		// The ledger's position is gone from the account.
		h.fb.positions = nil
		if _, pv, _ := h.prepare(t, prepareBody("spx500", "close", 0, "3616683295")); pv.Verdict != VerdictBlock || !hasReason(pv.Reasons, "no longer holds position 3616683295") {
			t.Fatalf("gone %+v", pv.Reasons)
		}
	})
	t.Run("venue failure is a 502, audited failed", func(t *testing.T) {
		h := tradeServer(t, tradeNow)
		h.fb.quoteErr = errors.New("rates 500")
		code, _, b := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
		if code != http.StatusBadGateway || !strings.Contains(string(b), "quote: rates 500") {
			t.Fatalf("%d %s", code, b)
		}
		if e := h.audit(t); e[0].Outcome != audit.Failed {
			t.Fatalf("audit %+v", e[0])
		}
	})
}

func TestTradeTokenRoundTrip(t *testing.T) {
	h := tradeServer(t, tradeNow)
	code, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	if code != 200 || pv.Verdict != VerdictAllow || pv.Token == "" || pv.ExpiresInS != 120 || pv.ExpiresAt != "2026-10-19T15:02:00Z" {
		t.Fatalf("prepare %d %+v", code, pv)
	}
	if !hasReason(pv.Warnings, "the rule says flat") {
		t.Fatalf("an open against the rule must warn: %+v", pv.Warnings)
	}
	payload, sig, _ := strings.Cut(pv.Token, ".")
	flip := func(s string) string {
		if s[0] == 'A' {
			return "B" + s[1:]
		}
		return "A" + s[1:]
	}
	reason, by := "token round trip test", "diego"
	// Bad signature, tampered payload, garbage: 401, denied, nothing sent.
	for _, bad := range []string{payload + "." + flip(sig), flip(payload) + "." + sig, "not-a-token", "", payload} {
		code, _, b := h.place(t, placeBody(bad, pv.Confirmation, reason, by))
		if code != http.StatusUnauthorized || !strings.Contains(string(b), "trade token invalid") {
			t.Fatalf("bad token %q: %d %s", bad, code, b)
		}
		if e := h.audit(t); e[0].Outcome != audit.Denied || e[0].Action != "trade.place" {
			t.Fatalf("audit %+v", e[0])
		}
	}
	// Wrong confirm phrase, bad reason: 400, refused, token not consumed.
	if code, _, b := h.place(t, placeBody(pv.Token, "PLACE SPX500", reason, by)); code != 400 || !strings.Contains(string(b), "confirm: type the confirmation phrase") || !strings.Contains(string(b), "PLACE NSDQ100") {
		t.Fatalf("wrong confirm %d %s", code, b)
	}
	if code, _, b := h.place(t, placeBody(pv.Token, pv.Confirmation, "short", by)); code != 400 || !strings.Contains(string(b), "reason") {
		t.Fatalf("short reason %d %s", code, b)
	}
	// Another operator session (a different operator token): the signature
	// no longer verifies.
	other := "fedcba9876543210fedcba9876543210-other"
	h.srv.OperatorToken, h.token = other, other
	if code, _, b := h.place(t, placeBody(pv.Token, pv.Confirmation, reason, by)); code != http.StatusUnauthorized {
		t.Fatalf("other operator %d %s", code, b)
	}
	h.srv.OperatorToken, h.token = testToken, testToken
	// Expired: 410.
	h.now = tradeNow.Add(121 * time.Second)
	if code, _, b := h.place(t, placeBody(pv.Token, pv.Confirmation, reason, by)); code != http.StatusGone || !strings.Contains(string(b), "expired at 2026-10-19T15:02:00Z") {
		t.Fatalf("expired %d %s", code, b)
	}
	if e := h.audit(t); e[0].Outcome != audit.Refused || e[0].Ref != pv.ClientRef {
		t.Fatalf("audit %+v", e[0])
	}
	if len(h.fb.opens) != 0 {
		t.Fatal("nothing may be sent before a valid place")
	}
	// Still inside the TTL: placed once, the second use is a 409.
	h.now = tradeNow.Add(119 * time.Second)
	code, resp, b := h.place(t, placeBody(pv.Token, pv.Confirmation, reason, by))
	if code != 200 || resp.Status != "executed" {
		t.Fatalf("place %d %s", code, b)
	}
	if code, _, b := h.place(t, placeBody(pv.Token, pv.Confirmation, reason, by)); code != http.StatusConflict || !strings.Contains(string(b), "already used") {
		t.Fatalf("reuse %d %s", code, b)
	}
	if len(h.fb.opens) != 1 {
		t.Fatalf("opens %d", len(h.fb.opens))
	}
	// A token prepared for another ledger name is refused (the claims carry it).
	if _, err := h.srv.Trading.verify(pv.Token, testToken); err != nil {
		t.Fatal(err)
	}
}

// The happy path: intent, then the order, then the ledger line, then the
// audit; the line parses back with the executor's reader and the views
// follow it.
func TestTradePlaceOpenThenClose(t *testing.T) {
	h := tradeServer(t, tradeNow)
	before := len(h.records(t))
	_, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	if pv.Verdict != VerdictAllow || pv.RequestID != etoro.RequestIDFor(pv.ClientRef) || pv.Units != 1100/29851.0 || pv.RuleSignal != "flat" || pv.Position.State != "flat" {
		t.Fatalf("prepare %+v", pv)
	}
	code, resp, b := h.place(t, placeBody(pv.Token, "PLACE NSDQ100", "Re-entering NSDQ100 by hand after the incident review", "diego"))
	if code != 200 {
		t.Fatalf("place %d %s", code, b)
	}
	if resp.Status != "executed" || resp.OrderID == "" || len(resp.PositionIDs) != 1 || !resp.LedgerRecorded || resp.LedgerError != "" || resp.AuditError != "" ||
		resp.ClientRef != pv.ClientRef || resp.Units != pv.Units || resp.AmountUSD != 1100 || resp.AuditPath != filepath.Join(h.dir, audit.FileName) ||
		resp.PositionAfter.State != "long" || resp.PositionAfter.PositionID != resp.PositionIDs[0] || resp.Audit.Outcome != audit.Done {
		t.Fatalf("response %+v", resp)
	}
	// Fill vs mid: (29851.5 - 29850.5) / 29850.5; vs close: against 29868.7.
	if want := (29851.5 - 29850.5) / 29850.5 * 1e4; resp.FillVsMidBps < want-1e-9 || resp.FillVsMidBps > want+1e-9 {
		t.Fatalf("fill vs mid %v want %v", resp.FillVsMidBps, want)
	}
	if want := (29851.5 - 29868.7) / 29868.7 * 1e4; resp.FillVsCloseBps < want-1e-9 || resp.FillVsCloseBps > want+1e-9 {
		t.Fatalf("fill vs close %v want %v", resp.FillVsCloseBps, want)
	}
	// The broker got the deterministic reference and the intent's time.
	if o := h.fb.opens[0]; o.ClientRef != pv.ClientRef || o.Amount != 1100 || o.Leverage != 1 || o.Instrument.ID != 28 || !o.IntentAt.Equal(tradeNow) {
		t.Fatalf("open request %+v", o)
	}
	// Intent on disk, written before the order.
	intents := etoroledger.IntentDir(filepath.Join(h.dir, "ledger.jsonl"))
	in, ok, err := etoroledger.LoadIntent(intents, pv.ClientRef)
	if err != nil || !ok || in.RequestID != pv.RequestID || in.Kind != "open" || in.BarDate != "2026-10-19" || in.Amount != 1100 || !in.IntentAt.Equal(tradeNow) {
		t.Fatalf("intent %+v %v %v", in, ok, err)
	}
	// The ledger line, read back with the executor's reader.
	recs := h.records(t)
	if len(recs) != before+1 {
		t.Fatalf("records %d, want %d", len(recs), before+1)
	}
	var ui *etoroledger.Record
	for i := range recs {
		if recs[i].IsUI() {
			ui = &recs[i]
		}
	}
	if ui == nil {
		t.Fatal("no ui record")
	}
	if ui.Mode != "demo" || ui.Venue != "etoro" || ui.Book != "nsdq100" || ui.Instrument.ID != 28 || ui.Schema != etoroledger.Schema || ui.CodeVersion != "ui-api test" ||
		ui.Decision.BarDate != "2026-10-19" || ui.Decision.FillDate != "2026-10-19" || ui.Decision.Signal != "manual" || ui.Decision.PrevSignal != "flat" ||
		ui.Decision.Action != "buy" || ui.Decision.Close != 29868.7 || ui.Paper.Started || ui.Candles.Bars != 0 ||
		ui.Operator == nil || ui.Operator.By != "diego" || !strings.HasPrefix(ui.Operator.Reason, "Re-entering") {
		t.Fatalf("ui record %+v", ui)
	}
	d := ui.Demo
	if d.Action != "open" || d.Target != "long" || d.PositionBefore.State != "flat" || d.PositionAfter.PositionID != resp.PositionIDs[0] || d.PositionAfter.Units != pv.Units ||
		d.Account == nil || d.Account.Equity != 99949.9 || d.Quote == nil || d.Quote.Ask != 29851 || d.Risk == nil || !d.Risk.Allowed || d.Risk.Order.Side != "buy" ||
		d.Order == nil || d.Order.ClientRef != pv.ClientRef || d.Order.RequestID != pv.RequestID || d.Order.Status != "executed" || d.Order.FillVsMidBps != resp.FillVsMidBps ||
		!d.Order.IntentAt.Equal(tradeNow) || d.Order.Finished.Before(d.Order.Started) || len(d.Notes) != 1 {
		t.Fatalf("ui demo %+v order %+v", d, d.Order)
	}
	if p := etoroledger.LastDemoPosition(recs, "nsdq100"); p.State != "long" || p.PositionID != resp.PositionIDs[0] {
		t.Fatalf("last position %+v", p)
	}
	if n := etoroledger.LegsOn(recs, "nsdq100", "2026-10-19"); n != 1 {
		t.Fatalf("legs today %d", n)
	}
	// Audit: prepare done, place requested, place done, all tied by the ref.
	e := h.audit(t)
	var got []string
	for i := len(e) - 1; i >= 0; i-- {
		got = append(got, e[i].Action+":"+e[i].Outcome)
		if e[i].Ref != pv.ClientRef {
			t.Fatalf("audit ref %+v", e[i])
		}
	}
	if want := "trade.prepare:done trade.place:requested trade.place:done"; strings.Join(got, " ") != want {
		t.Fatalf("audit %v want %s", got, want)
	}
	if e[0].ID == "" || e[0].ID != e[1].ID || e[0].By != "diego" || !strings.Contains(e[0].Detail, "open nsdq100 1100 USD") || !strings.Contains(e[0].Detail, "-> executed") {
		t.Fatalf("done line %+v", e[0])
	}
	// The views follow: the position is the console's, the rule's decision
	// and paper are untouched, the equity series has no extra point, the run
	// coverage ignores the console line, orders today = 1.
	ft := get[EtoroForwardTestsResponse](t, h.ts, "/api/ui/etoro/forward-tests?ledger=etoro-demo", 200)
	n := ft.Books[0]
	if n.DemoPosition == nil || n.DemoPosition.State != "long" || n.DemoAction != "open" || n.Decision.BarDate != "2026-10-15" || n.Decision.Signal != "flat" ||
		n.LastOrder == nil || n.LastOrder.Source != "ui" || n.LastOrder.By != "diego" || n.LastOrder.ClientRef != pv.ClientRef || n.Run.LastFillDate != "2026-10-16" ||
		n.Account == nil || n.Account.Cash != 97793.95 {
		t.Fatalf("forward test %+v", n)
	}
	if l := get[EtoroLedgerResponse](t, h.ts, "/api/ui/etoro/forward-tests/nsdq100/ledger?ledger=etoro-demo", 200); len(l.Records) != 6 || len(l.Equity) != 5 || !l.Records[5].IsUI() {
		t.Fatalf("ledger view records %d equity %d", len(l.Records), len(l.Equity))
	}
	if r := get[EtoroRiskResponse](t, h.ts, "/api/ui/etoro/risk?ledger=etoro-demo", 200); r.Books[0].OrdersToday != 1 || r.Books[0].State != "long" || r.Books[0].LastClose != 29868.7 {
		t.Fatalf("risk %+v", r.Books[0])
	}
	// etorohealth: the intent is in the ledger; the console line is not run coverage.
	var intentsOK, coverage string
	for _, c := range etorohealth.Evaluate(etorohealth.Options{LedgerPath: filepath.Join(h.dir, "ledger.jsonl")}, h.now) {
		switch c.ID {
		case etorohealth.IDIntents:
			intentsOK = c.Message
		case etorohealth.IDCoverage:
			coverage = c.Message
		}
	}
	if intentsOK != "1 intents, all in the ledger" || !strings.Contains(coverage, "nsdq100 2026-10-19") {
		t.Fatalf("health intents %q coverage %q", intentsOK, coverage)
	}
	// The ledgers list counts it.
	if ls := get[LedgersResponse](t, h.ts, "/api/ui/ledgers", 200); ls.Ledgers[1].Records != before+1 || ls.Ledgers[1].LastBarDate != "2026-10-19" {
		t.Fatalf("ledgers %+v", ls.Ledgers[1])
	}

	// A second open the same day is blocked (one leg per day), a close of
	// the new position is allowed and goes through the same path (a few
	// minutes later, as it would be).
	h.now = tradeNow.Add(5 * time.Minute)
	if _, pv2, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, "")); pv2.Verdict != VerdictBlock || !hasReason(pv2.Reasons, risk.RuleMaxOrdersPerDay) || !hasReason(pv2.Reasons, "already holds position") {
		t.Fatalf("second open %+v", pv2.Reasons)
	}
	_, pc, _ := h.prepare(t, prepareBody("nsdq100", "close", 0, resp.PositionIDs[0]))
	if pc.Verdict != VerdictAllow || pc.Confirmation != "CLOSE NSDQ100" || pc.Units != pv.Units || pc.Costs != nil || len(pc.Warnings) != 0 {
		t.Fatalf("prepare close %+v", pc)
	}
	code, rc, b := h.place(t, placeBody(pc.Token, "CLOSE NSDQ100", "Flat again before the close", "diego"))
	if code != 200 || rc.Status != "executed" || !rc.LedgerRecorded || rc.PositionAfter.State != "flat" || rc.Side != "close" {
		t.Fatalf("place close %d %s", code, b)
	}
	if c := h.fb.closes[0]; c.PositionID != resp.PositionIDs[0] || c.ClientRef != pc.ClientRef || c.Units != 0 {
		t.Fatalf("close request %+v (the whole position is closed)", c)
	}
	recs = h.records(t)
	if p := etoroledger.LastDemoPosition(recs, "nsdq100"); p.State != "flat" {
		t.Fatalf("after close %+v", p)
	}
	if n := etoroledger.LegsOn(recs, "nsdq100", "2026-10-19"); n != 2 {
		t.Fatalf("legs today %d", n)
	}
	if len(h.fb.positions) != 1 || h.fb.positions[0].ID != "3616683295" {
		t.Fatalf("account positions %+v", h.fb.positions)
	}
}

// A broker failure after the intent: the intent stays, the ledger is
// untouched, the audit says failed, and the response is a 502 naming it.
func TestTradePlaceBrokerFailure(t *testing.T) {
	h := tradeServer(t, tradeNow)
	before := h.records(t)
	_, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	h.fb.openErr = fmt.Errorf("%w: context deadline exceeded", broker.ErrOutcomeUnknown)
	code, resp, b := h.place(t, placeBody(pv.Token, pv.Confirmation, "broker failure drill", "diego"))
	if code != http.StatusBadGateway || resp.Status != "unknown" || resp.LedgerRecorded || !strings.Contains(resp.Error, "outcome unknown") ||
		!strings.Contains(resp.Error, "intent "+pv.ClientRef+" stays on disk") || resp.Audit.Outcome != audit.Failed {
		t.Fatalf("%d %s", code, b)
	}
	if _, ok, _ := etoroledger.LoadIntent(etoroledger.IntentDir(filepath.Join(h.dir, "ledger.jsonl")), pv.ClientRef); !ok {
		t.Fatal("the intent must stay on disk")
	}
	if after := h.records(t); len(after) != len(before) {
		t.Fatalf("ledger changed: %d -> %d", len(before), len(after))
	}
	e := h.audit(t)
	if e[0].Outcome != audit.Failed || e[1].Outcome != audit.Requested || e[0].ID != e[1].ID || e[0].Ref != pv.ClientRef {
		t.Fatalf("audit %+v %+v", e[0], e[1])
	}
	// reconcile / etorohealth would report it once the grace passes.
	var msg string
	for _, c := range etorohealth.Evaluate(etorohealth.Options{LedgerPath: filepath.Join(h.dir, "ledger.jsonl")}, h.now.Add(11*time.Minute)) {
		if c.ID == etorohealth.IDIntents {
			msg = c.Status + ": " + c.Message
		}
	}
	if !strings.HasPrefix(msg, "fail: "+pv.ClientRef) {
		t.Fatalf("health %q", msg)
	}
	// The token was consumed: a retry needs a new prepare (and a new ref).
	if code, _, _ := h.place(t, placeBody(pv.Token, pv.Confirmation, "broker failure drill", "diego")); code != http.StatusConflict {
		t.Fatalf("reuse after failure %d", code)
	}
	// A definite rejection is a 502 too, nothing recorded.
	h.fb.openErr = errors.New("eToro 400: insufficient funds")
	_, pv2, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	if code, resp, _ := h.place(t, placeBody(pv2.Token, pv2.Confirmation, "broker failure drill", "diego")); code != http.StatusBadGateway || resp.LedgerRecorded {
		t.Fatalf("rejected %d %+v", code, resp)
	}
	if after := h.records(t); len(after) != len(before) {
		t.Fatal("a rejected order must not be recorded")
	}
}

// A halt between prepare and place: the re-check refuses with 422 and
// nothing is sent; the same for a session that closed meanwhile.
func TestTradePlaceRecheckHaltAndSession(t *testing.T) {
	h := tradeServer(t, tradeNow)
	_, pv, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	code, b := post(t, h.ts, ctl{path: "/api/ui/risk/halt?ledger=etoro-demo", token: testToken, body: body("eToro demo incident", "diego", "etoro-demo")})
	if code != 200 {
		t.Fatalf("halt %d %s", code, b)
	}
	code, _, b = h.place(t, placeBody(pv.Token, pv.Confirmation, "halt recheck drill", "diego"))
	if code != http.StatusUnprocessableEntity || !strings.Contains(string(b), "trading is halted: eToro demo incident (halt file, by diego") || !strings.Contains(string(b), "nothing sent") {
		t.Fatalf("place after halt %d %s", code, b)
	}
	if len(h.fb.opens) != 0 || len(h.records(t)) != 10 {
		t.Fatal("nothing may be sent or recorded")
	}
	if _, ok, _ := etoroledger.LoadIntent(etoroledger.IntentDir(filepath.Join(h.dir, "ledger.jsonl")), pv.ClientRef); ok {
		t.Fatal("no intent before the re-check passes")
	}
	// The token was consumed by the attempt: prepare again after resume.
	post(t, h.ts, ctl{path: "/api/ui/risk/resume?ledger=etoro-demo", token: testToken, body: body("incident resolved", "diego", "etoro-demo")})
	if code, _, _ := h.place(t, placeBody(pv.Token, pv.Confirmation, "halt recheck drill", "diego")); code != http.StatusConflict {
		t.Fatalf("token after a refused place %d", code)
	}
	// The session closes before the place (prepared 15:59, placed 16:01 New York).
	h.now = time.Date(2026, 10, 19, 19, 59, 0, 0, time.UTC)
	_, pv, _ = h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	if pv.Verdict != VerdictAllow {
		t.Fatalf("15:59 prepare %+v", pv.Reasons)
	}
	h.now = time.Date(2026, 10, 19, 20, 0, 30, 0, time.UTC)
	if code, _, b := h.place(t, placeBody(pv.Token, pv.Confirmation, "session recheck drill", "diego")); code != http.StatusUnprocessableEntity || !strings.Contains(string(b), "cash session is closed") {
		t.Fatalf("place after close %d %s", code, b)
	}
	// The ledger changed since the preview (the executor ran): 422.
	h.now = tradeNow
	_, pv, _ = h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	led, _ := etoroledger.Open(filepath.Join(h.dir, "ledger.jsonl"))
	if err := led.Append(etoroledger.Record{Mode: "demo", Venue: "etoro", Book: "nsdq100", RecordedAt: "2026-10-19T15:00:30Z",
		Decision: etoroledger.Decision{BarDate: "2026-10-16", FillDate: "2026-10-19", Signal: "long"},
		Demo:     &etoroledger.Demo{Env: "demo", Action: "open", PositionAfter: etoroledger.Position{State: "long", PositionID: "5"}}}); err != nil {
		t.Fatal(err)
	}
	if code, _, b := h.place(t, placeBody(pv.Token, pv.Confirmation, "ledger changed drill", "diego")); code != http.StatusUnprocessableEntity || !strings.Contains(string(b), "ledger changed since the preview") {
		t.Fatalf("ledger changed %d %s", code, b)
	}
	// The executor holding the ledger lock: 409, nothing sent.
	_, pv, _ = h.prepare(t, prepareBody("spx500", "close", 0, "3616683295"))
	lock, err := lockLedger(filepath.Join(h.dir, "ledger.jsonl.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if code, _, b := h.place(t, placeBody(pv.Token, pv.Confirmation, "lock drill, executor running", "diego")); code != http.StatusConflict || !strings.Contains(string(b), "ledger lock") {
		t.Fatalf("locked %d %s", code, b)
	}
	lock.Close()
	if len(h.fb.opens) != 0 || len(h.fb.closes) != 0 {
		t.Fatal("nothing may be sent")
	}
}

// TestKillSwitchBlocksAssistedTrades is the assisted-path leg of the
// kill-switch drill (scripts/kill-switch-drill.sh cannot run it: the drill
// starts a real ui-api, which must never run with -etoro-trading outside an
// operator's hands). HALT ALL from the Strategies page: prepare answers a
// block verdict with no token, and a token prepared before the halt is
// refused at place with 422; nothing reaches the broker.
func TestKillSwitchBlocksAssistedTrades(t *testing.T) {
	h := tradeServer(t, tradeNow)
	_, pv, _ := h.prepare(t, prepareBody("spx500", "close", 0, "3616683295"))
	if pv.Verdict != VerdictAllow {
		t.Fatalf("before the halt: %+v", pv.Reasons)
	}
	code, b := post(t, h.ts, ctl{path: "/api/ui/risk/halt-all", token: testToken, body: body("Kill switch drill", "drill", HaltAllConfirm)})
	if code != 200 {
		t.Fatalf("halt-all %d %s", code, b)
	}
	if code, _, b := h.place(t, placeBody(pv.Token, pv.Confirmation, "Kill switch drill: place after halt", "drill")); code != http.StatusUnprocessableEntity || !strings.Contains(string(b), "trading is halted: Kill switch drill") {
		t.Fatalf("place after halt-all %d %s", code, b)
	}
	for _, body := range []string{prepareBody("spx500", "close", 0, "3616683295"), prepareBody("nsdq100", "open", 1100, "")} {
		if code, pv, _ := h.prepare(t, body); code != 200 || pv.Verdict != VerdictBlock || pv.Token != "" || !strings.HasPrefix(pv.Reasons[0], "halted: trading is halted: Kill switch drill (halt file, by drill") {
			t.Fatalf("prepare after halt-all %d %+v", code, pv.Reasons)
		}
	}
	if len(h.fb.opens) != 0 || len(h.fb.closes) != 0 {
		t.Fatal("the kill switch must keep every order from the broker")
	}
	if ci := get[ControlsInfo](t, h.ts, "/api/ui/controls?ledger=etoro-demo", 200); !ci.HaltFile.Halted || !ci.Trading.Enabled {
		t.Fatalf("controls %+v", ci)
	}
	// Resume: trading again.
	post(t, h.ts, ctl{path: "/api/ui/risk/resume?ledger=etoro-demo", token: testToken, body: body("Kill switch drill over", "drill", "etoro-demo")})
	if _, pv, _ := h.prepare(t, prepareBody("spx500", "close", 0, "3616683295")); pv.Verdict != VerdictAllow {
		t.Fatalf("after resume %+v", pv.Reasons)
	}
	_ = os.Remove(filepath.Join(h.dir, "risk-state.json"))
}

func cancelBody(token, reason, by string) string {
	b, _ := json.Marshal(TradeCancelRequest{Token: token, Reason: reason, By: by})
	return string(b)
}

func (h *tradeHarness) cancel(t *testing.T, body string) (int, TradeCancelResponse, []byte) {
	t.Helper()
	code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/cancel?ledger=etoro-demo", token: h.token, origin: "http://127.0.0.1:5173", body: body})
	var resp TradeCancelResponse
	_ = json.Unmarshal(b, &resp)
	return code, resp, b
}

// TestTradeCancel: a prepared trade can be discarded before its token
// expires. The token is burned (place refuses it afterwards), nothing
// reaches the broker or the ledger, and the decision is audited as
// trade.cancel with the preview's client_ref.
func TestTradeCancel(t *testing.T) {
	h := tradeServer(t, tradeNow)
	before := len(h.records(t))
	_, pv, _ := h.prepare(t, prepareBody("spx500", "close", 0, "3616683295"))
	if pv.Verdict != VerdictAllow || pv.Token == "" {
		t.Fatalf("prepare %+v", pv)
	}
	// A bad token is denied like at place; nothing is burned.
	if code, _, b := h.cancel(t, cancelBody("not-a-token", "", "")); code != http.StatusUnauthorized || !strings.Contains(string(b), "trade token invalid") {
		t.Fatalf("bad token %d %s", code, b)
	}
	if e := h.audit(t); e[0].Action != "trade.cancel" || e[0].Outcome != audit.Denied {
		t.Fatalf("audit %+v", e[0])
	}
	// A multi-line reason is refused (the audit log is one line per entry).
	if code, _, b := h.cancel(t, cancelBody(pv.Token, "two\nlines", "diego")); code != http.StatusBadRequest || !strings.Contains(string(b), "reason: one line") {
		t.Fatalf("bad reason %d %s", code, b)
	}
	// The cancel: 200, status cancelled, audited done with the ref.
	code, resp, b := h.cancel(t, cancelBody(pv.Token, "Changed my mind: the spread widened", "diego"))
	if code != 200 || resp.Status != CancelCancelled || resp.ClientRef != pv.ClientRef || resp.RequestID != pv.RequestID ||
		resp.Book != "spx500" || resp.Side != TradeClose || resp.ExpiresAt != pv.ExpiresAt || resp.AuditError != "" {
		t.Fatalf("cancel %d %s", code, b)
	}
	e := h.audit(t)
	if e[0].Action != "trade.cancel" || e[0].Outcome != audit.Done || e[0].Ref != pv.ClientRef || e[0].By != "diego" ||
		e[0].Reason != "Changed my mind: the spread widened" || !strings.Contains(e[0].Detail, "close spx500 position 3616683295") ||
		!strings.Contains(e[0].Detail, "token burned, nothing sent") {
		t.Fatalf("audit %+v", e[0])
	}
	if resp.Audit.ID != e[0].ID || resp.AuditPath != filepath.Join(h.dir, audit.FileName) {
		t.Fatalf("response audit %+v vs %+v", resp.Audit, e[0])
	}
	// The token is gone: place is a 409, a second cancel too.
	if code, _, b := h.place(t, placeBody(pv.Token, pv.Confirmation, "placing a cancelled preview", "diego")); code != http.StatusConflict || !strings.Contains(string(b), "already used") {
		t.Fatalf("place after cancel %d %s", code, b)
	}
	if code, _, b := h.cancel(t, cancelBody(pv.Token, "", "")); code != http.StatusConflict || !strings.Contains(string(b), "placed or cancelled") {
		t.Fatalf("second cancel %d %s", code, b)
	}
	if len(h.fb.opens) != 0 || len(h.fb.closes) != 0 || len(h.records(t)) != before {
		t.Fatal("a cancel must not reach the broker or the ledger")
	}
	// A placed token cannot be cancelled either (the place consumed it).
	_, pv2, _ := h.prepare(t, prepareBody("spx500", "close", 0, "3616683295"))
	if code, _, b := h.place(t, placeBody(pv2.Token, pv2.Confirmation, "closing SPX500 by hand", "diego")); code != 200 {
		t.Fatalf("place %d %s", code, b)
	}
	if code, _, _ := h.cancel(t, cancelBody(pv2.Token, "", "")); code != http.StatusConflict {
		t.Fatalf("cancel after place %d", code)
	}
	// An expired token: nothing to burn, still 200 and audited (status expired).
	_, pv3, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	if pv3.Verdict != VerdictAllow {
		t.Fatalf("prepare open %+v", pv3.Reasons)
	}
	h.now = tradeNow.Add(121 * time.Second)
	if code, resp, b := h.cancel(t, cancelBody(pv3.Token, "", "")); code != 200 || resp.Status != CancelExpired || !strings.Contains(resp.Audit.Detail, "had expired") {
		t.Fatalf("expired cancel %d %s", code, b)
	}
	// Reason and by are optional; by, when given, is validated.
	h.now = tradeNow.Add(200 * time.Second)
	_, pv4, _ := h.prepare(t, prepareBody("nsdq100", "open", 1100, ""))
	if code, _, b := h.cancel(t, cancelBody(pv4.Token, "", "bad\tby")); code != http.StatusBadRequest || !strings.Contains(string(b), "by:") {
		t.Fatalf("bad by %d %s", code, b)
	}
	if code, resp, _ := h.cancel(t, cancelBody(pv4.Token, "", "")); code != 200 || resp.Status != CancelCancelled || resp.Audit.By != "" {
		t.Fatalf("bare cancel %d %+v", code, resp)
	}
	// Gates: trading off is a 403 before anything else; a Bitso ledger is not an eToro ledger.
	h.srv.Trading = nil
	if code, _, _ := h.cancel(t, cancelBody("x.y", "", "")); code != http.StatusForbidden {
		t.Fatalf("trading off %d", code)
	}
	if code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/cancel?ledger=stage", token: testToken, origin: "http://127.0.0.1:5173", body: cancelBody("x.y", "", "")}); code != http.StatusBadRequest || !strings.Contains(string(b), "venue bitso") {
		t.Fatalf("bitso ledger %d %s", code, b)
	}
	get[errorBody](t, h.ts, "/api/ui/etoro/trades/cancel", 405)
}
