package news

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/models"
)

func TestBookToSymbol(t *testing.T) {
	if got := BookToSymbol("btc_mxn"); got != "BTC" {
		t.Fatalf("btc_mxn: got %q", got)
	}
	if got := BookToSymbol("AAPL"); got != "AAPL" {
		t.Fatalf("AAPL: got %q", got)
	}
}

func TestFilterBlocksBearishBuy(t *testing.T) {
	store := NewStore(time.Hour)
	store.Upsert(testEvent("BTC", "bearish", -0.5, "medium"))
	f := NewFilter(store, FilterConfig{BlockBearishBuy: true, MinSentimentForBuy: -1})
	ok, reason := f.AllowsSignal("btc_mxn", "buy")
	if ok || reason == "" {
		t.Fatalf("expected bearish buy block, ok=%v reason=%q", ok, reason)
	}
}

func TestFilterAllowsSellDespiteBearish(t *testing.T) {
	store := NewStore(time.Hour)
	store.Upsert(testEvent("BTC", "bearish", -0.5, "medium"))
	f := NewFilter(store, FilterConfig{BlockBearishBuy: true, MinSentimentForBuy: -1})
	ok, _ := f.AllowsSignal("btc_mxn", "sell")
	if !ok {
		t.Fatal("sell should not be blocked by bearish sentiment")
	}
}

func testEvent(sym, signal string, score float64, impact string) models.NewsAgenticEvent {
	return models.NewsAgenticEvent{
		Symbol:         sym,
		Signal:         signal,
		SentimentScore: score,
		ImpactLevel:    impact,
		PublishedAtMs:  time.Now().UnixMilli(),
	}
}
