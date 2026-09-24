package strategies

import (
	"context"
	"math"

	"bitso-trading-platform/shared/pkg/bitso"
)

// PositionDirection distinguishes a long round-trip (buy then sell) from a
// short one (sell then buy). It exists because fee arithmetic is not
// symmetric: the fee is charged on each leg's own notional, so which leg comes
// first changes the break-even price. Treating a short as a long with the
// prices swapped gives the wrong answer and — worse — reads as "never
// profitable", which would silently disable an entire side of a strategy.
type PositionDirection int

const (
	// DirectionLong is buy-then-sell.
	DirectionLong PositionDirection = iota
	// DirectionShort is sell-then-buy.
	DirectionShort
)

// Fee rate sources reported by FeeGate, for logging and for tests that need to
// assert which path was taken.
const (
	// FeeSourceProvider means live rates came from the Bitso fee provider.
	FeeSourceProvider = "bitso_api"
	// FeeSourceFallback means the configured static estimate was used.
	FeeSourceFallback = "fallback_bps"
	// FeeSourceUnavailable means no usable rates were obtainable.
	FeeSourceUnavailable = "unavailable"
)

// FeeGate answers "does this trade clear its own round-trip cost?".
//
// It is a value type with no internal state beyond its inputs so that
// strategies can hold it by value and copy it freely without locking.
type FeeGate struct {
	provider MakerTakerFeeProvider
	// fallbackRoundTripBPS is the assumed TOTAL round-trip cost in basis
	// points, used when no provider is configured. It is split evenly across
	// the two legs. Zero means "no fallback" — the gate then reports
	// FeeSourceUnavailable and fails open, preserving pre-gate behaviour.
	fallbackRoundTripBPS float64
}

// NewFeeGate builds a gate. Either argument may be zero-valued.
func NewFeeGate(provider MakerTakerFeeProvider, fallbackRoundTripBPS float64) FeeGate {
	return FeeGate{provider: provider, fallbackRoundTripBPS: fallbackRoundTripBPS}
}

// DefaultFallbackRoundTripBPS is the round-trip cost strategies assume when no
// live fee provider answers: Bitso retail taker, 65 bps on each of two legs.
//
// It is non-zero ON PURPOSE. The gate used to default to "off unless
// configured", and as a result it was silently inactive in every backtest --
// mean_reversion measured 700 trades / -7,291 MXN ungated against 4 trades /
// +64 MXN gated over the same data. An unconfigured strategy deciding whether
// to risk money should assume the real cost, not zero. To opt out, set
// fallback_round_trip_bps to 0 explicitly (with no provider injected).
const DefaultFallbackRoundTripBPS = 130.0

// HasRates reports whether the gate can produce usable rates at all. Callers
// use this to preserve exact legacy behaviour when fee gating is unconfigured.
func (g FeeGate) HasRates() bool {
	return g.provider != nil || g.fallbackRoundTripBPS > 0
}

// validRate rejects nonsense fee decimals. A rate at or above 1 would mean the
// fee consumes the entire notional and makes the break-even price infinite.
func validRate(r float64) bool {
	return !math.IsNaN(r) && !math.IsInf(r, 0) && r >= 0 && r < 1
}

// RoundTripRates resolves per-leg fee decimals.
//
// Preference order: the live provider, then the static fallback. If the
// provider answers with unusable values the fallback is still tried, so a
// malformed fee response degrades to an estimate rather than to nothing.
func (g FeeGate) RoundTripRates(ctx context.Context, book string) (buyRate, sellRate float64, source string) {
	if g.provider != nil {
		if maker, taker, ok := g.provider.MakerTakerRatesForBook(ctx, book); ok {
			if validRate(maker) && validRate(taker) {
				return maker, taker, FeeSourceProvider
			}
		}
	}

	if g.fallbackRoundTripBPS > 0 {
		perLeg := g.fallbackRoundTripBPS / 2 / 10000.0
		if validRate(perLeg) {
			return perLeg, perLeg, FeeSourceFallback
		}
	}

	return 0, 0, FeeSourceUnavailable
}

// NetPerBase returns net quote P&L per unit of base for a completed round trip.
//
// Long  : sell proceeds net of the sell fee, minus buy cost including the buy fee.
// Short : sell proceeds at entry net of the sell fee, minus buy-back cost at exit.
func (g FeeGate) NetPerBase(ctx context.Context, book string, dir PositionDirection, entryPrice, exitPrice float64) (net float64, source string) {
	buyRate, sellRate, src := g.RoundTripRates(ctx, book)
	if src == FeeSourceUnavailable || entryPrice <= 0 || exitPrice <= 0 {
		return 0, FeeSourceUnavailable
	}

	switch dir {
	case DirectionShort:
		net = entryPrice*(1-sellRate) - exitPrice*(1+buyRate)
	default:
		net = bitso.NetQuotePnLPerBase(entryPrice, exitPrice, buyRate, sellRate)
	}

	if math.IsNaN(net) || math.IsInf(net, 0) {
		return 0, FeeSourceUnavailable
	}
	return net, src
}

// EntryClearsCost reports whether an expected move covers round-trip cost plus
// the caller's required margin.
//
// FAIL-OPEN: when no usable fee rates exist this returns true. That is a
// deliberate choice — a missing or broken fee feed must not silently halt all
// trading, which would be a much louder and more damaging failure than trading
// on the previous (ungated) logic. The returned source is
// FeeSourceUnavailable so the caller can log and alert on it. Callers that
// prefer the opposite trade-off should use EntryClearsCostStrict.
func (g FeeGate) EntryClearsCost(ctx context.Context, book string, dir PositionDirection, entryPrice, expectedExitPrice, minNetPerBase float64) (ok bool, net float64, source string) {
	net, source = g.NetPerBase(ctx, book, dir, entryPrice, expectedExitPrice)
	if source == FeeSourceUnavailable {
		return true, 0, FeeSourceUnavailable
	}
	return net >= minNetPerBase, net, source
}

// EntryClearsCostStrict is EntryClearsCost but fails CLOSED when rates are
// unavailable: no fee data means no entry.
func (g FeeGate) EntryClearsCostStrict(ctx context.Context, book string, dir PositionDirection, entryPrice, expectedExitPrice, minNetPerBase float64) (ok bool, net float64, source string) {
	net, source = g.NetPerBase(ctx, book, dir, entryPrice, expectedExitPrice)
	if source == FeeSourceUnavailable {
		return false, 0, FeeSourceUnavailable
	}
	return net >= minNetPerBase, net, source
}

// ExitIsNetProfitable reports whether closing now clears round-trip cost.
//
// Fails open for the same reason as EntryClearsCost: if the fee feed is down,
// a position must still be closable by its normal exit path. Risk overrides
// (stop-loss, max-hold) must be evaluated by the caller BEFORE consulting this,
// since they are permitted to realize a loss by design.
func (g FeeGate) ExitIsNetProfitable(ctx context.Context, book string, dir PositionDirection, entryPrice, exitPrice float64) (ok bool, net float64, source string) {
	net, source = g.NetPerBase(ctx, book, dir, entryPrice, exitPrice)
	if source == FeeSourceUnavailable {
		return true, 0, FeeSourceUnavailable
	}
	return net > 0, net, source
}

// MinProfitableExit returns the lowest LONG exit price that breaks even after
// both legs' fees. Returns the entry price unchanged when rates are
// unavailable, so callers degrade to "any profit is fine".
func (g FeeGate) MinProfitableExit(ctx context.Context, book string, entryPrice float64) (threshold float64, source string) {
	buyRate, sellRate, src := g.RoundTripRates(ctx, book)
	if src == FeeSourceUnavailable || entryPrice <= 0 {
		return entryPrice, FeeSourceUnavailable
	}
	be := bitso.MinExitPriceAfterRoundTrip(entryPrice, buyRate, sellRate)
	if math.IsNaN(be) || math.IsInf(be, 0) || be <= 0 {
		return entryPrice, FeeSourceUnavailable
	}
	return be, src
}
