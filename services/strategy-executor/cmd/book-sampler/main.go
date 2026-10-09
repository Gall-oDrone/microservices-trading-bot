// Command book-sampler records one snapshot of each book's public order book
// (Bitso production REST, no credentials) as a line of
// <dir>/<book>.jsonl: spread, visible depth, walk cost at fixed sizes and
// the walk capacity within the slippage budget (shared/pkg/execcost). Run
// hourly (scripts/ops-run.sh sampler); ui-api's capacity endpoint turns the
// files into percentiles (plan §6.4.13).
//
//	go run ./cmd/book-sampler -dir ./daily-executor-data/book-samples
//
// Read-only: it places no orders and needs no keys. Exit status: 0 when
// every book was sampled, 1 when any failed (the others are still written),
// 2 on bad usage.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"bitso-trading-platform/shared/pkg/execcost"
)

func main() {
	books := flag.String("books", "btc_mxn,btc_usd", "books to sample")
	dir := flag.String("dir", "./daily-executor-data/book-samples", "samples directory (<dir>/<book>.jsonl)")
	base := flag.String("base", "https://api.bitso.com", "Bitso public API base URL")
	budget := flag.Float64("budget-bps", 10, "slippage budget for the walk capacity (the pre-registrations' 10 bps per leg)")
	timeout := flag.Duration("timeout", 15*time.Second, "HTTP timeout per book")
	flag.Parse()
	if flag.NArg() > 0 || strings.TrimSpace(*books) == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "book-sampler:", err)
		os.Exit(1)
	}
	c := &http.Client{Timeout: *timeout}
	failed := 0
	for _, b := range strings.Split(*books, ",") {
		b = strings.TrimSpace(b)
		s, err := sample(c, *base, b, *budget, time.Now())
		if err == nil {
			err = execcost.AppendSample(execcost.SamplePath(*dir, b), s)
		}
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "book-sampler: %s: %v\n", b, err)
			continue
		}
		fmt.Printf("%s %s mid %.2f spread %.2f bps depth %.3f/%.3f BTC capacity@%.0fbps %.4f BTC\n",
			s.At, b, s.Mid, s.SpreadBps, s.BidDepthBTC, s.AskDepthBTC, s.BudgetBps, s.WalkCapacityBTC)
	}
	if failed > 0 {
		os.Exit(1)
	}
}

type restLevel struct {
	Price  string `json:"price"`
	Amount string `json:"amount"`
}

type restBook struct {
	Success bool `json:"success"`
	Payload struct {
		Bids []restLevel `json:"bids"`
		Asks []restLevel `json:"asks"`
	} `json:"payload"`
}

func sample(c *http.Client, base, book string, budget float64, now time.Time) (execcost.Sample, error) {
	url := strings.TrimRight(base, "/") + "/v3/order_book/?aggregate=true&book=" + book
	resp, err := c.Get(url)
	if err != nil {
		return execcost.Sample{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return execcost.Sample{}, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	var rb restBook
	if err := json.NewDecoder(resp.Body).Decode(&rb); err != nil {
		return execcost.Sample{}, fmt.Errorf("decode: %w", err)
	}
	if !rb.Success {
		return execcost.Sample{}, fmt.Errorf("GET %s: success=false", url)
	}
	bids, err := levels(rb.Payload.Bids)
	if err != nil {
		return execcost.Sample{}, err
	}
	asks, err := levels(rb.Payload.Asks)
	if err != nil {
		return execcost.Sample{}, err
	}
	s, ok := execcost.NewSample(book, "bitso-rest", now, bids, asks, budget)
	if !ok {
		return execcost.Sample{}, fmt.Errorf("empty or crossed book (%d bids, %d asks)", len(bids), len(asks))
	}
	return s, nil
}

func levels(in []restLevel) ([]execcost.Level, error) {
	out := make([]execcost.Level, 0, min(len(in), execcost.SampleLevels))
	for _, l := range in {
		if len(out) == execcost.SampleLevels {
			break
		}
		p, err1 := strconv.ParseFloat(l.Price, 64)
		a, err2 := strconv.ParseFloat(l.Amount, 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("bad level %+v", l)
		}
		out = append(out, execcost.Level{Price: p, Amount: a})
	}
	return out, nil
}
