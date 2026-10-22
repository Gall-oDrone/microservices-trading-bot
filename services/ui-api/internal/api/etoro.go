package api

// The eToro index-CFD executor (services/strategy-executor/cmd/etoro-daily-
// executor) in ui-api, porting plan P5a (2026-10-16): read-only views under
// /api/ui/etoro/ and the halt reach through the shared controls.
//
// An eToro ledger is a Ledger with Venue "etoro" (cmd: -etoro-ledger). Its
// Store is used for paths and the halt file only; the ledger is read with
// shared/pkg/etoroledger, the candles with a parser that accepts the
// executor's CSV (empty volume/vwap fields, which store.ParseCandles
// rejects), and the reconcile and risk reports are read from the files the
// wrapper leaves next to the ledger. Everything here reads local disk: the
// eToro ledger is the file the executor writes, never an S3 copy.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/datahealth"
	"bitso-trading-platform/ui-api/internal/etorohealth"
	"bitso-trading-platform/ui-api/internal/store"
)

// etoroBookRe matches the eToro books (nsdq100, spx500); bookRe is the
// Bitso pattern and stays as it is.
var etoroBookRe = regexp.MustCompile(`^[a-z0-9]{3,12}$`)

// etoroReportsShown is how many reconcile reports GET etoro/reconcile returns.
const etoroReportsShown = 10

// pickEtoro returns the eToro ledger named by ?ledger= (default: the first
// eToro ledger; 404 when none is configured).
func (s *Server) pickEtoro(w http.ResponseWriter, r *http.Request) (Ledger, bool) {
	return s.pickVenue(w, r, VenueEtoro)
}

// etoroLoad picks the ledger and reads it with etoroledger.
func (s *Server) etoroLoad(w http.ResponseWriter, r *http.Request) (Ledger, map[string][]etoroledger.Record, bool) {
	l, ok := s.pickEtoro(w, r)
	if !ok {
		return Ledger{}, nil, false
	}
	recs, err := etoroledger.ReadFile(l.Store.LedgerPath)
	if err != nil {
		s.Log.Printf("ledger %s: %v", l.Name, err)
		writeErr(w, http.StatusInternalServerError, "cannot read the "+l.Name+" ledger: "+err.Error())
		return Ledger{}, nil, false
	}
	by := map[string][]etoroledger.Record{}
	for _, rec := range recs { // etoroledger.Read sorts by (book, bar_date)
		by[rec.Book] = append(by[rec.Book], rec)
	}
	return l, by, true
}

// etoroBooks is every pre-registered book plus any book in the ledger.
func etoroBooks(by map[string][]etoroledger.Record) []string {
	set := map[string]bool{}
	for b := range by {
		set[b] = true
	}
	if len(set) == 0 {
		// An empty ledger (before its first run) shows the index-CFD
		// cell's books; the ETF cells' ledgers carry their own books from
		// their first line.
		for _, b := range etorohealth.DefaultBooks {
			set[b] = true
		}
	}
	out := make([]string, 0, len(set))
	for b := range set {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

func etoroBookParam(w http.ResponseWriter, r *http.Request, by map[string][]etoroledger.Record) (string, bool) {
	b := strings.ToLower(r.PathValue("book"))
	if !etoroBookRe.MatchString(b) {
		writeErr(w, http.StatusBadRequest, "invalid book")
		return "", false
	}
	for _, k := range etoroBooks(by) {
		if k == b {
			return b, true
		}
	}
	writeErr(w, http.StatusNotFound, "unknown book "+b)
	return "", false
}

// etoroPolicyFor is policyFor for an eToro ledger: the executor's built-in
// policy for the ledger's product (risk.EtoroDemoPolicy for the index-CFD
// cell, risk.EtoroDemoETFPolicy for the ETF cells since 2026-10-21; the
// product is read from the ledger's paper block, "cfd" when absent) with the
// ledger's halt file merged in.
func (s *Server) etoroPolicyFor(l Ledger) (risk.Policy, HaltFileInfo) {
	pol := etoroBasePolicy(l)
	info := HaltFileInfo{Path: l.Store.Where(l.Store.HaltPath())}
	h, found, err := l.Store.Halt()
	info.Found = found
	if err != nil {
		info.Error = err.Error()
		return pol, info
	}
	info.Halted, info.Reason, info.By, info.At = h.Halted, h.Reason, h.By, h.At
	return risk.ApplyHalt(pol, h), info
}

// etoroBasePolicy is the policy the executor enforces on this ledger, before
// the halt file: the one its newest recorded risk check names, when this
// build knows that version (the ledger is the record of what was enforced;
// a ledger from before phase C of the protection plan keeps showing the
// unprotected policy its checks ran under). Without a recorded check, or
// with a version this build does not know, the product's current default:
// EtoroDemoETFPolicy for the ETF cells, else EtoroDemoProtectedPolicy, which
// etoro-daily-executor defaults to (-protect) since 2026-10-19 and the
// console's assisted trades (trades.go, protection.go) check against. An
// unknown version then surfaces as the policy_mismatch finding. A ledger
// marked real (the canary's) is under EtoroRealCanaryPolicy whatever it
// has recorded: no executor ever runs on it.
func etoroBasePolicy(l Ledger) risk.Policy {
	if l.environment() == string(etoro.EnvReal) {
		return risk.EtoroRealCanaryPolicy()
	}
	recs, err := etoroledger.ReadFile(l.Store.LedgerPath)
	if err != nil {
		return risk.EtoroDemoProtectedPolicy()
	}
	for i := len(recs) - 1; i >= 0; i-- {
		d := recs[i].Demo
		if d == nil || d.Risk == nil {
			continue
		}
		if p, ok := etoroKnownPolicy(d.Risk.PolicyVersion); ok {
			return p
		}
		break
	}
	if etoroRecordsAreETF(recs) {
		return risk.EtoroDemoETFPolicy()
	}
	return risk.EtoroDemoProtectedPolicy()
}

// etoroKnownPolicy maps a recorded policy version to the built-in policy.
func etoroKnownPolicy(version string) (risk.Policy, bool) {
	for _, p := range []risk.Policy{risk.EtoroDemoProtectedPolicy(), risk.EtoroDemoPolicy(), risk.EtoroDemoETFPolicy(), risk.EtoroRealCanaryPolicy(), risk.EtoroRealCanaryRehearsalPolicy()} {
		if p.Version == version {
			return p, true
		}
	}
	return risk.Policy{}, false
}

// etoroRecordsAreETF reports whether the ledger's executor lines are ETF
// products (paper.product == "etf"). An empty ledger is not.
func etoroRecordsAreETF(recs []etoroledger.Record) bool {
	for _, r := range recs {
		if r.IsUI() {
			continue
		}
		return r.Paper.Product == "etf"
	}
	return false
}

func etoroPolicySrc(p risk.Policy) string {
	if strings.HasPrefix(p.Version, etoro.RealAckPolicyPrefix) {
		return "built-in shared/pkg/risk.EtoroRealCanaryPolicy (" + p.Version + ") REAL ACCOUNT"
	}
	if strings.HasPrefix(p.Version, "etoro-canary-rehearsal-") {
		return "built-in shared/pkg/risk.EtoroRealCanaryRehearsalPolicy (" + p.Version + ") rehearsal on demo"
	}
	if strings.HasPrefix(p.Version, "etoro-demo-etf-") {
		return "built-in shared/pkg/risk.EtoroDemoETFPolicy (" + p.Version + ")"
	}
	if p.Version == risk.EtoroDemoProtectedPolicy().Version {
		return "built-in shared/pkg/risk.EtoroDemoProtectedPolicy (" + p.Version + ")"
	}
	return "built-in shared/pkg/risk.EtoroDemoPolicy (" + p.Version + ")"
}

// EtoroForwardTestsResponse is GET /api/ui/etoro/forward-tests.
type EtoroForwardTestsResponse struct {
	Ledger      string             `json:"ledger"`
	Venue       string             `json:"venue"`
	GeneratedAt string             `json:"generated_at"`
	Session     EtoroSession       `json:"session"`
	Books       []EtoroForwardTest `json:"books"`
}

func (s *Server) etoroForwardTests(w http.ResponseWriter, r *http.Request) {
	l, by, ok := s.etoroLoad(w, r)
	if !ok {
		return
	}
	now := s.Now()
	pol, _ := s.etoroPolicyFor(l)
	resp := EtoroForwardTestsResponse{Ledger: l.Name, Venue: VenueEtoro, GeneratedAt: now.UTC().Format(time.RFC3339),
		Session: etoroSession(now), Books: []EtoroForwardTest{}}
	for _, b := range etoroBooks(by) {
		ft := buildEtoroForwardTest(b, by[b], now, buildEtoroBookRisk(pol, b, by[b], now))
		ft.Ledger = l.Name
		resp.Books = append(resp.Books, ft)
	}
	writeJSON(w, http.StatusOK, resp)
}

// EtoroLedgerResponse is GET /api/ui/etoro/forward-tests/{book}/ledger.
type EtoroLedgerResponse struct {
	Ledger  string               `json:"ledger"`
	Venue   string               `json:"venue"`
	Book    string               `json:"book"`
	Mode    string               `json:"mode"`
	Records []etoroledger.Record `json:"records"`
	Equity  []EtoroEquityPoint   `json:"equity"`
}

func (s *Server) etoroLedger(w http.ResponseWriter, r *http.Request) {
	l, by, ok := s.etoroLoad(w, r)
	if !ok {
		return
	}
	b, ok := etoroBookParam(w, r, by)
	if !ok {
		return
	}
	recs := by[b]
	if mode := r.URL.Query().Get("mode"); mode != "" {
		var f []etoroledger.Record
		for _, x := range recs {
			if x.Mode == mode {
				f = append(f, x)
			}
		}
		recs = f
	}
	if recs == nil {
		recs = []etoroledger.Record{}
	}
	resp := EtoroLedgerResponse{Ledger: l.Name, Venue: VenueEtoro, Book: b, Records: recs, Equity: buildEtoroEquity(recs)}
	if len(recs) > 0 {
		resp.Mode = recs[len(recs)-1].Mode
	}
	writeJSON(w, http.StatusOK, resp)
}

// EtoroCandlesResponse is GET /api/ui/etoro/forward-tests/{book}/candles:
// the executor's latest <book>_daily_<date>.csv with the SMA50 (CandlePoint,
// as on the Bitso route).
type EtoroCandlesResponse struct {
	Ledger  string        `json:"ledger"`
	Venue   string        `json:"venue"`
	Book    string        `json:"book"`
	File    string        `json:"file"`
	Candles []CandlePoint `json:"candles"`
}

func (s *Server) etoroCandles(w http.ResponseWriter, r *http.Request) {
	l, by, ok := s.etoroLoad(w, r)
	if !ok {
		return
	}
	b, ok := etoroBookParam(w, r, by)
	if !ok {
		return
	}
	days := 180
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 5000 {
			writeErr(w, http.StatusBadRequest, "days must be 1..5000")
			return
		}
		days = n
	}
	rows, path, err := etoroCandleRows(l.Store, b)
	if errors.Is(err, fs.ErrNotExist) {
		writeErr(w, http.StatusNotFound, "no candle file for "+b+" in "+l.Store.Where(l.Store.CandlesDir))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, EtoroCandlesResponse{Ledger: l.Name, Venue: VenueEtoro, Book: b, File: filepath.Base(path), Candles: buildCandles(rows, days)})
}

// etoroCandleRows reads the newest candle file for book through the
// store (cached). Since 2026-10-19 store.ParseCandles reads eToro's empty
// volume as 0, so the former tolerant copy of the parser is gone.
func etoroCandleRows(st *store.Store, book string) ([]store.Candle, string, error) {
	return st.Candles(book)
}

// EtoroRiskResponse is GET /api/ui/etoro/risk.
type EtoroRiskResponse struct {
	Ledger      string      `json:"ledger"`
	Venue       string      `json:"venue"`
	GeneratedAt string      `json:"generated_at"`
	Policy      risk.Policy `json:"policy"`
	PolicySrc   string      `json:"policy_source"`
	Enforcement string      `json:"enforcement"`
	Note        string      `json:"note"`
	// Halted and HaltReason are the effective halt (the built-in policy
	// never halts, so the source is "none" or "file").
	Halted     bool         `json:"halted"`
	HaltReason string       `json:"halt_reason"`
	HaltSource string       `json:"halt_source"`
	HaltFile   HaltFileInfo `json:"halt_file"`
	// Account is the demo account as the newest run saw it (before its
	// order); AccountAsOf that run's recorded_at.
	Account     *etoroledger.Account `json:"account"`
	AccountAsOf string               `json:"account_as_of"`
	Books       []EtoroBookRisk      `json:"books"`
	Blocks      int                  `json:"blocks"`
	Warnings    int                  `json:"warnings"`
}

func (s *Server) etoroRisk(w http.ResponseWriter, r *http.Request) {
	l, by, ok := s.etoroLoad(w, r)
	if !ok {
		return
	}
	now := s.Now()
	pol, hf := s.etoroPolicyFor(l)
	src := "none"
	if hf.Halted {
		src = "file"
	}
	resp := EtoroRiskResponse{
		Ledger: l.Name, Venue: VenueEtoro, GeneratedAt: now.UTC().Format(time.RFC3339),
		Policy: etoroBasePolicy(l), PolicySrc: etoroPolicySrc(pol), Enforcement: "enforced",
		Note: "etoro-daily-executor runs shared/pkg/risk.Check with this built-in policy before every demo order and records " +
			"the result; a blocked order is recorded, not retried. Exposure here is units x the last close (the executor " +
			"uses the live quote). The halt file next to the ledger is the only operator override.",
		Halted: pol.Halted, HaltReason: pol.HaltReason, HaltSource: src, HaltFile: hf, Books: []EtoroBookRisk{},
	}
	for _, b := range etoroBooks(by) {
		if d := newestDemo(by[b]); d != nil && d.Demo.Account != nil && d.RecordedAt > resp.AccountAsOf {
			resp.Account, resp.AccountAsOf = d.Demo.Account, d.RecordedAt
		}
		br := buildEtoroBookRisk(pol, b, by[b], now)
		for _, f := range br.Findings {
			if f.Severity == risk.Block {
				resp.Blocks++
			} else {
				resp.Warnings++
			}
		}
		resp.Books = append(resp.Books, br)
	}
	writeJSON(w, http.StatusOK, resp)
}

// EtoroHealthResponse is GET /api/ui/etoro/health: etorohealth's checks (the
// ones ui-alerts evaluates every 15 minutes) in the data-health row shape.
type EtoroHealthResponse struct {
	Ledger      string            `json:"ledger"`
	Venue       string            `json:"venue"`
	GeneratedAt string            `json:"generated_at"`
	Status      string            `json:"status"` // worst of checks
	DueDate     string            `json:"due_date"`
	LedgerPath  string            `json:"ledger_path"`
	Checks      []HealthCheck     `json:"checks"`
	Thresholds  map[string]string `json:"thresholds"`
}

func (s *Server) etoroHealth(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pickEtoro(w, r)
	if !ok {
		return
	}
	now := s.Now()
	opts := etorohealth.Options{LedgerPath: l.Store.LedgerPath}
	resp := EtoroHealthResponse{Ledger: l.Name, Venue: VenueEtoro, GeneratedAt: now.UTC().Format(time.RFC3339),
		DueDate: opts.DueDate(now).Format(dateFmt), LedgerPath: l.Store.LedgerPath, Checks: []HealthCheck{},
		Thresholds: map[string]string{
			"run_at":        "09:35 New York",
			"due_by":        "10:00 New York",
			"coverage_days": "3",
			"intent_grace":  "10m0s",
		}}
	var all []string
	for _, c := range etorohealth.Evaluate(opts, now) {
		resp.Checks = append(resp.Checks, HealthCheck{ID: c.ID, Area: "executor", Label: c.Label, Status: c.Status, Message: c.Message})
		if c.Status != datahealth.Off {
			all = append(all, c.Status)
		}
	}
	resp.Status = datahealth.Worst(all...)
	writeJSON(w, http.StatusOK, resp)
}

// Reconcile reports as cmd/etoro-reconcile writes them (schema
// etoro-reconcile/v1). The types are local: the command's are private.

// EtoroReconcileAccount is the demo account at reconcile time.
type EtoroReconcileAccount struct {
	Currency      string  `json:"currency"`
	Cash          float64 `json:"cash"`
	Equity        float64 `json:"equity"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
}

// EtoroReconcileBook is one book's ledger-vs-account line.
type EtoroReconcileBook struct {
	Book             string   `json:"book"`
	Symbol           string   `json:"symbol"`
	InstrumentID     int64    `json:"instrument_id"`
	LastBarDate      string   `json:"last_bar_date"`
	LedgerState      string   `json:"ledger_state"`
	LedgerPositionID string   `json:"ledger_position_id,omitempty"`
	LedgerUnits      float64  `json:"ledger_units,omitempty"`
	AccountPositions []string `json:"account_positions"`
	FinancingToDate  float64  `json:"financing_to_date,omitempty"`
	OrdersChecked    int      `json:"orders_checked"`
}

// EtoroReconcileFinding is one finding; Severity is "drift" or "warn".
type EtoroReconcileFinding struct {
	Severity string `json:"severity"`
	Book     string `json:"book,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// etoroReconcileFile is the report file's content.
type etoroReconcileFile struct {
	Schema        string                  `json:"schema"`
	GeneratedAt   string                  `json:"generated_at"`
	Clean         bool                    `json:"clean"`
	Account       *EtoroReconcileAccount  `json:"account"`
	PositionReads int                     `json:"position_reads"`
	HistoryReads  int                     `json:"history_reads,omitempty"`
	SettleWaitedS float64                 `json:"settle_waited_s,omitempty"`
	Books         []EtoroReconcileBook    `json:"books"`
	Findings      []EtoroReconcileFinding `json:"findings"`
}

// EtoroRecheck is the operator's later read-only re-run of the same day
// (<date>.recheck.json, runbook §3).
type EtoroRecheck struct {
	Path        string                  `json:"path"`
	GeneratedAt string                  `json:"generated_at"`
	Clean       bool                    `json:"clean"`
	Findings    []EtoroReconcileFinding `json:"findings"`
	Error       string                  `json:"error,omitempty"`
}

// EtoroReconcileReport is one day's report.
type EtoroReconcileReport struct {
	Date string `json:"date"`
	Path string `json:"path"`
	etoroReconcileFile
	Drift   int           `json:"drift"`    // findings with severity drift
	Warn    int           `json:"warnings"` // the other findings
	Recheck *EtoroRecheck `json:"recheck"`
	// EffectiveClean: clean, or drift cleared by a clean re-check (the
	// rule etorohealth applies).
	EffectiveClean bool   `json:"effective_clean"`
	Error          string `json:"error,omitempty"` // the file could not be read
}

// EtoroReconcileResponse is GET /api/ui/etoro/reconcile.
type EtoroReconcileResponse struct {
	Ledger  string                 `json:"ledger"`
	Venue   string                 `json:"venue"`
	Dir     string                 `json:"dir"`
	Reports []EtoroReconcileReport `json:"reports"` // newest first
}

var reconcileDateRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.json$`)

func readReconcileFile(path string) (etoroReconcileFile, error) {
	var f etoroReconcileFile
	b, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, err
	}
	if f.Books == nil {
		f.Books = []EtoroReconcileBook{}
	}
	if f.Findings == nil {
		f.Findings = []EtoroReconcileFinding{}
	}
	return f, nil
}

func (s *Server) etoroReconcile(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pickEtoro(w, r)
	if !ok {
		return
	}
	dir := filepath.Join(filepath.Dir(l.Store.LedgerPath), "reconcile")
	resp := EtoroReconcileResponse{Ledger: l.Name, Venue: VenueEtoro, Dir: dir, Reports: []EtoroReconcileReport{}}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var dates []string
	rechecks := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if m := reconcileDateRe.FindStringSubmatch(n); m != nil {
			dates = append(dates, m[1])
		} else if strings.HasSuffix(n, ".recheck.json") {
			rechecks[strings.TrimSuffix(n, ".recheck.json")] = true
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	if len(dates) > etoroReportsShown {
		dates = dates[:etoroReportsShown]
	}
	for _, d := range dates {
		rep := EtoroReconcileReport{Date: d, Path: filepath.Join(dir, d+".json")}
		rep.etoroReconcileFile, err = readReconcileFile(rep.Path)
		if err != nil {
			rep.Error = err.Error()
			rep.Books, rep.Findings = []EtoroReconcileBook{}, []EtoroReconcileFinding{}
		}
		for _, f := range rep.Findings {
			if f.Severity == "drift" {
				rep.Drift++
			} else {
				rep.Warn++
			}
		}
		rep.EffectiveClean = err == nil && rep.Clean
		if rechecks[d] {
			rc := &EtoroRecheck{Path: filepath.Join(dir, d+".recheck.json")}
			if f, err := readReconcileFile(rc.Path); err != nil {
				rc.Error, rc.Findings = err.Error(), []EtoroReconcileFinding{}
			} else {
				rc.GeneratedAt, rc.Clean, rc.Findings = f.GeneratedAt, f.Clean, f.Findings
				if rc.Clean {
					rep.EffectiveClean = true
				}
			}
			rep.Recheck = rc
		}
		resp.Reports = append(resp.Reports, rep)
	}
	writeJSON(w, http.StatusOK, resp)
}

var riskReportDirRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// etoroRiskReport serves the newest <ledger dir>/risk/<date>/risk.json
// (cmd/etoro-risk-report, schema cfdrisk-report/v1, P4) as it is, with
// "date" and "path" added at the top level. 404 when none exists yet.
func (s *Server) etoroRiskReport(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pickEtoro(w, r)
	if !ok {
		return
	}
	dir := filepath.Join(filepath.Dir(l.Store.LedgerPath), "risk")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	newest := ""
	for _, e := range entries {
		if e.IsDir() && riskReportDirRe.MatchString(e.Name()) && e.Name() > newest {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), "risk.json")); err == nil {
				newest = e.Name()
			}
		}
	}
	if newest == "" {
		writeErr(w, http.StatusNotFound, "no risk report under "+dir+" yet (services/strategy-executor/cmd/etoro-risk-report)")
		return
	}
	path := filepath.Join(dir, newest, "risk.json")
	b, err := os.ReadFile(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(b, &doc); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("%s: %v", path, err))
		return
	}
	doc["date"], _ = json.Marshal(newest)
	doc["path"], _ = json.Marshal(path)
	writeJSON(w, http.StatusOK, doc)
}
