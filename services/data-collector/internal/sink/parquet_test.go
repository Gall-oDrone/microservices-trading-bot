package sink_test

import (
	"testing"
	"time"

	"bitso-trading-platform/data-collector/internal/models"
	"bitso-trading-platform/data-collector/internal/sink"
)

func TestParquetRoundTrip(t *testing.T) {
	base := time.Date(2026, 8, 19, 15, 30, 0, 123e6, time.UTC)
	in := []models.Trade{
		{Book: "btc_mxn", TID: 101, Price: 1234567.89, Amount: 0.0012, MakerSide: "buy",
			ExchangeTS: base, ReceivedAt: base.Add(250 * time.Millisecond)},
		{Book: "btc_mxn", TID: 102, Price: 1234570, Amount: 0.5, MakerSide: "sell",
			ExchangeTS: base.Add(time.Second), ReceivedAt: base.Add(1300 * time.Millisecond)},
	}

	data, err := sink.EncodeParquet(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := sink.DecodeParquet(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("rows=%d want %d", len(out), len(in))
	}
	for i := range in {
		a, b := out[i], in[i]
		if a.Book != b.Book || a.TID != b.TID || a.Price != b.Price || a.Amount != b.Amount ||
			a.MakerSide != b.MakerSide || !a.ExchangeTS.Equal(b.ExchangeTS) || !a.ReceivedAt.Equal(b.ReceivedAt) {
			t.Fatalf("row %d: got %+v want %+v", i, a, b)
		}
	}
}

func TestEncodeParquetRejectsEmptyBatch(t *testing.T) {
	if _, err := sink.EncodeParquet(nil); err == nil {
		t.Fatal("expected error for empty batch")
	}
}
