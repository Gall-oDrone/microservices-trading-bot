package indicators

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"bitso-trading-platform/strategy-executor/internal/metrics"
)

// ServiceConfig holds configuration for the indicator service
type ServiceConfig struct {
	SMAPeriod       int
	EMAPeriod       int
	RSIPeriod       int
	BollingerPeriod int
	BollingerStdDev float64
	ATRPeriod       int
	VWAPPeriod      int
	UpdateInterval  time.Duration
}

// DefaultServiceConfig returns default configuration
func DefaultServiceConfig() *ServiceConfig {
	return &ServiceConfig{
		SMAPeriod:       20,
		EMAPeriod:       20,
		RSIPeriod:       14,
		BollingerPeriod: 20,
		BollingerStdDev: 2.0,
		ATRPeriod:       14,
		VWAPPeriod:      0,
		UpdateInterval:  30 * time.Second,
	}
}

// Service manages indicator computation and storage
type Service struct {
	config       *ServiceConfig
	store        IndicatorStore
	dataProvider DataProvider
	logger       *log.Logger

	sma       *SMA
	ema       *EMA
	rsi       *RSI
	bollinger *Bollinger
	atr       *ATR
	vwap      *VWAP

	mu      sync.RWMutex
	running bool
	cancel  context.CancelFunc
}

// NewService creates a new indicator service
func NewService(config *ServiceConfig, store IndicatorStore, dataProvider DataProvider, logger *log.Logger) *Service {
	if config == nil {
		config = DefaultServiceConfig()
	}
	if logger == nil {
		logger = log.New(log.Writer(), "[INDICATORS] ", log.LstdFlags)
	}

	return &Service{
		config:       config,
		store:        store,
		dataProvider: dataProvider,
		logger:       logger,
		sma:          NewSMA(config.SMAPeriod),
		ema:          NewEMA(config.EMAPeriod),
		rsi:          NewRSI(config.RSIPeriod),
		bollinger:    NewBollinger(config.BollingerPeriod, config.BollingerStdDev),
		atr:          NewATR(config.ATRPeriod),
		vwap:         NewVWAP(config.VWAPPeriod),
	}
}

// Start begins periodic indicator computation
func (s *Service) Start(ctx context.Context, books []string) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("indicator service already running")
	}
	s.running = true
	ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()

	s.logger.Printf("Starting indicator service for books: %v", books)

	s.computeAllOnce(ctx, books)

	ticker := time.NewTicker(s.config.UpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Println("Indicator service stopped")
			return nil
		case <-ticker.C:
			s.computeAllOnce(ctx, books)
		}
	}
}

// Stop stops the indicator service
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}
	s.running = false
}

// computeAllOnce computes all indicators for all books
func (s *Service) computeAllOnce(ctx context.Context, books []string) {
	for _, book := range books {
		if err := s.ComputeAndStore(ctx, book); err != nil {
			s.logger.Printf("Error computing indicators for %s: %v", book, err)
		}
	}
}

// ComputeAndStore computes all indicators for a book and stores them
func (s *Service) ComputeAndStore(ctx context.Context, book string) error {
	promMetrics := metrics.GetPrometheusMetrics()

	trades, err := s.dataProvider.GetRecentTrades(ctx, book, 100)
	if err != nil {
		promMetrics.SetIndicatorsHealthy(false)
		return fmt.Errorf("get trades: %w", err)
	}

	if len(trades) < s.config.SMAPeriod {
		s.logger.Printf("Insufficient trades for %s: got %d, need %d", book, len(trades), s.config.SMAPeriod)
		return nil
	}

	prices := make([]float64, len(trades))
	for i, t := range trades {
		prices[i] = t.Price
	}

	now := time.Now()

	if smaVal, err := s.sma.Compute(prices); err == nil {
		s.store.Set(ctx, book, "sma", s.config.SMAPeriod, &IndicatorValue{
			Name:      "sma",
			Period:    s.config.SMAPeriod,
			Value:     smaVal,
			Timestamp: now,
			Book:      book,
		})
		promMetrics.SetIndicatorSMA(book, smaVal)
	}

	if emaVal, err := s.ema.Compute(prices); err == nil {
		s.store.Set(ctx, book, "ema", s.config.EMAPeriod, &IndicatorValue{
			Name:      "ema",
			Period:    s.config.EMAPeriod,
			Value:     emaVal,
			Timestamp: now,
			Book:      book,
		})
		promMetrics.SetIndicatorEMA(book, emaVal)
	}

	if rsiVal, err := s.rsi.Compute(prices); err == nil {
		s.store.Set(ctx, book, "rsi", s.config.RSIPeriod, &IndicatorValue{
			Name:      "rsi",
			Period:    s.config.RSIPeriod,
			Value:     rsiVal,
			Timestamp: now,
			Book:      book,
		})
		promMetrics.SetIndicatorRSI(book, rsiVal)
	}

	if bb, err := s.bollinger.ComputeBands(prices); err == nil {
		s.store.SetBollinger(ctx, book, s.config.BollingerPeriod, bb)
		s.store.Set(ctx, book, "bollinger_middle", s.config.BollingerPeriod, &IndicatorValue{
			Name:      "bollinger_middle",
			Period:    s.config.BollingerPeriod,
			Value:     bb.Middle,
			Timestamp: now,
			Book:      book,
			Extra: map[string]float64{
				"upper":  bb.Upper,
				"lower":  bb.Lower,
				"stddev": bb.StdDev,
			},
		})
		promMetrics.SetIndicatorBollinger(book, bb.Upper, bb.Middle, bb.Lower)
	}

	if vwapVal, err := s.vwap.ComputeFromTrades(trades); err == nil {
		s.store.Set(ctx, book, "vwap", s.config.VWAPPeriod, &IndicatorValue{
			Name:      "vwap",
			Period:    s.config.VWAPPeriod,
			Value:     vwapVal,
			Timestamp: now,
			Book:      book,
		})
		promMetrics.SetIndicatorVWAP(book, vwapVal)
	}

	bars, err := s.dataProvider.GetRecentBars(ctx, book, "1m", 30)
	if err == nil && len(bars) > s.config.ATRPeriod {
		if atrVal, err := s.atr.ComputeFromBars(bars); err == nil {
			s.store.Set(ctx, book, "atr", s.config.ATRPeriod, &IndicatorValue{
				Name:      "atr",
				Period:    s.config.ATRPeriod,
				Value:     atrVal,
				Timestamp: now,
				Book:      book,
			})
			promMetrics.SetIndicatorATR(book, atrVal)
		}
	}

	promMetrics.SetIndicatorsHealthy(true)

	return nil
}

// GetSMA returns the current SMA value for a book
func (s *Service) GetSMA(ctx context.Context, book string) (*IndicatorValue, error) {
	return s.store.Get(ctx, book, "sma", s.config.SMAPeriod)
}

// GetEMA returns the current EMA value for a book
func (s *Service) GetEMA(ctx context.Context, book string) (*IndicatorValue, error) {
	return s.store.Get(ctx, book, "ema", s.config.EMAPeriod)
}

// GetRSI returns the current RSI value for a book
func (s *Service) GetRSI(ctx context.Context, book string) (*IndicatorValue, error) {
	return s.store.Get(ctx, book, "rsi", s.config.RSIPeriod)
}

// GetBollinger returns the current Bollinger Bands for a book
func (s *Service) GetBollinger(ctx context.Context, book string) (*BollingerBands, error) {
	return s.store.GetBollinger(ctx, book, s.config.BollingerPeriod)
}

// GetATR returns the current ATR value for a book
func (s *Service) GetATR(ctx context.Context, book string) (*IndicatorValue, error) {
	return s.store.Get(ctx, book, "atr", s.config.ATRPeriod)
}

// GetVWAP returns the current VWAP value for a book
func (s *Service) GetVWAP(ctx context.Context, book string) (*IndicatorValue, error) {
	return s.store.Get(ctx, book, "vwap", s.config.VWAPPeriod)
}

// GetBookTicker returns best bid, ask, and last from the data provider when implemented (e.g. HTTP market-data).
func (s *Service) GetBookTicker(ctx context.Context, book string) (bid, ask, last float64, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.dataProvider == nil {
		return 0, 0, 0, false
	}
	return s.dataProvider.GetBookTicker(ctx, book)
}

// GetAllIndicators returns all indicators for a book
func (s *Service) GetAllIndicators(ctx context.Context, book string) (map[string]*IndicatorValue, error) {
	return s.store.GetAll(ctx, book)
}

// Snapshot returns a snapshot of all indicators for a book
type Snapshot struct {
	Book      string                    `json:"book"`
	Timestamp time.Time                 `json:"timestamp"`
	SMA       *IndicatorValue           `json:"sma,omitempty"`
	EMA       *IndicatorValue           `json:"ema,omitempty"`
	RSI       *IndicatorValue           `json:"rsi,omitempty"`
	Bollinger *BollingerBands           `json:"bollinger,omitempty"`
	ATR       *IndicatorValue           `json:"atr,omitempty"`
	VWAP      *IndicatorValue           `json:"vwap,omitempty"`
}

// GetSnapshot returns a snapshot of all indicators for a book
func (s *Service) GetSnapshot(ctx context.Context, book string) (*Snapshot, error) {
	snapshot := &Snapshot{
		Book:      book,
		Timestamp: time.Now(),
	}

	snapshot.SMA, _ = s.GetSMA(ctx, book)
	snapshot.EMA, _ = s.GetEMA(ctx, book)
	snapshot.RSI, _ = s.GetRSI(ctx, book)
	snapshot.Bollinger, _ = s.GetBollinger(ctx, book)
	snapshot.ATR, _ = s.GetATR(ctx, book)
	snapshot.VWAP, _ = s.GetVWAP(ctx, book)

	return snapshot, nil
}
