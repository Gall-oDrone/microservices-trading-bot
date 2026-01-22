package optimizer

import (
	"sort"

	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/models"
)

// Evaluator evaluates and ranks optimization results
type Evaluator struct {
	metric string
	logger logger.Logger
}

// NewEvaluator creates a new evaluator
func NewEvaluator(metric string, log logger.Logger) *Evaluator {
	if metric == "" {
		metric = "sharpe_ratio"
	}

	return &Evaluator{
		metric: metric,
		logger: log,
	}
}

// EvaluateAndRank evaluates results and ranks them by the specified metric
func (e *Evaluator) EvaluateAndRank(results []*OptimizationResult) []*OptimizationResult {
	if len(results) == 0 {
		return results
	}

	// Calculate scores for each result
	for _, result := range results {
		result.Score = e.calculateScore(result)
	}

	// Sort by score (descending)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Assign ranks
	for i, result := range results {
		result.Rank = i + 1
	}

	return results
}

// calculateScore calculates the score for a result based on the metric
func (e *Evaluator) calculateScore(result *OptimizationResult) float64 {
	if result.Result == nil || result.Result.Summary == nil {
		return -1e10 // Very low score for invalid results
	}

	summary := result.Result.Summary

	switch e.metric {
	case "sharpe_ratio":
		return summary.SharpeRatio

	case "sortino_ratio":
		return summary.SortinoRatio

	case "total_return":
		return summary.TotalReturn

	case "profit_factor":
		return summary.ProfitFactor

	case "win_rate":
		return summary.WinRate

	case "max_drawdown":
		// For drawdown, lower is better, so negate
		return -summary.MaxDrawdown

	case "total_trades":
		return float64(summary.TotalTrades)

	case "average_win":
		return summary.AverageWin

	case "composite":
		// Composite score: weighted combination of multiple metrics
		return e.calculateCompositeScore(summary)

	default:
		e.logger.Warn("Unknown metric, defaulting to sharpe_ratio", map[string]interface{}{
			"metric": e.metric,
		})
		return summary.SharpeRatio
	}
}

// calculateCompositeScore calculates a weighted composite score
func (e *Evaluator) calculateCompositeScore(summary *models.PerformanceSummary) float64 {
	// Weighted combination of key metrics
	weights := map[string]float64{
		"sharpe":        0.30,
		"total_return":  0.25,
		"profit_factor": 0.20,
		"win_rate":      0.15,
		"max_drawdown":  0.10,
	}

	// Normalize metrics to 0-1 range (approximate)
	sharpeNorm := normalizeMetric(summary.SharpeRatio, -2, 3)
	returnNorm := normalizeMetric(summary.TotalReturn, -0.5, 2.0)
	pfNorm := normalizeMetric(summary.ProfitFactor, 0, 3)
	wrNorm := summary.WinRate                                    // Already 0-1
	ddNorm := 1.0 - normalizeMetric(summary.MaxDrawdown, 0, 0.5) // Invert drawdown

	score := weights["sharpe"]*sharpeNorm +
		weights["total_return"]*returnNorm +
		weights["profit_factor"]*pfNorm +
		weights["win_rate"]*wrNorm +
		weights["max_drawdown"]*ddNorm

	return score
}

// normalizeMetric normalizes a metric to 0-1 range
func normalizeMetric(value, min, max float64) float64 {
	if max <= min {
		return 0
	}

	normalized := (value - min) / (max - min)

	// Clamp to 0-1
	if normalized < 0 {
		return 0
	}
	if normalized > 1 {
		return 1
	}

	return normalized
}
