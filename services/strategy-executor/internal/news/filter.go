package news

import (
	"strings"
	"sync"
	"time"
)

// FilterConfig controls post-signal gating from agentic news sentiment.
type FilterConfig struct {
	MinSentimentForBuy float64       // block buy if score below (typical range -1..1)
	BlockBearishBuy    bool          // block buy when signal is bearish
	HighImpactCooldown time.Duration // block all new signals after high-impact news
}

// Filter gates trade signals using the news sentiment store (Option B).
type Filter struct {
	store    *Store
	cfg      FilterConfig
	coolMu   sync.Mutex
	cooldown map[string]time.Time // symbol -> cooldown until
}

// NewFilter creates a sentiment gate. store may be nil (always allows).
func NewFilter(store *Store, cfg FilterConfig) *Filter {
	return &Filter{
		store:    store,
		cfg:      cfg,
		cooldown: make(map[string]time.Time),
	}
}

// AllowsSignal returns false when news context blocks the signal side for the book.
func (f *Filter) AllowsSignal(book, side string) (bool, string) {
	if f.store == nil {
		return true, ""
	}
	sym := BookToSymbol(book)
	if sym == "" {
		return true, ""
	}

	now := time.Now().UTC()
	f.coolMu.Lock()
	until, inCooldown := f.cooldown[sym]
	f.coolMu.Unlock()
	if inCooldown && now.Before(until) {
		return false, "news high-impact cooldown active for " + sym
	}

	snap, ok := f.store.Get(sym)
	if !ok {
		return true, ""
	}

	if f.cfg.HighImpactCooldown > 0 && snap.ImpactLevel == "high" {
		f.coolMu.Lock()
		f.cooldown[sym] = now.Add(f.cfg.HighImpactCooldown)
		f.coolMu.Unlock()
		return false, "news high-impact event for " + sym
	}

	side = strings.ToLower(strings.TrimSpace(side))
	if side != "buy" {
		return true, ""
	}

	if f.cfg.BlockBearishBuy && snap.Signal == "bearish" {
		return false, "news sentiment bearish for " + sym
	}
	if snap.SentimentScore < f.cfg.MinSentimentForBuy {
		return false, "news sentiment score below threshold for " + sym
	}
	return true, ""
}
