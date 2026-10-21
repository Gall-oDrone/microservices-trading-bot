package api

import (
	"reflect"
	"testing"
)

func TestParseNamedLedgers(t *testing.T) {
	if got := ParseNamedLedgers("", "etoro-demo"); got != nil {
		t.Fatalf("empty: %v", got)
	}
	if got := ParseNamedLedgers("/x/demo/ledger.jsonl", "etoro-demo"); !reflect.DeepEqual(got, []NamedLedger{{"etoro-demo", "/x/demo/ledger.jsonl"}}) {
		t.Fatalf("plain path: %v", got)
	}
	got := ParseNamedLedgers(" etoro-demo=/x/demo/ledger.jsonl, etoro-demo-etf=/x/demo-etf/ledger.jsonl ,bad,=nope,etoro-demo-rotation=/x/demo-rotation/ledger.jsonl", "ignored")
	want := []NamedLedger{{"etoro-demo", "/x/demo/ledger.jsonl"}, {"etoro-demo-etf", "/x/demo-etf/ledger.jsonl"}, {"etoro-demo-rotation", "/x/demo-rotation/ledger.jsonl"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list: %v", got)
	}
}
