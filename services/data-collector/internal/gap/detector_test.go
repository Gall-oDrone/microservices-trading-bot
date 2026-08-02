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
	d := gap.NewDetector("btc_mxn", clk, func(g models.GapRecord) {
		emitted = append(emitted, g)
	})

	d.OnDisconnect()
	clk.Advance(90 * time.Second)
	rec := d.OnReconnect()
	if rec == nil {
		t.Fatal("expected gap record")
	}
	if rec.Book != "btc_mxn" {
		t.Fatalf("book=%s", rec.Book)
	}
	if rec.Duration != 90*time.Second {
		t.Fatalf("duration=%s", rec.Duration)
	}
	if len(emitted) != 1 {
		t.Fatalf("emitted=%d", len(emitted))
	}

	// Second disconnect/reconnect
	clk.Advance(10 * time.Second)
	d.OnDisconnect()
	clk.Advance(5 * time.Second)
	rec2 := d.OnReconnect()
	if rec2 == nil || rec2.Duration != 5*time.Second {
		t.Fatalf("unexpected second gap: %+v", rec2)
	}
	gaps := d.Gaps()
	if len(gaps) != 2 {
		t.Fatalf("gaps=%d", len(gaps))
	}
}

func TestDetectorIgnoresReconnectWithoutDisconnect(t *testing.T) {
	clk := &clock.FakeClock{T: time.Now().UTC()}
	d := gap.NewDetector("btc_mxn", clk, nil)
	if d.OnReconnect() != nil {
		t.Fatal("expected nil gap")
	}
}

func TestDetectorIdempotentDisconnect(t *testing.T) {
	clk := &clock.FakeClock{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	d := gap.NewDetector("btc_mxn", clk, nil)
	d.OnDisconnect()
	clk.Advance(time.Minute)
	d.OnDisconnect() // should not move start
	clk.Advance(time.Minute)
	rec := d.OnReconnect()
	if rec.Duration != 2*time.Minute {
		t.Fatalf("duration=%s want 2m", rec.Duration)
	}
}
