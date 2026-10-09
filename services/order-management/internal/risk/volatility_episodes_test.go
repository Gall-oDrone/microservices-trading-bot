package risk

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/varmodel"
)

// rangeFetch serves a synthetic daily history per book, filtered to the
// requested [start, end) like the Bitso endpoint: price 100 until the
// crash day, then 60 for a day and 80 after.
type rangeFetch struct {
	mu      sync.Mutex
	listed  map[string]time.Time // book -> first day it traded
	crash   time.Time
	failing bool
	calls   int
}

func (f *rangeFetch) fetch(_ context.Context, book string, start, end time.Time) ([]varmodel.Close, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failing {
		return nil, errors.New("bitso down")
	}
	first, ok := f.listed[book]
	if !ok {
		return nil, nil
	}
	var out []varmodel.Close
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		if d.Before(first) {
			continue
		}
		p := 100.0
		switch {
		case d.Equal(f.crash):
			p = 60
		case d.After(f.crash):
			p = 80
		}
		out = append(out, varmodel.Close{Date: d, Price: p})
	}
	return out, nil
}

var (
	epOld = varmodel.Episode{ID: "old", Name: "old", Start: date0("2020-03-08"), End: date0("2020-03-16")}
	epNew = varmodel.Episode{ID: "new", Name: "new", Start: date0("2022-06-10"), End: date0("2022-06-18")}
)

func date0(s string) time.Time { t, _ := time.Parse("2006-01-02", s); return t }

func TestVolEstimatorFetchesEpisodesOnce(t *testing.T) {
	f := &rangeFetch{
		listed: map[string]time.Time{"btc_mxn": date0("2017-06-01"), "btc_usd": date0("2020-04-24")},
		crash:  date0("2022-06-13"),
	}
	est := NewVolEstimator(f.fetch)
	est.Episodes = []varmodel.Episode{epOld, epNew}
	est.Known = nil // counts fetches; the seed has its own test
	est.EpisodePause = 0
	est.Now = func() time.Time { return date0("2026-10-08") }
	est.Lookup("btc_mxn")
	est.Lookup("btc_usd")
	est.RefreshDue(context.Background())

	mxn, _ := est.Lookup("btc_mxn")
	usd, _ := est.Lookup("btc_usd")
	if len(mxn.Episodes) != 2 || len(usd.Episodes) != 1 {
		t.Fatalf("episodes mxn %v usd %v", keys(mxn.Episodes), keys(usd.Episodes))
	}
	// btc_usd did not trade in 2020: no path, and it is not asked again.
	if _, ok := usd.Episodes["old"]; ok {
		t.Fatal("btc_usd path for an episode before it listed")
	}
	if tr := varmodel.Trough(usd.Episodes["new"]); math.Abs(tr+0.4) > 1e-12 {
		t.Fatalf("trough %v", tr)
	}
	// 2 vol fetches + 2 + 2 episode fetches; the next refresh (within the
	// vol refresh interval) fetches nothing.
	if f.calls != 6 {
		t.Fatalf("calls %d", f.calls)
	}
	est.RefreshDue(context.Background())
	if f.calls != 6 {
		t.Fatalf("episodes refetched: %d calls", f.calls)
	}
}

func TestVolEstimatorEpisodeFailureBacksOff(t *testing.T) {
	f := &rangeFetch{listed: map[string]time.Time{"btc_mxn": date0("2017-06-01")}, crash: date0("2022-06-13"), failing: true}
	now := date0("2026-10-08")
	est := NewVolEstimator(f.fetch)
	est.Episodes = []varmodel.Episode{epOld, epNew}
	est.Known = nil
	est.EpisodePause = 0
	est.Now = func() time.Time { return now }
	var errs int
	est.OnError = func(string, error) { errs++ }
	est.Lookup("btc_mxn")
	est.RefreshDue(context.Background())
	// The vol fetch and the first episode fail; the second episode is not
	// tried in the same pass.
	if f.calls != 2 || errs != 2 {
		t.Fatalf("calls %d errors %d", f.calls, errs)
	}
	f.mu.Lock()
	f.failing = false
	f.mu.Unlock()
	now = now.Add(DefaultVolRetryAfter - time.Second)
	est.RefreshDue(context.Background())
	if v, _ := est.Lookup("btc_mxn"); len(v.Episodes) != 0 {
		t.Fatal("episodes retried before RetryAfter")
	}
	now = now.Add(time.Second)
	est.RefreshDue(context.Background())
	if v, _ := est.Lookup("btc_mxn"); len(v.Episodes) != 2 {
		t.Fatalf("no recovery: %v", keys(v.Episodes))
	}
}

// After a restart the known books start with every episode from the
// committed history and make no episode requests; a book the seed does not
// know fetches all of them.
func TestVolEstimatorSeedsKnownEpisodes(t *testing.T) {
	f := &rangeFetch{listed: map[string]time.Time{"eth_mxn": date0("2019-01-01")}, crash: date0("2022-06-13")}
	est := NewVolEstimator(f.fetch)
	est.EpisodePause = 0
	est.Now = func() time.Time { return date0("2026-10-08") }

	mxn, _ := est.Lookup("btc_mxn")
	usd, _ := est.Lookup("btc_usd")
	if len(mxn.Episodes) != len(varmodel.Episodes) || len(usd.Episodes) != 5 {
		t.Fatalf("seeded before any fetch: mxn %v usd %v", keys(mxn.Episodes), keys(usd.Episodes))
	}
	if tr := varmodel.Trough(mxn.Episodes["2018-01-crash"]); math.Abs(tr+0.620) > 0.001 {
		t.Fatalf("btc_mxn 2018-01 trough %v", tr)
	}
	est.RefreshDue(context.Background())
	if f.calls != 2 { // one vol fetch per book, no episode fetches
		t.Fatalf("calls %d", f.calls)
	}

	est.Lookup("eth_mxn")
	est.RefreshDue(context.Background())
	if f.calls != 2+1+len(varmodel.Episodes) {
		t.Fatalf("unknown book: calls %d", f.calls)
	}
}

// Episodes the seed does not cover (e.g. one added later) are fetched.
func TestVolEstimatorSeedPartialFetchesTheRest(t *testing.T) {
	f := &rangeFetch{listed: map[string]time.Time{"btc_mxn": date0("2017-06-01")}, crash: date0("2022-06-13")}
	est := NewVolEstimator(f.fetch)
	est.Episodes = []varmodel.Episode{epOld, epNew}
	est.Known = map[string]map[string][]varmodel.PathPoint{"btc_mxn": {"old": nil}}
	est.EpisodePause = 0
	est.Now = func() time.Time { return date0("2026-10-08") }
	est.Lookup("btc_mxn")
	est.RefreshDue(context.Background())
	v, _ := est.Lookup("btc_mxn")
	if f.calls != 2 || len(v.Episodes) != 1 || v.Episodes["new"] == nil {
		t.Fatalf("calls %d episodes %v", f.calls, keys(v.Episodes))
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
