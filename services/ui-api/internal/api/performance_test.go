package api

import (
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/ui-api/internal/store"
)

// fullHistoryServer serves the fixture ledger with the committed full Bitso
// btc_mxn history (2017-05-31..2026-09-26) as its candles.
func fullHistoryServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	led, err := os.ReadFile("testdata/ledger.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	csv, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "backtest-readiness", "evidence-2026-09-27", "btc_mxn_daily_bitso.csv"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "ledger.jsonl"), string(led))
	if err := os.MkdirAll(filepath.Join(dir, "candles"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "candles", "btc_mxn_daily_2026-09-26.csv"), string(csv))
	return newTestServer(t, fixedNow, func(s *Server) { s.Store = store.New(filepath.Join(dir, "ledger.jsonl"), "") })
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestPerformanceStagePnL(t *testing.T) {
	p := get[PerformanceResponse](t, newTestServer(t, fixedNow, nil), "/api/ui/forward-tests/btc_mxn/performance", 200)
	if p.PnL == nil {
		t.Fatal("no pnl")
	}
	g := *p.PnL
	// The real first leg: 1522.86 MXN for 0.00099999 BTC net, 78 bps fee in
	// BTC, +40.4 bps above the fill day's open; marked at the fixture's last
	// close (2026-10-01).
	if g.Legs != 1 || g.PositionBTC != 0.00099999 || !near(g.Invested, 1522.85979, 1e-6) || !near(g.CostBasis, 1522.85979, 1e-6) {
		t.Fatalf("position %+v", g)
	}
	if !near(g.Fees, 11.876, 0.01) || !near(g.Slippage, 6.15, 0.05) || g.MarkDate != "2026-10-01" {
		t.Fatalf("costs %+v", g)
	}
	if !near(g.Unrealized, g.PositionBTC*g.Mark-g.CostBasis, 1e-9) || g.Realized != 0 || !near(g.Total, g.Unrealized, 1e-9) {
		t.Fatalf("pnl %+v", g)
	}
	if !near(g.MarketPnL-g.Fees-g.Slippage, g.Total, 1e-9) {
		t.Fatalf("attribution does not add up: %+v", g)
	}
	if !near(g.BreakEvenPrimary, 1522.85979/0.00099999/0.993, 1e-6) || !near(g.BreakEvenPessimistic, 1522.85979/0.00099999/0.9912, 1e-6) {
		t.Fatalf("break-even %+v", g)
	}
	if p.Paper.Days != 3 || p.Paper.Meaningful {
		t.Fatalf("paper %+v", p.Paper)
	}
	// The fixture's 120 days of candles are too short for any closed trip.
	if p.History == nil || p.History.Trips.Count != 0 || p.History.LegBps != 70 {
		t.Fatalf("history %+v", p.History)
	}
	// A dry-run-only book has no stage P&L.
	usd := get[PerformanceResponse](t, newTestServer(t, fixedNow, nil), "/api/ui/forward-tests/btc_usd/performance", 200)
	if usd.Quote != "usd" || usd.PnL == nil || usd.PnL.Legs != 1 {
		t.Fatalf("btc_usd %+v", usd.PnL)
	}
}

func TestPerformanceHistoryOnFullCandles(t *testing.T) {
	p := get[PerformanceResponse](t, fullHistoryServer(t), "/api/ui/forward-tests/btc_mxn/performance", 200)
	h := p.History
	if h == nil || h.From != "2018-01-01" || h.Trips.Count < 100 || h.Trips.Best.Entry != "2020-10-10" || !(h.Trips.CompoundedExB1 < 0) {
		t.Fatalf("trips %+v", h)
	}
	// The pre-registration's base rates: H2 5/9, H1 6/9.
	if len(h.Calendar.Years) != 9 || h.Calendar.BeatsHold != 5 || h.Calendar.ShallowerDD != 6 {
		t.Fatalf("calendar %+v", h.Calendar)
	}
	o := p.OpenTrade
	if o == nil || o.Entry != "2026-08-20" || !(o.ExitBelow > 0) || !(o.MaxFavorable >= o.Return) || !(o.MaxAdverse <= o.Return) {
		t.Fatalf("open trade %+v", o)
	}
}

func TestMonteCarloEndpoint(t *testing.T) {
	ts := fullHistoryServer(t)
	m := get[MonteCarloResponse](t, ts, "/api/ui/forward-tests/btc_mxn/montecarlo?paths=300&horizon=365", 200)
	if m.Cost != CostPrimary || m.LegBps != 70 || m.Summary.Config.Paths != 300 || m.Summary.SampleFrom != "2018-01-01" || m.Cached {
		t.Fatalf("run %+v", m)
	}
	if len(m.Calibration.Years) != 9 || m.Calibration.BeatsHold != 5 {
		t.Fatalf("calibration %+v", m.Calibration)
	}
	again := get[MonteCarloResponse](t, ts, "/api/ui/forward-tests/btc_mxn/montecarlo?paths=300&horizon=365", 200)
	if !again.Cached || again.Summary.TrendReturn != m.Summary.TrendReturn {
		t.Fatalf("cache %+v", again.Summary.TrendReturn)
	}
	pes := get[MonteCarloResponse](t, ts, "/api/ui/forward-tests/btc_mxn/montecarlo?paths=300&cost=pessimistic", 200)
	if pes.LegBps != 88 || !(pes.Summary.TrendReturn.P50 < m.Summary.TrendReturn.P50) {
		t.Fatalf("pessimistic %v %+v", pes.LegBps, pes.Summary.TrendReturn)
	}
	// Realized: the one stage leg's fee (its fill day is past these
	// candles, so no slippage is known): 78 bps.
	if re := get[MonteCarloResponse](t, ts, "/api/ui/forward-tests/btc_mxn/montecarlo?paths=300&cost=realized", 200); re.LegBps != 78 {
		t.Fatalf("realized %v", re.LegBps)
	}
	for _, q := range []string{"paths=50", "paths=999999", "horizon=10", "block=0", "cost=free", "paths=x"} {
		get[errorBody](t, ts, "/api/ui/forward-tests/btc_mxn/montecarlo?"+q, 400)
	}
}

func TestBuildStagePnLRealizesOnSell(t *testing.T) {
	recs := []dailyledger.Record{
		{Decision: dailyledger.Decision{FillDate: "2026-01-02"}, Paper: dailyledger.Paper{Equity: 1}},
		{Decision: dailyledger.Decision{FillDate: "2026-02-01"}, Paper: dailyledger.Paper{Equity: 1.1}},
	}
	s := func(v float64) *float64 { return &v }
	fills := []Fill{
		{FillDate: "2026-01-02", Side: "buy", NetBTC: 0.001, Notional: 1000, FeeQuote: 7, SlippageBps: s(10)},
		{FillDate: "2026-02-01", Side: "sell", NetBTC: -0.001, Notional: 1200, FeeQuote: 8.4, SlippageBps: s(-5)},
	}
	p := buildStagePnL("btc_mxn", recs, fills, 1_300_000, "2026-02-02")
	// Bought for 1000, sold for 1200 less 8.4: realized 191.6, flat.
	if !near(p.Realized, 191.6, 1e-9) || p.PositionBTC != 0 || p.Unrealized != 0 || !near(p.Total, 191.6, 1e-9) {
		t.Fatalf("%+v", p)
	}
	// Slippage: +1.0 on the buy, -0.6 on the sell (improvement).
	if !near(p.Slippage, 0.4, 1e-9) || !near(p.Fees, 15.4, 1e-9) || !near(p.MarketPnL, 191.6+15.4+0.4, 1e-9) {
		t.Fatalf("attribution %+v", p)
	}
	// Stage +19.16 % on the 1000 invested vs paper +10 % over the same days.
	if !near(p.ReturnOnInvested, 0.1916, 1e-9) || !near(p.PaperReturn, 0.1, 1e-9) || !near(p.ShortfallBps, 916, 1e-6) {
		t.Fatalf("vs paper %+v", p)
	}
	if p.BreakEvenPrimary != 0 {
		t.Fatalf("break-even while flat %v", p.BreakEvenPrimary)
	}
	if buildStagePnL("btc_mxn", recs, nil, 1, "") != nil {
		t.Fatal("pnl without fills")
	}
}

func TestBuildPaperStats(t *testing.T) {
	var recs []dailyledger.Record
	eq, hold := 1.0, 1.0
	for i := 0; i < 120; i++ {
		r := 0.002
		if i%10 == 9 {
			r = -0.01
		}
		eq *= 1 + r
		hold *= 1.001
		recs = append(recs, dailyledger.Record{Paper: dailyledger.Paper{Equity: eq, HoldEquity: hold}})
	}
	s := buildPaperStats(recs)
	if s.Days != 120 || !s.Meaningful || !near(s.Return, eq-1, 1e-12) || !near(s.HoldReturn, hold-1, 1e-12) {
		t.Fatalf("%+v", s)
	}
	if !(s.Sharpe > 0) || !(s.Sortino > s.Sharpe) || !near(s.MaxDD, 0.01, 1e-12) || s.HoldMaxDD != 0 || !(s.Calmar > 0) || !(s.AnnVol > 0) {
		t.Fatalf("%+v", s)
	}
	if z := buildPaperStats(nil); z.Days != 0 || z.Sharpe != 0 {
		t.Fatalf("%+v", z)
	}
}

func TestPerformancePnLHistoryAndNAV(t *testing.T) {
	p := get[PerformanceResponse](t, newTestServer(t, fixedNow, nil), "/api/ui/forward-tests/btc_mxn/performance", 200)
	// Capital = the policy's max order notional (25,000 MXN, §6.4.10).
	if p.Capital != 25000 || len(p.PnLHistory) != 2 || p.PnLHistory[0].Date != "2026-09-30" || p.PnLHistory[1].Date != "2026-10-01" {
		t.Fatalf("history %+v capital %v", p.PnLHistory, p.Capital)
	}
	last := p.PnLHistory[len(p.PnLHistory)-1]
	if !near(last.Total, p.PnL.Total, 1e-9) || last.PositionBTC != 0.00099999 || !near(last.NAV, 25000+last.Total, 1e-9) {
		t.Fatalf("last %+v pnl %+v", last, p.PnL)
	}
	if !near(p.PnLHistory[0].Daily+last.Daily, last.Total, 1e-9) || last.PaperPnL == 0 {
		t.Fatalf("daily %+v", p.PnLHistory)
	}
	if p.MXNTerms != nil {
		t.Fatal("btc_mxn has no MXN terms")
	}
}

// btc_usd in MXN terms (BTCUSD pre-registration H2): forward start
// 2026-09-29, FX from the 09-28 close to the last common close.
func TestPerformanceMXNTerms(t *testing.T) {
	p := get[PerformanceResponse](t, newTestServer(t, fixedNow, nil), "/api/ui/forward-tests/btc_usd/performance", 200)
	m := p.MXNTerms
	if m == nil || m.From != "2026-09-29" || m.To != "2026-10-01" || m.ConvBps != 60 || p.Capital != 1500 {
		t.Fatalf("mxn terms %+v", m)
	}
	if !(m.FXStart > 15 && m.FXStart < 25) || !near(m.FXChange, m.FXEnd/m.FXStart-1, 1e-12) {
		t.Fatalf("fx %+v", m)
	}
	if !near(m.PaperMXN, (1+m.PaperUSD)*m.FXEnd/m.FXStart*0.994*0.994-1, 1e-12) || !near(m.Excess, m.PaperMXN-m.HoldBTCMXN, 1e-12) ||
		m.H2SoFar != (m.Excess > 0) {
		t.Fatalf("returns %+v", m)
	}
	// The rule is valued as if closed, like the registered evaluation: the exit leg is paid.
	var eq float64
	recs, _ := store.New("testdata/ledger.jsonl", "").Records()
	for _, r := range recs {
		if r.Book == "btc_usd" {
			eq = r.Paper.EquityClosed // the last btc_usd record wins
		}
	}
	if eq <= 0 || !near(m.PaperUSD, eq-1, 1e-12) {
		t.Fatalf("paper usd %v, want equity_if_closed %v - 1", m.PaperUSD, eq)
	}
	if !near(m.StagePnLMXN, p.PnL.Total*m.FXEnd, 1e-9) || !near(m.StageInvested, 83.481*m.FXEnd, 1e-6) {
		t.Fatalf("stage %+v", m)
	}
}
