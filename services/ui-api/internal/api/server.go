// Package api serves the JSON API for the web UI. It is read-only except for
// the operator controls (controls.go: halt/resume, token-gated and audited).
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/live"
	"bitso-trading-platform/ui-api/internal/objstore"
	"bitso-trading-platform/ui-api/internal/research"
	"bitso-trading-platform/ui-api/internal/store"
)

// Ledger is one named daily-executor ledger (with its own candles dir).
type Ledger struct {
	Name  string
	Store *store.Store
}

// Server holds the API's dependencies.
type Server struct {
	// Ledgers are served by name; the first is the default. When empty, Store
	// is served as the single ledger "stage".
	Ledgers   []Ledger
	Store     *store.Store
	Policy    risk.Policy
	PolicySrc string // file path or "built-in default"
	StageSize float64
	StaticDir string // optional built web/dist
	Version   string
	Now       func() time.Time
	Log       *log.Logger
	// Live is the display-only market data hub; nil disables /live and /stream.
	Live *live.Hub
	// Research indexes the study write-ups; nil serves an empty list.
	Research *research.Index
	// Archive is the collector's S3 trade archive (read-only) for
	// /health/data; nil reports it as not configured.
	Archive      objstore.Store
	archiveCache archiveCache
	// OperatorToken enables the operator controls (R4) when set; empty keeps
	// the API read-only. See controls.go.
	OperatorToken string
	controlsMu    sync.Mutex
}

var (
	bookRe       = regexp.MustCompile(`^[a-z]{2,6}_[a-z]{2,6}$`)
	ledgerNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
)

// ParseLedgers parses "name=path,name=path". Names must be unique and match
// ledgerNameRe; each ledger's candles are read from <ledger dir>/candles. A
// path may be s3://bucket/prefix: the copy scripts/daily-executor-run.sh
// uploads (read with the default AWS credentials, list and get only).
func ParseLedgers(spec string) ([]Ledger, error) {
	return ParseLedgersWith(spec, nil)
}

// OpenS3 opens a bucket; ParseLedgersWith calls it once per bucket.
type OpenS3 func(bucket string) (objstore.Store, error)

// ParseLedgersWith is ParseLedgers with the S3 opener given (nil: the
// default AWS chain).
func ParseLedgersWith(spec string, open OpenS3) ([]Ledger, error) {
	open = cachedOpener(open)
	var out []Ledger
	seen := map[string]bool{}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, path, ok := strings.Cut(part, "=")
		name, path = strings.TrimSpace(name), strings.TrimSpace(path)
		if !ok || path == "" {
			return nil, fmt.Errorf("ledger %q: want name=path", part)
		}
		if !ledgerNameRe.MatchString(name) {
			return nil, fmt.Errorf("ledger name %q: use lowercase letters, digits, '-' or '_' (max 32)", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("ledger name %q given twice", name)
		}
		seen[name] = true
		st, err := OpenStore(path, "", open)
		if err != nil {
			return nil, fmt.Errorf("ledger %s: %w", name, err)
		}
		out = append(out, Ledger{Name: name, Store: st})
	}
	if len(out) == 0 {
		return nil, errors.New("no ledgers")
	}
	return out, nil
}

// OpenStore opens a ledger given as a path on disk or as s3://bucket/prefix
// (candlesDir applies to disk only; in S3 the candles are <prefix>/candles).
func OpenStore(path, candlesDir string, open OpenS3) (*store.Store, error) {
	if !strings.HasPrefix(path, "s3://") {
		return store.New(path, candlesDir), nil
	}
	bucket, prefix, err := objstore.ParseURI(path)
	if err != nil {
		return nil, err
	}
	if candlesDir != "" {
		return nil, errors.New("a separate candles dir is not supported for an S3 ledger (they are read from <prefix>/candles)")
	}
	obj, err := cachedOpener(open)(bucket)
	if err != nil {
		return nil, err
	}
	return store.NewRemote(obj, prefix), nil
}

// cachedOpener shares one client per bucket.
func cachedOpener(open OpenS3) OpenS3 {
	if open == nil {
		open = func(bucket string) (objstore.Store, error) { return objstore.NewS3(context.Background(), bucket) }
	}
	var mu sync.Mutex
	buckets := map[string]objstore.Store{}
	return func(bucket string) (objstore.Store, error) {
		mu.Lock()
		defer mu.Unlock()
		if o, ok := buckets[bucket]; ok {
			return o, nil
		}
		o, err := open(bucket)
		if err != nil {
			return nil, err
		}
		buckets[bucket] = o
		return o, nil
	}
}

func (s *Server) ledgers() []Ledger {
	if len(s.Ledgers) > 0 {
		return s.Ledgers
	}
	return []Ledger{{Name: "stage", Store: s.Store}}
}

// pick returns the ledger named by ?ledger= (default: the first).
func (s *Server) pick(w http.ResponseWriter, r *http.Request) (Ledger, bool) {
	ls := s.ledgers()
	name := r.URL.Query().Get("ledger")
	if name == "" {
		return ls[0], true
	}
	if !ledgerNameRe.MatchString(name) {
		writeErr(w, http.StatusBadRequest, "invalid ledger name")
		return Ledger{}, false
	}
	for _, l := range ls {
		if l.Name == name {
			return l, true
		}
	}
	writeErr(w, http.StatusBadRequest, "unknown ledger "+name)
	return Ledger{}, false
}

// Handler returns the HTTP handler with every route.
func (s *Server) Handler() http.Handler {
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.Log == nil {
		s.Log = log.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/ui/healthz", s.healthz)
	mux.HandleFunc("GET /api/ui/ledgers", s.listLedgers)
	mux.HandleFunc("GET /api/ui/forward-tests", s.forwardTests)
	mux.HandleFunc("GET /api/ui/forward-tests/{book}", s.forwardTest)
	mux.HandleFunc("GET /api/ui/forward-tests/{book}/ledger", s.ledger)
	mux.HandleFunc("GET /api/ui/forward-tests/{book}/candles", s.candles)
	mux.HandleFunc("GET /api/ui/risk", s.riskStatus)
	mux.HandleFunc("GET /api/ui/live", s.liveSnapshot)
	mux.HandleFunc("GET /api/ui/stream", s.stream)
	mux.HandleFunc("GET /api/ui/research/studies", s.studies)
	mux.HandleFunc("GET /api/ui/research/studies/{name}", s.study)
	mux.HandleFunc("GET /api/ui/research/runs", s.runs)
	mux.HandleFunc("GET /api/ui/research/runs/{date}/{name}", s.run)
	mux.HandleFunc("GET /api/ui/health/data", s.dataHealth)
	mux.HandleFunc("GET /api/ui/controls", s.controls)
	mux.HandleFunc("POST /api/ui/risk/halt", s.haltAction)
	mux.HandleFunc("POST /api/ui/risk/resume", s.resumeAction)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "no such endpoint")
	})
	if s.StaticDir != "" {
		mux.Handle("/", spa(s.StaticDir))
	}
	return s.middleware(mux)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet && r.Method != http.MethodHead &&
			!(r.Method == http.MethodPost && controlPaths[r.URL.Path]) {
			// Read-only API: only the audited operator controls may change
			// trading state.
			writeErr(w, http.StatusMethodNotAllowed, "read-only API")
			return
		}
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.Log.Printf("%s %s %s", r.Method, r.URL.RequestURI(), time.Since(start).Round(time.Microsecond))
		}
	})
}

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

// books is every book with a frozen spec plus any book found in the ledger.
func (s *Server) books(by map[string][]dailyledger.Record) []string {
	set := map[string]bool{}
	for b := range PreregDates {
		set[b] = true
	}
	for b := range by {
		set[b] = true
	}
	out := make([]string, 0, len(set))
	for b := range set {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

func (s *Server) load(w http.ResponseWriter, l Ledger) (map[string][]dailyledger.Record, bool) {
	recs, err := l.Store.Records()
	if err != nil {
		s.Log.Printf("ledger %s: %v", l.Name, err)
		writeErr(w, http.StatusInternalServerError, "cannot read the "+l.Name+" ledger: "+err.Error())
		return nil, false
	}
	return dailyledger.ByBook(recs), true
}

func (s *Server) bookParam(w http.ResponseWriter, r *http.Request, by map[string][]dailyledger.Record) (string, bool) {
	b := strings.ToLower(r.PathValue("book"))
	if !bookRe.MatchString(b) {
		writeErr(w, http.StatusBadRequest, "invalid book")
		return "", false
	}
	for _, k := range s.books(by) {
		if k == b {
			return b, true
		}
	}
	writeErr(w, http.StatusNotFound, "unknown book "+b)
	return "", false
}

// Health is the /healthz body (about the default ledger).
type Health struct {
	Status      string `json:"status"`
	Version     string `json:"version"`
	Ledger      string `json:"ledger"`
	LedgerFound bool   `json:"ledger_found"`
	Records     int    `json:"records"`
	Ledgers     int    `json:"ledgers"`
	Policy      string `json:"policy"`
	Time        string `json:"time"`
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ls := s.ledgers()
	st := ls[0].Store
	recs, err := st.Records()
	_, found, statErr := st.LedgerInfo()
	h := Health{Status: "ok", Version: s.Version, Ledger: st.Where(st.LedgerPath), LedgerFound: found,
		Records: len(recs), Ledgers: len(ls), Policy: s.PolicySrc, Time: s.Now().UTC().Format(time.RFC3339)}
	if err != nil || statErr != nil {
		h.Status = "degraded"
	}
	writeJSON(w, http.StatusOK, h)
}

// LedgerInfo describes one configured ledger.
type LedgerInfo struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	Default     bool     `json:"default"`
	Found       bool     `json:"found"`
	Records     int      `json:"records"`
	Modes       []string `json:"modes"`
	LastBarDate string   `json:"last_bar_date"`
	Error       string   `json:"error,omitempty"`
}

// LedgersResponse is GET /api/ui/ledgers.
type LedgersResponse struct {
	Ledgers []LedgerInfo `json:"ledgers"`
}

func (s *Server) listLedgers(w http.ResponseWriter, r *http.Request) {
	resp := LedgersResponse{Ledgers: []LedgerInfo{}}
	for i, l := range s.ledgers() {
		info := LedgerInfo{Name: l.Name, Path: l.Store.Where(l.Store.LedgerPath), Default: i == 0, Modes: []string{}}
		_, info.Found, _ = l.Store.LedgerInfo()
		recs, err := l.Store.Records()
		if err != nil {
			info.Error = err.Error()
		}
		info.Records = len(recs)
		modes := map[string]bool{}
		for _, rec := range recs {
			if rec.Mode != "" && !modes[rec.Mode] {
				modes[rec.Mode] = true
				info.Modes = append(info.Modes, rec.Mode)
			}
			if rec.Decision.BarDate > info.LastBarDate {
				info.LastBarDate = rec.Decision.BarDate
			}
		}
		sort.Strings(info.Modes)
		resp.Ledgers = append(resp.Ledgers, info)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ForwardTestsResponse is GET /api/ui/forward-tests.
type ForwardTestsResponse struct {
	Ledger      string        `json:"ledger"`
	GeneratedAt string        `json:"generated_at"`
	Books       []ForwardTest `json:"books"`
}

// HaltFileInfo is the operator halt file next to a ledger (R2), read-only.
type HaltFileInfo struct {
	Path   string `json:"path"`
	Found  bool   `json:"found"`
	Halted bool   `json:"halted"`
	Reason string `json:"reason"`
	By     string `json:"by"`
	At     string `json:"at"`
	// Error is set when the file exists but is invalid: the executor then
	// refuses to run (exit 2) until it is fixed or removed.
	Error string `json:"error,omitempty"`
}

// policyFor is the policy as the executor would apply it to this ledger: the
// configured policy with the ledger's halt file merged in.
func (s *Server) policyFor(l Ledger) (risk.Policy, HaltFileInfo) {
	info := HaltFileInfo{Path: l.Store.Where(l.Store.HaltPath())}
	h, found, err := l.Store.Halt()
	info.Found = found
	if err != nil {
		info.Error = err.Error()
		return s.Policy, info
	}
	info.Halted, info.Reason, info.By, info.At = h.Halted, h.Reason, h.By, h.At
	return risk.ApplyHalt(s.Policy, h), info
}

func (s *Server) riskFor(l Ledger, pol risk.Policy, book string, recs []dailyledger.Record, now time.Time) (BookRisk, []Fill) {
	rows, _, _ := l.Store.Candles(book) // optional: slippage needs the fill day's open
	fills := buildFills(book, recs, rows)
	return buildBookRisk(pol, book, recs, fills, now, s.StageSize), fills
}

func (s *Server) forwardTests(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pick(w, r)
	if !ok {
		return
	}
	by, ok := s.load(w, l)
	if !ok {
		return
	}
	now := s.Now()
	resp := ForwardTestsResponse{Ledger: l.Name, GeneratedAt: now.UTC().Format(time.RFC3339), Books: []ForwardTest{}}
	pol, _ := s.policyFor(l)
	for _, b := range s.books(by) {
		br, _ := s.riskFor(l, pol, b, by[b], now)
		ft := buildForwardTest(b, by[b], now, br)
		ft.Ledger = l.Name
		resp.Books = append(resp.Books, ft)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) forwardTest(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pick(w, r)
	if !ok {
		return
	}
	by, ok := s.load(w, l)
	if !ok {
		return
	}
	b, ok := s.bookParam(w, r, by)
	if !ok {
		return
	}
	now := s.Now()
	pol, _ := s.policyFor(l)
	br, _ := s.riskFor(l, pol, b, by[b], now)
	ft := buildForwardTest(b, by[b], now, br)
	ft.Ledger = l.Name
	writeJSON(w, http.StatusOK, ft)
}

// LedgerResponse is GET /api/ui/forward-tests/{book}/ledger.
type LedgerResponse struct {
	Ledger  string               `json:"ledger"`
	Book    string               `json:"book"`
	Mode    string               `json:"mode"`
	Records []dailyledger.Record `json:"records"`
	Equity  []EquityPoint        `json:"equity"`
	Fills   []Fill               `json:"fills"`
}

func (s *Server) ledger(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pick(w, r)
	if !ok {
		return
	}
	by, ok := s.load(w, l)
	if !ok {
		return
	}
	b, ok := s.bookParam(w, r, by)
	if !ok {
		return
	}
	recs := by[b]
	if mode := r.URL.Query().Get("mode"); mode != "" {
		var f []dailyledger.Record
		for _, x := range recs {
			if x.Mode == mode {
				f = append(f, x)
			}
		}
		recs = f
	}
	if recs == nil {
		recs = []dailyledger.Record{}
	}
	rows, _, _ := l.Store.Candles(b)
	resp := LedgerResponse{Ledger: l.Name, Book: b, Records: recs, Equity: buildEquity(recs), Fills: buildFills(b, recs, rows)}
	if len(recs) > 0 {
		resp.Mode = recs[len(recs)-1].Mode
	}
	writeJSON(w, http.StatusOK, resp)
}

// CandlesResponse is GET /api/ui/forward-tests/{book}/candles.
type CandlesResponse struct {
	Ledger  string        `json:"ledger"`
	Book    string        `json:"book"`
	File    string        `json:"file"`
	Candles []CandlePoint `json:"candles"`
}

func (s *Server) candles(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pick(w, r)
	if !ok {
		return
	}
	by, ok := s.load(w, l)
	if !ok {
		return
	}
	b, ok := s.bookParam(w, r, by)
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
	rows, path, err := l.Store.Candles(b)
	if errors.Is(err, os.ErrNotExist) {
		writeErr(w, http.StatusNotFound, "no candle file for "+b+" in "+l.Store.Where(l.Store.CandlesDir))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, CandlesResponse{Ledger: l.Name, Book: b, File: filepath.Base(path), Candles: buildCandles(rows, days)})
}

// RiskResponse is GET /api/ui/risk.
type RiskResponse struct {
	Ledger      string      `json:"ledger"`
	GeneratedAt string      `json:"generated_at"`
	Policy      risk.Policy `json:"policy"`
	PolicySrc   string      `json:"policy_source"`
	StageSize   float64     `json:"stage_size_btc"`
	Enforcement string      `json:"enforcement"`
	Note        string      `json:"note"`
	// Halted and HaltReason are the effective halt (policy or halt file).
	Halted     bool         `json:"halted"`
	HaltReason string       `json:"halt_reason"`
	HaltSource string       `json:"halt_source"` // none | policy | file | both
	HaltFile   HaltFileInfo `json:"halt_file"`
	Books      []BookRisk   `json:"books"`
	Blocks     int          `json:"blocks"`
	Warnings   int          `json:"warnings"`
}

func (s *Server) riskStatus(w http.ResponseWriter, r *http.Request) {
	l, ok := s.pick(w, r)
	if !ok {
		return
	}
	by, ok := s.load(w, l)
	if !ok {
		return
	}
	now := s.Now()
	pol, hf := s.policyFor(l)
	src := "none"
	switch {
	case s.Policy.Halted && hf.Halted:
		src = "both"
	case s.Policy.Halted:
		src = "policy"
	case hf.Halted:
		src = "file"
	}
	resp := RiskResponse{
		Ledger:      l.Name,
		GeneratedAt: now.UTC().Format(time.RFC3339), Policy: s.Policy, PolicySrc: s.PolicySrc, StageSize: s.StageSize,
		Enforcement: "enforced",
		Note: "The daily-executor runs shared/pkg/risk.Check before every stage order and records the result; " +
			"a blocked order is skipped and recorded, not retried. Next-order previews here use the last close as the price " +
			"(the executor uses the live best bid/ask). Run ui-api with the same -risk-policy as the executor.",
		Halted: pol.Halted, HaltReason: pol.HaltReason, HaltSource: src, HaltFile: hf, Books: []BookRisk{},
	}
	for _, b := range s.books(by) {
		br, _ := s.riskFor(l, pol, b, by[b], now)
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

// spa serves the built web app, falling back to index.html for client routes.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Clean("/" + r.URL.Path)
		if fi, err := os.Stat(filepath.Join(dir, p)); err == nil && !fi.IsDir() {
			if strings.HasPrefix(p, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "static: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}
