package models

// FillObservation captures the realized maker/taker role and fee details extracted from a
// Bitso UserTrade. It is computed in the user-trades poller (where MakerSide + FeesAmount +
// FeesCurrency are still available) and persisted on order.Metadata so that the next emitted
// OrderFillEvent carries the realized fee and liquidity. Documented in docs/strategy-fee-accuracy/.
type FillObservation struct {
	// Liquidity is the role our order played in the fill: "maker" or "taker". Empty when
	// Bitso did not return enough information to determine it (callers should fall back to
	// the strategy's configured assumption).
	Liquidity string
	// FeeRate is the decimal fraction of notional Bitso charged (e.g. 0.00741 = 0.741%).
	FeeRate float64
	// FeeAmount is the absolute fee Bitso billed in FeeCurrency.
	FeeAmount float64
	// FeeCurrency is the currency Bitso billed the fee in (base for buys, quote for sells).
	FeeCurrency string
}
