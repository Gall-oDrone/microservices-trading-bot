package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"bitso-trading-platform/shared/pkg/execcost"
	"bitso-trading-platform/strategy-executor/internal/execsim"
)

func TestParseConfig(t *testing.T) {
	c, err := parseConfig("06:01, 06:15", "15m,1h", "0.001,0.1", "btc_mxn=1.5")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.starts) != 2 || c.starts[1] != 6*time.Hour+15*time.Minute || c.windows[1] != time.Hour || c.sizes[1] != 0.1 || c.halfSpread["btc_mxn"] != 1.5 {
		t.Fatalf("config %+v", c)
	}
	for _, bad := range [][4]string{{"6h", "1h", "1", "a=1"}, {"06:15", "-1h", "1", "a=1"}, {"06:15", "1h", "0", "a=1"}, {"06:15", "1h", "1", "a"}, {"", "1h", "1", ""}} {
		if _, err := parseConfig(bad[0], bad[1], bad[2], bad[3]); err == nil {
			t.Fatalf("want an error for %v", bad)
		}
	}
}

func TestSchedules_MarksTodayAndBuildsVariants(t *testing.T) {
	s := schedules([]time.Duration{30 * time.Minute, time.Hour})
	var names []string
	today := 0
	for _, x := range s {
		names = append(names, x.Name)
		if x.Today {
			today++
		}
	}
	want := "market,maker 30m,maker 1h,maker 30m repriced 5m,maker 1h repriced 5m,TWAP 4×15m"
	if strings.Join(names, ",") != want || today != 1 {
		t.Fatalf("schedules %v (today %d)", names, today)
	}
}

func TestMakerSide(t *testing.T) {
	if makerSide("buy") != 1 || makerSide(" Sell ") != -1 || makerSide("") != 0 {
		t.Fatal("maker side mapping")
	}
}

// synthTrades prints a bid at 100 and an ask at 100.02 every minute for
// three Bitso days, with a sell through the bid every 20 minutes.
func synthTrades() []execsim.Trade {
	var out []execsim.Trade
	start := time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC) // Mexico City midnight
	for m := 0; m < 3*24*60; m++ {
		at := start.Add(time.Duration(m) * time.Minute)
		out = append(out, execsim.Trade{At: at, Price: 100, Amount: 0.5, MakerSide: 1},
			execsim.Trade{At: at.Add(20 * time.Second), Price: 100.02, Amount: 0.5, MakerSide: -1})
		if m%20 == 10 {
			out = append(out, execsim.Trade{At: at.Add(40 * time.Second), Price: 99.99, Amount: 0.5, MakerSide: 1})
		}
	}
	return out
}

func TestStudy_SyntheticBook(t *testing.T) {
	c, err := parseConfig("06:15", "60m", "0.001", "btc_mxn=1")
	if err != nil {
		t.Fatal(err)
	}
	c.maxGap, c.quoteMaxAge, c.impactY, c.reportSize = 30*time.Minute, 10*time.Minute, 1, 0.001
	c.base, c.alt = "maker 1h", "maker 1h repriced 5m"
	br := study("btc_mxn", synthTrades(), c, schedules(c.windows))
	if br.DaysUsed != 3 || br.DaysSkipped != 0 || br.MakerFeeBps != 60 || br.TakerFeeBps != 78 {
		t.Fatalf("report %+v", br)
	}
	mk, ok := find(br.Rows, "market", "through", 0.001, "06:15")
	if !ok || mk.Legs != 6 || mk.MakerShare != 0 {
		t.Fatalf("market row %+v", mk)
	}
	// A buy takes the 100.02 ask from a 100.02 arrival (the last print); a sell
	// hits the 100 bid, 2 bps worse. Both pay the 78 bps taker fee: mean 79.
	if mk.MeanBps < 78+0.9 || mk.MeanBps > 78+1.1 {
		t.Fatalf("market mean %.3f", mk.MeanBps)
	}
	// Through: the buy fills on the 99.99 print; nothing trades above the
	// 100.02 ask, so the sell falls back to market. Touch fills both.
	thr, _ := find(br.Rows, "maker 1h", "through", 0.001, "06:15")
	if thr.FullMakerRate != 0.5 || thr.MakerShare != 0.5 {
		t.Fatalf("through row %+v", thr)
	}
	tch, _ := find(br.Rows, "maker 1h", "touch", 0.001, "06:15")
	if tch.FullMakerRate != 1 || tch.MeanBps > 60.01 {
		t.Fatalf("touch row %+v", tch)
	}
	// The default comparison (today against the 5-minute re-peg) exists per model.
	if len(br.Comparisons) != 2 || br.Comparisons[0].N != 6 || br.Comparisons[0].Base != "maker 1h" {
		t.Fatalf("comparisons %+v", br.Comparisons)
	}
	var buf bytes.Buffer
	writeMarkdown(&buf, Report{Schedules: schedules(c.windows), Books: []BookReport{br}}, c)
	for _, want := range []string{"## btc_mxn", "maker 1h (today)", "### By size, start 06:15 UTC", "### Paired: maker 1h repriced 5m against maker 1h"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("markdown lacks %q:\n%s", want, buf.String())
		}
	}
}

func TestFilterSchedules(t *testing.T) {
	all := schedules([]time.Duration{time.Hour})
	if got := filterSchedules(all, nil); len(got) != len(all) {
		t.Fatalf("no filter: %d", len(got))
	}
	got := filterSchedules(all, map[string]bool{"maker 1h": true, "maker 1h repriced 5m": true})
	if len(got) != 2 || got[0].Name != "maker 1h" || got[1].Reprice != 5*time.Minute {
		t.Fatalf("filtered %+v", got)
	}
}

func TestSampledHalfSpread(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		s := execcost.Sample{At: base.Add(time.Duration(i) * time.Hour).Format(time.RFC3339), Book: "btc_mxn", Mid: 100, SpreadBps: 3}
		if err := execcost.AppendSample(execcost.SamplePath(dir, "btc_mxn"), s); err != nil {
			t.Fatal(err)
		}
	}
	hs, n := sampledHalfSpread(dir, "btc_mxn", base, base.Add(24*time.Hour))
	if n != 24 || hs != 1.5 {
		t.Fatalf("half-spread %v from %d samples", hs, n)
	}
	if _, n := sampledHalfSpread(dir, "btc_usd", base, base.Add(time.Hour)); n != 0 {
		t.Fatalf("missing file: %d", n)
	}
}
