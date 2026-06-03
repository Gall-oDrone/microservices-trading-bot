package bars

import (
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/models"
)

func TestBuildFromTrades_1m(t *testing.T) {
	base := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	trades := []*models.TradeEvent{
		{Price: 100, Amount: 0.1, Timestamp: base.Add(10 * time.Second)},
		{Price: 105, Amount: 0.2, Timestamp: base.Add(40 * time.Second)},
		{Price: 102, Amount: 0.1, Timestamp: base.Add(time.Minute + 5*time.Second)},
	}
	bars := BuildFromTrades(trades, time.Minute, 10)
	if len(bars) != 2 {
		t.Fatalf("len(bars)=%d want 2", len(bars))
	}
	if bars[0].Open != 100 || bars[0].High != 105 || bars[0].Low != 100 || bars[0].Close != 105 {
		t.Fatalf("bar0 %+v", bars[0])
	}
	if bars[1].Open != 102 || bars[1].Close != 102 {
		t.Fatalf("bar1 %+v", bars[1])
	}
}

func TestParseInterval(t *testing.T) {
	d, err := ParseInterval("1m")
	if err != nil || d != time.Minute {
		t.Fatalf("1m: %v %v", d, err)
	}
	if _, err := ParseInterval("2h"); err == nil {
		t.Fatal("expected error for 2h")
	}
}
