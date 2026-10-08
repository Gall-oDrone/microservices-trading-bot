package guard

import (
	"context"
	"errors"
	"sort"
	"time"

	"bitso-trading-platform/shared/pkg/risk"
)

// Telemetry for the pre-trade gate and the halt files (plan §6.4.4). The
// guard package stays free of Prometheus: internal/metrics.RiskMetrics
// satisfies these interfaces.

// GateObserver receives one record per checked order.
type GateObserver interface {
	RecordPolicyCheck(book, result string, rules []string)
	ObserveUtilization(book, limit string, value, max float64)
	ObservePriceDeviation(book, side string, bps float64)
}

// Check results, matching internal/metrics Policy* constants.
const (
	ResultAllowed = "allowed"
	ResultBlocked = "blocked"
	ResultError   = "error"
)

// Check is Allow plus telemetry: limit utilization and price deviation are
// observed for every order (allowed or not), then the outcome and its rules.
func (p *PreTrade) Check(ctx context.Context, o risk.Order, obs GateObserver) error {
	if obs != nil {
		l := p.Policy.For(o.Book)
		obs.ObserveUtilization(o.Book, risk.RuleMaxOrderBTC, o.QtyBTC, l.MaxOrderBTC)
		obs.ObserveUtilization(o.Book, risk.RuleMaxOrderNotional, o.QtyBTC*o.Price, l.MaxOrderNotional)
		if bps, ok := risk.DeviationBps(o.Price, o.RefPrice); ok {
			obs.ObservePriceDeviation(o.Book, o.Side, bps)
		}
	}
	d, err := p.Allow(ctx, o)
	if obs != nil {
		switch {
		case err == nil:
			obs.RecordPolicyCheck(o.Book, ResultAllowed, nil)
		case errors.Is(err, ErrFailClosed):
			obs.RecordPolicyCheck(o.Book, ResultError, BlockingRules(d, err))
		default:
			obs.RecordPolicyCheck(o.Book, ResultBlocked, BlockingRules(d, err))
		}
	}
	return err
}

// LimitSetter receives configured limit values.
type LimitSetter interface {
	SetLimit(book, limit string, v float64)
}

// PublishLimits exports every per-book limit of the policy (and the
// default under book "default") so dashboards show what is enforced.
func PublishLimits(p risk.Policy, s LimitSetter) {
	books := p.BookNames()
	sort.Strings(books)
	put := func(book string, l risk.BookLimits) {
		s.SetLimit(book, risk.RuleMaxOrderBTC, l.MaxOrderBTC)
		s.SetLimit(book, risk.RuleMaxOrderNotional, l.MaxOrderNotional)
		s.SetLimit(book, risk.RuleMaxPositionBTC, l.MaxPositionBTC)
		s.SetLimit(book, risk.RulePriceDeviation, l.MaxPriceDeviationBps)
	}
	for _, b := range books {
		put(b, p.Books[b])
	}
	put("default", p.Default)
}

// HaltSink receives the halt-file evaluation.
type HaltSink func(configured, invalid int, halted bool, at time.Time, details []string)

// WatchHalts evaluates the halt files now and then every interval until ctx
// ends, so the kill-switch state is visible even when no signal arrives.
func WatchHalts(ctx context.Context, files []string, interval time.Duration, sink HaltSink) {
	eval := func() {
		halted, invalid, details := HaltStatus(files)
		sink(len(files), invalid, halted, time.Now(), details)
	}
	eval()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			eval()
		}
	}
}
