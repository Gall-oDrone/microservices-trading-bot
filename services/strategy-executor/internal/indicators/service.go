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
	BarInterval     string
	BarLimitBuffer  int
	MaxStaleness    time.Duration
	BootstrapWait   time.Duration
	BootstrapEvery  time.Duration
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
		BarInterval:     "1m",
		BarLimitBuffer:  5,
		MaxStaleness:    15 * time.Minute,
		BootstrapWait:   2 * time.Minute,
		BootstrapEvery:  10 * time.Second,
	}
}

// bookHealth tracks the latest compute outcome for a book.
type bookHealth struct {
	Ready        bool
	ComputedAt   time.Time
	LastBarAt    time.Time
	BarsUsed     int
	CurrentPrice float64
	StaleReason  string
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

	mu          sync.RWMutex
	running     bool
	cancel      context.CancelFunc
	healthByBook map[string]bookHealth
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
		healthByBook: make(map[string]bookHealth),
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

	s.logger.Printf("Starting indicator service for books: %v (bar_interval=%s)", books, s.config.BarInterval)
	s.bootstrap(ctx, books)

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

// bootstrap retries bar-based computation until warm-up succeeds or timeout.
func (s *Service) bootstrap(ctx context.Context, books []string) {
	deadline := time.Now().Add(s.config.BootstrapWait)
	for time.Now().Before(deadline) {
		allReady := true
		for _, book := range books {
			if err := s.ComputeAndStore(ctx, book); err != nil {
				s.logger.Printf("Bootstrap compute error for %s: %v", book, err)
			}
			h := s.getBookHealth(book)
			if !h.Ready {
				allReady = false
				s.logger.Printf("Bootstrap waiting for %s: %s (bars=%d)", book, h.StaleReason, h.BarsUsed)
			}
		}
		if allReady {
			s.logger.Printf("Indicator bootstrap complete for %v", books)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.config.BootstrapEvery):
		}
	}
	s.logger.Printf("Indicator bootstrap finished with warm-up incomplete (will retry on update interval)")
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

func (s *Service) setBookHealth(book string, h bookHealth) {
	s.mu.Lock()
	s.healthByBook[book] = h
	s.mu.Unlock()
}

func (s *Service) getBookHealth(book string) bookHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.healthByBook[book]
}

// ComputeAndStore computes all indicators for a book and stores them.
// Price-based indicators use 1m bar closes; VWAP still uses recent trades when available.
func (s *Service) ComputeAndStore(ctx context.Context, book string) error {
	promMetrics := metrics.GetPrometheusMetrics()
	now := time.Now()

	barLimit := barFetchLimit(s.config)
	bars, err := s.dataProvider.GetRecentBars(ctx, book, s.config.BarInterval, barLimit)
	if err != nil {
		promMetrics.SetIndicatorsHealthy(false)
		s.setBookHealth(book, bookHealth{
			Ready:       false,
			ComputedAt:  now,
			StaleReason: fmt.Sprintf("bars fetch failed: %v", err),
		})
		return fmt.Errorf("get bars: %w", err)
	}

	minBars := minBarsRequired(s.config)
	if len(bars) < minBars {
		s.logger.Printf("Insufficient bars for %s: got %d, need %d (interval=%s)", book, len(bars), minBars, s.config.BarInterval)
		promMetrics.SetIndicatorsHealthy(false)
		s.setBookHealth(book, bookHealth{
			Ready:       false,
			ComputedAt:  now,
			BarsUsed:    len(bars),
			StaleReason: fmt.Sprintf("insufficient bars: got %d need %d", len(bars), minBars),
		})
		return nil
	}

	closes := barCloses(bars)
	currentPrice := closes[len(closes)-1]
	lastBarAt := bars[len(bars)-1].Timestamp
	barStale := s.isBarDataStale(lastBarAt)

	if smaVal, err := s.sma.Compute(closes); err == nil {
		s.store.Set(ctx, book, "sma", s.config.SMAPeriod, &IndicatorValue{
			Name: "sma", Period: s.config.SMAPeriod, Value: smaVal, Timestamp: now, Book: book,
		})
		promMetrics.SetIndicatorSMA(book, smaVal)
	}

	if emaVal, err := s.ema.Compute(closes); err == nil {
		s.store.Set(ctx, book, "ema", s.config.EMAPeriod, &IndicatorValue{
			Name: "ema", Period: s.config.EMAPeriod, Value: emaVal, Timestamp: now, Book: book,
		})
		promMetrics.SetIndicatorEMA(book, emaVal)
	}

	if rsiVal, err := s.rsi.Compute(closes); err == nil {
		s.store.Set(ctx, book, "rsi", s.config.RSIPeriod, &IndicatorValue{
			Name: "rsi", Period: s.config.RSIPeriod, Value: rsiVal, Timestamp: now, Book: book,
		})
		promMetrics.SetIndicatorRSI(book, rsiVal)
	}

	if bb, err := s.bollinger.ComputeBands(closes); err == nil {
		s.store.SetBollinger(ctx, book, s.config.BollingerPeriod, bb)
		s.store.Set(ctx, book, "bollinger_middle", s.config.BollingerPeriod, &IndicatorValue{
			Name: "bollinger_middle", Period: s.config.BollingerPeriod, Value: bb.Middle, Timestamp: now, Book: book,
			Extra: map[string]float64{"upper": bb.Upper, "lower": bb.Lower, "stddev": bb.StdDev},
		})
		promMetrics.SetIndicatorBollinger(book, bb.Upper, bb.Middle, bb.Lower)
	}

	if atrVal, err := s.atr.ComputeFromBars(bars); err == nil {
		s.store.Set(ctx, book, "atr", s.config.ATRPeriod, &IndicatorValue{
			Name: "atr", Period: s.config.ATRPeriod, Value: atrVal, Timestamp: now, Book: book,
		})
		promMetrics.SetIndicatorATR(book, atrVal)
	}

	trades, tradeErr := s.dataProvider.GetRecentTrades(ctx, book, 100)
	if tradeErr == nil && len(trades) > 0 {
		if vwapVal, err := s.vwap.ComputeFromTrades(trades); err == nil {
			s.store.Set(ctx, book, "vwap", s.config.VWAPPeriod, &IndicatorValue{
				Name: "vwap", Period: s.config.VWAPPeriod, Value: vwapVal, Timestamp: now, Book: book,
			})
			promMetrics.SetIndicatorVWAP(book, vwapVal)
		}
	}

	ready := !barStale
	staleReason := ""
	if barStale {
		staleReason = barStaleReason(lastBarAt)
		promMetrics.SetIndicatorsHealthy(false)
	} else {
		promMetrics.SetIndicatorsHealthy(true)
	}

	s.setBookHealth(book, bookHealth{
		Ready:        ready,
		ComputedAt:   now,
		LastBarAt:    lastBarAt,
		BarsUsed:     len(bars),
		CurrentPrice: currentPrice,
		StaleReason:  staleReason,
	})
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
	Book         string          `json:"book"`
	Timestamp    time.Time       `json:"timestamp"`
	CurrentPrice float64         `json:"current_price,omitempty"`
	DataHealthy  bool            `json:"data_healthy"`
	ComputedAt   time.Time       `json:"computed_at,omitempty"`
	BarsUsed     int             `json:"bars_used,omitempty"`
	DataAgeSec    float64         `json:"data_age_sec,omitempty"`
	LastBarAgeSec float64         `json:"last_bar_age_sec,omitempty"`
	StaleReason   string          `json:"stale_reason,omitempty"`
	SMA          *IndicatorValue `json:"sma,omitempty"`
	EMA          *IndicatorValue `json:"ema,omitempty"`
	RSI          *IndicatorValue `json:"rsi,omitempty"`
	Bollinger    *BollingerBands `json:"bollinger,omitempty"`
	ATR          *IndicatorValue `json:"atr,omitempty"`
	VWAP         *IndicatorValue `json:"vwap,omitempty"`
}

// GetSnapshot returns a snapshot of all indicators for a book
func (s *Service) GetSnapshot(ctx context.Context, book string) (*Snapshot, error) {
	now := time.Now()
	snapshot := &Snapshot{
		Book:      book,
		Timestamp: now,
	}

	snapshot.SMA, _ = s.GetSMA(ctx, book)
	snapshot.EMA, _ = s.GetEMA(ctx, book)
	snapshot.RSI, _ = s.GetRSI(ctx, book)
	snapshot.Bollinger, _ = s.GetBollinger(ctx, book)
	snapshot.ATR, _ = s.GetATR(ctx, book)
	snapshot.VWAP, _ = s.GetVWAP(ctx, book)

	h := s.getBookHealth(book)
	snapshot.ComputedAt = h.ComputedAt
	snapshot.BarsUsed = h.BarsUsed
	snapshot.CurrentPrice = h.CurrentPrice
	if !h.ComputedAt.IsZero() {
		snapshot.DataAgeSec = now.Sub(h.ComputedAt).Seconds()
	}
	if !h.LastBarAt.IsZero() {
		snapshot.LastBarAgeSec = now.Sub(h.LastBarAt).Seconds()
	}

	snapshot.DataHealthy = s.evaluateSnapshotHealth(snapshot, h)
	if !snapshot.DataHealthy && h.StaleReason != "" {
		snapshot.StaleReason = h.StaleReason
	} else if !snapshot.DataHealthy {
		snapshot.StaleReason = "indicator values missing or stale"
	}

	return snapshot, nil
}

func (s *Service) isBarDataStale(lastBarAt time.Time) bool {
	return s.config.MaxStaleness > 0 && !lastBarAt.IsZero() && time.Since(lastBarAt) > s.config.MaxStaleness
}

func barStaleReason(lastBarAt time.Time) string {
	return fmt.Sprintf("bar data stale: last bar %v ago", time.Since(lastBarAt).Round(time.Second))
}

func (s *Service) evaluateSnapshotHealth(snapshot *Snapshot, h bookHealth) bool {
	if !h.Ready {
		return false
	}
	if snapshot.SMA == nil || snapshot.EMA == nil || snapshot.RSI == nil || snapshot.Bollinger == nil || snapshot.ATR == nil {
		return false
	}
	if snapshot.CurrentPrice <= 0 {
		return false
	}
	if h.ComputedAt.IsZero() {
		return false
	}
	if s.isBarDataStale(h.LastBarAt) {
		return false
	}
	if s.config.MaxStaleness > 0 && time.Since(h.ComputedAt) > s.config.MaxStaleness {
		return false
	}
	return true
}
