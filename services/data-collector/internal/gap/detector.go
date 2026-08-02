package gap

import (
	"sync"
	"time"

	"bitso-trading-platform/data-collector/internal/clock"
	"bitso-trading-platform/data-collector/internal/models"
)

// Detector tracks disconnect/reconnect windows and produces GapRecords.
type Detector struct {
	book  string
	clock clock.Clock

	mu             sync.Mutex
	disconnectAt   *time.Time
	gaps           []models.GapRecord
	onGap          func(models.GapRecord)
}

// NewDetector creates a gap detector for the given book.
func NewDetector(book string, clk clock.Clock, onGap func(models.GapRecord)) *Detector {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &Detector{
		book:  book,
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

// OnReconnect closes an open outage and emits a GapRecord.
func (d *Detector) OnReconnect() *models.GapRecord {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.disconnectAt == nil {
		return nil
	}

	end := d.clock.Now()
	start := *d.disconnectAt
	d.disconnectAt = nil

	rec := models.GapRecord{
		Book:      d.book,
		Start:     start,
		End:       end,
		Duration:  end.Sub(start),
		CreatedAt: end,
	}
	d.gaps = append(d.gaps, rec)
	if d.onGap != nil {
		d.onGap(rec)
	}
	return &rec
}

// Gaps returns a copy of recorded gaps.
func (d *Detector) Gaps() []models.GapRecord {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]models.GapRecord, len(d.gaps))
	copy(out, d.gaps)
	return out
}
