package execcost

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// Book samples (plan §6.4.13): one snapshot of the visible book is a single
// draw. The book sampler (strategy-executor cmd/book-sampler, hourly) appends
// a Sample per book to <dir>/<book>.jsonl, and SummarizeSamples turns the
// file into percentiles, so spread, depth and capacity become distributions.

// SampleLevels is how many levels per side a sample keeps: the same 20 the
// public websocket sends, so samples and the live capacity page agree.
const SampleLevels = 20

// SampleSizes are the order sizes (BTC) whose walk cost every sample records.
var SampleSizes = []float64{0.001, 0.01, 0.1, 0.5, 1}

// SizeCost is one size's walk cost in a sample.
type SizeCost struct {
	QtyBTC   float64 `json:"qty_btc"`
	BuyBps   float64 `json:"buy_bps"`
	SellBps  float64 `json:"sell_bps"`
	Complete bool    `json:"complete"` // the visible depth covered the size on both sides
}

// Sample is one book snapshot.
type Sample struct {
	At              string     `json:"at"` // RFC 3339, UTC
	Book            string     `json:"book"`
	Source          string     `json:"source"` // e.g. "bitso-rest"
	Mid             float64    `json:"mid"`
	SpreadBps       float64    `json:"spread_bps"`
	BidDepthBTC     float64    `json:"bid_depth_btc"` // top SampleLevels
	AskDepthBTC     float64    `json:"ask_depth_btc"`
	WalkCapacityBTC float64    `json:"walk_capacity_btc"` // largest size within BudgetBps on both sides
	BudgetBps       float64    `json:"budget_bps"`
	Sizes           []SizeCost `json:"sizes"`
}

// NewSample measures one snapshot. bids and asks are best first; only the
// first SampleLevels of each are used. ok is false for a crossed or empty
// book.
func NewSample(book, source string, at time.Time, bids, asks []Level, budgetBps float64) (Sample, bool) {
	if len(bids) == 0 || len(asks) == 0 || bids[0].Price <= 0 || asks[0].Price <= bids[0].Price {
		return Sample{}, false
	}
	bids, asks = bids[:min(len(bids), SampleLevels)], asks[:min(len(asks), SampleLevels)]
	mid := (bids[0].Price + asks[0].Price) / 2
	s := Sample{At: at.UTC().Format(time.RFC3339), Book: book, Source: source, Mid: mid,
		SpreadBps:   (asks[0].Price - bids[0].Price) / mid * 1e4,
		BidDepthBTC: Depth(bids), AskDepthBTC: Depth(asks), BudgetBps: budgetBps,
		WalkCapacityBTC: MaxQtyWithin(bids, asks, mid, budgetBps), Sizes: []SizeCost{}}
	for _, q := range SampleSizes {
		b, sl := Walk(asks, q, mid, true), Walk(bids, q, mid, false)
		s.Sizes = append(s.Sizes, SizeCost{QtyBTC: q, BuyBps: b.CostBps, SellBps: sl.CostBps, Complete: b.Complete && sl.Complete})
	}
	return s, true
}

// SamplePath is where book's samples live under dir.
func SamplePath(dir, book string) string {
	return strings.TrimRight(dir, "/") + "/" + book + ".jsonl"
}

// AppendSample appends s as one JSON line to path (created if missing).
func AppendSample(path string, s Sample) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadSamples reads a samples file. A missing file is no samples, not an
// error; a malformed line is skipped and counted (a crash can leave a torn
// last line).
func ReadSamples(path string) (samples []Sample, bad int, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s Sample
		if json.Unmarshal([]byte(line), &s) != nil || s.Mid <= 0 {
			bad++
			continue
		}
		samples = append(samples, s)
	}
	if err := sc.Err(); err != nil {
		return samples, bad, fmt.Errorf("read %s: %w", path, err)
	}
	return samples, bad, nil
}

// Pct is a distribution summary.
type Pct struct {
	P10 float64 `json:"p10"`
	P50 float64 `json:"p50"`
	P90 float64 `json:"p90"`
}

func pct(xs []float64) Pct {
	if len(xs) == 0 {
		return Pct{}
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	q := func(p float64) float64 {
		pos := p * float64(len(s)-1)
		lo, hi := int(math.Floor(pos)), int(math.Ceil(pos))
		return s[lo] + (s[hi]-s[lo])*(pos-float64(lo))
	}
	return Pct{P10: q(0.1), P50: q(0.5), P90: q(0.9)}
}

// SizeDist is one size's walk cost distribution (the worse side of each
// sample), over samples whose book covered the size.
type SizeDist struct {
	QtyBTC       float64 `json:"qty_btc"`
	WorseSideBps Pct     `json:"worse_side_bps"`
	CoveredShare float64 `json:"covered_share"` // samples whose visible depth covered the size
}

// SampleSummary is the distribution of a book's samples.
type SampleSummary struct {
	Samples         int        `json:"samples"`
	Bad             int        `json:"bad_lines"`
	From            string     `json:"from"`
	To              string     `json:"to"`
	SpreadBps       Pct        `json:"spread_bps"`
	BidDepthBTC     Pct        `json:"bid_depth_btc"`
	AskDepthBTC     Pct        `json:"ask_depth_btc"`
	WalkCapacityBTC Pct        `json:"walk_capacity_btc"`
	BudgetBps       float64    `json:"budget_bps"`
	Sizes           []SizeDist `json:"sizes"`
}

// SummarizeSamples summarises samples taken at or after since (zero: all).
func SummarizeSamples(samples []Sample, since time.Time) SampleSummary {
	var sum SampleSummary
	var spread, bid, ask, capa []float64
	type acc struct {
		costs []float64
		n     int
	}
	sizes := map[float64]*acc{}
	var order []float64
	for _, s := range samples {
		at, err := time.Parse(time.RFC3339, s.At)
		if err != nil || (!since.IsZero() && at.Before(since)) {
			continue
		}
		if sum.Samples == 0 || s.At < sum.From {
			sum.From = s.At
		}
		if s.At > sum.To {
			sum.To = s.At
		}
		sum.Samples++
		sum.BudgetBps = s.BudgetBps
		spread, bid, ask, capa = append(spread, s.SpreadBps), append(bid, s.BidDepthBTC), append(ask, s.AskDepthBTC), append(capa, s.WalkCapacityBTC)
		for _, c := range s.Sizes {
			a := sizes[c.QtyBTC]
			if a == nil {
				a = &acc{}
				sizes[c.QtyBTC] = a
				order = append(order, c.QtyBTC)
			}
			a.n++
			if c.Complete {
				a.costs = append(a.costs, math.Max(c.BuyBps, c.SellBps))
			}
		}
	}
	sum.SpreadBps, sum.BidDepthBTC, sum.AskDepthBTC, sum.WalkCapacityBTC = pct(spread), pct(bid), pct(ask), pct(capa)
	sort.Float64s(order)
	sum.Sizes = []SizeDist{}
	for _, q := range order {
		a := sizes[q]
		sum.Sizes = append(sum.Sizes, SizeDist{QtyBTC: q, WorseSideBps: pct(a.costs), CoveredShare: float64(len(a.costs)) / float64(a.n)})
	}
	return sum
}
