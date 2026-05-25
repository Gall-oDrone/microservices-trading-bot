package state

import (
	"sync"

	"bitso-trading-platform/shared/pkg/models"
)

// Store keeps the latest sentiment snapshot per symbol for HTTP queries.
type Store struct {
	mu       sync.RWMutex
	bySymbol map[string]models.NewsAgenticEvent
	lastKey  string
}

func New() *Store {
	return &Store{bySymbol: make(map[string]models.NewsAgenticEvent)}
}

func (s *Store) Upsert(evt models.NewsAgenticEvent) {
	if evt.Symbol == "" {
		evt.Symbol = "GENERAL"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.bySymbol[evt.Symbol]
	if !ok || evt.PublishedAtMs >= prev.PublishedAtMs {
		s.bySymbol[evt.Symbol] = evt
	}
}

func (s *Store) SetLastObjectKey(key string) {
	s.mu.Lock()
	s.lastKey = key
	s.mu.Unlock()
}

func (s *Store) LastObjectKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastKey
}

func (s *Store) Get(symbol string) (models.NewsAgenticEvent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	evt, ok := s.bySymbol[symbol]
	return evt, ok
}

func (s *Store) Snapshot() map[string]models.NewsAgenticEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]models.NewsAgenticEvent, len(s.bySymbol))
	for k, v := range s.bySymbol {
		out[k] = v
	}
	return out
}
