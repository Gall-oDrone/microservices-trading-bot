package main

import (
	"testing"

	"bitso-trading-platform/shared/pkg/dailyledger"
)

// TestFrozenSpecsMatchSharedPreregCosts keeps shared/pkg/dailyledger's cost
// table (read by ui-api's cost budget) equal to what the paper account uses.
func TestFrozenSpecsMatchSharedPreregCosts(t *testing.T) {
	if len(frozenSpecs) != len(dailyledger.PreregCosts) {
		t.Fatalf("frozenSpecs has %d books, dailyledger.PreregCosts %d", len(frozenSpecs), len(dailyledger.PreregCosts))
	}
	for book, s := range frozenSpecs {
		pc, ok := dailyledger.PreregCosts[book]
		if !ok {
			t.Fatalf("%s missing from dailyledger.PreregCosts", book)
		}
		if pc.PrimaryLegBps != s.LegCostBps || pc.Prereg != s.Prereg {
			t.Errorf("%s: shared %+v, executor leg %v prereg %s", book, pc, s.LegCostBps, s.Prereg)
		}
		if pc.SecondaryLegBps <= pc.PrimaryLegBps {
			t.Errorf("%s: pessimistic %v must exceed primary %v", book, pc.SecondaryLegBps, pc.PrimaryLegBps)
		}
	}
}
