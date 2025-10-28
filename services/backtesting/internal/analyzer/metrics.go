package analyzer

import (
	"math"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
)

// calculateReturns calculates total and annualized returns
func calculateReturns(initialBalance, finalBalance float64, days int) (total, annualized float64) {
	total = finalBalance - initialBalance
	
	if days == 0 || initialBalance == 0 {
		return total, 0
	}
	
	// Annualized return = (1 + total_return) ^ (365/days) - 1
	totalReturn := total / initialBalance
	years := float64(days) / 365.0
	annualized = math.Pow(1+totalReturn, 1/years) - 1
	
	return total, annualized
}

// calculateVolatility calculates return volatility (annualized)
func calculateVolatility(returns []float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	
	// Calculate mean
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	
	// Calculate variance
	variance := 0.0
	for _, r := range returns {
		diff := r - mean
		variance += diff * diff
	}
	variance /= float64(len(returns))
	
	// Standard deviation
	stdDev := math.Sqrt(variance)
	
	// Annualize (assuming daily returns)
	annualizedVol := stdDev * math.Sqrt(252) // 252 trading days
	
	return annualizedVol
}

// calculateSharpeRatio calculates the Sharpe ratio
func calculateSharpeRatio(returns []float64, riskFreeRate float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	
	// Calculate mean return
	meanReturn := 0.0
	for _, r := range returns {
		meanReturn += r
	}
	meanReturn /= float64(len(returns))
	
	// Annualize mean return
	annualizedReturn := meanReturn * 252
	
	// Calculate volatility
	vol := calculateVolatility(returns)
	
	if vol == 0 {
		return 0
	}
	
	// Sharpe = (Return - RiskFree) / Volatility
	return (annualizedReturn - riskFreeRate) / vol
}

// calculateSortinoRatio calculates the Sortino ratio (downside deviation)
func calculateSortinoRatio(returns []float64, targetReturn float64) float64 {
	if len(returns) < 2 {
		return 0
	}
	
	// Calculate mean return
	meanReturn := 0.0
	for _, r := range returns {
		meanReturn += r
	}
	meanReturn /= float64(len(returns))
	
	// Calculate downside deviation (only negative returns)
	downsideVariance := 0.0
	downsideCount := 0
	for _, r := range returns {
		if r < targetReturn {
			diff := r - targetReturn
			downsideVariance += diff * diff
			downsideCount++
		}
	}
	
	if downsideCount == 0 {
		return 0
	}
	
	downsideVariance /= float64(downsideCount)
	downsideDeviation := math.Sqrt(downsideVariance)
	
	// Annualize
	annualizedReturn := meanReturn * 252
	annualizedDownsideDeviation := downsideDeviation * math.Sqrt(252)
	
	if annualizedDownsideDeviation == 0 {
		return 0
	}
	
	return (annualizedReturn - targetReturn) / annualizedDownsideDeviation
}

// calculateMaxDrawdown calculates the maximum drawdown
func calculateMaxDrawdown(equityCurve []models.EquityPoint) (float64, float64) {
	if len(equityCurve) == 0 {
		return 0, 0
	}
	
	var maxDrawdown, maxDrawdownPercent float64
	peak := equityCurve[0].Equity
	
	for _, point := range equityCurve {
		if point.Equity > peak {
			peak = point.Equity
		}
		
		drawdown := peak - point.Equity
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
		}
		
		if peak > 0 {
			drawdownPercent := (drawdown / peak) * 100
			if drawdownPercent > maxDrawdownPercent {
				maxDrawdownPercent = drawdownPercent
			}
		}
	}
	
	return maxDrawdown, maxDrawdownPercent
}

// calculateWinRate calculates the win rate
func calculateWinRate(trades []models.Trade) float64 {
	if len(trades) == 0 {
		return 0
	}
	
	winningCount := 0
	for _, trade := range trades {
		if trade.IsWinning() {
			winningCount++
		}
	}
	
	return float64(winningCount) / float64(len(trades))
}

// calculateProfitFactor calculates the profit factor
func calculateProfitFactor(trades []models.Trade) float64 {
	grossProfit := 0.0
	grossLoss := 0.0
	
	for _, trade := range trades {
		if trade.IsWinning() {
			grossProfit += trade.ProfitLoss
		} else if trade.IsLosing() {
			grossLoss += -trade.ProfitLoss // Make positive
		}
	}
	
	if grossLoss == 0 {
		if grossProfit > 0 {
			return math.Inf(1) // Infinite profit factor (no losses)
		}
		return 0
	}
	
	return grossProfit / grossLoss
}

// calculateAverageWin calculates average winning trade P&L
func calculateAverageWin(winningTrades []models.Trade) float64 {
	if len(winningTrades) == 0 {
		return 0
	}
	
	total := 0.0
	for _, trade := range winningTrades {
		total += trade.ProfitLoss
	}
	
	return total / float64(len(winningTrades))
}

// calculateAverageLoss calculates average losing trade P&L
func calculateAverageLoss(losingTrades []models.Trade) float64 {
	if len(losingTrades) == 0 {
		return 0
	}
	
	total := 0.0
	for _, trade := range losingTrades {
		total += trade.ProfitLoss
	}
	
	return total / float64(len(losingTrades))
}

// calculateAverageHoldingTime calculates average trade duration
func calculateAverageHoldingTime(trades []models.Trade) time.Duration {
	if len(trades) == 0 {
		return 0
	}
	
	totalSeconds := int64(0)
	for _, trade := range trades {
		totalSeconds += trade.HoldingTime
	}
	
	avgSeconds := totalSeconds / int64(len(trades))
	return time.Duration(avgSeconds) * time.Second
}

// calculateMaxPosition calculates the maximum position size
func calculateMaxPosition(trades []models.Trade) float64 {
	maxPos := 0.0
	
	for _, trade := range trades {
		if trade.Amount > maxPos {
			maxPos = trade.Amount
		}
	}
	
	return maxPos
}

// calculateGrossPL calculates gross profit/loss (before commissions)
func calculateGrossPL(trades []models.Trade) float64 {
	total := 0.0
	
	for _, trade := range trades {
		total += trade.GetGrossProfit()
	}
	
	return total
}

