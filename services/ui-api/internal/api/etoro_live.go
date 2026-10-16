package api

// Live quotes for the eToro index CFDs (porting plan P5c, 2026-10-16):
// GET /api/ui/etoro/live and GET /api/ui/etoro/stream serve a second
// live.Hub (Server.EtoroLive) fed by internal/etorolive's rates poller, in
// the shapes of /live and /stream plus the NYSE session. The hub buckets
// the forming bar by UTC day, as eToro's OneDay bars and the executor's
// candle files do, and its provisional flip level comes from those files.
// Display only; nothing here reaches the executor.

import (
	"errors"
	"log"
	"net/http"
	"time"

	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/ui-api/internal/live"
	"bitso-trading-platform/ui-api/internal/store"
)

// etoroLiveOff is the 503 reason while the eToro feed is not running.
const etoroLiveOff = "eToro live quotes are off (no -etoro-ledger, no demo env file, or ui-api -etoro-live=false)"

// EtoroLiveSnapshot is GET /api/ui/etoro/live and the "snapshot" event of
// /api/ui/etoro/stream: the live snapshot with the NYSE session.
type EtoroLiveSnapshot struct {
	live.Snapshot
	Session EtoroSession `json:"session"`
}

func (s *Server) etoroLiveBooks(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	return hubBooks(w, r, s.EtoroLive, etoroBookRe, etoroLiveOff)
}

// etoroLiveSnapshot is GET /api/ui/etoro/live.
func (s *Server) etoroLiveSnapshot(w http.ResponseWriter, r *http.Request) {
	books, ok := s.etoroLiveBooks(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, EtoroLiveSnapshot{Snapshot: s.EtoroLive.SnapshotWith(books, wantMarket(r)), Session: etoroSession(s.Now())})
}

// etoroStream is GET /api/ui/etoro/stream: like /stream, with "session" in
// the snapshot and a "session" event after every heartbeat (15 s), so the
// page's open/closed badge follows the NYSE calendar without polling.
func (s *Server) etoroStream(w http.ResponseWriter, r *http.Request) {
	books, ok := s.etoroLiveBooks(w, r)
	if !ok {
		return
	}
	wrap := func(ev live.Event) []live.Event {
		switch ev.Name {
		case "snapshot":
			if snap, ok := ev.Data.(live.Snapshot); ok {
				return []live.Event{{Name: ev.Name, Data: EtoroLiveSnapshot{Snapshot: snap, Session: etoroSession(s.Now())}}}
			}
		case "heartbeat":
			return []live.Event{ev, {Name: "session", Data: etoroSession(s.Now())}}
		}
		return []live.Event{ev}
	}
	s.serveStream(w, r, s.EtoroLive, books, wantMarket(r), wrap)
}

// EtoroCloses is the live.Closes of an eToro ledger: the closes of the
// executor's newest candle file for the book (parseEtoroCandles) and the
// date of its last bar, read on every snapshot (the file is small).
func EtoroCloses(st *store.Store) live.Closes {
	return func(book string) ([]float64, string, error) {
		rows, _, err := etoroCandleRows(st, book)
		if err != nil {
			return nil, "", err
		}
		if len(rows) == 0 {
			return nil, "", errors.New("empty candle file")
		}
		cs := make([]float64, len(rows))
		for i, r := range rows {
			cs[i] = r.Close
		}
		return cs, rows[len(rows)-1].Date, nil
	}
}

// NewEtoroHub returns the hub for the eToro books: UTC-day buckets, the
// previous NYSE trading day as the bar before the forming one, quote-only
// (the mid is the price), closes from the ledger's candle files.
func NewEtoroHub(books []string, closes live.Closes, logger *log.Logger) *live.Hub {
	h := live.NewHubIn(time.UTC, books, "etoro", nil, closes, logger)
	h.QuoteOnly = true
	h.PrevBar = mktcal.PrevTradingDay
	return h
}
