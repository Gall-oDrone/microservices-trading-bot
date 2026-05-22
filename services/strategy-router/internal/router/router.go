// Package router contains the engine that turns one indicator snapshot
// into at most one start/stop lifecycle action against strategy-executor.
//
// The engine is intentionally pure: it depends on a Clock and a Client
// interface so unit tests can drive it through a deterministic timeline
// with no real HTTP or sleeps.
package router

import (
	"context"
	"fmt"
	"sync"
	"time"

	"bitso-trading-platform/strategy-router/internal/classifier"
	"bitso-trading-platform/strategy-router/internal/clients"
	"bitso-trading-platform/strategy-router/internal/config"
	"bitso-trading-platform/strategy-router/internal/metrics"
)

// Clock is the minimal time abstraction the engine needs.
type Clock interface {
	Now() time.Time
}

// realClock wraps time.Now for production.
type realClock struct{}

// Now returns the current time.
func (realClock) Now() time.Time { return time.Now() }

// Decision is the user-facing record of a single evaluation cycle.
type Decision struct {
	Timestamp time.Time          `json:"timestamp"`
	Book      string             `json:"book"`
	Regime    string             `json:"regime"`
	Preferred string             `json:"preferred"`
	Current   string             `json:"current"`
	Action    string             `json:"action"` // noop|started|stopped|switched|paused|blocked|dry_run
	Reason    string             `json:"reason"`
	Snapshot  classifier.Decision `json:"snapshot"`
}

// AuditWriter is anything that can persist a Decision to a side channel
// (file, stdout, no-op for tests).
type AuditWriter interface {
	WriteDecision(d Decision) error
}

// Engine holds the mutable router state and dependencies.
type Engine struct {
	cfg     *config.Config
	client  clients.Client
	clock   Clock
	audit   AuditWriter
	metrics *metrics.Metrics

	mu              sync.Mutex
	lastSwitchAt    time.Time
	lastDecision    Decision
	currentActive   string
	recentDecisions []Decision
	maxRecent       int
}

// New builds an Engine. Audit may be nil (a no-op writer is used).
func New(cfg *config.Config, client clients.Client, audit AuditWriter, m *metrics.Metrics) *Engine {
	if audit == nil {
		audit = noopAudit{}
	}
	return &Engine{
		cfg:       cfg,
		client:    client,
		clock:     realClock{},
		audit:     audit,
		metrics:   m,
		maxRecent: 50,
	}
}

// WithClock replaces the clock — for tests.
func (e *Engine) WithClock(c Clock) *Engine {
	e.clock = c
	return e
}

// thresholds copies the classifier knobs out of the config.
func (e *Engine) thresholds() classifier.Thresholds {
	return classifier.Thresholds{
		ATRHighVolPct: e.cfg.ATRHighVolPct,
		ATRLowVolPct:  e.cfg.ATRLowVolPct,
		RSIOverbought: e.cfg.RSIOverbought,
		RSIOversold:   e.cfg.RSIOversold,
		BBUpper:       e.cfg.BBUpper,
		BBLower:       e.cfg.BBLower,
		EMADistEntry:  e.cfg.EMADistEntry,
	}
}

// LastDecision returns a shallow copy of the most recent decision.
func (e *Engine) LastDecision() Decision {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastDecision
}

// RecentDecisions returns up to N most recent decisions (newest last).
func (e *Engine) RecentDecisions() []Decision {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Decision, len(e.recentDecisions))
	copy(out, e.recentDecisions)
	return out
}

// RunOnce performs a single evaluation cycle. It is safe to call from
// the timer loop or the HTTP /run endpoint concurrently — the engine
// holds a mutex around all decisions.
//
// Returned errors are limited to fatal/programming errors; transient
// HTTP/classification failures are reflected on the Decision struct
// itself (Action="noop"/"blocked", Reason explains why).
func (e *Engine) RunOnce(ctx context.Context) (Decision, error) {
	start := e.clock.Now()
	book := e.cfg.Book
	if e.metrics != nil {
		e.metrics.EvaluationsTotal.WithLabelValues(book).Inc()
	}

	defer func() {
		if e.metrics != nil {
			e.metrics.EvaluationLatency.WithLabelValues(book).Observe(float64(time.Since(start).Milliseconds()))
		}
	}()

	snap, err := e.client.GetSnapshot(ctx, book)
	if err != nil {
		if e.metrics != nil {
			e.metrics.EvaluationErrors.WithLabelValues(book).Inc()
		}
		return e.record(Decision{
			Timestamp: start,
			Book:      book,
			Regime:    "neutral",
			Action:    "noop",
			Reason:    fmt.Sprintf("snapshot fetch failed: %v", err),
		}), nil
	}

	cdec := classifier.Classify(snap, e.thresholds())
	if e.metrics != nil {
		e.metrics.SetRegime(book, cdec.Regime)
	}

	preferred := e.cfg.Routes.Resolve(cdec.Regime)

	strategies, err := e.client.ListStrategies(ctx)
	if err != nil {
		if e.metrics != nil {
			e.metrics.EvaluationErrors.WithLabelValues(book).Inc()
		}
		return e.record(Decision{
			Timestamp: start,
			Book:      book,
			Regime:    cdec.Regime,
			Preferred: preferred,
			Action:    "noop",
			Reason:    fmt.Sprintf("list strategies failed: %v", err),
			Snapshot:  cdec,
		}), nil
	}
	current := firstRunningForBook(strategies, book)

	d := Decision{
		Timestamp: start,
		Book:      book,
		Regime:    cdec.Regime,
		Preferred: preferred,
		Current:   current,
		Snapshot:  cdec,
	}

	// Case 1: regime says "pause".
	if config.IsPause(preferred) {
		if current == "" {
			d.Action = "noop"
			d.Reason = "regime pauses trading; no strategy currently running"
			return e.record(d), nil
		}
		// Need to stop the current strategy. Honour the has_position guard.
		state, err := e.client.GetStrategyState(ctx, current)
		if err == nil && state.HasPosition {
			d.Action = "blocked"
			d.Reason = "current strategy holds a position; refusing to pause"
			e.bumpBlocked("has_position")
			return e.record(d), nil
		}
		if e.cfg.DryRun {
			d.Action = "dry_run"
			d.Reason = fmt.Sprintf("would STOP %s (regime=%s → pause)", current, cdec.Regime)
			e.bumpBlocked("dry_run")
			return e.record(d), nil
		}
		if err := e.client.StopStrategy(ctx, current); err != nil {
			d.Action = "noop"
			d.Reason = fmt.Sprintf("stop %s failed: %v", current, err)
			return e.record(d), nil
		}
		e.markSwitch(current, "", cdec.Regime)
		d.Action = "paused"
		d.Reason = fmt.Sprintf("stopped %s; regime %s requires pause", current, cdec.Regime)
		return e.record(d), nil
	}

	// Case 2: preferred already running.
	if preferred == current {
		d.Action = "noop"
		d.Reason = "preferred strategy already running"
		return e.record(d), nil
	}

	// Case 3: need to switch — apply guardrails.
	if current != "" {
		state, err := e.client.GetStrategyState(ctx, current)
		if err == nil && state.HasPosition {
			d.Action = "blocked"
			d.Reason = fmt.Sprintf("%s holds a position; will not hand off to %s", current, preferred)
			e.bumpBlocked("has_position")
			return e.record(d), nil
		}
	}

	if !e.cooldownElapsed(start) {
		d.Action = "blocked"
		d.Reason = fmt.Sprintf("cooldown active (%ds remaining)", e.cooldownRemaining(start))
		e.bumpBlocked("cooldown")
		return e.record(d), nil
	}

	if !isRegistered(strategies, preferred) {
		d.Action = "blocked"
		d.Reason = fmt.Sprintf("preferred strategy %q is not registered", preferred)
		e.bumpBlocked("not_registered")
		return e.record(d), nil
	}

	if e.cfg.DryRun {
		d.Action = "dry_run"
		d.Reason = fmt.Sprintf("would switch from %s → %s", display(current), preferred)
		e.bumpBlocked("dry_run")
		return e.record(d), nil
	}

	// Real switch.
	if current != "" {
		if err := e.client.StopStrategy(ctx, current); err != nil {
			d.Action = "noop"
			d.Reason = fmt.Sprintf("stop %s failed: %v", current, err)
			return e.record(d), nil
		}
	}
	if err := e.client.StartStrategy(ctx, preferred); err != nil {
		d.Action = "noop"
		d.Reason = fmt.Sprintf("start %s failed: %v", preferred, err)
		return e.record(d), nil
	}
	e.markSwitch(current, preferred, cdec.Regime)
	if current == "" {
		d.Action = "started"
		d.Reason = fmt.Sprintf("started %s for regime %s", preferred, cdec.Regime)
	} else {
		d.Action = "switched"
		d.Reason = fmt.Sprintf("switched %s → %s for regime %s", current, preferred, cdec.Regime)
	}
	return e.record(d), nil
}

// Run is the long-running loop. It returns when ctx is done.
func (e *Engine) Run(ctx context.Context) {
	t := time.NewTicker(e.cfg.EvaluationInterval)
	defer t.Stop()

	// One eager evaluation so /api/v1/router/state is populated right
	// away (we don't want operators to see an empty state for a full
	// interval after a restart).
	if _, err := e.RunOnce(ctx); err != nil {
		// RunOnce never returns transient errors — only programming
		// errors. Just log via reason on the recorded decision.
		_ = err
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := e.RunOnce(ctx); err != nil {
				_ = err
			}
		}
	}
}

func (e *Engine) record(d Decision) Decision {
	e.mu.Lock()
	e.lastDecision = d
	e.recentDecisions = append(e.recentDecisions, d)
	if len(e.recentDecisions) > e.maxRecent {
		e.recentDecisions = e.recentDecisions[len(e.recentDecisions)-e.maxRecent:]
	}
	prev := e.currentActive
	if d.Action == "switched" || d.Action == "started" {
		e.currentActive = d.Preferred
	} else if d.Action == "paused" {
		e.currentActive = ""
	}
	active := e.currentActive
	e.mu.Unlock()

	if e.metrics != nil && (d.Action == "switched" || d.Action == "started" || d.Action == "paused") {
		e.metrics.SetActiveStrategy(e.cfg.Book, prev, active)
	}

	_ = e.audit.WriteDecision(d)
	return d
}

func (e *Engine) markSwitch(from, to, regime string) {
	e.mu.Lock()
	e.lastSwitchAt = e.clock.Now()
	e.mu.Unlock()
	if e.metrics != nil {
		e.metrics.SwitchesTotal.WithLabelValues(e.cfg.Book, display(from), display(to), regime).Inc()
	}
}

func (e *Engine) bumpBlocked(reason string) {
	if e.metrics != nil {
		e.metrics.BlockedTotal.WithLabelValues(e.cfg.Book, reason).Inc()
	}
}

func (e *Engine) cooldownElapsed(now time.Time) bool {
	e.mu.Lock()
	last := e.lastSwitchAt
	e.mu.Unlock()
	if last.IsZero() {
		return true
	}
	return now.Sub(last) >= time.Duration(e.cfg.CooldownSeconds)*time.Second
}

func (e *Engine) cooldownRemaining(now time.Time) int {
	e.mu.Lock()
	last := e.lastSwitchAt
	e.mu.Unlock()
	if last.IsZero() {
		return 0
	}
	elapsed := now.Sub(last)
	rem := time.Duration(e.cfg.CooldownSeconds)*time.Second - elapsed
	if rem < 0 {
		return 0
	}
	return int(rem.Seconds())
}

func firstRunning(list []clients.StrategyInfo) string {
	return firstRunningForBook(list, "")
}

// firstRunningForBook returns the running strategy for the book when Book is set on entries.
func firstRunningForBook(list []clients.StrategyInfo, book string) string {
	for _, s := range list {
		if !s.Running {
			continue
		}
		if book == "" || s.Book == "" || s.Book == book {
			return s.Name
		}
	}
	return ""
}

func isRegistered(list []clients.StrategyInfo, name string) bool {
	for _, s := range list {
		if s.Name == name {
			return true
		}
	}
	return false
}

func display(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}

type noopAudit struct{}

func (noopAudit) WriteDecision(Decision) error { return nil }
