package api

import (
	"path/filepath"
	"strings"
	"testing"

	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/ui-api/internal/store"
)

// A second eToro ledger (the ETF cell) is served next to the CFD one under
// ?ledger=, with the policy the executor applies to it (the ETF policy),
// while the default eToro ledger and its CFD policy are unchanged.
func TestEtoroSecondLedgerETFPolicy(t *testing.T) {
	dir := t.TempDir()
	led, err := etoroledger.Open(filepath.Join(dir, "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	rec := etoroledger.Record{Schema: etoroledger.Schema, RecordedAt: "2026-10-22T13:38:10Z", Mode: etoroledger.ModeDemo, Venue: "etoro",
		Book: "spy", Instrument: etoroledger.Instrument{Symbol: "SPY", ID: 3000}, Prereg: "FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md",
		Decision: etoroledger.Decision{BarDate: "2026-10-21", FillDate: "2026-10-22", Close: 771.6, SMA: 768.27, Signal: "long", PrevSignal: "flat", Action: "buy"},
		Paper:    etoroledger.Paper{ForwardStart: "2026-10-22", Product: "etf", FeeBps: 15, SpreadRTBps: 0.26, CostsStatus: "FROZEN", Equity: 1, HoldEquity: 1, Position: "flat", PendingAction: "buy"},
		Demo:     &etoroledger.Demo{Env: "demo", Target: "long", Action: etoroledger.ActionNone, PositionBefore: etoroledger.Flat(), PositionAfter: etoroledger.Flat()},
	}
	if err := led.Append(rec); err != nil {
		t.Fatal(err)
	}
	ts := etoroServer(t, etoroNow, func(s *Server) {
		s.Ledgers = append(s.Ledgers, Ledger{Name: "etoro-demo-etf", Store: store.New(led.Path(), ""), Venue: VenueEtoro})
	})
	// Default: the first eToro ledger, CFD policy.
	r := get[EtoroRiskResponse](t, ts, "/api/ui/etoro/risk", 200)
	if r.Ledger != "etoro-demo" || r.Policy.Version != "etoro-demo-2026-10-10" {
		t.Fatalf("default ledger %s policy %s", r.Ledger, r.Policy.Version)
	}
	// The ETF ledger by name, ETF policy.
	e := get[EtoroRiskResponse](t, ts, "/api/ui/etoro/risk?ledger=etoro-demo-etf", 200)
	if e.Ledger != "etoro-demo-etf" || e.Policy.Version != "etoro-demo-etf-2026-10-21" || !strings.Contains(e.PolicySrc, "EtoroDemoETFPolicy") {
		t.Fatalf("etf ledger %s policy %s src %s", e.Ledger, e.Policy.Version, e.PolicySrc)
	}
	if len(e.Books) != 1 || e.Books[0].Book != "spy" || e.Books[0].Limits.MaxOrderNotional != 1500 || e.Books[0].Limits.RequireStopLoss {
		t.Fatalf("etf books %+v", e.Books)
	}
	ft := get[EtoroForwardTestsResponse](t, ts, "/api/ui/etoro/forward-tests?ledger=etoro-demo-etf", 200)
	if ft.Ledger != "etoro-demo-etf" || len(ft.Books) != 1 || ft.Books[0].Book != "spy" {
		t.Fatalf("forward tests %+v", ft)
	}
	get[errorBody](t, ts, "/api/ui/etoro/risk?ledger=stage", 400)
}

// The Risk page shows the policy the executor's newest check ran under: a
// ledger whose last check names the protected policy (phase C, 2026-10-19)
// shows it with no policy_mismatch finding, while the testdata ledger, whose
// checks predate it, keeps showing the unprotected one. A CFD ledger with
// no recorded check falls back to the protected default.
func TestEtoroRiskFollowsRecordedPolicyVersion(t *testing.T) {
	dir := copyEtoroLedger(t)
	led, err := etoroledger.Open(filepath.Join(dir, "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	rec := etoroledger.Record{Schema: etoroledger.Schema, RecordedAt: "2026-10-21T13:35:13Z", Mode: etoroledger.ModeDemo, Venue: "etoro",
		Book: "spx500", Instrument: etoroledger.Instrument{Symbol: "SPX500", ID: 27}, Prereg: "FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md",
		Decision: etoroledger.Decision{BarDate: "2026-10-20", FillDate: "2026-10-21", Close: 7742.34, SMA: 7705.13, Signal: "long", PrevSignal: "flat", Action: "buy"},
		Paper:    etoroledger.Paper{ForwardStart: "2026-10-13", SpreadRTBps: 7.4, CostsStatus: "FROZEN", Equity: 0.977, HoldEquity: 0.989, Position: "flat", PendingAction: "buy"},
		Demo: &etoroledger.Demo{Env: "demo", Target: "long", Action: etoroledger.ActionNone, PositionBefore: etoroledger.Flat(), PositionAfter: etoroledger.Flat(),
			Risk: &etoroledger.RiskCheck{PolicyVersion: "etoro-demo-2026-10-19", Allowed: true}},
	}
	if err := led.Append(rec); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	ts := etoroServer(t, etoroNow, func(s *Server) {
		s.Ledgers = append(s.Ledgers,
			Ledger{Name: "etoro-protected", Store: store.New(filepath.Join(dir, "ledger.jsonl"), ""), Venue: VenueEtoro},
			Ledger{Name: "etoro-empty", Store: store.New(filepath.Join(empty, "ledger.jsonl"), ""), Venue: VenueEtoro})
	})
	r := get[EtoroRiskResponse](t, ts, "/api/ui/etoro/risk?ledger=etoro-protected", 200)
	if r.Policy.Version != "etoro-demo-2026-10-19" || !strings.Contains(r.PolicySrc, "EtoroDemoProtectedPolicy") {
		t.Fatalf("policy %s (%s)", r.Policy.Version, r.PolicySrc)
	}
	for _, b := range r.Books {
		mismatch := false
		for _, f := range b.Findings {
			mismatch = mismatch || f.Rule == RulePolicyMismatch
		}
		// spx500 was checked under the shown policy; nsdq100's last check
		// (2026-10-15) predates it, which the finding correctly flags.
		if want := b.Book == "nsdq100"; mismatch != want {
			t.Fatalf("%s: mismatch finding %v, want %v: %+v", b.Book, mismatch, want, b.Findings)
		}
	}
	if r := get[EtoroRiskResponse](t, ts, "/api/ui/etoro/risk", 200); r.Policy.Version != "etoro-demo-2026-10-10" {
		t.Fatalf("testdata ledger (checks before phase C) must keep its policy: %s", r.Policy.Version)
	}
	if r := get[EtoroRiskResponse](t, ts, "/api/ui/etoro/risk?ledger=etoro-empty", 200); r.Policy.Version != "etoro-demo-2026-10-19" {
		t.Fatalf("no recorded check: expected the protected default, got %s", r.Policy.Version)
	}
}
