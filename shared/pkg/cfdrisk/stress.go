package cfdrisk

import (
	"math"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/varmodel"
)

// Stress scenarios for index CFDs: hypothetical uniform shocks, replays of
// equity-index crises on today's exposure, the reverse stress against a
// capital figure, and the margin close-out.
//
// The replay arithmetic is varmodel's (EpisodePath, EpisodeLoss, ShockLoss,
// ReverseStressMove, unchanged); only the episodes and the data differ.
// eToro's own CFD history starts in December 2022 (1000-bar window), so the
// episodes are measured on the official indices from Yahoo, ^NDX for
// NSDQ100 and ^GSPC for SPX500 (docs/backtest-readiness/evidence-2026-10-10/
// data). On the 2022-12..2026-10 overlap the CFD bars track the indices
// (close ratio ~1.0000, P2 study), so an index move is the CFD's move.
//
// Margin close-out. At x1 the margin is the whole notional, and eToro
// (ESMA) closes a CFD position when the account's equity on it falls to
// 50 % of that margin: a -50 % move on a single, isolated position. The
// account here is far larger than the positions (100,000 USD against
// 2,200 USD on 2026-10-13), so in practice the account's equity, not the
// position's, decides; the flag is kept as the Bitso simulators keep it, as
// the move after which the position is read as "would have been closed
// out", not as a larger drawdown.

// Shocks are the hypothetical uniform moves applied to every book at once.
var Shocks = []float64{-0.05, -0.10, -0.20, -0.30}

// CloseOutMove is the uniform move at which an x1 CFD position's equity is
// half its margin.
const CloseOutMove = -0.5

// IndexEpisode is a historical stress window on an equity index: the last
// calm close (Base) and the trough (End), both NYSE trading days. The
// window replayed is (Base, End]: every trading-day close in it against
// Base's close.
type IndexEpisode struct {
	ID, Name  string
	Base, End time.Time
}

// Episode is the varmodel window with the same base close: varmodel
// measures from the calendar day before Start, so Start is Base + 1 day
// (a weekend or holiday there is harmless: only closes inside the window
// are read, and the base is looked up by its own date).
func (e IndexEpisode) Episode() varmodel.Episode {
	return varmodel.Episode{ID: e.ID, Name: e.Name, Start: e.Base.AddDate(0, 0, 1), End: e.End}
}

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// IndexEpisodes are the equity-index crises replayed on today's positions,
// from the last calm close to the trough on ^NDX and ^GSPC closes
// (episode_paths_gen_test.go pins the paths to the committed Yahoo CSVs;
// the comments give the ^NDX / ^GSPC troughs).
var IndexEpisodes = []IndexEpisode{
	{"2008-09-lehman", "2008-09 Lehman to the November low", date("2008-09-19"), date("2008-11-20")}, // -40.6 / -40.1 %
	{"2010-05-flash-crash", "2010-05 flash crash", date("2010-05-05"), date("2010-05-06")},           // -3.3 / -3.2 % (closes; intraday was -9 %)
	{"2011-08-downgrade", "2011-08 US downgrade", date("2011-07-22"), date("2011-08-08")},            // -15.2 / -16.8 %
	{"2015-08-china", "2015-08 China devaluation", date("2015-08-17"), date("2015-08-25")},           // -12.1 / -11.2 %
	{"2018-02-volmageddon", "2018-02 Volmageddon", date("2018-01-26"), date("2018-02-08")},           // -10.2 / -10.2 %
	{"2018-12-q4", "2018-12 Q4 sell-off", date("2018-12-03"), date("2018-12-24")},                    // -16.5 / -15.7 %
	{"2020-03-covid", "2020-03 COVID crash", date("2020-02-19"), date("2020-03-23")},                 // -28.0 / -33.9 %
	{"2022-bear", "2022 bear market", date("2022-01-03"), date("2022-10-12")},                        // -34.6 / -25.4 %
	{"2025-04-tariffs", "2025-04 tariff shock", date("2025-04-02"), date("2025-04-08")},              // -12.7 / -12.1 %
}

// IndexProxy maps an executor book to the index its episodes are measured on.
var IndexProxy = map[string]string{"nsdq100": "^NDX", "spx500": "^GSPC"}

// IndexEpisodePath is the index's cumulative move through the episode.
func IndexEpisodePath(closes []varmodel.Close, e IndexEpisode) ([]varmodel.PathPoint, bool) {
	return varmodel.EpisodePath(closes, e.Episode())
}

// IndexEpisodePaths computes every episode's path from index closes; a nil
// path means the index has no close in the window.
func IndexEpisodePaths(closes []varmodel.Close) map[string][]varmodel.PathPoint {
	out := map[string][]varmodel.PathPoint{}
	for _, e := range IndexEpisodes {
		p, ok := IndexEpisodePath(closes, e)
		if !ok {
			p = nil
		}
		out[e.ID] = p
	}
	return out
}

// ShockResult is one hypothetical move.
type ShockResult struct {
	Move     float64
	Loss     float64 // on the net exposure, floored at 0
	CloseOut bool    // the move reaches CloseOutMove on a long book
}

// EpisodeResult is one replay.
type EpisodeResult struct {
	ID, Name  string
	Base, End time.Time
	Loss      float64            // worst common day, floored at 0
	OK        bool               // every book had a path
	Troughs   map[string]float64 // per book: the worst cumulative move
	CloseOut  bool               // a long book's trough reached CloseOutMove
}

// Stress is the scenario set on one set of exposures.
type Stress struct {
	Net, Gross float64
	Shocks     []ShockResult
	Episodes   []EpisodeResult
	Worst      string  // episode id with the largest loss
	WorstLoss  float64 // its loss
	// ReverseStressMove is the uniform move whose loss equals capital; OK
	// is false when a long book cannot lose that much (capital > net) or
	// there is no position.
	ReverseStressMove float64
	ReverseStressOK   bool
	// CloseOutLoss is the loss on the gross exposure at CloseOutMove.
	CloseOutLoss float64
}

// StressTest replays Shocks and IndexEpisodes on exposures (book -> signed
// USD) with paths[book][episode id] (from IndexEpisodePaths on the book's
// index, or KnownIndexEpisodePaths[IndexProxy[book]]).
func StressTest(exposures map[string]float64, paths map[string]map[string][]varmodel.PathPoint, capital float64) Stress {
	var s Stress
	longs := map[string]bool{}
	for b, e := range exposures {
		s.Net += e
		s.Gross += math.Abs(e)
		if e > 0 {
			longs[b] = true
		}
	}
	for _, m := range Shocks {
		s.Shocks = append(s.Shocks, ShockResult{Move: m, Loss: varmodel.ShockLoss(s.Net, m), CloseOut: len(longs) > 0 && m <= CloseOutMove})
	}
	s.CloseOutLoss = varmodel.ShockLoss(s.Gross, CloseOutMove)
	s.ReverseStressMove, s.ReverseStressOK = varmodel.ReverseStressMove(s.Net, capital)
	for _, e := range IndexEpisodes {
		r := EpisodeResult{ID: e.ID, Name: e.Name, Base: e.Base, End: e.End, Troughs: map[string]float64{}}
		per := map[string][]varmodel.PathPoint{}
		for b := range exposures {
			if p := paths[b][e.ID]; len(p) > 0 {
				per[b] = p
				r.Troughs[b] = varmodel.Trough(p)
				if longs[b] && r.Troughs[b] <= CloseOutMove {
					r.CloseOut = true
				}
			}
		}
		r.Loss, r.OK = varmodel.EpisodeLoss(exposures, per)
		if r.OK && r.Loss > s.WorstLoss {
			s.Worst, s.WorstLoss = e.ID, r.Loss
		}
		s.Episodes = append(s.Episodes, r)
	}
	sort.SliceStable(s.Episodes, func(i, j int) bool { return s.Episodes[i].Base.Before(s.Episodes[j].Base) })
	return s
}
