package daily

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
)

func baseConfig() *models.BacktestConfig {
	return &models.BacktestConfig{
		ID: "t", Name: "t", Book: "btc_mxn",
		StartDate:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:        time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		InitialBalance: 1000, Strategy: Strategy,
		SlippageModel: "percentage", SlippageValue: 0.001, TakerFee: 0.0078,
		DataSource: "file",
	}
}

func TestResolveParams(t *testing.T) {
	p, err := ResolveParams(baseConfig(), "/bars")
	if err != nil {
		t.Fatal(err)
	}
	// 78 bps + 10 bps must be daily-research's (78+10)/1e4 exactly.
	if p.Costs.Buy != (78.0+10.0)/1e4 || p.Costs.Sell != p.Costs.Buy {
		t.Fatalf("costs %+v", p.Costs)
	}
	if p.BarsFile != filepath.Join("/bars", "btc_mxn_daily_bitso.csv") {
		t.Fatalf("bars file %s", p.BarsFile)
	}

	legacy := baseConfig()
	legacy.TakerFee, legacy.CommissionRate, legacy.SlippageModel = 0, 0.006, "none"
	if p, err := ResolveParams(legacy, "/bars"); err != nil || p.Costs.Buy != 0.006 {
		t.Fatalf("legacy commission: %+v %v", p, err)
	}

	cases := map[string]func(c *models.BacktestConfig){
		"data_source":      func(c *models.BacktestConfig) { c.DataSource = "market-data" },
		"frozen at sma=50": func(c *models.BacktestConfig) { c.StrategyParams = map[string]interface{}{"sma": 20.0} },
		"relative path": func(c *models.BacktestConfig) {
			c.StrategyParams = map[string]interface{}{"bars_file": "../secrets.csv"}
		},
		"relative path ":   func(c *models.BacktestConfig) { c.StrategyParams = map[string]interface{}{"bars_file": "/etc/passwd"} },
		"non-empty string": func(c *models.BacktestConfig) { c.StrategyParams = map[string]interface{}{"bars_file": 3.0} },
		"slippage_model":   func(c *models.BacktestConfig) { c.SlippageModel = "fixed" },
	}
	for want, mutate := range cases {
		c := baseConfig()
		mutate(c)
		if _, err := ResolveParams(c, "/bars"); err == nil || !strings.Contains(err.Error(), strings.TrimSpace(want)) {
			t.Errorf("%s: err = %v", want, err)
		}
	}
	if _, err := ResolveParams(baseConfig(), ""); err != ErrNoBarsDir {
		t.Errorf("no dir: %v", err)
	}
	ok := baseConfig()
	ok.StrategyParams = map[string]interface{}{"sma": 50.0, "bars_file": "sub/x.csv"}
	if _, err := ResolveParams(ok, "/bars"); err != nil {
		t.Errorf("sma 50 + nested file: %v", err)
	}
}

func writeCSV(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadBarsRejectsBadInput(t *testing.T) {
	dir := t.TempDir()
	writeCSV(t, dir, "dup.csv", "date,open,high,low,close\n2024-01-01,1,1,1,1\n2024-01-01,1,1,1,1\n")
	writeCSV(t, dir, "nocol.csv", "date,open,high,low\n2024-01-01,1,1,1\n")
	writeCSV(t, dir, "nan.csv", "date,open,high,low,close\n2024-01-01,1,1,1,x\n")
	writeCSV(t, dir, "empty.csv", "date,open,high,low,close\n")
	for _, f := range []string{"dup.csv", "nocol.csv", "nan.csv", "empty.csv"} {
		if _, _, err := ReadBars(filepath.Join(dir, f)); err == nil {
			t.Errorf("%s: expected error", f)
		}
	}
}

// The money view is consistent with the simulator's headline numbers.
func TestRunResultIsConsistent(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("date,book,open,high,low,close\n")
	d := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	price := 100.0
	for i := 0; i < 200; i++ {
		// A rise, a fall, a rise: enough to cross SMA50 both ways.
		switch {
		case i < 80:
			price *= 1.01
		case i < 130:
			price *= 0.985
		default:
			price *= 1.012
		}
		open := price * 0.999
		b.WriteString(d.AddDate(0, 0, i).Format("2006-01-02") + ",btc_mxn," +
			ftoa(open) + "," + ftoa(price*1.01) + "," + ftoa(price*0.98) + "," + ftoa(price) + "\n")
	}
	writeCSV(t, dir, "btc_mxn_daily_bitso.csv", b.String())

	cfg := baseConfig()
	cfg.StartDate, cfg.EndDate = d, d.AddDate(0, 0, 199)
	res, err := Run(context.Background(), cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	s := res.Summary
	if res.Status != "completed" || s.TotalTrades < 2 || len(res.Trades) != s.TotalTrades {
		t.Fatalf("status %s trips %d trades %d", res.Status, s.TotalTrades, len(res.Trades))
	}
	pl := 0.0
	for _, tr := range res.Trades {
		pl += tr.ProfitLoss
		if !tr.ExitTime.After(tr.EntryTime) && tr.StrategySignal != "close_out_at_window_end" {
			t.Errorf("trade %s exits before it enters", tr.ID)
		}
	}
	if math.Abs(pl-s.TotalReturn) > 1e-9 {
		t.Errorf("sum of trade P&L %v != total return %v", pl, s.TotalReturn)
	}
	if math.Abs(s.FinalBalance-cfg.InitialBalance*(1+s.TotalReturnPercent/100)) > 1e-9 {
		t.Errorf("final balance %v", s.FinalBalance)
	}
	if len(res.EquityCurve) != 200 {
		t.Errorf("equity points %d", len(res.EquityCurve))
	}
	maxDD := 0.0
	for _, p := range res.EquityCurve {
		maxDD = math.Max(maxDD, p.Drawdown)
	}
	if maxDD*100 != s.MaxDrawdownPercent {
		t.Errorf("curve drawdown %v != summary %v", maxDD*100, s.MaxDrawdownPercent)
	}
	costs := res.Metadata["cost_pct"].(float64) / 100 * cfg.InitialBalance
	if math.Abs(s.TotalCommissions+res.Metadata["total_slippage"].(float64)-costs) > 1e-9 {
		t.Errorf("commissions %v + slippage %v != costs %v", s.TotalCommissions, res.Metadata["total_slippage"], costs)
	}
	if s.WinningTrades+s.LosingTrades > s.TotalTrades || s.Volatility <= 0 {
		t.Errorf("summary %+v", s)
	}

	// A window too short to trade fails cleanly.
	cfg.StartDate, cfg.EndDate = d.AddDate(0, 0, 500), d.AddDate(0, 0, 600)
	if res, err := Run(context.Background(), cfg, dir); err == nil || res.Status != "failed" {
		t.Fatalf("empty window: %v %v", res.Status, err)
	}
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
