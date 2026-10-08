package guard

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"bitso-trading-platform/shared/pkg/risk"
)

// Risk step R5b: trading-engine checks every order against a policy in the
// shared format (shared/pkg/risk, the same file format the daily-executor
// and ui-api use) and honours the same operator halt files, so the kill
// switch (HALT ALL) and a per-ledger halt reach it too.
const (
	// EnvRiskPolicy is a policy JSON file (risk.LoadPolicy). Unset: DefaultPolicy.
	EnvRiskPolicy = "TRADING_RISK_POLICY"
	// EnvHaltFiles is a comma-separated list of halt files (risk-state.json,
	// risk.HaltState). Any one halted, unreadable or invalid blocks every new
	// order. Point it at the halt files ui-api writes (e.g. the stage ledger's
	// risk-state.json) so HALT ALL stops this engine as well.
	EnvHaltFiles = "TRADING_HALT_FILES"
)

// DefaultPolicy is trading-engine's built-in policy: the limits it already
// had in createTradingConfig (0.1 BTC and 10,000 MXN per order) plus a
// fat-finger guard against the touch. Position and order-count limits are
// left to order-management (position, open orders, orders per minute), and
// the daily-executor's one-leg-per-day limits do not fit intraday strategies.
func DefaultPolicy() risk.Policy {
	return risk.Policy{
		Version: "trading-engine-default-2026-10-08",
		Books: map[string]risk.BookLimits{
			"btc_mxn": {MaxOrderBTC: 0.1, MaxOrderNotional: 10000, MaxPriceDeviationBps: 500},
			"btc_usd": {MaxOrderBTC: 0.1, MaxOrderNotional: 600, MaxPriceDeviationBps: 500},
		},
		Default: risk.BookLimits{MaxOrderBTC: 0.01, MaxPriceDeviationBps: 500},
	}
}

// LoadPolicy reads EnvRiskPolicy (DefaultPolicy when unset).
func LoadPolicy(getenv func(string) string) (risk.Policy, string, error) {
	path := strings.TrimSpace(getenv(EnvRiskPolicy))
	if path == "" {
		return DefaultPolicy(), "built-in", nil
	}
	p, err := risk.LoadPolicy(path)
	return p, path, err
}

// HaltFiles reads EnvHaltFiles.
func HaltFiles(getenv func(string) string) []string {
	var out []string
	for _, f := range strings.Split(getenv(EnvHaltFiles), ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// PositionFunc returns the current base-currency position for a book (from
// order-management's exposure endpoint), so a sell that only reduces the
// position is never trapped by a size limit.
type PositionFunc func(ctx context.Context, book string) (float64, error)

// PreTrade is the per-order gate.
type PreTrade struct {
	Policy    risk.Policy
	HaltFiles []string
	Position  PositionFunc // nil: position 0 (dry run without order-management)
}

// ErrBlocked wraps a policy decision that blocks the order.
var ErrBlocked = errors.New("blocked by risk policy")

// Allow returns nil when the order may be sent. Every failure to know the
// halt state or the position blocks the order (fail closed).
func (p *PreTrade) Allow(ctx context.Context, o risk.Order) (risk.Decision, error) {
	pol := p.Policy
	for _, f := range p.HaltFiles {
		h, _, err := risk.LoadHaltState(f)
		if err != nil {
			d := risk.Decision{Findings: []risk.Finding{{Rule: risk.RuleHalted, Severity: risk.Block, Value: 1,
				Message: err.Error() + " (fix or remove it; no order is sent until then)"}}}
			return d, fmt.Errorf("%w: %s", ErrBlocked, d.Findings[0].Message)
		}
		pol = risk.ApplyHalt(pol, h)
	}
	var st risk.State
	if p.Position != nil {
		pos, err := p.Position(ctx, o.Book)
		if err != nil {
			return risk.Decision{}, fmt.Errorf("%w: position for %s unknown: %v", ErrBlocked, o.Book, err)
		}
		st.PositionBTC = pos
	}
	d := risk.Check(pol, o, st)
	if !d.Allowed {
		msgs := make([]string, 0, len(d.Findings))
		for _, f := range d.Findings {
			if f.Severity == risk.Block {
				msgs = append(msgs, f.Rule+": "+f.Message)
			}
		}
		return d, fmt.Errorf("%w (%s): %s", ErrBlocked, pol.Version, strings.Join(msgs, "; "))
	}
	return d, nil
}
