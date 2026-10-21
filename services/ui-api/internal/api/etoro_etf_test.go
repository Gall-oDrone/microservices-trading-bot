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
