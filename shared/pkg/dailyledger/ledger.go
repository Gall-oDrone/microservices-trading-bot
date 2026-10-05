// Package dailyledger is the importable, read-only contract for the
// daily-executor's append-only JSONL ledger
// (services/strategy-executor/cmd/daily-executor/ledger.go).
//
// The executor writes these lines with unexported types in package main and
// internal/dailyexec, which other modules cannot import. This package mirrors
// the JSON exactly; cmd/daily-executor/ledger_contract_test.go fails if the
// two drift apart. It never writes the ledger.
package dailyledger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
)

// Record is one ledger line: one book, one decision day.
type Record struct {
	RecordedAt  string     `json:"recorded_at"`
	CodeVersion string     `json:"code_version"`
	Mode        string     `json:"mode"` // "dry-run" | "stage"
	Book        string     `json:"book"`
	Prereg      string     `json:"prereg"`
	Decision    Decision   `json:"decision"`
	Paper       Paper      `json:"paper"`
	Candles     CandleInfo `json:"candles"`
	Stage       *Stage     `json:"stage,omitempty"`
}

// Decision is the rule's output for one day.
type Decision struct {
	BarDate    string  `json:"bar_date"`
	FillDate   string  `json:"fill_date"`
	Close      float64 `json:"close"`
	SMA        float64 `json:"sma50"`
	Signal     string  `json:"signal"`      // "long" | "flat"
	PrevSignal string  `json:"prev_signal"` // "long" | "flat"
	Action     string  `json:"action"`      // "buy" | "sell" | "hold"
}

// Paper is the forward test's paper account (starts at 1.0 quote unit).
type Paper struct {
	ForwardStart  string  `json:"forward_start"`
	Days          int     `json:"days"`
	Position      string  `json:"position"`
	Fills         int     `json:"fills"`
	LegCostBps    float64 `json:"leg_cost_bps"`
	Equity        float64 `json:"equity"`
	EquityClosed  float64 `json:"equity_if_closed"`
	HoldEquity    float64 `json:"hold_equity"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	PendingAction string  `json:"pending_action"`
}

// CandleInfo fingerprints the candle data a decision used.
type CandleInfo struct {
	Source      string `json:"source"`
	First       string `json:"first"`
	Last        string `json:"last"`
	Bars        int    `json:"bars"`
	RecentGaps  string `json:"recent_gaps,omitempty"`
	SHA256Short string `json:"sha256_prefix"`
}

// Position is what the executor holds on stage for one book.
type Position struct {
	State string  `json:"state"` // "long" | "flat"
	BTC   float64 `json:"btc"`
}

// Stage is the stage-execution part of a record.
type Stage struct {
	Env            string     `json:"env"`
	Target         string     `json:"target"`
	Action         string     `json:"action"` // "buy" | "sell" | "none" | "blocked"
	PositionBefore Position   `json:"position_before"`
	PositionAfter  Position   `json:"position_after"`
	Leg            *Leg       `json:"leg,omitempty"`
	Risk           *RiskCheck `json:"risk,omitempty"`
}

// ActionBlocked is Stage.Action when the executor's pre-trade risk check
// stopped the order (nothing was sent; the position is unchanged).
const ActionBlocked = "blocked"

// RiskCheck is the executor's pre-trade check (shared/pkg/risk.Check) for a
// planned order. Lines written before the check existed have none.
type RiskCheck struct {
	PolicyVersion string         `json:"policy_version"`
	Order         risk.Order     `json:"order"`
	State         risk.State     `json:"state"`
	Allowed       bool           `json:"allowed"`
	Findings      []risk.Finding `json:"findings,omitempty"`
}

// Leg mirrors internal/dailyexec.Result.
type Leg struct {
	Side        string             `json:"side"`
	Target      float64            `json:"target_btc"`
	Filled      float64            `json:"filled_btc"`
	MakerFilled float64            `json:"maker_btc"`
	TakerFilled float64            `json:"taker_btc"`
	BaseDelta   float64            `json:"base_delta"`
	AvgPrice    float64            `json:"avg_price"`
	Notional    float64            `json:"notional"`
	Fees        map[string]float64 `json:"fees"`
	MakerOrigin string             `json:"maker_origin_id"`
	TakerOrigin string             `json:"taker_origin_id"`
	Oids        []string           `json:"oids"`
	Placements  int                `json:"maker_placements"`
	Fallback    bool               `json:"market_fallback"`
	Shortfall   float64            `json:"shortfall_btc"`
	Notes       []string           `json:"notes,omitempty"`
	Started     time.Time          `json:"started"`
	Finished    time.Time          `json:"finished"`
}

// Read parses a ledger. Blank lines are skipped; any malformed line is an
// error with its line number. Records are returned sorted by (book, bar_date).
func Read(r io.Reader) ([]Record, error) {
	var out []Record
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 1; sc.Scan(); n++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Book != out[j].Book {
			return out[i].Book < out[j].Book
		}
		return out[i].Decision.BarDate < out[j].Decision.BarDate
	})
	return out, nil
}

// ReadFile reads a ledger file. A missing file is an empty ledger.
func ReadFile(path string) ([]Record, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	recs, err := Read(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return recs, nil
}

// ByBook groups records per book, each slice sorted by bar_date.
func ByBook(recs []Record) map[string][]Record {
	out := map[string][]Record{}
	for _, r := range recs {
		out[r.Book] = append(out[r.Book], r)
	}
	for b := range out {
		s := out[b]
		sort.SliceStable(s, func(i, j int) bool { return s[i].Decision.BarDate < s[j].Decision.BarDate })
	}
	return out
}
