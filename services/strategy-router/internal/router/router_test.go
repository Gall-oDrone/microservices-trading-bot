package router

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"bitso-trading-platform/strategy-router/internal/classifier"
	"bitso-trading-platform/strategy-router/internal/clients"
	"bitso-trading-platform/strategy-router/internal/config"
)

// --- Test doubles ---

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) Advance(d time.Duration) { f.now = f.now.Add(d) }

type fakeClient struct {
	mu sync.Mutex

	snapshot    classifier.Snapshot
	snapshotErr error

	strategies   []clients.StrategyInfo
	listErr      error
	state        map[string]clients.StrategyState
	stateErr     error
	startedCalls []string
	stoppedCalls []string
	startErr     error
	stopErr      error
}

func (c *fakeClient) GetSnapshot(ctx context.Context, book string) (classifier.Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.snapshotErr != nil {
		return classifier.Snapshot{}, c.snapshotErr
	}
	snap := c.snapshot
	snap.Book = book
	return snap, nil
}

func (c *fakeClient) ListStrategies(ctx context.Context) ([]clients.StrategyInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listErr != nil {
		return nil, c.listErr
	}
	out := make([]clients.StrategyInfo, len(c.strategies))
	copy(out, c.strategies)
	return out, nil
}

func (c *fakeClient) GetStrategyState(ctx context.Context, name string) (clients.StrategyState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stateErr != nil {
		return clients.StrategyState{}, c.stateErr
	}
	if c.state == nil {
		return clients.StrategyState{Name: name}, nil
	}
	st, ok := c.state[name]
	if !ok {
		return clients.StrategyState{Name: name}, nil
	}
	if st.Name == "" {
		st.Name = name
	}
	return st, nil
}

func (c *fakeClient) StartStrategy(ctx context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.startErr != nil {
		return c.startErr
	}
	c.startedCalls = append(c.startedCalls, name)
	for i := range c.strategies {
		c.strategies[i].Running = c.strategies[i].Name == name
	}
	return nil
}

func (c *fakeClient) StopStrategy(ctx context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopErr != nil {
		return c.stopErr
	}
	c.stoppedCalls = append(c.stoppedCalls, name)
	for i := range c.strategies {
		if c.strategies[i].Name == name {
			c.strategies[i].Running = false
		}
	}
	return nil
}

// --- Helpers ---

func baseConfig() *config.Config {
	return &config.Config{
		Book:               "btc_mxn",
		EvaluationInterval: time.Second,
		CooldownSeconds:    120,
		DryRun:             false,
		ATRHighVolPct:      1.5,
		ATRLowVolPct:       0.30,
		RSIOverbought:      70,
		RSIOversold:        30,
		BBUpper:            0.85,
		BBLower:            0.15,
		EMADistEntry:       0.10,
		Routes: config.RouteTable{
			LowVolRange:  "mean_reversion_btc_mxn",
			TrendingUp:   "momentum_btc_mxn",
			TrendingDown: "momentum_btc_mxn",
			HighVol:      "none",
			Neutral:      "mean_reversion_btc_mxn",
		},
	}
}

func newEngine(t *testing.T, cfg *config.Config, fc *fakeClient, clk *fakeClock) *Engine {
	t.Helper()
	e := New(cfg, fc, nil, nil).WithClock(clk)
	return e
}

// --- Tests ---

func TestRunOnce_StartsPreferredWhenNoneRunning(t *testing.T) {
	cfg := baseConfig()
	fc := &fakeClient{
		snapshot: classifier.Snapshot{
			Price: 1_000_000, ATR: 1_000, EMA: 1_000_000, RSI: 50,
			BBUpper: 1_005_000, BBMiddle: 1_000_000, BBLower: 995_000,
		},
		strategies: []clients.StrategyInfo{
			{Name: "mean_reversion_btc_mxn", Running: false},
			{Name: "momentum_btc_mxn", Running: false},
		},
	}
	clk := &fakeClock{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
	e := newEngine(t, cfg, fc, clk)

	d, err := e.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce err: %v", err)
	}
	if d.Regime != "low_vol_range" {
		t.Fatalf("regime = %q, want low_vol_range", d.Regime)
	}
	if d.Action != "started" {
		t.Fatalf("action = %q, want started", d.Action)
	}
	if len(fc.startedCalls) != 1 || fc.startedCalls[0] != "mean_reversion_btc_mxn" {
		t.Fatalf("startedCalls = %v", fc.startedCalls)
	}
}

func TestRunOnce_RefusesSwitchWhenHasPosition(t *testing.T) {
	cfg := baseConfig()
	fc := &fakeClient{
		// trending_up snapshot → preferred is momentum
		snapshot: classifier.Snapshot{
			Price: 1_000_000, ATR: 6_000, EMA: 995_000, RSI: 60,
			BBUpper: 1_010_000, BBMiddle: 1_000_000, BBLower: 990_000,
		},
		strategies: []clients.StrategyInfo{
			{Name: "mean_reversion_btc_mxn", Running: true},
			{Name: "momentum_btc_mxn", Running: false},
		},
		state: map[string]clients.StrategyState{
			"mean_reversion_btc_mxn": {Name: "mean_reversion_btc_mxn", HasPosition: true},
		},
	}
	clk := &fakeClock{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
	e := newEngine(t, cfg, fc, clk)

	d, err := e.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce err: %v", err)
	}
	if d.Regime != "trending_up" {
		t.Fatalf("regime = %q, want trending_up", d.Regime)
	}
	if d.Action != "blocked" {
		t.Fatalf("action = %q, want blocked", d.Action)
	}
	if len(fc.startedCalls) != 0 || len(fc.stoppedCalls) != 0 {
		t.Fatalf("no lifecycle calls expected; got start=%v stop=%v", fc.startedCalls, fc.stoppedCalls)
	}
}

func TestRunOnce_BlocksOnCooldown(t *testing.T) {
	cfg := baseConfig()
	cfg.CooldownSeconds = 120
	fc := &fakeClient{
		snapshot: classifier.Snapshot{
			Price: 1_000_000, ATR: 6_000, EMA: 995_000, RSI: 60,
			BBUpper: 1_010_000, BBMiddle: 1_000_000, BBLower: 990_000,
		},
		strategies: []clients.StrategyInfo{
			{Name: "mean_reversion_btc_mxn", Running: true},
			{Name: "momentum_btc_mxn", Running: false},
		},
		// No position — cooldown is the only thing blocking.
	}
	clk := &fakeClock{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
	e := newEngine(t, cfg, fc, clk)

	// First call switches (cooldown not yet armed).
	if _, err := e.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce err: %v", err)
	}
	if len(fc.startedCalls) != 1 {
		t.Fatalf("expected 1 start, got %v", fc.startedCalls)
	}

	// Reverse the snapshot — now the engine wants to go back to mean_reversion,
	// but cooldown should hold it.
	fc.mu.Lock()
	fc.snapshot = classifier.Snapshot{
		Price: 1_000_000, ATR: 1_000, EMA: 1_000_000, RSI: 50,
		BBUpper: 1_005_000, BBMiddle: 1_000_000, BBLower: 995_000,
	}
	fc.mu.Unlock()

	clk.Advance(30 * time.Second) // < 120s cooldown

	d, err := e.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second RunOnce err: %v", err)
	}
	if d.Action != "blocked" {
		t.Fatalf("action = %q, want blocked (cooldown)", d.Action)
	}
	if len(fc.startedCalls) != 1 {
		t.Fatalf("expected still 1 start; got %v", fc.startedCalls)
	}
}

func TestRunOnce_SwitchesAfterCooldown(t *testing.T) {
	cfg := baseConfig()
	cfg.CooldownSeconds = 10
	fc := &fakeClient{
		snapshot: classifier.Snapshot{
			Price: 1_000_000, ATR: 6_000, EMA: 995_000, RSI: 60,
			BBUpper: 1_010_000, BBMiddle: 1_000_000, BBLower: 990_000,
		},
		strategies: []clients.StrategyInfo{
			{Name: "mean_reversion_btc_mxn", Running: true},
			{Name: "momentum_btc_mxn", Running: false},
		},
	}
	clk := &fakeClock{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
	e := newEngine(t, cfg, fc, clk)

	if _, err := e.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce err: %v", err)
	}

	// Switch the regime back; advance past cooldown.
	fc.mu.Lock()
	fc.snapshot = classifier.Snapshot{
		Price: 1_000_000, ATR: 1_000, EMA: 1_000_000, RSI: 50,
		BBUpper: 1_005_000, BBMiddle: 1_000_000, BBLower: 995_000,
	}
	fc.mu.Unlock()
	clk.Advance(60 * time.Second)

	d, err := e.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("second RunOnce err: %v", err)
	}
	if d.Action != "switched" {
		t.Fatalf("action = %q, want switched after cooldown", d.Action)
	}
}

func TestRunOnce_BlocksWhenPreferredNotRegistered(t *testing.T) {
	cfg := baseConfig()
	cfg.Routes.TrendingUp = "exotic_strategy_that_does_not_exist"
	fc := &fakeClient{
		snapshot: classifier.Snapshot{
			Price: 1_000_000, ATR: 6_000, EMA: 995_000, RSI: 60,
			BBUpper: 1_010_000, BBMiddle: 1_000_000, BBLower: 990_000,
		},
		strategies: []clients.StrategyInfo{
			{Name: "mean_reversion_btc_mxn", Running: true},
		},
	}
	clk := &fakeClock{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
	e := newEngine(t, cfg, fc, clk)

	d, err := e.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce err: %v", err)
	}
	if d.Action != "blocked" {
		t.Fatalf("action = %q, want blocked (not_registered)", d.Action)
	}
	if len(fc.stoppedCalls) != 0 || len(fc.startedCalls) != 0 {
		t.Fatalf("no lifecycle calls expected; got start=%v stop=%v", fc.startedCalls, fc.stoppedCalls)
	}
}

func TestRunOnce_PausesOnHighVol(t *testing.T) {
	cfg := baseConfig()
	fc := &fakeClient{
		snapshot: classifier.Snapshot{
			Price: 1_000_000, ATR: 25_000, EMA: 999_000, RSI: 55,
			BBUpper: 1_010_000, BBMiddle: 1_000_000, BBLower: 990_000,
		},
		strategies: []clients.StrategyInfo{
			{Name: "mean_reversion_btc_mxn", Running: true},
		},
	}
	clk := &fakeClock{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
	e := newEngine(t, cfg, fc, clk)

	d, err := e.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce err: %v", err)
	}
	if d.Regime != "high_vol" {
		t.Fatalf("regime = %q, want high_vol", d.Regime)
	}
	if d.Action != "paused" {
		t.Fatalf("action = %q, want paused", d.Action)
	}
	if len(fc.stoppedCalls) != 1 || fc.stoppedCalls[0] != "mean_reversion_btc_mxn" {
		t.Fatalf("stoppedCalls = %v", fc.stoppedCalls)
	}
}

func TestRunOnce_DryRunDoesNotMutate(t *testing.T) {
	cfg := baseConfig()
	cfg.DryRun = true
	fc := &fakeClient{
		snapshot: classifier.Snapshot{
			Price: 1_000_000, ATR: 6_000, EMA: 995_000, RSI: 60,
			BBUpper: 1_010_000, BBMiddle: 1_000_000, BBLower: 990_000,
		},
		strategies: []clients.StrategyInfo{
			{Name: "mean_reversion_btc_mxn", Running: true},
			{Name: "momentum_btc_mxn", Running: false},
		},
	}
	clk := &fakeClock{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
	e := newEngine(t, cfg, fc, clk)

	d, err := e.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce err: %v", err)
	}
	if d.Action != "dry_run" {
		t.Fatalf("action = %q, want dry_run", d.Action)
	}
	if len(fc.startedCalls) != 0 || len(fc.stoppedCalls) != 0 {
		t.Fatalf("dry_run should not mutate; got start=%v stop=%v", fc.startedCalls, fc.stoppedCalls)
	}
}

func TestRunOnce_SnapshotErrorDegradesGracefully(t *testing.T) {
	cfg := baseConfig()
	fc := &fakeClient{
		snapshotErr: errors.New("connection refused"),
	}
	clk := &fakeClock{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
	e := newEngine(t, cfg, fc, clk)

	d, err := e.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce err: %v", err)
	}
	if d.Action != "noop" {
		t.Fatalf("action = %q, want noop", d.Action)
	}
	if d.Reason == "" {
		t.Fatalf("expected an error reason, got empty")
	}
}

func TestRunOnce_NoopWhenPreferredAlreadyRunning(t *testing.T) {
	cfg := baseConfig()
	fc := &fakeClient{
		snapshot: classifier.Snapshot{
			Price: 1_000_000, ATR: 1_000, EMA: 1_000_000, RSI: 50,
			BBUpper: 1_005_000, BBMiddle: 1_000_000, BBLower: 995_000,
		},
		strategies: []clients.StrategyInfo{
			{Name: "mean_reversion_btc_mxn", Running: true},
			{Name: "momentum_btc_mxn", Running: false},
		},
	}
	clk := &fakeClock{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
	e := newEngine(t, cfg, fc, clk)

	d, err := e.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce err: %v", err)
	}
	if d.Action != "noop" {
		t.Fatalf("action = %q, want noop (preferred already running)", d.Action)
	}
}

func TestRecentDecisions_RingBuffer(t *testing.T) {
	cfg := baseConfig()
	fc := &fakeClient{
		snapshot: classifier.Snapshot{
			Price: 1_000_000, ATR: 1_000, EMA: 1_000_000, RSI: 50,
			BBUpper: 1_005_000, BBMiddle: 1_000_000, BBLower: 995_000,
		},
		strategies: []clients.StrategyInfo{{Name: "mean_reversion_btc_mxn", Running: true}},
	}
	clk := &fakeClock{now: time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)}
	e := newEngine(t, cfg, fc, clk)
	e.maxRecent = 3

	for i := 0; i < 5; i++ {
		_, _ = e.RunOnce(context.Background())
	}
	if got := len(e.RecentDecisions()); got != 3 {
		t.Fatalf("recent decisions = %d, want 3 (ring buffer cap)", got)
	}
}
