package testing

import (
	"context"
	"testing"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/backtesting/internal/optimizer"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2EBacktestFlow tests a complete backtest flow from creation to completion
func TestE2EBacktestFlow(t *testing.T) {
	// This is a simplified E2E test
	// Full E2E would require actual market data service

	suite := SetupIntegrationTest(t)
	defer suite.TeardownIntegrationTest()

	// Create backtest config
	config := models.NewBacktestConfig(
		"E2E Test Backtest",
		"btc_mxn",
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
	)
	config.WithInitialBalance(100000.0)
	config.WithStrategy("basic", map[string]interface{}{
		"rsi_period":     14,
		"rsi_oversold":   30,
		"rsi_overbought": 70,
	})

	// Create backtest
	backtest, err := suite.manager.CreateBacktest(config)
	require.NoError(t, err)
	assert.NotEmpty(t, backtest.ID)
	assert.Equal(t, "pending", backtest.Status)

	// Start backtest
	err = suite.manager.StartBacktest(backtest.ID)
	require.NoError(t, err)

	// Wait a bit for processing (in real E2E, would wait for completion)
	time.Sleep(100 * time.Millisecond)

	// Get updated backtest status
	updated, err := suite.manager.GetBacktest(backtest.ID)
	require.NoError(t, err)
	assert.NotNil(t, updated)
}

// TestE2EOptimizationFlow tests a complete optimization flow
func TestE2EOptimizationFlow(t *testing.T) {
	suite := SetupIntegrationTest(t)
	defer suite.TeardownIntegrationTest()

	ctx := context.Background()

	// Create base config
	baseConfig := models.NewBacktestConfig(
		"E2E Optimization",
		"btc_mxn",
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
	)
	baseConfig.WithInitialBalance(100000.0)
	baseConfig.WithStrategy("basic", map[string]interface{}{})

	// Create optimization config
	optConfig := &optimizer.OptimizationConfig{
		Name:       "E2E Optimization Test",
		BaseConfig: baseConfig,
		Parameters: map[string]*optimizer.ParameterRange{
			"rsi_period": {
				Min:  10.0,
				Max:  14.0,
				Step: 2.0,
			},
		},
		Metric:     "sharpe_ratio",
		MaxWorkers: 2,
		TopN:       3,
	}

	// Start optimization
	opt, err := suite.optimizer.Optimize(ctx, optConfig)
	require.NoError(t, err)
	assert.NotEmpty(t, opt.ID)
	assert.Equal(t, "pending", opt.Status)

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Get optimization status
	updated, err := suite.optimizer.GetOptimization(opt.ID)
	require.NoError(t, err)
	assert.NotNil(t, updated)
}

// TestE2ECancellation tests cancelling operations
func TestE2ECancellation(t *testing.T) {
	suite := SetupIntegrationTest(t)
	defer suite.TeardownIntegrationTest()

	ctx := context.Background()

	// Create optimization
	baseConfig := models.NewBacktestConfig(
		"Cancellation Test",
		"btc_mxn",
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 7, 23, 59, 59, 0, time.UTC),
	)

	optConfig := &optimizer.OptimizationConfig{
		Name:       "Cancellation Test",
		BaseConfig: baseConfig,
		Parameters: map[string]*optimizer.ParameterRange{
			"rsi_period": {
				Min:  10.0,
				Max:  20.0,
				Step: 1.0,
			},
		},
		Metric:     "sharpe_ratio",
		MaxWorkers: 2,
		TopN:       3,
	}

	opt, err := suite.optimizer.Optimize(ctx, optConfig)
	require.NoError(t, err)

	// Cancel immediately
	err = suite.optimizer.CancelOptimization(opt.ID)
	require.NoError(t, err)

	// Verify cancelled
	updated, err := suite.optimizer.GetOptimization(opt.ID)
	require.NoError(t, err)
	assert.Equal(t, "cancelled", updated.Status)
}
