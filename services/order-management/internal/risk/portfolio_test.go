package risk

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
	sharedrisk "bitso-trading-platform/shared/pkg/risk"
)

func TestLoadPortfolioConfig(t *testing.T) {
	c, err := LoadPortfolioConfig(envMap(nil))
	if err != nil || c.Interval != DefaultPortfolioInterval || c.DefaultDailyVol != DefaultVaRDailyVol ||
		len(c.VaRLimits) != 0 || c.MarketDataURL != "" {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	c, err = LoadPortfolioConfig(envMap(map[string]string{
		EnvPortfolioInterval: "1m",
		EnvVaRDailyVol:       "0.03",
		EnvVaRDailyVolBooks:  " BTC_USD=0.025 , ",
		EnvVaRLimits:         "mxn=20000,USD=1000",
		EnvMarketDataURL:     "http://market-data:8083/",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Interval != time.Minute || c.VolFor("btc_mxn") != 0.03 || c.VolFor("btc_usd") != 0.025 ||
		c.VaRLimits["MXN"] != 20000 || c.VaRLimits["USD"] != 1000 || c.MarketDataURL != "http://market-data:8083" {
		t.Fatalf("parsed: %+v", c)
	}
	// A typo must stop start-up, not fall back to a default.
	for k, v := range map[string]string{
		EnvPortfolioInterval: "30",
		EnvVaRDailyVol:       "4%",
		EnvVaRDailyVolBooks:  "btc_mxn",
		EnvVaRLimits:         "MXN=-1",
		EnvMarketDataURL:     "market-data:8083",
	} {
		if _, err := LoadPortfolioConfig(envMap(map[string]string{k: v})); err == nil {
			t.Errorf("%s=%q accepted", k, v)
		}
	}
	if _, err := LoadPortfolioConfig(envMap(map[string]string{EnvVaRDailyVol: "1.5"})); err == nil {
		t.Error("daily vol above 1 accepted")
	}
}

type fakeMarks map[string]float64

func (f fakeMarks) Mark(_ context.Context, book string) (float64, error) {
	if v, ok := f[book]; ok {
		return v, nil
	}
	return 0, errors.New("no ticker")
}

func positionRepoWith(t *testing.T, ps ...*models.Position) repository.PositionRepository {
	t.Helper()
	repo := repository.NewInMemoryPositionRepository(testRiskLogger, testRiskMetrics)
	for _, p := range ps {
		if err := repo.Create(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func pos(book, side string, size, entry float64) *models.Position {
	p := models.NewPosition(book, side)
	p.Size, p.EntryPrice = size, entry
	return p
}

func TestPortfolioSnapshot(t *testing.T) {
	repo := positionRepoWith(t,
		pos("btc_mxn", "long", 0.1, 950_000),
		pos("btc_usd", "short", 0.01, 59_000), // no ticker: entry-price fallback
	)
	pm := &PortfolioMonitor{
		Positions: repo,
		Marks:     fakeMarks{"btc_mxn": 1_000_000},
		Config:    PortfolioConfig{DefaultDailyVol: 0.04, DailyVol: map[string]float64{"btc_usd": 0.05}, VaRLimits: map[string]float64{"MXN": 20_000, "EUR": 5}},
	}
	snap, err := pm.Snapshot(context.Background(), map[string]bool{"eth_mxn": true})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]metrics.BookExposure{}
	for _, b := range snap.Books {
		by[b.Book] = b
	}
	if b := by["btc_mxn"]; b.Base != 0.1 || b.Mark != 1_000_000 || b.Quote != 100_000 || b.Fallback || b.Currency != "MXN" {
		t.Fatalf("btc_mxn %+v", b)
	}
	if b := by["btc_usd"]; b.Base != -0.01 || b.Mark != 59_000 || math.Abs(b.Quote+590) > 1e-9 || !b.Fallback {
		t.Fatalf("btc_usd (short, fallback) %+v", b)
	}
	// A book seen before and now closed reports zero, not its last value.
	if b, ok := by["eth_mxn"]; !ok || b.Base != 0 || b.Quote != 0 || b.Fallback {
		t.Fatalf("closed known book %+v %v", b, ok)
	}
	if math.Abs(snap.NetBase["btc"]-0.09) > 1e-12 {
		t.Fatalf("net btc %v", snap.NetBase["btc"])
	}
	mxn := snap.Currencies["MXN"]
	if want := metrics.VaRZ * 100_000 * 0.04; math.Abs(mxn.VaR-want) > 1e-6 || mxn.Limit != 20_000 || mxn.Gross != 100_000 {
		t.Fatalf("MXN %+v want VaR %v", mxn, want)
	}
	usd := snap.Currencies["USD"]
	if want := metrics.VaRZ * 590 * 0.05; math.Abs(usd.VaR-want) > 1e-6 || usd.Net != -590 || usd.Gross != 590 {
		t.Fatalf("USD %+v want VaR %v", usd, want)
	}
	if eur, ok := snap.Currencies["EUR"]; !ok || eur.Limit != 5 || eur.VaR != 0 {
		t.Fatalf("a configured limit without positions is still published: %+v %v", eur, ok)
	}
}

// An open book with neither a market price nor an entry price is flagged:
// its exposure reads 0, which understates risk.
func TestPortfolioSnapshotFlagsUnpricedBook(t *testing.T) {
	pm := &PortfolioMonitor{Positions: positionRepoWith(t, pos("btc_mxn", "long", 0.2, 0)), Config: PortfolioConfig{DefaultDailyVol: 0.04}}
	snap, err := pm.Snapshot(context.Background(), nil)
	if err != nil || len(snap.Books) != 1 || !snap.Books[0].Fallback || snap.Books[0].Quote != 0 {
		t.Fatalf("%+v %v", snap.Books, err)
	}
}

func TestMarketDataMarks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ticker" {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Query().Get("book") {
		case "btc_mxn":
			_, _ = w.Write([]byte(`{"book":"btc_mxn","bid":"999000","ask":"1001000","last":"995000"}`))
		case "btc_usd": // crossed/empty book: last trade
			_, _ = w.Write([]byte(`{"book":"btc_usd","bid":"0","ask":"","last":"60000.5"}`))
		case "eth_mxn":
			_, _ = w.Write([]byte(`{"book":"eth_mxn","bid":"","ask":"","last":""}`))
		default:
			http.Error(w, "Ticker not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	m := MarketDataMarks{BaseURL: srv.URL}
	ctx := context.Background()
	if v, err := m.Mark(ctx, "btc_mxn"); err != nil || v != 1_000_000 {
		t.Fatalf("mid: %v %v", v, err)
	}
	if v, err := m.Mark(ctx, "btc_usd"); err != nil || v != 60000.5 {
		t.Fatalf("last: %v %v", v, err)
	}
	if _, err := m.Mark(ctx, "eth_mxn"); err == nil || !strings.Contains(err.Error(), "no usable price") {
		t.Fatalf("empty ticker: %v", err)
	}
	if _, err := m.Mark(ctx, "xrp_mxn"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing ticker: %v", err)
	}
}

func TestPortfolioMonitorRunPublishes(t *testing.T) {
	reg := prometheus.NewRegistry()
	series := metrics.NewRiskSeries(reg)
	pm := &PortfolioMonitor{
		Positions: positionRepoWith(t, pos("btc_mxn", "long", 0.1, 1_000_000)),
		Config:    PortfolioConfig{Interval: time.Hour, DefaultDailyVol: 0.04},
		Series:    series,
		Now:       func() time.Time { return time.Unix(1_790_000_000, 0) },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { pm.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for testutil.CollectAndCount(reg, "portfolio_risk_last_run_timestamp_seconds") == 0 ||
		testutil.CollectAndCount(reg, "portfolio_var_quote") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("first run not published")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if n := testutil.CollectAndCount(reg, "position_exposure_quote"); n != 1 {
		t.Fatalf("exposure series %d", n)
	}
}

// The shared check treats the position as long-positive: a buy that covers a
// short must not be blocked as if it grew a long, and a sell that grows a
// short is not "reducing".
func TestSharedCheckUsesSignedPosition(t *testing.T) {
	ctx := context.Background()
	positions := positionRepoWith(t, pos("btc_mxn", "short", 0.9, 1_000_000))
	m := NewRiskManager(setupRiskManager().config, testRiskLogger,
		repository.NewInMemoryOrderRepository(testRiskLogger, testRiskMetrics), positions, testRiskMetrics)
	p := sharedrisk.Policy{Version: "signed-test", Default: sharedrisk.BookLimits{MaxPositionBTC: 1.0}}
	m.SetSharedPolicy(&SharedPolicy{Policy: &p})

	if err := m.CheckRisk(ctx, models.NewOrder("cover", "btc_mxn", "buy", "limit", "basic", 10, 0.2)); err != nil {
		t.Fatalf("covering buy (short 0.9 -> 0.7) blocked: %v", err)
	}
	if err := m.CheckRisk(ctx, models.NewOrder("grow", "btc_mxn", "sell", "limit", "basic", 10, 0.2)); err == nil ||
		!strings.Contains(err.Error(), "max_position_btc") {
		t.Fatalf("sell growing the short to 1.1: want max_position_btc, got %v", err)
	}
	exp, err := m.GetCurrentExposure(ctx, "btc_mxn")
	if err != nil || exp.TotalSize != -0.9 {
		t.Fatalf("exposure is signed: %+v %v", exp, err)
	}
}
