package etorolive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/ui-api/internal/live"
)

// recorder is a live.Handler that keeps every call.
type recorder struct {
	mu       sync.Mutex
	connects int
	msgs     []time.Time
	tops     []live.Top
	trades   []live.Trade
	errs     []error
}

func (r *recorder) OnConnect()             { r.mu.Lock(); r.connects++; r.mu.Unlock() }
func (r *recorder) OnMessage(at time.Time) { r.mu.Lock(); r.msgs = append(r.msgs, at); r.mu.Unlock() }
func (r *recorder) OnTrade(t live.Trade)   { r.mu.Lock(); r.trades = append(r.trades, t); r.mu.Unlock() }
func (r *recorder) OnTop(t live.Top)       { r.mu.Lock(); r.tops = append(r.tops, t); r.mu.Unlock() }
func (r *recorder) OnDisconnect(err error) { r.mu.Lock(); r.errs = append(r.errs, err); r.mu.Unlock() }

func (r *recorder) snapshot() (int, int, int, int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.connects, len(r.msgs), len(r.tops), len(r.trades), len(r.errs)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPollerConnectsTopsAndMessages(t *testing.T) {
	now := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	var calls [][]int64
	var mu sync.Mutex
	rates := func(ctx context.Context, ids ...int64) ([]etoro.Rate, error) {
		mu.Lock()
		calls = append(calls, append([]int64(nil), ids...))
		mu.Unlock()
		return []etoro.Rate{
			{InstrumentID: 28, Bid: 29191.9, Ask: 29192.6, Date: "2026-10-16T14:59:58.1"},
			{InstrumentID: 27, Bid: 7646.83, Ask: 7647.13, Date: "2026-10-16T14:59:57.6"},
			{InstrumentID: 99, Bid: 1, Ask: 2}, // not one of ours
		}, nil
	}
	rec := &recorder{}
	p := &Poller{Rates: rates, Books: DefaultBooks, Interval: 10 * time.Millisecond, Handler: rec,
		Log: log.New(io.Discard, "", 0), Now: func() time.Time { return now }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	waitFor(t, "three polls", func() bool { _, m, _, _, _ := rec.snapshot(); return m >= 3 })
	cancel()
	<-done

	connects, msgs, tops, trades, errs := rec.snapshot()
	if connects != 1 || errs != 0 || trades != 0 || tops != 2*msgs {
		t.Fatalf("connects=%d msgs=%d tops=%d trades=%d errs=%d", connects, msgs, tops, trades, errs)
	}
	mu.Lock()
	first := calls[0]
	mu.Unlock()
	// One request for both ids, in book order (nsdq100 -> 28, spx500 -> 27).
	if len(first) != 2 || first[0] != 28 || first[1] != 27 {
		t.Fatalf("ids per request: %v", first)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	nsdq, spx := rec.tops[0], rec.tops[1]
	if nsdq.Book != "nsdq100" || nsdq.Bid != 29191.9 || nsdq.Ask != 29192.6 || !nsdq.At.Equal(now) || len(nsdq.Bids) != 0 {
		t.Fatalf("nsdq100 top %+v", nsdq)
	}
	if spx.Book != "spx500" || spx.Bid != 7646.83 || spx.Ask != 7647.13 || !spx.At.Equal(now) {
		t.Fatalf("spx500 top %+v", spx)
	}
	if !rec.msgs[0].Equal(now) {
		t.Fatalf("OnMessage at %v", rec.msgs[0])
	}
}

func TestPollerBacksOffAndRecovers(t *testing.T) {
	var n int
	var mu sync.Mutex
	boom := errors.New("etoro: 429 Too Many Requests")
	rates := func(ctx context.Context, ids ...int64) ([]etoro.Rate, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		switch {
		case n == 1 || n == 4:
			return []etoro.Rate{{InstrumentID: 28, Bid: 1, Ask: 2}}, nil
		case n == 2 || n == 3:
			return nil, boom
		case n == 5:
			return []etoro.Rate{{InstrumentID: 28, Bid: 0, Ask: 0}}, nil // answered, but no quote
		}
		return []etoro.Rate{{InstrumentID: 28, Bid: 1, Ask: 2}}, nil
	}
	rec := &recorder{}
	var logs bytes.Buffer
	p := &Poller{Rates: rates, Books: map[string]int64{"nsdq100": 28}, Interval: 5 * time.Millisecond,
		MinBackoff: 20 * time.Millisecond, MaxBackoff: 40 * time.Millisecond, Handler: rec, Log: log.New(&logs, "", 0)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	start := time.Now()
	go func() { p.Run(ctx); close(done) }()
	waitFor(t, "recovery after the errors", func() bool { c, _, _, _, e := rec.snapshot(); return c >= 3 && e >= 3 })
	cancel()
	<-done
	connects, _, _, _, errs := rec.snapshot()
	// connect (poll 1), disconnect ×2 (polls 2, 3), reconnect (4), disconnect
	// on the empty answer (5), reconnect (6).
	if connects != 3 || errs != 3 {
		t.Fatalf("connects=%d errs=%d", connects, errs)
	}
	rec.mu.Lock()
	if !errors.Is(rec.errs[0], boom) || !errors.Is(rec.errs[2], errNoRates) {
		t.Fatalf("errors %v", rec.errs)
	}
	rec.mu.Unlock()
	// Two backoffs (~20 ms, ~40 ms ±20 %) plus the empty-answer one: well over the 5 ms interval alone.
	if el := time.Since(start); el < 60*time.Millisecond {
		t.Fatalf("no backoff: finished in %s", el)
	}
	if !strings.Contains(logs.String(), "retry in") {
		t.Fatalf("log %q", logs.String())
	}
}

func TestPollerStopsOnContext(t *testing.T) {
	rates := func(ctx context.Context, ids ...int64) ([]etoro.Rate, error) {
		<-ctx.Done() // a hung request ends with the context
		return nil, ctx.Err()
	}
	rec := &recorder{}
	p := &Poller{Rates: rates, Books: DefaultBooks, Handler: rec, Log: log.New(io.Discard, "", 0)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if c, _, _, _, e := rec.snapshot(); c != 0 || e != 0 {
		t.Fatalf("no handler calls expected after cancel: connects=%d errs=%d", c, e)
	}
}

func TestPollerRefusesIncompleteConfig(t *testing.T) {
	var logs bytes.Buffer
	p := &Poller{Handler: &recorder{}, Log: log.New(&logs, "", 0)}
	p.Run(context.Background()) // returns at once
	if !strings.Contains(logs.String(), "not configured") {
		t.Fatalf("log %q", logs.String())
	}
}

func writeEnv(t *testing.T, mode os.FileMode, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "etoro-demo.env")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCredentials(t *testing.T) {
	const pub, priv = "pub-key-123", "user-key-456"
	t.Run("missing file", func(t *testing.T) {
		_, err := Credentials(filepath.Join(t.TempDir(), "none.env"))
		if !IsNotExist(err) || !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("real refused", func(t *testing.T) {
		p := writeEnv(t, 0o600, "ETORO_PUBLIC_KEY="+pub, "ETORO_PRIVATE_KEY="+priv, "ETORO_ENV=real")
		_, err := Credentials(p)
		if !errors.Is(err, ErrNotDemo) || !strings.Contains(err.Error(), `"real"`) {
			t.Fatalf("err %v", err)
		}
		if strings.Contains(err.Error(), pub) || strings.Contains(err.Error(), priv) {
			t.Fatalf("a key leaked into the error: %v", err)
		}
	})
	t.Run("env unset refused", func(t *testing.T) {
		p := writeEnv(t, 0o600, "ETORO_PUBLIC_KEY="+pub, "ETORO_PRIVATE_KEY="+priv)
		if _, err := Credentials(p); !errors.Is(err, ErrNotDemo) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("keys required", func(t *testing.T) {
		p := writeEnv(t, 0o600, "ETORO_ENV=demo", "ETORO_PUBLIC_KEY="+pub)
		if _, err := Credentials(p); err == nil || !strings.Contains(err.Error(), "required") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("world readable refused", func(t *testing.T) {
		p := writeEnv(t, 0o644, "ETORO_ENV=demo", "ETORO_PUBLIC_KEY="+pub, "ETORO_PRIVATE_KEY="+priv)
		if _, err := Credentials(p); err == nil || !strings.Contains(err.Error(), "chmod 600") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("demo ok, values not echoed", func(t *testing.T) {
		p := writeEnv(t, 0o600,
			"# eToro Public API - DEMO (Virtual) credentials.",
			"",
			`ETORO_PUBLIC_KEY="`+pub+`"`,
			"export ETORO_PRIVATE_KEY='"+priv+"'",
			"ETORO_ENV = demo",
			"not a pair",
		)
		cfg, err := Credentials(p)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.PublicKey != pub || cfg.PrivateKey != priv || cfg.Env != etoro.EnvDemo {
			t.Fatalf("cfg %+v", cfg)
		}
		if os.Getenv("ETORO_PUBLIC_KEY") == pub || os.Getenv("ETORO_PRIVATE_KEY") == priv {
			t.Fatal("the loader must not export the keys")
		}
		c, err := etoro.NewClient(cfg)
		if err != nil || c.Environment() != etoro.EnvDemo {
			t.Fatalf("client %v %v", c, err)
		}
	})
}
