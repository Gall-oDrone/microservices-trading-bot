// Package config loads strategy-router configuration from environment variables.
//
// The environment-variable surface intentionally mirrors scripts/strategy-regime-router.sh
// so operators can move from the bash router to this service without re-learning knobs.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the resolved runtime configuration.
type Config struct {
	ServiceName string
	Host        string
	Port        int

	// Books is the list of markets to route (comma-separated in STRATEGY_ROUTER_BOOK).
	Books []string
	// Book is the primary book (first entry in Books); kept for backward-compatible logging.
	Book                string
	StrategyExecutorURL string

	// Polling and lifecycle.
	EvaluationInterval time.Duration
	CooldownSeconds    int
	DryRun             bool

	// Regime thresholds — kept compatible with the bash router defaults.
	ATRHighVolPct float64
	ATRLowVolPct  float64
	RSIOverbought float64
	RSIOversold   float64
	BBUpper       float64
	BBLower       float64
	EMADistEntry  float64

	// Routing table: regime -> strategy name.
	Routes RouteTable

	// Audit log file (matches the bash script's /tmp default).
	AuditLogPath string

	// HTTP client timeout for calls to strategy-executor.
	HTTPTimeout time.Duration

	// AutoStart controls whether the router loop runs on process start.
	// When false, evaluations only happen via POST /api/v1/router/run.
	AutoStart bool
}

// RouteTable maps regime name -> preferred strategy name. A value of
// "none" (or empty) means "pause trading".
type RouteTable struct {
	LowVolRange  string
	TrendingUp   string
	TrendingDown string
	HighVol      string
	Neutral      string
}

// Load builds a Config from environment variables.
func Load() *Config {
	books := ParseBooks(getenv("STRATEGY_ROUTER_BOOK", "btc_mxn"))
	book := books[0]

	return &Config{
		ServiceName:         getenv("STRATEGY_ROUTER_SERVICE_NAME", "strategy-router"),
		Host:                getenv("STRATEGY_ROUTER_HOST", "0.0.0.0"),
		Port:                getenvInt("STRATEGY_ROUTER_PORT", 8092),
		Books:               books,
		Book:                book,
		StrategyExecutorURL: getenv("STRATEGY_EXECUTOR_URL", "http://strategy-executor:8081"),

		EvaluationInterval: time.Duration(getenvInt("ROUTER_INTERVAL_SEC", 30)) * time.Second,
		CooldownSeconds:    getenvInt("COOLDOWN_SECS", 120),
		DryRun:             getenvBool("DRY_RUN", false),

		ATRHighVolPct: getenvFloat("ATR_HIGH_VOL_PCT", 1.5),
		ATRLowVolPct:  getenvFloat("ATR_LOW_VOL_PCT", 0.30),
		RSIOverbought: getenvFloat("RSI_OVERBOUGHT", 70),
		RSIOversold:   getenvFloat("RSI_OVERSOLD", 30),
		BBUpper:       getenvFloat("BB_PB_UPPER", 0.85),
		BBLower:       getenvFloat("BB_PB_LOWER", 0.15),
		EMADistEntry:  getenvFloat("EMA_DIST_ENTRY_PCT", 0.10),

		Routes: defaultRoutesForBook(book),

		AuditLogPath: getenv("ROUTER_AUDIT_LOG_PATH", "/tmp/strategy-regime-router.log"),
		HTTPTimeout:  time.Duration(getenvInt("STRATEGY_ROUTER_HTTP_TIMEOUT_MS", 5000)) * time.Millisecond,
		AutoStart:    getenvBool("ROUTER_AUTOSTART", true),
	}
}

// Resolve returns the configured route name for the given regime label.
// An empty string or "none" (case-insensitive) means "pause".
func (rt RouteTable) Resolve(regime string) string {
	switch regime {
	case "low_vol_range":
		return rt.LowVolRange
	case "trending_up":
		return rt.TrendingUp
	case "trending_down":
		return rt.TrendingDown
	case "high_vol":
		return rt.HighVol
	default:
		return rt.Neutral
	}
}

// ParseBooks splits a comma-separated book list (e.g. "btc_mxn,eth_mxn").
func ParseBooks(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"btc_mxn"}
	}
	return out
}

// ForBook returns a copy of cfg scoped to a single book with default route names.
func (c *Config) ForBook(book string) *Config {
	cp := *c
	cp.Book = book
	cp.Books = []string{book}
	cp.Routes = defaultRoutesForBook(book)
	return &cp
}

func defaultRoutesForBook(book string) RouteTable {
	return RouteTable{
		LowVolRange:  getenv("ROUTE_LOW_VOL", fmt.Sprintf("mean_reversion_%s", book)),
		TrendingUp:   getenv("ROUTE_TRENDING_UP", fmt.Sprintf("momentum_%s", book)),
		TrendingDown: getenv("ROUTE_TRENDING_DOWN", fmt.Sprintf("momentum_%s", book)),
		HighVol:      getenv("ROUTE_HIGH_VOL", "none"),
		Neutral:      getenv("ROUTE_NEUTRAL", fmt.Sprintf("mean_reversion_%s", book)),
	}
}

// IsPause returns true when a resolved route value should be treated as
// "do nothing / stop the active strategy".
func IsPause(route string) bool {
	r := strings.ToLower(strings.TrimSpace(route))
	return r == "" || r == "none"
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

func getenvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func getenvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "t", "true", "yes", "y", "on":
		return true
	case "0", "f", "false", "no", "n", "off":
		return false
	}
	return fallback
}
