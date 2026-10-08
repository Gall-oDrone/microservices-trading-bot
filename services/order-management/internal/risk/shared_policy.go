package risk

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"bitso-trading-platform/order-management/internal/config"
	"bitso-trading-platform/order-management/internal/models"
	sharedrisk "bitso-trading-platform/shared/pkg/risk"
)

// Risk step R5b: order-management also checks every order against a policy
// in the shared format (shared/pkg/risk, the format the daily-executor,
// trading-engine and ui-api use) and honours the same operator halt files.
// The env names are the ones trading-engine reads, so one ConfigMap entry
// configures both services.
//
// Since 2026-10-08 (plan §6.4.6) that policy is order-management's ONLY
// limit set: position, order notional, open orders and orders per minute,
// per book and firm-wide. Without TRADING_RISK_POLICY the policy is built
// from the legacy env limits (MAX_POSITION_SIZE, MAX_ORDER_VALUE,
// MAX_OPEN_ORDERS, MAX_ORDERS_PER_MINUTE), so behaviour is unchanged.
const (
	// EnvSharedPolicy is a policy JSON file (sharedrisk.LoadPolicy). Unset:
	// the policy is built from the RiskConfig env limits (EnvPolicyVersion).
	EnvSharedPolicy = "TRADING_RISK_POLICY"
	// EnvHaltFiles is a comma-separated list of halt files (risk-state.json).
	// Any one halted, unreadable or invalid rejects every new order.
	EnvHaltFiles = "TRADING_HALT_FILES"
	// EnvPolicyVersion is the version of the policy built from env limits.
	EnvPolicyVersion = "order-management-env"
)

// SharedPolicy is the shared-format gate inside CheckRisk.
type SharedPolicy struct {
	// Policy is the effective limit set. nil: built from the RiskConfig env
	// limits (PolicyFromRiskConfig).
	Policy    *sharedrisk.Policy
	HaltFiles []string
	// Source is "env" or "file:<path>", for logs.
	Source string
	// PortfolioFromEnv is true when a policy file had no portfolio section
	// and the env MAX_OPEN_ORDERS / MAX_ORDERS_PER_MINUTE were used.
	PortfolioFromEnv bool
}

// PolicyFromRiskConfig expresses the legacy env limits in the shared format.
// Semantics kept: MAX_POSITION_SIZE and MAX_ORDER_VALUE apply to every book
// (Default); MAX_OPEN_ORDERS and MAX_ORDERS_PER_MINUTE are firm-wide
// (Portfolio), as the old global counters were. config.Validate rejects
// zero or negative env values, so these limits are always on.
func PolicyFromRiskConfig(cfg *config.RiskConfig) sharedrisk.Policy {
	p := sharedrisk.Policy{Version: EnvPolicyVersion, Books: map[string]sharedrisk.BookLimits{}}
	if cfg == nil {
		return p
	}
	p.Default = sharedrisk.BookLimits{
		MaxPositionBTC:   cfg.MaxPositionSize,
		MaxOrderNotional: cfg.MaxOrderValue,
	}
	p.Portfolio = envPortfolio(cfg)
	return p
}

func envPortfolio(cfg *config.RiskConfig) *sharedrisk.PortfolioLimits {
	if cfg == nil || (cfg.MaxOpenOrders <= 0 && cfg.MaxOrdersPerMinute <= 0) {
		return nil
	}
	return &sharedrisk.PortfolioLimits{MaxOpenOrders: cfg.MaxOpenOrders, MaxOrdersPerMinute: cfg.MaxOrdersPerMinute}
}

// LoadSharedPolicy reads EnvSharedPolicy and EnvHaltFiles and returns the
// effective gate (never nil). With a policy file, the file is the source of
// truth for every limit it carries; if it has no "portfolio" section the env
// firm-wide limits are kept (a file written for trading-engine must not
// silently drop order-management's runaway guards).
func LoadSharedPolicy(getenv func(string) string, cfg *config.RiskConfig) (*SharedPolicy, error) {
	sp := &SharedPolicy{Source: "env"}
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
		if p.Portfolio == nil {
			if p.Portfolio = envPortfolio(cfg); p.Portfolio != nil {
				sp.PortfolioFromEnv = true
			}
		}
		sp.Policy = &p
		sp.Source = "file:" + path
		return sp, nil
	}
	p := PolicyFromRiskConfig(cfg)
	sp.Policy = &p
	return sp, nil
}

// SetSharedPolicy installs the shared-format gate. nil (or a nil Policy)
// means the env limits with no halt files.
func (rm *Manager) SetSharedPolicy(sp *SharedPolicy) {
	rm.policyMu.Lock()
	defer rm.policyMu.Unlock()
	rm.shared = sp
}

// EffectivePolicy is the limit set CheckRisk enforces now.
func (rm *Manager) EffectivePolicy() sharedrisk.Policy {
	rm.policyMu.RLock()
	sp := rm.shared
	rm.policyMu.RUnlock()
	if sp != nil && sp.Policy != nil {
		return *sp.Policy
	}
	return PolicyFromRiskConfig(rm.config)
}

func (rm *Manager) haltFiles() []string {
	rm.policyMu.RLock()
	defer rm.policyMu.RUnlock()
	if rm.shared == nil {
		return nil
	}
	return rm.shared.HaltFiles
}

// evaluate runs the shared check with order-management's state: position,
// open orders and accepted orders in the last minute, per book and across
// books. It returns the blocking findings only. A halt file that cannot be
// read, or order state that cannot be read while a limit needs it, blocks
// (fail closed).
func (rm *Manager) evaluate(ctx context.Context, order *models.Order) []sharedrisk.Finding {
	pol := rm.EffectivePolicy()
	for _, f := range rm.haltFiles() {
		h, _, err := sharedrisk.LoadHaltState(f)
		if err != nil {
			return []sharedrisk.Finding{{Rule: sharedrisk.RuleHalted, Severity: sharedrisk.Block, Value: 1,
				Message: fmt.Sprintf("%v (fix or remove it; no order is accepted until then)", err)}}
		}
		pol = sharedrisk.ApplyHalt(pol, h)
	}

	var st sharedrisk.State
	if pos, err := rm.positionRepo.Get(ctx, order.Book); err == nil && pos != nil {
		// Signed: the shared check treats PositionBTC as long-positive.
		st.PositionBTC = SignedPositionBTC(pos)
	}
	var out []sharedrisk.Finding
	l := pol.For(order.Book)
	pf := pol.Portfolio
	if l.MaxOpenOrders > 0 || (pf != nil && pf.MaxOpenOrders > 0) {
		active, err := rm.orderRepo.GetActiveOrders(ctx)
		if err != nil {
			out = append(out, sharedrisk.Finding{Rule: sharedrisk.RuleMaxOpenOrders, Severity: sharedrisk.Block,
				Message: fmt.Sprintf("open orders unknown (%v); refusing while an open-order limit is set", err)})
		}
		for _, o := range active {
			if o.ID == order.ID {
				continue // the order under check is not "before this one"
			}
			st.PortfolioOpenOrders++
			if strings.EqualFold(o.Book, order.Book) {
				st.OpenOrders++
			}
		}
	}
	st.OrdersLastMinute, st.PortfolioOrdersLastMinute = rm.rate.count(order.Book, rm.now())

	d := sharedrisk.Check(pol, sharedrisk.Order{
		Book:   order.Book,
		Side:   order.Side,
		QtyBTC: order.Amount,
		Price:  order.Price,
	}, st)
	for _, f := range d.Findings {
		if f.Severity == sharedrisk.Block {
			f.Message = pol.Version + ": " + f.Message
			out = append(out, f)
		}
	}
	return out
}

// checkShared is evaluate under its R5b name (CheckRisk's first step).
func (rm *Manager) checkShared(ctx context.Context, order *models.Order) []sharedrisk.Finding {
	return rm.evaluate(ctx, order)
}

// rateWindow counts accepted orders in the trailing minute, per book and in
// total. An order is counted once per key (signal id, else order id): the
// signal consumer and POST /api/v1/orders/validate may both check the same
// signal.
type rateWindow struct {
	mu   sync.Mutex
	span time.Duration
	hits []rateHit
	seen map[string]bool
}

type rateHit struct {
	at   time.Time
	book string
	key  string
}

func newRateWindow(span time.Duration) *rateWindow {
	return &rateWindow{span: span, seen: map[string]bool{}}
}

func (w *rateWindow) prune(now time.Time) {
	cut := 0
	for cut < len(w.hits) && now.Sub(w.hits[cut].at) >= w.span {
		delete(w.seen, w.hits[cut].key)
		cut++
	}
	w.hits = w.hits[cut:]
}

func (w *rateWindow) count(book string, now time.Time) (bookN, total int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prune(now)
	for _, h := range w.hits {
		if strings.EqualFold(h.book, book) {
			bookN++
		}
	}
	return bookN, len(w.hits)
}

func (w *rateWindow) record(book, key string, now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.prune(now)
	if key != "" && w.seen[key] {
		return
	}
	w.hits = append(w.hits, rateHit{at: now, book: book, key: key})
	if key != "" {
		w.seen[key] = true
	}
}

func (w *rateWindow) reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.hits = nil
	w.seen = map[string]bool{}
}

func rateKey(o *models.Order) string {
	if o.SignalID != "" {
		return "signal:" + o.SignalID
	}
	return "order:" + o.ID
}
