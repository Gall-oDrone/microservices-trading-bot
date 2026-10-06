package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
	"bitso-trading-platform/ui-api/internal/live"
	"bitso-trading-platform/ui-api/internal/store"
)

type sseEvent struct{ name, data string }

func readEvent(t *testing.T, r *bufio.Reader) sseEvent {
	t.Helper()
	var ev sseEvent
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("stream ended: %v", err)
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case line == "" && ev.name != "":
			return ev
		case strings.HasPrefix(line, "event: "):
			ev.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			ev.data = strings.TrimPrefix(line, "data: ")
		}
	}
}

func TestStream(t *testing.T) {
	hub := live.NewHub([]string{"btc_mxn", "btc_usd"}, "test", nil, nil, log.New(io.Discard, "", 0))
	hub.Throttle = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	s := &Server{Store: store.New("testdata/ledger.jsonl", ""), Policy: risk.DefaultPolicy(), Version: "test",
		Log: log.New(io.Discard, "", 0), Live: hub}
	ts := httptest.NewUnstartedServer(s.Handler())
	ts.Config.WriteTimeout = 300 * time.Millisecond // the stream must outlive it
	ts.Start()
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/ui/stream?books=btc_usd")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	r := bufio.NewReader(res.Body)
	ev := readEvent(t, r)
	var snap live.Snapshot
	if ev.name != "snapshot" || json.Unmarshal([]byte(ev.data), &snap) != nil || len(snap.Books) != 1 || snap.Books[0].Book != "btc_usd" {
		t.Fatalf("first event: %+v", ev)
	}

	// Sleep past WriteTimeout; the stream must survive it.
	time.Sleep(400 * time.Millisecond)
	// btc_mxn is not subscribed, so only the btc_usd trade arrives.
	hub.OnTrade(live.Trade{Book: "btc_mxn", Price: 1, Amount: 1, At: time.Now()})
	hub.OnTrade(live.Trade{Book: "btc_usd", Price: 85000, Amount: 0.01, Side: "buy", At: time.Now()})
	ev = readEvent(t, r)
	var b live.BookSnapshot
	if ev.name != "book" || json.Unmarshal([]byte(ev.data), &b) != nil || b.Book != "btc_usd" || b.Last != 85000 || b.Candle == nil {
		t.Fatalf("book event: %+v", ev)
	}

	hub.OnDisconnect(io.EOF)
	if ev = readEvent(t, r); ev.name != "status" || !strings.Contains(ev.data, `"connected":false`) {
		t.Fatalf("status event: %+v", ev)
	}

	snapRes := get[live.Snapshot](t, ts, "/api/ui/live?books=btc_mxn,btc_usd", 200)
	if len(snapRes.Books) != 2 || snapRes.Books[1].Last != 85000 || snapRes.Upstream.Connected {
		t.Fatalf("snapshot %+v", snapRes)
	}
	get[errorBody](t, ts, "/api/ui/stream?books=eth_mxn", 404)
	get[errorBody](t, ts, "/api/ui/live?books=BTC*", 400)

	hub.Close() // server shutdown ends open streams
	if _, err := r.ReadString('\n'); err == nil {
		for {
			if _, err := r.ReadString('\n'); err != nil {
				break
			}
		}
	}
}

func TestLiveDisabled(t *testing.T) {
	ts := newTestServer(t, fixedNow, nil)
	get[errorBody](t, ts, "/api/ui/stream", 503)
	get[errorBody](t, ts, "/api/ui/live", 503)
}
