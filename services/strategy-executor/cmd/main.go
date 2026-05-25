package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/kafka"
	"bitso-trading-platform/shared/pkg/models"
	"bitso-trading-platform/strategy-executor/internal/config"
	"bitso-trading-platform/strategy-executor/internal/fees"
	"bitso-trading-platform/strategy-executor/internal/health"
	"bitso-trading-platform/strategy-executor/internal/indicators"
	"bitso-trading-platform/strategy-executor/internal/logger"
	"bitso-trading-platform/strategy-executor/internal/metrics"
	"bitso-trading-platform/strategy-executor/internal/news"
	"bitso-trading-platform/strategy-executor/internal/ordermgmt"
	"bitso-trading-platform/strategy-executor/internal/persistence"
	"bitso-trading-platform/strategy-executor/internal/server"
	"bitso-trading-platform/strategy-executor/internal/strategies"

	"github.com/google/uuid"
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
	var redisUsable bool

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
			redisUsable = true
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

	if cfg.OrderManagement.BaseURL != "" {
		strategyRegistry.SetPendingBuyCancelClient(ordermgmt.NewClient(cfg.OrderManagement.BaseURL))
		appLogger.Infof("Order-management cancel client enabled (base URL: %s)", cfg.OrderManagement.BaseURL)
	}

	if redisUsable && cfg.Redis.LimitProfitStateEnabled {
		strategyRegistry.SetLimitProfitRawStateStore(persistence.NewRedisLimitProfitStore(redisClient))
		appLogger.Info("Redis limit_profit durable state enabled")
	}

	if cfg.Bitso.FeesEnabled {
		bc := bitso.NewClient()
		bc.SetAuth(cfg.Bitso.APIKey, cfg.Bitso.APISecret)
		if cfg.Bitso.APIBaseURL != "" {
			bc.SetAPIBaseURL(cfg.Bitso.APIBaseURL)
		}
		strategyRegistry.SetFeeRatesProvider(fees.NewCachedBitsoFees(bc, cfg.Bitso.FeesCacheTTL))
		appLogger.Info("Bitso fee provider registered (cached GET /fees for limit_profit thresholds)")
	}

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

	// Pre-create required Kafka topics. Avoids a kafka-go race where the consumer joins
	// a group before the topic exists and ends up with 0 partition assignments.
	if len(cfg.Kafka.Brokers) > 0 && cfg.Kafka.Brokers[0] != "localhost:9092" {
		topics := []kafka.TopicSpec{
			{Name: cfg.Kafka.ProducerTopics.Signals, NumPartitions: 1, ReplicationFactor: 1},
			{Name: cfg.Kafka.TopicOrderFills, NumPartitions: 1, ReplicationFactor: 1},
		}
		ensureCtx, ensureCancel := context.WithTimeout(ctx, 15*time.Second)
		if err := kafka.EnsureTopics(ensureCtx, cfg.Kafka.Brokers, topics); err != nil {
			appLogger.Warnf("EnsureTopics best-effort failed (continuing): %v", err)
		} else {
			appLogger.Infof("Kafka topics ensured: %s, %s", cfg.Kafka.ProducerTopics.Signals, cfg.Kafka.TopicOrderFills)
		}
		ensureCancel()
	}

	// Initialize Kafka producer for signals (before HTTP server so /process can publish)
	var signalProducer *kafka.Producer
	signalPublishingEnabled := len(cfg.Kafka.Brokers) > 0 && cfg.Kafka.Brokers[0] != "localhost:9092"

	if signalPublishingEnabled {
		producerCfg := &kafka.ProducerConfig{
			Brokers:          cfg.Kafka.Brokers,
			Topic:            cfg.Kafka.ProducerTopics.Signals,
			BatchSize:        cfg.Kafka.BatchSize,
			BatchTimeout:     cfg.Kafka.BatchTimeout,
			CompressionCodec: cfg.Kafka.CompressionCodec,
			RequiredAcks:     cfg.Kafka.RequiredAcks,
		}
		var err error
		signalProducer, err = kafka.NewProducer(producerCfg)
		if err != nil {
			appLogger.Warnf("Failed to create Kafka producer for signals: %v", err)
			signalPublishingEnabled = false
		} else {
			appLogger.Infof("Signal publishing enabled to topic: %s", cfg.Kafka.ProducerTopics.Signals)

			healthMgr.RegisterCheck(health.NewSimpleCheck("kafka_producer", func(ctx context.Context) error {
				return nil
			}))
		}
	} else {
		appLogger.Info("Signal publishing disabled (no Kafka brokers configured)")
	}

	var newsStore *news.Store
	var newsFilter *news.Filter
	var newsConsumer *kafka.Consumer
	if cfg.News.Enabled && len(cfg.Kafka.Brokers) > 0 && cfg.Kafka.Brokers[0] != "localhost:9092" {
		newsStore = news.NewStore(cfg.News.SentimentTTL)
		newsFilter = news.NewFilter(newsStore, news.FilterConfig{
			MinSentimentForBuy: cfg.News.MinSentimentForBuy,
			BlockBearishBuy:    cfg.News.BlockBearishBuy,
			HighImpactCooldown: cfg.News.HighImpactCooldown,
		})
		c, err := kafka.NewConsumer(&kafka.ConsumerConfig{
			Brokers:         cfg.Kafka.Brokers,
			Topic:           cfg.News.Topic,
			GroupID:         cfg.Kafka.ConsumerGroup + "-news",
			AutoOffsetReset: cfg.News.AutoOffsetReset,
			CommitInterval:  cfg.Kafka.CommitInterval,
			MaxWait:         cfg.Kafka.MaxWait,
		})
		if err != nil {
			appLogger.Warnf("News Kafka consumer not started: %v", err)
		} else {
			newsConsumer = c
			go news.RunConsumer(ctx, newsConsumer, newsStore, func(format string, args ...interface{}) {
				appLogger.Warnf(format, args...)
			})
			appLogger.Infof("News sentiment consumer enabled (topic=%s)", cfg.News.Topic)
		}
	} else if cfg.News.Enabled {
		appLogger.Warn("NEWS_ENABLED=true but Kafka brokers unavailable; sentiment filter disabled")
	}

	var orderFillsConsumer *kafka.Consumer
	if cfg.Kafka.OrderFillsConsumerEnabled && cfg.Kafka.TopicOrderFills != "" {
		c, err := kafka.NewConsumer(&kafka.ConsumerConfig{
			Brokers:         cfg.Kafka.Brokers,
			Topic:           cfg.Kafka.TopicOrderFills,
			GroupID:         cfg.Kafka.ConsumerGroup + "-order-fills",
			AutoOffsetReset: cfg.Kafka.OrderFillsAutoOffsetReset,
			CommitInterval:  cfg.Kafka.CommitInterval,
			MaxWait:         cfg.Kafka.MaxWait,
		})
		if err != nil {
			appLogger.Warnf("Order fills Kafka consumer not started: %v", err)
		} else {
			orderFillsConsumer = c
			appLogger.Infof("Order fills consumer subscribed to topic: %s", cfg.Kafka.TopicOrderFills)
		}
	}

	publishTradeSignals := func(pubCtx context.Context, book string, signals []*strategies.Signal) error {
		if !signalPublishingEnabled || signalProducer == nil {
			return nil
		}
		for _, signal := range signals {
			if signal == nil {
				continue
			}
			if newsFilter != nil {
				if ok, reason := newsFilter.AllowsSignal(book, signal.Side); !ok {
					appLogger.Infof("Signal blocked by news filter for %s %s: %s", book, signal.Side, reason)
					continue
				}
			}
			eventID := uuid.New().String()
			if signal.Metadata != nil {
				if v, ok := signal.Metadata["event_id"].(string); ok && v != "" {
					eventID = v
				}
			}
			meta := map[string]interface{}{
				"reason":     signal.Reason,
				"confidence": signal.Confidence,
			}
			if signal.Metadata != nil {
				for k, v := range signal.Metadata {
					meta[k] = v
				}
			}
			event := &models.TradeSignalEvent{
				EventID:   eventID,
				Timestamp: signal.Timestamp.UnixMilli(),
				Book:      signal.Book,
				Strategy:  signal.Strategy,
				Signal:    signal.Side,
				Price:     signal.Price,
				Amount:    signal.Amount,
				Metadata:  meta,
			}
			data, err := json.Marshal(event)
			if err != nil {
				return fmt.Errorf("marshal signal: %w", err)
			}
			// Partition by event_id so OM + trading-engine see a stable per-signal ordering when the topic has multiple partitions.
			partitionKey := eventID
			if partitionKey == "" {
				partitionKey = book
			}
			cctx, cancel := context.WithTimeout(pubCtx, 5*time.Second)
			err = signalProducer.Produce(cctx, []byte(partitionKey), data)
			cancel()
			if err != nil {
				return fmt.Errorf("kafka produce: %w", err)
			}
			appLogger.Infof("Published %s signal for %s: price=%.2f amount=%.8f reason=%s",
				signal.Side, book, signal.Price, signal.Amount, signal.Reason)
		}
		return nil
	}

	serverOpts := &server.ServerOptions{
		IndicatorService: indicatorSvc,
		StrategyRegistry: strategyRegistry,
	}
	if signalPublishingEnabled && signalProducer != nil {
		serverOpts.PublishTradeSignals = publishTradeSignals
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

	// Signal processing loop - processes market data through strategies
	if orderFillsConsumer != nil {
		go func() {
			appLogger.Info("Order fills consumer loop started")
			for {
				msg, err := orderFillsConsumer.Consume(ctx)
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					appLogger.Warnf("order fills consume: %v", err)
					time.Sleep(time.Second)
					continue
				}
				var ev models.OrderFillEvent
				if err := json.Unmarshal(msg, &ev); err != nil {
					appLogger.Warnf("order fills unmarshal: %v", err)
					continue
				}
				fill := strategies.OrderFill{
					EventID:      ev.EventID,
					Book:         ev.Book,
					Side:         ev.Side,
					AveragePrice: ev.AveragePrice,
					FilledAmount: ev.FilledAmount,
					Liquidity:    ev.Liquidity,
					FeeRate:      ev.FeeRate,
				}
				// Back-compat: populate BuyFeeRate pointer when the fill is a BUY and we have a rate.
				// Documented in docs/strategy-fee-accuracy/.
				if ev.FeeRate > 0 && ev.Side == "buy" {
					rate := ev.FeeRate
					fill.BuyFeeRate = &rate
				}
				strategyRegistry.NotifyOrderFilled(fill)
			}
		}()
	}

	go func() {
		ticker := time.NewTicker(cfg.Indicators.UpdateInterval)
		defer ticker.Stop()
		
		appLogger.Info("Starting signal processing loop...")
		
		for {
			select {
			case <-ticker.C:
				// Get latest trades from market-data for each book
				for _, book := range cfg.Indicators.Books {
					trades, err := dataProvider.GetRecentTrades(ctx, book, 1)
					if err != nil || len(trades) == 0 {
						continue
					}
					
					// Get the latest trade
					latestTrade := &trades[0]
					
					// Process through all running strategies
					signals, err := strategyRegistry.ProcessTick(latestTrade, book)
					if err != nil {
						appLogger.Warnf("Error processing tick for %s: %v", book, err)
						continue
					}
					
					if err := publishTradeSignals(ctx, book, signals); err != nil {
						appLogger.Errorf("Failed to publish signals: %v", err)
					} else if len(signals) > 0 && !signalPublishingEnabled {
						for _, signal := range signals {
							if signal == nil {
								continue
							}
							appLogger.Infof("[LOCAL] Generated %s signal for %s: price=%.2f amount=%.8f reason=%s",
								signal.Side, book, signal.Price, signal.Amount, signal.Reason)
						}
					}
				}
			case <-ctx.Done():
				appLogger.Info("Signal processing loop stopping...")
				return
			}
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

	if signalProducer != nil {
		if err := signalProducer.Close(); err != nil {
			appLogger.Errorf("Error closing Kafka producer: %v", err)
		}
	}

	if orderFillsConsumer != nil {
		if err := orderFillsConsumer.Close(); err != nil {
			appLogger.Errorf("Error closing order fills Kafka consumer: %v", err)
		}
	}
	if newsConsumer != nil {
		if err := newsConsumer.Close(); err != nil {
			appLogger.Errorf("Error closing news Kafka consumer: %v", err)
		}
	}

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
