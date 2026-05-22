package strategies

import "testing"

func TestRealizedQuotePnL_withFees(t *testing.T) {
	entry := 1_000_000.0
	exit := 1_010_000.0
	size := 0.001
	buyR := 0.00741
	sellR := 0.00570

	gross, net, model := RealizedQuotePnL(entry, exit, size, buyR, sellR)
	if model != "bitso_realized" {
		t.Fatalf("feeModel = %q, want bitso_realized", model)
	}
	if gross <= 0 {
		t.Fatalf("gross = %v, want positive", gross)
	}
	if net >= gross {
		t.Fatalf("net (%v) should be less than gross (%v) after fees", net, gross)
	}
}

func TestRealizedQuotePnL_noFees(t *testing.T) {
	_, net, model := RealizedQuotePnL(100, 110, 1, 0, 0)
	if model != "gross_only" {
		t.Fatalf("feeModel = %q", model)
	}
	if net != 10 {
		t.Fatalf("net = %v", net)
	}
}
