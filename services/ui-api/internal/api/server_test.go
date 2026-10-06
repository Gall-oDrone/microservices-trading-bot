package api

import (
	"encoding/json"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/store"
)

// 2026-10-02 21:00 Mexico City: the 2026-10-01 bar is the latest closed one.
var fixedNow = time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)

func newTestServer(t *testing.T, now time.Time, mutate func(*Server)) *httptest.Server {
	t.Helper()
	s := &Server{
		Store: store.New("testdata/ledger.jsonl", ""), Policy: risk.DefaultPolicy(), PolicySrc: "test",
		StageSize: 0.001, Version: "test", Now: func() time.Time { return now },
		Log: log.New(io.Discard, "", 0),
	}
	if mutate != nil {
		mutate(s)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func get[T any](t *testing.T, ts *httptest.Server, path string, wantStatus int) T {
	t.Helper()
	res, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != wantStatus {
		t.Fatalf("GET %s: status %d, want %d: %s", path, res.StatusCode, wantStatus, body)
	}
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("GET %s: %v: %s", path, err, body)
	}
	return v
}

func TestForwardTests(t *testing.T) {
	ts := newTestServer(t, fixedNow, nil)
	resp := get[ForwardTestsResponse](t, ts, "/api/ui/forward-tests", 200)
	if len(resp.Books) != 2 || resp.Books[0].Book != "btc_mxn" || resp.Books[1].Book != "btc_usd" {
		t.Fatalf("books: %+v", resp.Books)
	}
	mxn := resp.Books[0]
	if mxn.Decision.Signal != "long" || mxn.Run.Status != "ok" || mxn.Mode != "stage" {
		t.Fatalf("btc_mxn: %+v", mxn)
	}
	// (1570090 - 1336351) / 1570090 = 14.89%
	if math.Abs(mxn.DistanceToSMAPct-14.887) > 0.01 {
		t.Fatalf("distance to SMA %.3f", mxn.DistanceToSMAPct)
	}
	if mxn.StagePosition == nil || mxn.StagePosition.BTC != 0.00099999 {
		t.Fatalf("stage position %+v", mxn.StagePosition)
	}
	if mxn.Milestones.Interim != "2027-03-26" || mxn.Milestones.DaysElapsed != 4 || mxn.Milestones.WindowProgress <= 0 {
		t.Fatalf("milestones %+v", mxn.Milestones)
	}
}

func TestRunStatusMissedAndPending(t *testing.T) {
	// 2026-10-05 18:00 Mexico City: 2026-10-02..04 are missing.
	late := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	ft := get[ForwardTest](t, newTestServer(t, late, nil), "/api/ui/forward-tests/btc_usd", 200)
	if ft.Run.Status != "missed" || len(ft.Run.MissingDays) != 3 || ft.RiskWarnings == 0 {
		t.Fatalf("want missed with warnings, got %+v", ft)
	}
	// 2026-10-03 01:00 Mexico City: only 2026-10-02 missing, run not due yet.
	early := time.Date(2026, 10, 3, 7, 0, 0, 0, time.UTC)
	ft = get[ForwardTest](t, newTestServer(t, early, nil), "/api/ui/forward-tests/btc_usd", 200)
	if ft.Run.Status != "pending" {
		t.Fatalf("want pending, got %+v", ft.Run)
	}
}

func TestLedgerFillsAndCosts(t *testing.T) {
	ts := newTestServer(t, fixedNow, nil)
	l := get[LedgerResponse](t, ts, "/api/ui/forward-tests/btc_mxn/ledger", 200)
	if len(l.Records) != 3 || len(l.Equity) != 3 || len(l.Fills) != 1 {
		t.Fatalf("records %d equity %d fills %d", len(l.Records), len(l.Equity), len(l.Fills))
	}
	f := l.Fills[0]
	if f.RefOpen == nil || *f.RefOpen != 1504920 || f.SlippageBps == nil {
		t.Fatalf("ref open / slippage missing: %+v", f)
	}
	// avg 1510998.45 vs open 1504920 = +40.4 bps adverse; fee 78 bps.
	if math.Abs(*f.SlippageBps-40.4) > 0.5 || math.Abs(f.FeeBps-78) > 1 || math.Abs(f.TotalCostBps-118.4) > 1.5 {
		t.Fatalf("costs: fee %.1f slip %.1f total %.1f", f.FeeBps, *f.SlippageBps, f.TotalCostBps)
	}
	if !f.Fallback || f.AssumedLegBps != 70 {
		t.Fatalf("fill flags: %+v", f)
	}
	if l2 := get[LedgerResponse](t, ts, "/api/ui/forward-tests/btc_mxn/ledger?mode=dry-run", 200); len(l2.Records) != 0 {
		t.Fatalf("mode filter: %d", len(l2.Records))
	}
}

func TestCandles(t *testing.T) {
	ts := newTestServer(t, fixedNow, nil)
	c := get[CandlesResponse](t, ts, "/api/ui/forward-tests/btc_usd/candles?days=30", 200)
	if len(c.Candles) != 30 || c.File != "btc_usd_daily_2026-10-01.csv" {
		t.Fatalf("candles %d file %s", len(c.Candles), c.File)
	}
	last := c.Candles[len(c.Candles)-1]
	if last.Date != "2026-10-01" || last.SMA50 == nil || last.VolumeRatio == nil || last.Long == nil || !*last.Long {
		t.Fatalf("last candle %+v", last)
	}
	// The ledger's SMA50 came from the full history; the 50-bar window is
	// the same, so the values must agree.
	if math.Abs(*last.SMA50-77750.86) > 0.01 {
		t.Fatalf("sma50 %.4f does not match the ledger's 77750.86", *last.SMA50)
	}
	get[errorBody](t, ts, "/api/ui/forward-tests/btc_usd/candles?days=0", 400)
}

func TestRisk(t *testing.T) {
	ts := newTestServer(t, fixedNow, nil)
	r := get[RiskResponse](t, ts, "/api/ui/risk", 200)
	if r.Enforcement != "enforced" || r.Halted || len(r.Books) != 2 {
		t.Fatalf("risk: %+v", r)
	}
	mxn := r.Books[0]
	if mxn.NextOrder == nil || mxn.NextOrder.Action != "none" || !mxn.NextOrder.Decision.Allowed {
		t.Fatalf("next order: %+v", mxn.NextOrder)
	}
	if mxn.Cost.Legs != 1 || mxn.Cost.FallbackLegs != 1 || math.Abs(mxn.Utilization.Position-0.099999) > 1e-6 {
		t.Fatalf("cost/util: %+v %+v", mxn.Cost, mxn.Utilization)
	}
	if r.Blocks != 0 {
		t.Fatalf("no blocks expected today: %+v", r)
	}
	// The fixture predates the executor's risk check.
	if mxn.LastCheck != nil || mxn.BlockedDays == nil || len(mxn.BlockedDays) != 0 {
		t.Fatalf("last check %+v blocked %v", mxn.LastCheck, mxn.BlockedDays)
	}
}

// A day the executor blocked (stage action "blocked", risk.allowed false)
// shows up as the last check, a blocked day and a warning.
func TestRiskShowsExecutorBlocks(t *testing.T) {
	dir := t.TempDir()
	line := `{"recorded_at":"2026-10-02T01:00:00Z","code_version":"x","mode":"stage","book":"btc_usd","prereg":"P",` +
		`"decision":{"bar_date":"2026-10-01","fill_date":"2026-10-02","close":85944,"sma50":77750,"signal":"long","prev_signal":"flat","action":"buy"},` +
		`"paper":{"forward_start":"2026-09-29","days":3,"position":"flat","fills":0,"leg_cost_bps":40,"equity":1,"equity_if_closed":1,"hold_equity":1.02,"max_drawdown":0.01,"pending_action":"buy"},` +
		`"candles":{"source":"s","first":"2020-04-24","last":"2026-10-01","bars":10,"sha256_prefix":"ff"},` +
		`"stage":{"env":"e","target":"long","action":"blocked","position_before":{"state":"flat","btc":0},"position_after":{"state":"flat","btc":0},` +
		`"risk":{"policy_version":"old-policy","order":{"book":"btc_usd","side":"buy","qty_btc":0.001,"price":100000,"ref_price":85944},` +
		`"state":{"position_btc":0,"orders_today":0},"allowed":false,` +
		`"findings":[{"rule":"max_price_deviation_bps","severity":"block","limit":1500,"value":1635,"message":"m"}]}}}`
	writeFile(t, dir+"/ledger.jsonl", line+"\n")
	ts := newTestServer(t, fixedNow, func(s *Server) { s.Store = store.New(dir+"/ledger.jsonl", "") })
	r := get[RiskResponse](t, ts, "/api/ui/risk", 200)
	var usd BookRisk
	for _, b := range r.Books {
		if b.Book == "btc_usd" {
			usd = b
		}
	}
	if usd.LastCheck == nil || usd.LastCheck.Allowed || usd.LastCheck.BarDate != "2026-10-01" || usd.LastCheck.Order.Price != 100000 {
		t.Fatalf("last check: %+v", usd.LastCheck)
	}
	if len(usd.BlockedDays) != 1 || usd.BlockedDays[0] != "2026-10-01" {
		t.Fatalf("blocked days: %v", usd.BlockedDays)
	}
	rules := map[string]bool{}
	for _, f := range usd.Findings {
		rules[f.Rule] = true
	}
	if !rules[RuleOrderBlocked] || !rules[RulePolicyMismatch] {
		t.Fatalf("findings: %+v", usd.Findings)
	}
	// The position is unchanged, so the next run would try the buy again.
	if usd.NextOrder == nil || usd.NextOrder.Action != "buy" {
		t.Fatalf("next order: %+v", usd.NextOrder)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRiskBlocksWhenHalted(t *testing.T) {
	dir := t.TempDir()
	// A flat stage position with a long signal: the next run would buy.
	line := `{"recorded_at":"2026-10-02T01:00:00Z","code_version":"x","mode":"stage","book":"btc_usd","prereg":"P",` +
		`"decision":{"bar_date":"2026-10-01","fill_date":"2026-10-02","close":85944,"sma50":77750,"signal":"long","prev_signal":"flat","action":"buy"},` +
		`"paper":{"forward_start":"2026-09-29","days":3,"position":"flat","fills":0,"leg_cost_bps":40,"equity":1,"equity_if_closed":1,"hold_equity":1.02,"max_drawdown":0.3,"pending_action":"buy"},` +
		`"candles":{"source":"s","first":"2020-04-24","last":"2026-10-01","bars":10,"recent_gaps":"2026-09-15","sha256_prefix":"ff"},` +
		`"stage":{"env":"e","target":"flat","action":"none","position_before":{"state":"flat","btc":0},"position_after":{"state":"flat","btc":0}}}`
	writeFile(t, dir+"/ledger.jsonl", line+"\n")
	ts := newTestServer(t, fixedNow, func(s *Server) {
		s.Store = store.New(dir+"/ledger.jsonl", "")
		s.Policy.Halted, s.Policy.HaltReason = true, "test halt"
	})
	r := get[RiskResponse](t, ts, "/api/ui/risk", 200)
	var usd BookRisk
	for _, b := range r.Books {
		if b.Book == "btc_usd" {
			usd = b
		}
	}
	if usd.NextOrder == nil || usd.NextOrder.Action != "buy" || usd.NextOrder.Decision.Allowed {
		t.Fatalf("halted buy must be blocked: %+v", usd.NextOrder)
	}
	rules := map[string]bool{}
	for _, f := range usd.Findings {
		rules[f.Rule] = true
	}
	for _, want := range []string{risk.RuleHalted, risk.RuleDrawdownWarn, RuleDataGap} {
		if !rules[want] {
			t.Fatalf("missing finding %s in %+v", want, usd.Findings)
		}
	}
	if usd.Findings[0].Severity != risk.Block {
		t.Fatalf("blocks must sort first: %+v", usd.Findings)
	}
}

func TestMultiLedger(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile("testdata/ledger.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	// The dry-run ledger: the btc_usd lines only, re-labelled.
	var keep []string
	for _, l := range strings.Split(strings.TrimSpace(string(src)), "\n") {
		if strings.Contains(l, `"book":"btc_usd"`) {
			keep = append(keep, strings.Replace(l, `"mode":"stage"`, `"mode":"dry-run"`, 1))
		}
	}
	writeFile(t, dir+"/ledger.jsonl", strings.Join(keep, "\n")+"\n")
	ts := newTestServer(t, fixedNow, func(s *Server) {
		s.Ledgers = []Ledger{{Name: "stage", Store: s.Store}, {Name: "dry-run", Store: store.New(dir+"/ledger.jsonl", "")},
			{Name: "volume", Store: store.New(dir+"/missing/ledger.jsonl", "")}}
	})

	ls := get[LedgersResponse](t, ts, "/api/ui/ledgers", 200)
	if len(ls.Ledgers) != 3 || !ls.Ledgers[0].Default || ls.Ledgers[1].Default || ls.Ledgers[0].Records != 6 {
		t.Fatalf("ledgers %+v", ls.Ledgers)
	}
	if dr := ls.Ledgers[1]; !dr.Found || dr.Records != len(keep) || len(dr.Modes) != 1 || dr.Modes[0] != "dry-run" || dr.LastBarDate == "" {
		t.Fatalf("dry-run info %+v", dr)
	}
	if v := ls.Ledgers[2]; v.Found || v.Records != 0 || v.Error != "" {
		t.Fatalf("missing ledger must be reported as not found, not as an error: %+v", v)
	}

	def := get[ForwardTestsResponse](t, ts, "/api/ui/forward-tests", 200)
	if def.Ledger != "stage" || def.Books[0].Ledger != "stage" {
		t.Fatalf("default ledger: %+v", def.Ledger)
	}
	dr := get[ForwardTestsResponse](t, ts, "/api/ui/forward-tests?ledger=dry-run", 200)
	if dr.Ledger != "dry-run" {
		t.Fatalf("ledger %q", dr.Ledger)
	}
	for _, b := range dr.Books {
		if b.Ledger != "dry-run" {
			t.Fatalf("card ledger %q", b.Ledger)
		}
		if b.Book == "btc_usd" && b.Mode != "dry-run" {
			t.Fatalf("btc_usd mode %q", b.Mode)
		}
		if b.Book == "btc_mxn" && b.RecordedAt != "" {
			t.Fatalf("btc_mxn has no dry-run records: %+v", b)
		}
	}
	if r := get[RiskResponse](t, ts, "/api/ui/risk?ledger=dry-run", 200); r.Ledger != "dry-run" {
		t.Fatalf("risk ledger %q", r.Ledger)
	}
	if l := get[LedgerResponse](t, ts, "/api/ui/forward-tests/btc_usd/ledger?ledger=dry-run", 200); l.Ledger != "dry-run" || len(l.Records) != len(keep) {
		t.Fatalf("ledger view %q %d", l.Ledger, len(l.Records))
	}
	get[errorBody](t, ts, "/api/ui/forward-tests?ledger=nope", 400)
	get[errorBody](t, ts, "/api/ui/risk?ledger=../etc", 400)
}

func TestParseLedgers(t *testing.T) {
	ls, err := ParseLedgers(" stage=/a/ledger.jsonl , dry-run=/b/ledger.jsonl,")
	if err != nil || len(ls) != 2 || ls[0].Name != "stage" || ls[1].Store.LedgerPath != "/b/ledger.jsonl" || ls[1].Store.CandlesDir != "/b/candles" {
		t.Fatalf("%+v %v", ls, err)
	}
	for _, bad := range []string{"", "stage", "stage=", "Stage=/a", "a=/x,a=/y", "../x=/y"} {
		if _, err := ParseLedgers(bad); err == nil {
			t.Fatalf("%q must fail", bad)
		}
	}
}

func TestReadOnlyAndValidation(t *testing.T) {
	ts := newTestServer(t, fixedNow, nil)
	res, err := http.Post(ts.URL+"/api/ui/risk", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST must be 405, got %d", res.StatusCode)
	}
	get[errorBody](t, ts, "/api/ui/forward-tests/btc_*/candles", 400)
	get[errorBody](t, ts, "/api/ui/forward-tests/eth_mxn", 404)
	get[errorBody](t, ts, "/api/ui/nope", 404)
	h := get[Health](t, ts, "/api/ui/healthz", 200)
	if !h.LedgerFound || h.Records != 6 {
		t.Fatalf("health %+v", h)
	}
}
