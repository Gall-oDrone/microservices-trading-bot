package bitso

// Trade represents a recent trade from the specified book
type Trade struct {
	Book      Book      `json:"book"`
	CreatedAt Time      `json:"created_at"`
	Amount    Monetary  `json:"amount"`
	MakerSide OrderSide `json:"maker_side"`
	Price     Monetary  `json:"price"`
	TID       TID       `json:"tid"`
}

// UserTrade represents a trade made by the user
type UserTrade struct {
	Book          Book      `json:"book"`
	Major         Monetary  `json:"major"`
	MajorCurrency Currency  `json:"major_currency"`
	CreatedAt     Time      `json:"created_at"`
	Minor         Monetary  `json:"minor"`
	MinorCurrency Currency  `json:"minor_currency"`
	FeesAmount    Monetary  `json:"fees_amount"`
	FeesCurrency  Currency  `json:"fees_currency"`
	Price         Monetary  `json:"price"`
	TID           TID       `json:"tid"`
	OID           string    `json:"oid"`
	OriginOID     string    `json:"origin_id"`
	Side          OrderSide `json:"side"`
	MakerSide     OrderSide `json:"maker_side"`
}
