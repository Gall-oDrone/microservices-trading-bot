package api

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/audit"
	"bitso-trading-platform/ui-api/internal/datahealth"
	"bitso-trading-platform/ui-api/internal/store"
)

// etoroNow is 2026-10-16 13:00 New York (a Friday, cash session open):
// the 09:35 run of that day is in testdata/etoro-demo (it closed NSDQ100 at
// 29770.5 and held SPX500), the reconcile of the day drifted and its
// re-check at 16:43 UTC was clean.
var etoroNow = time.Date(2026, 10, 16, 17, 0, 0, 0, time.UTC)

const etoroTestdata = "testdata/etoro-demo"

// etoroServer serves the Bitso test ledger as "stage" and the eToro
// testdata tree as "etoro-demo" (read-only; the tree is not copied).
func etoroServer(t *testing.T, now time.Time, mutate func(*Server)) *httptest.Server {
	t.Helper()
	return newTestServer(t, now, func(s *Server) {
		s.Ledgers = []Ledger{
			{Name: "stage", Store: s.Store},
			{Name: "etoro-demo", Store: store.New(filepath.Join(etoroTestdata, "ledger.jsonl"), ""), Venue: VenueEtoro},
		}
		if mutate != nil {
			mutate(s)
		}
	})
}

// copyEtoroLedger copies the eToro testdata ledger into a temp dir, for
// tests that write a halt file or an audit log next to it.
func copyEtoroLedger(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join(etoroTestdata, "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "ledger.jsonl"), string(src))
	return dir
}

func TestLedgersListBothVenues(t *testing.T) {
	ts := etoroServer(t, etoroNow, nil)
	ls := get[LedgersResponse](t, ts, "/api/ui/ledgers", 200)
	if len(ls.Ledgers) != 2 {
		t.Fatalf("ledgers %+v", ls.Ledgers)
	}
	if b := ls.Ledgers[0]; b.Name != "stage" || b.Venue != "bitso" || !b.Default || b.Records != 6 {
		t.Fatalf("bitso %+v", b)
	}
	e := ls.Ledgers[1]
	if e.Name != "etoro-demo" || e.Venue != "etoro" || e.Default || !e.Found || e.Error != "" {
		t.Fatalf("etoro %+v", e)
	}
	// Counted with etoroledger, not dailyledger: 10 demo lines through the
	// 2026-10-15 bar.
	if e.Records != 10 || len(e.Modes) != 1 || e.Modes[0] != "demo" || e.LastBarDate != "2026-10-15" {
		t.Fatalf("etoro counts %+v", e)
	}
}

// The Bitso-only routes refuse the eToro ledger (and the eToro routes the
// Bitso one) with a clean 400; without an eToro ledger they are 404.
func TestVenueRoutesRejectTheOtherLedger(t *testing.T) {
	ts := etoroServer(t, etoroNow, nil)
	for _, p := range []string{
		"/api/ui/forward-tests", "/api/ui/forward-tests/btc_mxn", "/api/ui/forward-tests/btc_mxn/ledger",
		"/api/ui/forward-tests/btc_mxn/candles", "/api/ui/forward-tests/btc_mxn/performance",
		"/api/ui/forward-tests/btc_mxn/capacity", "/api/ui/risk", "/api/ui/health/data",
	} {
		t.Run(p, func(t *testing.T) {
			e := get[errorBody](t, ts, p+"?ledger=etoro-demo", 400)
			if !strings.Contains(e.Error, "venue etoro") || !strings.Contains(e.Error, "serves bitso ledgers") {
				t.Fatalf("error %q", e.Error)
			}
		})
	}
	for _, p := range []string{"/api/ui/etoro/forward-tests", "/api/ui/etoro/risk", "/api/ui/etoro/health",
		"/api/ui/etoro/reconcile", "/api/ui/etoro/risk-report", "/api/ui/etoro/forward-tests/nsdq100/ledger"} {
		t.Run(p, func(t *testing.T) {
			if e := get[errorBody](t, ts, p+"?ledger=stage", 400); !strings.Contains(e.Error, "venue bitso") || !strings.Contains(e.Error, "serves etoro ledgers") {
				t.Fatalf("error %q", e.Error)
			}
			get[errorBody](t, ts, p+"?ledger=nope", 400)
		})
	}
	// No eToro ledger configured: the namespace answers 404, not 500.
	plain := newTestServer(t, etoroNow, nil)
	if e := get[errorBody](t, plain, "/api/ui/etoro/forward-tests", 404); !strings.Contains(e.Error, "no etoro ledger") {
		t.Fatalf("error %q", e.Error)
	}
	// Book names: the eToro pattern, and only known books.
	get[errorBody](t, ts, "/api/ui/etoro/forward-tests/btc_mxn/ledger", 400)
	get[errorBody](t, ts, "/api/ui/etoro/forward-tests/dax40/ledger", 404)
}

func TestEtoroForwardTests(t *testing.T) {
	ts := etoroServer(t, etoroNow, nil)
	resp := get[EtoroForwardTestsResponse](t, ts, "/api/ui/etoro/forward-tests", 200)
	if resp.Ledger != "etoro-demo" || resp.Venue != "etoro" || len(resp.Books) != 2 {
		t.Fatalf("resp %+v", resp)
	}
	// 13:00 New York on a trading Friday: the cash session is open.
	if s := resp.Session; !s.Open || !s.TradingDay || s.Date != "2026-10-16" || s.NowNewYork != "2026-10-16T13:00:00-04:00" ||
		s.OpensAt != "2026-10-16T09:30:00-04:00" || s.ClosesAt != "2026-10-16T16:00:00-04:00" {
		t.Fatalf("session %+v", s)
	}
	n, sp := resp.Books[0], resp.Books[1]
	if n.Book != "nsdq100" || sp.Book != "spx500" || n.Ledger != "etoro-demo" || n.Quote != "USD" || n.Instrument.ID != 28 {
		t.Fatalf("books %+v %+v", n, sp)
	}
	// NSDQ100: the 2026-10-15 close fell under the SMA50; the 10-16 run
	// closed position 3616683146 at 29770.5 and holds flat.
	if n.Decision.BarDate != "2026-10-15" || n.Decision.Signal != "flat" || n.Decision.Action != "sell" || n.Mode != "demo" {
		t.Fatalf("nsdq100 decision %+v", n.Decision)
	}
	if n.DemoPosition == nil || n.DemoPosition.State != "flat" || n.DemoPosition.PositionID != "" || n.DemoAction != "close" {
		t.Fatalf("nsdq100 position %+v action %q", n.DemoPosition, n.DemoAction)
	}
	if n.LastOrder == nil || n.LastOrder.Kind != "close" || n.LastOrder.Status != "executed" || n.LastOrder.AvgPrice != 29770.5 ||
		n.LastOrder.FillDate != "2026-10-16" || n.LastOrder.PositionIDs[0] != "3616683146" {
		t.Fatalf("nsdq100 last order %+v", n.LastOrder)
	}
	if n.RecordedAt != "2026-10-16T13:35:05Z" || n.Account == nil || n.Account.Equity != 99949.9 || n.Account.Currency != "USD" {
		t.Fatalf("nsdq100 recorded %s account %+v", n.RecordedAt, n.Account)
	}
	// (29868.7 - 29871.96) / 29868.7 = -0.0109%
	if math.Abs(n.DistanceToSMAPct+0.0109) > 0.001 || math.Abs(n.ExcessVsHoldPct) > 1e-9 {
		t.Fatalf("distance %.4f excess %.4f", n.DistanceToSMAPct, n.ExcessVsHoldPct)
	}
	// Today's run is recorded; the next one is Monday 09:35 New York.
	if r := n.Run; r.Status != "ok" || !r.TodayDue || !r.TodayDone || r.DueDate != "2026-10-16" || r.LastFillDate != "2026-10-16" ||
		r.NextRunDate != "2026-10-19" || r.NextRunAt != "2026-10-19T09:35:00-04:00" || len(r.MissingDays) != 0 {
		t.Fatalf("nsdq100 run %+v", r)
	}
	if n.RiskBlocks != 0 || n.RiskWarnings != 0 || len(n.RiskFindings) != 0 {
		t.Fatalf("nsdq100 risk %+v", n.RiskFindings)
	}
	// SPX500: still long 0.140735 units, position 3616683295 opened 10-12.
	if sp.DemoPosition == nil || sp.DemoPosition.State != "long" || sp.DemoPosition.PositionID != "3616683295" ||
		sp.DemoPosition.Units != 0.140735 || math.Abs(sp.DemoPosition.OpenRate-7816.07) > 1e-6 || sp.DemoAction != "none" {
		t.Fatalf("spx500 position %+v", sp.DemoPosition)
	}
	if sp.LastOrder == nil || sp.LastOrder.Kind != "open" || sp.LastOrder.FillDate != "2026-10-12" || sp.Run.Status != "ok" {
		t.Fatalf("spx500 last order %+v run %+v", sp.LastOrder, sp.Run)
	}
}

// The run status around the 09:35 New York schedule.
func TestEtoroRunStatusTable(t *testing.T) {
	for _, c := range []struct {
		name     string
		now      time.Time
		status   string
		nextRun  string
		missing  int
		todayDue bool
		open     bool
	}{
		// Monday 10-19 09:00 New York, no 10-19 line yet: today's run is
		// pending and next is today 09:35.
		{"before the run", time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC), "pending", "2026-10-19T09:35:00-04:00", 0, true, false},
		// 09:50 New York: the run is late but inside the grace.
		{"inside the grace", time.Date(2026, 10, 19, 13, 50, 0, 0, time.UTC), "pending", "2026-10-20T09:35:00-04:00", 0, true, true},
		// Saturday: nothing due; next run Monday.
		{"weekend", time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC), "ok", "2026-10-19T09:35:00-04:00", 0, false, false},
		// Monday 11:00 New York with no 10-19 line: missed.
		{"missed monday", time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC), "missed", "2026-10-20T09:35:00-04:00", 1, true, true},
		// Wednesday: three days missing.
		{"missed three days", time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC), "missed", "2026-10-22T09:35:00-04:00", 3, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			ts := etoroServer(t, c.now, nil)
			resp := get[EtoroForwardTestsResponse](t, ts, "/api/ui/etoro/forward-tests", 200)
			r := resp.Books[0].Run
			if r.Status != c.status || r.NextRunAt != c.nextRun || len(r.MissingDays) != c.missing || r.TodayDue != c.todayDue {
				t.Fatalf("run %+v, want %s next %s missing %d", r, c.status, c.nextRun, c.missing)
			}
			if resp.Session.Open != c.open {
				t.Fatalf("session %+v, want open=%v", resp.Session, c.open)
			}
			if c.status == "missed" && resp.Books[0].RiskWarnings == 0 {
				t.Fatalf("a missed run must warn: %+v", resp.Books[0].RiskFindings)
			}
		})
	}
}

func TestEtoroLedgerAndCandles(t *testing.T) {
	ts := etoroServer(t, etoroNow, nil)
	l := get[EtoroLedgerResponse](t, ts, "/api/ui/etoro/forward-tests/nsdq100/ledger", 200)
	if l.Ledger != "etoro-demo" || l.Book != "nsdq100" || l.Mode != "demo" || len(l.Records) != 5 || len(l.Equity) != 5 {
		t.Fatalf("ledger %s %s %s records %d equity %d", l.Ledger, l.Book, l.Mode, len(l.Records), len(l.Equity))
	}
	first, last := l.Equity[0], l.Equity[4]
	if first.Date != "2026-10-09" || first.Equity != 1 || first.DemoEquity == nil || *first.DemoEquity != 99993.93 {
		t.Fatalf("first point %+v", first)
	}
	if last.Date != "2026-10-15" || math.Abs(last.Equity-0.97286) > 1e-4 || last.Signal != "flat" || last.Close != 29868.7 ||
		last.DemoEquity == nil || *last.DemoEquity != 99949.9 || last.DemoExposure == nil || *last.DemoExposure != 2155.95 {
		t.Fatalf("last point %+v", last)
	}
	if l.Records[4].Demo == nil || l.Records[4].Demo.Order == nil || l.Records[4].Demo.Order.ClientRef != "sma50-nsdq100-2026-10-16-close" {
		t.Fatalf("last record %+v", l.Records[4])
	}
	if l2 := get[EtoroLedgerResponse](t, ts, "/api/ui/etoro/forward-tests/nsdq100/ledger?mode=dry-run", 200); len(l2.Records) != 0 || l2.Mode != "" {
		t.Fatalf("mode filter: %+v", l2)
	}

	// The executor's CSV has empty volume and vwap columns; the file is
	// the 120-bar tail of the one the 10-16 run wrote.
	c := get[EtoroCandlesResponse](t, ts, "/api/ui/etoro/forward-tests/nsdq100/candles", 200)
	if c.File != "nsdq100_daily_2026-10-15.csv" || len(c.Candles) != 120 || c.Venue != "etoro" {
		t.Fatalf("candles %s %d", c.File, len(c.Candles))
	}
	lc := c.Candles[119]
	if lc.Date != "2026-10-15" || lc.Close != 29868.7 || lc.Open != 29911.9 || lc.Volume != 0 || lc.VolumeRatio != nil {
		t.Fatalf("last candle %+v", lc)
	}
	// The same 50-bar window as the executor's: its SMA50 was 29871.9592.
	if lc.SMA50 == nil || math.Abs(*lc.SMA50-29871.9592) > 0.01 || lc.Long == nil || *lc.Long {
		t.Fatalf("sma50 %+v long %+v", lc.SMA50, lc.Long)
	}
	if c30 := get[EtoroCandlesResponse](t, ts, "/api/ui/etoro/forward-tests/spx500/candles?days=30", 200); len(c30.Candles) != 30 || c30.File != "spx500_daily_2026-10-15.csv" {
		t.Fatalf("spx500 candles %s %d", c30.File, len(c30.Candles))
	}
	get[errorBody](t, ts, "/api/ui/etoro/forward-tests/nsdq100/candles?days=0", 400)
	// store.ParseCandles reads eToro's empty volume as 0 (2026-10-19), so
	// this route and the protection recommendation share the store's cache.
	if rows, _, err := store.New(filepath.Join(etoroTestdata, "ledger.jsonl"), "").Candles("nsdq100"); err != nil || len(rows) != 120 {
		t.Fatalf("store.ParseCandles must accept the eToro CSV: %d rows, %v", len(rows), err)
	}
}

func TestEtoroRisk(t *testing.T) {
	ts := etoroServer(t, etoroNow, nil)
	r := get[EtoroRiskResponse](t, ts, "/api/ui/etoro/risk", 200)
	if r.Ledger != "etoro-demo" || r.Policy.Version != "etoro-demo-2026-10-10" || !strings.Contains(r.PolicySrc, "EtoroDemoPolicy") ||
		r.Enforcement != "enforced" || r.Halted || r.HaltSource != "none" || r.HaltFile.Found {
		t.Fatalf("risk %+v", r)
	}
	if r.HaltFile.Path != filepath.Join(etoroTestdata, "risk-state.json") {
		t.Fatalf("halt path %q", r.HaltFile.Path)
	}
	if r.Account == nil || r.Account.Equity != 99949.9 || r.AccountAsOf != "2026-10-16T13:35:06Z" || len(r.Books) != 2 {
		t.Fatalf("account %+v as of %s books %d", r.Account, r.AccountAsOf, len(r.Books))
	}
	n, sp := r.Books[0], r.Books[1]
	if n.State != "flat" || n.Position.Units != 0 || n.ExposureUSD != 0 || n.LastClose != 29868.7 || n.OrdersToday != 1 ||
		n.LastOrder == nil || n.LastOrder.Status != "executed" || n.Limits.MaxOrderNotional != 1500 || !n.Limits.RequireMarketOpen {
		t.Fatalf("nsdq100 %+v", n)
	}
	if n.LastCheck == nil || n.LastCheck.BarDate != "2026-10-15" || !n.LastCheck.Allowed || n.LastCheck.PolicyVersion != "etoro-demo-2026-10-10" ||
		n.LastCheck.Order.Side != "sell" || n.LastCheck.Order.QtyBTC != 0.035799 {
		t.Fatalf("nsdq100 last check %+v", n.LastCheck)
	}
	if len(n.BlockedDays) != 0 || len(n.Findings) != 0 {
		t.Fatalf("nsdq100 findings %+v blocked %v", n.Findings, n.BlockedDays)
	}
	// SPX500: 0.140735 units x 7738.57 = 1089.08 USD, 1.09 % of 99949.90.
	if sp.State != "long" || sp.Position.PositionID != "3616683295" || sp.Position.Units != 0.140735 || math.Abs(sp.Position.OpenRate-7816.07) > 1e-6 ||
		math.Abs(sp.ExposureUSD-1089.08) > 0.01 || math.Abs(sp.ExposurePctEquity-1.0896) > 0.001 || sp.OrdersToday != 0 {
		t.Fatalf("spx500 %+v", sp)
	}
	// Exposure cap is 5 % of equity: 1.09 % is 21.8 % of it; the open
	// amount 1099.99 is 73 % of the 1500 USD notional cap.
	if math.Abs(sp.Utilization.Exposure-0.2179) > 0.001 || math.Abs(sp.Utilization.Notional-0.7333) > 0.001 {
		t.Fatalf("spx500 utilization %+v", sp.Utilization)
	}
	// The last check is the open of 10-12 (no check on hold days).
	if sp.LastCheck == nil || sp.LastCheck.FillDate != "2026-10-12" || sp.LastOrder == nil || sp.LastOrder.Kind != "open" {
		t.Fatalf("spx500 last check %+v order %+v", sp.LastCheck, sp.LastOrder)
	}
	if r.Blocks != 0 || r.Warnings != 0 {
		t.Fatalf("counts %d/%d", r.Blocks, r.Warnings)
	}
}

// A halt file next to the eToro ledger blocks the next run: the risk page
// shows it per book, and an invalid file is reported, not swallowed.
func TestEtoroRiskShowsHaltFile(t *testing.T) {
	dir := copyEtoroLedger(t)
	ts := etoroServer(t, etoroNow, func(s *Server) {
		s.Ledgers[1].Store = store.New(filepath.Join(dir, "ledger.jsonl"), "")
	})
	writeFile(t, filepath.Join(dir, "risk-state.json"), `{"halted":true,"reason":"eToro demo incident","by":"diego","at":"2026-10-16T15:00:00Z"}`)
	r := get[EtoroRiskResponse](t, ts, "/api/ui/etoro/risk", 200)
	if !r.Halted || r.HaltSource != "file" || !r.HaltFile.Halted || r.HaltFile.By != "diego" || !strings.Contains(r.HaltReason, "halt file, by diego") {
		t.Fatalf("halted %+v %q %+v", r.HaltSource, r.HaltReason, r.HaltFile)
	}
	if r.Blocks != 2 || r.Books[0].Findings[0].Rule != risk.RuleHalted || r.Books[0].Findings[0].Severity != risk.Block {
		t.Fatalf("blocks %d findings %+v", r.Blocks, r.Books[0].Findings)
	}
	ft := get[EtoroForwardTestsResponse](t, ts, "/api/ui/etoro/forward-tests", 200)
	if ft.Books[0].RiskBlocks != 1 {
		t.Fatalf("forward test blocks %+v", ft.Books[0])
	}
	writeFile(t, filepath.Join(dir, "risk-state.json"), `{"halted":true}`)
	r = get[EtoroRiskResponse](t, ts, "/api/ui/etoro/risk", 200)
	if r.HaltFile.Error == "" || !r.HaltFile.Found || r.Halted {
		t.Fatalf("invalid file must be reported: %+v", r.HaltFile)
	}
	// The health view fails on it, as the executor would (exit 2).
	h := get[EtoroHealthResponse](t, ts, "/api/ui/etoro/health", 200)
	if h.Status != datahealth.Fail {
		t.Fatalf("health with an invalid halt file: %+v", h.Checks)
	}
}

func TestEtoroHealth(t *testing.T) {
	ts := etoroServer(t, etoroNow, nil)
	h := get[EtoroHealthResponse](t, ts, "/api/ui/etoro/health", 200)
	// The fixture's SPX500 position was opened before protection existed
	// (phase C, 2026-10-19), so the protection check warns on it and the
	// view as a whole is warn; every other check is ok.
	if h.Ledger != "etoro-demo" || h.Venue != "etoro" || h.DueDate != "2026-10-16" || h.Status != datahealth.Warn || len(h.Checks) != 9 {
		t.Fatalf("health %+v", h)
	}
	want := map[string]string{
		"etoro.ledger":     "10 records",
		"etoro.halted":     "not halted",
		"etoro.coverage":   "every book recorded through 2026-10-16",
		"etoro.run":        "run of 2026-10-16 13:35 UTC ended exit=0",
		"etoro.reconcile":  "2026-10-16: drift on the run, clean on the operator's re-check",
		"etoro.intents":    "3 intents, all in the ledger",
		"etoro.upload":     "not configured (ETORO_EXECUTOR_S3_URI unset for the run of 2026-10-16 13:35 UTC)",
		"etoro.protection": "spx500 position 3616683295 (held without a stop since 2026-10-16 13:35 UTC): the next 09:35 New York run sets the disaster stop (protect on); to protect it now use the console's Trade page (protect)",
		"etoro.stop_out":   "none recorded on 2026-10-15 or 2026-10-16",
	}
	for _, c := range h.Checks {
		status := datahealth.OK
		if c.ID == "etoro.protection" {
			status = datahealth.Warn
		}
		if c.Area != "executor" || c.Status != status || c.Message != want[c.ID] {
			t.Fatalf("check %+v, want %s %q", c, status, want[c.ID])
		}
		delete(want, c.ID)
	}
	if len(want) != 0 {
		t.Fatalf("checks missing: %v", want)
	}
	if h.Thresholds["run_at"] != "09:35 New York" {
		t.Fatalf("thresholds %+v", h.Thresholds)
	}
	// Monday 11:00 New York with no 10-19 line: coverage fails.
	late := get[EtoroHealthResponse](t, etoroServer(t, time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC), nil), "/api/ui/etoro/health", 200)
	if late.Status != datahealth.Fail || late.DueDate != "2026-10-19" {
		t.Fatalf("late health %+v", late.Checks)
	}
}

func TestEtoroReconcile(t *testing.T) {
	ts := etoroServer(t, etoroNow, nil)
	r := get[EtoroReconcileResponse](t, ts, "/api/ui/etoro/reconcile", 200)
	if r.Ledger != "etoro-demo" || r.Dir != filepath.Join(etoroTestdata, "reconcile") || len(r.Reports) != 2 {
		t.Fatalf("reconcile %+v", r)
	}
	// Newest first: 10-16 drifted (close_not_in_history, a history lag)
	// and the 16:43 UTC re-check was clean.
	d := r.Reports[0]
	if d.Date != "2026-10-16" || d.Clean || d.Schema != "etoro-reconcile/v1" || d.GeneratedAt != "2026-10-16T13:35:06Z" ||
		d.Drift != 1 || d.Warn != 1 || d.PositionReads != 3 || d.SettleWaitedS != 10 || d.Error != "" {
		t.Fatalf("10-16 %+v", d)
	}
	if d.Findings[1].Code != "close_not_in_history" || d.Findings[1].Severity != "drift" || d.Findings[1].Book != "nsdq100" ||
		d.Findings[0].Code != "portfolio_lag" || d.Findings[0].Severity != "warn" {
		t.Fatalf("10-16 findings %+v", d.Findings)
	}
	if d.Account == nil || d.Account.Equity != 99950.28 || len(d.Books) != 2 || d.Books[1].LedgerPositionID != "3616683295" ||
		d.Books[1].LedgerUnits != 0.140735 || d.Books[0].LedgerState != "flat" || len(d.Books[0].AccountPositions) != 0 {
		t.Fatalf("10-16 account/books %+v %+v", d.Account, d.Books)
	}
	if d.Recheck == nil || !d.Recheck.Clean || d.Recheck.GeneratedAt != "2026-10-16T16:43:43Z" || len(d.Recheck.Findings) != 0 ||
		d.Recheck.Path != filepath.Join(etoroTestdata, "reconcile", "2026-10-16.recheck.json") || !d.EffectiveClean {
		t.Fatalf("10-16 recheck %+v effective %v", d.Recheck, d.EffectiveClean)
	}
	c := r.Reports[1]
	if c.Date != "2026-10-15" || !c.Clean || !c.EffectiveClean || c.Recheck != nil || c.Drift != 0 || c.Warn != 0 ||
		c.Books[0].LedgerPositionID != "3616683146" {
		t.Fatalf("10-15 %+v", c)
	}
	// No reconcile dir yet: an empty list, not an error.
	dir := copyEtoroLedger(t)
	empty := etoroServer(t, etoroNow, func(s *Server) { s.Ledgers[1].Store = store.New(filepath.Join(dir, "ledger.jsonl"), "") })
	if r := get[EtoroReconcileResponse](t, empty, "/api/ui/etoro/reconcile", 200); len(r.Reports) != 0 {
		t.Fatalf("empty %+v", r)
	}
}

func TestEtoroRiskReport(t *testing.T) {
	ts := etoroServer(t, etoroNow, nil)
	rep := get[map[string]json.RawMessage](t, ts, "/api/ui/etoro/risk-report", 200)
	var date, path, schema, asOf string
	_ = json.Unmarshal(rep["date"], &date)
	_ = json.Unmarshal(rep["path"], &path)
	_ = json.Unmarshal(rep["schema"], &schema)
	_ = json.Unmarshal(rep["as_of"], &asOf)
	if date != "2026-10-13" || path != filepath.Join(etoroTestdata, "risk", "2026-10-13", "risk.json") ||
		schema != "cfdrisk-report/v1" || asOf != "2026-10-13" {
		t.Fatalf("risk report date %q path %q schema %q as_of %q", date, path, schema, asOf)
	}
	// Passthrough: the report's own sections are there untouched.
	for _, k := range []string{"generated_at", "inputs", "params"} {
		if _, ok := rep[k]; !ok {
			t.Fatalf("missing %q in %v", k, keysOf(rep))
		}
	}
	dir := copyEtoroLedger(t)
	none := etoroServer(t, etoroNow, func(s *Server) { s.Ledgers[1].Store = store.New(filepath.Join(dir, "ledger.jsonl"), "") })
	if e := get[errorBody](t, none, "/api/ui/etoro/risk-report", 404); !strings.Contains(e.Error, "no risk report") {
		t.Fatalf("error %q", e.Error)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Controls over both venues: the eToro ledger is listed with its venue,
// halt-all writes both halt files and audits both ledgers.
func TestHaltAllReachesEtoro(t *testing.T) {
	bitso, etoro := t.TempDir(), copyEtoroLedger(t)
	src, err := os.ReadFile("testdata/ledger.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bitso, "ledger.jsonl"), string(src))
	ts := newTestServer(t, etoroNow, func(s *Server) {
		s.Ledgers = []Ledger{
			{Name: "stage", Store: store.New(filepath.Join(bitso, "ledger.jsonl"), "testdata/candles")},
			{Name: "etoro-demo", Store: store.New(filepath.Join(etoro, "ledger.jsonl"), ""), Venue: VenueEtoro},
		}
		s.OperatorToken = testToken
	})

	info := getStrategies(t, ts)
	if len(info.Ledgers) != 2 || info.Ledgers[0].Venue != "bitso" || info.Ledgers[1].Name != "etoro-demo" || info.Ledgers[1].Venue != "etoro" ||
		!info.Ledgers[1].ControlsEnabled || info.Ledgers[1].Remote {
		t.Fatalf("ledgers %+v", info.Ledgers)
	}
	if ks := info.KillSwitch; !ks.Enabled || len(ks.Targets) != 2 || ks.Targets[1] != "etoro-demo" {
		t.Fatalf("kill switch %+v", ks)
	}
	ci := get[ControlsInfo](t, ts, "/api/ui/controls?ledger=etoro-demo", 200)
	if ci.Venue != "etoro" || !ci.Enabled || ci.AuditPath != filepath.Join(etoro, audit.FileName) || ci.HaltFile.Path != filepath.Join(etoro, "risk-state.json") {
		t.Fatalf("controls %+v", ci)
	}

	code, b := post(t, ts, ctl{path: "/api/ui/risk/halt-all", token: testToken, body: body("venue-wide incident", "diego", HaltAllConfirm)})
	if code != http.StatusOK {
		t.Fatalf("halt-all = %d %s", code, b)
	}
	var resp HaltAllResponse
	_ = json.Unmarshal(b, &resp)
	if len(resp.Results) != 2 || resp.Results[0].Ledger != "stage" || resp.Results[0].Outcome != "halted" ||
		resp.Results[1].Ledger != "etoro-demo" || resp.Results[1].Outcome != "halted" || !resp.Results[1].HaltFile.Halted {
		t.Fatalf("results %+v", resp.Results)
	}
	for _, dir := range []string{bitso, etoro} {
		// The executors' own reader accepts both files.
		h, found, err := risk.LoadHaltState(filepath.Join(dir, "risk-state.json"))
		if err != nil || !found || !h.Halted || h.By != "diego" || h.Reason != "venue-wide incident" || h.At != "2026-10-16T17:00:00Z" {
			t.Fatalf("%s halt file %+v %v %v", dir, h, found, err)
		}
		e, err := audit.Tail(filepath.Join(dir, audit.FileName), 10)
		if err != nil || len(e) != 2 || e[0].Outcome != audit.Done || e[1].Outcome != audit.Requested || e[0].Group != resp.Group || e[0].Action != "halt_all" {
			t.Fatalf("%s audit %+v %v", dir, e, err)
		}
	}
	if e, _ := audit.Tail(filepath.Join(etoro, audit.FileName), 1); e[0].Ledger != "etoro-demo" {
		t.Fatalf("etoro audit ledger %+v", e[0])
	}
	// The views see it: eToro risk halted by file, health warns.
	if r := get[EtoroRiskResponse](t, ts, "/api/ui/etoro/risk?ledger=etoro-demo", 200); !r.Halted || r.HaltSource != "file" {
		t.Fatalf("etoro risk after halt-all %+v", r.HaltFile)
	}
	if h := get[EtoroHealthResponse](t, ts, "/api/ui/etoro/health", 200); h.Status != datahealth.Warn {
		t.Fatalf("etoro health after halt-all %+v", h.Checks)
	}
	if info := getStrategies(t, ts); len(info.KillSwitch.AlreadyHalted) != 2 {
		t.Fatalf("already halted %v", info.KillSwitch.AlreadyHalted)
	}
}

// Halt and resume on the eToro ledger alone: the confirm phrase is its
// name, the Bitso ledger is untouched, the audit sits next to the eToro
// ledger.
func TestEtoroHaltAndResume(t *testing.T) {
	bitso, etoro := t.TempDir(), copyEtoroLedger(t)
	src, err := os.ReadFile("testdata/ledger.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bitso, "ledger.jsonl"), string(src))
	ts := newTestServer(t, etoroNow, func(s *Server) {
		s.Ledgers = []Ledger{
			{Name: "stage", Store: store.New(filepath.Join(bitso, "ledger.jsonl"), "testdata/candles")},
			{Name: "etoro-demo", Store: store.New(filepath.Join(etoro, "ledger.jsonl"), ""), Venue: VenueEtoro},
		}
		s.OperatorToken = testToken
	})
	// Wrong confirm (the Bitso name) is refused and audited on the eToro log.
	code, b := post(t, ts, ctl{path: "/api/ui/risk/halt?ledger=etoro-demo", token: testToken, body: body("eToro demo incident", "diego", "stage")})
	if code != http.StatusBadRequest || !strings.Contains(string(b), "etoro-demo") {
		t.Fatalf("wrong confirm = %d %s", code, b)
	}
	code, b = post(t, ts, ctl{path: "/api/ui/risk/halt?ledger=etoro-demo", token: testToken, origin: "http://127.0.0.1:5173",
		body: body("eToro demo incident", "diego", "etoro-demo")})
	if code != http.StatusOK {
		t.Fatalf("halt = %d %s", code, b)
	}
	var resp ControlResponse
	_ = json.Unmarshal(b, &resp)
	if resp.Ledger != "etoro-demo" || !resp.HaltFile.Halted || resp.HaltFile.Path != filepath.Join(etoro, "risk-state.json") || resp.Audit.Outcome != audit.Done {
		t.Fatalf("halt response %+v", resp)
	}
	if h, found, err := risk.LoadHaltState(filepath.Join(etoro, "risk-state.json")); err != nil || !found || !h.Halted || h.Reason != "eToro demo incident" {
		t.Fatalf("etoro halt file %+v %v %v", h, found, err)
	}
	if _, err := os.Stat(filepath.Join(bitso, "risk-state.json")); !os.IsNotExist(err) {
		t.Fatal("the Bitso halt file must not be written")
	}
	if _, err := os.Stat(filepath.Join(bitso, audit.FileName)); !os.IsNotExist(err) {
		t.Fatal("the Bitso audit log must not be written")
	}
	if r := get[RiskResponse](t, ts, "/api/ui/risk", 200); r.Halted {
		t.Fatal("the Bitso risk page must not be halted")
	}
	if r := get[EtoroRiskResponse](t, ts, "/api/ui/etoro/risk", 200); !r.Halted || r.HaltSource != "file" {
		t.Fatalf("etoro risk %+v", r.HaltFile)
	}
	code, _ = post(t, ts, ctl{path: "/api/ui/risk/halt?ledger=etoro-demo", token: testToken, body: body("second halt reason", "diego", "etoro-demo")})
	if code != http.StatusConflict {
		t.Fatalf("second halt = %d", code)
	}
	code, b = post(t, ts, ctl{path: "/api/ui/risk/resume?ledger=etoro-demo", token: testToken, body: body("incident resolved", "diego", "etoro-demo")})
	if code != http.StatusOK {
		t.Fatalf("resume = %d %s", code, b)
	}
	if h, _, err := risk.LoadHaltState(filepath.Join(etoro, "risk-state.json")); err != nil || h.Halted {
		t.Fatalf("after resume %+v %v", h, err)
	}
	e, err := audit.Tail(filepath.Join(etoro, audit.FileName), 20)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for i := len(e) - 1; i >= 0; i-- {
		got = append(got, e[i].Action+":"+e[i].Outcome)
		if e[i].Ledger != "etoro-demo" {
			t.Fatalf("audit ledger %+v", e[i])
		}
	}
	if want := "halt:refused halt:requested halt:done halt:refused resume:requested resume:done"; strings.Join(got, " ") != want {
		t.Fatalf("audit = %v\nwant %s", got, want)
	}
	ci := get[ControlsInfo](t, ts, "/api/ui/controls?ledger=etoro-demo", 200)
	if len(ci.Audit) != 6 || ci.HaltFile.Halted || ci.Venue != "etoro" {
		t.Fatalf("controls after %+v", ci)
	}
}

func TestParseEtoroCandles(t *testing.T) {
	rows, err := store.ParseCandles(strings.NewReader("date,book,open,high,low,close,volume,vwap,trade_count,bucket_start_utc\n" +
		"2026-10-14,NSDQ100,30580.3,30675.1,29860.5,29917.2,,,0,2026-10-14T00:00:00Z\n" +
		"2026-10-15,NSDQ100,29911.9,29989,29406.3,29868.7,12.5,29800,3,2026-10-15T00:00:00Z\n"))
	if err != nil || len(rows) != 2 || rows[0].Volume != 0 || rows[0].Close != 29917.2 || rows[1].Volume != 12.5 || rows[1].TradeCount != 3 {
		t.Fatalf("%+v %v", rows, err)
	}
	if _, err := store.ParseCandles(strings.NewReader("date,open\n2026-10-14,1\n")); err == nil {
		t.Fatal("missing columns must fail")
	}
	if _, err := store.ParseCandles(strings.NewReader("date,open,high,low,close,volume\n2026-10-14,x,1,1,1,\n")); err == nil {
		t.Fatal("a non-numeric price must fail")
	}
}
