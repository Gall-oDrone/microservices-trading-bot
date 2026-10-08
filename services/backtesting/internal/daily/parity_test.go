package daily_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"bitso-trading-platform/backtesting/internal/daily"
	"bitso-trading-platform/backtesting/internal/engine"
	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"
)

// evidenceRoot is docs/backtest-readiness, where cmd/daily-research's
// registered runs and the Bitso daily bars they read are committed.
var evidenceRoot = filepath.Join("..", "..", "..", "..", "docs", "backtest-readiness")

// barsFor maps a report's data.prices to the committed CSV it was run on.
var barsFor = map[string]struct{ dir, file, book string }{
	"bitso":     {"evidence-2026-09-27", "btc_mxn_daily_bitso.csv", "btc_mxn"},
	"bitso-usd": {"evidence-2026-09-28", "btc_usd_daily_bitso.csv", "btc_usd"},
}

type report struct {
	Data struct {
		Prices string `json:"prices"`
		Bars   int    `json:"bars"`
		First  string `json:"first"`
		Last   string `json:"last"`
	} `json:"data"`
	Costs struct {
		BuyBPS      float64 `json:"buy_bps"`
		SellBPS     float64 `json:"sell_bps"`
		SlippageBPS float64 `json:"slippage_bps"`
	} `json:"costs"`
	Params struct {
		SMA int `json:"sma"`
	} `json:"params"`
	Windows []struct {
		Label   string `json:"label"`
		From    string `json:"from"`
		To      string `json:"to"`
		Bars    int    `json:"bars"`
		Results []struct {
			Rule        string  `json:"rule"`
			ReturnPct   float64 `json:"return_pct"`
			RoundTrips  int     `json:"round_trips"`
			ExposurePct float64 `json:"exposure_pct"`
			MaxDDPct    float64 `json:"max_dd_pct"`
			CostPct     float64 `json:"cost_pct"`
		} `json:"results"`
	} `json:"windows"`
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestParityWithRegisteredEvidence runs the service's sma50_daily path on
// every registered cmd/daily-research run made on the committed Bitso bars
// (btc_mxn and btc_usd; frictionless, maker, maker+slippage and taker
// costs; in-sample, out-of-sample and per-year windows) and requires the
// trend_sma50 numbers to be bit-identical to the JSON daily-research wrote.
// Go's JSON encoder writes the shortest float that round-trips, so ==
// compares exact float64 bits.
func TestParityWithRegisteredEvidence(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(evidenceRoot, "evidence-*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	checked, reports := 0, 0
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var rep report
		if json.Unmarshal(raw, &rep) != nil || rep.Data.Prices == "" {
			continue // not a daily-research report
		}
		src, ok := barsFor[rep.Data.Prices]
		if !ok {
			continue // run on bars that are not committed (yahoo parquet, later snapshots)
		}
		if rep.Costs.BuyBPS != rep.Costs.SellBPS {
			t.Fatalf("%s: asymmetric costs are not expressible as one taker fee", path)
		}
		if rep.Params.SMA != daily.SMA {
			t.Fatalf("%s: sma %d", path, rep.Params.SMA)
		}
		reports++
		barsDir := filepath.Join(evidenceRoot, src.dir)

		// The report must describe exactly the committed file.
		bars, _, err := daily.ReadBars(filepath.Join(barsDir, src.file))
		if err != nil {
			t.Fatal(err)
		}
		if len(bars) != rep.Data.Bars || bars[0].Date.Format("2006-01-02") != rep.Data.First || bars[len(bars)-1].Date.Format("2006-01-02") != rep.Data.Last {
			t.Fatalf("%s: bars %d %s..%s, report says %d %s..%s", path, len(bars), bars[0].Date.Format("2006-01-02"), bars[len(bars)-1].Date.Format("2006-01-02"), rep.Data.Bars, rep.Data.First, rep.Data.Last)
		}

		for _, w := range rep.Windows {
			for _, want := range w.Results {
				if want.Rule != "trend_sma50" {
					continue
				}
				cfg := &models.BacktestConfig{
					ID: "parity", Name: "parity", Book: src.book,
					StartDate: mustDate(t, w.From), EndDate: mustDate(t, w.To),
					InitialBalance: 100000, Strategy: daily.Strategy,
					StrategyParams: map[string]interface{}{"bars_file": src.file},
					SlippageModel:  "percentage", SlippageValue: rep.Costs.SlippageBPS / 1e4,
					TakerFee: rep.Costs.BuyBPS / 1e4, MakerFee: rep.Costs.BuyBPS / 1e4,
					DataSource: "file",
				}
				got, err := daily.Run(context.Background(), cfg, barsDir)
				if err != nil {
					t.Fatalf("%s %s: %v", filepath.Base(path), w.Label, err)
				}
				s := got.Summary
				name := filepath.Base(path) + " " + w.From + ".." + w.To
				if s.TotalReturnPercent != want.ReturnPct {
					t.Errorf("%s: return %v, registered %v", name, s.TotalReturnPercent, want.ReturnPct)
				}
				if s.TotalTrades != want.RoundTrips || len(got.Trades) != want.RoundTrips {
					t.Errorf("%s: trips %d (trades %d), registered %d", name, s.TotalTrades, len(got.Trades), want.RoundTrips)
				}
				if s.MaxDrawdownPercent != want.MaxDDPct {
					t.Errorf("%s: max dd %v, registered %v", name, s.MaxDrawdownPercent, want.MaxDDPct)
				}
				if got.Metadata["exposure_pct"] != want.ExposurePct {
					t.Errorf("%s: exposure %v, registered %v", name, got.Metadata["exposure_pct"], want.ExposurePct)
				}
				if got.Metadata["cost_pct"] != want.CostPct {
					t.Errorf("%s: cost %v, registered %v", name, got.Metadata["cost_pct"], want.CostPct)
				}
				if got.Metadata["window_bars"] != w.Bars {
					t.Errorf("%s: window bars %v, registered %d", name, got.Metadata["window_bars"], w.Bars)
				}
				checked++
			}
		}
	}
	// 4 btc_mxn reports x 10 windows, 2 x 7, and 4 btc_usd reports x 7.
	if reports != 10 || checked != 82 {
		t.Fatalf("checked %d windows in %d reports, want 82 in 10: evidence moved?", checked, reports)
	}
}

// The engine routes sma50_daily to this path and keeps the numbers.
func TestEngineRoutesDailyStrategy(t *testing.T) {
	barsDir := filepath.Join(evidenceRoot, "evidence-2026-09-27")
	eng := engine.NewEngine(nil, nil, logger.NewDefault(), nil)
	cfg := &models.BacktestConfig{
		ID: "bt-daily", Name: "sma50 2025", Book: "btc_mxn",
		StartDate: mustDate(t, "2025-01-01"), EndDate: mustDate(t, "2025-12-31"),
		InitialBalance: 100000, Strategy: daily.Strategy,
		SlippageModel: "percentage", SlippageValue: 0.001, TakerFee: 0.0078, MakerFee: 0.006,
		DataSource: "file",
	}
	if _, err := eng.Run(context.Background(), cfg); err == nil {
		t.Fatal("expected an error without BACKTEST_DAILY_BARS_DIR")
	}
	eng.SetDailyBarsDir(barsDir)
	res, err := eng.Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Registered in evidence-2026-09-28/btc-mxn-taker-same-windows.json.
	if res.Status != "completed" || res.Summary.TotalReturnPercent != -23.52507123070574 || res.Summary.TotalTrades != 16 {
		t.Fatalf("status %s return %v trips %d", res.Status, res.Summary.TotalReturnPercent, res.Summary.TotalTrades)
	}
	if res.Metadata["bars_file"] != "btc_mxn_daily_bitso.csv" || res.Metadata["engine"] != "dailyrule" {
		t.Fatalf("metadata %v", res.Metadata)
	}
}
