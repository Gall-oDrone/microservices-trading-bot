package optimizer

import (
	"testing"

	"bitso-trading-platform/backtesting/internal/models"

	"github.com/stretchr/testify/assert"
)

func TestValidateOptimizationConfig(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		err := validateOptimizationConfig(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config is nil")
	})

	t.Run("missing name", func(t *testing.T) {
		config := &OptimizationConfig{}
		err := validateOptimizationConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("valid config with defaults", func(t *testing.T) {
		config := &OptimizationConfig{
			Name:       "Test Optimization",
			BaseConfig: &models.BacktestConfig{},
			Parameters: map[string]*ParameterRange{
				"test_param": {
					Min:  1.0,
					Max:  10.0,
					Step: 1.0,
				},
			},
		}
		err := validateOptimizationConfig(config)
		assert.NoError(t, err)
		assert.Equal(t, "sharpe_ratio", config.Metric)
		assert.Equal(t, 4, config.MaxWorkers)
		assert.Equal(t, 10, config.TopN)
	})

	t.Run("invalid parameter range", func(t *testing.T) {
		config := &OptimizationConfig{
			Name:       "Test",
			BaseConfig: &models.BacktestConfig{},
			Parameters: map[string]*ParameterRange{
				"bad_param": {
					Min:  10.0,
					Max:  1.0, // Max < Min
					Step: 1.0,
				},
			},
		}
		err := validateOptimizationConfig(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max must be greater than min")
	})
}

func TestParameterGrid(t *testing.T) {
	t.Run("empty grid", func(t *testing.T) {
		grid := NewParameterGrid(map[string]*ParameterRange{})
		combinations := grid.Generate()
		assert.Empty(t, combinations)
		assert.Equal(t, 0, grid.Count())
	})

	t.Run("single parameter range", func(t *testing.T) {
		grid := NewParameterGrid(map[string]*ParameterRange{
			"param1": {
				Min:  1.0,
				Max:  3.0,
				Step: 1.0,
			},
		})

		combinations := grid.Generate()
		assert.Len(t, combinations, 3)
		assert.Equal(t, 3, grid.Count())

		// Check values
		assert.Equal(t, 1.0, combinations[0]["param1"])
		assert.Equal(t, 2.0, combinations[1]["param1"])
		assert.Equal(t, 3.0, combinations[2]["param1"])
	})

	t.Run("multiple parameters", func(t *testing.T) {
		grid := NewParameterGrid(map[string]*ParameterRange{
			"param1": {
				Min:  1.0,
				Max:  2.0,
				Step: 1.0,
			},
			"param2": {
				Min:  10.0,
				Max:  20.0,
				Step: 10.0,
			},
		})

		combinations := grid.Generate()
		assert.Len(t, combinations, 4) // 2 * 2 = 4
		assert.Equal(t, 4, grid.Count())

		// Each combination should have both parameters
		for _, combo := range combinations {
			assert.Contains(t, combo, "param1")
			assert.Contains(t, combo, "param2")
		}
	})

	t.Run("discrete values", func(t *testing.T) {
		grid := NewParameterGrid(map[string]*ParameterRange{
			"strategy": {
				Values: []interface{}{"rsi", "macd", "bollinger"},
			},
		})

		combinations := grid.Generate()
		assert.Len(t, combinations, 3)
		assert.Equal(t, 3, grid.Count())

		// Check discrete values
		strategies := make([]interface{}, len(combinations))
		for i, combo := range combinations {
			strategies[i] = combo["strategy"]
		}
		assert.Contains(t, strategies, "rsi")
		assert.Contains(t, strategies, "macd")
		assert.Contains(t, strategies, "bollinger")
	})
}

func TestGenerateRangeValues(t *testing.T) {
	t.Run("basic range", func(t *testing.T) {
		values := generateRangeValues(1.0, 5.0, 1.0)
		assert.Len(t, values, 5)
		assert.Equal(t, 1.0, values[0])
		assert.Equal(t, 5.0, values[4])
	})

	t.Run("fractional step", func(t *testing.T) {
		values := generateRangeValues(0.0, 1.0, 0.25)
		assert.Len(t, values, 5)
		assert.Equal(t, 0.0, values[0])
		assert.Equal(t, 0.25, values[1])
		assert.Equal(t, 1.0, values[4])
	})

	t.Run("non-exact range", func(t *testing.T) {
		values := generateRangeValues(0.0, 1.0, 0.3)
		// Should generate: 0.0, 0.3, 0.6, 0.9
		// 1.2 would be > max, so stopped
		assert.Len(t, values, 4)
	})
}

func TestEvaluator(t *testing.T) {
	t.Run("evaluate by sharpe ratio", func(t *testing.T) {
		evaluator := NewEvaluator("sharpe_ratio", nil)

		results := []*OptimizationResult{
			{
				Result: &models.BacktestResult{
					Summary: &models.PerformanceSummary{
						SharpeRatio: 2.5,
					},
				},
			},
			{
				Result: &models.BacktestResult{
					Summary: &models.PerformanceSummary{
						SharpeRatio: 1.5,
					},
				},
			},
			{
				Result: &models.BacktestResult{
					Summary: &models.PerformanceSummary{
						SharpeRatio: 3.0,
					},
				},
			},
		}

		ranked := evaluator.EvaluateAndRank(results)

		// Check order (descending by Sharpe)
		assert.Equal(t, 3.0, ranked[0].Result.Summary.SharpeRatio)
		assert.Equal(t, 2.5, ranked[1].Result.Summary.SharpeRatio)
		assert.Equal(t, 1.5, ranked[2].Result.Summary.SharpeRatio)

		// Check ranks
		assert.Equal(t, 1, ranked[0].Rank)
		assert.Equal(t, 2, ranked[1].Rank)
		assert.Equal(t, 3, ranked[2].Rank)

		// Check scores
		assert.Equal(t, 3.0, ranked[0].Score)
		assert.Equal(t, 2.5, ranked[1].Score)
		assert.Equal(t, 1.5, ranked[2].Score)
	})

	t.Run("evaluate by total return", func(t *testing.T) {
		evaluator := NewEvaluator("total_return", nil)

		results := []*OptimizationResult{
			{
				Result: &models.BacktestResult{
					Summary: &models.PerformanceSummary{
						TotalReturn: 0.5,
					},
				},
			},
			{
				Result: &models.BacktestResult{
					Summary: &models.PerformanceSummary{
						TotalReturn: 1.2,
					},
				},
			},
		}

		ranked := evaluator.EvaluateAndRank(results)

		assert.Equal(t, 1.2, ranked[0].Result.Summary.TotalReturn)
		assert.Equal(t, 0.5, ranked[1].Result.Summary.TotalReturn)
	})

	t.Run("handle nil results", func(t *testing.T) {
		evaluator := NewEvaluator("sharpe_ratio", nil)

		results := []*OptimizationResult{
			{Result: nil},
			{
				Result: &models.BacktestResult{
					Summary: nil,
				},
			},
		}

		ranked := evaluator.EvaluateAndRank(results)

		// Invalid results should get very low score
		assert.True(t, ranked[0].Score < -1e9)
		assert.True(t, ranked[1].Score < -1e9)
	})
}

func TestNormalizeMetric(t *testing.T) {
	t.Run("value in range", func(t *testing.T) {
		normalized := normalizeMetric(1.5, 1.0, 2.0)
		assert.Equal(t, 0.5, normalized)
	})

	t.Run("value below min", func(t *testing.T) {
		normalized := normalizeMetric(0.5, 1.0, 2.0)
		assert.Equal(t, 0.0, normalized)
	})

	t.Run("value above max", func(t *testing.T) {
		normalized := normalizeMetric(2.5, 1.0, 2.0)
		assert.Equal(t, 1.0, normalized)
	})

	t.Run("invalid range", func(t *testing.T) {
		normalized := normalizeMetric(1.5, 2.0, 1.0)
		assert.Equal(t, 0.0, normalized)
	})
}
