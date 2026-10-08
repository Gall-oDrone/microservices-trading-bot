// Package guard holds trading-engine's start-up safety rules (risk step R5,
// docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §6.4):
//
//   - the session limits (daily realized loss, drawdown from peak) come from
//     the environment with non-zero defaults, so the session check that runs
//     before every order actually blocks (they were 0, i.e. disabled);
//   - live mode (DRY_RUN unset) refuses to start without order-management:
//     pre-trade validation and the session P&L both come from it, and without
//     them every order would pass unchecked;
//   - live mode is limited to Bitso stage unless production is explicitly
//     allowed (decided 2026-10-08: production later, stage for now).
//
// It is pure (no I/O beyond the getenv it is given) so main and tests share it.
package guard

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Environment variables.
const (
	EnvMaxDailyLoss    = "TRADING_MAX_DAILY_LOSS"          // quote currency of the book (MXN for btc_mxn)
	EnvMaxDrawdownPct  = "TRADING_MAX_DRAWDOWN_PCT"        // 0 < pct ≤ 100, from peak equity
	EnvAllowProduction = "TRADING_ENGINE_ALLOW_PRODUCTION" // "1" to allow a non-stage Bitso URL in live mode
	EnvOrderManagement = "ORDER_MANAGEMENT_URL"            // pre-trade validation + session risk
)

// Defaults, the same as strategy-executor's built-in risk config
// (cmd/main.go: MaxDailyLoss 500, MaxDrawdownPct 10).
const (
	DefaultMaxDailyLoss   = 500.0
	DefaultMaxDrawdownPct = 10.0
)

// StageHost is the only Bitso host live mode may trade on by default.
const StageHost = "stage.bitso.com"

// Limits are the session limits the executor enforces before every order.
type Limits struct {
	MaxDailyLoss   float64
	MaxDrawdownPct float64
}

// LoadLimits reads the session limits. Unset means the default; a value that
// does not parse, is not positive, or a drawdown above 100 is an error (a
// typo must not silently disable a limit).
func LoadLimits(getenv func(string) string) (Limits, error) {
	l := Limits{MaxDailyLoss: DefaultMaxDailyLoss, MaxDrawdownPct: DefaultMaxDrawdownPct}
	var err error
	if l.MaxDailyLoss, err = positive(getenv, EnvMaxDailyLoss, DefaultMaxDailyLoss); err != nil {
		return Limits{}, err
	}
	if l.MaxDrawdownPct, err = positive(getenv, EnvMaxDrawdownPct, DefaultMaxDrawdownPct); err != nil {
		return Limits{}, err
	}
	if l.MaxDrawdownPct > 100 {
		return Limits{}, fmt.Errorf("%s=%v: a drawdown limit is a percentage in (0, 100]", EnvMaxDrawdownPct, l.MaxDrawdownPct)
	}
	return l, nil
}

func positive(getenv func(string) string, key string, def float64) (float64, error) {
	v := strings.TrimSpace(getenv(key))
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 || f != f { // f != f: NaN
		return 0, fmt.Errorf("%s=%q: want a number > 0 (unset for the default %v)", key, v, def)
	}
	return f, nil
}

// Startup is what CheckStartup needs to know.
type Startup struct {
	DryRun             bool
	OrderManagementURL string
	BitsoBaseURL       string
	AllowProduction    bool
}

// FromEnv fills the parts of Startup that come straight from the environment.
func FromEnv(getenv func(string) string, dryRun bool, bitsoBaseURL string) Startup {
	return Startup{
		DryRun:             dryRun,
		OrderManagementURL: strings.TrimSpace(getenv(EnvOrderManagement)),
		BitsoBaseURL:       bitsoBaseURL,
		AllowProduction:    strings.TrimSpace(getenv(EnvAllowProduction)) == "1",
	}
}

// CheckStartup refuses a live configuration that would place orders without
// order-management's checks or outside Bitso stage. Dry run never places an
// order, so it is always allowed.
func CheckStartup(s Startup) error {
	if s.DryRun {
		return nil
	}
	if s.OrderManagementURL == "" {
		return fmt.Errorf("live mode needs %s: pre-trade validation and the session P&L (daily loss, drawdown) come from order-management; set it, or DRY_RUN=1", EnvOrderManagement)
	}
	u, err := url.Parse(s.BitsoBaseURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("live mode: cannot parse the Bitso API URL %q", s.BitsoBaseURL)
	}
	if !strings.EqualFold(u.Hostname(), StageHost) && !s.AllowProduction {
		return fmt.Errorf("live mode is limited to Bitso stage (%s), got %s; set %s=1 only when production trading is approved", StageHost, u.Host, EnvAllowProduction)
	}
	return nil
}
