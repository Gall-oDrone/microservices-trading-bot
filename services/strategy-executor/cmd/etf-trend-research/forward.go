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

// Forward evaluation of FORWARD-TEST-PREREGISTRATION-SMA50-ETF-*.md (§5).
// It runs on eToro's own ETF bars (a fresh cmd/etoro-spike capture:
// daily_qqq.csv / daily_spy.csv) with the frozen costs passed as flags:
// commission in bps per order and the bid/ask (primary), both x2
// (pessimistic). Rule and hold use the same bars, so neither includes cash
// dividends; the Yahoo total-return hold over the same dates is reported
// next to them (E-H2 is judged on eToro bars, as §4 says).

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
	Rows       []row        `json:"rows"` // etf_hold, etf_trend_self, etf_trend_index (index = own-close signal too here; kept for shape)
	YahooTRPct *float64     `json:"yahoo_total_return_hold_pct,omitempty"`
	Hyp        []forwardHyp `json:"hypotheses"`
	Note       string       `json:"note,omitempty"`
}

type forwardETF struct {
	Symbol    string            `json:"symbol"`
	LastBar   string            `json:"last_bar"`
	Scenarios []forwardScenario `json:"scenarios"`
}

type forwardReport struct {
	Schema      string       `json:"schema"`
	GeneratedAt string       `json:"generated_at"`
	Window      string       `json:"window"`
	Params      params       `json:"params"`
	ETFs        []forwardETF `json:"etfs"`
}

func runForward(spec, etoroDir, out string, load func(string) []yahoodaily.Row, p params) error {
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
		return fmt.Errorf("-forward needs -etoro (a fresh etoro-spike capture with daily_qqq.csv / daily_spy.csv)")
	}
	rep := forwardReport{Schema: "etf-trend-forward/v1", GeneratedAt: time.Now().UTC().Format(time.RFC3339), Window: spec, Params: p}
	w := os.Stdout
	fmt.Fprintf(w, "forward evaluation %s (costs %s)\n", spec, p.Status)
	for _, e := range p.ETFs {
		rows, err := bitsodaily.ReadCSV(filepath.Join(etoroDir, "daily_"+strings.ToLower(e.Symbol)+".csv"))
		if err != nil {
			return err
		}
		eb := etorodaily.Bars(rows)
		want := dailyrule.Trend(eb, p.SMA)
		hold := make([]bool, len(eb))
		for i := range hold {
			hold[i] = true
		}
		yahooTR := yahoodaily.TotalReturnBars(load(e.Symbol))
		fe := forwardETF{Symbol: e.Symbol, LastBar: rows[len(rows)-1].Date}
		for _, sc := range []struct {
			name string
			mult float64
		}{{"primary", 1}, {"pessimistic", 2}} {
			c := cfdsim.Costs{SpreadPerLeg: e.SpreadRTBps * sc.mult / 2 / 1e4, FeePerLeg: p.FeeBps * sc.mult / 1e4}
			fmt.Fprintf(w, "\n%s %s (eToro bars)\n", e.Symbol, sc.name)
			header(w)
			win := evalWindow(w, e.Symbol+" "+sc.name, eb, hold, want, want, from, to, c, rand.New(rand.NewSource(p.Seed)), p.Sims)
			fs := forwardScenario{Name: sc.name, From: win.From, To: win.To, Rows: win.Rows}
			lo, hi := span(eb, from, to)
			fs.Bars = hi - lo + 1
			if len(win.Rows) < 3 {
				fs.Note = "fewer than 20 forward bars: no verdict yet"
				fe.Scenarios = append(fe.Scenarios, fs)
				continue
			}
			// Yahoo total-return hold over the same dates, for the dividend gap.
			if ylo, yhi := span(yahooTR, from, to); yhi-ylo >= 20 {
				yh := make([]bool, len(yahooTR))
				for i := range yh {
					yh[i] = true
				}
				v := cfdsim.Simulate(yahooTR, yh, ylo, yhi, c).ReturnPct
				fs.YahooTRPct = &v
				fmt.Fprintf(w, "  (Yahoo total-return hold over the same dates: %.2f%%)\n", v)
			}
			h, t := win.Rows[0].R, win.Rows[1].R
			beat := 0.0
			if win.Rows[1].BeatPc != nil {
				beat = *win.Rows[1].BeatPc
			}
			fs.RoundTrips = t.RoundTrips
			fs.Hyp = []forwardHyp{
				{"E-H1", "trend max drawdown < ETF hold max drawdown", t.MaxDDPct, h.MaxDDPct, t.MaxDDPct < h.MaxDDPct},
				{"E-H2", "trend return > ETF hold return (same bars, after commission)", t.ReturnPct, h.ReturnPct, t.ReturnPct > h.ReturnPct},
				{"E-H3", "trend beats >= 95% of random same-trip strategies", beat, 95, beat >= 95},
			}
			for _, hy := range fs.Hyp {
				fmt.Fprintf(w, "  %-5s %-5v trend %8.2f vs %8.2f  %s\n", hy.ID, hy.Pass, hy.Trend, hy.Bench, hy.Criteria)
			}
			fe.Scenarios = append(fe.Scenarios, fs)
		}
		rep.ETFs = append(rep.ETFs, fe)
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	return os.WriteFile(filepath.Join(out, "forward.json"), append(b, '\n'), 0o644)
}
