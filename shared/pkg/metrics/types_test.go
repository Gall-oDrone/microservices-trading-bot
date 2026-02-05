package metrics

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestMonetaryAmount_Add(t *testing.T) {
	a := NewMonetaryAmount(decimal.NewFromFloat(100.5), "MXN")
	b := NewMonetaryAmount(decimal.NewFromFloat(50.25), "MXN")
	sum := a.Add(b)
	if sum.Amount().Cmp(decimal.NewFromFloat(150.75)) != 0 {
		t.Errorf("Add: want 150.75, got %s", sum.Amount().String())
	}
	if sum.Currency() != "MXN" {
		t.Errorf("Currency: want MXN, got %s", sum.Currency())
	}
}

func TestMonetaryAmount_Add_PanicsDifferentCurrency(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when adding different currencies")
		}
	}()
	a := NewMonetaryAmount(decimal.NewFromFloat(100), "MXN")
	b := NewMonetaryAmount(decimal.NewFromFloat(50), "USD")
	a.Add(b)
}

func TestPnLSnapshot_TotalPnL(t *testing.T) {
	snap := PnLSnapshot{
		RealizedPnL:   NewMonetaryAmount(decimal.NewFromFloat(10), "MXN"),
		UnrealizedPnL: NewMonetaryAmount(decimal.NewFromFloat(-2.5), "MXN"),
		Currency:      "MXN",
	}
	total := snap.TotalPnL()
	if total.Amount().Cmp(decimal.NewFromFloat(7.5)) != 0 {
		t.Errorf("TotalPnL: want 7.5, got %s", total.Amount().String())
	}
}

func TestNewMonetaryAmountFromFloat(t *testing.T) {
	m := NewMonetaryAmountFromFloat(99.99, "USD")
	if m.Currency() != "USD" {
		t.Errorf("Currency: want USD, got %s", m.Currency())
	}
	if m.Float64() != 99.99 {
		t.Errorf("Float64: want 99.99, got %v", m.Float64())
	}
}
