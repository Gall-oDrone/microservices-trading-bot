package main

import (
	"reflect"
	"testing"
)

func TestEtoroLedgers(t *testing.T) {
	if got := etoroLedgers("", "etoro-demo"); got != nil {
		t.Fatalf("empty: %v", got)
	}
	if got := etoroLedgers("/x/demo/ledger.jsonl", "etoro-demo"); !reflect.DeepEqual(got, []namedLedger{{"etoro-demo", "/x/demo/ledger.jsonl"}}) {
		t.Fatalf("plain path: %v", got)
	}
	got := etoroLedgers(" etoro-demo=/x/demo/ledger.jsonl, etoro-demo-etf=/x/demo-etf/ledger.jsonl ,bad,=nope,etoro-demo-rotation=/x/demo-rotation/ledger.jsonl", "ignored")
	want := []namedLedger{{"etoro-demo", "/x/demo/ledger.jsonl"}, {"etoro-demo-etf", "/x/demo-etf/ledger.jsonl"}, {"etoro-demo-rotation", "/x/demo-rotation/ledger.jsonl"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list: %v", got)
	}
}
