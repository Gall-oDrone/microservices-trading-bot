package bitso

import (
	"fmt"
	"strconv"

	"github.com/shopspring/decimal"
)

// Monetary represents a monetary value
type Monetary string

// Float64 returns the monetary value as a float64
func (m *Monetary) Float64() float64 {
	v, _ := strconv.ParseFloat(string(*m), 64)
	return v
}

func (m *Monetary) Decimal() (decimal.Decimal, error) {
	return decimal.NewFromString(string(*m))
}

// ToMonetary converts a float64 value into Monetary
func ToMonetary(in float64) Monetary {
	return Monetary(fmt.Sprintf("%f", in))

}
func ToMonetaryWithP(in float64) Monetary {
	precision := 20 // Set the desired precision
	// Use strconv.FormatFloat to convert the float64 to a string with the desired precision
	return Monetary(strconv.FormatFloat(in, 'f', precision, 64))
}
