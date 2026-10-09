package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/order-management/internal/metrics"
	"bitso-trading-platform/order-management/internal/models"
	"bitso-trading-platform/order-management/internal/repository"
	"bitso-trading-platform/shared/pkg/bitsodaily"
	"bitso-trading-platform/shared/pkg/varmodel"
)

// Portfolio exposure and VaR (plan §6.4.7). Every interval the monitor marks
// each book's position to market and publishes exposure per book, net
// exposure per base asset, gross/net exposure and a 1-day 99 % parametric VaR
// per quote currency. It is reporting, not a control: nothing is blocked on
// VaR; the alert on the VaR limit escalates to the risk function.
//
// VaR = z(99 %) x |sum over the currency's books of exposure x daily vol|.
// Books in one quote currency are assumed perfectly correlated (they share
// the base asset). Since §6.4.8 the daily vol is estimated from Bitso daily
// closes (volatility.go: max(EWMA 0.94, 365-day), backtested); the
// configured RISK_VAR_DAILY_VOL is the fallback while no fresh estimate
// exists, and RISK_VAR_DAILY_VOL_BOOKS pins a book's vol (a desk override).
// RISK_VAR_VOL_MODEL=fixed restores the configured-parameter behaviour. A
// historical-simulation VaR over the same 365 days is published alongside;
// the limit applies to the parametric VaR.
//
// Since §6.4.9 each currency also gets a 97.5 % expected shortfall
// (historical over the same days, and normal) and stress losses: uniform
// spot shocks (RISK_STRESS_SHOCKS) and the historical crypto crises in
// varmodel.Episodes replayed on today's exposure, against an optional
// RISK_STRESS_LIMITS. Reporting only, like VaR.
//
// Since §6.4.10 an optional RISK_CAPITAL per currency expresses every
// measure as a fraction of capital and gives the reverse-stress move (the
// uniform spot move that loses all of it).
const (
	EnvPortfolioInterval = "RISK_PORTFOLIO_INTERVAL"
	EnvVaRDailyVol       = "RISK_VAR_DAILY_VOL"
	EnvVaRDailyVolBooks  = "RISK_VAR_DAILY_VOL_BOOKS"
	EnvVaRLimits         = "RISK_VAR_LIMITS"
	EnvMarketDataURL     = "MARKET_DATA_URL"
	EnvVaRVolModel       = "RISK_VAR_VOL_MODEL"
	EnvVaREWMALambda     = "RISK_VAR_EWMA_LAMBDA"
	EnvVaRVolRefresh     = "RISK_VAR_VOL_REFRESH"
	EnvVaRVolStale       = "RISK_VAR_VOL_STALE"
	EnvVaRVolSourceURL   = "RISK_VAR_VOL_SOURCE_URL"
	EnvStressShocks      = "RISK_STRESS_SHOCKS"
	EnvStressLimits      = "RISK_STRESS_LIMITS"
	EnvCapital           = "RISK_CAPITAL"

	// DefaultVaRDailyVol is deliberately above BTC's typical realized daily
	// vol (~2.5-3.5 %), so an unconfigured VaR errs high.
	DefaultVaRDailyVol       = 0.04
	DefaultPortfolioInterval = 30 * time.Second

	// Vol models.
	VolModelEstimated = "estimated"
	VolModelFixed     = "fixed"

	// Vol sources, as published in risk_var_vol_source.
	VolSourceEstimated = "estimated" // varmodel estimate, fresh
	VolSourceFallback  = "fallback"  // estimator on, no fresh estimate: RISK_VAR_DAILY_VOL
	VolSourceOverride  = "override"  // RISK_VAR_DAILY_VOL_BOOKS
	VolSourceFixed     = "fixed"     // RISK_VAR_VOL_MODEL=fixed: RISK_VAR_DAILY_VOL

	// MinHistScenarios is the fewest aligned days for a historical VaR.
	MinHistScenarios = 250
)

// DefaultStressShocks are the hypothetical spot moves applied to every
// book at once: crashes the size of the worst Bitso episodes, and a rally
// for short positions.
var DefaultStressShocks = []float64{-0.5, -0.3, -0.2, -0.1, 0.2}

// PortfolioConfig parameterises the monitor.
type PortfolioConfig struct {
	Interval        time.Duration
	DefaultDailyVol float64
	DailyVol        map[string]float64 // per book (lower case): overrides
	VaRLimits       map[string]float64 // per quote currency (upper case)
	MarketDataURL   string

	VolModel     string // VolModelEstimated (default) or VolModelFixed
	Lambda       float64
	VolRefresh   time.Duration
	VolStale     time.Duration
	VolSourceURL string

	StressShocks []float64          // spot moves, e.g. -0.3
	StressLimits map[string]float64 // per quote currency (upper case)

	Capital map[string]float64 // per quote currency (upper case)
}

// VolFor is the configured daily vol for book: its override, else the
// default (the fallback when an estimate is in use).
func (c PortfolioConfig) VolFor(book string) float64 {
	if v, ok := c.DailyVol[strings.ToLower(book)]; ok {
		return v
	}
	return c.DefaultDailyVol
}

// LoadPortfolioConfig reads the RISK_* env. Like the trading limits, a value
// that does not parse stops start-up rather than silently using a default.
func LoadPortfolioConfig(getenv func(string) string) (PortfolioConfig, error) {
	c := PortfolioConfig{
		Interval:        DefaultPortfolioInterval,
		DefaultDailyVol: DefaultVaRDailyVol,
		DailyVol:        map[string]float64{},
		VaRLimits:       map[string]float64{},
		MarketDataURL:   strings.TrimRight(strings.TrimSpace(getenv(EnvMarketDataURL)), "/"),
		VolModel:        VolModelEstimated,
		Lambda:          varmodel.Lambda,
		VolRefresh:      DefaultVolRefresh,
		VolStale:        DefaultVolStale,
		VolSourceURL:    bitsodaily.DefaultBaseURL,
		StressShocks:    append([]float64(nil), DefaultStressShocks...),
		StressLimits:    map[string]float64{},
	}
	if s := strings.TrimSpace(getenv(EnvStressShocks)); s != "" {
		c.StressShocks = nil
		seen := map[float64]bool{}
		for _, part := range strings.Split(s, ",") {
			v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
			if err != nil || !(v > -1) || v == 0 || v > 10 || seen[v] {
				return c, fmt.Errorf("%s=%q: want distinct non-zero spot moves above -1, e.g. -0.5,-0.3,0.2", EnvStressShocks, s)
			}
			seen[v] = true
			c.StressShocks = append(c.StressShocks, v)
		}
	}
	switch s := strings.ToLower(strings.TrimSpace(getenv(EnvVaRVolModel))); s {
	case "", VolModelEstimated:
	case VolModelFixed:
		c.VolModel = VolModelFixed
	default:
		return c, fmt.Errorf("%s=%q: want %q or %q", EnvVaRVolModel, s, VolModelEstimated, VolModelFixed)
	}
	if s := strings.TrimSpace(getenv(EnvVaREWMALambda)); s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || !(v >= 0.8 && v < 1) {
			return c, fmt.Errorf("%s=%q: want a decay in [0.8, 1), e.g. 0.94", EnvVaREWMALambda, s)
		}
		c.Lambda = v
	}
	for _, d := range []struct {
		env string
		dst *time.Duration
		min time.Duration
	}{{EnvVaRVolRefresh, &c.VolRefresh, time.Minute}, {EnvVaRVolStale, &c.VolStale, time.Hour}} {
		if s := strings.TrimSpace(getenv(d.env)); s != "" {
			v, err := time.ParseDuration(s)
			if err != nil || v < d.min {
				return c, fmt.Errorf("%s=%q: want a duration of at least %s", d.env, s, d.min)
			}
			*d.dst = v
		}
	}
	if s := strings.TrimRight(strings.TrimSpace(getenv(EnvVaRVolSourceURL)), "/"); s != "" {
		if u, err := url.Parse(s); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return c, fmt.Errorf("%s=%q: want http(s)://host[:port]", EnvVaRVolSourceURL, s)
		}
		c.VolSourceURL = s
	}
	if s := strings.TrimSpace(getenv(EnvPortfolioInterval)); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d < time.Second {
			return c, fmt.Errorf("%s=%q: want a duration of at least 1s", EnvPortfolioInterval, s)
		}
		c.Interval = d
	}
	if s := strings.TrimSpace(getenv(EnvVaRDailyVol)); s != "" {
		v, err := parseVol(s)
		if err != nil {
			return c, fmt.Errorf("%s: %w", EnvVaRDailyVol, err)
		}
		c.DefaultDailyVol = v
	}
	books, err := parsePairs(getenv(EnvVaRDailyVolBooks), strings.ToLower, parseVol)
	if err != nil {
		return c, fmt.Errorf("%s: %w", EnvVaRDailyVolBooks, err)
	}
	c.DailyVol = books
	positive := func(s string) (float64, error) {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || !(v > 0) || math.IsInf(v, 0) {
			return 0, fmt.Errorf("limit %q: want a positive number", s)
		}
		return v, nil
	}
	limits, err := parsePairs(getenv(EnvVaRLimits), strings.ToUpper, positive)
	if err != nil {
		return c, fmt.Errorf("%s: %w", EnvVaRLimits, err)
	}
	c.VaRLimits = limits
	if c.StressLimits, err = parsePairs(getenv(EnvStressLimits), strings.ToUpper, positive); err != nil {
		return c, fmt.Errorf("%s: %w", EnvStressLimits, err)
	}
	if c.Capital, err = parsePairs(getenv(EnvCapital), strings.ToUpper, positive); err != nil {
		return c, fmt.Errorf("%s: %w", EnvCapital, err)
	}
	if c.MarketDataURL != "" {
		if u, err := url.Parse(c.MarketDataURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return c, fmt.Errorf("%s=%q: want http(s)://host[:port]", EnvMarketDataURL, c.MarketDataURL)
		}
	}
	return c, nil
}

// parseVol accepts a daily vol as a ratio in (0, 1], e.g. 0.035.
func parseVol(s string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || !(v > 0) || v > 1 {
		return 0, fmt.Errorf("daily vol %q: want a ratio in (0, 1], e.g. 0.035", s)
	}
	return v, nil
}

// parsePairs parses "k=v,k=v".
func parsePairs(s string, key func(string) string, val func(string) (float64, error)) (map[string]float64, error) {
	out := map[string]float64{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		k = key(strings.TrimSpace(k))
		if !ok || k == "" {
			return nil, fmt.Errorf("%q: want key=value", part)
		}
		f, err := val(strings.TrimSpace(v))
		if err != nil {
			return nil, err
		}
		out[k] = f
	}
	return out, nil
}

// MarkSource prices a book. An error means "no market price"; the monitor
// then falls back to the position's average entry price.
type MarkSource interface {
	Mark(ctx context.Context, book string) (float64, error)
}

// MarketDataMarks reads market-data's GET /api/v1/ticker (the Bitso ticker it
// caches) and marks at the bid/ask mid, else the last trade.
type MarketDataMarks struct {
	BaseURL string
	Client  *http.Client
}

func (m MarketDataMarks) Mark(ctx context.Context, book string) (float64, error) {
	c := m.Client
	if c == nil {
		c = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		m.BaseURL+"/api/v1/ticker?book="+url.QueryEscape(book), nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("market-data ticker %s: HTTP %d", book, resp.StatusCode)
	}
	var t struct {
		Bid  string `json:"bid"`
		Ask  string `json:"ask"`
		Last string `json:"last"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&t); err != nil {
		return 0, fmt.Errorf("market-data ticker %s: %w", book, err)
	}
	f := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	bid, ask, last := f(t.Bid), f(t.Ask), f(t.Last)
	if bid > 0 && ask > 0 && ask >= bid {
		return (bid + ask) / 2, nil
	}
	if last > 0 {
		return last, nil
	}
	return 0, fmt.Errorf("market-data ticker %s: no usable price", book)
}

// SignedPositionBTC is the position long-positive. models.Position keeps an
// unsigned Size and a Side.
func SignedPositionBTC(p *models.Position) float64 {
	if p == nil {
		return 0
	}
	if p.Side == "short" {
		return -p.Size
	}
	return p.Size
}

// splitBook returns the base asset (lower case) and quote currency (upper
// case) of a Bitso book such as "btc_mxn".
func splitBook(book string) (asset, currency string) {
	b := strings.ToLower(strings.TrimSpace(book))
	if i := strings.LastIndex(b, "_"); i > 0 && i < len(b)-1 {
		return b[:i], strings.ToUpper(b[i+1:])
	}
	return b, "UNKNOWN"
}

// PortfolioSnapshot is one run's result.
type PortfolioSnapshot struct {
	Books      []metrics.BookExposure
	NetBase    map[string]float64
	Currencies map[string]metrics.CurrencyRisk
	At         time.Time
}

// VolLookup is the estimator as the monitor sees it (VolEstimator).
type VolLookup interface {
	Lookup(book string) (VolView, bool)
}

// PortfolioMonitor computes and publishes the snapshot.
type PortfolioMonitor struct {
	Positions repository.PositionRepository
	Marks     MarkSource // nil: entry-price fallback only
	Config    PortfolioConfig
	Series    *metrics.RiskSeries
	Vol       VolLookup // nil: configured vol only (RISK_VAR_VOL_MODEL=fixed)
	Now       func() time.Time
	OnError   func(error)
}

// bookVol picks the daily vol for book and fills the model fields of be.
// It returns the book's historical returns when the estimate is used, and
// its stress-episode paths whenever known (they do not go stale).
func (pm *PortfolioMonitor) bookVol(be *metrics.BookExposure) ([]varmodel.Return, map[string][]varmodel.PathPoint) {
	be.VolDataAgeSeconds = -1
	if v, ok := pm.Config.DailyVol[be.Book]; ok {
		be.DailyVol, be.VolSource = v, VolSourceOverride
		view, fresh := pm.attachModel(be)
		if fresh {
			return view.Estimate.HistReturns, view.Episodes // historical VaR does not use the vol
		}
		return nil, view.Episodes
	}
	be.DailyVol = pm.Config.DefaultDailyVol
	if pm.Vol == nil {
		be.VolSource = VolSourceFixed
		return nil, nil
	}
	view, fresh := pm.attachModel(be)
	if !fresh {
		be.VolSource = VolSourceFallback
		return nil, view.Episodes
	}
	be.DailyVol, be.VolSource = view.Estimate.Vol, VolSourceEstimated
	return view.Estimate.HistReturns, view.Episodes
}

// attachModel copies the estimator's view of be.Book into be (published
// even when overridden or stale, so the model stays visible).
func (pm *PortfolioMonitor) attachModel(be *metrics.BookExposure) (VolView, bool) {
	if pm.Vol == nil {
		return VolView{}, false
	}
	view, fresh := pm.Vol.Lookup(be.Book)
	if view.Have {
		e := view.Estimate
		be.Model = &metrics.VolModel{EWMA: e.EWMA, Long: e.Long, Estimate: e.Vol, Backtest: e.Backtest}
		be.VolDataAgeSeconds = view.DataAge.Seconds()
	}
	if len(view.Episodes) > 0 {
		be.EpisodeTroughs = map[string]float64{}
		for id, p := range view.Episodes {
			be.EpisodeTroughs[id] = varmodel.Trough(p)
		}
	}
	return view, fresh
}

// historicalRisk revalues the currency's exposures over the days on which
// every exposed book has a return, P&L_d = sum(exposure x (e^r - 1)), and
// returns the 99 % VaR and 97.5 % ES of those P&Ls.
func historicalRisk(exposed map[string]float64, rets map[string][]varmodel.Return) (hvar, es float64, n int, ok bool) {
	if len(exposed) == 0 {
		return 0, 0, 0, true
	}
	pnl := map[time.Time]float64{}
	count := map[time.Time]int{}
	for book, q := range exposed {
		rs, ok := rets[book]
		if !ok {
			return 0, 0, 0, false // an exposed book without history
		}
		for _, r := range rs {
			pnl[r.Date] += q * math.Expm1(r.R)
			count[r.Date]++
		}
	}
	var scen []float64
	for d, c := range count {
		if c == len(exposed) {
			scen = append(scen, pnl[d])
		}
	}
	hvar, ok = varmodel.HistoricalVaR(scen, varmodel.Confidence, MinHistScenarios)
	es, _ = varmodel.ExpectedShortfall(scen, varmodel.ESConfidence, MinHistScenarios)
	return hvar, es, len(scen), ok
}

// shockID names a hypothetical scenario, e.g. "spot-30%".
func shockID(s float64) string { return fmt.Sprintf("spot%+g%%", math.Round(s*1000)/10) }

// stressScenarios runs the hypothetical shocks and, when episodes are
// known (estimated model), the historical episodes on one currency's
// exposures. episodes holds every snapshot book's paths, so a book with no
// data for an episode (it did not trade then) can be proxied by another
// book on the same base asset, in book-name order.
func (pm *PortfolioMonitor) stressScenarios(net float64, exposed map[string]float64, episodes map[string]map[string][]varmodel.PathPoint) []metrics.StressResult {
	out := make([]metrics.StressResult, 0, len(pm.Config.StressShocks)+len(varmodel.Episodes))
	for _, s := range pm.Config.StressShocks {
		out = append(out, metrics.StressResult{Scenario: shockID(s), Type: metrics.StressHypothetical, Loss: varmodel.ShockLoss(net, s), OK: true})
	}
	if pm.Vol == nil {
		return out
	}
	donors := make([]string, 0, len(episodes))
	for b := range episodes {
		donors = append(donors, b)
	}
	sort.Strings(donors)
	for _, ep := range varmodel.Episodes {
		res := metrics.StressResult{Scenario: ep.ID, Type: metrics.StressHistorical, OK: true}
		paths := map[string][]varmodel.PathPoint{}
		for book := range exposed {
			if p, ok := episodes[book][ep.ID]; ok {
				paths[book] = p
				continue
			}
			asset, _ := splitBook(book)
			for _, d := range donors {
				if a, _ := splitBook(d); a == asset && d != book {
					if p, ok := episodes[d][ep.ID]; ok {
						paths[book], res.Proxied = p, true
						break
					}
				}
			}
		}
		res.Loss, res.OK = varmodel.EpisodeLoss(exposed, paths)
		out = append(out, res)
	}
	return out
}

// Snapshot marks every position and aggregates. Books seen once stay in the
// snapshot at zero after they close, so their gauges do not freeze at the
// last open value.
func (pm *PortfolioMonitor) Snapshot(ctx context.Context, known map[string]bool) (PortfolioSnapshot, error) {
	now := time.Now
	if pm.Now != nil {
		now = pm.Now
	}
	positions, err := pm.Positions.GetAll(ctx)
	if err != nil {
		return PortfolioSnapshot{}, err
	}
	byBook := map[string]*models.Position{}
	for _, p := range positions {
		if p == nil || p.Book == "" {
			continue
		}
		byBook[strings.ToLower(p.Book)] = p
	}
	for b := range known {
		if _, ok := byBook[b]; !ok {
			byBook[b] = nil
		}
	}
	names := make([]string, 0, len(byBook))
	for b := range byBook {
		names = append(names, b)
	}
	sort.Strings(names)

	snap := PortfolioSnapshot{NetBase: map[string]float64{}, Currencies: map[string]metrics.CurrencyRisk{}, At: now()}
	volSum := map[string]float64{}                           // currency -> sum(exposure x vol)
	exposed := map[string]map[string]float64{}               // currency -> book -> exposure
	hist := map[string]map[string][]varmodel.Return{}        // currency -> book -> returns
	episodes := map[string]map[string][]varmodel.PathPoint{} // book -> episode -> path
	for _, book := range names {
		p := byBook[book]
		asset, ccy := splitBook(book)
		be := metrics.BookExposure{Book: book, Asset: asset, Currency: ccy, Base: SignedPositionBTC(p)}
		rets, eps := pm.bookVol(&be)
		if eps != nil {
			episodes[book] = eps
		}
		if pm.Marks != nil {
			if m, err := pm.Marks.Mark(ctx, book); err == nil && m > 0 {
				be.Mark = m
			}
		}
		if be.Mark == 0 {
			// No market price: an open book is flagged even when there is no
			// entry price either (its exposure then reads 0, which is wrong).
			be.Fallback = be.Base != 0
			if p != nil && p.EntryPrice > 0 {
				be.Mark = p.EntryPrice
			}
		}
		be.Quote = be.Base * be.Mark
		snap.Books = append(snap.Books, be)
		snap.NetBase[asset] += be.Base
		r := snap.Currencies[ccy]
		r.Gross += math.Abs(be.Quote)
		r.Net += be.Quote
		snap.Currencies[ccy] = r
		volSum[ccy] += be.Quote * be.DailyVol
		if exposed[ccy] == nil {
			exposed[ccy], hist[ccy] = map[string]float64{}, map[string][]varmodel.Return{}
		}
		if be.Quote != 0 {
			exposed[ccy][book] = be.Quote
		}
		if rets != nil {
			hist[ccy][book] = rets
		}
	}
	for ccy, r := range snap.Currencies {
		r.VaR = metrics.VaRZ * math.Abs(volSum[ccy])
		r.ESParam = varmodel.ZES * math.Abs(volSum[ccy])
		if pm.Vol != nil {
			r.HistVaR, r.HistES, r.HistScenarios, r.HistOK = historicalRisk(exposed[ccy], hist[ccy])
		}
		r.Limit = pm.Config.VaRLimits[ccy]
		r.Stress = pm.stressScenarios(r.Net, exposed[ccy], episodes)
		r.StressLimit = pm.Config.StressLimits[ccy]
		r.Capital = pm.Config.Capital[ccy]
		snap.Currencies[ccy] = r
	}
	// A configured limit is published even before any position exists.
	for ccy := range unionKeys(pm.Config.VaRLimits, pm.Config.StressLimits, pm.Config.Capital) {
		if _, ok := snap.Currencies[ccy]; !ok {
			snap.Currencies[ccy] = metrics.CurrencyRisk{
				Limit:       pm.Config.VaRLimits[ccy],
				StressLimit: pm.Config.StressLimits[ccy],
				Stress:      pm.stressScenarios(0, nil, nil),
				Capital:     pm.Config.Capital[ccy],
			}
		}
	}
	return snap, nil
}

func unionKeys(ms ...map[string]float64) map[string]bool {
	out := map[string]bool{}
	for _, m := range ms {
		for k := range m {
			out[k] = true
		}
	}
	return out
}

// Run publishes a snapshot every Config.Interval until ctx ends.
func (pm *PortfolioMonitor) Run(ctx context.Context) {
	interval := pm.Config.Interval
	if interval <= 0 {
		interval = DefaultPortfolioInterval
	}
	known := map[string]bool{}
	tick := func() {
		c, cancel := context.WithTimeout(ctx, interval)
		defer cancel()
		snap, err := pm.Snapshot(c, known)
		if err != nil {
			pm.Series.RecordRunError()
			if pm.OnError != nil {
				pm.OnError(err)
			}
			return
		}
		for _, b := range snap.Books {
			known[b.Book] = true
		}
		pm.Series.SetPortfolio(snap.Books, snap.NetBase, snap.Currencies, snap.At)
	}
	tick()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}
