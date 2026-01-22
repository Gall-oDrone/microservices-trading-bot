package router

import (
	"bitso-trading-platform/api-gateway/internal/api"
	"bitso-trading-platform/api-gateway/internal/config"
	"bitso-trading-platform/api-gateway/internal/logger"
	"bitso-trading-platform/api-gateway/internal/metrics"
	"bitso-trading-platform/api-gateway/internal/middleware"
)

// SetupRoutes sets up all routes and middleware
func SetupRoutes(
	cfg *config.Config,
	handler *api.Handler,
	logger *logger.Logger,
	metrics *metrics.MetricsCollector,
) *Router {
	router := NewRouter(logger)

	// Register routes first (using the ServeMux)
	handler.RegisterRoutes(router.GetMux())

	// Add middleware in order (outer to inner)
	// 1. Recovery - catch panics first
	router.Use(middleware.NewRecoveryMiddleware(logger).Handler)

	// 2. Logging - log all requests
	router.Use(middleware.NewLoggingMiddleware(logger).Handler)

	// 3. Metrics - collect metrics
	router.Use(middleware.NewMetricsMiddleware(metrics).Handler)

	// 4. CORS - handle cross-origin requests
	corsConfig := middleware.DefaultCORSConfig()
	router.Use(middleware.NewCORSMiddleware(corsConfig).Handler)

	// 5. Timeout - enforce request timeout
	router.Use(middleware.NewTimeoutMiddleware(cfg.Client.Timeout, logger).Handler)

	// 6. Rate Limiting - protect from abuse (if enabled)
	if cfg.RateLimit.Enabled {
		rateLimitConfig := &middleware.RateLimiterConfig{
			RequestsPerMinute:  cfg.RateLimit.RequestsPerMinute,
			Burst:              cfg.RateLimit.Burst,
			CleanupInterval:    cfg.RateLimit.CleanupInterval,
			InactivityDuration: cfg.RateLimit.InactivityDuration,
		}
		router.Use(middleware.NewRateLimiterMiddleware(rateLimitConfig, logger, metrics).Handler)
	}

	// 7. Circuit Breaker - protect backend services (if enabled)
	if cfg.CircuitBreaker.Enabled {
		cbConfig := &middleware.CircuitBreakerConfig{
			Threshold:   cfg.CircuitBreaker.Threshold,
			Timeout:     cfg.CircuitBreaker.Timeout,
			MaxRequests: cfg.CircuitBreaker.MaxRequests,
			Interval:    cfg.CircuitBreaker.Interval,
		}
		router.Use(middleware.NewCircuitBreakerMiddleware(cbConfig, logger, metrics).Handler)
	}

	// 8. Authentication - authenticate requests (if enabled)
	if cfg.Auth.Enabled {
		authConfig := &middleware.AuthConfig{
			Enabled:      cfg.Auth.Enabled,
			JWTSecret:    cfg.Auth.JWTSecret,
			APIKeyHeader: cfg.Auth.APIKeyHeader,
		}
		router.Use(middleware.NewAuthMiddleware(authConfig, logger).Handler)
	}

	logger.Info("Routes and middleware configured", map[string]interface{}{
		"middleware_count": len(router.middleware),
		"rate_limit":       cfg.RateLimit.Enabled,
		"circuit_breaker":  cfg.CircuitBreaker.Enabled,
		"auth":             cfg.Auth.Enabled,
	})

	return router
}

