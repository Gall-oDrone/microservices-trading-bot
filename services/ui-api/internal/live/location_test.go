package live

import (
	"io"
	"log"
	"testing"
	"time"
)

// 2026-10-16 02:00 UTC is still 2026-10-15 in Mexico City (UTC-6).
var smallHours = time.Date(2026, 10, 16, 2, 0, 0, 0, time.UTC)

func TestHubLocationBucketsTheDay(t *testing.T) {
	now := smallHours
	tr := Trade{Book: "btc_usd", Price: 100, Amount: 1, Side: "buy", At: smallHours}

	mx := NewHub([]string{"btc_usd"}, "test", nil, nil, log.New(io.Discard, "", 0))
	mx.Now = func() time.Time { return now }
	mx.OnTrade(tr)
	if c := mx.Snapshot(nil).Books[0].Candle; c == nil || c.Date != "2026-10-15" {
		t.Fatalf("Mexico City bucket: %+v", c)
	}

	utc := NewHubIn(time.UTC, []string{"btc_usd"}, "test", nil, nil, log.New(io.Discard, "", 0))
	utc.Now = func() time.Time { return now }
	utc.OnTrade(tr)
	if c := utc.Snapshot(nil).Books[0].Candle; c == nil || c.Date != "2026-10-16" {
		t.Fatalf("UTC bucket: %+v", c)
	}
	if Day(smallHours) != "2026-10-15" || DayIn(smallHours, time.UTC) != "2026-10-16" {
		t.Fatalf("Day helpers: %s %s", Day(smallHours), DayIn(smallHours, time.UTC))
	}
}

// A quote-only hub (the eToro rates poller sends no trades): the mid of
// each top-of-book update is the last price and builds the forming bar,
// the tape stays empty, and the provisional level follows the mid.
func TestHubQuoteOnlyUsesTheMid(t *testing.T) {
	now := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	h := NewHubIn(time.UTC, []string{"nsdq100"}, "etoro", nil, closesEndingOn("2026-10-15", 60, 30000), log.New(io.Discard, "", 0))
	h.QuoteOnly = true
	h.Now = func() time.Time { return now }
	h.OnTop(Top{Book: "nsdq100", Bid: 29990, Ask: 30010, At: now})
	h.OnTop(Top{Book: "nsdq100", Bid: 30090, Ask: 30110, At: now.Add(5 * time.Second)})
	h.OnTop(Top{Book: "nsdq100", Bid: 0, Ask: 30120, At: now.Add(10 * time.Second)}) // one-sided: no mid
	s := h.SnapshotWith(nil, true)
	b := s.Books[0]
	if b.Last != 30100 || b.LastSide != "" || b.Bid != 30090 || b.Ask != 30120 || b.LastAt != rfc(now.Add(5*time.Second)) {
		t.Fatalf("book %+v", b)
	}
	c := b.Candle
	if c == nil || c.Date != "2026-10-16" || c.Open != 30000 || c.High != 30100 || c.Low != 30000 || c.Close != 30100 || c.TradeCount != 2 || c.Volume != 0 {
		t.Fatalf("candle %+v", c)
	}
	if b.Provisional == nil || b.Provisional.FlipLevel != 30000 || b.Provisional.Signal != "long" || b.Provisional.Price != 30100 {
		t.Fatalf("provisional %+v (%s)", b.Provisional, b.ProvisionalNote)
	}
	if m := s.Markets[0]; len(m.Trades) != 0 || m.Mid != 30105 || m.SpreadBps <= 0 {
		t.Fatalf("market %+v", m)
	}
	// Without QuoteOnly a top-of-book update never sets the last price.
	plain := NewHubIn(time.UTC, []string{"nsdq100"}, "etoro", nil, nil, log.New(io.Discard, "", 0))
	plain.OnTop(Top{Book: "nsdq100", Bid: 1, Ask: 2, At: now})
	if b := plain.Snapshot(nil).Books[0]; b.Last != 0 || b.Candle != nil {
		t.Fatalf("plain hub %+v", b)
	}
}

// With PrevBar the bar before Monday's forming bar is Friday's; the
// calendar default would wait for a Sunday bar that never comes.
func TestProvisionalPrevBar(t *testing.T) {
	monday := "2026-10-19"
	closes := make([]float64, 49)
	for i := range closes {
		closes[i] = 100
	}
	if p, note := ComputeProvisional(closes, "2026-10-16", monday, 101); p != nil || note == "" {
		t.Fatalf("calendar rule must withhold: %+v %q", p, note)
	}
	prevTrading := func(d time.Time) time.Time {
		for d = d.AddDate(0, 0, -1); d.Weekday() == time.Saturday || d.Weekday() == time.Sunday; d = d.AddDate(0, 0, -1) {
		}
		return d
	}
	p, note := ComputeProvisionalWith(closes, "2026-10-16", monday, 101, prevTrading)
	if p == nil || p.Signal != "long" || p.BasedOn != "2026-10-16" {
		t.Fatalf("trading-day rule: %+v %q", p, note)
	}
	h := NewHubIn(time.UTC, []string{"spx500"}, "etoro", nil, closesEndingOn("2026-10-16", 60, 100), log.New(io.Discard, "", 0))
	h.QuoteOnly, h.PrevBar = true, prevTrading
	at := time.Date(2026, 10, 19, 14, 0, 0, 0, time.UTC)
	h.Now = func() time.Time { return at }
	h.OnTop(Top{Book: "spx500", Bid: 98, Ask: 100, At: at})
	if b := h.Snapshot(nil).Books[0]; b.Provisional == nil || b.Provisional.Signal != "flat" || b.Candle.Date != monday {
		t.Fatalf("hub with PrevBar: %+v (%s)", b, b.ProvisionalNote)
	}
}
