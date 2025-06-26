package tests

import (
	"fmt"
	"testing"

	"bitso_trading_bot/internal/utils"
)

func TestSimpleColorizeTabularData(t *testing.T) {
	table := `BOOK	SIDE	ENTRY	EXIT	AMOUNT	PnL	PnL%	FEES	NET_PNL
` +
		`btc_usd	buy	50000.0000	51000.0000	0.1000	100.0000	2.00%	5.0000	95.0000
` +
		`btc_usd	sell	51000.0000	50000.0000	0.1000	-100.0000	-2.00%	5.0000	-105.0000`

	colored := utils.ColorizeTabularData(table)
	fmt.Println("\nColored Table Output:")
	fmt.Println(colored)

	if !containsGreen(colored) {
		t.Error("Expected green color code for positive PnL values")
	}
	if !containsRed(colored) {
		t.Error("Expected red color code for negative PnL values")
	}
}

func containsGreen(s string) bool {
	return containsANSI(s, "32m")
}

func containsRed(s string) bool {
	return containsANSI(s, "31m")
}

func containsANSI(s, code string) bool {
	return len(s) > 0 && (contains(s, "\033["+code) || contains(s, "\u001b["+code))
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (stringIndex(s, substr) >= 0)
}

func stringIndex(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
