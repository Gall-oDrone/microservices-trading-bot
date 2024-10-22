package bitso

// ExchangeOrderBook represents order placement limits on books.
type ExchangeOrderBook struct {

	// Order book symbol
	Book Book `json:"book"`

	// Minimum amount of major when placing orders
	MinimumAmount Monetary `json:"minimum_amount"`

	// Maximum amount of major when placing orders
	MaximumAmount Monetary `json:"maximum_amount"`

	// Minimum price when placing orders
	MinimumPrice Monetary `json:"minimum_price"`
	// Maximum price when placing orders
	MaximumPrice Monetary `json:"maximum_price"`

	// Minimum value amount (amount*price) when placing orders
	MinimumValue Monetary `json:"minimum_value"`

	// Maximum value amount (amount*price) when placing orders
	MaximumValue Monetary `json:"maximum_value"`

	// The minimum price difference that must exist at all times between consecutive bid and offer prices in the order book. In other words, it is the minimum increment in which prices can change in the order book.
	TickSize Monteray `json:"tick_size"`
}
