package risk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/varmodel"
)

// syntheticCloses returns n daily closes ending on last, alternating
// returns of +/-step so every vol estimate is close to step.
func syntheticCloses(last time.Time, n int, step float64) []varmodel.Close {
	out := make([]varmodel.Close, n)
	p := 1_000_000.0
	for i := 0; i < n; i++ {
		out[i] = varmodel.Close{Date: last.AddDate(0, 0, i-n+1), Price: p}
		if i%2 == 0 {
			p *= math.Exp(step)
		} else {
			p *= math.Exp(-step)
		}
	}
	return out
}

// fakeFetch serves closes per book and counts calls.
type fakeFetch struct {
	mu     sync.Mutex
	closes map[string][]varmodel.Close
	err    error
	calls  map[string]int
	called chan string
}

func (f *fakeFetch) fetch(_ context.Context, book string, _, _ time.Time) ([]varmodel.Close, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[book]++
	cs, err := f.closes[book], f.err
	ch := f.called
	f.mu.Unlock()
	if ch != nil {
		select {
		case ch <- book:
		default:
		}
	}
	return cs, err
}

func (f *fakeFetch) count(book string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[book]
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

var volDay = time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC) // last closed bar

func newTestEstimator(f *fakeFetch, c *clock) *VolEstimator {
	est := NewVolEstimator(f.fetch)
	est.Now = c.now
	est.Episodes = nil // the vol tests count fetches; episodes have their own tests
	return est
}

func TestVolEstimatorLookupRefreshAndStale(t *testing.T) {
	f := &fakeFetch{closes: map[string][]varmodel.Close{"btc_mxn": syntheticCloses(volDay, 700, 0.02)}}
	c := &clock{t: closeTime(volDay).Add(2 * time.Hour)}
	est := newTestEstimator(f, c)

	// Unknown book: registered, nothing to use yet, no network call.
	if v, ok := est.Lookup("BTC_MXN"); ok || v.Have {
		t.Fatalf("first lookup %+v %v", v, ok)
	}
	if b := est.Books(); len(b) != 1 || b[0] != "btc_mxn" || f.count("btc_mxn") != 0 {
		t.Fatalf("books %v calls %d", b, f.count("btc_mxn"))
	}

	est.RefreshDue(context.Background())
	v, ok := est.Lookup("btc_mxn")
	if !ok || !v.Have || v.Err != nil {
		t.Fatalf("after refresh %+v %v", v, ok)
	}
	if math.Abs(v.Estimate.Vol-0.02) > 0.001 || v.Estimate.Backtest.Observations != varmodel.BacktestWindow {
		t.Fatalf("estimate %+v", v.Estimate)
	}
	if v.DataAge != 2*time.Hour {
		t.Fatalf("data age %v", v.DataAge)
	}

	// Within the refresh interval nothing is fetched again.
	c.t = c.t.Add(DefaultVolRefresh - time.Minute)
	est.RefreshDue(context.Background())
	if n := f.count("btc_mxn"); n != 1 {
		t.Fatalf("refetched early: %d calls", n)
	}
	c.t = c.t.Add(time.Minute)
	est.RefreshDue(context.Background())
	if n := f.count("btc_mxn"); n != 2 {
		t.Fatalf("not refreshed after the interval: %d calls", n)
	}

	// The source stops publishing new closes: the estimate is kept but
	// stops being usable once its last close is older than Stale.
	c.t = closeTime(volDay).Add(DefaultVolStale)
	if _, ok := est.Lookup("btc_mxn"); !ok {
		t.Fatal("unusable at exactly the stale limit")
	}
	c.t = c.t.Add(time.Second)
	if v, ok := est.Lookup("btc_mxn"); ok || !v.Have {
		t.Fatalf("stale estimate still used: %+v %v", v, ok)
	}
}

func TestVolEstimatorFailureBacksOffAndKeepsLastEstimate(t *testing.T) {
	f := &fakeFetch{closes: map[string][]varmodel.Close{"btc_mxn": syntheticCloses(volDay, 700, 0.02)}}
	c := &clock{t: closeTime(volDay)}
	est := newTestEstimator(f, c)
	var errs []string
	est.OnError = func(book string, err error) { errs = append(errs, book+": "+err.Error()) }

	est.Lookup("btc_mxn")
	est.RefreshDue(context.Background())

	// Next refresh fails: the last good estimate stays in use (it is still
	// fresh), the error is reported, and retries wait RetryAfter.
	f.mu.Lock()
	f.err = errors.New("bitso down")
	f.mu.Unlock()
	c.t = c.t.Add(DefaultVolRefresh)
	est.RefreshDue(context.Background())
	v, ok := est.Lookup("btc_mxn")
	if !ok || v.Err == nil || len(errs) != 1 {
		t.Fatalf("after failure %+v %v errs=%v", v, ok, errs)
	}
	c.t = c.t.Add(DefaultVolRetryAfter - time.Second)
	est.RefreshDue(context.Background())
	if n := f.count("btc_mxn"); n != 2 {
		t.Fatalf("retried before RetryAfter: %d calls", n)
	}
	c.t = c.t.Add(time.Second)
	f.mu.Lock()
	f.err = nil
	f.mu.Unlock()
	est.RefreshDue(context.Background())
	if v, _ := est.Lookup("btc_mxn"); f.count("btc_mxn") != 3 || v.Err != nil {
		t.Fatalf("no recovery: calls %d err %v", f.count("btc_mxn"), v.Err)
	}
}

func TestVolEstimatorShortHistoryIsAnError(t *testing.T) {
	f := &fakeFetch{closes: map[string][]varmodel.Close{"eth_mxn": syntheticCloses(volDay, varmodel.MinReturns, 0.03)}}
	est := newTestEstimator(f, &clock{t: closeTime(volDay)})
	est.Lookup("eth_mxn")
	est.RefreshDue(context.Background())
	v, ok := est.Lookup("eth_mxn")
	if ok || v.Have || !errors.Is(v.Err, varmodel.ErrInsufficient) {
		t.Fatalf("short history %+v %v", v, ok)
	}
}

// Run fetches a book as soon as Lookup registers it, not a minute later.
func TestVolEstimatorRunWakesOnNewBook(t *testing.T) {
	f := &fakeFetch{closes: map[string][]varmodel.Close{}, called: make(chan string, 4)}
	est := NewVolEstimator(f.fetch)
	est.Episodes = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { est.Run(ctx); close(done) }()
	// Let Run create the wake channel before registering.
	deadline := time.Now().Add(2 * time.Second)
	for {
		est.mu.Lock()
		ready := est.wake != nil
		est.mu.Unlock()
		if ready || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	est.Lookup("btc_usd")
	select {
	case b := <-f.called:
		if b != "btc_usd" {
			t.Fatalf("fetched %q", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("new book not fetched promptly")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop with its context")
	}
}

// BitsoCloses labels closed buckets by Mexico City date and drops the
// in-progress one.
func TestBitsoCloses(t *testing.T) {
	today := time.Now().In(time.UTC)
	mxMidnight := func(daysAgo int) int64 {
		y, m, d := today.AddDate(0, 0, -daysAgo).Date()
		return time.Date(y, m, d, 6, 0, 0, 0, time.UTC).UnixMilli()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/ohlc" || r.URL.Query().Get("book") != "btc_mxn" || r.URL.Query().Get("time_bucket") != "86400" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var payload []map[string]any
		for i := 3; i >= -1; i-- { // -1: tomorrow's bucket, never closed
			payload = append(payload, map[string]any{
				"bucket_start_time": mxMidnight(i), "first_rate": "100", "last_rate": fmt.Sprint(100 + i),
				"min_rate": "99", "max_rate": "105",
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "payload": payload})
	}))
	defer srv.Close()
	cs, err := BitsoCloses(srv.URL, srv.Client())(context.Background(), "btc_mxn", today.AddDate(0, 0, -5), today)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) < 2 || len(cs) > 4 {
		t.Fatalf("closes %+v", cs)
	}
	for i := 1; i < len(cs); i++ {
		if cs[i].Date.Sub(cs[i-1].Date) != 24*time.Hour {
			t.Fatalf("not consecutive Mexico dates: %+v", cs)
		}
	}
	if cs[0].Price != 103 || cs[0].Date.Hour() != 0 {
		t.Fatalf("first close %+v", cs[0])
	}
}
