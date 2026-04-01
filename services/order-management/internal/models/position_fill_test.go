package models

import (
	"math"
	"testing"
)

func TestPosition_ApplyFill_BuyThenSellRealized(t *testing.T) {
	p := NewPosition("btc_mxn", "")
	buy := &Order{Side: "buy", Book: "btc_mxn", Amount: 1}
	if rd := p.ApplyFill(buy, 0.01, 1_000_000); math.Abs(rd) > 1e-9 {
		t.Fatalf("buy realized want 0, got %v", rd)
	}
	if p.Size != 0.01 || p.Side != "long" {
		t.Fatalf("after buy: size=%v side=%s", p.Size, p.Side)
	}
	sell := &Order{Side: "sell", Book: "btc_mxn", Amount: 0.01}
	rd := p.ApplyFill(sell, 0.01, 1_100_000)
	want := (1_100_000 - 1_000_000) * 0.01
	if math.Abs(rd-want) > 1e-6 {
		t.Fatalf("sell realized: want %v got %v", want, rd)
	}
	if p.Size != 0 {
		t.Fatalf("flat size want 0 got %v", p.Size)
	}
}

func TestPosition_ApplyFill_SellOpenShortNoRealized(t *testing.T) {
	p := NewPosition("btc_mxn", "")
	sell := &Order{Side: "sell", Book: "btc_mxn", Amount: 0.01}
	rd := p.ApplyFill(sell, 0.01, 1_000_000)
	if math.Abs(rd) > 1e-9 {
		t.Fatalf("naked short realized want 0, got %v", rd)
	}
	if p.Side != "short" || math.Abs(p.Size-0.01) > 1e-12 {
		t.Fatalf("short open: side=%s size=%v", p.Side, p.Size)
	}
}

func TestPosition_ApplyFill_BuyCoversShort(t *testing.T) {
	p := NewPosition("btc_mxn", "")
	_ = p.ApplyFill(&Order{Side: "sell", Book: "btc_mxn", Amount: 0.01}, 0.01, 1_000_000)
	buy := &Order{Side: "buy", Book: "btc_mxn", Amount: 0.01}
	rd := p.ApplyFill(buy, 0.01, 950_000)
	want := (1_000_000 - 950_000) * 0.01
	if math.Abs(rd-want) > 1e-6 {
		t.Fatalf("cover short realized: want %v got %v", want, rd)
	}
}
