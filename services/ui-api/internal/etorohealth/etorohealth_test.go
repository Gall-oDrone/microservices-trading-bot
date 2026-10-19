package etorohealth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/ui-api/internal/datahealth"
)

// ny builds a New York local time.
func ny(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, mktcal.NewYork)
}

// dir is a fake <ledger dir> built per test.
type dir struct {
	t    *testing.T
	path string
}

func newDir(t *testing.T) dir {
	t.Helper()
	return dir{t, t.TempDir()}
}

func (d dir) ledger() string { return filepath.Join(d.path, "ledger.jsonl") }

// record appends one demo line for book with the given fill date and
// action; an open/close also carries an executed order with ref.
func (d dir) record(book, barDate, fillDate, action, ref string) {
	d.t.Helper()
	r := etoroledger.Record{Mode: etoroledger.ModeDemo, Book: book, Instrument: etoroledger.Instrument{Symbol: strings.ToUpper(book)},
		Decision: etoroledger.Decision{BarDate: barDate, FillDate: fillDate},
		Demo:     &etoroledger.Demo{Action: action, PositionBefore: etoroledger.Flat(), PositionAfter: etoroledger.Flat()}}
	if ref != "" {
		r.Demo.Order = &etoroledger.Order{Kind: action, ClientRef: ref, Status: "executed"}
	}
	b, err := json.Marshal(r)
	if err != nil {
		d.t.Fatal(err)
	}
	f, err := os.OpenFile(d.ledger(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		d.t.Fatal(err)
	}
	defer f.Close()
	f.Write(append(b, '\n'))
}

func (d dir) runLog(at time.Time, body string) {
	d.t.Helper()
	name := "run-" + at.UTC().Format("20060102T150405Z") + ".log"
	if err := os.WriteFile(filepath.Join(d.path, name), []byte(body), 0o644); err != nil {
		d.t.Fatal(err)
	}
}

func (d dir) reconcile(name string, clean bool, findings ...map[string]string) {
	d.t.Helper()
	rep := map[string]any{"schema": "etoro-reconcile/v1", "clean": clean, "findings": findings}
	if findings == nil {
		rep["findings"] = []any{}
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	if err := os.MkdirAll(filepath.Join(d.path, "reconcile"), 0o755); err != nil {
		d.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.path, "reconcile", name), b, 0o644); err != nil {
		d.t.Fatal(err)
	}
}

func (d dir) intent(ref, book string, at time.Time) {
	d.t.Helper()
	if _, _, err := etoroledger.RecordIntent(filepath.Join(d.path, "intents"),
		etoroledger.Intent{ClientRef: ref, Book: book, Kind: etoroledger.ActionOpen, IntentAt: at}); err != nil {
		d.t.Fatal(err)
	}
}

func (d dir) halt(content string) {
	d.t.Helper()
	if err := os.WriteFile(filepath.Join(d.path, "risk-state.json"), []byte(content), 0o644); err != nil {
		d.t.Fatal(err)
	}
}

func byID(checks []Check) map[string]Check {
	m := map[string]Check{}
	for _, c := range checks {
		m[c.ID] = c
	}
	return m
}

func want(t *testing.T, c Check, status, contains string) {
	t.Helper()
	if c.Status != status || !strings.Contains(c.Message, contains) {
		t.Fatalf("%s: want %s containing %q, got %s %q", c.ID, status, contains, c.Status, c.Message)
	}
}

// soak is the real tree as of Wed 2026-10-14 after the run: opens on Mon
// 10-12 (drift then a clean re-check), holds on Tue and Wed, clean.
func soak(t *testing.T) dir {
	d := newDir(t)
	d.record("nsdq100", "2026-10-09", "2026-10-12", etoroledger.ActionOpen, "sma50-nsdq100-2026-10-12-open")
	d.record("spx500", "2026-10-09", "2026-10-12", etoroledger.ActionOpen, "sma50-spx500-2026-10-12-open")
	d.record("nsdq100", "2026-10-12", "2026-10-13", etoroledger.ActionNone, "")
	d.record("spx500", "2026-10-12", "2026-10-13", etoroledger.ActionNone, "")
	d.record("nsdq100", "2026-10-13", "2026-10-14", etoroledger.ActionNone, "")
	d.record("spx500", "2026-10-13", "2026-10-14", etoroledger.ActionNone, "")
	d.intent("sma50-nsdq100-2026-10-12-open", "nsdq100", ny(2026, 10, 12, 9, 35))
	d.intent("sma50-spx500-2026-10-12-open", "spx500", ny(2026, 10, 12, 9, 35))
	d.runLog(ny(2026, 10, 12, 9, 35), "etoro-daily-executor ...\nreconcile=drift ./x/2026-10-12.json\nexit=0\n")
	d.runLog(ny(2026, 10, 13, 9, 35), "etoro-daily-executor ...\nreconcile=clean\nexit=0\n")
	d.runLog(ny(2026, 10, 14, 9, 35), "etoro-daily-executor ...\nreconcile=clean\nexit=0\n")
	d.reconcile("2026-10-12.json", false, map[string]string{"severity": "drift", "book": "nsdq100", "code": "missing_position", "message": "ledger holds position 1 on NSDQ100 but the account does not"})
	d.reconcile("2026-10-12.recheck.json", true)
	d.reconcile("2026-10-13.json", true)
	d.reconcile("2026-10-14.json", true)
	return d
}

func TestDueDate(t *testing.T) {
	var o Options
	cases := []struct {
		now  time.Time
		want string
	}{
		{ny(2026, 10, 14, 9, 59), "2026-10-13"},  // Wed before 10:00: Tuesday is due
		{ny(2026, 10, 14, 10, 0), "2026-10-14"},  // Wed at 10:00: today
		{ny(2026, 10, 17, 12, 0), "2026-10-16"},  // Saturday: Friday
		{ny(2026, 10, 12, 8, 0), "2026-10-09"},   // Monday morning: Friday
		{ny(2026, 11, 26, 15, 0), "2026-11-25"},  // Thanksgiving: Wednesday
		{ny(2026, 11, 27, 10, 30), "2026-11-27"}, // Black Friday (early close) is a trading day
	}
	for _, c := range cases {
		if got := o.DueDate(c.now).Format("2006-01-02"); got != c.want {
			t.Errorf("DueDate(%s) = %s, want %s", c.now.Format(time.RFC3339), got, c.want)
		}
	}
}

func TestCleanSoak(t *testing.T) {
	d := soak(t)
	checks := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 14, 13, 0)))
	for _, id := range []string{IDLedger, IDHalt, IDCoverage, IDRun, IDReconcile, IDIntents, IDUpload} {
		if c := checks[id]; c.Status != datahealth.OK {
			t.Errorf("%s: want ok, got %s %q", id, c.Status, c.Message)
		}
	}
	want(t, checks[IDCoverage], datahealth.OK, "through 2026-10-14")
	want(t, checks[IDIntents], datahealth.OK, "2 intents, all in the ledger")
	want(t, checks[IDUpload], datahealth.OK, "not configured")

	// Monday 10-12 alone: the drift report is covered by the clean re-check.
	os.Remove(filepath.Join(d.path, "reconcile", "2026-10-13.json"))
	os.Remove(filepath.Join(d.path, "reconcile", "2026-10-14.json"))
	checks = byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 12, 15, 0)))
	want(t, checks[IDReconcile], datahealth.OK, "clean on the operator's re-check")
	os.Remove(filepath.Join(d.path, "reconcile", "2026-10-12.recheck.json"))
	checks = byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 12, 15, 0)))
	want(t, checks[IDReconcile], datahealth.Fail, "nsdq100 missing_position")
}

func TestMissedRun(t *testing.T) {
	d := soak(t)
	// Thursday 10-15: the 09:35 run never happened. Before 10:00 nothing is
	// due; from 10:00 both books are missing; a later partial run leaves one.
	before := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 9, 50)))
	want(t, before[IDCoverage], datahealth.OK, "through 2026-10-14")
	after := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 10, 0)))
	want(t, after[IDCoverage], datahealth.Fail, "nsdq100 2026-10-15, spx500 2026-10-15")
	d.record("nsdq100", "2026-10-14", "2026-10-15", etoroledger.ActionNone, "")
	partial := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 11, 0)))
	want(t, partial[IDCoverage], datahealth.Fail, "no ledger line for spx500 2026-10-15:")
	// A hole stays visible for Days trading days (the weekend does not count).
	later := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 19, 12, 0)))
	want(t, later[IDCoverage], datahealth.Fail, "spx500 2026-10-15")
	d.record("spx500", "2026-10-14", "2026-10-15", etoroledger.ActionNone, "")
	fixed := byID(Evaluate(Options{LedgerPath: d.ledger(), Days: 1}, ny(2026, 10, 15, 11, 0)))
	want(t, fixed[IDCoverage], datahealth.OK, "through 2026-10-15")
}

func TestFailedAndHangingRun(t *testing.T) {
	d := soak(t)
	d.runLog(ny(2026, 10, 15, 9, 35), "[nsdq100] FAILED: open needs the NYSE cash session on 2026-10-15 (nothing recorded; re-run today to retry or resume)\nreconcile=clean\nexit=1\n")
	c := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 9, 40)))
	want(t, c[IDRun], datahealth.Fail, "ended exit=1: [nsdq100] FAILED: open needs the NYSE cash session")
	// A later clean run of the same day resolves it.
	d.runLog(ny(2026, 10, 15, 10, 5), "exit=0\n")
	c = byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 10, 10)))
	want(t, c[IDRun], datahealth.OK, "ended exit=0")
	// No exit line: in progress for 15 minutes, then a failure.
	d.runLog(ny(2026, 10, 15, 10, 20), "etoro-daily-executor ...\n")
	c = byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 10, 25)))
	want(t, c[IDRun], datahealth.OK, "in progress")
	c = byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 10, 40)))
	want(t, c[IDRun], datahealth.Fail, "has no exit line after 20m0s")
}

func TestReconcileMissingAndWarnings(t *testing.T) {
	d := soak(t)
	// Thursday's run ended exit=0 and recorded, but no report was written.
	d.record("nsdq100", "2026-10-14", "2026-10-15", etoroledger.ActionNone, "")
	d.record("spx500", "2026-10-14", "2026-10-15", etoroledger.ActionNone, "")
	d.runLog(ny(2026, 10, 15, 9, 35), "reconcile=error\nexit=0\n")
	c := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 10, 0)))
	want(t, c[IDReconcile], datahealth.Warn, "left no reconcile/2026-10-15.json")
	// Warnings: portfolio_lag is normal, other_instruments is not.
	d.reconcile("2026-10-15.json", true, map[string]string{"severity": "warn", "book": "nsdq100", "code": "portfolio_lag", "message": "the portfolio showed the ledger's latest order only after 3 reads (10 s)"})
	c = byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 10, 0)))
	want(t, c[IDReconcile], datahealth.OK, "2026-10-15: clean")
	d.reconcile("2026-10-15.json", true, map[string]string{"severity": "warn", "code": "other_instruments", "message": "positions on instruments the executor does not trade: [77 (instrument 1001)]"})
	c = byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 10, 0)))
	want(t, c[IDReconcile], datahealth.Warn, "other_instruments")
}

func TestUnrecordedIntent(t *testing.T) {
	d := soak(t)
	d.intent("sma50-nsdq100-2026-10-15-close", "nsdq100", ny(2026, 10, 15, 9, 35))
	fresh := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 9, 40)))
	want(t, fresh[IDIntents], datahealth.OK, "3 intents") // a run may still be writing the ledger
	stale := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 9, 50)))
	want(t, stale[IDIntents], datahealth.Fail, "sma50-nsdq100-2026-10-15-close (sent 2026-10-15 13:35 UTC): re-run the executor")
}

func TestHaltFile(t *testing.T) {
	d := soak(t)
	d.halt(`{"halted":true,"reason":"drill","by":"diego","at":"2026-10-15T12:00:00Z"}` + "\n")
	c := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 8, 0)))
	want(t, c[IDHalt], datahealth.Warn, "drill (by diego at 2026-10-15T12:00:00Z, halt file)")
	d.halt(`{"halted":true}`)
	c = byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 8, 0)))
	want(t, c[IDHalt], datahealth.Fail, "halted needs reason, by, at: the executor refuses to run")
	d.halt(`{"halted":false}`)
	c = byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 8, 0)))
	want(t, c[IDHalt], datahealth.OK, "not halted")
}

func TestEmptyTree(t *testing.T) {
	d := newDir(t)
	c := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 12, 0)))
	want(t, c[IDLedger], datahealth.Warn, "no ledger yet")
	want(t, c[IDCoverage], datahealth.Warn, "no demo records yet")
	want(t, c[IDRun], datahealth.Warn, "no run log yet")
	want(t, c[IDReconcile], datahealth.Warn, "no reconcile reports yet")
	want(t, c[IDIntents], datahealth.OK, "0 intents")
	// A ledger that is not JSON is a failure, not a quiet warning.
	os.WriteFile(d.ledger(), []byte("{not json\n"), 0o644)
	c = byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 15, 12, 0)))
	if c[IDLedger].Status != datahealth.Fail {
		t.Fatalf("corrupt ledger: %+v", c[IDLedger])
	}
}

func TestUploadCheck(t *testing.T) {
	d := soak(t)
	d.runLog(ny(2026, 10, 19, 9, 35), "reconcile=clean\nupload=ok s3://b/daily-executor/etoro-demo\nexit=0\n")
	c := byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 19, 10, 0)))
	want(t, c[IDUpload], datahealth.OK, "copied to S3")
	d.runLog(ny(2026, 10, 19, 9, 40), "reconcile=clean\nupload=failed s3://b/daily-executor/etoro-demo\nexit=0\n")
	c = byID(Evaluate(Options{LedgerPath: d.ledger()}, ny(2026, 10, 19, 10, 0)))
	want(t, c[IDUpload], datahealth.Warn, "did not reach S3 (upload=failed)")
	// The run itself is still fine: a failed copy never fails the run.
	want(t, c[IDRun], datahealth.OK, "ended exit=0")
}

// Protection plan phase C5 (2026-10-19).
func (d dir) rawRecord(r etoroledger.Record) {
	d.t.Helper()
	r.Mode = etoroledger.ModeDemo
	b, err := json.Marshal(r)
	if err != nil {
		d.t.Fatal(err)
	}
	f, err := os.OpenFile(d.ledger(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		d.t.Fatal(err)
	}
	defer f.Close()
	f.Write(append(b, '\n'))
}

func TestProtectionCheck(t *testing.T) {
	now := ny(2026, 10, 14, 13, 0)
	held := func(book, pid string, sl float64, recordedAt time.Time) etoroledger.Record {
		return etoroledger.Record{Book: book, RecordedAt: recordedAt.UTC().Format(time.RFC3339), Instrument: etoroledger.Instrument{Symbol: strings.ToUpper(book)},
			Decision: etoroledger.Decision{BarDate: "2026-10-13", FillDate: "2026-10-14"},
			Demo: &etoroledger.Demo{Action: etoroledger.ActionOpen, PositionBefore: etoroledger.Flat(),
				PositionAfter: etoroledger.Position{State: "long", PositionID: pid, Units: 0.03, StopLossRate: sl},
				Order:         &etoroledger.Order{Kind: etoroledger.ActionOpen, ClientRef: "sma50-" + book + "-2026-10-14-open", Status: "executed"}}}
	}
	t.Run("flat", func(t *testing.T) {
		d := newDir(t)
		d.record("nsdq100", "2026-10-13", "2026-10-14", etoroledger.ActionNone, "")
		c := byID(Evaluate(Options{LedgerPath: d.ledger()}, now))
		want(t, c[IDProtection], datahealth.OK, "no position held")
		want(t, c[IDStopOut], datahealth.OK, "none recorded")
	})
	t.Run("protected", func(t *testing.T) {
		d := newDir(t)
		d.rawRecord(held("nsdq100", "11", 27000, now.Add(-2*time.Hour)))
		want(t, byID(Evaluate(Options{LedgerPath: d.ledger()}, now))[IDProtection], datahealth.OK, "nsdq100 stop 27000.00")
	})
	t.Run("bare beyond the grace warns", func(t *testing.T) {
		d := newDir(t)
		d.rawRecord(held("nsdq100", "11", 0, now.Add(-2*time.Hour)))
		d.rawRecord(held("spx500", "22", 7000, now.Add(-2*time.Hour)))
		c := byID(Evaluate(Options{LedgerPath: d.ledger()}, now))[IDProtection]
		want(t, c, datahealth.Warn, "nsdq100 position 11")
		if strings.Contains(c.Message, "spx500") {
			t.Fatalf("the protected book must not be named: %q", c.Message)
		}
	})
	t.Run("bare inside the grace is quiet", func(t *testing.T) {
		d := newDir(t)
		d.rawRecord(held("nsdq100", "11", 0, now.Add(-5*time.Minute)))
		want(t, byID(Evaluate(Options{LedgerPath: d.ledger()}, now))[IDProtection], datahealth.OK, "no position held")
	})
	t.Run("a later venue close clears it", func(t *testing.T) {
		d := newDir(t)
		d.rawRecord(held("nsdq100", "11", 0, now.Add(-26*time.Hour)))
		d.rawRecord(etoroledger.Record{Book: "nsdq100", RecordedAt: now.Add(-time.Hour).UTC().Format(time.RFC3339), Instrument: etoroledger.Instrument{Symbol: "NSDQ100"},
			Decision: etoroledger.Decision{BarDate: "2026-10-13", FillDate: "2026-10-14"},
			Demo: &etoroledger.Demo{Action: etoroledger.ActionVenueClose, PositionBefore: etoroledger.Position{State: "long", PositionID: "11"}, PositionAfter: etoroledger.Flat(),
				VenueClose: &etoroledger.VenueClose{Kind: etoroledger.VenueCloseStoppedOut, StopLossRate: 27000, CloseRate: 26990, NetProfit: -88.2, Fees: 0.5},
				Order:      &etoroledger.Order{Kind: etoroledger.ActionVenueClose, Status: "executed", AvgPrice: 26990}}})
		c := byID(Evaluate(Options{LedgerPath: d.ledger()}, now))
		want(t, c[IDProtection], datahealth.OK, "no position held")
		want(t, c[IDStopOut], datahealth.OK, "nsdq100: position 11 closed by eToro at 26990.00 on 2026-10-14 (stopped_out, net -88.70)")
	})
}
