package strategies

import (
	"context"
	"math"
	"testing"
)

// Bitso retail rates, used so the thresholds in these tests reflect the costs
// the live system actually pays rather than a round number.
const (
	testMakerRate = 0.005  // 0.50%
	testTakerRate = 0.0065 // 0.65%
)

type fakeFeeProvider struct {
	maker, taker float64
	ok           bool
	calls        int
}

func (f *fakeFeeProvider) MakerTakerRatesForBook(_ context.Context, _ string) (float64, float64, bool) {
	f.calls++
	return f.maker, f.taker, f.ok
}

func TestFeeGate_RoundTripRates(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		provider    *fakeFeeProvider
		fallbackBPS float64
		wantBuy     float64
		wantSell    float64
		wantSource  string
	}{
		{
			name:       "provider supplies usable rates",
			provider:   &fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true},
			wantBuy:    testMakerRate,
			wantSell:   testTakerRate,
			wantSource: FeeSourceProvider,
		},
		{
			name:        "provider unavailable falls back to the static estimate",
			provider:    &fakeFeeProvider{ok: false},
			fallbackBPS: 130, // 1.30% round trip => 0.65% per leg
			wantBuy:     0.0065,
			wantSell:    0.0065,
			wantSource:  FeeSourceFallback,
		},
		{
			name:       "no provider and no fallback is unavailable",
			wantSource: FeeSourceUnavailable,
		},
		{
			name:        "nonsense provider rates degrade to the fallback",
			provider:    &fakeFeeProvider{maker: 1.5, taker: 0.0065, ok: true},
			fallbackBPS: 100,
			wantBuy:     0.005,
			wantSell:    0.005,
			wantSource:  FeeSourceFallback,
		},
		{
			name:       "NaN provider rates with no fallback are unavailable",
			provider:   &fakeFeeProvider{maker: math.NaN(), taker: 0.0065, ok: true},
			wantSource: FeeSourceUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p MakerTakerFeeProvider
			if tc.provider != nil {
				p = tc.provider
			}
			g := NewFeeGate(p, tc.fallbackBPS)

			buy, sell, src := g.RoundTripRates(ctx, "btc_mxn")
			if src != tc.wantSource {
				t.Fatalf("source = %q, want %q", src, tc.wantSource)
			}
			if src != FeeSourceUnavailable {
				if buy != tc.wantBuy || sell != tc.wantSell {
					t.Errorf("rates = (%v, %v), want (%v, %v)", buy, sell, tc.wantBuy, tc.wantSell)
				}
			}
		})
	}
}

// TestFeeGate_ShortDirectionIsNotScoredAsLong is the regression guard for the
// most dangerous failure mode here: evaluating a short round-trip with long
// arithmetic makes every short look unprofitable, which silently disables one
// whole side of a strategy without any error surfacing.
func TestFeeGate_ShortDirectionIsNotScoredAsLong(t *testing.T) {
	ctx := context.Background()
	g := NewFeeGate(&fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}, 0)

	// A genuinely good short: sell at 1,020,000 and buy back at 1,000,000.
	entry, exit := 1_020_000.0, 1_000_000.0

	shortNet, _ := g.NetPerBase(ctx, "btc_mxn", DirectionShort, entry, exit)
	if shortNet <= 0 {
		t.Fatalf("short net = %.2f, want positive — a 2%% favourable move must clear ~1.15%% of fees", shortNet)
	}

	// Scored as a long, the same pair is a large loss. If the two agree, the
	// direction argument is being ignored.
	longNet, _ := g.NetPerBase(ctx, "btc_mxn", DirectionLong, entry, exit)
	if longNet >= 0 {
		t.Fatalf("long net = %.2f, want negative for a falling price", longNet)
	}
}

func TestFeeGate_EntryClearsCost(t *testing.T) {
	ctx := context.Background()
	const entry = 1_000_000.0

	// Round-trip cost at maker buy + taker sell is roughly
	// entry*(maker+taker) = 11,500 MXN per base unit.
	tests := []struct {
		name         string
		dir          PositionDirection
		expectedExit float64
		minNet       float64
		wantOK       bool
	}{
		{
			name:         "long: move far smaller than fees is suppressed",
			dir:          DirectionLong,
			expectedExit: 1_002_000, // +0.2%, fees ~1.15%
			wantOK:       false,
		},
		{
			name:         "long: move comfortably above fees still fires",
			dir:          DirectionLong,
			expectedExit: 1_030_000, // +3.0%
			wantOK:       true,
		},
		{
			name:         "long: just under break-even is suppressed",
			dir:          DirectionLong,
			expectedExit: 1_011_000, // ~+1.1% vs ~1.15% cost
			wantOK:       false,
		},
		{
			name:         "long: just over break-even fires",
			dir:          DirectionLong,
			expectedExit: 1_012_000,
			wantOK:       true,
		},
		{
			name:         "long: required margin can suppress an otherwise passing trade",
			dir:          DirectionLong,
			expectedExit: 1_012_000,
			minNet:       5_000, // demand 5000 MXN/base of net edge
			wantOK:       false,
		},
		{
			name:         "short: favourable move clears cost",
			dir:          DirectionShort,
			expectedExit: 970_000, // -3.0%
			wantOK:       true,
		},
		{
			name:         "short: tiny move is suppressed",
			dir:          DirectionShort,
			expectedExit: 998_000, // -0.2%
			wantOK:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewFeeGate(&fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}, 0)
			ok, net, src := g.EntryClearsCost(ctx, "btc_mxn", tc.dir, entry, tc.expectedExit, tc.minNet)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v (net=%.2f source=%s)", ok, tc.wantOK, net, src)
			}
			if src != FeeSourceProvider {
				t.Errorf("source = %q, want %q", src, FeeSourceProvider)
			}
		})
	}
}

func TestFeeGate_FailOpenVsFailClosed(t *testing.T) {
	ctx := context.Background()
	g := NewFeeGate(nil, 0) // no rates available at all

	ok, _, src := g.EntryClearsCost(ctx, "btc_mxn", DirectionLong, 1_000_000, 1_000_100, 0)
	if !ok {
		t.Error("EntryClearsCost must FAIL OPEN when fee data is unavailable — a dead fee feed must not halt trading")
	}
	if src != FeeSourceUnavailable {
		t.Errorf("source = %q, want %q so the caller can alert", src, FeeSourceUnavailable)
	}

	okStrict, _, _ := g.EntryClearsCostStrict(ctx, "btc_mxn", DirectionLong, 1_000_000, 1_000_100, 0)
	if okStrict {
		t.Error("EntryClearsCostStrict must FAIL CLOSED when fee data is unavailable")
	}
}

func TestFeeGate_ExitIsNetProfitable(t *testing.T) {
	ctx := context.Background()
	g := NewFeeGate(&fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}, 0)
	const entry = 1_000_000.0

	tests := []struct {
		name   string
		exit   float64
		dir    PositionDirection
		wantOK bool
	}{
		{name: "long: exit above break-even is profitable", exit: 1_020_000, dir: DirectionLong, wantOK: true},
		{name: "long: exit at entry is a net loss after fees", exit: entry, dir: DirectionLong, wantOK: false},
		{name: "long: small gain still net-negative after fees", exit: 1_005_000, dir: DirectionLong, wantOK: false},
		{name: "short: buy-back below entry clears fees", exit: 970_000, dir: DirectionShort, wantOK: true},
		{name: "short: buy-back at entry is a net loss", exit: entry, dir: DirectionShort, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, net, _ := g.ExitIsNetProfitable(ctx, "btc_mxn", tc.dir, entry, tc.exit)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v (net = %.2f)", ok, tc.wantOK, net)
			}
		})
	}
}

func TestFeeGate_MinProfitableExitMatchesBitsoHelper(t *testing.T) {
	ctx := context.Background()
	g := NewFeeGate(&fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}, 0)

	got, src := g.MinProfitableExit(ctx, "btc_mxn", 1_000_000)
	if src != FeeSourceProvider {
		t.Fatalf("source = %q", src)
	}
	// entry * (1+buy) / (1-sell)
	want := 1_000_000 * (1 + testMakerRate) / (1 - testTakerRate)
	if math.Abs(got-want) > 0.01 {
		t.Errorf("threshold = %.4f, want %.4f", got, want)
	}

	// Exiting exactly at the threshold must be break-even, not a loss.
	net, _ := g.NetPerBase(ctx, "btc_mxn", DirectionLong, 1_000_000, got)
	if math.Abs(net) > 0.01 {
		t.Errorf("net at the break-even threshold = %.6f, want ~0", net)
	}
}

func TestFeeGate_HasRates(t *testing.T) {
	if NewFeeGate(nil, 0).HasRates() {
		t.Error("an unconfigured gate must report HasRates()==false so callers can preserve legacy behaviour")
	}
	if !NewFeeGate(nil, 50).HasRates() {
		t.Error("a fallback-only gate should report HasRates()==true")
	}
	if !NewFeeGate(&fakeFeeProvider{ok: true}, 0).HasRates() {
		t.Error("a provider-backed gate should report HasRates()==true")
	}
}

func TestFeeGate_InvalidPricesDegradeSafely(t *testing.T) {
	ctx := context.Background()
	g := NewFeeGate(&fakeFeeProvider{maker: testMakerRate, taker: testTakerRate, ok: true}, 0)

	for _, tc := range []struct {
		name        string
		entry, exit float64
	}{
		{"zero entry", 0, 1_000_000},
		{"negative entry", -5, 1_000_000},
		{"zero exit", 1_000_000, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, _, src := g.EntryClearsCost(ctx, "btc_mxn", DirectionLong, tc.entry, tc.exit, 0)
			if src != FeeSourceUnavailable {
				t.Errorf("source = %q, want %q", src, FeeSourceUnavailable)
			}
			if !ok {
				t.Error("must fail open rather than block on a malformed price")
			}
		})
	}
}

// TestDefaultFallbackCoversProductionTaker pins the fallback to the production
// btc_mxn taker rate confirmed via GET /v3/fees on 2026-09-25 (78 bps/leg).
// An underestimated fallback lets sub-cost trades through whenever the live
// fee provider is absent, so lowering it should be a deliberate, reviewed act.
func TestDefaultFallbackCoversProductionTaker(t *testing.T) {
	const productionTakerBPS = 78.0
	if DefaultFallbackRoundTripBPS < 2*productionTakerBPS {
		t.Fatalf("DefaultFallbackRoundTripBPS = %.0f, below the production taker round trip %.0f",
			DefaultFallbackRoundTripBPS, 2*productionTakerBPS)
	}
	g := NewFeeGate(nil, DefaultFallbackRoundTripBPS)
	buy, sell, src := g.RoundTripRates(context.Background(), "btc_mxn")
	if src != FeeSourceFallback || math.Abs(buy-0.0078) > 1e-12 || math.Abs(sell-0.0078) > 1e-12 {
		t.Fatalf("fallback legs = %v/%v (%s), want 0.0078/0.0078 (fallback)", buy, sell, src)
	}
}
