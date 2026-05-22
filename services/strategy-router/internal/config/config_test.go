package config

import (
	"testing"
)

func TestRouteTable_Resolve(t *testing.T) {
	rt := RouteTable{
		LowVolRange:  "mean_reversion_btc_mxn",
		TrendingUp:   "momentum_btc_mxn",
		TrendingDown: "momentum_btc_mxn",
		HighVol:      "none",
		Neutral:      "mean_reversion_btc_mxn",
	}

	cases := map[string]string{
		"low_vol_range": "mean_reversion_btc_mxn",
		"trending_up":   "momentum_btc_mxn",
		"trending_down": "momentum_btc_mxn",
		"high_vol":      "none",
		"neutral":       "mean_reversion_btc_mxn",
		"unknown":       "mean_reversion_btc_mxn",
	}

	for regime, want := range cases {
		if got := rt.Resolve(regime); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", regime, got, want)
		}
	}
}

func TestIsPause(t *testing.T) {
	pauseCases := []string{"", "none", "None", "NONE", "  none  "}
	for _, c := range pauseCases {
		if !IsPause(c) {
			t.Errorf("IsPause(%q) = false, want true", c)
		}
	}

	notPause := []string{"mean_reversion_btc_mxn", "momentum_btc_mxn", "lim"}
	for _, c := range notPause {
		if IsPause(c) {
			t.Errorf("IsPause(%q) = true, want false", c)
		}
	}
}

func TestLoad_Defaults(t *testing.T) {
	// Make sure we don't leak env from the host into this test.
	t.Setenv("STRATEGY_ROUTER_BOOK", "btc_mxn")
	t.Setenv("ROUTER_INTERVAL_SEC", "")
	t.Setenv("ROUTE_LOW_VOL", "")
	t.Setenv("ROUTE_HIGH_VOL", "")

	cfg := Load()
	if cfg.Book != "btc_mxn" {
		t.Errorf("Book = %q, want btc_mxn", cfg.Book)
	}
	if cfg.Routes.LowVolRange != "mean_reversion_btc_mxn" {
		t.Errorf("default LowVolRange = %q", cfg.Routes.LowVolRange)
	}
	if cfg.Routes.HighVol != "none" {
		t.Errorf("default HighVol = %q, want none", cfg.Routes.HighVol)
	}
	if cfg.CooldownSeconds <= 0 {
		t.Errorf("CooldownSeconds = %d, want > 0", cfg.CooldownSeconds)
	}
}

func TestLoad_BookSubstitution(t *testing.T) {
	t.Setenv("STRATEGY_ROUTER_BOOK", "eth_mxn")
	t.Setenv("ROUTE_LOW_VOL", "")
	t.Setenv("ROUTE_TRENDING_UP", "")
	t.Setenv("ROUTE_NEUTRAL", "")

	cfg := Load()
	if cfg.Routes.LowVolRange != "mean_reversion_eth_mxn" {
		t.Errorf("LowVolRange = %q, want mean_reversion_eth_mxn", cfg.Routes.LowVolRange)
	}
	if cfg.Routes.TrendingUp != "momentum_eth_mxn" {
		t.Errorf("TrendingUp = %q, want momentum_eth_mxn", cfg.Routes.TrendingUp)
	}
}
