package analyzer

import (
	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/backtesting/internal/portfolio"
)

// PerformanceAnalyzer analyzes backtest performance
type PerformanceAnalyzer struct {
	logger logger.Logger
}

// NewAnalyzer creates a new performance analyzer
func NewAnalyzer(log logger.Logger) *PerformanceAnalyzer {
	return &PerformanceAnalyzer{
		logger: log,
	}
}

// Analyze performs complete performance analysis
func (a *PerformanceAnalyzer) Analyze(
	port *portfolio.VirtualPortfolio,
	trades []models.Trade,
	equityCurve []models.EquityPoint,
) (*models.PerformanceSummary, error) {
	
	summary := &models.PerformanceSummary{
		InitialBalance: port.InitialBalance,
		FinalBalance:   port.GetBalance(),
		PeakBalance:    port.GetSummary().PeakBalance,
	}
	
	// Calculate returns
	totalReturn, annualizedReturn := calculateReturns(
		port.InitialBalance,
		port.GetBalance(),
		len(equityCurve),
	)
	summary.TotalReturn = totalReturn
	summary.TotalReturnPercent = (totalReturn / port.InitialBalance) * 100
	summary.AnnualizedReturn = annualizedReturn
	
	// Calculate risk metrics
	if len(equityCurve) > 0 {
		returns := extractReturns(equityCurve)
		summary.Volatility = calculateVolatility(returns)
		summary.SharpeRatio = calculateSharpeRatio(returns, 0.0)
		summary.SortinoRatio = calculateSortinoRatio(returns, 0.0)
	}
	
	// Calculate drawdown
	if len(equityCurve) > 0 {
		maxDD, maxDDPercent := calculateMaxDrawdown(equityCurve)
		summary.MaxDrawdown = maxDD
		summary.MaxDrawdownPercent = maxDDPercent
	}
	
	// Calculate trade statistics
	if len(trades) > 0 {
		summary.TotalTrades = len(trades)
		
		winningTrades, losingTrades := classifyTrades(trades)
		summary.WinningTrades = len(winningTrades)
		summary.LosingTrades = len(losingTrades)
		summary.WinRate = float64(len(winningTrades)) / float64(len(trades))
		
		if len(winningTrades) > 0 {
			summary.AverageWin = calculateAverageWin(winningTrades)
		}
		if len(losingTrades) > 0 {
			summary.AverageLoss = calculateAverageLoss(losingTrades)
		}
		
		summary.ProfitFactor = calculateProfitFactor(trades)
		summary.AverageHoldingTime = int64(calculateAverageHoldingTime(trades).Seconds())
		summary.MaxPosition = calculateMaxPosition(trades)
	}
	
	// Calculate P&L
	summary.GrossProfitLoss = calculateGrossPL(trades)
	summary.NetProfitLoss = summary.TotalReturn
	summary.TotalCommissions = port.GetSummary().TotalCommissions
	
	if a.logger != nil {
		a.logger.Info("Performance analysis complete", map[string]interface{}{
			"total_return": summary.TotalReturn,
			"sharpe_ratio": summary.SharpeRatio,
			"win_rate":     summary.WinRate,
		})
	}
	
	return summary, nil
}

// CalculateMetrics calculates metrics for a portfolio
func (a *PerformanceAnalyzer) CalculateMetrics(port *portfolio.VirtualPortfolio) (*models.PerformanceSummary, error) {
	trades := port.GetTrades()
	// Create minimal equity curve
	equityCurve := []models.EquityPoint{
		{
			Balance: port.InitialBalance,
			Equity:  port.InitialBalance,
		},
		{
			Balance: port.GetBalance(),
			Equity:  port.GetBalance(),
		},
	}
	
	return a.Analyze(port, trades, equityCurve)
}

// GenerateReport generates a text report of the results
func (a *PerformanceAnalyzer) GenerateReport(result *models.BacktestResult) (string, error) {
	return generateTextReport(result), nil
}

// Helper functions

func classifyTrades(trades []models.Trade) (winning, losing []models.Trade) {
	winning = make([]models.Trade, 0)
	losing = make([]models.Trade, 0)
	
	for _, trade := range trades {
		if trade.IsWinning() {
			winning = append(winning, trade)
		} else if trade.IsLosing() {
			losing = append(losing, trade)
		}
	}
	
	return winning, losing
}

func extractReturns(equityCurve []models.EquityPoint) []float64 {
	if len(equityCurve) < 2 {
		return []float64{}
	}
	
	returns := make([]float64, len(equityCurve)-1)
	for i := 1; i < len(equityCurve); i++ {
		if equityCurve[i-1].Equity > 0 {
			returns[i-1] = (equityCurve[i].Equity - equityCurve[i-1].Equity) / equityCurve[i-1].Equity
		}
	}
	
	return returns
}

