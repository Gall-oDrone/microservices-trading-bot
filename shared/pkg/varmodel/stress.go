package varmodel

import (
	"math"
	"sort"
	"time"
)

// Expected shortfall and stress scenarios (plan §6.4.9).
//
// Expected shortfall (ES) at 97.5 % is the Basel FRTB replacement for 99 %
// VaR: the average loss on the worst 2.5 % of days, so it sees how bad the
// tail is, not only where it starts. For a normal distribution ES 97.5 % is
// about VaR 99 % (2.338 vs 2.326 sigma); on fat-tailed returns the
// historical ES is larger, which is the point.
//
// Stress scenarios answer "what would today's positions lose if a known
// crisis happened again, or if spot simply dropped by X %": losses VaR does
// not describe because they lie beyond its confidence level and horizon.
// Historical episodes are replayed on the current exposure from each
// book's own Bitso closes: the loss is the worst point of the episode,
// measured from the close before it started (the position is assumed held
// throughout, i.e. it could not be exited). Hypothetical shocks move every
// book's spot by the same ratio at once.

// ESConfidence is the expected-shortfall level (Basel FRTB).
const ESConfidence = 0.975

// ZES is the normal ES multiplier at ESConfidence: phi(z) / (1 - c).
var ZES = func() float64 {
	z := math.Sqrt2 * math.Erfinv(2*ESConfidence-1)
	return math.Exp(-z*z/2) / math.Sqrt(2*math.Pi) / (1 - ESConfidence)
}()

// ExpectedShortfall is the historical ES of scenario P&Ls: the mean of the
// k worst losses with k = ceil(n x (1 - confidence)), floored at 0. ok is
// false when there are fewer than minScenarios.
func ExpectedShortfall(pnl []float64, confidence float64, minScenarios int) (float64, bool) {
	n := len(pnl)
	if n == 0 || n < minScenarios {
		return 0, false
	}
	losses := make([]float64, n)
	for i, v := range pnl {
		losses[i] = -v
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(losses)))
	k := int(math.Ceil(float64(n)*(1-confidence) - 1e-9))
	if k < 1 {
		k = 1
	}
	sum := 0.0
	for _, l := range losses[:k] {
		sum += l
	}
	return math.Max(sum/float64(k), 0), true
}

// Episode is a historical stress window, in Bitso's (Mexico City) daily
// bar dates, both ends included.
type Episode struct {
	ID         string // metric label
	Name       string
	Start, End time.Time
}

func date(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// Episodes are the crypto crises replayed on today's positions. Windows
// run from the last calm close to the trough on Bitso's btc_mxn and
// btc_usd closes (stress_real_data_test.go pins the moves). btc_usd has no
// Bitso history before 2020-04-24; a book without data for an episode is
// proxied by a book with the same base asset.
var Episodes = []Episode{
	{"2018-01-crash", "2018-01 post-peak crash", date("2018-01-14"), date("2018-02-06")},     // btc_mxn -62 %
	{"2018-11-hash-war", "2018-11 hash war", date("2018-11-14"), date("2018-11-25")},         // btc_mxn -41 %
	{"2020-03-covid", "2020-03 COVID crash", date("2020-03-08"), date("2020-03-16")},         // btc_mxn -36 %
	{"2021-05-china-ban", "2021-05 China ban", date("2021-05-10"), date("2021-05-23")},       // -41 %
	{"2022-05-terra", "2022-05 Terra/LUNA", date("2022-05-05"), date("2022-05-12")},          // -29 / -30 %
	{"2022-06-celsius-3ac", "2022-06 Celsius/3AC", date("2022-06-10"), date("2022-06-18")},   // -36 / -38 %
	{"2022-11-ftx", "2022-11 FTX", date("2022-11-06"), date("2022-11-10")},                   // -21 %
	{"2024-08-carry-unwind", "2024-08 carry unwind", date("2024-08-02"), date("2024-08-05")}, // -12 / -17 %
}

// Base is the date of the close the episode is measured from.
func (e Episode) Base() time.Time { return e.Start.AddDate(0, 0, -1) }

// PathPoint is the simple return from the episode's base close to Date's.
type PathPoint struct {
	Date time.Time
	Cum  float64
}

// EpisodePath is the book's cumulative move through the episode. ok is
// false without the base close or without any close in the window (the
// book did not trade then).
func EpisodePath(closes []Close, e Episode) ([]PathPoint, bool) {
	base := 0.0
	var in []Close
	for _, c := range closes {
		d := day(c.Date)
		switch {
		case d.Equal(e.Base()):
			base = c.Price
		case !d.Before(e.Start) && !d.After(e.End) && c.Price > 0:
			in = append(in, Close{Date: d, Price: c.Price})
		}
	}
	if !(base > 0) || len(in) == 0 {
		return nil, false
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Date.Before(in[j].Date) })
	out := make([]PathPoint, len(in))
	for i, c := range in {
		out[i] = PathPoint{Date: c.Date, Cum: c.Price/base - 1}
	}
	return out, true
}

// Trough is the worst cumulative move on the path (0 if it never fell).
func Trough(path []PathPoint) float64 {
	t := 0.0
	for _, p := range path {
		t = math.Min(t, p.Cum)
	}
	return t
}

// EpisodeLoss is the worst loss of exposures (quote currency, signed) over
// the days on which every book has a path point: max over d of
// -sum(q x cum_d), floored at 0 (the base close itself is no loss). ok is
// false when a book has no path or the paths share no day.
func EpisodeLoss(exposures map[string]float64, paths map[string][]PathPoint) (float64, bool) {
	if len(exposures) == 0 {
		return 0, true
	}
	pnl := map[time.Time]float64{}
	count := map[time.Time]int{}
	for book, q := range exposures {
		p, ok := paths[book]
		if !ok || len(p) == 0 {
			return 0, false
		}
		for _, pt := range p {
			pnl[pt.Date] += q * pt.Cum
			count[pt.Date]++
		}
	}
	loss, found := 0.0, false
	for d, n := range count {
		if n == len(exposures) {
			found = true
			loss = math.Max(loss, -pnl[d])
		}
	}
	return loss, found
}

// ShockLoss is the loss of a net exposure when every book's spot moves by
// shock (e.g. -0.3), floored at 0.
func ShockLoss(net, shock float64) float64 {
	return math.Max(-net*shock, 0)
}

// ReverseStressMove is the uniform spot move whose loss on net equals
// capital (reverse stress testing: which move exhausts the capital?). A
// long needs a fall of capital/net and cannot lose more than net, so ok is
// false when capital exceeds net; a short is always reachable by a rise.
// ok is false for a flat position or no capital.
func ReverseStressMove(net, capital float64) (move float64, ok bool) {
	if net == 0 || !(capital > 0) {
		return 0, false
	}
	move = -capital / net
	return move, move >= -1
}
