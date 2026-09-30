package gap_test

import (
	"testing"
	"time"

	"bitso-trading-platform/data-collector/internal/clock"
	"bitso-trading-platform/data-collector/internal/gap"
	"bitso-trading-platform/data-collector/internal/models"
)

func TestDetectorProducesGapsFromDisconnectReconnect(t *testing.T) {
	clk := &clock.FakeClock{T: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
	var emitted []models.GapRecord
	d := gap.NewDetector([]string{"btc_mxn"}, clk, func(g models.GapRecord) {
		emitted = append(emitted, g)
	})

	d.OnDisconnect()
	clk.Advance(90 * time.Second)
	recs := d.OnReconnect()
	if len(recs) != 1 {
		t.Fatalf("expected 1 gap record, got %d", len(recs))
	}
	if recs[0].Book != "btc_mxn" {
		t.Fatalf("book=%s", recs[0].Book)
	}
	if recs[0].Duration != 90*time.Second {
		t.Fatalf("duration=%s", recs[0].Duration)
	}
	if len(emitted) != 1 {
		t.Fatalf("emitted=%d", len(emitted))
	}

	clk.Advance(10 * time.Second)
	d.OnDisconnect()
	clk.Advance(5 * time.Second)
	recs2 := d.OnReconnect()
	if len(recs2) != 1 || recs2[0].Duration != 5*time.Second {
		t.Fatalf("unexpected second gap: %+v", recs2)
	}
	gaps := d.Gaps()
	if len(gaps) != 2 {
		t.Fatalf("gaps=%d", len(gaps))
	}
}

func TestDetectorEmitsOneGapPerBook(t *testing.T) {
	clk := &clock.FakeClock{T: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)}
	d := gap.NewDetector([]string{"btc_mxn", "btc_usd"}, clk, nil)
	d.OnDisconnect()
	clk.Advance(time.Minute)
	recs := d.OnReconnect()
	if len(recs) != 2 {
		t.Fatalf("got %d gaps, want 2", len(recs))
	}
	if recs[0].Book != "btc_mxn" || recs[1].Book != "btc_usd" {
		t.Fatalf("books=%s,%s", recs[0].Book, recs[1].Book)
	}
	if recs[0].Duration != time.Minute || recs[1].Duration != time.Minute {
		t.Fatalf("durations=%s,%s", recs[0].Duration, recs[1].Duration)
	}
}

func TestDetectorIgnoresReconnectWithoutDisconnect(t *testing.T) {
	clk := &clock.FakeClock{T: time.Now().UTC()}
	d := gap.NewDetector([]string{"btc_mxn"}, clk, nil)
	if d.OnReconnect() != nil {
		t.Fatal("expected nil gap")
	}
}

func TestDetectorIdempotentDisconnect(t *testing.T) {
	clk := &clock.FakeClock{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	d := gap.NewDetector([]string{"btc_mxn"}, clk, nil)
	d.OnDisconnect()
	clk.Advance(time.Minute)
	d.OnDisconnect()
	clk.Advance(time.Minute)
	recs := d.OnReconnect()
	if len(recs) != 1 || recs[0].Duration != 2*time.Minute {
		t.Fatalf("duration=%v want 2m", recs)
	}
}
