package restfallback

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/models"
)

type stubIngestor struct {
	lastTrade time.Time
	events    []*models.TradeEvent
}

func (s *stubIngestor) ProcessTradeEvent(trade *models.TradeEvent) error {
	s.events = append(s.events, trade)
	s.lastTrade = trade.Timestamp
	return nil
}

func (s *stubIngestor) LastTradeAge() time.Duration {
	if s.lastTrade.IsZero() {
		return time.Hour
	}
	return time.Since(s.lastTrade)
}

func TestPollerSkipsWhenStreamFresh(t *testing.T) {
	ingestor := &stubIngestor{lastTrade: time.Now()}
	poller := NewPoller(Config{
		Books:     []string{"btc_mxn"},
		Interval:  time.Minute,
		Threshold: 5 * time.Minute,
		Ingestor:  ingestor,
	})
	poller.pollOnce(nil)
	if len(ingestor.events) != 0 {
		t.Fatalf("expected no REST fetch when stream fresh, got %d events", len(ingestor.events))
	}
}
