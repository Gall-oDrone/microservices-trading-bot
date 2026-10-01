// Command bitso-daily downloads Bitso's own daily OHLC candles for one book
// (default btc_mxn) from the public, unauthenticated /api/v3/ohlc endpoint
// and writes them as a single CSV that cmd/daily-research reads directly:
//
//	go run ./cmd/bitso-daily -book btc_mxn -from 2017-06-01 -out ./bitso/btc_mxn_daily.csv
//	go run ./cmd/daily-research -prices ./bitso -windows 2018-01-01:2026-09-26
//
// It exists because the Yahoo series is BTC-USD: it misses the USD/MXN leg and
// is not the market the bot actually trades.
//
// Day labelling. Bitso buckets days on Mexico City time (bucket_start_time is
// 06:00 UTC, or 05:00 UTC before Mexico dropped DST in Oct 2022). Each bar is
// labelled with its Mexico City calendar date. A bar labelled D therefore
// closes at D+1 00:00 Mexico time, which is 05:00-06:00 UTC on D+1. So any
// news whose UTC date is on or before D was published before that bar closed,
// and daily-research's "news up to day t" rule stays free of look-ahead.
//
// The in-progress (today's) bucket is dropped: its close is not a close.
// No credentials are used or needed. The fetch/CSV logic lives in
// internal/bitsodaily, shared with cmd/daily-executor.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"bitso-trading-platform/strategy-executor/internal/bitsodaily"
)

const day = bitsodaily.Day

func main() {
	book := flag.String("book", "btc_mxn", "Bitso book")
	from := flag.String("from", "2017-06-01", "first date to request (YYYY-MM-DD)")
	to := flag.String("to", "", "last date to request (YYYY-MM-DD, default today)")
	out := flag.String("out", "", "output CSV path (required)")
	base := flag.String("base-url", bitsodaily.DefaultBaseURL, "API base URL")
	chunkDays := flag.Int("chunk-days", 365, "days per request")
	flag.Parse()
	if *out == "" {
		fail(fmt.Errorf("-out is required"))
	}
	start, err := time.Parse("2006-01-02", *from)
	if err != nil {
		fail(err)
	}
	now := time.Now().UTC()
	end := now
	if *to != "" {
		if end, err = time.Parse("2006-01-02", *to); err != nil {
			fail(err)
		}
		end = end.Add(day)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	all, err := bitsodaily.FetchRange(client, *base, *book, start, end, time.Duration(*chunkDays)*day)
	if err != nil {
		fail(err)
	}
	rows, dropped := bitsodaily.ToRows(all, now)
	if len(rows) == 0 {
		fail(fmt.Errorf("no complete candles returned for %s", *book))
	}
	if err := bitsodaily.WriteCSV(*out, *book, rows); err != nil {
		fail(err)
	}
	fmt.Printf("%s: %d daily bars %s .. %s -> %s (dropped %d in-progress/duplicate)\n",
		*book, len(rows), rows[0].Date, rows[len(rows)-1].Date, *out, dropped)
	if g := bitsodaily.Gaps(rows); g != "" {
		fmt.Printf("gaps: %s\n", g)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "bitso-daily: %v\n", err)
	os.Exit(1)
}
