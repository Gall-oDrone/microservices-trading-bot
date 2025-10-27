package api

import (
	"net/http"
	"sync"

	"bitso-trading-platform/api-gateway/internal/client"
	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
)

// AggregationHandler handles aggregated data requests from multiple services
type AggregationHandler struct {
	marketDataClient  client.MarketDataClient
	orderClient       client.OrderManagementClient
	strategyClient    client.StrategyExecutorClient
	logger            *logger.Logger
	metrics           *metrics.MetricsCollector
}

// NewAggregationHandler creates a new aggregation handler
func NewAggregationHandler(
	marketDataClient client.MarketDataClient,
	orderClient client.OrderManagementClient,
	strategyClient client.StrategyExecutorClient,
	logger *logger.Logger,
	metrics *metrics.MetricsCollector,
) *AggregationHandler {
	return &AggregationHandler{
		marketDataClient: marketDataClient,
		orderClient:      orderClient,
		strategyClient:   strategyClient,
		logger:           logger.WithComponent("aggregation-handler"),
		metrics:          metrics,
	}
}

// DashboardData represents aggregated dashboard data
type DashboardData struct {
	MarketSummary    interface{}   `json:"market_summary,omitempty"`
	ActiveOrders     []interface{} `json:"active_orders,omitempty"`
	PositionSummary  interface{}   `json:"position_summary,omitempty"`
	ActiveStrategies []interface{} `json:"active_strategies,omitempty"`
	Errors           []string      `json:"errors,omitempty"`
}

// HandleGetDashboard handles GET /api/v1/dashboard
func (h *AggregationHandler) HandleGetDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	h.logger.Debug("Getting dashboard data", nil)

	ctx := r.Context()
	dashboard := &DashboardData{
		Errors: make([]string, 0),
	}

	// Use WaitGroup to fetch data concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Fetch market summary
	wg.Add(1)
	go func() {
		defer wg.Done()
		if summary, err := h.marketDataClient.GetMarketSummary(ctx); err == nil {
			mu.Lock()
			dashboard.MarketSummary = summary
			mu.Unlock()
		} else {
			mu.Lock()
			dashboard.Errors = append(dashboard.Errors, "Failed to fetch market summary")
			mu.Unlock()
			h.logger.Error("Dashboard: failed to get market summary", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	// Fetch active orders
	wg.Add(1)
	go func() {
		defer wg.Done()
		if orders, err := h.orderClient.GetActiveOrders(ctx); err == nil {
			mu.Lock()
			dashboard.ActiveOrders = make([]interface{}, len(orders))
			for i, order := range orders {
				dashboard.ActiveOrders[i] = order
			}
			mu.Unlock()
		} else {
			mu.Lock()
			dashboard.Errors = append(dashboard.Errors, "Failed to fetch active orders")
			mu.Unlock()
			h.logger.Error("Dashboard: failed to get active orders", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	// Fetch position summary
	wg.Add(1)
	go func() {
		defer wg.Done()
		if summary, err := h.orderClient.GetPositionSummary(ctx); err == nil {
			mu.Lock()
			dashboard.PositionSummary = summary
			mu.Unlock()
		} else {
			mu.Lock()
			dashboard.Errors = append(dashboard.Errors, "Failed to fetch position summary")
			mu.Unlock()
			h.logger.Error("Dashboard: failed to get position summary", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	// Fetch active strategies
	wg.Add(1)
	go func() {
		defer wg.Done()
		if strategies, err := h.strategyClient.ListStrategies(ctx); err == nil {
			mu.Lock()
			// Filter only active strategies
			activeStrategies := make([]interface{}, 0)
			for _, strategy := range strategies {
				if strategy.Status == "active" {
					activeStrategies = append(activeStrategies, strategy)
				}
			}
			dashboard.ActiveStrategies = activeStrategies
			mu.Unlock()
		} else {
			mu.Lock()
			dashboard.Errors = append(dashboard.Errors, "Failed to fetch strategies")
			mu.Unlock()
			h.logger.Error("Dashboard: failed to get strategies", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	SuccessResponse(w, r, dashboard)
}

// PortfolioOverview represents portfolio overview data
type PortfolioOverview struct {
	Positions       interface{} `json:"positions,omitempty"`
	PositionSummary interface{} `json:"position_summary,omitempty"`
	ActiveOrders    interface{} `json:"active_orders,omitempty"`
	TotalValue      float64     `json:"total_value"`
	TotalPnL        float64     `json:"total_pnl"`
	Errors          []string    `json:"errors,omitempty"`
}

// HandleGetPortfolio handles GET /api/v1/portfolio
func (h *AggregationHandler) HandleGetPortfolio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	h.logger.Debug("Getting portfolio overview", nil)

	ctx := r.Context()
	portfolio := &PortfolioOverview{
		Errors: make([]string, 0),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	// Fetch positions
	wg.Add(1)
	go func() {
		defer wg.Done()
		if positions, err := h.orderClient.ListPositions(ctx, &client.PositionFilters{}); err == nil {
			mu.Lock()
			portfolio.Positions = positions
			mu.Unlock()
		} else {
			mu.Lock()
			portfolio.Errors = append(portfolio.Errors, "Failed to fetch positions")
			mu.Unlock()
		}
	}()

	// Fetch position summary
	wg.Add(1)
	go func() {
		defer wg.Done()
		if summary, err := h.orderClient.GetPositionSummary(ctx); err == nil {
			mu.Lock()
			portfolio.PositionSummary = summary
			portfolio.TotalPnL = summary.TotalPnL
			mu.Unlock()
		} else {
			mu.Lock()
			portfolio.Errors = append(portfolio.Errors, "Failed to fetch position summary")
			mu.Unlock()
		}
	}()

	// Fetch active orders
	wg.Add(1)
	go func() {
		defer wg.Done()
		if orders, err := h.orderClient.GetActiveOrders(ctx); err == nil {
			mu.Lock()
			portfolio.ActiveOrders = orders
			mu.Unlock()
		} else {
			mu.Lock()
			portfolio.Errors = append(portfolio.Errors, "Failed to fetch active orders")
			mu.Unlock()
		}
	}()

	wg.Wait()

	SuccessResponse(w, r, portfolio)
}

// TradingOverview represents trading overview data
type TradingOverview struct {
	MarketSummary interface{}   `json:"market_summary,omitempty"`
	RecentOrders  []interface{} `json:"recent_orders,omitempty"`
	Strategies    []interface{} `json:"strategies,omitempty"`
	Errors        []string      `json:"errors,omitempty"`
}

// HandleGetTradingOverview handles GET /api/v1/trading/overview
func (h *AggregationHandler) HandleGetTradingOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	h.logger.Debug("Getting trading overview", nil)

	ctx := r.Context()
	overview := &TradingOverview{
		Errors: make([]string, 0),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	// Fetch market summary
	wg.Add(1)
	go func() {
		defer wg.Done()
		if summary, err := h.marketDataClient.GetMarketSummary(ctx); err == nil {
			mu.Lock()
			overview.MarketSummary = summary
			mu.Unlock()
		} else {
			mu.Lock()
			overview.Errors = append(overview.Errors, "Failed to fetch market summary")
			mu.Unlock()
		}
	}()

	// Fetch recent orders
	wg.Add(1)
	go func() {
		defer wg.Done()
		filters := &client.OrderFilters{Limit: 20}
		if orderList, err := h.orderClient.ListOrders(ctx, filters); err == nil {
			mu.Lock()
			overview.RecentOrders = make([]interface{}, len(orderList.Orders))
			for i, order := range orderList.Orders {
				overview.RecentOrders[i] = order
			}
			mu.Unlock()
		} else {
			mu.Lock()
			overview.Errors = append(overview.Errors, "Failed to fetch recent orders")
			mu.Unlock()
		}
	}()

	// Fetch strategies
	wg.Add(1)
	go func() {
		defer wg.Done()
		if strategies, err := h.strategyClient.ListStrategies(ctx); err == nil {
			mu.Lock()
			overview.Strategies = make([]interface{}, len(strategies))
			for i, strategy := range strategies {
				overview.Strategies[i] = strategy
			}
			mu.Unlock()
		} else {
			mu.Lock()
			overview.Errors = append(overview.Errors, "Failed to fetch strategies")
			mu.Unlock()
		}
	}()

	wg.Wait()

	SuccessResponse(w, r, overview)
}

// SystemStatus represents system-wide status
type SystemStatus struct {
	MarketDataStatus       string   `json:"market_data_status"`
	OrderManagementStatus  string   `json:"order_management_status"`
	StrategyExecutorStatus string   `json:"strategy_executor_status"`
	OverallStatus          string   `json:"overall_status"`
	Errors                 []string `json:"errors,omitempty"`
}

// HandleGetSystemStatus handles GET /api/v1/system/status
func (h *AggregationHandler) HandleGetSystemStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		MethodNotAllowedResponse(w, r)
		return
	}

	h.logger.Debug("Getting system status", nil)

	ctx := r.Context()
	status := &SystemStatus{
		MarketDataStatus:       "unknown",
		OrderManagementStatus:  "unknown",
		StrategyExecutorStatus: "unknown",
		OverallStatus:          "healthy",
		Errors:                 make([]string, 0),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	// Check market data service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := h.marketDataClient.Health(ctx); err == nil {
			mu.Lock()
			status.MarketDataStatus = "healthy"
			mu.Unlock()
		} else {
			mu.Lock()
			status.MarketDataStatus = "unhealthy"
			status.OverallStatus = "degraded"
			status.Errors = append(status.Errors, "Market data service is unhealthy")
			mu.Unlock()
		}
	}()

	// Check order management service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := h.orderClient.Health(ctx); err == nil {
			mu.Lock()
			status.OrderManagementStatus = "healthy"
			mu.Unlock()
		} else {
			mu.Lock()
			status.OrderManagementStatus = "unhealthy"
			status.OverallStatus = "degraded"
			status.Errors = append(status.Errors, "Order management service is unhealthy")
			mu.Unlock()
		}
	}()

	// Check strategy executor service
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := h.strategyClient.Health(ctx); err == nil {
			mu.Lock()
			status.StrategyExecutorStatus = "healthy"
			mu.Unlock()
		} else {
			mu.Lock()
			status.StrategyExecutorStatus = "unhealthy"
			status.OverallStatus = "degraded"
			status.Errors = append(status.Errors, "Strategy executor service is unhealthy")
			mu.Unlock()
		}
	}()

	wg.Wait()

	// Return appropriate status code based on overall status
	if status.OverallStatus == "degraded" {
		SuccessResponseWithStatus(w, r, http.StatusOK, status)
	} else {
		SuccessResponse(w, r, status)
	}
}

