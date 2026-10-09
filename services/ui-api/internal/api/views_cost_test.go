package api

import (
	"math"
	"strings"
	"testing"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/shared/pkg/risk"
)

func costFinding(fs []risk.Finding) *risk.Finding {
	for i := range fs {
		if fs[i].Rule == RuleCostOverPessimistic {
			return &fs[i]
		}
	}
	return nil
}

func slip(v float64) *float64 { return &v }

func TestCostBudgetFindingNeedsThreeLegsAbovePessimistic(t *testing.T) {
	recs := []dailyledger.Record{{Mode: "stage", Book: "btc_mxn",
		Decision: dailyledger.Decision{BarDate: "2026-10-01", Close: 1.5e6, Signal: "flat"},
		Paper:    dailyledger.Paper{LegCostBps: 70}}}
	// Each leg: 78 bps fee + 20 bps slippage = 98 bps, above 88.
	leg := Fill{Notional: 1500, FeeQuote: 1500 * 78 / 1e4, SlippageBps: slip(20)}
	two := buildBookRisk(risk.DefaultPolicy(), "btc_mxn", recs, []Fill{leg, leg}, fixedNow, 0.001)
	if f := costFinding(two.Findings); f != nil || !two.Cost.Budget.OverPessimistic {
		t.Fatalf("two legs: finding %+v budget %+v", f, two.Cost.Budget)
	}
	three := buildBookRisk(risk.DefaultPolicy(), "btc_mxn", recs, []Fill{leg, leg, leg}, fixedNow, 0.001)
	f := costFinding(three.Findings)
	if f == nil || f.Severity != risk.Warn || f.Limit != 88 || math.Abs(f.Value-98) > 1e-9 {
		t.Fatalf("three legs: %+v", three.Findings)
	}
	// 3 × 1500 × (98 − 70) bps = 12.60 MXN over the primary budget.
	if !strings.Contains(f.Message, "12.60 MXN over the primary budget") || !strings.Contains(f.Message, "3 legs") {
		t.Fatalf("message: %s", f.Message)
	}
}

func TestCostBudgetNoFindingWithinPessimistic(t *testing.T) {
	recs := []dailyledger.Record{{Mode: "stage", Book: "btc_usd",
		Decision: dailyledger.Decision{BarDate: "2026-10-01", Close: 85000, Signal: "flat"},
		Paper:    dailyledger.Paper{LegCostBps: 40}}}
	// 30 bps fee + 12 bps slippage = 42: over the 40 bps budget, under 46.
	leg := Fill{Notional: 85, FeeQuote: 85 * 30 / 1e4, SlippageBps: slip(12)}
	br := buildBookRisk(risk.DefaultPolicy(), "btc_usd", recs, []Fill{leg, leg, leg, leg}, fixedNow, 0.001)
	bg := br.Cost.Budget
	if costFinding(br.Findings) != nil || bg.OverPessimistic || bg.BudgetUsed <= 1 || math.Abs(bg.WeightedBps-42) > 1e-9 {
		t.Fatalf("budget %+v findings %+v", bg, br.Findings)
	}
}
