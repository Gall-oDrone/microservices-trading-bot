// Package etorolive polls eToro's rates route for the index-CFD books and
// feeds a live.Hub through the venue-neutral live.Handler seam, so the
// operator console shows NSDQ100 and SPX500 bid/ask like the Bitso books
// (porting plan P5c, 2026-10-16).
//
// Read-only by construction: the poller's only upstream call is Rates
// (etoro.Client.GetRates, one request for every book), 12 reads a minute at
// the default 5 s interval, inside the client's 55/min pacing. No trading,
// portfolio or account route is reachable from here. Everything it produces
// is display only; the executor decides on closed daily candles.
package etorolive

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/etoro"
	"bitso-trading-platform/ui-api/internal/live"
)

// DefaultBooks maps the executor's books to eToro instrument ids (the
// executor's instrument table, cmd/etoro-daily-executor/decide.go; the
// ledger records carry the same ids).
var DefaultBooks = map[string]int64{"nsdq100": 28, "spx500": 27}

// DefaultInterval is the poll period: 12 reads a minute for both books.
const DefaultInterval = 5 * time.Second

// Poller calls Rates every Interval and reports to Handler.
type Poller struct {
	// Rates fetches bid/ask for instrument ids (etoro.Client.GetRates).
	Rates func(ctx context.Context, ids ...int64) ([]etoro.Rate, error)
	// Books maps book -> instrument id; every id goes in one request.
	Books    map[string]int64
	Interval time.Duration
	// Timeout bounds one Rates call (default 10 s).
	Timeout time.Duration
	// After an error the next poll waits MinBackoff, doubling up to
	// MaxBackoff (±20 % jitter) until a poll succeeds again.
	MinBackoff time.Duration
	MaxBackoff time.Duration
	Handler    live.Handler
	Log        *log.Logger
	Now        func() time.Time
}

func (p *Poller) defaults() {
	if p.Interval <= 0 {
		p.Interval = DefaultInterval
	}
	if p.Timeout <= 0 {
		p.Timeout = 10 * time.Second
	}
	if p.MinBackoff <= 0 {
		p.MinBackoff = time.Second
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = 30 * time.Second
	}
	if p.Log == nil {
		p.Log = log.Default()
	}
	if p.Now == nil {
		p.Now = time.Now
	}
}

// IDs returns the instrument ids in book order (deterministic requests).
func (p *Poller) IDs() ([]string, []int64) {
	books := make([]string, 0, len(p.Books))
	for b := range p.Books {
		books = append(books, b)
	}
	sort.Strings(books)
	ids := make([]int64, len(books))
	for i, b := range books {
		ids[i] = p.Books[b]
	}
	return books, ids
}

// Run polls until ctx is done. The first successful poll is OnConnect;
// each rate is an OnTop (bid, ask, stamped with the poll time) followed by
// OnMessage; an error is OnDisconnect and a backoff, after which polling
// resumes and the next success is a reconnect.
//
// The poll time, not the route's "date": that field lags and is not
// monotonic (2026-10-16: it kept the first poll's value while bid and ask
// moved), and the hub orders last prices by time. When ui-api observed the
// quote is the honest timestamp for a display-only feed.
func (p *Poller) Run(ctx context.Context) {
	p.defaults()
	if p.Rates == nil || p.Handler == nil || len(p.Books) == 0 {
		p.Log.Printf("etoro live: poller not configured (rates, handler and books are required)")
		return
	}
	_, ids := p.IDs()
	byID := make(map[int64]string, len(p.Books))
	for b, id := range p.Books {
		byID[id] = b
	}
	connected := false
	backoff := p.MinBackoff
	for {
		err := p.poll(ctx, ids, byID, &connected)
		if ctx.Err() != nil {
			return
		}
		wait := p.Interval
		if err != nil {
			p.Handler.OnDisconnect(err)
			connected = false
			wait = jitter(backoff)
			p.Log.Printf("etoro live: rates: %v (retry in %s)", err, wait.Round(100*time.Millisecond))
			if backoff *= 2; backoff > p.MaxBackoff {
				backoff = p.MaxBackoff
			}
		} else {
			backoff = p.MinBackoff
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// errNoRates: the route answered without a usable quote for any book.
var errNoRates = errors.New("no rates in the response")

func (p *Poller) poll(ctx context.Context, ids []int64, byID map[int64]string, connected *bool) error {
	cctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	rates, err := p.Rates(cctx, ids...)
	if err != nil {
		return err
	}
	now := p.Now()
	got := 0
	for _, r := range rates {
		book, ok := byID[r.InstrumentID]
		if !ok || r.Bid <= 0 || r.Ask <= 0 {
			continue
		}
		if !*connected {
			*connected = true
			p.Handler.OnConnect()
		}
		p.Handler.OnTop(live.Top{Book: book, Bid: r.Bid, Ask: r.Ask, At: now})
		got++
	}
	if got == 0 {
		return errNoRates
	}
	p.Handler.OnMessage(now)
	return nil
}

func jitter(d time.Duration) time.Duration {
	f := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(d) * f)
}
