package etoroledger

import (
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
