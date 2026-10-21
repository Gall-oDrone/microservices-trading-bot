package cfdsim

import (
	"math"

	"bitso-trading-platform/shared/pkg/dailyrule"
)

// RotationResult is SimulateRotation's output: Result plus how the equity
// was split across assets and how often the rule switched.
type RotationResult struct {
	Result
	// Switches counts asset-to-asset moves (flat->asset and asset->flat are
	// round-trip legs counted in RoundTrips; an asset->asset move is one
	// switch and one round trip).
	Switches int `json:"switches"`
	// ExposureByAsset is the % of bars each asset was held.
	ExposureByAsset []float64 `json:"exposure_by_asset_pct"`
}

// SimulateRotation trades aligned bar series (same dates, see
// rotation.Align) with one position at a time: want[i] is the asset index
// to hold after bar i's close (or -1 for cash), filled at bars[k][i+1].Open.
// Costs are per asset. The fill model, the financing nights, the close-out
// flag and the metrics are those of Simulate, so with one asset and a
// bool->int mapping of want the two agree bit for bit
// (TestRotationMatchesSimulate).
//
// A switch from asset a to asset b at the same open closes a (its leg cost)
// and opens b (its leg cost) in the same bar, from the proceeds.
func SimulateRotation(bars [][]dailyrule.Bar, want []int, lo, hi int, costs []Costs) RotationResult {
	const start = 1.0
	na := len(bars)
	cash, units := start, 0.0
	held := -1
	peak, maxDD, trade, finance := start, 0.0, 0.0, 0.0
	trips, nights, switches := 0, 0, 0
	exposed := make([]int, na)
	prevEq := start
	minEq, closeOut, margin := start, false, 0.0
	var rets []float64

	for i := lo; i <= hi; i++ {
		desire := -1
		if i > 0 {
			desire = want[i-1]
		}
		if desire >= na {
			desire = -1
		}
		if desire != held {
			if held >= 0 {
				gross := units * bars[held][i].Open
				fee := gross * costs[held].leg()
				cash += gross - fee
				trade += fee
				units = 0
				trips++
			}
			if desire >= 0 {
				if held >= 0 {
					switches++
				}
				o := bars[desire][i].Open
				fee := cash * costs[desire].leg()
				units = (cash - fee) / o
				margin = cash - fee
				trade += fee
				cash = 0
			}
			held = desire
		}
		if held >= 0 {
			exposed[held]++
		}
		if held >= 0 && i < hi && costs[held].NightlyRate != nil {
			b := bars[held]
			n := int(math.Round(b[i+1].Date.Sub(b[i].Date).Hours() / 24))
			if n < 1 {
				n = 1
			}
			f := units * b[i].Close * costs[held].NightlyRate(b[i].Date) * float64(n)
			cash -= f
			finance += f
			nights += n
		}
		eq := cash
		if held >= 0 {
			eq += units * bars[held][i].Close
		}
		if eq < minEq {
			minEq = eq
		}
		if held >= 0 && costs[held].NightlyRate != nil && eq < 0.5*margin {
			closeOut = true
		}
		if eq > peak {
			peak = eq
		}
		if dd := (peak - eq) / peak; dd > maxDD {
			maxDD = dd
		}
		if i > lo && prevEq > 0 {
			rets = append(rets, eq/prevEq-1)
		}
		prevEq = eq
	}
	if held >= 0 {
		gross := units * bars[held][hi].Close
		fee := gross * costs[held].leg()
		cash += gross - fee
		trade += fee
		trips++
	}
	days := hi - lo + 1
	totalExposed := 0
	byAsset := make([]float64, na)
	for k, e := range exposed {
		totalExposed += e
		byAsset[k] = 100 * float64(e) / float64(days)
	}
	r := RotationResult{Result: Result{
		ReturnPct:      (cash/start - 1) * 100,
		RoundTrips:     trips,
		ExposurePct:    100 * float64(totalExposed) / float64(days),
		MaxDDPct:       maxDD * 100,
		TradeCostPct:   trade / start * 100,
		FinancePct:     finance / start * 100,
		NightsHeld:     nights,
		Bars:           days,
		SharpeAnn:      sharpe(rets),
		MinEquity:      minEq,
		MarginCloseOut: closeOut,
	}, Switches: switches, ExposureByAsset: byAsset}
	if days > 1 && cash > 0 {
		r.CAGRPct = (math.Pow(cash/start, 252/float64(days-1)) - 1) * 100
	}
	return r
}
