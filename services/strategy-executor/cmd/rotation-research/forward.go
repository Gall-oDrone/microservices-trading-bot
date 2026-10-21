package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/cfdsim"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/etorodaily"
	"bitso-trading-platform/shared/pkg/rotation"
)

// Forward evaluation of FORWARD-TEST-PREREGISTRATION-DUAL-MOMENTUM-ETF-*.md
// (§5), on eToro's own ETF bars (daily_qqq.csv / daily_spy.csv from a
// cmd/etoro-spike capture), ETF product only: the study ruled the CFD
// product out. Primary costs are the frozen commission and spreads;
// pessimistic doubles both. Rule and benchmarks use the same bars, so
// dividends are in neither leg (reported separately by the ETF cell).
//
// With -state it only prints the rule's state at the last closed bar
// (momentum of each asset, the target in force, the next decision date):
// the "state at registration" block of the pre-registration.

type fwdHyp struct {
	ID       string  `json:"id"`
	Criteria string  `json:"criteria"`
	Rule     float64 `json:"rule"`
	Bench    float64 `json:"benchmark"`
	Pass     bool    `json:"pass"`
}

type fwdScenario struct {
	Name       string   `json:"name"`
	Bars       int      `json:"bars"`
	From       string   `json:"from"`
	To         string   `json:"to"`
	Rows       []row    `json:"rows"`
	Hyp        []fwdHyp `json:"hypotheses"`
	Note       string   `json:"note,omitempty"`
	Decisions  int      `json:"month_end_decisions"`
	TargetPath []string `json:"targets_in_force,omitempty"`
}

type fwdReport struct {
	Schema      string        `json:"schema"`
	GeneratedAt string        `json:"generated_at"`
	Window      string        `json:"window"`
	Variant     variant       `json:"variant"`
	Params      params        `json:"params"`
	LastBar     string        `json:"last_bar"`
	Scenarios   []fwdScenario `json:"scenarios"`
}

type stateReport struct {
	Schema       string    `json:"schema"`
	LastBar      string    `json:"last_bar"`
	Variant      variant   `json:"variant"`
	LastDecision string    `json:"last_decision_bar"`
	Momentum     []float64 `json:"momentum_at_last_decision"`
	Hurdle       float64   `json:"hurdle"`
	Target       string    `json:"target_in_force"`
	NextDecision string    `json:"next_decision_bar_expected"`
	Closes       []float64 `json:"closes_last_bar"`
	Assets       []string  `json:"assets"`
}

var etfNames = []string{"QQQ", "SPY"}

func loadEtoroETF(dir string) ([][]dailyrule.Bar, string, error) {
	var out [][]dailyrule.Bar
	last := ""
	for _, s := range etfNames {
		rows, err := bitsodaily.ReadCSV(filepath.Join(dir, "daily_"+strings.ToLower(s)+".csv"))
		if err != nil {
			return nil, "", err
		}
		out = append(out, etorodaily.Bars(rows))
		last = rows[len(rows)-1].Date
	}
	return rotation.Align(out...), last, nil
}

func targetName(t int) string {
	if t < 0 {
		return "cash"
	}
	return etfNames[t]
}

func runState(etoroDir string, v variant, p params, irxHurdle func(time.Time) float64) error {
	bars, last, err := loadEtoroETF(etoroDir)
	if err != nil {
		return err
	}
	pts := rotation.Evaluate(bars, rotation.Params{Lookback: v.Lookback, Skip: v.Skip, Hurdle: irxHurdle})
	n := len(bars[0])
	st := stateReport{Schema: "dual-momentum-state/v1", LastBar: last, Variant: v, Assets: etfNames,
		Target: targetName(pts[n-1].Target)}
	for k := range bars {
		st.Closes = append(st.Closes, bars[k][n-1].Close)
	}
	for i := n - 1; i >= 0; i-- {
		if pts[i].Decision {
			st.LastDecision = bars[0][i].Date.Format("2006-01-02")
			st.Momentum, st.Hurdle = pts[i].Momentum, pts[i].Hurdle
			break
		}
	}
	// Next decision: the last NYSE trading day of the current month of the last bar.
	d := bars[0][n-1].Date
	first := time.Date(d.Year(), d.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	for x := first.AddDate(0, 0, -1); ; x = x.AddDate(0, 0, -1) {
		if wd := x.Weekday(); wd != time.Saturday && wd != time.Sunday {
			st.NextDecision = x.Format("2006-01-02") + " (last weekday of the month; a holiday moves it earlier)"
			break
		}
	}
	// Warm-up check.
	if !pts[n-1].Warm {
		return fmt.Errorf("rule not warm at %s: %d aligned bars, need %d", last, n, v.Lookback+v.Skip+1)
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	fmt.Println(string(b))
	return nil
}

func runForward(spec, etoroDir, out string, v variant, p params, etfCosts []cfdsim.Costs, irxHurdle func(time.Time) float64, sims int, seed int64) error {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("-forward wants FROM:TO, got %q", spec)
	}
	from, err := time.Parse("2006-01-02", parts[0])
	if err != nil {
		return err
	}
	to, err := time.Parse("2006-01-02", parts[1])
	if err != nil {
		return err
	}
	bars, last, err := loadEtoroETF(etoroDir)
	if err != nil {
		return err
	}
	want := rotation.Targets(bars, rotation.Params{Lookback: v.Lookback, Skip: v.Skip, Hurdle: irxHurdle})
	rep := fwdReport{Schema: "dual-momentum-forward/v1", GeneratedAt: time.Now().UTC().Format(time.RFC3339), Window: spec, Variant: v, Params: p, LastBar: last}
	w := os.Stdout
	fmt.Fprintf(w, "forward evaluation %s, variant %s, ETF product, eToro bars to %s (costs %s)\n", spec, v.Name, last, p.CostsStatus)
	lo, hi := span(bars[0], from, to)
	for _, sc := range []struct {
		name string
		mult float64
	}{{"primary", 1}, {"pessimistic", 2}} {
		costs := make([]cfdsim.Costs, len(etfCosts))
		for k, c := range etfCosts {
			costs[k] = cfdsim.Costs{SpreadPerLeg: c.SpreadPerLeg * sc.mult, FeePerLeg: c.FeePerLeg * sc.mult}
		}
		fs := fwdScenario{Name: sc.name, From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Bars: hi - lo + 1}
		if hi-lo < 20 {
			fs.Note = "fewer than 20 forward bars: no verdict yet"
			rep.Scenarios = append(rep.Scenarios, fs)
			fmt.Fprintf(w, "\n%s: %s\n", sc.name, fs.Note)
			continue
		}
		fmt.Fprintf(w, "\n%s\n", sc.name)
		header(w)
		rot := cfdsim.SimulateRotation(bars, want, lo, hi, costs)
		rows := []row{toRow(v.Name+"_etf", rot, nil)}
		var hold [2]cfdsim.RotationResult
		for k := range bars {
			h := make([]int, len(bars[k]))
			for i := range h {
				h[i] = k
			}
			hold[k] = cfdsim.SimulateRotation(bars, h, lo, hi, costs)
			rows = append(rows, toRow(strings.ToLower(etfNames[k])+"_hold", hold[k], nil))
		}
		// SMA50 on QQQ with the same costs (H4).
		tr := dailyrule.Trend(bars[0], p.SMA)
		sw := make([]int, len(tr))
		for i, t := range tr {
			sw[i] = -1
			if t {
				sw[i] = 0
			}
		}
		sma := cfdsim.SimulateRotation(bars, sw, lo, hi, costs)
		rows = append(rows, toRow("sma50_qqq", sma, nil))
		// Permutation baseline on the primary variant.
		st := study{p: p, rng: rand.New(rand.NewSource(seed)), primary: v}
		st.p.Sims = sims
		if b, ok := st.randomBeat(bars, want, lo, hi, costs, rot.ReturnPct); ok {
			rows[0].BeatPct = &b
		}
		for _, r := range rows {
			printRow(w, sc.name, r)
		}
		for i := lo; i <= hi; i++ {
			if rotation.IsMonthEnd(bars[0], i) {
				fs.Decisions++
			}
			if i == lo || want[i-1] != want[i-2] {
				fs.TargetPath = append(fs.TargetPath, bars[0][i].Date.Format("2006-01-02")+":"+targetName(want[i-1]))
			}
		}
		beat := 0.0
		if rows[0].BeatPct != nil {
			beat = *rows[0].BeatPct
		}
		fs.Rows = rows
		fs.Hyp = []fwdHyp{
			{"H1", "rotation max drawdown < SPY hold max drawdown", rot.MaxDDPct, hold[1].MaxDDPct, rot.MaxDDPct < hold[1].MaxDDPct},
			{"H2", "rotation return > SPY hold return", rot.ReturnPct, hold[1].ReturnPct, rot.ReturnPct > hold[1].ReturnPct},
			{"H2b", "rotation return > QQQ hold return", rot.ReturnPct, hold[0].ReturnPct, rot.ReturnPct > hold[0].ReturnPct},
			{"H3", "rotation beats >= 95% of permutations of its monthly targets (n/a when one target all window)", beat, 95, rows[0].BeatPct != nil && beat >= 95},
			{"H4", "rotation return > SMA50 on QQQ (same costs)", rot.ReturnPct, sma.ReturnPct, rot.ReturnPct > sma.ReturnPct},
		}
		for _, hy := range fs.Hyp {
			fmt.Fprintf(w, "  %-4s %-5v rule %8.2f vs %8.2f  %s\n", hy.ID, hy.Pass, hy.Rule, hy.Bench, hy.Criteria)
		}
		rep.Scenarios = append(rep.Scenarios, fs)
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	return os.WriteFile(filepath.Join(out, "forward.json"), append(b, '\n'), 0o644)
}
