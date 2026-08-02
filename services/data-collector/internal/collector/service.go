package collector

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"bitso-trading-platform/data-collector/internal/clock"
	"bitso-trading-platform/data-collector/internal/config"
	"bitso-trading-platform/data-collector/internal/gap"
	"bitso-trading-platform/data-collector/internal/health"
	"bitso-trading-platform/data-collector/internal/metrics"
	"bitso-trading-platform/data-collector/internal/models"
	"bitso-trading-platform/data-collector/internal/sink"
	"bitso-trading-platform/data-collector/internal/websocket"
	"bitso-trading-platform/shared/pkg/bitso"
)

// Service orchestrates WebSocket ingestion and dual-sink persistence.
type Service struct {
	cfg      *config.Config
	logger   *log.Logger
	clock    clock.Clock
	metrics  *metrics.Collector
	health   *health.Checker
	gaps     *gap.Detector
	batcher  *sink.ParquetBatcher
	hotStore sink.TradeWriter
	ws       *websocket.Manager
}

// New builds a Service from config and dependencies.
func New(
	cfg *config.Config,
	logger *log.Logger,
	clk clock.Clock,
	met *metrics.Collector,
	hc *health.Checker,
	batcher *sink.ParquetBatcher,
	hotStore sink.TradeWriter,
) *Service {
	if logger == nil {
		logger = log.Default()
	}
	if clk == nil {
		clk = clock.RealClock{}
	}
	if hotStore == nil {
		hotStore = sink.NopTradeWriter{}
	}

	s := &Service{
		cfg:      cfg,
		logger:   logger,
		clock:    clk,
		metrics:  met,
		health:   hc,
		batcher:  batcher,
		hotStore: hotStore,
	}

	s.gaps = gap.NewDetector(cfg.BitsoBook, clk, func(g models.GapRecord) {
		logger.Printf("WS_GAP book=%s start=%s end=%s duration=%s",
			g.Book, g.Start.Format(time.RFC3339), g.End.Format(time.RFC3339), g.Duration)
		if err := hotStore.WriteGap(context.Background(), g); err != nil {
			logger.Printf("failed to persist gap: %v", err)
			if met != nil {
				met.PostgresFailures.Inc()
			}
		}
	})

	s.ws = websocket.NewManager(&websocket.ManagerConfig{
		WSURL:             cfg.BitsoWSURL,
		ReconnectAttempts: cfg.WSReconnectAttempts,
		ReconnectInterval: cfg.WSReconnectInterval,
		ReconnectMaxDelay: cfg.WSReconnectMaxDelay,
		Logger:            logger,
		OnDisconnect:      s.gaps.OnDisconnect,
		OnReconnect: func() {
			if met != nil {
				met.WSReconnects.Inc()
			}
			s.gaps.OnReconnect()
		},
	})

	return s
}

// Start connects, subscribes to trades, and processes the stream until ctx is done.
func (s *Service) Start(ctx context.Context) error {
	book, err := parseBook(s.cfg.BitsoBook)
	if err != nil {
		return err
	}

	if s.batcher != nil {
		s.batcher.Start()
	}

	if err := s.ws.Connect(ctx); err != nil {
		return err
	}
	if err := s.ws.Subscribe([]*bitso.Book{book}, []string{"trades"}); err != nil {
		return err
	}
	if err := s.ws.Start(ctx); err != nil {
		return err
	}

	go s.stalenessLoop(ctx)

	s.logger.Printf("Collecting trades for book=%s", s.cfg.BitsoBook)
	for {
		select {
		case <-ctx.Done():
			return s.shutdown(context.Background())
		case msg, ok := <-s.ws.GetTradesStream():
			if !ok {
				return s.shutdown(context.Background())
			}
			s.handleTrade(ctx, msg)
		}
	}
}

func (s *Service) handleTrade(ctx context.Context, msg *bitso.WebSocketTrade) {
	now := s.clock.Now()
	trades := models.FromBitsoWebSocketTrade(msg, now)
	if len(trades) == 0 {
		return
	}

	for range trades {
		if s.metrics != nil {
			s.metrics.ObserveTrade(now)
		}
		if s.health != nil {
			s.health.RecordTrade(now)
		}
	}

	if s.batcher != nil {
		if err := s.batcher.Add(ctx, trades); err != nil {
			s.logger.Printf("parquet batch add/flush error: %v", err)
		}
	}

	if err := s.hotStore.WriteTrades(ctx, trades); err != nil {
		s.logger.Printf("postgres write error: %v", err)
		if s.metrics != nil {
			s.metrics.PostgresFailures.Inc()
		}
	}
}

func (s *Service) stalenessLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.metrics != nil {
				s.metrics.UpdateStaleness(s.clock.Now())
			}
		}
	}
}

func (s *Service) shutdown(ctx context.Context) error {
	_ = s.ws.Stop()
	if s.batcher != nil {
		if err := s.batcher.Stop(ctx); err != nil {
			s.logger.Printf("final parquet flush error: %v", err)
		}
	}
	_ = s.hotStore.Close()
	return nil
}

func parseBook(bookStr string) (*bitso.Book, error) {
	parts := strings.Split(strings.TrimSpace(bookStr), "_")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid book format: %s (expected major_minor)", bookStr)
	}
	major := bitso.ToCurrency(parts[0])
	minor := bitso.ToCurrency(parts[1])
	if major == bitso.CurrencyNone || minor == bitso.CurrencyNone {
		return nil, fmt.Errorf("invalid currency in book: %s", bookStr)
	}
	return bitso.NewBook(major, minor), nil
}
