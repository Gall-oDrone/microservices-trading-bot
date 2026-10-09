package execcost

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func lv(pq ...float64) []Level {
	var out []Level
	for i := 0; i+1 < len(pq); i += 2 {
		out = append(out, Level{Price: pq[i], Amount: pq[i+1]})
	}
	return out
}

func TestNewSample(t *testing.T) {
	at := time.Date(2026, 10, 9, 21, 0, 0, 0, time.UTC)
	s, ok := NewSample("btc_mxn", "test", at, lv(99.99, 0.5, 99.9, 1), lv(100.01, 0.5, 100.1, 1), 10)
	if !ok {
		t.Fatal("not ok")
	}
	if s.At != "2026-10-09T21:00:00Z" || s.Mid != 100 || s.BidDepthBTC != 1.5 || s.AskDepthBTC != 1.5 {
		t.Fatalf("sample %+v", s)
	}
	if d := s.SpreadBps - 2; d > 1e-9 || d < -1e-9 {
		t.Fatalf("spread %v", s.SpreadBps)
	}
	if s.WalkCapacityBTC < 0.5 || s.WalkCapacityBTC > 1.5 || len(s.Sizes) != len(SampleSizes) {
		t.Fatalf("capacity %v sizes %d", s.WalkCapacityBTC, len(s.Sizes))
	}
	for _, c := range s.Sizes { // 1.5 BTC per side covers every sample size up to 1 BTC
		if !c.Complete || c.BuyBps <= 0 || c.SellBps <= 0 {
			t.Fatalf("size %+v", c)
		}
	}
	if _, ok := NewSample("b", "t", at, lv(100, 1), lv(100, 1), 10); ok {
		t.Fatal("a crossed/locked book must be rejected")
	}
	if _, ok := NewSample("b", "t", at, nil, lv(100, 1), 10); ok {
		t.Fatal("an empty side must be rejected")
	}
}

func TestNewSample_KeepsOnlyTheTopLevels(t *testing.T) {
	var bids, asks []Level
	for i := 0; i < SampleLevels+5; i++ {
		bids = append(bids, Level{Price: 100 - float64(i), Amount: 1})
		asks = append(asks, Level{Price: 101 + float64(i), Amount: 1})
	}
	s, _ := NewSample("b", "t", time.Now(), bids, asks, 10)
	if s.BidDepthBTC != SampleLevels || s.AskDepthBTC != SampleLevels {
		t.Fatalf("depth %v/%v", s.BidDepthBTC, s.AskDepthBTC)
	}
}

func TestSamplesFileRoundTripAndSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "btc_mxn.jsonl")
	if ss, bad, err := ReadSamples(path); err != nil || len(ss) != 0 || bad != 0 {
		t.Fatalf("missing file: %v %d %v", ss, bad, err)
	}
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 11; i++ {
		spread := float64(i) // 0..10 bps
		s := Sample{At: base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339), Book: "btc_mxn", Mid: 100,
			SpreadBps: spread, BidDepthBTC: 1, AskDepthBTC: 2, WalkCapacityBTC: float64(i) / 10, BudgetBps: 10,
			Sizes: []SizeCost{{QtyBTC: 0.1, BuyBps: spread, SellBps: spread / 2, Complete: true}, {QtyBTC: 5, Complete: i%2 == 0, BuyBps: 50}}}
		if err := AppendSample(path, s); err != nil {
			t.Fatal(err)
		}
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("{torn\n")
	f.Close()

	ss, bad, err := ReadSamples(path)
	if err != nil || len(ss) != 11 || bad != 1 {
		t.Fatalf("read: %d samples, %d bad, %v", len(ss), bad, err)
	}
	sum := SummarizeSamples(ss, time.Time{})
	if sum.Samples != 11 || sum.From != "2026-10-01T00:00:00Z" || sum.To != "2026-10-01T10:00:00Z" || sum.BudgetBps != 10 {
		t.Fatalf("summary %+v", sum)
	}
	if sum.SpreadBps != (Pct{P10: 1, P50: 5, P90: 9}) || sum.AskDepthBTC.P50 != 2 {
		t.Fatalf("spread %+v", sum.SpreadBps)
	}
	if len(sum.Sizes) != 2 || sum.Sizes[0].QtyBTC != 0.1 || sum.Sizes[0].WorseSideBps.P50 != 5 || sum.Sizes[0].CoveredShare != 1 {
		t.Fatalf("sizes %+v", sum.Sizes)
	}
	if c := sum.Sizes[1].CoveredShare; c < 0.54 || c > 0.55 { // 6 of 11
		t.Fatalf("covered %v", c)
	}
	if late := SummarizeSamples(ss, base.Add(5*time.Hour)); late.Samples != 6 || late.From != "2026-10-01T05:00:00Z" {
		t.Fatalf("since: %+v", late)
	}
}
