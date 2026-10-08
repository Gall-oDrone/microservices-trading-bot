package risk

import (
	"context"
	"fmt"
	"strings"

	"bitso-trading-platform/order-management/internal/models"
	sharedrisk "bitso-trading-platform/shared/pkg/risk"
)

// Risk step R5b: order-management also checks every order against a policy
// in the shared format (shared/pkg/risk, the format the daily-executor,
// trading-engine and ui-api use) and honours the same operator halt files.
// The env names are the ones trading-engine reads, so one ConfigMap entry
// configures both services.
const (
	// EnvSharedPolicy is a policy JSON file (sharedrisk.LoadPolicy). Unset:
	// no shared-policy limits (the RiskConfig limits above still apply).
	EnvSharedPolicy = "TRADING_RISK_POLICY"
	// EnvHaltFiles is a comma-separated list of halt files (risk-state.json).
	// Any one halted, unreadable or invalid rejects every new order.
	EnvHaltFiles = "TRADING_HALT_FILES"
)

// SharedPolicy is the shared-format gate inside CheckRisk.
type SharedPolicy struct {
	Policy    *sharedrisk.Policy // nil: halt files only
	HaltFiles []string
}

// LoadSharedPolicy reads EnvSharedPolicy and EnvHaltFiles. It returns nil
// (no gate) when neither is set.
func LoadSharedPolicy(getenv func(string) string) (*SharedPolicy, error) {
	sp := &SharedPolicy{}
	for _, f := range strings.Split(getenv(EnvHaltFiles), ",") {
		if f = strings.TrimSpace(f); f != "" {
			sp.HaltFiles = append(sp.HaltFiles, f)
		}
	}
	if path := strings.TrimSpace(getenv(EnvSharedPolicy)); path != "" {
		p, err := sharedrisk.LoadPolicy(path)
		if err != nil {
			return nil, err
		}
		sp.Policy = &p
	}
	if sp.Policy == nil && len(sp.HaltFiles) == 0 {
		return nil, nil
	}
	return sp, nil
}

// SetSharedPolicy installs the shared-format gate (nil removes it).
func (rm *Manager) SetSharedPolicy(sp *SharedPolicy) { rm.shared = sp }

// checkShared returns the blocking findings of the shared gate. A halt file
// that cannot be read or parsed blocks (fail closed).
func (rm *Manager) checkShared(ctx context.Context, order *models.Order) []sharedrisk.Finding {
	sp := rm.shared
	if sp == nil {
		return nil
	}
	pol := sharedrisk.Policy{Version: "halt-files-only"}
	if sp.Policy != nil {
		pol = *sp.Policy
	}
	for _, f := range sp.HaltFiles {
		h, _, err := sharedrisk.LoadHaltState(f)
		if err != nil {
			return []sharedrisk.Finding{{Rule: sharedrisk.RuleHalted, Severity: sharedrisk.Block, Value: 1,
				Message: fmt.Sprintf("%v (fix or remove it; no order is accepted until then)", err)}}
		}
		pol = sharedrisk.ApplyHalt(pol, h)
	}
	var st sharedrisk.State
	if pos, err := rm.positionRepo.Get(ctx, order.Book); err == nil && pos != nil {
		st.PositionBTC = pos.Size
	}
	d := sharedrisk.Check(pol, sharedrisk.Order{
		Book:   order.Book,
		Side:   order.Side,
		QtyBTC: order.Amount,
		Price:  order.Price,
	}, st)
	var out []sharedrisk.Finding
	for _, f := range d.Findings {
		if f.Severity == sharedrisk.Block {
			f.Message = pol.Version + ": " + f.Message
			out = append(out, f)
		}
	}
	return out
}
