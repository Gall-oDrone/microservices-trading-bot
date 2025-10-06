package models

type TradeSignalEvent struct {
	EventID   string                 `json:"event_id"`
	Timestamp int64                  `json:"timestamp"`
	Book      string                 `json:"book"`
	Strategy  string                 `json:"strategy"`
	Signal    string                 `json:"signal"` // BUY, SELL, HOLD
	Price     float64                `json:"price"`
	Amount    float64                `json:"amount"`
	Metadata  map[string]interface{} `json:"metadata"`
}

type OrderEvent struct {
	EventID   string  `json:"event_id"`
	OrderID   string  `json:"order_id"`
	Timestamp int64   `json:"timestamp"`
	Book      string  `json:"book"`
	Side      string  `json:"side"`
	Type      string  `json:"type"`
	Status    string  `json:"status"`
	Price     float64 `json:"price"`
	Amount    float64 `json:"amount"`
}
