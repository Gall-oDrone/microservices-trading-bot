package news

import (
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/models"
)

// Snapshot is the latest sentiment state for one symbol.
type Snapshot struct {
	Symbol         string
	Signal         string
	SentimentScore float64
	ImpactLevel    string
	Actionable     bool
	UpdatedAt      time.Time
}

// Store holds per-symbol sentiment from news.agentic (in-memory, optional).
type Store struct {
	mu       sync.RWMutex
	bySymbol map[string]Snapshot
	ttl      time.Duration
}

// NewStore creates a sentiment store. ttl <= 0 disables expiry checks.
func NewStore(ttl time.Duration) *Store {
	return &Store{
		bySymbol: make(map[string]Snapshot),
		ttl:      ttl,
	}
}

// Upsert applies a NewsAgenticEvent (keeps newest PublishedAtMs per symbol).
func (s *Store) Upsert(evt models.NewsAgenticEvent) {
	sym := normalizeSymbol(evt.Symbol)
	if sym == "" {
		sym = "GENERAL"
	}
	at := time.UnixMilli(evt.PublishedAtMs)
	if at.IsZero() {
		at = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.bySymbol[sym]
	if ok && !prev.UpdatedAt.IsZero() && at.Before(prev.UpdatedAt) {
		return
	}
	s.bySymbol[sym] = Snapshot{
		Symbol:         sym,
		Signal:         strings.ToLower(strings.TrimSpace(evt.Signal)),
		SentimentScore: evt.SentimentScore,
		ImpactLevel:    strings.ToLower(strings.TrimSpace(evt.ImpactLevel)),
		Actionable:     evt.Actionable,
		UpdatedAt:      at,
	}
}

// Get returns the latest snapshot for a symbol if present and not expired.
func (s *Store) Get(symbol string) (Snapshot, bool) {
	sym := normalizeSymbol(symbol)
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.bySymbol[sym]
	if !ok {
		return Snapshot{}, false
	}
	if s.ttl > 0 && time.Since(snap.UpdatedAt) > s.ttl {
		return Snapshot{}, false
	}
	return snap, true
}

// BookToSymbol maps a strategy book to a news symbol (btc_mxn → BTC, AAPL → AAPL).
func BookToSymbol(book string) string {
	book = strings.TrimSpace(strings.ToLower(book))
	if book == "" {
		return ""
	}
	if i := strings.Index(book, "_"); i > 0 {
		return strings.ToUpper(book[:i])
	}
	if i := strings.Index(book, "-"); i > 0 {
		return strings.ToUpper(book[:i])
	}
	return strings.ToUpper(book)
}

func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}
