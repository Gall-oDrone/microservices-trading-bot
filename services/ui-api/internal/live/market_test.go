package live

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Market page: depth, tape, market events (FRONTEND-UI-PLAN §8.11) ---

func TestLevelsSortMergeCapAndSkipBad(t *testing.T) {
	var in []wsLevel
	for i := 0; i < 30; i++ {
		in = append(in, wsLevel{R: fmt.Sprint(100 + i), A: "1"})
	}
	in = append(in, wsLevel{R: "105", A: "0.5"}, wsLevel{R: "x", A: "1"}, wsLevel{R: "90", A: "0"}, wsLevel{R: "-1", A: "1"})
	bids := levels(in, true)
	if len(bids) != MaxDepth || bids[0].Price != 129 || bids[MaxDepth-1].Price != 110 {
		t.Fatalf("bids must be the highest %d, best first: %+v", MaxDepth, bids)
	}
	asks := levels(in, false)
	if asks[0].Price != 100 || asks[5].Price != 105 || asks[5].Amount != 1.5 {
		t.Fatalf("asks must be lowest first with equal prices merged: %+v", asks[:6])
	}
	if got := levels(nil, true); len(got) != 0 {
		t.Fatalf("empty side: %+v", got)
	}
}

func TestFeedKeepsDepthLevels(t *testing.T) {
	// The orders message of TestFeedParsesSubscribesAndReconnects, parsed directly.
	o := wsOrders{Bids: []wsLevel{{R: "84990", A: "0.1"}, {R: "84995", A: "0.2"}}, Asks: []wsLevel{{R: "85020", A: "0.3"}, {R: "85010", A: "0.4"}}}
	b, a := levels(o.Bids, true), levels(o.Asks, false)
	if b[0] != (Level{84995, 0.2}) || b[1] != (Level{84990, 0.1}) || a[0] != (Level{85010, 0.4}) || a[1] != (Level{85020, 0.3}) {
		t.Fatalf("bids %+v asks %+v", b, a)
	}
}

func tapeTrade(id int64, at time.Time) Trade {
	return Trade{Book: "btc_usd", ID: id, Price: 85000 + float64(id), Amount: 0.01, Side: "buy", At: at}
}

func TestTapeOrderDedupeAndCap(t *testing.T) {
	st := &bookState{}
	base := noon
	st.addTape(tapeTrade(2, base.Add(2*time.Second)))
	st.addTape(tapeTrade(1, base.Add(time.Second)))
	st.addTape(tapeTrade(3, base.Add(2*time.Second))) // same time, higher id: first
	st.addTape(tapeTrade(2, base.Add(2*time.Second))) // duplicate
	st.addTape(tapeTrade(1, base.Add(time.Second)))   // duplicate further down
	ids := func() (out []int64) {
		for _, x := range st.tape {
			out = append(out, x.ID)
		}
		return
	}
	if got := fmt.Sprint(ids()); got != "[3 2 1]" {
		t.Fatalf("tape order %s", got)
	}
	for i := int64(10); i < 10+TapeSize+5; i++ {
		st.addTape(tapeTrade(i, base.Add(time.Duration(i)*time.Second)))
	}
	if len(st.tape) != TapeSize || st.tape[0].ID != 10+TapeSize+4 {
		t.Fatalf("cap: len %d first %d", len(st.tape), st.tape[0].ID)
	}
	// Older than everything on a full tape: dropped.
	st.addTape(tapeTrade(9999, base.Add(-time.Hour)))
	for _, x := range st.tape {
		if x.ID == 9999 {
			t.Fatal("a trade older than a full tape must not be kept")
		}
	}
}

func TestHubMarketSnapshotAndSeededTape(t *testing.T) {
	now := noon
	h := newTestHub(&now, nil, nil)
	seedCalls := 0
	h.Tape = func(_ context.Context, book string) ([]Trade, error) {
		seedCalls++
		if book == "btc_mxn" {
			return nil, errors.New("boom")
		}
		// REST overlaps the live trade 7 and adds older ones.
		return []Trade{tapeTrade(7, noon), tapeTrade(6, noon.Add(-time.Second)), tapeTrade(5, noon.Add(-2*time.Second)),
			{Book: "btc_mxn", ID: 1, Price: 1, At: noon}}, nil
	}
	h.OnTrade(tapeTrade(7, noon))
	h.OnTop(Top{Book: "btc_usd", Bid: 84990, Ask: 85010, At: noon,
		Bids: []Level{{84990, 0.5}, {84980, 1}}, Asks: []Level{{85010, 0.2}, {85030, 2}}})
	h.seedTape(context.Background(), "btc_usd")
	h.seedTape(context.Background(), "btc_mxn")

	s := h.SnapshotWith([]string{"btc_usd"}, true)
	if len(s.Markets) != 1 {
		t.Fatalf("markets %+v", s.Markets)
	}
	m := s.Markets[0]
	if m.Bid != 84990 || m.Ask != 85010 || m.Mid != 85000 || m.Spread != 20 || fmt.Sprintf("%.4f", m.SpreadBps) != "2.3529" {
		t.Fatalf("spread %+v", m)
	}
	if len(m.Bids) != 2 || m.Asks[1].Price != 85030 || m.DepthAt != rfc(noon) {
		t.Fatalf("depth %+v", m)
	}
	if !m.TapeSeeded || len(m.Trades) != 3 || m.Trades[0].ID != 7 || m.Trades[2].ID != 5 || m.Trades[0].Side != "buy" {
		t.Fatalf("tape must merge REST and live by id, newest first: %+v", m.Trades)
	}
	mx := h.SnapshotWith([]string{"btc_mxn"}, true).Markets[0]
	if mx.TapeSeeded || len(mx.Trades) != 0 || mx.Mid != 0 || mx.SpreadBps != 0 {
		t.Fatalf("a failed seed leaves an unseeded, empty tape; no quotes means no spread: %+v", mx)
	}
	if plain := h.Snapshot(nil); plain.Markets != nil {
		t.Fatal("markets only when asked for")
	}
	// An orders message with an empty side keeps the previous levels.
	h.OnTop(Top{Book: "btc_usd", Bid: 84991, At: noon.Add(time.Second), Bids: []Level{{84991, 1}}})
	m = h.SnapshotWith([]string{"btc_usd"}, true).Markets[0]
	if len(m.Asks) != 2 || m.Bids[0].Price != 84991 {
		t.Fatalf("empty side: %+v", m)
	}
	if seedCalls != 2 {
		t.Fatalf("seed calls %d", seedCalls)
	}
}

func TestMarketEventsOnlyForMarketSubscribers(t *testing.T) {
	now := noon
	h := newTestHub(&now, nil, nil)
	plain := h.Subscribe([]string{"btc_usd"})
	mkt := h.SubscribeWith([]string{"btc_usd"}, true)
	other := h.SubscribeWith([]string{"btc_mxn"}, true)
	h.OnTrade(tapeTrade(1, noon))
	h.flush()
	names := func(s *Subscription) (out []string) {
		for {
			select {
			case ev := <-s.C:
				out = append(out, ev.Name)
			default:
				return
			}
		}
	}
	if got := fmt.Sprint(names(plain)); got != "[book]" {
		t.Fatalf("plain subscriber got %s", got)
	}
	if got := fmt.Sprint(names(mkt)); got != "[book market]" {
		t.Fatalf("market subscriber got %s", got)
	}
	if got := fmt.Sprint(names(other)); got != "[]" {
		t.Fatalf("other book got %s", got)
	}
	// No market subscriber at all: no market payload is built or sent.
	h.Unsubscribe(mkt)
	h.Unsubscribe(other)
	h.OnTrade(tapeTrade(2, noon))
	h.flush()
	if got := fmt.Sprint(names(plain)); got != "[book]" {
		t.Fatalf("plain subscriber got %s", got)
	}
}

func TestRESTTapeSeeder(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		_, _ = w.Write([]byte(`{"success":true,"payload":[` +
			`{"book":"btc_mxn","created_at":"2026-10-08T00:29:50+0000","amount":"0.02298311","maker_side":"buy","price":"1495100","tid":202476650},` +
			`{"book":"btc_mxn","created_at":"2026-10-08T00:29:14+0000","amount":"0.00001317","maker_side":"sell","price":"1496230","tid":202476625},` +
			`{"book":"btc_mxn","created_at":"bad","amount":"1","maker_side":"sell","price":"1","tid":1},` +
			`{"book":"btc_mxn","created_at":"2026-10-08T00:29:14+0000","amount":"1","maker_side":"sell","price":"0","tid":2}]}`))
	}))
	defer srv.Close()
	trades, err := RESTTapeSeeder(srv.URL, nil)(context.Background(), "btc_mxn")
	if err != nil {
		t.Fatal(err)
	}
	if gotURL != fmt.Sprintf("/api/v3/trades?book=btc_mxn&limit=%d", TapeSize) {
		t.Fatalf("url %s", gotURL)
	}
	if len(trades) != 2 {
		t.Fatalf("bad rows must be skipped: %+v", trades)
	}
	a, b := trades[0], trades[1]
	// maker_side buy = the taker sold (hit the bid), and vice versa.
	if a.Side != "sell" || a.ID != 202476650 || a.Price != 1495100 || a.Amount != 0.02298311 || !a.At.Equal(time.Date(2026, 10, 8, 0, 29, 50, 0, time.UTC)) {
		t.Fatalf("trade a %+v", a)
	}
	if b.Side != "buy" || b.Book != "btc_mxn" {
		t.Fatalf("trade b %+v", b)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"0201","message":"Too many requests"}}`))
	}))
	defer bad.Close()
	if _, err := RESTTapeSeeder(bad.URL, nil)(context.Background(), "btc_mxn"); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("error must name the status: %v", err)
	}
}
