package risk

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/varmodel"
)

// VaR volatility estimate (plan §6.4.8). The portfolio monitor's VaR used a
// configured daily vol; it now uses varmodel's estimate from Bitso's own
// daily closes (the bars cmd/daily-executor trades on): max(RiskMetrics
// EWMA, 365-day equal-weighted), with a 250-day Basel backtest and a
// historical-simulation window alongside.
//
// The estimator refreshes in the background, so a slow or unreachable Bitso
// never delays a portfolio run. Lookup is non-blocking and reports whether
// the estimate is usable; when it is not (never fetched, failed, or its
// last close is older than the staleness limit), the monitor falls back to
// the configured vol and says so in risk_var_vol_source.

// Defaults for the estimator.
const (
	DefaultVolRefresh    = 6 * time.Hour
	DefaultVolRetryAfter = 10 * time.Minute
	DefaultVolStale      = 72 * time.Hour
	DefaultVolHistory    = 800 // calendar days: 250 backtest days + 365 window + EWMA warm-up
	DefaultEpisodePause  = 1100 * time.Millisecond
	volFetchTimeout      = 2 * time.Minute
	volChunk             = 300 * bitsodaily.Day // under the endpoint's per-response cap
)

// ClosesFetcher returns daily closes for book over [start, end).
type ClosesFetcher func(ctx context.Context, book string, start, end time.Time) ([]varmodel.Close, error)

// BitsoCloses fetches closed daily candles from Bitso's public OHLC
// endpoint (no credentials), labelled by Mexico City date like the
// daily-executor's bars.
func BitsoCloses(baseURL string, client *http.Client) ClosesFetcher {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return func(ctx context.Context, book string, start, end time.Time) ([]varmodel.Close, error) {
		cs, err := bitsodaily.FetchRangeContext(ctx, client, baseURL, book, start, end, volChunk)
		if err != nil {
			return nil, err
		}
		rows, _ := bitsodaily.ToRows(cs, time.Now())
		out := make([]varmodel.Close, 0, len(rows))
		for _, r := range rows {
			d, err := time.Parse("2006-01-02", r.Date)
			if err != nil {
				return nil, fmt.Errorf("bitso ohlc %s: date %q: %w", book, r.Date, err)
			}
			out = append(out, varmodel.Close{Date: d, Price: r.Close})
		}
		return out, nil
	}
}

// VolView is what the estimator knows about one book.
type VolView struct {
	Estimate varmodel.Estimate
	Have     bool          // an estimate was computed at least once
	DataAge  time.Duration // now minus the last close used
	Err      error         // the last refresh error, if any

	// Episodes holds the book's path through each historical stress
	// episode it traded in, by episode ID (§6.4.9). Read-only.
	Episodes map[string][]varmodel.PathPoint
}

// VolEstimator keeps one estimate per book, and the book's paths through
// the historical stress episodes (fetched once: the past does not change).
type VolEstimator struct {
	Fetch        ClosesFetcher
	Lambda       float64
	Refresh      time.Duration
	RetryAfter   time.Duration
	Stale        time.Duration
	HistoryDays  int
	Episodes     []varmodel.Episode
	EpisodePause time.Duration // between episode requests (public rate limit)
	Now          func() time.Time
	OnError      func(book string, err error)

	mu    sync.Mutex
	books map[string]*volEntry
	wake  chan struct{}
}

type volEntry struct {
	est                  varmodel.Estimate
	have                 bool
	fetchedAt, attemptAt time.Time
	err                  error

	episodes  map[string][]varmodel.PathPoint // replaced, never mutated
	epDone    map[string]bool                 // fetched: a path, or no data then
	epAttempt time.Time                       // last failed episode fetch
}

// NewVolEstimator returns an estimator with the defaults.
func NewVolEstimator(fetch ClosesFetcher) *VolEstimator {
	return &VolEstimator{
		Fetch:        fetch,
		Lambda:       varmodel.Lambda,
		Refresh:      DefaultVolRefresh,
		RetryAfter:   DefaultVolRetryAfter,
		Stale:        DefaultVolStale,
		HistoryDays:  DefaultVolHistory,
		Episodes:     varmodel.Episodes,
		EpisodePause: DefaultEpisodePause,
	}
}

func (v *VolEstimator) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// closeTime is when the bar labelled with date closed: Bitso daily buckets
// end at Mexico City midnight, 05:00-06:00 UTC the next day. 06:00 is used.
func closeTime(date time.Time) time.Time {
	y, m, d := date.Date()
	return time.Date(y, m, d+1, 6, 0, 0, 0, time.UTC)
}

// Lookup returns book's view and whether the estimate is usable now. It
// never blocks on the network; an unknown book is registered and fetched
// by the next refresh.
func (v *VolEstimator) Lookup(book string) (VolView, bool) {
	book = strings.ToLower(book)
	v.mu.Lock()
	if v.books == nil {
		v.books = map[string]*volEntry{}
	}
	e, ok := v.books[book]
	if !ok {
		e = &volEntry{}
		v.books[book] = e
		if v.wake != nil {
			select {
			case v.wake <- struct{}{}:
			default:
			}
		}
	}
	view := VolView{Estimate: e.est, Have: e.have, Err: e.err, Episodes: e.episodes}
	v.mu.Unlock()
	if !view.Have {
		return view, false
	}
	view.DataAge = v.now().Sub(closeTime(view.Estimate.LastDate))
	return view, view.DataAge <= v.Stale
}

// Books lists the registered books.
func (v *VolEstimator) Books() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]string, 0, len(v.books))
	for b := range v.books {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

// due reports whether e should be fetched now.
func (v *VolEstimator) due(e *volEntry, now time.Time) bool {
	if !e.attemptAt.IsZero() && now.Sub(e.attemptAt) < v.RetryAfter {
		return false
	}
	return !e.have || now.Sub(e.fetchedAt) >= v.Refresh
}

// RefreshDue fetches every registered book whose estimate is missing or
// older than Refresh, at most once per RetryAfter after a failure; then
// the stress episodes each book has not fetched yet.
func (v *VolEstimator) RefreshDue(ctx context.Context) {
	for _, book := range v.Books() {
		now := v.now()
		v.mu.Lock()
		e := v.books[book]
		run := v.due(e, now)
		if run {
			e.attemptAt = now
		}
		v.mu.Unlock()
		if !run {
			continue
		}
		est, err := v.fetch(ctx, book, now)
		v.mu.Lock()
		if err != nil {
			e.err = err
		} else {
			e.est, e.have, e.fetchedAt, e.err = est, true, now, nil
		}
		v.mu.Unlock()
		if err != nil && v.OnError != nil {
			v.OnError(book, err)
		}
		if ctx.Err() != nil {
			return
		}
	}
	for _, book := range v.Books() {
		if !v.refreshEpisodes(ctx, book) {
			return
		}
	}
}

// refreshEpisodes fetches book's missing episodes, stopping at the first
// failure (retried after RetryAfter). It returns false when ctx ended.
func (v *VolEstimator) refreshEpisodes(ctx context.Context, book string) bool {
	now := v.now()
	v.mu.Lock()
	e := v.books[book]
	if !e.epAttempt.IsZero() && now.Sub(e.epAttempt) < v.RetryAfter {
		v.mu.Unlock()
		return true
	}
	var todo []varmodel.Episode
	for _, ep := range v.Episodes {
		if !e.epDone[ep.ID] {
			todo = append(todo, ep)
		}
	}
	v.mu.Unlock()
	for i, ep := range todo {
		if i > 0 && v.EpisodePause > 0 {
			t := time.NewTimer(v.EpisodePause)
			select {
			case <-ctx.Done():
				t.Stop()
				return false
			case <-t.C:
			}
		}
		c, cancel := context.WithTimeout(ctx, volFetchTimeout)
		// From the day before the base close to the day after the end, so
		// the Mexico-labelled buckets at both edges are inside.
		closes, err := v.Fetch(c, book, ep.Base().AddDate(0, 0, -1), ep.End.AddDate(0, 0, 2))
		cancel()
		if err != nil {
			v.mu.Lock()
			e.epAttempt = now
			v.mu.Unlock()
			if v.OnError != nil {
				v.OnError(book, fmt.Errorf("stress episode %s: %w", ep.ID, err))
			}
			return ctx.Err() == nil
		}
		path, ok := varmodel.EpisodePath(closes, ep)
		v.mu.Lock()
		if e.epDone == nil {
			e.epDone = map[string]bool{}
		}
		e.epDone[ep.ID] = true
		if ok {
			next := make(map[string][]varmodel.PathPoint, len(e.episodes)+1)
			for k, p := range e.episodes {
				next[k] = p
			}
			next[ep.ID] = path
			e.episodes = next
		}
		v.mu.Unlock()
	}
	return ctx.Err() == nil
}

func (v *VolEstimator) fetch(ctx context.Context, book string, now time.Time) (varmodel.Estimate, error) {
	c, cancel := context.WithTimeout(ctx, volFetchTimeout)
	defer cancel()
	days := v.HistoryDays
	if days <= 0 {
		days = DefaultVolHistory
	}
	closes, err := v.Fetch(c, book, now.AddDate(0, 0, -days), now)
	if err != nil {
		return varmodel.Estimate{}, err
	}
	lambda := v.Lambda
	if !(lambda > 0 && lambda < 1) {
		lambda = varmodel.Lambda
	}
	est, err := varmodel.EstimateFromCloses(closes, lambda)
	if err != nil {
		return varmodel.Estimate{}, fmt.Errorf("%s: %w (%d returns from %d closes)", book, err, est.Returns, len(closes))
	}
	return est, nil
}

// Run refreshes until ctx ends: once a minute, and at once when Lookup
// registers a new book.
func (v *VolEstimator) Run(ctx context.Context) {
	v.mu.Lock()
	if v.wake == nil {
		v.wake = make(chan struct{}, 1)
	}
	wake := v.wake
	v.mu.Unlock()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		v.RefreshDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-wake:
		}
	}
}
