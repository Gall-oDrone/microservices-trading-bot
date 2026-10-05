// Package api serves the read-only JSON API for the web UI.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/dailyledger"
	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/store"
)

// Server holds the API's dependencies.
type Server struct {
	Store     *store.Store
	Policy    risk.Policy
	PolicySrc string // file path or "built-in default"
	StageSize float64
	StaticDir string // optional built web/dist
	Version   string
	Now       func() time.Time
	Log       *log.Logger
}

var bookRe = regexp.MustCompile(`^[a-z]{2,6}_[a-z]{2,6}$`)

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
	mux.HandleFunc("GET /api/ui/forward-tests", s.forwardTests)
	mux.HandleFunc("GET /api/ui/forward-tests/{book}", s.forwardTest)
	mux.HandleFunc("GET /api/ui/forward-tests/{book}/ledger", s.ledger)
	mux.HandleFunc("GET /api/ui/forward-tests/{book}/candles", s.candles)
	mux.HandleFunc("GET /api/ui/risk", s.riskStatus)
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
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// Read-only API: nothing here may change trading state.
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

func (s *Server) load(w http.ResponseWriter) (map[string][]dailyledger.Record, bool) {
	recs, err := s.Store.Records()
	if err != nil {
		s.Log.Printf("ledger: %v", err)
		writeErr(w, http.StatusInternalServerError, "cannot read the ledger: "+err.Error())
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

// Health is the /healthz body.
type Health struct {
	Status      string `json:"status"`
	Version     string `json:"version"`
	Ledger      string `json:"ledger"`
	LedgerFound bool   `json:"ledger_found"`
	Records     int    `json:"records"`
	Policy      string `json:"policy"`
	Time        string `json:"time"`
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	recs, err := s.Store.Records()
	_, statErr := os.Stat(s.Store.LedgerPath)
	h := Health{Status: "ok", Version: s.Version, Ledger: s.Store.LedgerPath, LedgerFound: statErr == nil,
		Records: len(recs), Policy: s.PolicySrc, Time: s.Now().UTC().Format(time.RFC3339)}
	if err != nil {
		h.Status = "degraded"
	}
	writeJSON(w, http.StatusOK, h)
}

// ForwardTestsResponse is GET /api/ui/forward-tests.
type ForwardTestsResponse struct {
	GeneratedAt string        `json:"generated_at"`
	Books       []ForwardTest `json:"books"`
}

func (s *Server) riskFor(book string, recs []dailyledger.Record, now time.Time) (BookRisk, []Fill) {
	rows, _, _ := s.Store.Candles(book) // optional: slippage needs the fill day's open
	fills := buildFills(book, recs, rows)
	return buildBookRisk(s.Policy, book, recs, fills, now, s.StageSize), fills
}

func (s *Server) forwardTests(w http.ResponseWriter, r *http.Request) {
	by, ok := s.load(w)
	if !ok {
		return
	}
	now := s.Now()
	resp := ForwardTestsResponse{GeneratedAt: now.UTC().Format(time.RFC3339), Books: []ForwardTest{}}
	for _, b := range s.books(by) {
		br, _ := s.riskFor(b, by[b], now)
		resp.Books = append(resp.Books, buildForwardTest(b, by[b], now, br))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) forwardTest(w http.ResponseWriter, r *http.Request) {
	by, ok := s.load(w)
	if !ok {
		return
	}
	b, ok := s.bookParam(w, r, by)
	if !ok {
		return
	}
	now := s.Now()
	br, _ := s.riskFor(b, by[b], now)
	writeJSON(w, http.StatusOK, buildForwardTest(b, by[b], now, br))
}

// LedgerResponse is GET /api/ui/forward-tests/{book}/ledger.
type LedgerResponse struct {
	Book    string               `json:"book"`
	Mode    string               `json:"mode"`
	Records []dailyledger.Record `json:"records"`
	Equity  []EquityPoint        `json:"equity"`
	Fills   []Fill               `json:"fills"`
}

func (s *Server) ledger(w http.ResponseWriter, r *http.Request) {
	by, ok := s.load(w)
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
	rows, _, _ := s.Store.Candles(b)
	resp := LedgerResponse{Book: b, Records: recs, Equity: buildEquity(recs), Fills: buildFills(b, recs, rows)}
	if len(recs) > 0 {
		resp.Mode = recs[len(recs)-1].Mode
	}
	writeJSON(w, http.StatusOK, resp)
}

// CandlesResponse is GET /api/ui/forward-tests/{book}/candles.
type CandlesResponse struct {
	Book    string        `json:"book"`
	File    string        `json:"file"`
	Candles []CandlePoint `json:"candles"`
}

func (s *Server) candles(w http.ResponseWriter, r *http.Request) {
	by, ok := s.load(w)
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
	rows, path, err := s.Store.Candles(b)
	if errors.Is(err, os.ErrNotExist) {
		writeErr(w, http.StatusNotFound, "no candle file for "+b+" in "+s.Store.CandlesDir)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, CandlesResponse{Book: b, File: filepath.Base(path), Candles: buildCandles(rows, days)})
}

// RiskResponse is GET /api/ui/risk.
type RiskResponse struct {
	GeneratedAt string      `json:"generated_at"`
	Policy      risk.Policy `json:"policy"`
	PolicySrc   string      `json:"policy_source"`
	StageSize   float64     `json:"stage_size_btc"`
	Enforcement string      `json:"enforcement"`
	Note        string      `json:"note"`
	Halted      bool        `json:"halted"`
	HaltReason  string      `json:"halt_reason"`
	Books       []BookRisk  `json:"books"`
	Blocks      int         `json:"blocks"`
	Warnings    int         `json:"warnings"`
}

func (s *Server) riskStatus(w http.ResponseWriter, r *http.Request) {
	by, ok := s.load(w)
	if !ok {
		return
	}
	now := s.Now()
	resp := RiskResponse{
		GeneratedAt: now.UTC().Format(time.RFC3339), Policy: s.Policy, PolicySrc: s.PolicySrc, StageSize: s.StageSize,
		Enforcement: "enforced",
		Note: "The daily-executor runs shared/pkg/risk.Check before every stage order and records the result; " +
			"a blocked order is skipped and recorded, not retried. Next-order previews here use the last close as the price " +
			"(the executor uses the live best bid/ask). Run ui-api with the same -risk-policy as the executor.",
		Halted: s.Policy.Halted, HaltReason: s.Policy.HaltReason, Books: []BookRisk{},
	}
	for _, b := range s.books(by) {
		br, _ := s.riskFor(b, by[b], now)
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
