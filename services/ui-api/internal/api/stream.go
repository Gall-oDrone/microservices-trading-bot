package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"bitso-trading-platform/ui-api/internal/live"
)

// liveBooks parses ?books=a,b (default: every live book).
func (s *Server) liveBooks(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	if s.Live == nil {
		writeErr(w, http.StatusServiceUnavailable, "live data is disabled (ui-api -live=false)")
		return nil, false
	}
	var out []string
	if v := r.URL.Query().Get("books"); v != "" {
		for _, b := range strings.Split(v, ",") {
			b = strings.ToLower(strings.TrimSpace(b))
			if !bookRe.MatchString(b) {
				writeErr(w, http.StatusBadRequest, "invalid book")
				return nil, false
			}
			if !s.Live.Has(b) {
				writeErr(w, http.StatusNotFound, "no live data for "+b)
				return nil, false
			}
			out = append(out, b)
		}
	}
	return out, true
}

// wantMarket is ?market=1: also the Market page's depth and tape.
func wantMarket(r *http.Request) bool {
	v := r.URL.Query().Get("market")
	return v == "1" || v == "true"
}

// liveSnapshot is GET /api/ui/live: the same state as the stream, as JSON
// (REST fallback and first paint). ?market=1 adds "markets".
func (s *Server) liveSnapshot(w http.ResponseWriter, r *http.Request) {
	books, ok := s.liveBooks(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.Live.SnapshotWith(books, wantMarket(r)))
}

// stream is GET /api/ui/stream: Server-Sent Events with a "snapshot" on
// connect, then throttled "book" updates, "status" changes and a
// "heartbeat" every 15 s. With ?market=1 the snapshot has "markets" and a
// throttled "market" event (spread, top levels, tape) follows each "book".
// Display only: nothing here reaches the executor.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	books, ok := s.liveBooks(w, r)
	if !ok {
		return
	}
	market := wantMarket(r)
	rc := http.NewResponseController(w)
	// The server's WriteTimeout would cut a long-lived stream.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.Log.Printf("stream: clearing write deadline: %v", err)
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	sub := s.Live.SubscribeWith(books, market)
	defer s.Live.Unsubscribe(sub)
	send := func(ev live.Event) error {
		b, err := json.Marshal(ev.Data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, b); err != nil {
			return err
		}
		return rc.Flush()
	}
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}
	if err := send(live.Event{Name: "snapshot", Data: s.Live.SnapshotWith(books, market)}); err != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			if err := send(ev); err != nil {
				return
			}
		}
	}
}
