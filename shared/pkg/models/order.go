package models

// TODO: Move shared domain models from existing services
// This will contain shared data structures

type Order struct {
	ID     string `json:"id"`
	Symbol string `json:"symbol"`
	Side   string `json:"side"`
	Amount float64 `json:"amount"`
	Price  float64 `json:"price"`
	// TODO: Add more fields
}
