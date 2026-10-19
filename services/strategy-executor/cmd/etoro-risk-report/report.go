package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/cfdrisk"
	"bitso-trading-platform/shared/pkg/cfdsim"
	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/etorodaily"
	"bitso-trading-platform/shared/pkg/etoroledger"
	"bitso-trading-platform/shared/pkg/mktcal"
	"bitso-trading-platform/shared/pkg/stops"
	"bitso-trading-platform/shared/pkg/varmodel"
	"bitso-trading-platform/shared/pkg/yahoodaily"
)

// Schema is Report.Schema.
const Schema = "cfdrisk-report/v1"

// smaDays is frozen by the pre-registration (§1).
const smaDays = 50

type options struct {
	ledger, candles, yahoo, reconcile, out, asOf, mcSince string
	size, capital                                         float64
	paths, horizon, block                                 int
	seed                                                  uint64
	nsdqSpread, spxSpread, overnight, measuredSpread      float64
	etfFee                                                float64
	costsStatus                                           string
}

// bookSpec is the frozen per-book configuration: the eToro instrument, its
// index proxy for long history and stress, and the ETF benchmark
// (pre-registration §3).
type bookSpec struct {
	Book, Symbol, Index, IndexFile, ETF string
	ETFSpreadRTBps                      float64
}

var specs = []bookSpec{
	{"nsdq100", "NSDQ100", "^NDX", "ndx_yahoo_daily.csv", "QQQ", 0.13},
	{"spx500", "SPX500", "^GSPC", "gspc_yahoo_daily.csv", "SPY", 0.26},
}

// Report is risk.json.
type Report struct {
	Schema      string              `json:"schema"`
	GeneratedAt string              `json:"generated_at"`
	AsOf        string              `json:"as_of"`
	Inputs      Inputs              `json:"inputs"`
	Params      Params              `json:"params"`
	Positions   []Position          `json:"positions"`
	VaR         VaRSection          `json:"var"`
	Stress      StressSection       `json:"stress"`
	MonteCarlo  []MCScenario        `json:"monte_carlo"`
	Attribution cfdrisk.Attribution `json:"attribution"`
	FillQuality cfdrisk.FillQuality `json:"fill_quality"`
	Protection  ProtectionSection   `json:"protection"`
	Notes       []string            `json:"notes"`
}

// ProtectionSection (M15, plan phase C6, 2026-10-19) is what the ledger
// says about stops: the held positions' stops against the mark and the
// ATR, every stop edit (console protect, executor ratchet), and every
// venue close with the realised loss next to the loss the stop modelled.
type ProtectionSection struct {
	Positions   []ProtectedPosition `json:"positions"`
	Edits       []ProtectionEdit    `json:"edits"`
	VenueCloses []VenueCloseRow     `json:"venue_closes"`
	Summary     ProtectionSummary   `json:"summary"`
}

// ProtectedPosition is a held position's stop as the ledger records it.
type ProtectedPosition struct {
	Book           string  `json:"book"`
	PositionID     string  `json:"position_id"`
	Source         string  `json:"source"` // rule | manual
	OpenRate       float64 `json:"open_rate"`
	Units          float64 `json:"units"`
	StopLossRate   float64 `json:"stop_loss_rate,omitempty"`
	TakeProfitRate float64 `json:"take_profit_rate,omitempty"`
	StopType       string  `json:"stop_type,omitempty"`
	Protected      bool    `json:"protected"`
	StopPctOfOpen  float64 `json:"stop_pct_of_open,omitempty"` // (open - stop) / open
	MaxLossUSD     float64 `json:"max_loss_usd,omitempty"`     // units x (open - stop)
	Mark           float64 `json:"mark"`                       // last close on or before as-of
	StopPctOfMark  float64 `json:"stop_pct_of_mark,omitempty"` // (mark - stop) / mark
	ATR            float64 `json:"atr,omitempty"`
	StopATRs       float64 `json:"stop_atrs,omitempty"` // (mark - stop) / ATR
}

// ProtectionEdit is one action-protect record.
type ProtectionEdit struct {
	FillDate    string  `json:"fill_date"`
	Book        string  `json:"book"`
	PositionID  string  `json:"position_id"`
	Source      string  `json:"source"` // rule | manual
	Profile     string  `json:"profile"`
	StopBefore  float64 `json:"stop_before,omitempty"`
	StopAfter   float64 `json:"stop_after,omitempty"`
	TPBefore    float64 `json:"tp_before,omitempty"`
	TPAfter     float64 `json:"tp_after,omitempty"`
	ATR         float64 `json:"atr,omitempty"`
	MaxLossUSD  float64 `json:"max_loss_usd,omitempty"`
	Status      string  `json:"status"`
	OperationID string  `json:"operation_id,omitempty"`
	Reason      string  `json:"reason,omitempty"`
}

// VenueCloseRow is one action-venue_close record.
type VenueCloseRow struct {
	FillDate       string  `json:"fill_date"`
	Book           string  `json:"book"`
	PositionID     string  `json:"position_id"`
	Kind           string  `json:"kind"`
	OpenRate       float64 `json:"open_rate"`
	Units          float64 `json:"units"`
	StopLossRate   float64 `json:"stop_loss_rate,omitempty"`
	TakeProfitRate float64 `json:"take_profit_rate,omitempty"`
	CloseRate      float64 `json:"close_rate"`
	GapBps         float64 `json:"gap_bps,omitempty"`
	ClosedAt       string  `json:"closed_at,omitempty"`
	NetProfit      float64 `json:"net_profit"`
	Fees           float64 `json:"fees"`
	Realized       float64 `json:"realized_usd"`              // net profit - fees
	ModelledAtStop float64 `json:"modelled_at_stop_usd"`      // -(units x (open - stop)) for a stop-out; 0 otherwise
	VsModelled     float64 `json:"vs_modelled_usd,omitempty"` // realized - modelled (negative: worse than the stop promised)
}

// ProtectionSummary counts the above.
type ProtectionSummary struct {
	Held          int     `json:"held"`
	HeldProtected int     `json:"held_protected"`
	Edits         int     `json:"edits"`
	Ratchets      int     `json:"ratchets"` // executor edits
	StopOuts      int     `json:"stop_outs"`
	TakeProfits   int     `json:"take_profits"`
	OtherCloses   int     `json:"other_closes"`
	RealizedUSD   float64 `json:"venue_close_realized_usd"`
	ModelledUSD   float64 `json:"stop_out_modelled_usd"`
	WorstGapBps   float64 `json:"worst_stop_gap_bps,omitempty"`
}

// Inputs fingerprints what was read.
type Inputs struct {
	Ledger    FileInfo            `json:"ledger"`
	Records   int                 `json:"ledger_records"`
	Candles   map[string]FileInfo `json:"candles"`
	Yahoo     map[string]FileInfo `json:"yahoo,omitempty"`
	Reconcile []string            `json:"reconcile,omitempty"`
}

// FileInfo is a file's path, SHA-256 prefix and, for bar files, its span.
type FileInfo struct {
	Path        string `json:"path"`
	SHA256Short string `json:"sha256_prefix"`
	First       string `json:"first,omitempty"`
	Last        string `json:"last,omitempty"`
	Bars        int    `json:"bars,omitempty"`
}

// Params are the settings the numbers depend on.
type Params struct {
	SMA              int                `json:"sma"`
	SizeUSD          float64            `json:"size_usd"`
	CapitalUSD       float64            `json:"capital_usd"`
	CostsStatus      string             `json:"costs_status"`
	SpreadRTBps      map[string]float64 `json:"spread_rt_bps"` // frozen, per book
	OvernightBps     float64            `json:"overnight_bps_per_night"`
	MeasuredSpreadRT float64            `json:"measured_spread_rt_bps"`
	ETFFeeUSD        float64            `json:"etf_fee_usd_per_order"`
	ETFSpreadRTBps   map[string]float64 `json:"etf_spread_rt_bps"`
	VaR              varmodel.Params    `json:"var"`
	MinScenarios     int                `json:"min_scenarios"`
	MC               cfdrisk.MCConfig   `json:"monte_carlo"`
	MCSince          string             `json:"mc_sample_since,omitempty"`
}

// Position is a book's demo position as of the report.
type Position struct {
	Book       string  `json:"book"`
	Symbol     string  `json:"symbol"`
	State      string  `json:"state"`
	PositionID string  `json:"position_id,omitempty"`
	Units      float64 `json:"units"`
	OpenRate   float64 `json:"open_rate,omitempty"`
	LastClose  float64 `json:"last_close"`
	LastDate   string  `json:"last_date"`
	Exposure   float64 `json:"exposure_usd"`
}

// BookVaR is the JSON view of cfdrisk.BookVaR (decimals and USD).
type BookVaR struct {
	Book          string  `json:"book"`
	Exposure      float64 `json:"exposure_usd"`
	Returns       int     `json:"returns"`
	LastDate      string  `json:"last_return_date,omitempty"`
	VolDaily      float64 `json:"vol_daily"`
	VolEWMA       float64 `json:"vol_ewma"`
	VolLong       float64 `json:"vol_long_252"`
	VaR           float64 `json:"var_99_usd"`
	VaRHistorical float64 `json:"var_99_historical_usd"`
	ESHistorical  float64 `json:"es_975_historical_usd"`
	ESNormal      float64 `json:"es_975_normal_usd"`
	HistOK        bool    `json:"historical_ok"`
	Scenarios     int     `json:"scenarios"`
	Backtest      struct {
		Observations    int     `json:"observations"`
		ExceptionsLong  int     `json:"exceptions_long"`
		ExceptionsShort int     `json:"exceptions_short"`
		KupiecLong      float64 `json:"kupiec_p_long"`
		KupiecShort     float64 `json:"kupiec_p_short"`
		Zone            string  `json:"zone"`
	} `json:"backtest"`
	Error string `json:"error,omitempty"`
}

// VaRSection is the per-book and portfolio VaR.
type VaRSection struct {
	Books            []BookVaR          `json:"books"`
	NetExposure      float64            `json:"net_exposure_usd"`
	GrossExposure    float64            `json:"gross_exposure_usd"`
	VaR              float64            `json:"var_99_usd"`
	VaRUndiversified float64            `json:"var_99_undiversified_usd"`
	ESNormal         float64            `json:"es_975_normal_usd"`
	Correlation      map[string]float64 `json:"correlation_ewma"` // "a|b" -> rho
	LongCorrelation  map[string]float64 `json:"correlation_252"`
	ImpliedCorr      float64            `json:"implied_correlation"` // the pair's EWMA correlation
	AlignedReturns   int                `json:"aligned_returns"`
	CorrelationOK    bool               `json:"correlation_ok"`
	VaRHistorical    float64            `json:"var_99_historical_usd"`
	ESHistorical     float64            `json:"es_975_historical_usd"`
	HistOK           bool               `json:"historical_ok"`
	Scenarios        int                `json:"scenarios"`
	WorstDate        string             `json:"worst_scenario_date,omitempty"`
	WorstLoss        float64            `json:"worst_scenario_loss_usd"`
	DivergenceRatio  float64            `json:"historical_over_parametric"`
	VaRPctOfCapital  float64            `json:"var_99_fraction_of_capital"`
}

// StressSection is the scenario set.
type StressSection struct {
	Source            string            `json:"episode_source"` // "yahoo csv" | "embedded"
	NetExposure       float64           `json:"net_exposure_usd"`
	Shocks            []StressShock     `json:"shocks"`
	Episodes          []StressEpisode   `json:"episodes"`
	Worst             string            `json:"worst_episode"`
	WorstLoss         float64           `json:"worst_episode_loss_usd"`
	WorstFracCapital  float64           `json:"worst_episode_fraction_of_capital"`
	ReverseStressMove float64           `json:"reverse_stress_move"`
	ReverseStressOK   bool              `json:"reverse_stress_ok"`
	CloseOutMove      float64           `json:"margin_close_out_move"`
	CloseOutLoss      float64           `json:"margin_close_out_loss_usd"`
	Proxies           map[string]string `json:"index_proxy"`
}

type StressShock struct {
	Move     float64 `json:"move"`
	Loss     float64 `json:"loss_usd"`
	CloseOut bool    `json:"margin_close_out"`
}

type StressEpisode struct {
	ID       string             `json:"id"`
	Name     string             `json:"name"`
	Base     string             `json:"base"`
	End      string             `json:"end"`
	Loss     float64            `json:"loss_usd"`
	OK       bool               `json:"complete"`
	Troughs  map[string]float64 `json:"troughs"`
	CloseOut bool               `json:"margin_close_out"`
}

// MCScenario is one Monte Carlo run: a book at a cost scenario.
type MCScenario struct {
	Book        string            `json:"book"`
	Scenario    string            `json:"scenario"` // "frozen" | "measured"
	SpreadRTBps float64           `json:"spread_rt_bps"`
	Overnight   float64           `json:"overnight_bps_per_night"`
	HistoryFrom string            `json:"history_from"`
	HistorySrc  string            `json:"history_source"`
	Summary     cfdrisk.MCSummary `json:"summary"`
}

// loaded is what run read from disk, per book.
type loaded struct {
	spec    bookSpec
	rows    []bitsodaily.Row
	bars    []dailyrule.Bar
	closes  []varmodel.Close
	pos     etoroledger.Position
	yahoo   []yahoodaily.Row // nil without -yahoo
	candles FileInfo
}

func run(o options, now time.Time) (*Report, error) {
	asOf, _ := time.Parse("2006-01-02", o.asOf)
	rep := &Report{Schema: Schema, GeneratedAt: now.UTC().Format(time.RFC3339), AsOf: o.asOf, Notes: []string{}}
	allRecs, err := etoroledger.ReadFile(o.ledger)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(o.ledger); err != nil {
		return nil, fmt.Errorf("ledger: %w", err)
	}
	// A demo record acts on its fill date: records filled after the as-of
	// date are not part of the picture yet (a re-run on an older date must
	// give the older answer).
	var recs []etoroledger.Record
	for _, r := range allRecs {
		if r.Decision.FillDate <= o.asOf {
			recs = append(recs, r)
		}
	}
	rep.Inputs.Ledger = FileInfo{Path: o.ledger, SHA256Short: sha(o.ledger)}
	rep.Inputs.Records = len(recs)
	rep.Inputs.Candles = map[string]FileInfo{}

	spreads := map[string]float64{"nsdq100": o.nsdqSpread, "spx500": o.spxSpread}
	etfSpreads := map[string]float64{}
	var books []loaded
	for _, s := range specs {
		etfSpreads[s.Book] = s.ETFSpreadRTBps
		l := loaded{spec: s, pos: etoroledger.LastDemoPosition(recs, s.Book)}
		path, err := newestCandles(o.candles, s.Book, o.asOf)
		if err != nil {
			return nil, err
		}
		rows, err := bitsodaily.ReadCSV(path)
		if err != nil {
			return nil, err
		}
		// Bars after the as-of date are not known yet from the report's point
		// of view (a re-run on an older date must give the older answer).
		var kept []bitsodaily.Row
		for _, r := range rows {
			if r.Date <= o.asOf {
				kept = append(kept, r)
			}
		}
		if len(kept) == 0 {
			return nil, fmt.Errorf("%s: no bars on or before %s", path, o.asOf)
		}
		l.rows = kept
		l.bars = etorodaily.Bars(kept)
		for _, b := range l.bars {
			l.closes = append(l.closes, varmodel.Close{Date: b.Date, Price: b.Close})
		}
		l.candles = FileInfo{Path: path, SHA256Short: sha(path), First: kept[0].Date, Last: kept[len(kept)-1].Date, Bars: len(kept)}
		rep.Inputs.Candles[s.Book] = l.candles
		if o.yahoo != "" {
			yp := filepath.Join(o.yahoo, s.IndexFile)
			yrows, err := yahoodaily.ReadCSV(yp)
			if err != nil {
				return nil, err
			}
			l.yahoo = yrows
			if rep.Inputs.Yahoo == nil {
				rep.Inputs.Yahoo = map[string]FileInfo{}
			}
			rep.Inputs.Yahoo[s.Index] = FileInfo{Path: yp, SHA256Short: sha(yp), First: yrows[0].Date, Last: yrows[len(yrows)-1].Date, Bars: len(yrows)}
		}
		books = append(books, l)
	}
	recon, reconFiles, err := readReconcile(o.reconcile)
	if err != nil {
		return nil, err
	}
	rep.Inputs.Reconcile = reconFiles

	mc := cfdrisk.MCConfig{Paths: o.paths, Horizon: o.horizon, MeanBlock: o.block, Seed: o.seed, SMA: smaDays}
	rep.Params = Params{SMA: smaDays, SizeUSD: o.size, CapitalUSD: o.capital, CostsStatus: o.costsStatus, SpreadRTBps: spreads,
		OvernightBps: o.overnight, MeasuredSpreadRT: o.measuredSpread, ETFFeeUSD: o.etfFee, ETFSpreadRTBps: etfSpreads,
		VaR: cfdrisk.DefaultParams(), MinScenarios: cfdrisk.MinScenarios, MC: mc, MCSince: o.mcSince}

	// Positions and VaR.
	var vbooks []cfdrisk.Book
	exposures := map[string]float64{}
	marks := map[string]cfdrisk.Mark{}
	for _, l := range books {
		units := 0.0
		if l.pos.State == "long" {
			units = l.pos.Units
		}
		last := l.bars[len(l.bars)-1]
		marks[l.spec.Book] = cfdrisk.Mark{Date: last.Date, Close: last.Close}
		rep.Positions = append(rep.Positions, Position{Book: l.spec.Book, Symbol: l.spec.Symbol, State: l.pos.State, PositionID: l.pos.PositionID,
			Units: units, OpenRate: l.pos.OpenRate, LastClose: last.Close, LastDate: last.Date.Format("2006-01-02"), Exposure: units * last.Close})
		exposures[l.spec.Book] = units * last.Close
		vbooks = append(vbooks, cfdrisk.Book{Name: l.spec.Book, Closes: l.closes, Units: units})
	}
	pf := cfdrisk.VaR(vbooks)
	rep.VaR = varSection(pf, o.capital)
	for _, b := range pf.Books {
		if b.Err != nil {
			rep.Notes = append(rep.Notes, fmt.Sprintf("%s: %v (%d trading-day returns)", b.Book, b.Err, b.Returns))
		}
	}

	// Stress.
	paths := map[string]map[string][]varmodel.PathPoint{}
	src := "embedded (cfdrisk.KnownIndexEpisodePaths, from the committed Yahoo CSVs)"
	for _, l := range books {
		if l.yahoo != nil {
			paths[l.spec.Book] = cfdrisk.IndexEpisodePaths(yahooCloses(l.yahoo))
			src = "yahoo csv (-yahoo)"
		} else {
			paths[l.spec.Book] = cfdrisk.KnownIndexEpisodePaths[l.spec.Index]
		}
	}
	rep.Stress = stressSection(cfdrisk.StressTest(exposures, paths, o.capital), src, o.capital)

	// Monte Carlo: two cost scenarios per book, same seed so the paths are
	// the same and only the costs differ.
	for _, l := range books {
		hist, from, hsrc := l.bars, 1, "etoro bars"
		if l.yahoo != nil {
			hist = splice(yahoodaily.Bars(l.yahoo), l.bars)
			hsrc = l.spec.Index + " to " + l.bars[0].Date.Format("2006-01-02") + ", then etoro bars"
		}
		if o.mcSince != "" {
			since, err := time.Parse("2006-01-02", o.mcSince)
			if err != nil {
				return nil, fmt.Errorf("-mc-since: %w", err)
			}
			from = sort.Search(len(hist), func(i int) bool { return !hist[i].Date.Before(since) })
			if from < 1 {
				from = 1
			}
		}
		etf := cfdsim.Costs{SpreadPerLeg: l.spec.ETFSpreadRTBps / 2 / 1e4, FeePerLeg: o.etfFee / o.size}
		night := cfdsim.FixedNightly(o.overnight / 1e4)
		for _, sc := range []struct {
			name   string
			spread float64
		}{{"frozen", spreads[l.spec.Book]}, {"measured", o.measuredSpread}} {
			cfg := mc
			cfg.CFD = cfdsim.Costs{SpreadPerLeg: sc.spread / 2 / 1e4, NightlyRate: night}
			cfg.ETF = etf
			sum, err := cfdrisk.MonteCarlo(hist, from, cfg)
			if err != nil {
				rep.Notes = append(rep.Notes, fmt.Sprintf("%s monte carlo (%s): %v", l.spec.Book, sc.name, err))
				continue
			}
			rep.MonteCarlo = append(rep.MonteCarlo, MCScenario{Book: l.spec.Book, Scenario: sc.name, SpreadRTBps: sc.spread, Overnight: o.overnight,
				HistoryFrom: hist[from].Date.Format("2006-01-02"), HistorySrc: hsrc, Summary: sum})
		}
	}

	// Attribution and fill quality.
	overnight := map[string]float64{}
	for _, s := range specs {
		overnight[s.Book] = o.overnight
	}
	charged, chargedSrc := financingCharged(recon, asOf)
	rep.Attribution = cfdrisk.Attribute(cfdrisk.AttributionInput{Records: recs, Marks: marks, AsOf: asOf, OvernightBps: overnight, FinancingCharged: charged})
	if chargedSrc != "" {
		rep.Notes = append(rep.Notes, "financing_charged from "+chargedSrc)
	}
	opens := map[string]map[string]float64{}
	for _, l := range books {
		opens[l.spec.Book] = map[string]float64{}
		for _, r := range l.rows {
			opens[l.spec.Book][r.Date] = r.Open
		}
	}
	days := map[string]cfdrisk.ReconcileDay{}
	for date, r := range recon {
		days[date] = r.day
	}
	rep.FillQuality = cfdrisk.MeasureFills(cfdrisk.FillInput{Records: recs, Opens: opens, Reconcile: days, FrozenSpreadRTBps: spreads})
	if len(rep.FillQuality.Fills) == 0 {
		rep.Notes = append(rep.Notes, "no executed demo orders in the ledger: attribution and fill quality are empty")
	}
	if o.yahoo == "" {
		rep.Notes = append(rep.Notes, "no -yahoo: stress episodes from the embedded paths; Monte Carlo resamples eToro's own bars only")
	}
	atrs := map[string]float64{}
	for _, l := range books {
		sb := make([]stops.Bar, 0, len(l.rows))
		for _, r := range l.rows {
			sb = append(sb, stops.Bar{High: r.High, Low: r.Low, Close: r.Close})
		}
		if a, ok := stops.ATR(sb, stops.DefaultATRPeriod); ok {
			atrs[l.spec.Book] = a
		}
	}
	rep.Protection = protectionSection(recs, marks, atrs)
	if rep.Protection.Summary.Held > rep.Protection.Summary.HeldProtected {
		rep.Notes = append(rep.Notes, fmt.Sprintf("%d of %d held positions carry no stop on the ledger (opened before protection phase C, or the venue dropped it)", rep.Protection.Summary.Held-rep.Protection.Summary.HeldProtected, rep.Protection.Summary.Held))
	}
	for _, p := range rep.Positions {
		if p.State == "long" && p.LastDate != "" {
			rep.Notes = append(rep.Notes, fmt.Sprintf("%s marked at the %s close %.2f (the last closed bar on or before %s)", p.Book, p.LastDate, p.LastClose, o.asOf))
		}
	}
	return rep, nil
}

func varSection(pf cfdrisk.Portfolio, capital float64) VaRSection {
	v := VaRSection{NetExposure: pf.Net, GrossExposure: pf.Gross, VaR: pf.VaR, VaRUndiversified: pf.VaRUndiversified, ESNormal: pf.ESNormal,
		Correlation: map[string]float64{}, LongCorrelation: map[string]float64{}, ImpliedCorr: 1, AlignedReturns: pf.AlignedReturns, CorrelationOK: pf.CorrelationOK,
		VaRHistorical: pf.VaRHistorical, ESHistorical: pf.ESHistorical, HistOK: pf.HistOK, Scenarios: pf.Scenarios, WorstLoss: pf.WorstLoss}
	if !pf.WorstDate.IsZero() {
		v.WorstDate = pf.WorstDate.Format("2006-01-02")
	}
	for _, b := range pf.Books {
		bv := BookVaR{Book: b.Book, Exposure: b.Exposure, Returns: b.Returns, VolDaily: b.Vol, VolEWMA: b.EWMA, VolLong: b.Long, VaR: b.VaR,
			VaRHistorical: b.VaRHistorical, ESHistorical: b.ESHistorical, ESNormal: b.ESNormal, HistOK: b.HistOK, Scenarios: b.Scenarios}
		if !b.LastDate.IsZero() {
			bv.LastDate = b.LastDate.Format("2006-01-02")
		}
		if b.Err != nil {
			bv.Error = b.Err.Error()
		}
		bt := b.Backtest
		bv.Backtest.Observations, bv.Backtest.ExceptionsLong, bv.Backtest.ExceptionsShort = bt.Observations, bt.ExceptionsLong, bt.ExceptionsShort
		bv.Backtest.KupiecLong, bv.Backtest.KupiecShort, bv.Backtest.Zone = bt.KupiecLong, bt.KupiecShort, bt.Zone.String()
		v.Books = append(v.Books, bv)
	}
	for i := range pf.Books {
		for j := i + 1; j < len(pf.Books); j++ {
			k := pf.Books[i].Book + "|" + pf.Books[j].Book
			v.Correlation[k] = pf.Correlation[i][j]
			v.LongCorrelation[k] = pf.LongCorrelation[i][j]
			if len(pf.Books) == 2 {
				v.ImpliedCorr = pf.Correlation[i][j]
			}
		}
	}
	if pf.VaR > 0 {
		v.DivergenceRatio = pf.VaRHistorical / pf.VaR
	}
	if capital > 0 {
		v.VaRPctOfCapital = pf.VaR / capital
	}
	return v
}

func stressSection(s cfdrisk.Stress, src string, capital float64) StressSection {
	out := StressSection{Source: src, NetExposure: s.Net, Worst: s.Worst, WorstLoss: s.WorstLoss, ReverseStressMove: s.ReverseStressMove,
		ReverseStressOK: s.ReverseStressOK, CloseOutMove: cfdrisk.CloseOutMove, CloseOutLoss: s.CloseOutLoss, Proxies: cfdrisk.IndexProxy}
	if capital > 0 {
		out.WorstFracCapital = s.WorstLoss / capital
	}
	for _, sh := range s.Shocks {
		out.Shocks = append(out.Shocks, StressShock{Move: sh.Move, Loss: sh.Loss, CloseOut: sh.CloseOut})
	}
	for _, e := range s.Episodes {
		out.Episodes = append(out.Episodes, StressEpisode{ID: e.ID, Name: e.Name, Base: e.Base.Format("2006-01-02"), End: e.End.Format("2006-01-02"),
			Loss: e.Loss, OK: e.OK, Troughs: e.Troughs, CloseOut: e.CloseOut})
	}
	return out
}

// newestCandles finds the newest <book>_daily_<date>.csv dated on or
// before asOf, or, when every file is newer, the oldest one (its rows are
// cut at asOf by the caller; eToro's later versions of a close differ by
// at most ~1.3 bps, see the executor's revision check).
func newestCandles(dir, book, asOf string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, book+"_daily_*.csv"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no %s_daily_<date>.csv in %s", book, dir)
	}
	sort.Strings(matches)
	for i := len(matches) - 1; i >= 0; i-- {
		d := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(matches[i]), book+"_daily_"), ".csv")
		if d <= asOf {
			return matches[i], nil
		}
	}
	return matches[0], nil
}

func yahooCloses(rows []yahoodaily.Row) []varmodel.Close {
	out := make([]varmodel.Close, 0, len(rows))
	for _, r := range rows {
		d, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			continue
		}
		out = append(out, varmodel.Close{Date: d, Price: r.Close})
	}
	return out
}

// splice is the long index history up to the day before eToro's first bar,
// then eToro's own bars: one series whose last close is the traded
// instrument's. The two are at the same level (P2: close ratio ~1.0000).
func splice(index, etoro []dailyrule.Bar) []dailyrule.Bar {
	if len(etoro) == 0 {
		return index
	}
	first := etoro[0].Date
	var out []dailyrule.Bar
	for _, b := range index {
		if !b.Date.Before(first) {
			break
		}
		if mktcal.IsTradingDay(b.Date) || b.Date.Year() < 1990 {
			out = append(out, b)
		}
	}
	return append(out, etoro...)
}

// reconcileReport is the subset of cmd/etoro-reconcile's report read here.
type reconcileReport struct {
	Schema        string  `json:"schema"`
	GeneratedAt   string  `json:"generated_at"`
	Clean         bool    `json:"clean"`
	PositionReads int     `json:"position_reads"`
	SettleWaitedS float64 `json:"settle_waited_s"`
	Books         []struct {
		Book             string   `json:"book"`
		LedgerState      string   `json:"ledger_state"`
		AccountPositions []string `json:"account_positions"`
		FinancingToDate  float64  `json:"financing_to_date"`
	} `json:"books"`
	Findings []struct {
		Severity string `json:"severity"`
		Book     string `json:"book"`
		Code     string `json:"code"`
	} `json:"findings"`
}

type reconcileFile struct {
	path string
	rep  reconcileReport
	at   time.Time
	day  cfdrisk.ReconcileDay
}

// readReconcile reads <date>.json (the run's report; a .recheck.json is
// listed but keyed under its own name) from dir. Keys are the New York
// dates of the file names.
func readReconcile(dir string) (map[string]reconcileFile, []string, error) {
	out := map[string]reconcileFile{}
	if dir == "" {
		return out, nil, nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(matches)
	var files []string
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			return nil, nil, err
		}
		var r reconcileReport
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", m, err)
		}
		if !strings.HasPrefix(r.Schema, "etoro-reconcile/") {
			continue
		}
		at, _ := time.Parse(time.RFC3339, r.GeneratedAt)
		day := cfdrisk.ReconcileDay{GeneratedAt: at, Clean: r.Clean, PositionReads: r.PositionReads, SettleWaitedS: r.SettleWaitedS}
		for _, f := range r.Findings {
			day.Findings = append(day.Findings, cfdrisk.ReconcileFinding{Severity: f.Severity, Book: f.Book, Code: f.Code})
		}
		key := strings.TrimSuffix(filepath.Base(m), ".json")
		out[key] = reconcileFile{path: m, rep: r, at: at, day: day}
		files = append(files, m)
	}
	return out, files, nil
}

// financingCharged takes, per book, financing_to_date from the newest
// report (by generated_at, on or before the end of asOf) in which the
// book's position was read in the account. The source file is returned.
func financingCharged(recon map[string]reconcileFile, asOf time.Time) (map[string]float64, string) {
	out := map[string]float64{}
	var newest reconcileFile
	limit := asOf.AddDate(0, 0, 1)
	for _, r := range recon {
		if !r.at.IsZero() && r.at.Before(limit) && r.at.After(newest.at) {
			newest = r
		}
	}
	if newest.path == "" {
		return nil, ""
	}
	for _, b := range newest.rep.Books {
		if b.LedgerState == "long" && len(b.AccountPositions) > 0 {
			out[b.Book] = b.FinancingToDate
		}
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out, newest.path
}

func sha(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])[:16]
}

var errNoOut = errors.New("no output directory")

func writeOutputs(dir string, rep *Report, md string) error {
	if dir == "" {
		return errNoOut
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "risk.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "risk.md"), []byte(md), 0o644)
}
