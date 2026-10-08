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
)

// Portfolio exposure and VaR (plan §6.4.7). Every interval the monitor marks
// each book's position to market and publishes exposure per book, net
// exposure per base asset, gross/net exposure and a 1-day 99 % parametric VaR
// per quote currency. It is reporting, not a control: nothing is blocked on
// VaR; the alert on the VaR limit escalates to the risk function.
//
// VaR = z(99 %) x |sum over the currency's books of exposure x daily vol|.
// Books in one quote currency are assumed perfectly correlated (they share
// the base asset), and the daily vol is a configured model parameter
// (RISK_VAR_DAILY_VOL*), published so the assumption is visible. A historical
// or EWMA estimate needs daily closes the cluster does not have yet.
const (
	EnvPortfolioInterval = "RISK_PORTFOLIO_INTERVAL"
	EnvVaRDailyVol       = "RISK_VAR_DAILY_VOL"
	EnvVaRDailyVolBooks  = "RISK_VAR_DAILY_VOL_BOOKS"
	EnvVaRLimits         = "RISK_VAR_LIMITS"
	EnvMarketDataURL     = "MARKET_DATA_URL"

	// DefaultVaRDailyVol is deliberately above BTC's typical realized daily
	// vol (~2.5-3.5 %), so an unconfigured VaR errs high.
	DefaultVaRDailyVol       = 0.04
	DefaultPortfolioInterval = 30 * time.Second
)

// PortfolioConfig parameterises the monitor.
type PortfolioConfig struct {
	Interval        time.Duration
	DefaultDailyVol float64
	DailyVol        map[string]float64 // per book (lower case)
	VaRLimits       map[string]float64 // per quote currency (upper case)
	MarketDataURL   string
}

// VolFor is the daily vol assumed for book.
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
	limits, err := parsePairs(getenv(EnvVaRLimits), strings.ToUpper, func(s string) (float64, error) {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || !(v > 0) || math.IsInf(v, 0) {
			return 0, fmt.Errorf("limit %q: want a positive number", s)
		}
		return v, nil
	})
	if err != nil {
		return c, fmt.Errorf("%s: %w", EnvVaRLimits, err)
	}
	c.VaRLimits = limits
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

// PortfolioMonitor computes and publishes the snapshot.
type PortfolioMonitor struct {
	Positions repository.PositionRepository
	Marks     MarkSource // nil: entry-price fallback only
	Config    PortfolioConfig
	Series    *metrics.RiskSeries
	Now       func() time.Time
	OnError   func(error)
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
	volSum := map[string]float64{} // currency -> sum(exposure x vol)
	for _, book := range names {
		p := byBook[book]
		asset, ccy := splitBook(book)
		be := metrics.BookExposure{Book: book, Asset: asset, Currency: ccy, Base: SignedPositionBTC(p), DailyVol: pm.Config.VolFor(book)}
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
	}
	for ccy, r := range snap.Currencies {
		r.VaR = metrics.VaRZ * math.Abs(volSum[ccy])
		r.Limit = pm.Config.VaRLimits[ccy]
		snap.Currencies[ccy] = r
	}
	// A configured limit is published even before any position exists.
	for ccy, lim := range pm.Config.VaRLimits {
		if _, ok := snap.Currencies[ccy]; !ok {
			snap.Currencies[ccy] = metrics.CurrencyRisk{Limit: lim}
		}
	}
	return snap, nil
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
