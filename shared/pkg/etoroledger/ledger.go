// Package etoroledger is the append-only JSONL ledger of the eToro index-CFD
// executor (services/strategy-executor/cmd/etoro-daily-executor): one line
// per instrument per decision day, plus an intent journal that makes every
// demo order resumable after a crash.
//
// Unlike the Bitso ledger (shared/pkg/dailyledger, a read-only mirror of
// types private to cmd/daily-executor), the types here are the schema: the
// executor writes them and every reader (cmd/etoro-reconcile, and ui-api from
// porting-plan phase P5) imports them, so they cannot drift apart.
//
// Since P5d (2026-10-16) the ledger also holds the assisted trades an
// operator places from the console (ui-api, Record.Source == SourceUI).
// They are demo lines like the executor's, keyed by (book, bar_date,
// client_ref) instead of (book, bar_date), so a UI trade on day D never
// collides with the executor's bar-D line written on D+1; LastDemoPosition
// orders by (bar_date, recorded_at), so the executor plans from the UI line
// the next morning and brings the book back to the rule's target.
package etoroledger

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
)

// Schema is Record.Schema for every line this version writes.
const Schema = "etoro-ledger/v1"

// Modes.
const (
	ModeDryRun = "dry-run"
	ModeDemo   = "demo"
)

// Demo actions.
const (
	ActionOpen    = "open"
	ActionClose   = "close"
	ActionNone    = "none"
	ActionBlocked = "blocked"
)

// Sources of a record.
const (
	// SourceExecutor is the daily run (the default; omitted in JSON).
	SourceExecutor = ""
	// SourceUI is an assisted trade an operator placed from the console
	// (ui-api POST /api/ui/etoro/trades/place).
	SourceUI = "ui"
)

// SignalManual is Decision.Signal of a UI record: the trade was the
// operator's call, not the rule's.
const SignalManual = "manual"

// Operator is who placed a UI record's trade and why (the same reason and
// by the control's audit line carries).
type Operator struct {
	By     string `json:"by"`
	Reason string `json:"reason"`
}

// Record is one ledger line: one instrument, one decision day (executor),
// or one assisted trade (Source == SourceUI).
type Record struct {
	Schema      string     `json:"schema"`
	RecordedAt  string     `json:"recorded_at"`
	CodeVersion string     `json:"code_version"`
	Mode        string     `json:"mode"`  // ModeDryRun | ModeDemo
	Venue       string     `json:"venue"` // "etoro"
	Book        string     `json:"book"`  // "nsdq100" | "spx500"
	Instrument  Instrument `json:"instrument"`
	Prereg      string     `json:"prereg"`
	Decision    Decision   `json:"decision"`
	Paper       Paper      `json:"paper"`
	Candles     CandleInfo `json:"candles"`
	Demo        *Demo      `json:"demo,omitempty"`
	// Source is SourceExecutor (empty) or SourceUI. A UI record has
	// Decision{BarDate, FillDate: the New York date of the trade, Signal:
	// SignalManual, Action: buy|sell, Close: the reference close the risk
	// check used}, zero Paper and Candles, and Demo as the executor writes
	// it. Readers that describe the rule (paper equity, run coverage, the
	// data-integrity check on recorded closes) skip UI records.
	Source   string    `json:"source,omitempty"`
	Operator *Operator `json:"operator,omitempty"`
}

// IsUI reports whether r is an assisted trade from the console.
func (r Record) IsUI() bool { return r.Source == SourceUI }

// ClientRef is the idempotency key of the record's order ("" without one).
func (r Record) ClientRef() string {
	if r.Demo != nil && r.Demo.Order != nil {
		return r.Demo.Order.ClientRef
	}
	return ""
}

// Instrument is the eToro instrument a book trades.
type Instrument struct {
	Symbol string `json:"symbol"`
	ID     int64  `json:"id"`
}

// Decision is the frozen rule's output for one day.
type Decision struct {
	BarDate    string  `json:"bar_date"`  // NYSE trading day t whose close decided
	FillDate   string  `json:"fill_date"` // next NYSE trading day, whose open (paper) / cash session (demo) executes
	Close      float64 `json:"close"`
	SMA        float64 `json:"sma50"`
	Signal     string  `json:"signal"`      // "long" | "flat" at t's close
	PrevSignal string  `json:"prev_signal"` // at t-1's close
	Action     string  `json:"action"`      // "buy" | "sell" | "hold"
}

// Paper is the pre-registered paper account (cfdsim with the frozen costs),
// from the forward start through the last closed bar, starting at 1.0.
type Paper struct {
	ForwardStart  string  `json:"forward_start"`
	Started       bool    `json:"started"`
	Days          int     `json:"days"`
	Position      string  `json:"position"` // held through the last close
	RoundTrips    int     `json:"round_trips"`
	SpreadRTBps   float64 `json:"spread_rt_bps"`
	OvernightBps  float64 `json:"overnight_bps"`
	CostsStatus   string  `json:"costs_status"`
	Equity        float64 `json:"equity_if_closed"`      // trend, closed at the last close with its spread
	HoldEquity    float64 `json:"hold_equity_if_closed"` // CFD hold over the same days
	MaxDrawdown   float64 `json:"max_drawdown"`          // fraction
	FinancingPct  float64 `json:"financing_pct"`         // overnight financing paid, % of start
	PendingAction string  `json:"pending_action"`        // what the next open does
}

// CandleInfo fingerprints the bars a decision used.
type CandleInfo struct {
	Source      string `json:"source"`
	First       string `json:"first"`
	Last        string `json:"last"`
	Bars        int    `json:"bars"`
	Missing     int    `json:"missing_trading_days"`
	SHA256Short string `json:"sha256_prefix"`
	// The executor fetches the bars twice: eToro's live daily route has been
	// seen serving two versions of recent history whose closes differ by up
	// to ~1.3 bps (2026-10-10). Revised counts the closes that differed
	// between the two fetches, MaxRevisionBps the largest difference.
	Revised        int     `json:"revised_closes,omitempty"`
	MaxRevisionBps float64 `json:"max_revision_bps,omitempty"`
}

// Position is what the executor holds on the demo account for one book.
type Position struct {
	State      string  `json:"state"` // "long" | "flat"
	PositionID string  `json:"position_id,omitempty"`
	Units      float64 `json:"units,omitempty"`
	Amount     float64 `json:"amount,omitempty"`
	OpenRate   float64 `json:"open_rate,omitempty"`
	OpenedAt   string  `json:"opened_at,omitempty"`
}

// Flat is the empty position.
func Flat() Position { return Position{State: "flat"} }

// Account is the demo account before the run's order.
type Account struct {
	Currency string  `json:"currency"`
	Cash     float64 `json:"cash"`
	Equity   float64 `json:"equity"`
	Exposure float64 `json:"exposure"` // all open positions, account currency
}

// Quote is the venue quote the order was checked against.
type Quote struct {
	Bid       float64 `json:"bid"`
	Ask       float64 `json:"ask"`
	At        string  `json:"at"`
	SpreadBps float64 `json:"spread_bps"`
}

// Costs is eToro's cost preview for an open (account currency).
type Costs struct {
	Spread         float64 `json:"spread"`
	Markup         float64 `json:"markup"`
	TransactionFee float64 `json:"transaction_fee"`
	OvernightFee   float64 `json:"overnight_fee"`
	WeekendFee     float64 `json:"weekend_fee"`
}

// RiskCheck is the pre-trade check of a planned order.
type RiskCheck struct {
	PolicyVersion string          `json:"policy_version"`
	Order         risk.Order      `json:"order"`
	State         risk.State      `json:"state"`
	Allowed       bool            `json:"allowed"`
	Findings      []risk.Finding  `json:"findings,omitempty"`
	Halt          *risk.HaltState `json:"halt,omitempty"`
}

// Order is one demo order (open or close) and its outcome.
type Order struct {
	Kind        string    `json:"kind"` // ActionOpen | ActionClose
	ClientRef   string    `json:"client_ref"`
	RequestID   string    `json:"request_id"` // x-request-id sent to eToro
	OrderID     string    `json:"order_id,omitempty"`
	Status      string    `json:"status"`
	PositionIDs []string  `json:"position_ids,omitempty"`
	AvgPrice    float64   `json:"avg_price,omitempty"`
	Units       float64   `json:"units,omitempty"`
	Amount      float64   `json:"amount,omitempty"`
	Fees        float64   `json:"fees,omitempty"`
	SpreadCost  float64   `json:"spread_cost,omitempty"`
	Error       string    `json:"error,omitempty"`
	ErrorCode   int       `json:"error_code,omitempty"`
	IntentAt    time.Time `json:"intent_at"`
	Started     time.Time `json:"started"`
	Finished    time.Time `json:"finished"`
	// Resumed: an earlier run sent this order (same ClientRef) and this run
	// resolved its outcome instead of sending it again.
	Resumed bool `json:"resumed,omitempty"`
	// FillVsMidBps is the fill's cost against the quote mid at check time:
	// (avg - mid)/mid for a buy, (mid - avg)/mid for a sell, in bps.
	FillVsMidBps float64 `json:"fill_vs_mid_bps,omitempty"`
	// FillVsCloseBps compares the fill with the decision bar's close (the
	// same sign convention): the gap between the paper reference and the
	// cash-session fill.
	FillVsCloseBps float64 `json:"fill_vs_close_bps,omitempty"`
}

// Demo is the demo-account part of a record.
type Demo struct {
	Env            string     `json:"env"` // "demo"
	Target         string     `json:"target"`
	Action         string     `json:"action"` // ActionOpen | ActionClose | ActionNone | ActionBlocked
	PositionBefore Position   `json:"position_before"`
	PositionAfter  Position   `json:"position_after"`
	Account        *Account   `json:"account,omitempty"`
	Quote          *Quote     `json:"quote,omitempty"`
	Costs          *Costs     `json:"costs,omitempty"`
	Risk           *RiskCheck `json:"risk,omitempty"`
	Order          *Order     `json:"order,omitempty"`
	// FinancingToDate is what eToro had charged the held position before
	// this run's order (position totalFees), account currency.
	FinancingToDate float64  `json:"financing_to_date,omitempty"`
	Notes           []string `json:"notes,omitempty"`
}

// Ledger is keyed by (book, bar_date) for the executor's lines (a decision
// day is written once) and by (book, bar_date, client_ref) for UI lines.
type Ledger struct {
	path    string
	mu      sync.Mutex
	entries map[string]Record
}

func key(book, barDate string) string { return book + "|" + barDate }

// recordKey is the dedup key of r: the executor's (book, bar_date), or
// (book, bar_date, client_ref) for a UI record so it never collides with
// the executor's line for the same bar date.
func recordKey(r Record) string {
	if r.IsUI() {
		return key(r.Book, r.Decision.BarDate) + "|" + r.ClientRef()
	}
	return key(r.Book, r.Decision.BarDate)
}

// Open reads the ledger at path (a missing file is an empty ledger).
func Open(path string) (*Ledger, error) {
	l := &Ledger{path: path, entries: map[string]Record{}}
	recs, err := ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		l.entries[recordKey(r)] = r
	}
	return l, nil
}

// Path is the ledger file.
func (l *Ledger) Path() string { return l.path }

// Get returns the executor's record of a book's decision day (UI records
// have their own keys and are not returned here).
func (l *Ledger) Get(book, barDate string) (Record, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.entries[key(book, barDate)]
	return r, ok
}

// Records returns every record sorted by (book, bar_date, recorded_at).
func (l *Ledger) Records() []Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Record, 0, len(l.entries))
	for _, r := range l.entries {
		out = append(out, r)
	}
	sortRecords(out)
	return out
}

// LastDemoPosition is the position after the book's latest demo record.
func (l *Ledger) LastDemoPosition(book string) Position {
	return LastDemoPosition(l.Records(), book)
}

// LastDemoPosition over records (any order): the position after the newest
// demo record by (bar_date, recorded_at), see LatestDemo.
func LastDemoPosition(recs []Record, book string) Position {
	if r, ok := LatestDemo(recs, book); ok {
		return r.Demo.PositionAfter
	}
	return Flat()
}

// LatestDemo is the book's newest record with a demo section, ordered by
// bar date and then by recorded time (a tie goes to the later record in
// recs, i.e. the later line of the file). A UI trade on day D (bar_date D,
// recorded D 11:00) is therefore newer than the executor's bar D-1 line
// (recorded D 09:35) and older than its bar-D line (recorded D+1 09:35),
// which is what both the executor and reconcile must plan from.
func LatestDemo(recs []Record, book string) (Record, bool) {
	var best Record
	found := false
	for _, r := range recs {
		if r.Book != book || r.Demo == nil {
			continue
		}
		if !found || !Newer(best, r) {
			best, found = r, true
		}
	}
	return best, found
}

// Newer reports whether a is newer than b: by bar date, then by recorded
// time (RFC 3339; a time that does not parse sorts as its string).
func Newer(a, b Record) bool {
	if a.Decision.BarDate != b.Decision.BarDate {
		return a.Decision.BarDate > b.Decision.BarDate
	}
	ta, ea := time.Parse(time.RFC3339Nano, a.RecordedAt)
	tb, eb := time.Parse(time.RFC3339Nano, b.RecordedAt)
	if ea == nil && eb == nil {
		return ta.After(tb)
	}
	return a.RecordedAt > b.RecordedAt
}

// LegsOn counts the book's demo orders on a fill date, the executor's and
// the console's alike: one leg per instrument per trading day is policy
// (risk.EtoroDemoPolicy MaxOrdersPerDay), whoever sends it.
func LegsOn(recs []Record, book, fillDate string) int {
	n := 0
	for _, r := range recs {
		if r.Book == book && r.Decision.FillDate == fillDate && r.Demo != nil && r.Demo.Order != nil {
			n++
		}
	}
	return n
}

// RuleRecords returns the executor's records only (no UI lines), in the
// order given, for readers that describe the frozen rule.
func RuleRecords(recs []Record) []Record {
	out := make([]Record, 0, len(recs))
	for _, r := range recs {
		if !r.IsUI() {
			out = append(out, r)
		}
	}
	return out
}

// Append writes r (fsync) unless its key is already recorded: the day for
// an executor line, the (day, client_ref) for a UI line, which must carry
// an order with a ClientRef.
func (l *Ledger) Append(r Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if r.IsUI() && r.ClientRef() == "" {
		return fmt.Errorf("etoroledger: a ui record needs demo.order.client_ref")
	}
	k := recordKey(r)
	if _, dup := l.entries[k]; dup {
		if r.IsUI() {
			return fmt.Errorf("etoroledger: already has %s %s %s", r.Book, r.Decision.BarDate, r.ClientRef())
		}
		return fmt.Errorf("etoroledger: already has %s %s", r.Book, r.Decision.BarDate)
	}
	if r.Schema == "" {
		r.Schema = Schema
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
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
	l.entries[k] = r
	return f.Close()
}

// Read parses a ledger; a malformed line is an error with its number.
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
	sortRecords(out)
	return out, nil
}

// ReadFile reads a ledger file; a missing file is an empty ledger.
func ReadFile(path string) ([]Record, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
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

// sortRecords orders by book, then oldest first by Newer (bar date, then
// recorded time), so the last record of a book is its newest.
func sortRecords(rs []Record) {
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Book != rs[j].Book {
			return rs[i].Book < rs[j].Book
		}
		return Newer(rs[j], rs[i])
	})
}
