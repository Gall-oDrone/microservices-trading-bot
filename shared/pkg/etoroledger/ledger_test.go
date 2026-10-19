package etoroledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rec(book, bar, pos string) Record {
	r := Record{Mode: ModeDemo, Venue: "etoro", Book: book, Decision: Decision{BarDate: bar, FillDate: bar + "+1"}}
	r.Demo = &Demo{Env: "demo", PositionAfter: Position{State: pos}}
	if pos == "long" {
		r.Demo.Order = &Order{Kind: ActionOpen}
	}
	return r
}

func TestLedgerAppendDedupAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []Record{rec("spx500", "2026-10-13", "long"), rec("nsdq100", "2026-10-12", "long"), rec("nsdq100", "2026-10-13", "flat")} {
		if err := l.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Append(rec("nsdq100", "2026-10-13", "long")); err == nil {
		t.Fatal("second line for the same day accepted")
	}
	l2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rs := l2.Records()
	if len(rs) != 3 || rs[0].Book != "nsdq100" || rs[0].Decision.BarDate != "2026-10-12" || rs[0].Schema != Schema {
		t.Fatalf("records %+v", rs)
	}
	if p := l2.LastDemoPosition("nsdq100"); p.State != "flat" {
		t.Fatalf("nsdq100 last %+v", p)
	}
	if p := l2.LastDemoPosition("spx500"); p.State != "long" {
		t.Fatalf("spx500 last %+v", p)
	}
	if p := l2.LastDemoPosition("ger40"); p.State != "flat" {
		t.Fatalf("unknown book %+v", p)
	}
	if n := LegsOn(rs, "nsdq100", "2026-10-12+1"); n != 1 {
		t.Fatalf("legs %d", n)
	}
	// A malformed line is reported with its number.
	if err := os.WriteFile(path, []byte("{}\nnot json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(path); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("malformed: %v", err)
	}
	if rs, err := ReadFile(filepath.Join(t.TempDir(), "none.jsonl")); err != nil || rs != nil {
		t.Fatalf("missing file: %v %v", rs, err)
	}
}

func TestIntentFirstWriteWins(t *testing.T) {
	dir := IntentDir(filepath.Join(t.TempDir(), "ledger.jsonl"))
	ref := ClientRef("nsdq100", "2026-10-13", ActionOpen)
	if ref != "sma50-nsdq100-2026-10-13-open" {
		t.Fatalf("ref %q", ref)
	}
	t0 := time.Date(2026, 10, 13, 13, 35, 0, 0, time.UTC)
	in, existed, err := RecordIntent(dir, Intent{ClientRef: ref, RequestID: "r1", Kind: ActionOpen, Book: "nsdq100", IntentAt: t0})
	if err != nil || existed || in.RequestID != "r1" {
		t.Fatalf("first: %+v %v %v", in, existed, err)
	}
	// A re-run must get the original IntentAt back, not its own clock.
	in, existed, err = RecordIntent(dir, Intent{ClientRef: ref, RequestID: "r1", Kind: ActionOpen, Book: "nsdq100", IntentAt: t0.Add(time.Hour)})
	if err != nil || !existed || !in.IntentAt.Equal(t0) {
		t.Fatalf("second: %+v %v %v", in, existed, err)
	}
	if _, ok, err := LoadIntent(dir, "sma50-spx500-2026-10-13-open"); ok || err != nil {
		t.Fatalf("absent intent: %v %v", ok, err)
	}
	all, err := Intents(dir)
	if err != nil || len(all) != 1 || all[0].ClientRef != ref {
		t.Fatalf("intents %+v %v", all, err)
	}
	if _, _, err := RecordIntent(dir, Intent{ClientRef: "x"}); err == nil {
		t.Fatal("intent without intent_at accepted")
	}
}

// uiRec is an assisted trade from the console on nyDate (bar_date and
// fill_date are both the trade's New York date).
func uiRec(book, nyDate, recordedAt, kind, ref string, after Position) Record {
	r := Record{Mode: ModeDemo, Venue: "etoro", Book: book, Source: SourceUI, RecordedAt: recordedAt,
		Decision: Decision{BarDate: nyDate, FillDate: nyDate, Signal: SignalManual, Action: map[string]string{ActionOpen: "buy", ActionClose: "sell"}[kind]},
		Operator: &Operator{By: "diego", Reason: "manual re-entry after the incident"}}
	r.Demo = &Demo{Env: "demo", Target: SignalManual, Action: kind, PositionAfter: after,
		Order: &Order{Kind: kind, ClientRef: ref, Status: "executed"}}
	return r
}

func exRec(book, bar, fill, recordedAt, pos string) Record {
	r := rec(book, bar, pos)
	r.Decision.FillDate, r.RecordedAt = fill, recordedAt
	return r
}

// A UI trade on day D is keyed apart from the executor's bar-D line, sits
// between the executor's bar D-1 and bar D lines in time, and counts as a
// leg on D.
func TestUIRecordsKeyOrderAndLegs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	long := Position{State: "long", PositionID: "p1", Units: 0.03}
	// Executor, bar 10-15, recorded 10-16 09:35 NY: closed, flat.
	closed := exRec("nsdq100", "2026-10-15", "2026-10-16", "2026-10-16T13:35:05Z", "flat")
	closed.Demo.Order = &Order{Kind: ActionClose, ClientRef: ClientRef("nsdq100", "2026-10-16", ActionClose), Status: "executed"}
	if err := l.Append(closed); err != nil {
		t.Fatal(err)
	}
	if p := l.LastDemoPosition("nsdq100"); p.State != "flat" {
		t.Fatalf("after the executor's line: %+v", p)
	}
	// Operator, 10-16 11:00 NY: opened by hand.
	ui := uiRec("nsdq100", "2026-10-16", "2026-10-16T15:00:00Z", ActionOpen, "ui-nsdq100-2026-10-16-open-3f9a1c", long)
	if err := l.Append(ui); err != nil {
		t.Fatal(err)
	}
	if p := l.LastDemoPosition("nsdq100"); p.State != "long" || p.PositionID != "p1" {
		t.Fatalf("after the ui line: %+v", p)
	}
	// A second UI line on the same day with another ref is fine; the same ref is not.
	ui2 := uiRec("nsdq100", "2026-10-16", "2026-10-16T15:30:00Z", ActionClose, "ui-nsdq100-2026-10-16-close-9b2d44", Flat())
	if err := l.Append(ui2); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(ui2); err == nil || !strings.Contains(err.Error(), "ui-nsdq100-2026-10-16-close-9b2d44") {
		t.Fatalf("duplicate ui ref accepted: %v", err)
	}
	bad := ui
	bad.Demo.Order = nil
	if err := l.Append(bad); err == nil {
		t.Fatal("ui record without an order accepted")
	}
	// Executor, bar 10-16, recorded 10-19 09:35 NY: same bar_date as the UI
	// lines, its own key, and the newest by recorded time.
	ex := exRec("nsdq100", "2026-10-16", "2026-10-19", "2026-10-19T13:35:05Z", "long")
	ex.Demo.PositionAfter.PositionID = "p2"
	if err := l.Append(ex); err != nil {
		t.Fatalf("executor's bar-10-16 line collided with the ui lines: %v", err)
	}
	if err := l.Append(ex); err == nil {
		t.Fatal("second executor line for the day accepted")
	}
	if p := l.LastDemoPosition("nsdq100"); p.PositionID != "p2" {
		t.Fatalf("after the executor's bar-10-16 line: %+v", p)
	}
	if got, ok := l.Get("nsdq100", "2026-10-16"); !ok || got.IsUI() {
		t.Fatalf("Get must return the executor's line: %+v %v", got, ok)
	}

	// Reopen: keys, order and legs survive the round trip.
	l2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rs := l2.Records()
	if len(rs) != 4 {
		t.Fatalf("records %d", len(rs))
	}
	var order []string
	for _, r := range rs {
		order = append(order, r.Decision.BarDate+"/"+r.RecordedAt[11:16]+"/"+r.Source)
	}
	if want := "2026-10-15/13:35/ 2026-10-16/15:00/ui 2026-10-16/15:30/ui 2026-10-16/13:35/"; strings.Join(order, " ") != want {
		t.Fatalf("order %v\nwant %s", order, want)
	}
	if rs[1].Operator == nil || rs[1].Operator.By != "diego" || rs[1].Decision.Signal != SignalManual {
		t.Fatalf("ui record round trip %+v", rs[1])
	}
	if n := LegsOn(rs, "nsdq100", "2026-10-16"); n != 3 { // the executor's close at 09:35 and the two ui legs
		t.Fatalf("legs on 10-16 = %d", n)
	}
	if n := LegsOn(rs, "nsdq100", "2026-10-19"); n != 1 {
		t.Fatalf("legs on 10-19 = %d", n)
	}
	if rule := RuleRecords(rs); len(rule) != 2 || rule[0].IsUI() || rule[1].IsUI() {
		t.Fatalf("rule records %+v", rule)
	}
	if latest, ok := LatestDemo(rs[:3], "nsdq100"); !ok || latest.ClientRef() != "ui-nsdq100-2026-10-16-close-9b2d44" {
		t.Fatalf("latest of the first three %+v", latest)
	}
	// The executor's line is omitted of source; the UI line says ui.
	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if strings.Contains(lines[0], `"source":"ui"`) || !strings.Contains(lines[1], `"source":"ui"`) || !strings.Contains(lines[1], `"operator":{"by":"diego"`) {
		t.Fatalf("json: %s\n%s", lines[0], lines[1])
	}
	ref, err := UIClientRef("spx500", "2026-10-16", ActionOpen)
	if err != nil || !strings.HasPrefix(ref, "ui-spx500-2026-10-16-open-") || len(ref) != len("ui-spx500-2026-10-16-open-")+6 {
		t.Fatalf("ui ref %q %v", ref, err)
	}
}

// Newer: bar date first, then recorded time; sub-second times compare as
// times, not strings.
func TestNewer(t *testing.T) {
	a := Record{Decision: Decision{BarDate: "2026-10-16"}, RecordedAt: "2026-10-16T15:00:00.5Z"}
	b := Record{Decision: Decision{BarDate: "2026-10-16"}, RecordedAt: "2026-10-16T15:00:00Z"}
	c := Record{Decision: Decision{BarDate: "2026-10-15"}, RecordedAt: "2026-10-17T00:00:00Z"}
	if !Newer(a, b) || Newer(b, a) || !Newer(b, c) || Newer(c, a) {
		t.Fatal("ordering")
	}
}

// Protection (2026-10-19): a protect line carries the new rates in
// position_after and is not a leg; a venue close is not a leg either; the
// new fields are omitted on lines without them.
func TestProtectRecordsAreNotLegs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	long := Position{State: "long", PositionID: "3616683146", Units: 0.035, Amount: 1100, OpenRate: 29770.5}
	if long.Protected() {
		t.Fatal("no stop yet")
	}
	protected := long
	protected.StopLossRate, protected.TakeProfitRate, protected.StopType = 29145.2, 31021.1, "fixed"
	if !protected.Protected() || Flat().Protected() {
		t.Fatal("Protected")
	}
	open := uiRec("nsdq100", "2026-10-19", "2026-10-19T14:00:00Z", ActionOpen, "ui-nsdq100-2026-10-19-open-aaaaaa", long)
	protect := uiRec("nsdq100", "2026-10-19", "2026-10-19T14:30:00Z", ActionProtect, "ui-nsdq100-2026-10-19-protect-bbbbbb", protected)
	protect.Demo.PositionBefore = long
	protect.Demo.Protection = &Protection{Profile: "moderate", ATR: 312.65, ATRPct: 0.0105, MaxLossUSD: 21.9, Reason: "set from the console", OperationID: "op-1"}
	for _, r := range []Record{open, protect} {
		if err := l.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	rs := l.Records()
	if n := LegsOn(rs, "nsdq100", "2026-10-19"); n != 1 {
		t.Fatalf("legs %d: a protect is not a leg", n)
	}
	if got := l.LastDemoPosition("nsdq100"); !got.Protected() || got.StopLossRate != 29145.2 || got.PositionID != "3616683146" {
		t.Fatalf("latest position %+v: the protect line must carry the rates", got)
	}

	// A venue close the executor records the next morning.
	vc := exRec("nsdq100", "2026-10-19", "2026-10-20", "2026-10-20T13:35:00Z", "flat")
	vc.Demo.Action = ActionVenueClose
	vc.Demo.PositionBefore = protected
	vc.Demo.Order = &Order{Kind: ActionVenueClose, Status: "executed", AvgPrice: 29140.0, Units: 0.035}
	vc.Demo.VenueClose = &VenueClose{Kind: VenueCloseStoppedOut, StopLossRate: 29145.2, CloseRate: 29140.0, GapBps: 1.8, ClosedAt: "2026-10-20T08:12:00Z", NetProfit: -22.1, Fees: 0.4}
	if err := l.Append(vc); err != nil {
		t.Fatal(err)
	}
	if n := LegsOn(l.Records(), "nsdq100", "2026-10-20"); n != 0 {
		t.Fatalf("legs on the stop-out day %d: a venue close is not a leg (the rule may re-enter)", n)
	}
	if got := l.LastDemoPosition("nsdq100"); got.State != "flat" {
		t.Fatalf("after the venue close %+v", got)
	}

	// Re-read from disk: round trip and omitempty.
	again, err := ReadFile(path)
	if err != nil || len(again) != 3 {
		t.Fatal(len(again), err)
	}
	var sawProtect, sawVenue bool
	for _, r := range again {
		switch r.Demo.Action {
		case ActionProtect:
			sawProtect = r.Demo.Protection != nil && r.Demo.Protection.Profile == "moderate" && r.Demo.PositionAfter.TakeProfitRate == 31021.1
		case ActionVenueClose:
			sawVenue = r.Demo.VenueClose != nil && r.Demo.VenueClose.Kind == VenueCloseStoppedOut
		}
	}
	if !sawProtect || !sawVenue {
		t.Fatalf("round trip: protect %v venue %v", sawProtect, sawVenue)
	}
	b, _ := json.Marshal(rec("spx500", "2026-10-15", "long"))
	for _, k := range []string{"stop_loss_rate", "take_profit_rate", "stop_type", "protection", "venue_close"} {
		if strings.Contains(string(b), `"`+k+`"`) {
			t.Errorf("%s present on a plain line: %s", k, b)
		}
	}
}
