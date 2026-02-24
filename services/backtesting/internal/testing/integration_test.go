package testing

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sync"

	"bitso-trading-platform/backtesting/internal/api"
	"bitso-trading-platform/backtesting/internal/data"
	"bitso-trading-platform/backtesting/internal/engine"
	"bitso-trading-platform/backtesting/internal/logger"
	"bitso-trading-platform/backtesting/internal/manager"
	"bitso-trading-platform/backtesting/internal/metrics"
	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/backtesting/internal/optimizer"
	"bitso-trading-platform/backtesting/internal/storage"
	"bitso-trading-platform/shared/pkg/health"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testMetricsCollector     *metrics.MetricsCollector
	testMetricsCollectorOnce sync.Once
)

// IntegrationTestSuite provides integration test utilities
type IntegrationTestSuite struct {
	server    *httptest.Server
	handler   *api.Handler
	manager   *manager.BacktestManager
	optimizer optimizer.Optimizer
	ctx       context.Context
	cancel    context.CancelFunc
}

// SetupIntegrationTest creates a test suite with all components
func SetupIntegrationTest(t *testing.T) *IntegrationTestSuite {
	ctx, cancel := context.WithCancel(context.Background())

	// Create logger
	appLogger := logger.New(&logger.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	})

	// Create metrics (once to avoid duplicate Prometheus registration panics)
	testMetricsCollectorOnce.Do(func() {
		testMetricsCollector = metrics.NewMetricsCollector("test")
	})
	metricsCollector := testMetricsCollector

	// Create mock data provider
	dataProvider := data.NewMarketDataProvider(
		"http://localhost:8083",
		nil, // No cache for tests
		appLogger,
		3,
		time.Second,
		metricsCollector,
	)

	// Create storage (file-based for tests, no Redis needed)
	resultStorage := storage.NewFileStorage(t.TempDir(), appLogger)

	// Create engine
	backtestEngine := engine.NewEngine(dataProvider, resultStorage, appLogger, metricsCollector)

	// Create manager
	backtestManager := manager.NewBacktestManager(
		backtestEngine,
		resultStorage,
		5, // max concurrent
		appLogger,
		metricsCollector,
	)

	// Start manager
	err := backtestManager.Start(ctx)
	require.NoError(t, err)

	// Create optimizer
	opt := optimizer.NewOptimizer(backtestEngine, resultStorage, appLogger)

	// Create API handler
	handler := api.NewHandler(backtestManager, opt, appLogger, metricsCollector)

	// Create test server
	testServer := httptest.NewServer(nil)

	// Setup routes manually for test
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/backtests", handler.HandleBacktests)
	mux.HandleFunc("/api/v1/backtests/", handler.HandleBacktestByID)
	mux.HandleFunc("/api/v1/optimizations", handler.HandleOptimizations)
	mux.HandleFunc("/api/v1/optimizations/", handler.HandleOptimizationByID)
	mux.HandleFunc("/health", health.NewHealthManager(nil).HTTPHandler())

	testServer = httptest.NewServer(mux)

	return &IntegrationTestSuite{
		server:    testServer,
		handler:   handler,
		manager:   backtestManager,
		optimizer: opt,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// TeardownIntegrationTest cleans up test suite
func (s *IntegrationTestSuite) TeardownIntegrationTest() {
	s.server.Close()
	s.manager.Stop()
	s.cancel()
}

// TestCreateBacktest tests creating a backtest via API
func TestCreateBacktest(t *testing.T) {
	suite := SetupIntegrationTest(t)
	defer suite.TeardownIntegrationTest()

	reqBody := map[string]interface{}{
		"name":            "Integration Test Backtest",
		"description":     "Test backtest from integration test",
		"start_date":      "2024-01-01T00:00:00Z",
		"end_date":        "2024-01-31T23:59:59Z",
		"book":            "btc_mxn",
		"initial_balance": 100000.0,
		"strategy":        "basic",
		"strategy_params": map[string]interface{}{
			"rsi_period": 14,
		},
		"commission_rate": 0.001,
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req, err := http.NewRequest("POST", suite.server.URL+"/api/v1/backtests", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var response map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response["success"].(bool))
	assert.Contains(t, response, "data")

	respData := response["data"].(map[string]interface{})
	assert.Contains(t, respData, "id")
	// Handler calls StartBacktest after create, so status may be "pending" or "running"
	assert.Contains(t, []string{
		string(models.BacktestStatusPending),
		string(models.BacktestStatusRunning),
	}, respData["status"])
}

// TestGetBacktest tests retrieving a backtest
func TestGetBacktest(t *testing.T) {
	suite := SetupIntegrationTest(t)
	defer suite.TeardownIntegrationTest()

	// Create a backtest first
	config := models.NewBacktestConfig(
		"Test Backtest",
		"btc_mxn",
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
	)
	config.WithInitialBalance(100000.0)
	config.WithStrategy("basic", map[string]interface{}{"rsi_period": 14})

	backtest, err := suite.manager.CreateBacktest(config)
	require.NoError(t, err)

	// Get backtest via API
	req, err := http.NewRequest("GET", suite.server.URL+"/api/v1/backtests/"+backtest.ID, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var response map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response["success"].(bool))
	respData2 := response["data"].(map[string]interface{})
	assert.Equal(t, backtest.ID, respData2["id"])
	assert.Equal(t, string(backtest.Status), respData2["status"])
}

// TestListBacktests tests listing backtests
func TestListBacktests(t *testing.T) {
	suite := SetupIntegrationTest(t)
	defer suite.TeardownIntegrationTest()

	// Create multiple backtests
	for i := 0; i < 3; i++ {
		config := models.NewBacktestConfig(
			"Test Backtest",
			"btc_mxn",
			time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
		)
		config.WithInitialBalance(100000.0)
		_, err := suite.manager.CreateBacktest(config)
		require.NoError(t, err)
	}

	// List backtests
	req, err := http.NewRequest("GET", suite.server.URL+"/api/v1/backtests", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var response map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.True(t, response["success"].(bool))
	respData3 := response["data"].(map[string]interface{})
	assert.Contains(t, respData3, "backtests")
}

// TestHealthEndpoint tests health endpoint
func TestHealthEndpoint(t *testing.T) {
	suite := SetupIntegrationTest(t)
	defer suite.TeardownIntegrationTest()

	req, err := http.NewRequest("GET", suite.server.URL+"/health", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
