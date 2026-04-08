package strategies

import "context"

// MakerTakerFeeProvider returns maker and taker fee rates as decimal fractions of notional
// (e.g. 0.005 for 0.5%), typically from Bitso GET /fees. ok is false if unavailable.
type MakerTakerFeeProvider interface {
	MakerTakerRatesForBook(ctx context.Context, book string) (maker, taker float64, ok bool)
}

// feeRatesInjectable is implemented by strategies that consume MakerTakerFeeProvider (e.g. limit_profit).
type feeRatesInjectable interface {
	SetFeeRatesProvider(MakerTakerFeeProvider)
}
