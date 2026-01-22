package analyzer

import (
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/backtesting/internal/portfolio"
)

func TestCalculateReturns(t *testing.T) {
	total, annualized := calculateReturns(100000.0, 110000.0, 365)

	if total != 10000.0 {
		t.Errorf("Expected total return 10000.0, got %f", total)
	}

	// Annualized should be ~10% for 1 year
	if annualized < 0.09 || annualized > 0.11 {
		t.Errorf("Expected annualized return ~0.10, got %f", annualized)
	}
}

func TestCalculateVolatility(t *testing.T) {
	returns := []float64{0.01, -0.01, 0.02, -0.02, 0.01}

	vol := calculateVolatility(returns)

	if vol <= 0 {
		t.Error("Expected positive volatility")
	}

	// Empty returns
	vol = calculateVolatility([]float64{})
	if vol != 0 {
		t.Error("Expected 0 volatility for empty returns")
	}
}

func TestCalculateSharpeRatio(t *testing.T) {
	// Positive returns
	returns := []float64{0.01, 0.02, 0.01, 0.03, 0.02}

	sharpe := calculateSharpeRatio(returns, 0.0)

	if sharpe <= 0 {
		t.Error("Expected positive Sharpe ratio for positive returns")
	}

	// Mixed returns
	returns = []float64{0.01, -0.01, 0.02, -0.02, 0.01}
	sharpe = calculateSharpeRatio(returns, 0.0)

	// Should still calculate
	if sharpe == 0 {
		t.Log("Sharpe ratio is 0 for mixed returns (acceptable)")
	}
}

func TestCalculateMaxDrawdown(t *testing.T) {
	equityCurve := []models.EquityPoint{
		{Equity: 100000.0},
		{Equity: 110000.0}, // Peak
		{Equity: 105000.0}, // 5000 drawdown
		{Equity: 108000.0},
		{Equity: 103000.0}, // 7000 drawdown from peak
	}

	maxDD, maxDDPercent := calculateMaxDrawdown(equityCurve)

	expectedDD := 7000.0
	if maxDD != expectedDD {
		t.Errorf("Expected max drawdown %f, got %f", expectedDD, maxDD)
	}

	expectedPercent := (7000.0 / 110000.0) * 100
	if maxDDPercent < expectedPercent-0.1 || maxDDPercent > expectedPercent+0.1 {
		t.Errorf("Expected max drawdown percent ~%f, got %f", expectedPercent, maxDDPercent)
	}
}

func TestCalculateWinRate(t *testing.T) {
	now := time.Now()

	trades := []models.Trade{
		*createTestTrade("buy", 500000.0, 510000.0, now), // Win
		*createTestTrade("buy", 500000.0, 490000.0, now), // Loss
		*createTestTrade("buy", 500000.0, 505000.0, now), // Win
	}

	winRate := calculateWinRate(trades)
	expected := 2.0 / 3.0

	if winRate < expected-0.01 || winRate > expected+0.01 {
		t.Errorf("Expected win rate ~%f, got %f", expected, winRate)
	}
}

func TestCalculateProfitFactor(t *testing.T) {
	now := time.Now()

	trades := []models.Trade{
		*createTestTrade("buy", 500000.0, 510000.0, now), // +100 profit
		*createTestTrade("buy", 500000.0, 490000.0, now), // -100 loss
	}

	pf := calculateProfitFactor(trades)

	// Should be ~1.0 for equal profit and loss
	if pf < 0.9 || pf > 1.1 {
		t.Errorf("Expected profit factor ~1.0, got %f", pf)
	}
}

func TestAnalyze(t *testing.T) {
	analyzer := NewAnalyzer(nil)

	// Create test portfolio
	port := portfolio.NewVirtualPortfolio("test", 100000.0)
	port.CurrentBalance = 110000.0

	// Create test trades
	now := time.Now()
	trades := []models.Trade{
		*createTestTrade("buy", 500000.0, 510000.0, now),
		*createTestTrade("buy", 500000.0, 505000.0, now),
	}

	// Create equity curve
	equityCurve := []models.EquityPoint{
		{Timestamp: now, Balance: 100000.0, Equity: 100000.0},
		{Timestamp: now.Add(time.Hour), Balance: 110000.0, Equity: 110000.0},
	}

	// Analyze
	summary, err := analyzer.Analyze(port, trades, equityCurve)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if summary == nil {
		t.Fatal("Expected summary to be returned")
	}

	if summary.TotalReturn != 10000.0 {
		t.Errorf("Expected total return 10000.0, got %f", summary.TotalReturn)
	}

	if summary.TotalTrades != 2 {
		t.Errorf("Expected 2 trades, got %d", summary.TotalTrades)
	}
}

func TestGenerateTextReport(t *testing.T) {
	result := models.NewBacktestResult("bt-123", "cfg-456")
	result.SetSummary(&models.PerformanceSummary{
		InitialBalance:     100000.0,
		FinalBalance:       110000.0,
		TotalReturn:        10000.0,
		TotalReturnPercent: 10.0,
		SharpeRatio:        1.5,
		WinRate:            0.65,
		TotalTrades:        10,
	})

	report := generateTextReport(result)

	if report == "" {
		t.Error("Expected non-empty report")
	}

	// Check report contains key metrics
	if !strings.Contains(report, "BACKTEST REPORT") {
		t.Error("Report should contain title")
	}
	if !strings.Contains(report, "110000.00") {
		t.Error("Report should contain final balance")
	}
	if !strings.Contains(report, "Sharpe Ratio") {
		t.Error("Report should contain Sharpe ratio")
	}
}

func TestGenerateJSONReport(t *testing.T) {
	result := models.NewBacktestResult("bt-123", "cfg-456")
	result.SetSummary(&models.PerformanceSummary{
		TotalReturn: 5000.0,
	})

	jsonData, err := generateJSONReport(result)
	if err != nil {
		t.Fatalf("generateJSONReport() error = %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("Expected non-empty JSON")
	}
}

// Helper function to create test trades
func createTestTrade(side string, entryPrice, exitPrice float64, entryTime time.Time) *models.Trade {
	trade := models.NewTrade(side, "btc_mxn", entryPrice, 0.01, 0, 0, entryTime)
	trade.Close(exitPrice, entryTime.Add(time.Hour))
	return trade
}
