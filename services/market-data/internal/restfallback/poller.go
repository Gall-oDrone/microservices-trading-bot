package restfallback

import (
	"context"
	"log"
	"net/url"
	"sync"
	"time"

	"bitso-trading-platform/market-data/internal/processor"
	"bitso-trading-platform/shared/pkg/bitso"
	"bitso-trading-platform/shared/pkg/models"
)

// TradeIngestor ingests normalized trades (implemented by processor.Processor).
type TradeIngestor interface {
	ProcessTradeEvent(trade *models.TradeEvent) error
	LastTradeAge() time.Duration
}

// Metrics records REST fallback activity.
type Metrics interface {
	RecordRESTFallbackFetch(success bool, tradesIngested int)
}

// Config holds REST fallback poller settings.
type Config struct {
	Books     []string
	Interval  time.Duration
	Threshold time.Duration
	APIBaseURL string
	Logger    *log.Logger
	Ingestor  TradeIngestor
	Metrics   Metrics
}

// Poller polls Bitso REST /trades when the WebSocket stream goes quiet.
type Poller struct {
	cfg Config

	client *bitso.Client

	mu          sync.Mutex
	lastTradeID map[string]uint64
	stopChan    chan struct{}
	wg          sync.WaitGroup
}

// NewPoller creates a REST fallback poller.
func NewPoller(cfg Config) *Poller {
	logger := cfg.Logger
	if logger == nil {
		logger = log.New(log.Writer(), "[REST-FALLBACK] ", log.LstdFlags|log.Lshortfile)
	}
	client := bitso.NewClient()
	if cfg.APIBaseURL != "" {
		client.SetAPIBaseURL(cfg.APIBaseURL)
	}
	return &Poller{
		cfg:         cfg,
		client:      client,
		lastTradeID: make(map[string]uint64),
		stopChan:    make(chan struct{}),
	}
}

// Start begins periodic REST polling.
func (p *Poller) Start(ctx context.Context) error {
	if p.cfg.Ingestor == nil || p.cfg.Threshold <= 0 || p.cfg.Interval <= 0 {
		return nil
	}
	p.wg.Add(1)
	go p.loop(ctx)
	return nil
}

// Stop stops the poller.
func (p *Poller) Stop() {
	close(p.stopChan)
	p.wg.Wait()
}

func (p *Poller) loop(ctx context.Context) {
	defer p.wg.Done()

	ticker := time.NewTicker(p.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *Poller) pollOnce(ctx context.Context) {
	if p.cfg.Ingestor.LastTradeAge() < p.cfg.Threshold {
		return
	}

	ingested := 0
	for _, book := range p.cfg.Books {
		n, err := p.fetchBook(ctx, book)
		if err != nil {
			p.cfg.Logger.Printf("REST fallback fetch failed for %s: %v", book, err)
			if p.cfg.Metrics != nil {
				p.cfg.Metrics.RecordRESTFallbackFetch(false, 0)
			}
			continue
		}
		ingested += n
	}

	if p.cfg.Metrics != nil {
		p.cfg.Metrics.RecordRESTFallbackFetch(true, ingested)
	}
	if ingested > 0 {
		p.cfg.Logger.Printf("REST fallback ingested %d trade(s)", ingested)
	}
}

func (p *Poller) fetchBook(ctx context.Context, book string) (int, error) {
	_ = ctx
	params := url.Values{"book": {book}}
	trades, err := p.client.Trades(params)
	if err != nil {
		return 0, err
	}

	ingested := 0
	p.mu.Lock()
	defer p.mu.Unlock()

	lastID := p.lastTradeID[book]
	for i := len(trades) - 1; i >= 0; i-- {
		trade := trades[i]
		id := uint64(trade.TID)
		if id <= lastID {
			continue
		}
		event := models.FromBitsoRESTTrade(&trade)
		if event == nil {
			continue
		}
		if err := p.cfg.Ingestor.ProcessTradeEvent(event); err != nil {
			return ingested, err
		}
		ingested++
		if id > lastID {
			lastID = id
		}
	}
	p.lastTradeID[book] = lastID
	return ingested, nil
}

// Ensure processor implements TradeIngestor.
var _ TradeIngestor = (*processor.Processor)(nil)
