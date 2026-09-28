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
// No credentials are used or needed.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

type candle struct {
	BucketStart int64  `json:"bucket_start_time"` // ms since epoch
	FirstRate   string `json:"first_rate"`
	LastRate    string `json:"last_rate"`
	MinRate     string `json:"min_rate"`
	MaxRate     string `json:"max_rate"`
	TradeCount  int64  `json:"trade_count"`
	Volume      string `json:"volume"`
	VWAP        string `json:"vwap"`
}

type ohlcResponse struct {
	Success bool     `json:"success"`
	Payload []candle `json:"payload"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

const day = 24 * time.Hour

func main() {
	book := flag.String("book", "btc_mxn", "Bitso book")
	from := flag.String("from", "2017-06-01", "first date to request (YYYY-MM-DD)")
	to := flag.String("to", "", "last date to request (YYYY-MM-DD, default today)")
	out := flag.String("out", "", "output CSV path (required)")
	base := flag.String("base-url", "https://api.bitso.com", "API base URL")
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
	all, err := fetchRange(client, *base, *book, start, end, time.Duration(*chunkDays)*day)
	if err != nil {
		fail(err)
	}
	rows, dropped := toRows(all, now)
	if len(rows) == 0 {
		fail(fmt.Errorf("no complete candles returned for %s", *book))
	}
	if err := writeCSV(*out, *book, rows); err != nil {
		fail(err)
	}
	fmt.Printf("%s: %d daily bars %s .. %s -> %s (dropped %d in-progress/duplicate)\n",
		*book, len(rows), rows[0].Date, rows[len(rows)-1].Date, *out, dropped)
	if g := gaps(rows); g != "" {
		fmt.Printf("gaps: %s\n", g)
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "bitso-daily: %v\n", err)
	os.Exit(1)
}

// fetchRange walks [start, end) in chunks. The endpoint caps a response at a
// few hundred buckets, so chunks are kept under that.
func fetchRange(c *http.Client, base, book string, start, end time.Time, chunk time.Duration) ([]candle, error) {
	var all []candle
	for s := start; s.Before(end); s = s.Add(chunk) {
		e := s.Add(chunk)
		if e.After(end) {
			e = end
		}
		url := fmt.Sprintf("%s/api/v3/ohlc?book=%s&time_bucket=86400&start=%d&end=%d",
			base, book, s.UnixMilli(), e.UnixMilli())
		got, err := fetchOnce(c, url)
		if err != nil {
			return nil, err
		}
		all = append(all, got...)
		time.Sleep(1100 * time.Millisecond) // stay well inside the public rate limit
	}
	return all, nil
}

func fetchOnce(c *http.Client, url string) ([]candle, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 3 * time.Second)
		}
		resp, err := c.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		var r ohlcResponse
		err = json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("%s: decode: %w", url, err)
			continue
		}
		if resp.StatusCode != http.StatusOK || !r.Success {
			msg := resp.Status
			if r.Error != nil {
				msg = r.Error.Code + " " + r.Error.Message
			}
			lastErr = fmt.Errorf("%s: %s", url, msg)
			continue
		}
		return r.Payload, nil
	}
	return nil, lastErr
}

type row struct {
	Date                   string // Mexico City calendar date of the bucket
	Open, High, Low, Close float64
	Volume, VWAP           string
	Trades                 int64
	BucketStartUTC         time.Time
}

var mexico = mustLoc("America/Mexico_City")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// toRows converts candles to rows: labels each by Mexico City date, drops
// buckets that have not closed as of now, and de-duplicates chunk overlaps.
func toRows(cs []candle, now time.Time) ([]row, int) {
	byDate := map[string]row{}
	dropped := 0
	for _, c := range cs {
		st := time.UnixMilli(c.BucketStart).UTC()
		if !st.Add(day).Before(now) {
			dropped++
			continue
		}
		r := row{
			Date:           st.In(mexico).Format("2006-01-02"),
			Open:           num(c.FirstRate),
			High:           num(c.MaxRate),
			Low:            num(c.MinRate),
			Close:          num(c.LastRate),
			Volume:         c.Volume,
			VWAP:           c.VWAP,
			Trades:         c.TradeCount,
			BucketStartUTC: st,
		}
		if r.Open <= 0 || r.Close <= 0 {
			dropped++
			continue
		}
		if _, dup := byDate[r.Date]; dup {
			dropped++
		}
		byDate[r.Date] = r
	}
	out := make([]row, 0, len(byDate))
	for _, r := range byDate {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, dropped
}

func num(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func writeCSV(path, book string, rows []row) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"date", "book", "open", "high", "low", "close", "volume", "vwap", "trade_count", "bucket_start_utc"})
	for _, r := range rows {
		_ = w.Write([]string{
			r.Date, book,
			strconv.FormatFloat(r.Open, 'f', -1, 64),
			strconv.FormatFloat(r.High, 'f', -1, 64),
			strconv.FormatFloat(r.Low, 'f', -1, 64),
			strconv.FormatFloat(r.Close, 'f', -1, 64),
			r.Volume, r.VWAP, strconv.FormatInt(r.Trades, 10),
			r.BucketStartUTC.Format(time.RFC3339),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func gaps(rows []row) string {
	var out []string
	for i := 1; i < len(rows); i++ {
		a, _ := time.Parse("2006-01-02", rows[i-1].Date)
		b, _ := time.Parse("2006-01-02", rows[i].Date)
		if n := int(b.Sub(a)/day) - 1; n > 0 {
			out = append(out, fmt.Sprintf("%s..%s (%dd)", a.Add(day).Format("2006-01-02"), b.Add(-day).Format("2006-01-02"), n))
		}
	}
	if len(out) > 12 {
		return fmt.Sprintf("%d gaps, first: %v", len(out), out[:12])
	}
	return fmt.Sprint(out)[1 : len(fmt.Sprint(out))-1]
}
