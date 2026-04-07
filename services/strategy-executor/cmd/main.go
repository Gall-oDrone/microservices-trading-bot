package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bitso-trading-platform/strategy-executor/internal/config"
	"bitso-trading-platform/strategy-executor/internal/health"
	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"
	"bitso-trading-platform/strategy-executor/internal/server"
	"bitso-trading-platform/strategy-executor/internal/strategies"

	"github.com/redis/go-redis/v9"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	appLogger := logger.New(&logger.Config{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		Output: cfg.Logging.Output,
	})
	logger.SetGlobalLogger(appLogger)

	appLogger.Info("Strategy Executor Service starting...")
	appLogger.Infof("Service: %s v%s", cfg.Service.Name, cfg.Service.Version)
	appLogger.Infof("Environment: %s", cfg.Service.Environment)

	appMetrics := metrics.New(cfg.Service.Name)
	appMetrics.RecordServiceStart()

	healthMgr := health.NewManager(cfg.Service.Name, cfg.Service.Version, 30*time.Second)

	healthMgr.RegisterCheck(health.NewSimpleCheck("service", func(ctx context.Context) error {
		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var indicatorStore indicators.IndicatorStore
	var redisClient *redis.Client

	if cfg.Redis.Enabled {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})

		if err := redisClient.Ping(ctx).Err(); err != nil {
			appLogger.Warnf("Redis connection failed, using in-memory store: %v", err)
			indicatorStore = indicators.NewInMemoryIndicatorStore()
		} else {
			appLogger.Info("Connected to Redis")
			indicatorStore = indicators.NewRedisIndicatorStoreWithTTL(redisClient, cfg.Redis.TTL)

			healthMgr.RegisterCheck(health.NewSimpleCheck("redis", func(ctx context.Context) error {
				return redisClient.Ping(ctx).Err()
			}))
		}
	} else {
		appLogger.Info("Redis disabled, using in-memory indicator store")
		indicatorStore = indicators.NewInMemoryIndicatorStore()
	}

	dataProvider := indicators.NewHTTPDataProvider(cfg.MarketData.BaseURL)

	indicatorConfig := &indicators.ServiceConfig{
		SMAPeriod:       cfg.Indicators.SMAPeriod,
		EMAPeriod:       cfg.Indicators.EMAPeriod,
		RSIPeriod:       cfg.Indicators.RSIPeriod,
		BollingerPeriod: cfg.Indicators.BollingerPeriod,
		BollingerStdDev: cfg.Indicators.BollingerStdDev,
		ATRPeriod:       cfg.Indicators.ATRPeriod,
		UpdateInterval:  cfg.Indicators.UpdateInterval,
	}

	indicatorSvc := indicators.NewService(indicatorConfig, indicatorStore, dataProvider, nil)

	if cfg.Indicators.Enabled {
		appLogger.Infof("Starting indicator service for books: %v", cfg.Indicators.Books)
		go func() {
			if err := indicatorSvc.Start(ctx, cfg.Indicators.Books); err != nil {
				appLogger.Errorf("Indicator service error: %v", err)
			}
		}()

		healthMgr.RegisterCheck(health.NewSimpleCheck("indicators", func(ctx context.Context) error {
			return nil
		}))
	}

	strategyRegistry := strategies.NewEnhancedRegistry(indicatorSvc)

	if cfg.Strategy.DefaultStrategy != "" && cfg.Strategy.DefaultStrategy != "none" {
		defaultConfig := strategies.StrategyConfig{
			Name:    fmt.Sprintf("%s_%s", cfg.Strategy.DefaultStrategy, cfg.Strategy.DefaultBook),
			Type:    cfg.Strategy.DefaultStrategy,
			Version: "1.0.0",
			Enabled: true,
			Book:    cfg.Strategy.DefaultBook,
			Parameters: map[string]interface{}{
				"lookback_period":     float64(20),
				"entry_threshold":     float64(2.0),
				"exit_threshold":      float64(0.5),
				"min_signal_interval": float64(60),
				"position_size":       cfg.Risk.MinTradeAmount,
			},
			Sizing: strategies.SizingConfig{
				Method:           "fixed",
				MaxPositionSize:  cfg.Risk.MaxTradeAmount,
				MaxPositionValue: cfg.Risk.MaxTradeValue,
			},
			Risk: strategies.RiskConfig{
				MaxDailyLoss:         500,
				MaxDrawdownPct:       10,
				MaxTradesPerDay:      50,
				MaxConsecutiveLoss:   5,
				MinTimeBetweenTrades: 60,
				CooldownAfterLoss:    300,
			},
		}

		if _, err := strategyRegistry.CreateAndRegister(defaultConfig); err != nil {
			appLogger.Warnf("Failed to create default strategy: %v", err)
		} else {
			appLogger.Infof("Registered default strategy: %s", defaultConfig.Name)
		}
	}

	serverOpts := &server.ServerOptions{
		IndicatorService: indicatorSvc,
		StrategyRegistry: strategyRegistry,
	}

	httpServer := server.NewWithOptions(&server.Config{
		Host: cfg.Service.Host,
		Port: cfg.Service.Port,
	}, healthMgr, appMetrics, serverOpts)

	go func() {
		appLogger.Infof("Starting HTTP server on %s:%d", cfg.Service.Host, cfg.Service.Port)
		if err := httpServer.Start(ctx); err != nil {
			appLogger.Errorf("HTTP server error: %v", err)
		}
	}()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		startTime := time.Now()
		for {
			select {
			case <-ticker.C:
				uptime := time.Since(startTime)
				appMetrics.RecordServiceUptime(uptime)
				appMetrics.RecordServiceHealth(true)
			case <-ctx.Done():
				return
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	appLogger.Info("Service started successfully. Waiting for interrupt signal...")
	<-sigChan

	appLogger.Info("Shutdown signal received. Gracefully shutting down...")

	cancel()

	if err := strategyRegistry.StopAll(); err != nil {
		appLogger.Errorf("Error stopping strategies: %v", err)
	}

	indicatorSvc.Stop()

	if redisClient != nil {
		if err := redisClient.Close(); err != nil {
			appLogger.Errorf("Error closing Redis connection: %v", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := httpServer.Stop(shutdownCtx); err != nil {
		appLogger.Errorf("Error shutting down HTTP server: %v", err)
	}

	appLogger.Info("Service shutdown complete")
}
