package news

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/models"
)

func TestStoreUpsertKeepsNewest(t *testing.T) {
	s := NewStore(time.Hour)
	old := time.Now().Add(-time.Hour).UnixMilli()
	newer := time.Now().UnixMilli()
	s.Upsert(models.NewsAgenticEvent{Symbol: "BTC", Signal: "bearish", PublishedAtMs: newer})
	s.Upsert(models.NewsAgenticEvent{Symbol: "BTC", Signal: "bullish", PublishedAtMs: old})
	snap, ok := s.Get("BTC")
	if !ok || snap.Signal != "bearish" {
		t.Fatalf("expected newest bearish, got %+v ok=%v", snap, ok)
	}
}
