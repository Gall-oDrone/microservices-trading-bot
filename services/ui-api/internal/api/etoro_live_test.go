package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/ui-api/internal/live"
	"bitso-trading-platform/ui-api/internal/store"
)

// etoroTestHub is the eToro hub over the testdata candle files (ending
// 2026-10-15, the trading day before etoroNow's forming UTC day), seeded
// with one rates poll through the Handler seam like the poller does.
func etoroTestHub(t *testing.T, now time.Time) *live.Hub {
	t.Helper()
	st := store.New(filepath.Join(etoroTestdata, "ledger.jsonl"), "")
	hub := NewEtoroHub([]string{"nsdq100", "spx500"}, EtoroCloses(st), log.New(io.Discard, "", 0))
	hub.Now = func() time.Time { return now }
	hub.Throttle = 20 * time.Millisecond
	hub.OnConnect()
	hub.OnTop(live.Top{Book: "nsdq100", Bid: 29191.9, Ask: 29192.6, At: now})
	hub.OnTop(live.Top{Book: "spx500", Bid: 7646.83, Ask: 7647.13, At: now})
	hub.OnMessage(now)
	return hub
}

func TestEtoroLiveSnapshot(t *testing.T) {
	hub := etoroTestHub(t, etoroNow)
	ts := etoroServer(t, etoroNow, func(s *Server) { s.EtoroLive = hub })

	snap := get[EtoroLiveSnapshot](t, ts, "/api/ui/etoro/live?books=nsdq100,spx500", 200)
	if snap.Upstream.Source != "etoro" || !snap.Upstream.Connected || len(snap.Books) != 2 {
		t.Fatalf("snapshot %+v", snap.Snapshot)
	}
	n := snap.Books[0]
	if n.Book != "nsdq100" || n.Bid != 29191.9 || n.Ask != 29192.6 || n.Last != 29192.25 || n.LastSide != "" {
		t.Fatalf("nsdq100 %+v", n)
	}
	// The forming bar is the UTC day (etoroNow is 17:00Z on 10-16; 12:00 in
	// Mexico City would give the same date here, so check the rule directly).
	if n.Candle == nil || n.Candle.Date != "2026-10-16" || n.Candle.Open != 29192.25 || n.Candle.TradeCount != 1 || n.Candle.Seeded {
		t.Fatalf("candle %+v", n.Candle)
	}
	// Flip level = mean of the last 49 closes of the testdata file (ends 10-15).
	p := n.Provisional
	if p == nil || p.BasedOn != "2026-10-15" || p.Signal != "flat" || p.FlipLevel < 29000 || p.FlipLevel > 31000 || p.Price != 29192.25 {
		t.Fatalf("provisional %+v (%s)", p, n.ProvisionalNote)
	}
	if s := snap.Books[1]; s.Book != "spx500" || s.Bid != 7646.83 || s.Provisional == nil || s.Provisional.BasedOn != "2026-10-15" {
		t.Fatalf("spx500 %+v (%s)", s, s.ProvisionalNote)
	}
	// The NYSE session (13:00 New York on a Friday: open until 16:00).
	if !snap.Session.Open || snap.Session.Date != "2026-10-16" || !strings.HasPrefix(snap.Session.ClosesAt, "2026-10-16T16:00:00") || !snap.Session.TradingDay {
		t.Fatalf("session %+v", snap.Session)
	}
	// Default is every book; ?market=1 adds mid/spread without depth or tape.
	all := get[EtoroLiveSnapshot](t, ts, "/api/ui/etoro/live?market=1", 200)
	if len(all.Books) != 2 || len(all.Markets) != 2 || all.Markets[0].Mid != 29192.25 || all.Markets[0].SpreadBps <= 0 || len(all.Markets[0].Trades) != 0 {
		t.Fatalf("market view %+v", all.Markets)
	}
	// Book validation: the eToro pattern, not Bitso's; unknown is a 404.
	get[errorBody](t, ts, "/api/ui/etoro/live?books=btc_mxn", 400)
	get[errorBody](t, ts, "/api/ui/etoro/live?books=NSDQ-100", 400)
	get[errorBody](t, ts, "/api/ui/etoro/live?books=dj30", 404)
	// The Bitso route does not know the eToro books (and is off here).
	get[errorBody](t, ts, "/api/ui/live?books=nsdq100", 503)
}

func TestEtoroLiveOff(t *testing.T) {
	ts := etoroServer(t, etoroNow, nil)
	e := get[errorBody](t, ts, "/api/ui/etoro/live", 503)
	if !strings.Contains(e.Error, "eToro live quotes are off") {
		t.Fatalf("error %+v", e)
	}
	get[errorBody](t, ts, "/api/ui/etoro/stream", 503)
	// The Bitso hub does not turn the eToro routes on.
	ts2 := etoroServer(t, etoroNow, func(s *Server) {
		s.Live = live.NewHub([]string{"btc_mxn"}, "test", nil, nil, log.New(io.Discard, "", 0))
	})
	get[errorBody](t, ts2, "/api/ui/etoro/live", 503)
}

func TestEtoroStream(t *testing.T) {
	hub := etoroTestHub(t, etoroNow)
	hub.Heartbeat = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)
	ts := etoroServer(t, etoroNow, func(s *Server) { s.EtoroLive = hub })

	res, err := http.Get(ts.URL + "/api/ui/etoro/stream?books=spx500")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	r := bufio.NewReader(res.Body)
	ev := readEvent(t, r)
	var snap EtoroLiveSnapshot
	if ev.name != "snapshot" || json.Unmarshal([]byte(ev.data), &snap) != nil {
		t.Fatalf("first event: %+v", ev)
	}
	if len(snap.Books) != 1 || snap.Books[0].Book != "spx500" || snap.Books[0].Ask != 7647.13 || !snap.Session.Open || snap.Session.ClosesAt == "" {
		t.Fatalf("snapshot %+v session %+v", snap.Books, snap.Session)
	}
	// The next poll: a "book" event for the subscribed book only.
	hub.OnTop(live.Top{Book: "nsdq100", Bid: 1, Ask: 2, At: etoroNow.Add(5 * time.Second)})
	hub.OnTop(live.Top{Book: "spx500", Bid: 7650, Ask: 7650.3, At: etoroNow.Add(5 * time.Second)})
	ev = readEvent(t, r)
	var b live.BookSnapshot
	if ev.name != "book" || json.Unmarshal([]byte(ev.data), &b) != nil || b.Book != "spx500" || b.Bid != 7650 || b.Last != 7650.15 || b.Candle.TradeCount != 2 {
		t.Fatalf("book event: %+v", ev)
	}
	// A heartbeat is followed by a "session" event.
	seen := map[string]int{}
	for i := 0; i < 6 && (seen["heartbeat"] == 0 || seen["session"] == 0); i++ {
		ev = readEvent(t, r)
		seen[ev.name]++
		if ev.name == "session" {
			var s EtoroSession
			if json.Unmarshal([]byte(ev.data), &s) != nil || !s.Open || s.Date != "2026-10-16" {
				t.Fatalf("session event: %+v", ev)
			}
		}
	}
	if seen["heartbeat"] == 0 || seen["session"] == 0 {
		t.Fatalf("events seen: %v", seen)
	}
	// A poll error is a status event, like the Bitso feed's disconnect.
	hub.OnDisconnect(io.ErrUnexpectedEOF)
	for i := 0; i < 4; i++ {
		if ev = readEvent(t, r); ev.name == "status" {
			break
		}
	}
	if ev.name != "status" || !strings.Contains(ev.data, `"connected":false`) || !strings.Contains(ev.data, "unexpected EOF") {
		t.Fatalf("status event: %+v", ev)
	}
}
