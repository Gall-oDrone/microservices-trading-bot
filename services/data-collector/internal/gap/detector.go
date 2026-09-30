package gap

import (
	"sync"
	"time"

	"bitso-trading-platform/data-collector/internal/clock"
	"bitso-trading-platform/data-collector/internal/models"
)

// Detector tracks disconnect/reconnect windows and produces GapRecords.
type Detector struct {
	books []string
	clock clock.Clock

	mu           sync.Mutex
	disconnectAt *time.Time
	gaps         []models.GapRecord
	onGap        func(models.GapRecord)
}

// NewDetector creates a gap detector. One GapRecord is emitted per book on
// reconnect, because a WebSocket outage affects every subscribed book.
func NewDetector(books []string, clk clock.Clock, onGap func(models.GapRecord)) *Detector {
	if clk == nil {
		clk = clock.RealClock{}
	}
	copied := append([]string(nil), books...)
	return &Detector{
		books: copied,
		clock: clk,
		onGap: onGap,
	}
}

// OnDisconnect records the start of an outage.
func (d *Detector) OnDisconnect() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.disconnectAt != nil {
		return
	}
	now := d.clock.Now()
	d.disconnectAt = &now
}

// OnReconnect closes an open outage and emits one GapRecord per book.
func (d *Detector) OnReconnect() []models.GapRecord {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.disconnectAt == nil {
		return nil
	}

	end := d.clock.Now()
	start := *d.disconnectAt
	d.disconnectAt = nil

	out := make([]models.GapRecord, 0, len(d.books))
	for _, book := range d.books {
		rec := models.GapRecord{
			Book:      book,
			Start:     start,
			End:       end,
			Duration:  end.Sub(start),
			CreatedAt: end,
		}
		d.gaps = append(d.gaps, rec)
		if d.onGap != nil {
			d.onGap(rec)
		}
		out = append(out, rec)
	}
	return out
}

// Gaps returns a copy of recorded gaps.
func (d *Detector) Gaps() []models.GapRecord {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]models.GapRecord, len(d.gaps))
	copy(out, d.gaps)
	return out
}
