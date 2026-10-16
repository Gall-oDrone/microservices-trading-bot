package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"bitso-trading-platform/ui-api/internal/live"
)

// liveBooks parses ?books=a,b (default: every live book).
func (s *Server) liveBooks(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	return hubBooks(w, r, s.Live, bookRe, "live data is disabled (ui-api -live=false)")
}

// hubBooks is liveBooks for any hub: nil hub is a 503 with off as the
// reason; a book must match re and be one the hub tracks.
func hubBooks(w http.ResponseWriter, r *http.Request, hub *live.Hub, re *regexp.Regexp, off string) ([]string, bool) {
	if hub == nil {
		writeErr(w, http.StatusServiceUnavailable, off)
		return nil, false
	}
	var out []string
	if v := r.URL.Query().Get("books"); v != "" {
		for _, b := range strings.Split(v, ",") {
			b = strings.ToLower(strings.TrimSpace(b))
			if !re.MatchString(b) {
				writeErr(w, http.StatusBadRequest, "invalid book")
				return nil, false
			}
			if !hub.Has(b) {
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
	s.serveStream(w, r, s.Live, books, wantMarket(r), nil)
}

// serveStream writes hub events for books as SSE until the client goes
// away or the hub closes. wrap, when set, maps each event (the initial
// "snapshot" included) to the events actually sent, so a venue can add to
// the snapshot or follow a heartbeat with its own event.
func (s *Server) serveStream(w http.ResponseWriter, r *http.Request, hub *live.Hub, books []string, market bool, wrap func(live.Event) []live.Event) {
	if wrap == nil {
		wrap = func(ev live.Event) []live.Event { return []live.Event{ev} }
	}
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

	sub := hub.SubscribeWith(books, market)
	defer hub.Unsubscribe(sub)
	send := func(ev live.Event) error {
		for _, e := range wrap(ev) {
			b, err := json.Marshal(e.Data)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Name, b); err != nil {
				return err
			}
		}
		return rc.Flush()
	}
	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}
	if err := send(live.Event{Name: "snapshot", Data: hub.SnapshotWith(books, market)}); err != nil {
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
