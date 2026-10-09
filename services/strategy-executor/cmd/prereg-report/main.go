// Command prereg-report turns the registered evaluation runs into the
// pre-registered verdicts (H1/H2/H3) for the two SMA50 forward tests, so
// the interim look (2027-03-26) and the primary evaluation (2027-09-26)
// are mechanical (plan §6.4.13).
//
// It does not compute the strategy: it reads the daily-research -json
// reports produced by the procedure in each pre-registration (run by
// scripts/prereg-evaluation.sh) and applies the frozen pass criteria:
//
//	btc_mxn (FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md, window from 2026-09-27)
//	  H1 trend max drawdown < hold max drawdown
//	  H2 trend return > hold return
//	  H3 trend beats >= 95% of 2,000 random same-trade-count strategies (seed 1)
//	btc_usd (FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md, window from 2026-09-29)
//	  H1 trend max drawdown (USD) < hold btc_usd max drawdown
//	  H2 trend return in MXN after two conversions > hold btc_mxn return
//	  H3 as above, on btc_usd
//
// The MXN conversion is scripts/research/mxn-terms.py's formula:
// (1 + r_usd) · fx(end)/fx(start) · (1 − conv)² − 1, fx = btc_mxn close /
// btc_usd close on the same Mexico City day, fx(start) the close before the
// window, fx(end) the last close at or before its end.
//
// Primary costs decide; secondary costs are reported. Before the end date
// the report is labelled "as of" its last bar and decides nothing; the
// interim look decides nothing either. Exit status: 0 on success, 1 on
// errors, 2 on bad usage.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/prereg"
)

const schema = prereg.Schema

// The frozen evaluation dates (both pre-registrations).
const (
	interimDate = prereg.InterimDate
	finalDate   = prereg.FinalDate
	h3Threshold = 95.0
)

// researchReport is the subset of daily-research's research-run/v1 used here.
type researchReport struct {
	Schema string `json:"schema"`
	Data   struct {
		Prices string `json:"prices"`
		Last   string `json:"last"`
	} `json:"data"`
	Costs struct {
		BuyBPS      float64 `json:"buy_bps"`
		SellBPS     float64 `json:"sell_bps"`
		SlippageBPS float64 `json:"slippage_bps"`
	} `json:"costs"`
	Windows []struct {
		From    string       `json:"from"`
		To      string       `json:"to"`
		Bars    int          `json:"bars"`
		Note    string       `json:"note"`
		Results []ruleResult `json:"results"`
	} `json:"windows"`
}

type ruleResult struct {
	Rule       string  `json:"rule"`
	ReturnPct  float64 `json:"return_pct"`
	RoundTrips int     `json:"round_trips"`
	MaxDDPct   float64 `json:"max_dd_pct"`
	Random     *struct {
		Sims    int     `json:"sims"`
		BeatPct float64 `json:"beat_pct"`
	} `json:"random"`
}

// The report types live in shared/pkg/prereg so ui-api reads the same schema.
type (
	Hypothesis  = prereg.Hypothesis
	Scenario    = prereg.Scenario
	BookVerdict = prereg.BookVerdict
	Report      = prereg.Report
)

func main() {
	phase := flag.String("phase", "as-of", "as-of (before a frozen date: report only), interim ("+interimDate+", report only) or final ("+finalDate+")")
	mxnP := flag.String("mxn-primary", "", "daily-research -json on btc_mxn, its window, primary costs (60+10)")
	mxnS := flag.String("mxn-secondary", "", "same, secondary costs (78+10)")
	usdP := flag.String("usd-primary", "", "daily-research -json on btc_usd, its window, primary costs (30+10)")
	usdS := flag.String("usd-secondary", "", "same, secondary costs (36+10)")
	benchP := flag.String("mxn-on-usd-primary", "", "daily-research -json on btc_mxn over the btc_usd window, primary costs (the btc_usd H2 benchmark)")
	benchS := flag.String("mxn-on-usd-secondary", "", "same, secondary costs")
	usdCSV := flag.String("usd-csv", "", "btc_usd daily CSV (bitsodaily) for the implied USD/MXN")
	mxnCSV := flag.String("mxn-csv", "", "btc_mxn daily CSV (bitsodaily)")
	convP := flag.Float64("conv-primary-bps", 60, "MXN<->USD conversion per leg, primary")
	convS := flag.Float64("conv-secondary-bps", 78, "MXN<->USD conversion per leg, secondary")
	outJSON := flag.String("out-json", "", "write the JSON here")
	outMD := flag.String("out-md", "", "write the markdown here (default stdout)")
	flag.Parse()
	if flag.NArg() > 0 || (*phase != "as-of" && *phase != "interim" && *phase != "final") || *mxnP == "" || *usdP == "" || *benchP == "" || *usdCSV == "" || *mxnCSV == "" {
		fmt.Fprintln(os.Stderr, "prereg-report: -phase as-of|interim|final and -mxn-primary, -usd-primary, -mxn-on-usd-primary, -usd-csv, -mxn-csv are required")
		flag.Usage()
		os.Exit(2)
	}
	fx, err := impliedFX(*usdCSV, *mxnCSV)
	if err != nil {
		fail(err)
	}
	rep, err := build(*phase, inputs{mxnP: *mxnP, mxnS: *mxnS, usdP: *usdP, usdS: *usdS, benchP: *benchP, benchS: *benchS}, fx, *convP, *convS)
	if err != nil {
		fail(err)
	}
	rep.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	if *outJSON != "" {
		data, _ := json.MarshalIndent(rep, "", "  ")
		if err := os.WriteFile(*outJSON, append(data, '\n'), 0o644); err != nil {
			fail(err)
		}
	}
	w := io.Writer(os.Stdout)
	if *outMD != "" {
		f, err := os.Create(*outMD)
		if err != nil {
			fail(err)
		}
		defer f.Close()
		w = f
	}
	writeMarkdown(w, rep)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "prereg-report:", err)
	os.Exit(1)
}

type inputs struct{ mxnP, mxnS, usdP, usdS, benchP, benchS string }

func readReport(path string) (*researchReport, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r researchReport
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if r.Schema != "research-run/v1" {
		return nil, fmt.Errorf("%s: schema %q, want research-run/v1", path, r.Schema)
	}
	if len(r.Windows) != 1 {
		return nil, fmt.Errorf("%s: %d windows, want exactly the forward window", path, len(r.Windows))
	}
	return &r, nil
}

// fxSeries maps Mexico City dates to the implied USD/MXN (btc_mxn / btc_usd close).
type fxSeries struct {
	days []string
	fx   map[string]float64
}

func impliedFX(usdCSV, mxnCSV string) (fxSeries, error) {
	u, err := bitsodaily.ReadCSV(usdCSV)
	if err != nil {
		return fxSeries{}, err
	}
	m, err := bitsodaily.ReadCSV(mxnCSV)
	if err != nil {
		return fxSeries{}, err
	}
	mx := map[string]float64{}
	for _, r := range m {
		mx[r.Date] = r.Close
	}
	s := fxSeries{fx: map[string]float64{}}
	for _, r := range u {
		if c, ok := mx[r.Date]; ok && r.Close > 0 {
			s.fx[r.Date] = c / r.Close
			s.days = append(s.days, r.Date)
		}
	}
	sort.Strings(s.days)
	return s, nil
}

// before returns the last fx strictly before d; atOrBefore the last at or before d.
func (s fxSeries) before(d string) (float64, bool) {
	i := sort.SearchStrings(s.days, d) - 1
	if i < 0 {
		return 0, false
	}
	return s.fx[s.days[i]], true
}

func (s fxSeries) atOrBefore(d string) (float64, string, bool) {
	i := sort.SearchStrings(s.days, d)
	if i < len(s.days) && s.days[i] == d {
		return s.fx[d], d, true
	}
	if i-1 < 0 {
		return 0, "", false
	}
	return s.fx[s.days[i-1]], s.days[i-1], true
}

func rule(r *researchReport, name string) (ruleResult, bool) {
	for _, x := range r.Windows[0].Results {
		if x.Rule == name {
			return x, true
		}
	}
	return ruleResult{}, false
}

func h3(trend ruleResult, label string) Hypothesis {
	h := Hypothesis{ID: "H3", Criteria: "trend beats >= 95% of 2,000 random same-trade-count strategies " + label, Bench: h3Threshold}
	if trend.Random == nil {
		h.Note = "no completed round trip yet: the random baseline is undefined, so H3 cannot pass"
		return h
	}
	h.Trend, h.Pass = trend.Random.BeatPct, trend.Random.BeatPct >= h3Threshold
	return h
}

func scenarioMXN(name string, r *researchReport) (Scenario, error) {
	tr, ok1 := rule(r, "trend_sma50")
	bh, ok2 := rule(r, "buy_and_hold")
	if !ok1 || !ok2 {
		return Scenario{}, fmt.Errorf("btc_mxn %s: trend_sma50 or buy_and_hold missing", name)
	}
	w := r.Windows[0]
	return Scenario{Name: name, LegBps: r.Costs.BuyBPS + r.Costs.SlippageBPS, From: w.From, To: w.To, Bars: w.Bars, RoundTrips: tr.RoundTrips,
		Hyp: []Hypothesis{
			{ID: "H1", Criteria: "trend max drawdown < hold max drawdown (%)", Trend: tr.MaxDDPct, Bench: bh.MaxDDPct, Pass: tr.MaxDDPct < bh.MaxDDPct},
			{ID: "H2", Criteria: "trend return > hold return (%)", Trend: tr.ReturnPct, Bench: bh.ReturnPct, Pass: tr.ReturnPct > bh.ReturnPct},
			h3(tr, "(btc_mxn, seed 1)"),
		}}, nil
}

func scenarioUSD(name string, r, bench *researchReport, fx fxSeries, convBps float64) (Scenario, error) {
	tr, ok1 := rule(r, "trend_sma50")
	bh, ok2 := rule(r, "buy_and_hold")
	if !ok1 || !ok2 {
		return Scenario{}, fmt.Errorf("btc_usd %s: trend_sma50 or buy_and_hold missing", name)
	}
	mh, ok := rule(bench, "buy_and_hold")
	if !ok {
		return Scenario{}, fmt.Errorf("btc_mxn on the btc_usd window (%s): buy_and_hold missing", name)
	}
	w := r.Windows[0]
	if bw := bench.Windows[0]; bw.From != w.From || bw.To != w.To {
		return Scenario{}, fmt.Errorf("%s: benchmark window %s..%s differs from btc_usd's %s..%s", name, bw.From, bw.To, w.From, w.To)
	}
	f0, ok := fx.before(w.From)
	f1, d1, ok1 := fx.atOrBefore(w.To)
	if !ok || !ok1 {
		return Scenario{}, fmt.Errorf("%s: no implied USD/MXN around %s..%s", name, w.From, w.To)
	}
	conv := convBps / 1e4
	trMXN := ((1+tr.ReturnPct/100)*f1/f0*(1-conv)*(1-conv) - 1) * 100
	return Scenario{Name: name, LegBps: r.Costs.BuyBPS + r.Costs.SlippageBPS, From: w.From, To: w.To, Bars: w.Bars, RoundTrips: tr.RoundTrips,
		Hyp: []Hypothesis{
			{ID: "H1", Criteria: "trend max drawdown (USD) < hold btc_usd max drawdown (%)", Trend: tr.MaxDDPct, Bench: bh.MaxDDPct, Pass: tr.MaxDDPct < bh.MaxDDPct},
			{ID: "H2", Criteria: fmt.Sprintf("trend return in MXN after two %.0f bps conversions > hold btc_mxn return (%%)", convBps), Trend: trMXN, Bench: mh.ReturnPct, Pass: trMXN > mh.ReturnPct,
				Note: fmt.Sprintf("USD return %.2f %%, USD/MXN %.4f -> %.4f (%s)", tr.ReturnPct, f0, f1, d1)},
			h3(tr, "(btc_usd, seed 1)"),
		}}, nil
}

func passes(s Scenario) map[string]bool {
	m := map[string]bool{}
	for _, h := range s.Hyp {
		m[h.ID] = h.Pass
	}
	return m
}

// readingMXN and readingUSD quote each pre-registration's "how to read the
// outcome"; combinations it does not name are reported as such.
func readingMXN(p map[string]bool) string {
	switch {
	case p["H1"] && p["H2"] && p["H3"]:
		return "All three pass: worth designing a live strategy and a small, capped Stage soak."
	case p["H1"] && !p["H2"] && !p["H3"]:
		return "H1 only: a drawdown tool. Only useful if the product objective is changed to risk reduction."
	case !p["H1"] && !p["H2"]:
		return "None pass, or only H3: close the line of work."
	}
	return "A combination the pre-registration does not name: report it; no rule change."
}

func readingUSD(p map[string]bool) string {
	switch {
	case p["H1"] && p["H2"]:
		return "H1 and H2 pass: worth designing a live btc_usd strategy and a small, capped Stage soak, whatever H3 says."
	case !p["H2"] && p["H1"]:
		return "H1 only: a drawdown tool (and H2 fails: close the btc_usd variant)."
	case !p["H2"]:
		return "H2 fails: close the btc_usd variant."
	}
	return "A combination the pre-registration does not name: report it; no rule change."
}

func build(phase string, in inputs, fx fxSeries, convP, convS float64) (Report, error) {
	rep := Report{Schema: schema, Phase: phase, Books: []BookVerdict{}}
	paths := []string{in.mxnP, in.mxnS, in.usdP, in.usdS, in.benchP, in.benchS}
	reps := make([]*researchReport, len(paths))
	for i, p := range paths {
		r, err := readReport(p)
		if err != nil {
			return rep, err
		}
		reps[i] = r
	}
	mP, mS, uP, uS, bP, bS := reps[0], reps[1], reps[2], reps[3], reps[4], reps[5]
	rep.AsOf = mP.Data.Last
	if uP.Data.Last < rep.AsOf {
		rep.AsOf = uP.Data.Last
	}

	mx := BookVerdict{Book: "btc_mxn", Prereg: "FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md"}
	for _, x := range []struct {
		name string
		r    *researchReport
	}{{"primary", mP}, {"secondary", mS}} {
		if x.r == nil {
			continue
		}
		s, err := scenarioMXN(x.name, x.r)
		if err != nil {
			return rep, err
		}
		mx.Scenarios = append(mx.Scenarios, s)
	}
	mx.Reading = readingMXN(passes(mx.Scenarios[0]))

	us := BookVerdict{Book: "btc_usd", Prereg: "FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md"}
	for _, x := range []struct {
		name    string
		r, b    *researchReport
		convBps float64
	}{{"primary", uP, bP, convP}, {"secondary", uS, bS, convS}} {
		if x.r == nil || x.b == nil {
			continue
		}
		s, err := scenarioUSD(x.name, x.r, x.b, fx, x.convBps)
		if err != nil {
			return rep, err
		}
		us.Scenarios = append(us.Scenarios, s)
	}
	us.Reading = readingUSD(passes(us.Scenarios[0]))
	rep.Books = append(rep.Books, mx, us)

	rep.Decides = phase == "final" && rep.AsOf >= finalDate
	switch {
	case phase == "final" && !rep.Decides:
		rep.Note = fmt.Sprintf("Phase final, but the data end on %s, before %s: nothing is decided until the window is complete.", rep.AsOf, finalDate)
	case phase == "final":
		rep.Note = "Primary evaluation: the primary-cost verdicts decide, as frozen in each pre-registration."
	case phase == "interim":
		rep.Note = "Interim look: report only. No decision is taken on it, in either direction."
	default:
		rep.Note = fmt.Sprintf("As of %s: a progress report on an incomplete window. Nothing is decided before %s.", rep.AsOf, finalDate)
	}
	return rep, nil
}

func writeMarkdown(w io.Writer, rep Report) {
	title := map[string]string{"as-of": "progress report", "interim": "interim look (" + interimDate + ")", "final": "primary evaluation (" + finalDate + ")"}[rep.Phase]
	fmt.Fprintf(w, "# SMA50 forward tests: %s\n\nGenerated %s; data through %s. Schema `%s`.\n\n> %s\n\n", title, rep.GeneratedAt, rep.AsOf, rep.Schema, rep.Note)
	for _, b := range rep.Books {
		fmt.Fprintf(w, "## %s\n\nPre-registration: [%s](../%s).\n\n", b.Book, b.Prereg, b.Prereg)
		for _, s := range b.Scenarios {
			role := "decides"
			if s.Name != "primary" {
				role = "reported"
			}
			fmt.Fprintf(w, "### %s costs (%.0f bps per leg, %s)\n\nWindow %s → %s, %d bars so far, %d round trips (an open position counts as one).\n\n| | Criterion | Trend | Benchmark | Result |\n|---|---|---:|---:|---|\n",
				strings.ToUpper(s.Name[:1])+s.Name[1:], s.LegBps, role, s.From, s.To, s.Bars, s.RoundTrips)
			for _, h := range s.Hyp {
				res := "fail"
				if h.Pass {
					res = "**pass**"
				}
				if !h.Pass && h.ID != "H3" && math.Abs(h.Trend-h.Bench) < 1e-9 {
					res += " (tie: the criterion is strict)"
				}
				if h.Note != "" {
					res += " (" + h.Note + ")"
				}
				fmt.Fprintf(w, "| %s | %s | %.2f | %.2f | %s |\n", h.ID, h.Criteria, h.Trend, h.Bench, res)
			}
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "Reading of the primary outcome, as registered: %s", b.Reading)
		if !rep.Decides {
			fmt.Fprintf(w, " (not a decision: %s)", map[bool]string{true: "interim", false: "window incomplete"}[rep.Phase == "interim"])
		}
		fmt.Fprint(w, "\n\n")
	}
}
