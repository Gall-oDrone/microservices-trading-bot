package server

import (
	"log"
	"net/http"
	"os"
	"sync/atomic"
)

// The strategy-executor /api/v1/backtests API is deprecated (plan §7 item 5,
// 2026-10-08). services/backtesting is the canonical backtest engine: it runs
// the frozen SMA50 daily rule (strategy "sma50_daily") on the shared rule and
// simulator, bit-identical to cmd/daily-research, plus the event-driven
// strategies. This in-process engine (internal/backtest) is kept, not
// deleted, so cmd/backtest-archive and the regime studies still build.
//
// The routes answer 410 Gone with a pointer to the replacement. Setting
// STRATEGY_EXECUTOR_LEGACY_BACKTESTS=1 restores the old handler for a
// transition period (it still needs a trade source wired in main).

// LegacyBacktestsEnv re-enables the deprecated routes when set to "1".
const LegacyBacktestsEnv = "STRATEGY_EXECUTOR_LEGACY_BACKTESTS"

// BacktestsSuccessor is where callers should go instead.
const BacktestsSuccessor = "services/backtesting POST /api/v1/backtests (strategy \"sma50_daily\" for the frozen daily rule)"

func legacyBacktestsEnabled() bool { return os.Getenv(LegacyBacktestsEnv) == "1" }

// goneHits counts calls to the deprecated routes, so the log stays readable:
// the first call and every 100th after it are logged.
var goneHits atomic.Int64

// backtestsGone answers a deprecated /api/v1/backtests call.
func backtestsGone(w http.ResponseWriter, r *http.Request) {
	if n := goneHits.Add(1); n == 1 || n%100 == 0 {
		log.Printf("WARN deprecated endpoint called: %s %s from %s (call %d); use %s, or set %s=1 for the legacy handler",
			r.Method, r.URL.Path, r.RemoteAddr, n, BacktestsSuccessor, LegacyBacktestsEnv)
	}
	w.Header().Set("Deprecation", "true")
	writeJSON(w, http.StatusGone, map[string]interface{}{
		"error":     "gone",
		"message":   "strategy-executor /api/v1/backtests is deprecated; services/backtesting is the canonical backtest engine",
		"successor": BacktestsSuccessor,
		"legacy":    "set " + LegacyBacktestsEnv + "=1 on strategy-executor to restore the old handler temporarily",
	})
}
