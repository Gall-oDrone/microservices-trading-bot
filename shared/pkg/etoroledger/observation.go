package etoroledger

// Observations (2026-10-22, docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md
// §3.5): the measurement series of the real-account canary. One line per
// run of cmd/etoro-canary-observe (18:00 New York daily, weekends
// included) in observations.jsonl next to the canary ledger: the account,
// every open position with the fees eToro has charged it so far and the
// day's delta (the measurement), the cost preview for the held size, and
// the trading-history rows that appeared since the previous observation.
// The job reads; it never sends anything. etoro-fees-check reads the file
// back for its "per night, as observed" table.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ObservationSchema is Observation.Schema.
const ObservationSchema = "etoro-observation/v1"

// ObservationsFile is the file's name next to the ledger.
const ObservationsFile = "observations.jsonl"

// ObservationsPath is the observations file of a ledger path.
func ObservationsPath(ledgerPath string) string {
	return filepath.Join(filepath.Dir(ledgerPath), ObservationsFile)
}

// Observation is one line of observations.jsonl.
type Observation struct {
	Schema string `json:"schema"`
	// At is the read's UTC time; NYDate the New York calendar date it
	// belongs to (the 18:00 run of day D observes the night D-1 -> D as
	// booked at 17:00 New York).
	At     string `json:"at"`
	NYDate string `json:"ny_date"`
	// Env is "demo" (rehearsal) or "real"; Ledger the ledger name.
	Env    string `json:"env"`
	Ledger string `json:"ledger"`
	// Account is the account as the portfolio reports it.
	Account Account `json:"account"`
	// Positions are the open positions, every instrument.
	Positions []ObservedPosition `json:"positions"`
	// HistoryNew are the closed trades the history lists now and did not
	// at the previous observation (by position id).
	HistoryNew []ObservedClosed `json:"history_new"`
	// HistoryIDs are every closed position id seen so far (so the next
	// observation can tell new rows apart without re-reading old lines).
	HistoryIDs []string `json:"history_ids,omitempty"`
	// PreviousAt is the observation this one's deltas are measured
	// against ("" on the first).
	PreviousAt string `json:"previous_at,omitempty"`
	// Errors are reads that failed (the line is still written so a gap is
	// visible); Notes are facts worth a sentence.
	Errors []string `json:"errors,omitempty"`
	Notes  []string `json:"notes,omitempty"`
}

// ObservedPosition is one open position as observed.
type ObservedPosition struct {
	PositionID   string  `json:"position_id"`
	InstrumentID int64   `json:"instrument_id"`
	Symbol       string  `json:"symbol,omitempty"`
	Units        float64 `json:"units"`
	Amount       float64 `json:"amount"`
	Leverage     int     `json:"leverage"`
	OpenRate     float64 `json:"open_rate"`
	OpenedAt     string  `json:"opened_at"`
	// Exposure and Mark come from the position's unrealised block.
	Exposure   float64 `json:"exposure,omitempty"`
	Mark       float64 `json:"mark,omitempty"`
	Unrealized float64 `json:"unrealized"`
	// FeesToDate is eToro's totalFees (financing charged so far, negative
	// or positive as the venue reports it); FeesDelta is the change since
	// the previous observation of the same position (the night's charge),
	// nil on the first observation of the position.
	FeesToDate float64  `json:"fees_to_date"`
	FeesDelta  *float64 `json:"fees_delta,omitempty"`
	// Nights is how many calendar nights the position had been held at the
	// observation (17:00 New York boundaries).
	Nights         int     `json:"nights"`
	StopLossRate   float64 `json:"stop_loss_rate,omitempty"`
	TakeProfitRate float64 `json:"take_profit_rate,omitempty"`
	// Preview is the cost preview for an open of the same size and
	// leverage now (what eToro says one night and the weekend would cost).
	Preview *Costs `json:"preview,omitempty"`
	// InLedger says whether the canary ledger knows the position.
	InLedger bool `json:"in_ledger"`
}

// ObservedClosed is a trading-history row.
type ObservedClosed struct {
	PositionID   string  `json:"position_id"`
	InstrumentID int64   `json:"instrument_id"`
	Units        float64 `json:"units"`
	Leverage     int     `json:"leverage"`
	OpenRate     float64 `json:"open_rate"`
	CloseRate    float64 `json:"close_rate"`
	OpenedAt     string  `json:"opened_at"`
	ClosedAt     string  `json:"closed_at"`
	// NetProfit excludes Fees (the history's convention).
	NetProfit      float64 `json:"net_profit"`
	Fees           float64 `json:"fees"`
	StopLossRate   float64 `json:"stop_loss_rate,omitempty"`
	TakeProfitRate float64 `json:"take_profit_rate,omitempty"`
	// SeenAt is when the observe job first listed it: the history's lag
	// after a close (measurement M4) is SeenAt - ClosedAt.
	SeenAt string `json:"seen_at"`
}

// NightsHeld counts the 17:00 New York boundaries between openedAt and at
// (eToro books overnight fees at 17:00 New York; a position opened at
// 10:00 and observed at 18:00 the same day has crossed one).
func NightsHeld(openedAt, at time.Time, ny *time.Location) int {
	if ny == nil || openedAt.IsZero() || !at.After(openedAt) {
		return 0
	}
	o, a := openedAt.In(ny), at.In(ny)
	boundary := func(t time.Time) time.Time {
		// The 17:00 boundary at or before t.
		b := time.Date(t.Year(), t.Month(), t.Day(), 17, 0, 0, 0, ny)
		if t.Before(b) {
			b = b.AddDate(0, 0, -1)
		}
		return b
	}
	bo, ba := boundary(o), boundary(a)
	n := 0
	for d := bo; d.Before(ba); d = d.AddDate(0, 0, 1) {
		n++
	}
	return n
}

// ReadObservations reads a file; a missing file is an empty series. Lines
// come back in file order (oldest first).
func ReadObservations(path string) ([]Observation, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readObservations(f, path)
}

func readObservations(r io.Reader, name string) ([]Observation, error) {
	var out []Observation
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var o Observation
		if err := json.Unmarshal(sc.Bytes(), &o); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", name, n, err)
		}
		out = append(out, o)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

// AppendObservation writes o as one line (fsync).
func AppendObservation(path string, o Observation) error {
	if o.Schema == "" {
		o.Schema = ObservationSchema
	}
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Latest is the newest observation by At, or nil.
func LatestObservation(obs []Observation) *Observation {
	if len(obs) == 0 {
		return nil
	}
	best := 0
	for i := range obs {
		if obs[i].At > obs[best].At {
			best = i
		}
	}
	return &obs[best]
}

// FeeDeltas lists, per position, the nightly fee deltas the series
// recorded (newest last): the measurement M1 reads.
type FeeDelta struct {
	PositionID string  `json:"position_id"`
	NYDate     string  `json:"ny_date"`
	At         string  `json:"at"`
	Nights     int     `json:"nights"`
	Delta      float64 `json:"delta"`
	FeesToDate float64 `json:"fees_to_date"`
	// PreviewOvernight / PreviewWeekend are the cost preview's figures on
	// the same observation (0 when it could not be read).
	PreviewOvernight float64 `json:"preview_overnight,omitempty"`
	PreviewWeekend   float64 `json:"preview_weekend,omitempty"`
	Exposure         float64 `json:"exposure,omitempty"`
}

// FeeDeltas extracts every recorded delta, ordered by position then time.
func FeeDeltas(obs []Observation) []FeeDelta {
	var out []FeeDelta
	for _, o := range obs {
		for _, p := range o.Positions {
			if p.FeesDelta == nil {
				continue
			}
			d := FeeDelta{PositionID: p.PositionID, NYDate: o.NYDate, At: o.At, Nights: p.Nights, Delta: *p.FeesDelta, FeesToDate: p.FeesToDate, Exposure: p.Exposure}
			if p.Preview != nil {
				d.PreviewOvernight, d.PreviewWeekend = p.Preview.OvernightFee, p.Preview.WeekendFee
			}
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PositionID != out[j].PositionID {
			return out[i].PositionID < out[j].PositionID
		}
		return out[i].At < out[j].At
	})
	return out
}
