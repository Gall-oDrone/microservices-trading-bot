package models

import (
	"testing"
)

func TestAccumulateFill_DoesNotChangeStatus(t *testing.T) {
	o := NewOrder("oid-1", "btc_mxn", "buy", "limit", "basic", 1_000_000, 0.01)
	o.UpdateStatus(OrderStatusSubmitted)
	o.AccumulateFill(0.01, 1_000_000)
	if o.FilledAmount != 0.01 || o.RemainingAmount > 1e-9 {
		t.Fatalf("fill amounts: filled=%f remaining=%f", o.FilledAmount, o.RemainingAmount)
	}
	if o.Status != OrderStatusSubmitted {
		t.Fatalf("status must stay exchange-driven; got %s", o.Status)
	}
}

func TestRecordFill_UpdatesStatusFromAmounts(t *testing.T) {
	o := NewOrder("oid-2", "btc_mxn", "buy", "limit", "basic", 1_000_000, 0.01)
	o.UpdateStatus(OrderStatusSubmitted)
	o.RecordFill(0.01, 1_000_000)
	if o.Status != OrderStatusFilled {
		t.Fatalf("RecordFill should set filled when complete; got %s", o.Status)
	}
}
