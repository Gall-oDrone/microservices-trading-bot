package risk

import (
	"context"
	"math"
	"testing"
	"time"

	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/shared/pkg/varmodel"
)

type fakeVolView struct {
	view  VolView
	fresh bool
}

type fakeVol map[string]fakeVolView

func (f fakeVol) Lookup(book string) (VolView, bool) {
	v := f[book]
	return v.view, v.fresh
}

// returnsWith builds n daily returns ending on volDay, zero except shocks
// (index -> log return), skipping the indices in skip.
func returnsWith(n int, shocks map[int]float64, skip ...int) []varmodel.Return {
	sk := map[int]bool{}
	for _, i := range skip {
		sk[i] = true
	}
	var out []varmodel.Return
	for i := 0; i < n; i++ {
		if !sk[i] {
			out = append(out, varmodel.Return{Date: volDay.AddDate(0, 0, i-n+1), R: shocks[i]})
		}
	}
	return out
}

func viewWith(vol float64, rets []varmodel.Return) VolView {
	return VolView{
		Have:    true,
		DataAge: time.Hour,
		Estimate: varmodel.Estimate{
			Forecast:    varmodel.Forecast{EWMA: vol * 0.8, Long: vol, Vol: vol, Ready: true},
			Backtest:    varmodel.Backtest{Observations: 250, ExceptionsLong: 5, Zone: varmodel.ZoneYellow},
			HistReturns: rets,
		},
	}
}

func TestPortfolioVolSourcesAndHistoricalVaR(t *testing.T) {
	repo := positionRepoWith(t,
		pos("btc_mxn", "long", 0.1, 950_000),
		pos("eth_mxn", "long", 1, 50_000),
		pos("btc_usd", "short", 0.01, 59_000),
		pos("xrp_mxn", "long", 0, 10), // flat: no exposure, no history needed
	)
	btc := returnsWith(300, map[int]float64{10: -0.10, 20: -0.05})
	eth := returnsWith(300, map[int]float64{10: -0.20, 30: -0.08}, 5) // day 5 missing
	pm := &PortfolioMonitor{
		Positions: repo,
		Marks:     fakeMarks{"btc_mxn": 1_000_000},
		Config:    PortfolioConfig{DefaultDailyVol: 0.04, DailyVol: map[string]float64{"eth_mxn": 0.06}},
		Vol: fakeVol{
			"btc_mxn": {viewWith(0.02, btc), true},
			"eth_mxn": {viewWith(0.03, eth), true},   // overridden: vol 0.06, history still used
			"btc_usd": {viewWith(0.025, nil), false}, // stale: fallback
		},
	}
	snap, err := pm.Snapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]metrics.BookExposure{}
	for _, b := range snap.Books {
		by[b.Book] = b
	}
	for book, want := range map[string]struct {
		src string
		vol float64
		age float64
	}{
		"btc_mxn": {VolSourceEstimated, 0.02, 3600},
		"eth_mxn": {VolSourceOverride, 0.06, 3600},
		"btc_usd": {VolSourceFallback, 0.04, 3600},
		"xrp_mxn": {VolSourceFallback, 0.04, -1}, // never estimated
	} {
		b := by[book]
		if b.VolSource != want.src || b.DailyVol != want.vol || b.VolDataAgeSeconds != want.age {
			t.Errorf("%s: source %q vol %v age %v, want %+v", book, b.VolSource, b.DailyVol, b.VolDataAgeSeconds, want)
		}
	}
	// The model stays visible when overridden or stale.
	if m := by["btc_usd"].Model; m == nil || m.Estimate != 0.025 || m.Backtest.Zone != varmodel.ZoneYellow {
		t.Fatalf("stale model not published: %+v", m)
	}
	if by["xrp_mxn"].Model != nil {
		t.Fatal("model published for a book without an estimate")
	}

	mxn := snap.Currencies["MXN"]
	if want := metrics.VaRZ * (100_000*0.02 + 50_000*0.06); math.Abs(mxn.VaR-want) > 1e-6 {
		t.Fatalf("parametric MXN VaR %v want %v", mxn.VaR, want)
	}
	// 299 aligned days (eth lacks one), k = ceil(2.99) = 3: the losses are
	// day 10 (both books), day 20 (btc), day 30 (eth); the 3rd is eth's.
	if !mxn.HistOK || mxn.HistScenarios != 299 {
		t.Fatalf("historical MXN %+v", mxn)
	}
	if want := -50_000 * math.Expm1(-0.08); math.Abs(mxn.HistVaR-want) > 1e-6 {
		t.Fatalf("historical MXN VaR %v want %v", mxn.HistVaR, want)
	}
	// An exposed book without fresh history: no historical VaR, not a
	// number that silently omits it.
	if usd := snap.Currencies["USD"]; usd.HistOK {
		t.Fatalf("historical USD VaR without history: %+v", usd)
	}
}

func TestPortfolioHistoricalVaRNeedsEnoughDays(t *testing.T) {
	pm := &PortfolioMonitor{
		Positions: positionRepoWith(t, pos("btc_mxn", "long", 0.1, 1_000_000)),
		Config:    PortfolioConfig{DefaultDailyVol: 0.04},
		Vol:       fakeVol{"btc_mxn": {viewWith(0.02, returnsWith(MinHistScenarios-1, nil)), true}},
	}
	snap, err := pm.Snapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if mxn := snap.Currencies["MXN"]; mxn.HistOK || mxn.HistScenarios != MinHistScenarios-1 {
		t.Fatalf("historical VaR from %d days: %+v", MinHistScenarios-1, mxn)
	}
}

func TestPortfolioFixedVolModel(t *testing.T) {
	pm := &PortfolioMonitor{
		Positions: positionRepoWith(t, pos("btc_mxn", "long", 0.1, 1_000_000)),
		Config:    PortfolioConfig{DefaultDailyVol: 0.04},
	}
	snap, err := pm.Snapshot(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if b := snap.Books[0]; b.VolSource != VolSourceFixed || b.DailyVol != 0.04 || b.Model != nil {
		t.Fatalf("fixed model book %+v", b)
	}
	if mxn := snap.Currencies["MXN"]; mxn.HistOK {
		t.Fatalf("historical VaR with the fixed model: %+v", mxn)
	}
}

func TestLoadPortfolioConfigVolModel(t *testing.T) {
	c, err := LoadPortfolioConfig(envMap(nil))
	if err != nil || c.VolModel != VolModelEstimated || c.Lambda != varmodel.Lambda ||
		c.VolRefresh != DefaultVolRefresh || c.VolStale != DefaultVolStale || c.VolSourceURL != "https://api.bitso.com" {
		t.Fatalf("defaults %+v %v", c, err)
	}
	c, err = LoadPortfolioConfig(envMap(map[string]string{
		EnvVaRVolModel:     " Fixed ",
		EnvVaREWMALambda:   "0.97",
		EnvVaRVolRefresh:   "1h",
		EnvVaRVolStale:     "48h",
		EnvVaRVolSourceURL: "http://bitso-mirror:8080/",
	}))
	if err != nil || c.VolModel != VolModelFixed || c.Lambda != 0.97 || c.VolRefresh != time.Hour ||
		c.VolStale != 48*time.Hour || c.VolSourceURL != "http://bitso-mirror:8080" {
		t.Fatalf("parsed %+v %v", c, err)
	}
	for k, v := range map[string]string{
		EnvVaRVolModel:     "garch",
		EnvVaREWMALambda:   "1",
		EnvVaRVolRefresh:   "30s",
		EnvVaRVolStale:     "10m",
		EnvVaRVolSourceURL: "api.bitso.com",
	} {
		if _, err := LoadPortfolioConfig(envMap(map[string]string{k: v})); err == nil {
			t.Errorf("%s=%q accepted", k, v)
		}
	}
}
