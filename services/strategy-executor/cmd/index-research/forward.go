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
	"bitso-trading-platform/shared/pkg/yahoodaily"
)

// Forward evaluation of FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-*.md.
// It runs on eToro's own CFD bars (a fresh cmd/etoro-spike capture), with
// the frozen costs passed as flags: CFD spread round trip and the flat
// overnight fee (primary), spread x2 and fee x1.5 (pessimistic); the ETF
// benchmark pays its fixed commission on the frozen size.

type forwardHyp struct {
	ID       string  `json:"id"`
	Criteria string  `json:"criteria"`
	Trend    float64 `json:"trend"`
	Bench    float64 `json:"benchmark"`
	Pass     bool    `json:"pass"`
}

type forwardScenario struct {
	Name       string       `json:"name"`
	Bars       int          `json:"bars"`
	From       string       `json:"from"`
	To         string       `json:"to"`
	RoundTrips int          `json:"round_trips"`
	Rows       []ruleRow    `json:"rows"`
	Hyp        []forwardHyp `json:"hypotheses"`
	Note       string       `json:"note,omitempty"`
}

type forwardPair struct {
	CFD       string            `json:"cfd"`
	ETF       string            `json:"etf"`
	LastBar   string            `json:"last_bar"`
	Scenarios []forwardScenario `json:"scenarios"`
}

type forwardReport struct {
	Schema      string        `json:"schema"`
	GeneratedAt string        `json:"generated_at"`
	Window      string        `json:"window"`
	Params      params        `json:"params"`
	Pairs       []forwardPair `json:"pairs"`
}

func runForward(spec, etoroDir, out string, load func(string) []yahoodaily.Row, irx yahoodaily.Series, p params) error {
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
	if etoroDir == "" {
		return fmt.Errorf("-forward needs -etoro (a fresh etoro-spike capture)")
	}
	rep := forwardReport{Schema: "index-cfd-forward/v1", GeneratedAt: time.Now().UTC().Format(time.RFC3339), Window: spec, Params: p}
	w := os.Stdout
	fmt.Fprintf(w, "forward evaluation %s (costs %s)\n", spec, p.CostsStatus)
	for _, pr := range p.Pairs {
		rows, err := bitsodaily.ReadCSV(filepath.Join(etoroDir, "daily_"+strings.ToLower(pr.CFD)+".csv"))
		if err != nil {
			return err
		}
		eb := etorodaily.Bars(rows)
		ew := dailyrule.Trend(eb, p.SMA)
		etf := yahoodaily.TotalReturnBars(load(pr.ETF))
		etfW := alignWant(eb, ew, etf)
		hold := func(n int) []bool {
			h := make([]bool, n)
			for i := range h {
				h[i] = true
			}
			return h
		}
		fp := forwardPair{CFD: pr.CFD, ETF: pr.ETF, LastBar: rows[len(rows)-1].Date}
		for _, sc := range []struct {
			name              string
			spread, overnight float64
		}{{"primary", 1, 1}, {"pessimistic", 2, 1.5}} {
			c := costSet{
				cfd: cfdsim.Costs{SpreadPerLeg: pr.SpreadRTBps * sc.spread / 2 / 1e4, NightlyRate: cfdsim.FixedNightly(p.OvernightBps * sc.overnight / 1e4)},
				etf: cfdsim.Costs{SpreadPerLeg: pr.ETFSpreadRTBps / 2 / 1e4, FeePerLeg: p.ETFFeeUSD / p.SizeUSD},
			}
			fmt.Fprintf(w, "\n%s %s\n", pr.CFD, sc.name)
			header(w)
			win := evalWindow(w, pr.CFD+" "+sc.name, eb, ew, hold(len(eb)), etf, etfW, hold(len(etf)), from, to, c, rand.New(rand.NewSource(p.Seed)), p.Sims)
			fs := forwardScenario{Name: sc.name, From: win.From, To: win.To, Rows: win.Rows}
			lo, hi := span(eb, from, to)
			fs.Bars = hi - lo + 1
			if len(win.Rows) < 4 {
				fs.Note = "fewer than 20 forward bars: no verdict yet"
				fp.Scenarios = append(fp.Scenarios, fs)
				continue
			}
			h, t, e := win.Rows[0].R, win.Rows[1].R, win.Rows[2].R
			beat := 0.0
			if win.Rows[1].BeatPc != nil {
				beat = *win.Rows[1].BeatPc
			}
			fs.RoundTrips = t.RoundTrips
			fs.Hyp = []forwardHyp{
				{"H1", "trend max drawdown < CFD hold max drawdown", t.MaxDDPct, h.MaxDDPct, t.MaxDDPct < h.MaxDDPct},
				{"H2", "trend return > CFD hold return (same instrument, after spread and financing)", t.ReturnPct, h.ReturnPct, t.ReturnPct > h.ReturnPct},
				{"H2b", "trend return > " + pr.ETF + " hold return (dividends, commissions)", t.ReturnPct, e.ReturnPct, t.ReturnPct > e.ReturnPct},
				{"H3", "trend beats >= 95% of random same-trip strategies", beat, 95, beat >= 95},
			}
			for _, hy := range fs.Hyp {
				fmt.Fprintf(w, "  %-4s %-5v trend %8.2f vs %8.2f  %s\n", hy.ID, hy.Pass, hy.Trend, hy.Bench, hy.Criteria)
			}
			fp.Scenarios = append(fp.Scenarios, fs)
		}
		rep.Pairs = append(rep.Pairs, fp)
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	return os.WriteFile(filepath.Join(out, "forward.json"), append(b, '\n'), 0o644)
}
