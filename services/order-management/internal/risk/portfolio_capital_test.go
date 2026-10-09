package risk

import (
	"context"
	"testing"
)

func TestLoadPortfolioConfigCapital(t *testing.T) {
	c, err := LoadPortfolioConfig(envMap(nil))
	if err != nil || len(c.Capital) != 0 {
		t.Fatalf("default %+v %v", c.Capital, err)
	}
	c, err = LoadPortfolioConfig(envMap(map[string]string{EnvCapital: "mxn=250000, USD=12000"}))
	if err != nil || c.Capital["MXN"] != 250_000 || c.Capital["USD"] != 12_000 {
		t.Fatalf("parsed %+v %v", c.Capital, err)
	}
	for _, v := range []string{"MXN=0", "MXN=-5", "MXN", "MXN=abc"} {
		if _, err := LoadPortfolioConfig(envMap(map[string]string{EnvCapital: v})); err == nil {
			t.Errorf("%s=%q accepted", EnvCapital, v)
		}
	}
}

// Capital is attached to each currency's risk, and a currency with capital
// but no position is still reported (its ratios read 0).
func TestPortfolioSnapshotCarriesCapital(t *testing.T) {
	pm := &PortfolioMonitor{
		Positions: positionRepoWith(t, pos("btc_mxn", "long", 0.1, 1_000_000)),
		Config: PortfolioConfig{
			DefaultDailyVol: 0.04,
			StressShocks:    DefaultStressShocks,
			Capital:         map[string]float64{"MXN": 50_000, "USD": 3_000},
		},
	}
	snap, err := pm.Snapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	mxn, usd := snap.Currencies["MXN"], snap.Currencies["USD"]
	if mxn.Capital != 50_000 || mxn.Net != 100_000 {
		t.Fatalf("MXN %+v", mxn)
	}
	if worst, _ := mxn.WorstStress(); worst/mxn.Capital != 1 { // spot-50% on 100k = the 50k capital
		t.Fatalf("worst stress / capital = %v", worst/mxn.Capital)
	}
	if usd.Capital != 3_000 || usd.Net != 0 || len(usd.Stress) == 0 {
		t.Fatalf("USD %+v", usd)
	}
}
