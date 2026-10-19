package main

import (
	"fmt"
	"sort"
	"strings"

	"bitso-trading-platform/shared/pkg/cfdrisk"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/montecarlo"
)

// renderMarkdown is risk.md: the same numbers as risk.json, in percent and
// basis points, as tables.
func renderMarkdown(r *Report) string {
	var b strings.Builder
	p := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	pct := func(x float64) string { return fmt.Sprintf("%.2f %%", x*100) }
	usd := func(x float64) string { return fmt.Sprintf("%.2f", x) }
	bps := func(x float64) string { return fmt.Sprintf("%.2f", x) }

	p("# eToro index-CFD risk report, as of %s (New York)\n\n", r.AsOf)
	p("Schema `%s`, generated %s. Reporting only: nothing here blocks an order.\n\n", r.Schema, r.GeneratedAt)
	p("- Ledger `%s` (%d records, sha %s).\n", r.Inputs.Ledger.Path, r.Inputs.Records, r.Inputs.Ledger.SHA256Short)
	for _, book := range sortedKeys(r.Inputs.Candles) {
		c := r.Inputs.Candles[book]
		p("- Candles %s: `%s`, %d bars %s..%s (sha %s).\n", book, c.Path, c.Bars, c.First, c.Last, c.SHA256Short)
	}
	for _, sym := range sortedKeys(r.Inputs.Yahoo) {
		y := r.Inputs.Yahoo[sym]
		p("- Index %s: `%s`, %d bars %s..%s (sha %s).\n", sym, y.Path, y.Bars, y.First, y.Last, y.SHA256Short)
	}
	if len(r.Inputs.Reconcile) > 0 {
		p("- Reconcile reports: %d (%s).\n", len(r.Inputs.Reconcile), strings.Join(basenames(r.Inputs.Reconcile), ", "))
	}
	p("- Costs [%s]: spread round trip NSDQ100 %.1f / SPX500 %.1f bps, overnight %.1f bps per calendar night; measured in-session round trip %.1f bps (second Monte Carlo scenario); ETF %.2f USD per order on %.0f USD.\n",
		r.Params.CostsStatus, r.Params.SpreadRTBps["nsdq100"], r.Params.SpreadRTBps["spx500"], r.Params.OvernightBps, r.Params.MeasuredSpreadRT, r.Params.ETFFeeUSD, r.Params.SizeUSD)
	p("- Capital for the reverse stress: %.0f USD. VaR model: EWMA λ %.2f, long window %d trading days, 99 %%, backtest %d trading days.\n\n",
		r.Params.CapitalUSD, r.Params.VaR.Lambda, r.Params.VaR.LongWindow, r.Params.VaR.BacktestWindow)

	p("## 1. Positions and VaR (M10)\n\n")
	p("| Book | State | Units | Last close (%s) | Exposure USD | Vol/day | EWMA | 252-day | VaR 99 %% | Hist VaR | ES 97.5 %% hist | ES normal | Backtest |\n", lastDate(r))
	p("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for i, pos := range r.Positions {
		v := r.VaR.Books[i]
		bt := fmt.Sprintf("%s (%d/%d exc. in %d, Kupiec p %.2f)", v.Backtest.Zone, v.Backtest.ExceptionsLong, v.Backtest.ExceptionsShort, v.Backtest.Observations, v.Backtest.KupiecLong)
		if v.Error != "" {
			bt = v.Error
		}
		p("| %s | %s | %.6f | %.2f | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", pos.Book, pos.State, pos.Units, pos.LastClose, usd(pos.Exposure),
			pct(v.VolDaily), pct(v.VolEWMA), pct(v.VolLong), usd(v.VaR), usd(v.VaRHistorical), usd(v.ESHistorical), usd(v.ESNormal), bt)
	}
	p("\n| Portfolio | Value |\n|---|---:|\n")
	p("| Net / gross exposure USD | %s / %s |\n", usd(r.VaR.NetExposure), usd(r.VaR.GrossExposure))
	p("| Parametric VaR 99 %% 1-day USD (EWMA covariance) | %s |\n", usd(r.VaR.VaR))
	p("| Undiversified (correlation 1) | %s |\n", usd(r.VaR.VaRUndiversified))
	p("| Implied NSDQ100/SPX500 correlation (EWMA / 252-day) | %.3f / %.3f (%d aligned returns) |\n", r.VaR.ImpliedCorr, r.VaR.LongCorrelation["nsdq100|spx500"], r.VaR.AlignedReturns)
	p("| Historical VaR 99 %% (aligned days) | %s (%d scenarios, ok %v) |\n", usd(r.VaR.VaRHistorical), r.VaR.Scenarios, r.VaR.HistOK)
	p("| ES 97.5 %% historical / normal | %s / %s |\n", usd(r.VaR.ESHistorical), usd(r.VaR.ESNormal))
	p("| Historical / parametric | %.2f |\n", r.VaR.DivergenceRatio)
	p("| Worst aligned day | %s, %s USD |\n", r.VaR.WorstDate, usd(r.VaR.WorstLoss))
	p("| VaR as a fraction of capital | %s |\n\n", pct(r.VaRPctOfCapital()))

	p("## 2. Stress (M11)\n\n")
	p("Episodes from %s; index proxies NSDQ100 → ^NDX, SPX500 → ^GSPC. Net exposure %s USD.\n\n", r.Stress.Source, usd(r.Stress.NetExposure))
	p("| Shock | Loss USD | Close-out |\n|---|---:|---|\n")
	for _, s := range r.Stress.Shocks {
		p("| %s | %s | %v |\n", pct(s.Move), usd(s.Loss), s.CloseOut)
	}
	p("\n| Episode | Window (base → trough) | NSDQ100 | SPX500 | Loss USD | Complete |\n|---|---|---:|---:|---:|---|\n")
	for _, e := range r.Stress.Episodes {
		p("| %s | %s → %s | %s | %s | %s | %v |\n", e.Name, e.Base, e.End, pct(e.Troughs["nsdq100"]), pct(e.Troughs["spx500"]), usd(e.Loss), e.OK)
	}
	reverse := fmt.Sprintf("a uniform move of %s exhausts the capital", pct(r.Stress.ReverseStressMove))
	if !r.Stress.ReverseStressOK {
		reverse = fmt.Sprintf("the capital (%.0f USD) exceeds the net exposure, so no uniform fall can exhaust it (it would take %s)", r.Params.CapitalUSD, pct(r.Stress.ReverseStressMove))
	}
	p("\nWorst episode: **%s**, %s USD (%s of capital). Reverse stress: %s. Margin close-out at %s: %s USD on the gross exposure.\n\n",
		r.Stress.Worst, usd(r.Stress.WorstLoss), pct(r.Stress.WorstFracCapital), reverse, pct(r.Stress.CloseOutMove), usd(r.Stress.CloseOutLoss))

	p("## 3. Monte Carlo (M12)\n\n")
	p("%d paths, %d trading days, mean block %d, seed %d, SMA %d; paths priced with cfdsim (spread per leg, financing per calendar night, close-out flag). Returns are fractions of the starting equity; the ETF hold pays %.2f USD per order and no financing (no dividends in the paths).\n\n",
		r.Params.MC.Paths, r.Params.MC.Horizon, r.Params.MC.MeanBlock, r.Params.MC.Seed, r.Params.MC.SMA, r.Params.ETFFeeUSD)
	p("| Book | Scenario | Spread RT | History | Trend p5/p50/p95 | CFD hold p50 | ETF hold p50 | Trend DD p50 | Hold DD p50 | Trips p50 | Financing trend/hold p50 | P(beats CFD hold) | P(beats ETF hold) | P(shallower DD) | P(loss) | P(close-out) trend/hold |\n")
	p("|---|---|---:|---|---|---:|---:|---:|---:|---:|---|---|---|---|---|---|\n")
	for _, m := range r.MonteCarlo {
		s := m.Summary
		p("| %s | %s | %.1f bps | %s.. (%d days) | %s / %s / %s | %s | %s | %s | %s | %.0f | %s / %s | %s | %s | %s | %s | %s / %s |\n",
			m.Book, m.Scenario, m.SpreadRTBps, s.SampleFrom, s.SampleDays,
			pct(s.TrendReturn.P5), pct(s.TrendReturn.P50), pct(s.TrendReturn.P95), pct(s.CFDHoldReturn.P50), pct(s.ETFHoldReturn.P50),
			pct(s.TrendMaxDD.P50), pct(s.CFDHoldMaxDD.P50), s.TrendTrips.P50, pct(s.TrendFinance.P50), pct(s.HoldFinance.P50),
			prob(s.PBeatsCFDHold), prob(s.PBeatsETFHold), prob(s.PShallowerDD), prob(s.PTrendLoss), pct(s.PTrendCloseOut.P), pct(s.PHoldCloseOut.P))
	}
	p("\n")

	p("## 4. Attribution (M13)\n\n")
	a := r.Attribution
	p("USD, gains positive; financing modelled at %.1f bps per calendar night on the open notional (weekend = 3 nights) to %s; open lots marked at the last close.\n\n", r.Params.OvernightBps, a.AsOf)
	p("| Book | Lots (open) | Price P&L | Spread | Markup | Fees | Financing modelled | Financing charged | Net | Realized / unrealized | Paper equity | Demo equity | Tracking bps |\n")
	p("|---|---|---:|---:|---:|---:|---:|---:|---:|---|---:|---:|---:|\n")
	for _, bk := range a.Books {
		open := 0
		for _, l := range bk.Lots {
			if l.Open {
				open++
			}
		}
		charged := "n/a"
		if bk.FinancingCharged != nil {
			charged = usd(*bk.FinancingCharged)
		}
		paper := "not started"
		if bk.PaperStarted {
			paper = fmt.Sprintf("%.4f", bk.PaperEquity)
		}
		p("| %s | %d (%d) | %s | %s | %s | %s | %s | %s | **%s** | %s / %s | %s | %.4f | %+.1f |\n", bk.Book, len(bk.Lots), open,
			usd(bk.Total.PricePnL), usd(bk.Total.SpreadPaid), usd(bk.Total.Markup), usd(bk.Total.Fees), usd(bk.Total.FinancingModelled), charged,
			usd(bk.Total.Net), usd(bk.Realized.Net), usd(bk.Unrealized.Net), paper, bk.DemoEquity, bk.TrackingBps)
	}
	p("| **total** | | %s | %s | %s | %s | %s | | **%s** | %s / %s | | | |\n\n", usd(a.Total.PricePnL), usd(a.Total.SpreadPaid), usd(a.Total.Markup), usd(a.Total.Fees),
		usd(a.Total.FinancingModelled), usd(a.Total.Net), usd(a.Realized.Net), usd(a.Unrealized.Net))
	if len(a.Books) > 0 {
		p("| Lot | Open (date, price) | Close / mark | Units | Nights | Price P&L | Costs | Financing | Net |\n|---|---|---|---:|---:|---:|---:|---:|---:|\n")
		for _, bk := range a.Books {
			for _, l := range bk.Lots {
				cl := fmt.Sprintf("mark %.2f", l.ClosePrice)
				if !l.Open {
					cl = fmt.Sprintf("%s %.2f", l.CloseDate, l.ClosePrice)
				}
				p("| %s %s | %s %.2f | %s | %.6f | %d | %s | %s | %s | %s |\n", l.Book, l.PositionID, l.OpenDate, l.OpenPrice, cl, l.Units, l.Nights,
					usd(l.PricePnL), usd(l.SpreadPaid+l.Markup+l.Fees), usd(l.FinancingModelled), usd(l.Net))
			}
		}
		p("\n")
	}

	p("## 5. Fill quality (M14)\n\n")
	q := r.FillQuality
	p("| Fill | Kind | Price | Quoted spread bps | Fill vs mid bps | Fill vs decision close bps | Fill vs day open bps | Spread cost USD (bps) | Latency s | Portfolio lag | Reconcile |\n")
	p("|---|---|---:|---:|---:|---:|---:|---:|---:|---|---|\n")
	for _, f := range q.Fills {
		vsOpen := "n/a"
		if f.FillVsOpenBps != nil {
			vsOpen = bps(*f.FillVsOpenBps)
		}
		recon := "n/a"
		if f.ReconcileClean != nil {
			recon = map[bool]string{true: "clean", false: "drift"}[*f.ReconcileClean]
			if len(f.ReconcileCodes) > 0 {
				recon += " (" + strings.Join(f.ReconcileCodes, ", ") + ")"
			}
		}
		p("| %s %s | %s | %.2f | %s | %s | %s | %s | %s (%s) | %.2f | %v | %s |\n", f.FillDate, f.Book, f.Kind, f.AvgPrice, bps(f.QuotedSpreadBps), bps(f.FillVsMidBps),
			bps(f.FillVsCloseBps), vsOpen, usd(f.SpreadCostUSD), bps(f.SpreadCostBps), f.LatencyS, f.PortfolioLag, recon)
	}
	p("\n| Aggregate | n | Quoted spread mean/median/worst | Fill vs mid | Fill vs close | Fill vs open | Spread cost bps | Latency s | Lags | Measured spread per leg vs frozen |\n")
	p("|---|---:|---|---|---|---|---|---|---:|---|\n")
	for _, bq := range append(append([]cfdrisk.BookFillQuality{}, q.Books...), q.All) {
		if bq.N == 0 && bq.Book != "all" {
			continue
		}
		p("| %s | %d | %s | %s | %s | %s | %s | %s | %d | %.2f vs %.2f bps (×%.3f) |\n", bq.Book, bq.N, statS(bq.QuotedSpread), statS(bq.FillVsMid), statS(bq.FillVsClose), statS(bq.FillVsOpen),
			statS(bq.SpreadCost), statS(bq.Latency), bq.PortfolioLags, bq.MeasuredSpreadPerLegBps, bq.FrozenSpreadPerLegBps, bq.MeasuredVsFrozen)
	}
	p("\nThe cost-model check uses eToro's own spread cost per leg; half the quoted spread at check time was %.2f bps (all fills). A negative fill-vs-mid is a fill better than the quote's mid, i.e. the market moved between the quote and the fill.\n\n", q.All.HalfQuotedSpreadBps)

	p("## 6. Protection (M15)\n\n")
	pr := r.Protection
	p("From the ledger: the stop each held position carries, every stop edit (a console protect or the executor's daily ratchet), and every close eToro made (stop, target, other) with the realised result next to the loss the stop modelled. Reporting only.\n\n")
	if len(pr.Positions) == 0 {
		p("No position held.\n\n")
	} else {
		p("| Book | Position | Source | Open | Units | Stop | Type | %% below open | Max loss USD | Mark | %% below mark | ATRs below mark | Target |\n")
		p("|---|---|---|---:|---:|---:|---|---:|---:|---:|---:|---:|---:|\n")
		for _, x := range pr.Positions {
			stop, typ, pctOpen, maxLoss, pctMark, atrs := "**none**", "", "", "", "", ""
			if x.Protected {
				stop, typ = fmt.Sprintf("%.2f", x.StopLossRate), stopTypeLabel(x.StopType)
				pctOpen, maxLoss, pctMark = fmt.Sprintf("%.2f %%", x.StopPctOfOpen*100), fmt.Sprintf("%.2f", x.MaxLossUSD), fmt.Sprintf("%.2f %%", x.StopPctOfMark*100)
				if x.StopATRs > 0 {
					atrs = fmt.Sprintf("%.1f", x.StopATRs)
				}
			}
			tp := ""
			if x.TakeProfitRate > 0 {
				tp = fmt.Sprintf("%.2f", x.TakeProfitRate)
			}
			p("| %s | %s | %s | %.2f | %.6f | %s | %s | %s | %s | %.2f | %s | %s | %s |\n", x.Book, x.PositionID, x.Source, x.OpenRate, x.Units, stop, typ, pctOpen, maxLoss, x.Mark, pctMark, atrs, tp)
		}
		p("\n")
	}
	if len(pr.Edits) > 0 {
		p("| Edit (fill date) | Book | Position | Source | Profile | Stop before → after | Target before → after | ATR | Max loss USD | Status | Reason |\n")
		p("|---|---|---|---|---|---|---|---:|---:|---|---|\n")
		for _, e := range pr.Edits {
			p("| %s | %s | %s | %s | %s | %s → %s | %s → %s | %s | %s | %s | %s |\n", e.FillDate, e.Book, e.PositionID, e.Source, e.Profile,
				rateOrNone(e.StopBefore), rateOrNone(e.StopAfter), rateOrNone(e.TPBefore), rateOrNone(e.TPAfter), optF(e.ATR), optF(e.MaxLossUSD), e.Status, e.Reason)
		}
		p("\n")
	}
	if len(pr.VenueCloses) > 0 {
		p("| Venue close (fill date) | Book | Position | Kind | Open | Stop / target | Close rate | Gap bps | Closed at | Net profit | Fees | Realised | Modelled at the stop | Realised − modelled |\n")
		p("|---|---|---|---|---:|---|---:|---:|---|---:|---:|---:|---:|---:|\n")
		for _, v := range pr.VenueCloses {
			modelled, diff := "", ""
			if v.Kind == etoroledger.VenueCloseStoppedOut && v.StopLossRate > 0 {
				modelled, diff = fmt.Sprintf("%.2f", v.ModelledAtStop), fmt.Sprintf("%+.2f", v.VsModelled)
			}
			p("| %s | %s | %s | **%s** | %.2f | %s / %s | %.2f | %s | %s | %.2f | %.2f | **%.2f** | %s | %s |\n", v.FillDate, v.Book, v.PositionID, v.Kind, v.OpenRate,
				rateOrNone(v.StopLossRate), rateOrNone(v.TakeProfitRate), v.CloseRate, bps(v.GapBps), v.ClosedAt, v.NetProfit, v.Fees, v.Realized, modelled, diff)
		}
		p("\n")
	}
	s := pr.Summary
	p("Held %d (protected %d) · stop edits %d (executor ratchets %d) · venue closes: stop-outs %d, take-profits %d, other %d · realised at venue closes %.2f USD, modelled at the stops %.2f USD, worst stop gap %.1f bps.\n\n",
		s.Held, s.HeldProtected, s.Edits, s.Ratchets, s.StopOuts, s.TakeProfits, s.OtherCloses, s.RealizedUSD, s.ModelledUSD, s.WorstGapBps)

	if len(r.Notes) > 0 {
		p("## Notes\n\n")
		for _, n := range r.Notes {
			p("- %s\n", n)
		}
	}
	return b.String()
}

// VaRPctOfCapital is the parametric VaR over the capital figure.
func (r *Report) VaRPctOfCapital() float64 { return r.VaR.VaRPctOfCapital }

func prob(p montecarlo.Prob) string {
	return fmt.Sprintf("%.1f %% [%.1f, %.1f]", p.P*100, p.Lo*100, p.Hi*100)
}

func statS(s cfdrisk.Stat) string {
	if s.N == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.2f / %.2f / %.2f", s.Mean, s.Median, s.Worst)
}

func lastDate(r *Report) string {
	if len(r.Positions) == 0 {
		return ""
	}
	return r.Positions[0].LastDate
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func basenames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = p[strings.LastIndex(p, "/")+1:]
	}
	return out
}
