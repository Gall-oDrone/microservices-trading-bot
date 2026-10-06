package live

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeBitso is a stand-in for wss://ws.bitso.com. Each connection runs the
// next script in order; a script returning closes the connection.
type fakeBitso struct {
	t       *testing.T
	mu      sync.Mutex
	scripts []func(c *websocket.Conn, subs []map[string]string)
	conns   int
}

func (f *fakeBitso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{}
	c, err := up.Upgrade(w, r, nil)
	if err != nil {
		f.t.Errorf("upgrade: %v", err)
		return
	}
	defer c.Close()
	f.mu.Lock()
	i := f.conns
	f.conns++
	f.mu.Unlock()
	// Read the 4 subscriptions (2 books x trades, orders) and ack them.
	var subs []map[string]string
	for len(subs) < 4 {
		var m map[string]string
		if err := c.ReadJSON(&m); err != nil {
			return
		}
		subs = append(subs, m)
		_ = c.WriteJSON(map[string]any{"action": "subscribe", "response": "ok", "time": 1, "type": m["type"]})
	}
	if i < len(f.scripts) {
		f.scripts[i](c, subs)
		return
	}
	// Later connections stay open and quiet (with keep-alives) until closed.
	for {
		if err := c.WriteJSON(map[string]string{"type": "ka"}); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type recorder struct {
	mu          sync.Mutex
	connects    int
	disconnects []string
	trades      []Trade
	tops        []Top
	messages    int
}

func (r *recorder) OnConnect()          { r.mu.Lock(); r.connects++; r.mu.Unlock() }
func (r *recorder) OnMessage(time.Time) { r.mu.Lock(); r.messages++; r.mu.Unlock() }
func (r *recorder) OnTrade(t Trade)     { r.mu.Lock(); r.trades = append(r.trades, t); r.mu.Unlock() }
func (r *recorder) OnTop(t Top)         { r.mu.Lock(); r.tops = append(r.tops, t); r.mu.Unlock() }
func (r *recorder) OnDisconnect(err error) {
	r.mu.Lock()
	r.disconnects = append(r.disconnects, err.Error())
	r.mu.Unlock()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestFeedParsesSubscribesAndReconnects(t *testing.T) {
	trades := `{"type":"trades","book":"btc_mxn","payload":[{"i":77777,"a":"0.0035","r":"1510000","v":"5285","mo":"m","to":"t","t":1,"x":1791100000000}],"sent":1791100000000}`
	orders := `{"type":"orders","book":"btc_usd","payload":{"bids":[{"o":"a","r":"84990","a":"0.1","v":"1","t":0,"d":1,"s":"undefined"},{"o":"b","r":"84995","a":"0.1","v":"1","t":0,"d":1}],` +
		`"asks":[{"o":"c","r":"85020","a":"0.1","v":"1","t":1,"d":1},{"o":"d","r":"85010","a":"0.1","v":"1","t":1,"d":1}]},"sent":1791100000500}`
	fb := &fakeBitso{t: t}
	var gotSubs []map[string]string
	fb.scripts = []func(*websocket.Conn, []map[string]string){
		func(c *websocket.Conn, subs []map[string]string) {
			gotSubs = subs
			_ = c.WriteMessage(websocket.TextMessage, []byte(`{"type":"ka"}`))
			_ = c.WriteMessage(websocket.TextMessage, []byte(trades))
			_ = c.WriteMessage(websocket.TextMessage, []byte(orders))
			_ = c.WriteMessage(websocket.TextMessage, []byte(`not json`))
			// then drop the connection: the feed must reconnect
		},
	}
	srv := httptest.NewServer(fb)
	defer srv.Close()

	rec := &recorder{}
	f := &Feed{URL: "ws" + strings.TrimPrefix(srv.URL, "http"), Books: []string{"btc_mxn", "btc_usd"}, Handler: rec,
		Log: log.New(io.Discard, "", 0), MinBackoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond, ReadTimeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.Run(ctx); close(done) }()

	waitFor(t, "a reconnect", func() bool { rec.mu.Lock(); defer rec.mu.Unlock(); return rec.connects >= 2 })
	cancel()
	<-done

	if len(gotSubs) != 4 || gotSubs[0]["action"] != "subscribe" || gotSubs[0]["book"] != "btc_mxn" || gotSubs[1]["type"] != "orders" {
		t.Fatalf("subscriptions: %v", gotSubs)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.trades) != 1 {
		t.Fatalf("trades: %+v", rec.trades)
	}
	tr := rec.trades[0]
	if tr.Book != "btc_mxn" || tr.Price != 1510000 || tr.Amount != 0.0035 || tr.Side != "sell" || tr.ID != 77777 || tr.At.UnixMilli() != 1791100000000 {
		t.Fatalf("trade: %+v", tr)
	}
	if len(rec.tops) != 1 || rec.tops[0].Bid != 84995 || rec.tops[0].Ask != 85010 || rec.tops[0].Book != "btc_usd" {
		t.Fatalf("top of book must be the best levels whatever their order: %+v", rec.tops)
	}
	if len(rec.disconnects) == 0 || rec.messages < 4 {
		t.Fatalf("disconnects %v messages %d", rec.disconnects, rec.messages)
	}
}

func TestFeedReconnectsWhenSilent(t *testing.T) {
	fb := &fakeBitso{t: t}
	fb.scripts = []func(*websocket.Conn, []map[string]string){
		func(c *websocket.Conn, _ []map[string]string) { time.Sleep(400 * time.Millisecond) }, // no keep-alives
	}
	srv := httptest.NewServer(fb)
	defer srv.Close()
	rec := &recorder{}
	f := &Feed{URL: "ws" + strings.TrimPrefix(srv.URL, "http"), Books: []string{"btc_mxn", "btc_usd"}, Handler: rec,
		Log: log.New(io.Discard, "", 0), MinBackoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond, ReadTimeout: 100 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.Run(ctx)
	waitFor(t, "a reconnect after silence", func() bool { rec.mu.Lock(); defer rec.mu.Unlock(); return rec.connects >= 2 })
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if !strings.Contains(rec.disconnects[0], "no message for 100ms") {
		t.Fatalf("disconnect reason: %v", rec.disconnects)
	}
}

func TestFeedRejectedSubscription(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		var m map[string]string
		_ = c.ReadJSON(&m)
		_ = c.WriteJSON(map[string]any{"action": "subscribe", "response": "error", "type": "trades"})
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()
	rec := &recorder{}
	f := &Feed{URL: "ws" + strings.TrimPrefix(srv.URL, "http"), Books: []string{"btc_mxn"}, Handler: rec,
		Log: log.New(io.Discard, "", 0), MinBackoff: time.Hour, ReadTimeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.Run(ctx)
	waitFor(t, "the rejection", func() bool { rec.mu.Lock(); defer rec.mu.Unlock(); return len(rec.disconnects) > 0 })
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if !strings.Contains(rec.disconnects[0], "rejected") {
		t.Fatalf("%v", rec.disconnects)
	}
}

// --- hub ---

func closesEndingOn(last string, n int, v float64) Closes {
	return func(string) ([]float64, string, error) {
		cs := make([]float64, n)
		for i := range cs {
			cs[i] = v
		}
		return cs, last, nil
	}
}

func TestComputeProvisional(t *testing.T) {
	closes := make([]float64, 60)
	for i := range closes {
		closes[i] = float64(i + 1) // 1..60; the last 49 are 12..60, mean 36
	}
	p, note := ComputeProvisional(closes, "2026-10-05", "2026-10-06", 40)
	if p == nil {
		t.Fatal(note)
	}
	if p.FlipLevel != 36 || p.Signal != "long" || p.Label != ProvisionalLabel || p.BasedOn != "2026-10-05" {
		t.Fatalf("%+v", p)
	}
	if want := (36.0*49 + 40) / 50; p.SMA50 != want {
		t.Fatalf("sma50 %v want %v", p.SMA50, want)
	}
	// At the flip level the close equals the SMA: not long (the rule needs close > SMA).
	if p, _ := ComputeProvisional(closes, "2026-10-05", "2026-10-06", 36); p.Signal != "flat" || p.SMA50 != 36 {
		t.Fatalf("at the level: %+v", p)
	}
	if p, note := ComputeProvisional(closes, "2026-10-04", "2026-10-06", 40); p != nil || !strings.Contains(note, "not 2026-10-05") {
		t.Fatalf("stale closes must withhold the level: %+v %q", p, note)
	}
	if p, _ := ComputeProvisional(closes[:48], "2026-10-05", "2026-10-06", 40); p != nil {
		t.Fatal("needs 49 closes")
	}
}

func newTestHub(now *time.Time, seeder Seeder, closes Closes) *Hub {
	h := &Hub{Books: []string{"btc_mxn", "btc_usd"}, Source: "test", Seeder: seeder, Closes: closes,
		Throttle: time.Hour, Heartbeat: time.Hour, Log: log.New(io.Discard, "", 0), Now: func() time.Time { return *now }}
	h.init()
	return h
}

// 2026-10-06 12:00 Mexico City.
var noon = time.Date(2026, 10, 6, 18, 0, 0, 0, time.UTC)

func TestHubFormingCandleAcrossMidnight(t *testing.T) {
	now := noon
	h := newTestHub(&now, nil, closesEndingOn("2026-10-05", 60, 100))
	tr := func(at time.Time, p, a float64) Trade {
		return Trade{Book: "btc_usd", Price: p, Amount: a, Side: "buy", At: at}
	}
	h.OnTrade(tr(noon, 101, 1))
	h.OnTrade(tr(noon.Add(time.Second), 99, 2))
	h.OnTrade(tr(noon.Add(2*time.Second), 103, 0.5))
	s := h.Snapshot([]string{"btc_usd"}).Books[0]
	c := s.Candle
	if c.Date != "2026-10-06" || c.Open != 101 || c.High != 103 || c.Low != 99 || c.Close != 103 || c.Volume != 3.5 || c.TradeCount != 3 || c.Seeded {
		t.Fatalf("candle %+v", c)
	}
	if s.Last != 103 || s.Provisional == nil || s.Provisional.FlipLevel != 100 || s.Provisional.Signal != "long" {
		t.Fatalf("snapshot %+v", s)
	}
	// 00:00:01 Mexico City on 10-07: a new bar. The candle file still ends
	// 10-05, so the provisional level is withheld until the executor runs.
	midnight := time.Date(2026, 10, 7, 6, 0, 1, 0, time.UTC)
	now = midnight
	h.OnTrade(tr(midnight, 98, 1))
	h.OnTrade(tr(noon.Add(3*time.Second), 500, 1)) // late trade for the old day: ignored by the bar
	s = h.Snapshot([]string{"btc_usd"}).Books[0]
	if s.Candle.Date != "2026-10-07" || s.Candle.Open != 98 || s.Candle.TradeCount != 1 {
		t.Fatalf("new day %+v", s.Candle)
	}
	if s.Last != 98 {
		t.Fatalf("an older trade must not replace the last price: %v", s.Last)
	}
	if s.Provisional != nil || !strings.Contains(s.ProvisionalNote, "not 2026-10-06") {
		t.Fatalf("provisional must be withheld: %+v %q", s.Provisional, s.ProvisionalNote)
	}
}

func TestHubSeedsOnConnectAndKeepsLaterTrades(t *testing.T) {
	now := noon
	release := make(chan struct{})
	seeder := func(ctx context.Context, book, day string) (Candle, bool, error) {
		<-release
		if book == "btc_mxn" {
			return Candle{}, false, nil
		}
		return Candle{Date: day, Open: 90, High: 110, Low: 85, Close: 100, Volume: 50, TradeCount: 400}, true, nil
	}
	h := newTestHub(&now, seeder, nil)
	h.OnTrade(Trade{Book: "btc_usd", Price: 105, Amount: 1, At: noon.Add(-time.Minute)}) // before the seed: in REST already
	h.OnConnect()
	waitFor(t, "seeding to start", func() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.books["btc_usd"].seeding })
	h.OnTrade(Trade{Book: "btc_usd", Price: 120, Amount: 2, At: noon.Add(time.Second)}) // during the seed
	close(release)
	waitFor(t, "the seed", func() bool { s := h.Snapshot(nil); return s.Books[1].Candle != nil && s.Books[1].Candle.Seeded })
	s := h.Snapshot(nil)
	c := s.Books[1].Candle
	if c.Open != 90 || c.High != 120 || c.Low != 85 || c.Close != 120 || c.Volume != 52 || c.TradeCount != 401 {
		t.Fatalf("seeded candle + later trade: %+v", c)
	}
	if !s.Upstream.Connected || s.Upstream.Source != "test" {
		t.Fatalf("status %+v", s.Upstream)
	}
	waitFor(t, "btc_mxn seed", func() bool { s := h.Snapshot(nil); return s.Books[0].Candle != nil })
	if m := h.Snapshot(nil).Books[0].Candle; !m.Seeded || m.TradeCount != 0 || m.Date != "2026-10-06" {
		t.Fatalf("no trades today yet: %+v", m)
	}
	h.OnDisconnect(io.EOF)
	h.OnConnect()
	if st := h.Snapshot(nil).Upstream; st.Reconnects != 1 || !st.Connected {
		t.Fatalf("reconnect count: %+v", st)
	}
}

func TestHubThrottlesAndFansOut(t *testing.T) {
	now := noon
	h := newTestHub(&now, nil, nil)
	all := h.Subscribe(nil)
	mxn := h.Subscribe([]string{"btc_mxn"})
	for i := 0; i < 50; i++ {
		h.OnTrade(Trade{Book: "btc_usd", Price: 100 + float64(i), Amount: 0.1, At: noon.Add(time.Duration(i) * time.Millisecond)})
	}
	h.OnTop(Top{Book: "btc_usd", Bid: 148, Ask: 150})
	h.flush()
	h.flush() // nothing new: no event
	var got []Event
	for len(all.C) > 0 {
		got = append(got, <-all.C)
	}
	if len(got) != 1 || got[0].Name != "book" {
		t.Fatalf("50 trades in one throttle window must give one event: %+v", got)
	}
	b := got[0].Data.(BookSnapshot)
	if b.Last != 149 || b.Bid != 148 || b.Ask != 150 || b.Candle.TradeCount != 50 {
		t.Fatalf("latest value wins: %+v", b)
	}
	if len(mxn.C) != 0 {
		t.Fatal("a btc_mxn subscriber must not get btc_usd events")
	}
	if _, err := json.Marshal(got[0].Data); err != nil {
		t.Fatal(err)
	}

	// A subscriber that stops reading is dropped, not allowed to block the hub.
	for i := 0; i < 100; i++ {
		h.OnTop(Top{Book: "btc_mxn", Bid: float64(i + 1)})
		h.flush()
	}
	waitFor(t, "slow subscriber dropped", func() bool {
		for {
			select {
			case _, ok := <-mxn.C:
				if !ok {
					return true
				}
			default:
				return false
			}
		}
	})
	h.Close()
	if _, ok := <-all.C; ok {
		for range all.C {
		}
	}
	if s := h.Subscribe(nil); s != nil {
		if _, ok := <-s.C; ok {
			t.Fatal("subscribing after Close must give a closed channel")
		}
	}
}
