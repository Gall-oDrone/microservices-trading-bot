package table

import (
	"bytes"
	"testing"
	"text/tabwriter"
)

func TestGetTableOptimalAskPriceSummary(t *testing.T) {
	// Create a buffer to capture the output
	var buf bytes.Buffer

	// Initialize the TableData with the buffer
	td := &TableData{
		tw: tabwriter.NewWriter(&buf, 4, 4, 3, ' ', 0),
	}

	// Test data
	amount := 1.000000
	bid_rate := 1.000000
	ask_value := 1.000000
	taker_fee := 1.000000
	maker_fee := 1.000000
	total := 1.000000
	price_percentage_change := 1.000000

	// Call the method
	td.GetTableOptimalPriceSummary("sell", amount, bid_rate, ask_value, taker_fee, maker_fee, total, price_percentage_change)

	// Flush the writer to ensure all data is written to the buffer
	td.tw.Flush()

	// Define the expected output
	expectedOutput := "AMOUNT(BTC)\tBID_RATE\tASK_VALUE\tTAKER_FEE\tMAKER_FEE\tTOTAL\tBID_RATE_CHANGE(%)\n1.000000\t1.000000\t1.000000\t1.000000\t1.000000\t1.000000\t1.000000\n"

	// Compare the buffer output to the expected output
	if buf.String() != expectedOutput {
		t.Errorf("Output was incorrect, got:\n%s\nWant: \n%s", buf.String(), expectedOutput)
	}
}
func TestGetTableOptimalBidPriceSummary(t *testing.T) {
	// Create a buffer to capture the output
	var buf bytes.Buffer

	// Initialize the TableData with the buffer
	td := &TableData{
		tw: tabwriter.NewWriter(&buf, 4, 4, 3, ' ', 0),
	}

	// Test data
	amount := 1.000000
	bid_rate := 1.000000
	ask_value := 1.000000
	taker_fee := 1.000000
	maker_fee := 1.000000
	total := 1.000000
	price_percentage_change := 1.000000

	// Call the method
	td.GetTableOptimalPriceSummary("buy", amount, bid_rate, ask_value, taker_fee, maker_fee, total, price_percentage_change)

	// Flush the writer to ensure all data is written to the buffer
	td.tw.Flush()

	// Define the expected output
	expectedOutput := "AMOUNT(MXN)\tASK_RATE\tBID_VALUE\tTAKER_FEE\tMAKER_FEE\tTOTAL\tASK_RATE_CHANGE(%)\n1.000000\t1.000000\t1.000000\t1.000000\t1.000000\t1.000000\t1.000000\n"

	// Compare the buffer output to the expected output
	if buf.String() != expectedOutput {
		t.Errorf("Output was incorrect, got:\n%s\nWant: \n%s", buf.String(), expectedOutput)
	}
}
