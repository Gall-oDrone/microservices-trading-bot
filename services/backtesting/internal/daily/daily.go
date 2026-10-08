// Package daily runs the frozen SMA50 daily rule inside services/backtesting,
// the canonical backtest engine (plan §7 item 5).
//
// The rule (dailyrule.Trend) and the simulator (dailyrule.SimulateTrace) are
// the shared ones cmd/daily-research produced the registered evidence with,
// so for the same bars, window and per-leg costs this package reports the
// same return, round trips, exposure, max drawdown and cost bit for bit; the
// parity test pins that against the committed evidence JSON. Everything this
// package adds (trades, equity curve in money, Sharpe and friends) is derived
// from the simulator's trace and never feeds back into it.
package daily

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"bitso-trading-platform/backtesting/internal/models"
	"bitso-trading-platform/shared/pkg/dailyrule"
)

// Strategy is the strategy name that selects this engine path.
const Strategy = "sma50_daily"

// SMA is the frozen lookback. Other values are rejected: a different
// lookback is a different rule and needs its own pre-registration.
const SMA = 50

// AnnualizationDays annualizes daily statistics. Crypto trades every
// calendar day, so 365 (not the equity-market 252).
const AnnualizationDays = 365

// ErrNoBarsDir means the service was started without BACKTEST_DAILY_BARS_DIR.
var ErrNoBarsDir = errors.New("daily bars directory not configured (set BACKTEST_DAILY_BARS_DIR)")

// IsDaily reports whether cfg selects this engine path.
func IsDaily(cfg *models.BacktestConfig) bool {
	return cfg != nil && cfg.Strategy == Strategy
}

// Params are the resolved run parameters.
type Params struct {
	BarsFile       string  // path under the bars directory
	CommissionRate float64 // per leg, decimal
	SlippageRate   float64 // per leg, decimal
	Costs          dailyrule.Costs
}

// ResolveParams validates cfg for the daily path. barsDir is the directory
// bars files are read from; bars_file must be a local path inside it.
func ResolveParams(cfg *models.BacktestConfig, barsDir string) (Params, error) {
	var p Params
	if cfg.DataSource != "file" {
		return p, fmt.Errorf("%s reads daily bars from a file: data_source must be \"file\", got %q", Strategy, cfg.DataSource)
	}
	if v, ok := cfg.StrategyParams["sma"]; ok {
		n, ok := v.(float64)
		if iv, isInt := v.(int); isInt {
			n, ok = float64(iv), true
		}
		if !ok || n != SMA {
			return p, fmt.Errorf("%s is frozen at sma=%d, got %v", Strategy, SMA, v)
		}
	}
	if barsDir == "" {
		return p, ErrNoBarsDir
	}
	name := cfg.Book + "_daily_bitso.csv"
	if v, ok := cfg.StrategyParams["bars_file"]; ok {
		s, isStr := v.(string)
		if !isStr || s == "" {
			return p, fmt.Errorf("bars_file must be a non-empty string")
		}
		name = s
	}
	if !filepath.IsLocal(name) {
		return p, fmt.Errorf("bars_file %q must be a relative path inside the bars directory", name)
	}
	p.BarsFile = filepath.Join(barsDir, name)

	// Bitso fee model: the taker fee per leg (the rule fills at the open with
	// a market order), falling back to the legacy single commission rate.
	p.CommissionRate = cfg.TakerFee
	if p.CommissionRate == 0 {
		p.CommissionRate = cfg.CommissionRate
	}
	switch cfg.SlippageModel {
	case "none":
	case "percentage":
		p.SlippageRate = cfg.SlippageValue
	default:
		return p, fmt.Errorf("%s needs slippage_model \"percentage\" or \"none\" (a fixed price offset has no meaning per leg of a daily fill), got %q", Strategy, cfg.SlippageModel)
	}
	c := quantize(p.CommissionRate + p.SlippageRate)
	p.Costs = dailyrule.Costs{Buy: c, Sell: c}
	return p, nil
}

// quantize rounds a per-leg cost to 1e-8 (0.0001 bps). daily-research
// computes (fee_bps + slip_bps) / 1e4; the float sum fee + slip of the same
// rates can land one ulp away (0.0078 + 0.001 != 0.0088). Both 88/1e4 and
// 880000/1e8 are the correctly rounded double of 0.0088, so quantizing makes
// the service's cost identical to daily-research's for any whole or
// hundredth-bps inputs.
func quantize(x float64) float64 { return math.Round(x*1e8) / 1e8 }

// ReadBars reads a Bitso daily CSV (columns date, open, high, low, close;
// others ignored), sorted by date. Parsing matches cmd/daily-research:
// time.Parse("2006-01-02") and strconv.ParseFloat. Duplicate dates are an
// error. The second return is the file's SHA-256, for provenance.
func ReadBars(path string) ([]dailyrule.Bar, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	h := sha256.New()
	r := csv.NewReader(io.TeeReader(f, h))
	header, err := r.Read()
	if err != nil {
		return nil, "", fmt.Errorf("%s: header: %w", path, err)
	}
	col := map[string]int{}
	for i, name := range header {
		col[name] = i
	}
	for _, k := range []string{"date", "open", "high", "low", "close"} {
		if _, ok := col[k]; !ok {
			return nil, "", fmt.Errorf("%s: missing column %q", path, k)
		}
	}
	seen := map[string]bool{}
	var bars []dailyrule.Bar
	for line := 2; ; line++ {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("%s:%d: %w", path, line, err)
		}
		get := func(k string) string {
			if i := col[k]; i < len(rec) {
				return rec[i]
			}
			return ""
		}
		ds := get("date")
		d, err := time.Parse("2006-01-02", ds)
		if err != nil {
			return nil, "", fmt.Errorf("%s:%d: date %q: %w", path, line, ds, err)
		}
		if seen[ds] {
			return nil, "", fmt.Errorf("%s:%d: duplicate date %s", path, line, ds)
		}
		seen[ds] = true
		b := dailyrule.Bar{Date: d}
		for k, dst := range map[string]*float64{"open": &b.Open, "high": &b.High, "low": &b.Low, "close": &b.Close} {
			v, err := strconv.ParseFloat(get(k), 64)
			if err != nil {
				return nil, "", fmt.Errorf("%s:%d: %s: %w", path, line, k, err)
			}
			*dst = v
		}
		bars = append(bars, b)
	}
	if len(bars) == 0 {
		return nil, "", fmt.Errorf("%s: no bars", path)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Date.Before(bars[j].Date) })
	return bars, hex.EncodeToString(h.Sum(nil)), nil
}

// Window returns the inclusive bar range [lo, hi] for the config's dates,
// the same search cmd/daily-research uses: the first bar on or after the
// start date and the last bar on or before the end date.
func Window(bars []dailyrule.Bar, start, end time.Time) (lo, hi int) {
	from := day(start)
	to := day(end)
	lo = sort.Search(len(bars), func(i int) bool { return !bars[i].Date.Before(from) })
	hi = sort.Search(len(bars), func(i int) bool { return bars[i].Date.After(to) }) - 1
	return lo, hi
}

func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Run executes the daily SMA50 backtest for cfg, reading bars from barsDir.
func Run(ctx context.Context, cfg *models.BacktestConfig, barsDir string) (*models.BacktestResult, error) {
	started := time.Now()
	result := models.NewBacktestResult(cfg.ID, cfg.ID)
	result.SetConfigSnapshot(cfg)
	fail := func(err error) (*models.BacktestResult, error) {
		result.MarkFailed(err)
		return result, err
	}

	p, err := ResolveParams(cfg, barsDir)
	if err != nil {
		return fail(err)
	}
	bars, digest, err := ReadBars(p.BarsFile)
	if err != nil {
		return fail(err)
	}
	lo, hi := Window(bars, cfg.StartDate, cfg.EndDate)
	if hi-lo+1 < 2 {
		return fail(fmt.Errorf("not enough bars in window %s..%s (%d)", day(cfg.StartDate).Format("2006-01-02"), day(cfg.EndDate).Format("2006-01-02"), max(hi-lo+1, 0)))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}

	// The indicator runs over the full history so it is warm at the
	// window's start; only trading is restricted to the window.
	want := dailyrule.Trend(bars, SMA)
	res, tr := dailyrule.SimulateTrace(bars, want, lo, hi, p.Costs)

	Fill(result, cfg, p, res, tr)
	result.Metadata["engine"] = "dailyrule"
	result.Metadata["rule"] = fmt.Sprintf("trend_sma%d (frozen; long while close > SMA(%d))", SMA, SMA)
	result.Metadata["timing"] = "decide at day t close, fill at day t+1 open, close out at the window's final close"
	result.Metadata["bars_file"] = filepath.Base(p.BarsFile)
	result.Metadata["bars_sha256"] = digest
	result.Metadata["window_first"] = bars[lo].Date.Format("2006-01-02")
	result.Metadata["window_last"] = bars[hi].Date.Format("2006-01-02")
	result.Metadata["window_bars"] = hi - lo + 1
	result.Metadata["missing_days_in_window"] = missingDays(bars[lo : hi+1])
	result.Metadata["exposure_pct"] = res.ExposurePct
	result.Metadata["cost_pct"] = res.CostPct
	result.Metadata["cost_per_leg"] = p.Costs.Buy
	result.Metadata["commission_rate"] = p.CommissionRate
	result.Metadata["slippage_rate"] = p.SlippageRate
	result.Metadata["annualization_days"] = AnnualizationDays

	result.EvaluateSuccessCriteria(cfg.SuccessCriteria)
	result.MarkCompleted()
	result.Duration = int64(time.Since(started).Seconds())
	return result, nil
}

// Fill maps the simulator's result and trace (per 1.0 of equity) onto the
// service's result model, scaled by the initial balance. Headline numbers
// come straight from res, never recomputed, so they stay bit-identical to
// daily-research.
func Fill(result *models.BacktestResult, cfg *models.BacktestConfig, p Params, res dailyrule.Result, tr dailyrule.Trace) {
	scale := cfg.InitialBalance
	commShare := 1.0
	if total := p.CommissionRate + p.SlippageRate; total > 0 {
		commShare = p.CommissionRate / total
	}

	// Trades: each buy is paired with the next sell (long-only rule).
	cash := 1.0
	var entry *dailyrule.Fill
	var entryCash float64
	totalSlip := 0.0
	for i := range tr.Fills {
		f := tr.Fills[i]
		if f.Buy {
			entry, entryCash = &tr.Fills[i], cash
			cash = f.Cash
			continue
		}
		cash = f.Cash
		if entry == nil {
			continue // cannot happen: the simulator never sells flat
		}
		cost := (entry.Fee + f.Fee) * scale
		t := models.Trade{
			ID:                fmt.Sprintf("%s-%03d", Strategy, len(result.Trades)+1),
			EntryTime:         entry.Date,
			ExitTime:          f.Date,
			Book:              cfg.Book,
			Side:              "buy",
			EntryPrice:        entry.Price,
			ExitPrice:         f.Price,
			Amount:            entry.Units * scale,
			ProfitLoss:        (f.Cash - entryCash) * scale,
			ProfitLossPercent: (f.Cash/entryCash - 1) * 100,
			Commission:        cost * commShare,
			Slippage:          cost * (1 - commShare),
			HoldingTime:       int64(f.Date.Sub(entry.Date).Seconds()),
			StrategySignal:    "close_above_sma50",
		}
		if f.Final {
			t.StrategySignal = "close_out_at_window_end"
		}
		totalSlip += t.Slippage
		result.AddTrade(t)
		entry = nil
	}

	peak := 0.0
	for _, e := range tr.Equity {
		eq := e.Equity * scale
		peak = math.Max(peak, eq)
		result.AddEquityPoint(models.EquityPoint{
			Timestamp: e.Date,
			Balance:   e.Cash * scale,
			Equity:    eq,
			Return:    e.Equity - 1,
			Drawdown:  e.Drawdown,
		})
	}

	final := (1 + res.ReturnPct/100) * scale
	s := &models.PerformanceSummary{
		TotalReturn:        final - scale,
		TotalReturnPercent: res.ReturnPct,
		MaxDrawdownPercent: res.MaxDDPct,
		MaxDrawdown:        maxDrawdownMoney(result.EquityCurve),
		TotalTrades:        res.RoundTrips,
		FinalBalance:       final,
		PeakBalance:        math.Max(peak, final),
		InitialBalance:     scale,
		NetProfitLoss:      final - scale,
		TotalCommissions:   res.CostPct / 100 * scale * commShare,
	}
	if days := len(tr.Equity); days > 0 {
		s.AnnualizedReturn = math.Pow(1+res.ReturnPct/100, float64(AnnualizationDays)/float64(days)) - 1
	}
	rets := dailyReturns(tr.Equity)
	s.Volatility, s.SharpeRatio, s.SortinoRatio = riskRatios(rets)

	var grossWin, grossLoss, holding float64
	for _, t := range result.Trades {
		switch {
		case t.ProfitLoss > 0:
			s.WinningTrades++
			grossWin += t.ProfitLoss
		case t.ProfitLoss < 0:
			s.LosingTrades++
			grossLoss -= t.ProfitLoss
		}
		s.GrossProfitLoss += t.GetGrossProfit()
		holding += float64(t.HoldingTime)
		s.MaxPosition = math.Max(s.MaxPosition, t.Amount)
	}
	if n := len(result.Trades); n > 0 {
		s.WinRate = float64(s.WinningTrades) / float64(n)
		s.AverageHoldingTime = int64(holding / float64(n))
	}
	if s.WinningTrades > 0 {
		s.AverageWin = grossWin / float64(s.WinningTrades)
	}
	if s.LosingTrades > 0 {
		s.AverageLoss = -grossLoss / float64(s.LosingTrades)
		s.ProfitFactor = grossWin / grossLoss
	} // no losing trade: profit factor undefined, left 0 (+Inf does not encode as JSON)
	result.SetSummary(s)
	result.Metadata["total_slippage"] = totalSlip
}

func maxDrawdownMoney(curve []models.EquityPoint) float64 {
	peak, dd := 0.0, 0.0
	for _, p := range curve {
		peak = math.Max(peak, p.Equity)
		dd = math.Max(dd, peak-p.Equity)
	}
	return dd
}

func dailyReturns(eq []dailyrule.EquityPoint) []float64 {
	if len(eq) < 2 {
		return nil
	}
	out := make([]float64, 0, len(eq)-1)
	for i := 1; i < len(eq); i++ {
		if eq[i-1].Equity > 0 {
			out = append(out, eq[i].Equity/eq[i-1].Equity-1)
		}
	}
	return out
}

// riskRatios returns annualized volatility, Sharpe and Sortino of daily
// returns with a zero risk-free rate and target. Volatility uses the sample
// standard deviation; Sortino's downside deviation is the root mean square
// of min(r, 0) over all days (the standard definition, not only the
// negative days).
func riskRatios(r []float64) (vol, sharpe, sortino float64) {
	n := float64(len(r))
	if n < 2 {
		return 0, 0, 0
	}
	mean, down := 0.0, 0.0
	for _, x := range r {
		mean += x
		if x < 0 {
			down += x * x
		}
	}
	mean /= n
	ss := 0.0
	for _, x := range r {
		ss += (x - mean) * (x - mean)
	}
	sd := math.Sqrt(ss / (n - 1))
	ann := math.Sqrt(AnnualizationDays)
	vol = sd * ann
	if sd > 0 {
		sharpe = mean / sd * ann
	}
	if dd := math.Sqrt(down / n); dd > 0 {
		sortino = mean / dd * ann
	}
	return vol, sharpe, sortino
}

// missingDays counts calendar days absent between consecutive bars.
func missingDays(bars []dailyrule.Bar) int {
	n := 0
	for i := 1; i < len(bars); i++ {
		if gap := int(bars[i].Date.Sub(bars[i-1].Date).Hours()/24) - 1; gap > 0 {
			n += gap
		}
	}
	return n
}
