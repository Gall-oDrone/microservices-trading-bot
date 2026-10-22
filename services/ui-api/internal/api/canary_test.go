package api

// Tests of the real-account canary in ui-api (plan C5): the constructors'
// refusals, the form bounds and environment in /controls and /ledgers, a
// full prepare -> place -> close under the canary policy against the fake
// broker (REAL phrases, x20, ui-real- refs, mode real), the rehearsal on
// demo, and the demo tests' expectations untouched (they live in
// trades_test.go and run unchanged).

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/broker"
	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/store"
)

const canaryTestKey = "canarytest-0000-4000-8000-000000000000"

func testAck(t *testing.T, ledger string) etoro.RealAck {
	t.Helper()
	p := filepath.Join(t.TempDir(), "REAL-ACCOUNT-ACK")
	body := strings.Join([]string{etoro.RealAckSentence, "key_prefix: canarytest-0", "ledger: " + ledger,
		"policy: " + risk.EtoroRealCanaryVersion, "start: 2026-10-01", "end: 2026-12-31", "name: Operator"}, "\n") + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ack, err := etoro.LoadRealAck(p)
	if err != nil {
		t.Fatal(err)
	}
	return ack
}

func goodRealCheck(ledger string) etoro.RealAckCheck {
	return etoro.RealAckCheck{Flag: true, LedgerPath: ledger, OtherLedgers: []string{"/srv/etoro-daily-data/demo/ledger.jsonl"},
		TokenFile: "/home/op/.config/mtb/operator-token-real-canary", DemoTokenFile: "/home/op/.config/mtb/operator-token",
		TokenSHA256: "aa", DemoTokenSHA256: "bb", Now: time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)}
}

func TestRealCanaryConstructorsRefuse(t *testing.T) {
	ledger := "/srv/etoro-daily-data/real-canary/ledger.jsonl"
	ack := testAck(t, ledger)
	realCfg := etoro.Config{PublicKey: canaryTestKey, PrivateKey: "x", Env: etoro.EnvReal}
	demoCfg := etoro.Config{PublicKey: canaryTestKey, PrivateKey: "x", Env: etoro.EnvDemo}

	// The demo constructors never accept a real key.
	if _, err := NewEtoroTrading(realCfg); err == nil || !strings.Contains(err.Error(), "refused in code") {
		t.Fatalf("NewEtoroTrading(real): %v", err)
	}
	if _, err := NewEtoroCanaryRehearsal(realCfg); err == nil || !strings.Contains(err.Error(), "must be demo") {
		t.Fatalf("NewEtoroCanaryRehearsal(real): %v", err)
	}
	// The real constructor refuses a demo key, and each missing condition.
	if _, err := NewEtoroRealCanary(demoCfg, ack, goodRealCheck(ledger)); !errors.Is(err, etoro.ErrRealAckEnv) {
		t.Fatalf("NewEtoroRealCanary(demo): %v", err)
	}
	cases := map[string]struct {
		mut  func(*etoro.RealAckCheck)
		want error
	}{
		"flag":   {func(c *etoro.RealAckCheck) { c.Flag = false }, etoro.ErrRealAckFlag},
		"ledger": {func(c *etoro.RealAckCheck) { c.LedgerPath = "/srv/etoro-daily-data/demo/ledger.jsonl" }, etoro.ErrRealAckLedger},
		"window": {func(c *etoro.RealAckCheck) { c.Now = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) }, etoro.ErrRealAckWindow},
		"token":  {func(c *etoro.RealAckCheck) { c.TokenFile = c.DemoTokenFile }, etoro.ErrRealAckToken},
	}
	for name, tc := range cases {
		chk := goodRealCheck(ledger)
		tc.mut(&chk)
		if _, err := NewEtoroRealCanary(realCfg, ack, chk); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A key whose prefix is not the ack's.
	other := realCfg
	other.PublicKey = "otherkey-0000-4000-8000-000000000000"
	if _, err := NewEtoroRealCanary(other, ack, goodRealCheck(ledger)); !errors.Is(err, etoro.ErrRealAckKeyPrefix) {
		t.Fatalf("key prefix: %v", err)
	}
	// Everything right: a real broker under the canary policy (never called).
	tr, err := NewEtoroRealCanary(realCfg, ack, goodRealCheck(ledger))
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Real() || tr.Environment() != "real" || tr.Policy.Version != risk.EtoroRealCanaryVersion || tr.Ack == nil || tr.Ack.Name != "Operator" || tr.Rehearsal {
		t.Fatalf("trading %+v", tr)
	}
	// The wrapper refuses a real broker without the ack or with a demo policy.
	if _, err := newEtoroTradingWith(newFakeTradeBroker(), risk.EtoroDemoProtectedPolicy(), etoro.EnvReal, false, &ack); err == nil {
		t.Fatal("real with the demo policy accepted")
	}
	if _, err := newEtoroTradingWith(newFakeTradeBroker(), risk.EtoroRealCanaryPolicy(), etoro.EnvReal, false, nil); err == nil {
		t.Fatal("real without the ack accepted")
	}
	// The demo wrapper is what it was.
	d, err := newEtoroTrading(newFakeTradeBroker())
	if err != nil || d.Real() || d.Environment() != "demo" || d.policy().Version != risk.EtoroDemoProtectedPolicy().Version {
		t.Fatalf("demo trading %+v %v", d, err)
	}
}

// canaryServer is tradeServer with the canary policy on a ledger named
// etoro-real-canary under a real-canary/ directory (real) or on a
// rehearsal ledger (demo), the fake broker's account emptied unless told
// otherwise, and the testdata candles for an ATR.
func canaryServer(t *testing.T, real bool, keepPositions bool) *tradeHarness {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "real-canary")
	name := RealCanaryLedgerName
	if !real {
		dir = filepath.Join(root, "rehearsal-canary")
		name = "etoro-canary-rehearsal"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	h := &tradeHarness{fb: newFakeTradeBroker(), dir: dir, now: tradeNow, token: testToken}
	if !keepPositions {
		h.fb.positions = nil
	}
	var tr *EtoroTrading
	var err error
	if real {
		ack := testAck(t, filepath.Join(dir, "ledger.jsonl"))
		tr, err = newEtoroTradingWith(h.fb, risk.EtoroRealCanaryPolicy(), etoro.EnvReal, false, &ack)
	} else {
		tr, err = newEtoroTradingWith(h.fb, risk.EtoroRealCanaryRehearsalPolicy(), etoro.EnvDemo, true, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	l := Ledger{Name: name, Store: store.New(filepath.Join(dir, "ledger.jsonl"), filepath.Join(etoroTestdata, "candles")), Venue: VenueEtoro}
	if real {
		l.Environment = "real"
	}
	bitso := t.TempDir()
	src, err := os.ReadFile("testdata/ledger.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bitso, "ledger.jsonl"), string(src))
	stage := store.New(filepath.Join(bitso, "ledger.jsonl"), "testdata/candles")
	h.srv = &Server{Ledgers: []Ledger{{Name: "stage", Store: stage}, l}, Store: stage, Policy: risk.DefaultPolicy(), PolicySrc: "test", Version: "test",
		Now: func() time.Time { return h.now }, Log: log.New(io.Discard, "", 0), OperatorToken: testToken, Trading: tr}
	h.ts = httptest.NewServer(h.srv.Handler())
	t.Cleanup(h.ts.Close)
	return h
}

func (h *tradeHarness) ledgerName() string { return h.srv.Ledgers[1].Name }

func (h *tradeHarness) prepareOn(t *testing.T, body string) (int, TradePreview, []byte) {
	t.Helper()
	code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/prepare?ledger=" + h.ledgerName(), token: h.token, origin: "http://127.0.0.1:5174", body: body})
	var pv TradePreview
	if code == http.StatusOK {
		if err := json.Unmarshal(b, &pv); err != nil {
			t.Fatalf("prepare body: %v: %s", err, b)
		}
	}
	return code, pv, b
}

func (h *tradeHarness) placeOn(t *testing.T, body string) (int, TradePlaceResponse, []byte) {
	t.Helper()
	code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/place?ledger=" + h.ledgerName(), token: h.token, origin: "http://127.0.0.1:5174", body: body})
	var resp TradePlaceResponse
	_ = json.Unmarshal(b, &resp)
	return code, resp, b
}

func canaryBody(book, side string, amount float64, positionID string, lev int) string {
	m := map[string]any{"book": book, "side": side, "leverage": lev}
	if amount != 0 {
		m["amount_usd"] = amount
	}
	if positionID != "" {
		m["position_id"] = positionID
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestRealCanaryControlsAndLedgersReportTheEnvironment(t *testing.T) {
	h := canaryServer(t, true, false)
	ci := get[ControlsInfo](t, h.ts, "/api/ui/controls?ledger=etoro-real-canary", 200)
	if ci.Environment != "real" || ci.Venue != "etoro" || !ci.Trading.Enabled || ci.Trading.Environment != "real" {
		t.Fatalf("controls %+v", ci)
	}
	tr := ci.Trading
	if tr.MinAmountUSD != 50 || tr.MaxAmountUSD != 60 || tr.DefaultAmount != 50 || len(tr.LeverageOptions) != 1 || tr.LeverageOptions[0] != 20 ||
		tr.DefaultLeverage != 20 || tr.PolicyVersion != risk.EtoroRealCanaryVersion || len(tr.Books) != 1 || tr.Books[0] != "nsdq100" || tr.Rehearsal {
		t.Fatalf("trading info %+v", tr)
	}
	ls := get[LedgersResponse](t, h.ts, "/api/ui/ledgers", 200)
	if ls.Ledgers[0].Environment != "" || ls.Ledgers[1].Name != "etoro-real-canary" || ls.Ledgers[1].Environment != "real" {
		t.Fatalf("ledgers %+v", ls.Ledgers)
	}
	// The Risk page shows the canary policy on a real ledger.
	rr := get[RiskResponse](t, h.ts, "/api/ui/etoro/risk?ledger=etoro-real-canary", 200)
	if rr.Policy.Version != risk.EtoroRealCanaryVersion || !strings.Contains(rr.PolicySrc, "REAL ACCOUNT") {
		t.Fatalf("risk policy %s %s", rr.Policy.Version, rr.PolicySrc)
	}
	// Demo tradeServer: unchanged x1 bounds and environment demo.
	d := tradeServer(t, tradeNow)
	dc := get[ControlsInfo](t, d.ts, "/api/ui/controls?ledger=etoro-demo", 200)
	if dc.Environment != "demo" || dc.Trading.Environment != "demo" || dc.Trading.MinAmountUSD != 1000 || dc.Trading.MaxAmountUSD != 1500 ||
		dc.Trading.DefaultAmount != 1100 || len(dc.Trading.LeverageOptions) != 1 || dc.Trading.LeverageOptions[0] != 1 || dc.Trading.DefaultLeverage != 1 ||
		dc.Trading.PolicyVersion != risk.EtoroDemoProtectedPolicy().Version {
		t.Fatalf("demo trading info %+v", dc.Trading)
	}
}

func TestRealCanaryPrepareValidatesBoundsAndLeverage(t *testing.T) {
	h := canaryServer(t, true, false)
	cases := []struct {
		body string
		want string
	}{
		{canaryBody("nsdq100", "open", 50, "", 1), "leverage: must be 20"},
		{canaryBody("nsdq100", "open", 50, "", 10), "leverage: must be 20"},
		{canaryBody("nsdq100", "open", 1100, "", 20), "between 50 and 60 USD"},
		{canaryBody("nsdq100", "open", 49, "", 20), "between 50 and 60 USD"},
	}
	for _, c := range cases {
		code, _, b := h.prepareOn(t, c.body)
		if code != 400 || !strings.Contains(string(b), c.want) {
			t.Errorf("%s: %d %s (want %q)", c.body, code, b, c.want)
		}
	}
	// spx500 is a book of the executor but not of the canary policy: the
	// policy refuses it outright (max_order_notional 1) and so do the bounds
	// (x20 is not allowed on a default book).
	code, _, b := h.prepareOn(t, canaryBody("spx500", "open", 50, "", 20))
	if code != 400 || !strings.Contains(string(b), "leverage") {
		t.Fatalf("spx500: %d %s", code, b)
	}
}

func TestRealCanaryOpenCloseRoundTrip(t *testing.T) {
	// D7 (2026-10-22): the account's holdings on OTHER instruments (the
	// fake's SPX500) are outside the canary: a REAL warning that names them,
	// not a block.
	h := canaryServer(t, true, true)
	code, pv, b := h.prepareOn(t, canaryBody("nsdq100", "open", 50, "", 20))
	if code != 200 || pv.Verdict != VerdictAllow {
		t.Fatalf("prepare with a holding on another instrument: %d %s", code, b)
	}
	if !hasReason(pv.Warnings, "REAL: the account also holds position(s) 3616683295 on other instruments") || !hasReason(pv.Warnings, "plan D7") {
		t.Fatalf("warnings %v", pv.Warnings)
	}
	if pv.Risk == nil || pv.Risk.State.OpenPositions != 0 {
		t.Fatalf("open positions must count the instrument only: %+v", pv.Risk)
	}
	// A position on the canary's own instrument that the ledger does not
	// know: blocked, and max_positions trips (it counts the instrument).
	h.fb.positions = append(h.fb.positions, broker.Position{ID: "9000000001", Instrument: broker.Instrument{Venue: "etoro", ID: 28}, Side: broker.Long, Units: 0.03, OpenRate: 29800, Amount: 50, Leverage: 20, Exposure: 1000})
	code, pv, b = h.prepareOn(t, canaryBody("nsdq100", "open", 50, "", 20))
	if code != 200 || pv.Verdict != VerdictBlock {
		t.Fatalf("prepare with a foreign NSDQ100 position: %d %s", code, b)
	}
	if !hasReason(pv.Reasons, "the account holds position(s) 9000000001 on NSDQ100 that the ledger does not") || !hasReason(pv.Reasons, "max_positions") {
		t.Fatalf("reasons %v", pv.Reasons)
	}
	if pv.Token != "" || pv.Confirmation != "PLACE REAL NSDQ100" || pv.Environment != "real" || pv.Leverage != 20 {
		t.Fatalf("blocked preview %+v", pv)
	}

	// An empty account: the day-0 open.
	h = canaryServer(t, true, false)
	code, pv, b = h.prepareOn(t, canaryBody("nsdq100", "open", 50, "", 20))
	if code != 200 || pv.Verdict != VerdictAllow {
		t.Fatalf("prepare: %d %s", code, b)
	}
	ask := 29851.0
	if pv.Leverage != 20 || pv.AmountUSD != 50 || pv.Units != 50*20/ask || pv.ExposureUSD < 999.99 || pv.ExposureUSD > 1000.01 || pv.Environment != "real" {
		t.Fatalf("preview sizing %+v", pv)
	}
	if !strings.HasPrefix(pv.ClientRef, "ui-real-nsdq100-2026-10-19-open-") || pv.RequestID != etoro.RequestIDFor(pv.ClientRef) {
		t.Fatalf("ref %s", pv.ClientRef)
	}
	if pv.Protection == nil || !pv.Protection.Levels.Feasible {
		t.Fatalf("protection %+v", pv.Protection)
	}
	// The stop sits inside the policy's 2.3-2.45 % band: 23 to 24.5 USD on
	// 50 USD invested, under the venue's 25 USD cap. The venue bounds were
	// read for x20.
	sl := pv.Protection.Levels
	if sl.SLPct < 0.023-1e-9 || sl.SLPct > 0.0245+1e-9 || pv.MaxLossUSD < 23-0.05 || pv.MaxLossUSD > 24.5+0.05 || pv.MaxLossUSD != sl.MaxLossUSD {
		t.Fatalf("stop %.4f%% max loss %.2f (%+v)", sl.SLPct*100, pv.MaxLossUSD, sl)
	}
	if h.fb.limitsLeverage != 20 || pv.Protection.Bounds.MaxStopLossPct != 0.025 || pv.Protection.BoundsSource != "venue" {
		t.Fatalf("bounds for x%d: %+v (%s)", h.fb.limitsLeverage, pv.Protection.Bounds, pv.Protection.BoundsSource)
	}
	if pv.Risk == nil || pv.Risk.PolicyVersion != risk.EtoroRealCanaryVersion || pv.Risk.Order.Leverage != 20 || pv.Risk.State.OpenPositions != 0 {
		t.Fatalf("risk %+v", pv.Risk)
	}
	// The cost preview was asked at x20 with the order's stop (eToro's costs
	// route needs one at leverage > 1; the fake refuses without it) and the
	// cash check is on invested.
	if h.fb.previews != 1 || pv.Costs == nil {
		t.Fatalf("previews %d costs %+v", h.fb.previews, pv.Costs)
	}
	if lp := h.fb.lastPreview; lp.Leverage != 20 || lp.Amount != 50 || lp.StopLoss != sl.SLRate {
		t.Fatalf("cost preview request %+v (want x20, 50 USD, stop %.2f)", lp, sl.SLRate)
	}
	if pv.Account == nil || pv.Account.Cash < 50 {
		t.Fatalf("account %+v", pv.Account)
	}

	// The demo phrase is refused; the REAL phrase places at x20.
	if code, _, b := h.placeOn(t, placeBody(pv.Token, "PLACE NSDQ100", "canary day 0", "diego")); code != 400 || !strings.Contains(string(b), "confirmation phrase") {
		t.Fatalf("demo phrase on real: %d %s", code, b)
	}
	code, resp, b := h.placeOn(t, placeBody(pv.Token, "PLACE REAL NSDQ100", "canary day 0", "diego"))
	if code != 200 || resp.Status != "executed" || !resp.LedgerRecorded {
		t.Fatalf("place: %d %s", code, b)
	}
	if o := h.fb.opens[0]; o.Leverage != 20 || o.Amount != 50 || o.StopLoss != sl.SLRate || !strings.HasPrefix(o.ClientRef, "ui-real-") {
		t.Fatalf("open request %+v", o)
	}
	recs := h.records(t)
	if len(recs) != 1 {
		t.Fatalf("records %d", len(recs))
	}
	r := recs[0]
	if r.Mode != etoroledger.ModeReal || !r.IsAccount() || r.Demo.Env != "real" || r.Demo.Risk.PolicyVersion != risk.EtoroRealCanaryVersion ||
		r.Demo.Risk.Order.Leverage != 20 || !etoroledger.IsRealRef(r.ClientRef()) || !strings.Contains(r.Demo.Notes[0], "REAL account") ||
		r.Demo.PositionAfter.Units != pv.Units || r.Demo.PositionAfter.Amount != 50 || r.Demo.Protection == nil || r.Demo.Protection.MaxLossUSD <= 0 {
		t.Fatalf("real record %+v demo %+v", r, r.Demo)
	}
	// The audit says REAL and x20.
	var saw bool
	for _, e := range h.audit(t) {
		if e.Action == "trade.place" && strings.Contains(e.Detail, "REAL open nsdq100 50 USD x20") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("audit detail without REAL x20: %+v", h.audit(t))
	}

	// A second open the same day: the ledger holds the canary (and the
	// account holds one position: max_positions).
	code, pv2, _ := h.prepareOn(t, canaryBody("nsdq100", "open", 50, "", 20))
	if code != 200 || pv2.Verdict != VerdictBlock || !hasReason(pv2.Reasons, "already holds position") || !hasReason(pv2.Reasons, "max_positions") {
		t.Fatalf("second open %+v", pv2)
	}

	// Close: CLOSE REAL NSDQ100, never trapped by the caps.
	pid := resp.PositionIDs[0]
	code, pc, b := h.prepareOn(t, canaryBody("nsdq100", "close", 0, pid, 20))
	if code != 200 || pc.Verdict != VerdictAllow || pc.Confirmation != "CLOSE REAL NSDQ100" || !strings.HasPrefix(pc.ClientRef, "ui-real-nsdq100-2026-10-19-close-") {
		t.Fatalf("prepare close: %d %s", code, b)
	}
	code, cr, b := h.placeOn(t, placeBody(pc.Token, "CLOSE REAL NSDQ100", "canary close-out", "diego"))
	if code != 200 || cr.Status != "executed" || cr.PositionAfter.State != "flat" {
		t.Fatalf("close: %d %s", code, b)
	}
	recs = h.records(t)
	if len(recs) != 2 || recs[1].Mode != etoroledger.ModeReal || recs[1].Demo.Action != "close" {
		t.Fatalf("after close %+v", recs)
	}
	if p := etoroledger.LastDemoPosition(recs, "nsdq100"); p.State != "flat" {
		t.Fatalf("last position %+v", p)
	}
}

// A token prepared by a demo process (no env claim) is refused by a real
// process even when signed with the same operator session, and vice versa.
func TestRealCanaryTokenEnvironmentIsBound(t *testing.T) {
	h := canaryServer(t, true, false)
	now := h.now
	c := tradeClaims{ID: newID(), Ledger: RealCanaryLedgerName, ClientRef: "ui-nsdq100-2026-10-19-open-abcdef", RequestID: "r", Book: "nsdq100", Side: TradeOpen,
		AmountUSD: 50, Units: 0.03, Bid: 29850, Ask: 29851, Confirmation: "PLACE REAL NSDQ100", Leverage: 20, Iat: now.Unix(), Exp: now.Add(time.Minute).Unix()}
	tok, err := h.srv.Trading.sign(c, testToken)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, b := h.placeOn(t, placeBody(tok, "PLACE REAL NSDQ100", "x", "diego")); code != 400 || !strings.Contains(string(b), "another account environment") {
		t.Fatalf("demo token on real: %d %s", code, b)
	}
	if len(h.fb.opens) != 0 {
		t.Fatal("an order was sent")
	}
}

func TestCanaryRehearsalOnDemo(t *testing.T) {
	// The demo account keeps the cells' positions (SPX500 here) and the
	// rehearsal must still open NSDQ100 at x20 with REAL-less phrases.
	h := canaryServer(t, false, true)
	ci := get[ControlsInfo](t, h.ts, "/api/ui/controls?ledger=etoro-canary-rehearsal", 200)
	if ci.Environment != "demo" || ci.Trading.Environment != "demo" || !ci.Trading.Rehearsal || ci.Trading.MaxAmountUSD != 60 ||
		ci.Trading.LeverageOptions[0] != 20 || !strings.HasPrefix(ci.Trading.PolicyVersion, "etoro-canary-rehearsal-") || len(ci.Trading.Books) != 2 {
		t.Fatalf("rehearsal controls %+v", ci.Trading)
	}
	code, pv, b := h.prepareOn(t, canaryBody("nsdq100", "open", 50, "", 20))
	if code != 200 || pv.Verdict != VerdictAllow || pv.Confirmation != "PLACE NSDQ100" || pv.Environment != "demo" || !strings.HasPrefix(pv.ClientRef, "ui-nsdq100-") {
		t.Fatalf("rehearsal prepare: %d %s", code, b)
	}
	// A demo-cell position on the same instrument is a warning in the
	// rehearsal, not a block.
	h.fb.positions = append(h.fb.positions, broker.Position{ID: "999", Instrument: broker.Instrument{Venue: "etoro", ID: 28}, Side: broker.Long, Units: 0.03, Amount: 1100, Leverage: 1})
	code, pv, b = h.prepareOn(t, canaryBody("nsdq100", "open", 50, "", 20))
	if code != 200 || pv.Verdict != VerdictAllow || !hasReason(pv.Warnings, "rehearsal: the demo account holds position(s) 999") {
		t.Fatalf("rehearsal with the cell's position: %d %s", code, b)
	}
	code, resp, b := h.placeOn(t, placeBody(pv.Token, "PLACE NSDQ100", "canary rehearsal", "diego"))
	if code != 200 || resp.Status != "executed" {
		t.Fatalf("rehearsal place: %d %s", code, b)
	}
	if o := h.fb.opens[0]; o.Leverage != 20 || o.Amount != 50 {
		t.Fatalf("rehearsal open %+v", o)
	}
	recs := h.records(t)
	if recs[0].Mode != etoroledger.ModeDemo || recs[0].Demo.Env != "demo" || !strings.Contains(recs[0].Demo.Notes[0], "rehearsal") ||
		!strings.HasPrefix(recs[0].Demo.Risk.PolicyVersion, "etoro-canary-rehearsal-") {
		t.Fatalf("rehearsal record %+v", recs[0].Demo)
	}
	// On the real account the same foreign position would block.
	r := canaryServer(t, true, false)
	r.fb.positions = []broker.Position{{ID: "999", Instrument: broker.Instrument{Venue: "etoro", ID: 28}, Side: broker.Long, Units: 0.03, Amount: 1100, Leverage: 1}}
	_, rp, _ := r.prepareOn(t, canaryBody("nsdq100", "open", 50, "", 20))
	if rp.Verdict != VerdictBlock || !hasReason(rp.Reasons, "the account holds position(s) 999 on NSDQ100 that the ledger does not") {
		t.Fatalf("real with a foreign position: %+v", rp.Reasons)
	}
}

// The recommend endpoint sizes the profiles for the canary's leverage.
func TestRealCanaryRecommendUsesTheCanaryLeverage(t *testing.T) {
	h := canaryServer(t, true, false)
	body, _ := json.Marshal(map[string]any{"book": "nsdq100", "side": "open"})
	code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/recommend?ledger=etoro-real-canary", token: h.token, origin: "http://127.0.0.1:5174", body: string(body)})
	if code != 200 {
		t.Fatalf("recommend %d %s", code, b)
	}
	var rec TradeRecommendation
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.AmountUSD != 50 || rec.Leverage != 20 || rec.ExposureUSD < 999 || rec.ExposureUSD > 1001 || rec.Environment != "real" || rec.PolicyVersion != risk.EtoroRealCanaryVersion {
		t.Fatalf("recommendation %+v", rec)
	}
	for name, l := range rec.Profiles {
		if !l.Feasible || l.SLPct < 0.023-1e-9 || l.SLPct > 0.0245+1e-9 {
			t.Errorf("%s: %+v", name, l)
		}
	}
	body, _ = json.Marshal(map[string]any{"book": "nsdq100", "side": "open", "leverage": 1})
	if code, b := post(t, h.ts, ctl{path: "/api/ui/etoro/trades/recommend?ledger=etoro-real-canary", token: h.token, origin: "http://127.0.0.1:5174", body: string(body)}); code != 400 || !strings.Contains(string(b), "leverage") {
		t.Fatalf("recommend x1 on the canary: %d %s", code, b)
	}
}
