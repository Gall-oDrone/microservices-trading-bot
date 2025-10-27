package client

import (
	"fmt"
	"net/http"

	"bitso-trading-platform/api-gateway/internal/config"
	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
)

// ClientFactory creates and manages HTTP clients for backend services
type ClientFactory struct {
	config     *config.Config
	logger     *logger.Logger
	metrics    *metrics.MetricsCollector
	httpClient *http.Client

	// Cached clients
	marketDataClient       MarketDataClient
	orderManagementClient  OrderManagementClient
	strategyExecutorClient StrategyExecutorClient
}

// NewClientFactory creates a new client factory
func NewClientFactory(cfg *config.Config, logger *logger.Logger, metrics *metrics.MetricsCollector) (*ClientFactory, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger cannot be nil")
	}
	if metrics == nil {
		return nil, fmt.Errorf("metrics cannot be nil")
	}

	// Create shared HTTP client with connection pooling
	httpClient := createHTTPClient(&cfg.Client)

	factory := &ClientFactory{
		config:     cfg,
		logger:     logger.WithComponent("client-factory"),
		metrics:    metrics,
		httpClient: httpClient,
	}

	// Pre-initialize clients
	if err := factory.initializeClients(); err != nil {
		return nil, fmt.Errorf("failed to initialize clients: %w", err)
	}

	return factory, nil
}

// initializeClients initializes all backend service clients
func (f *ClientFactory) initializeClients() error {
	var err error

	// Initialize market data client
	f.marketDataClient, err = f.createMarketDataClient()
	if err != nil {
		return fmt.Errorf("failed to create market data client: %w", err)
	}
	f.logger.Info("Market data client initialized", map[string]interface{}{
		"base_url": f.config.Backend.MarketDataURL,
	})

	// Initialize order management client
	f.orderManagementClient, err = f.createOrderManagementClient()
	if err != nil {
		return fmt.Errorf("failed to create order management client: %w", err)
	}
	f.logger.Info("Order management client initialized", map[string]interface{}{
		"base_url": f.config.Backend.OrderManagementURL,
	})

	// Initialize strategy executor client
	f.strategyExecutorClient, err = f.createStrategyExecutorClient()
	if err != nil {
		return fmt.Errorf("failed to create strategy executor client: %w", err)
	}
	f.logger.Info("Strategy executor client initialized", map[string]interface{}{
		"base_url": f.config.Backend.StrategyExecutorURL,
	})

	return nil
}

// MarketDataClient returns the market data client
func (f *ClientFactory) MarketDataClient() MarketDataClient {
	return f.marketDataClient
}

// OrderManagementClient returns the order management client
func (f *ClientFactory) OrderManagementClient() OrderManagementClient {
	return f.orderManagementClient
}

// StrategyExecutorClient returns the strategy executor client
func (f *ClientFactory) StrategyExecutorClient() StrategyExecutorClient {
	return f.strategyExecutorClient
}

// createMarketDataClient creates a market data client
func (f *ClientFactory) createMarketDataClient() (MarketDataClient, error) {
	clientConfig := &ClientConfig{
		BaseURL:            f.config.Backend.MarketDataURL,
		Timeout:            f.config.Client.Timeout,
		MaxRetries:         f.config.Client.MaxRetries,
		RetryDelay:         f.config.Client.RetryDelay,
		MaxIdleConns:       f.config.Client.MaxIdleConns,
		IdleConnTimeout:    f.config.Client.IdleConnTimeout,
		MaxConnsPerHost:    f.config.Client.MaxConnsPerHost,
		DisableKeepAlives:  f.config.Client.DisableKeepAlives,
		DisableCompression: f.config.Client.DisableCompression,
	}

	return NewMarketDataClient(clientConfig, f.logger, f.metrics)
}

// createOrderManagementClient creates an order management client
func (f *ClientFactory) createOrderManagementClient() (OrderManagementClient, error) {
	clientConfig := &ClientConfig{
		BaseURL:            f.config.Backend.OrderManagementURL,
		Timeout:            f.config.Client.Timeout,
		MaxRetries:         f.config.Client.MaxRetries,
		RetryDelay:         f.config.Client.RetryDelay,
		MaxIdleConns:       f.config.Client.MaxIdleConns,
		IdleConnTimeout:    f.config.Client.IdleConnTimeout,
		MaxConnsPerHost:    f.config.Client.MaxConnsPerHost,
		DisableKeepAlives:  f.config.Client.DisableKeepAlives,
		DisableCompression: f.config.Client.DisableCompression,
	}

	return NewOrderManagementClient(clientConfig, f.logger, f.metrics)
}

// createStrategyExecutorClient creates a strategy executor client
func (f *ClientFactory) createStrategyExecutorClient() (StrategyExecutorClient, error) {
	clientConfig := &ClientConfig{
		BaseURL:            f.config.Backend.StrategyExecutorURL,
		Timeout:            f.config.Client.Timeout,
		MaxRetries:         f.config.Client.MaxRetries,
		RetryDelay:         f.config.Client.RetryDelay,
		MaxIdleConns:       f.config.Client.MaxIdleConns,
		IdleConnTimeout:    f.config.Client.IdleConnTimeout,
		MaxConnsPerHost:    f.config.Client.MaxConnsPerHost,
		DisableKeepAlives:  f.config.Client.DisableKeepAlives,
		DisableCompression: f.config.Client.DisableCompression,
	}

	return NewStrategyExecutorClient(clientConfig, f.logger, f.metrics)
}

// createHTTPClient creates a configured HTTP client with connection pooling
func createHTTPClient(cfg *config.ClientConfig) *http.Client {
	return &http.Client{
		Timeout: cfg.Timeout,
		Transport: &http.Transport{
			MaxIdleConns:        cfg.MaxIdleConns,
			MaxIdleConnsPerHost: cfg.MaxConnsPerHost,
			MaxConnsPerHost:     cfg.MaxConnsPerHost,
			IdleConnTimeout:     cfg.IdleConnTimeout,
			DisableKeepAlives:   cfg.DisableKeepAlives,
			DisableCompression:  cfg.DisableCompression,
		},
	}
}

// Close closes all client connections
func (f *ClientFactory) Close() error {
	f.logger.Info("Closing client factory", nil)

	// Close HTTP client transport
	if f.httpClient != nil && f.httpClient.Transport != nil {
		if transport, ok := f.httpClient.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}

	return nil
}
