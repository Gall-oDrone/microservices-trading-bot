// Package bitsodaily fetches Bitso's own daily OHLC candles from the public,
// unauthenticated /api/v3/ohlc endpoint and reads/writes them as CSV.
//
// Day labelling. Bitso buckets days on Mexico City time (bucket_start_time is
// 06:00 UTC, or 05:00 UTC before Mexico dropped DST in Oct 2022). Each bar is
// labelled with its Mexico City calendar date. A bar labelled D therefore
// closes at D+1 00:00 Mexico time, which is 05:00-06:00 UTC on D+1.
//
// The in-progress (today's) bucket is dropped: its close is not a close.
// No credentials are used or needed.
//
// Used by cmd/bitso-daily (writes the evidence CSVs) and cmd/daily-executor
// (computes the day's decision), so both see identical bars.
package bitsodaily

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// Candle is one bucket as returned by /api/v3/ohlc.
type Candle struct {
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
	Payload []Candle `json:"payload"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Day is one calendar day.
const Day = 24 * time.Hour

// DefaultBaseURL is Bitso production. Candles always come from production:
// the forward tests are judged on real market prices, not stage's.
const DefaultBaseURL = "https://api.bitso.com"

// Mexico is the time zone Bitso uses to bucket daily candles.
var Mexico = mustLoc("America/Mexico_City")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// FetchRange walks [start, end) in chunks. The endpoint caps a response at a
// few hundred buckets, so chunks are kept under that.
func FetchRange(c *http.Client, base, book string, start, end time.Time, chunk time.Duration) ([]Candle, error) {
	var all []Candle
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
		if e.Before(end) {
			time.Sleep(1100 * time.Millisecond) // stay well inside the public rate limit
		}
	}
	return all, nil
}

func fetchOnce(c *http.Client, url string) ([]Candle, error) {
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

// Row is one closed daily candle.
type Row struct {
	Date                   string // Mexico City calendar date of the bucket
	Open, High, Low, Close float64
	Volume, VWAP           string
	Trades                 int64
	BucketStartUTC         time.Time
}

// ToRows converts candles to rows: labels each by Mexico City date, drops
// buckets that have not closed as of now, and de-duplicates chunk overlaps.
// It returns the rows sorted by date and the number of candles dropped.
func ToRows(cs []Candle, now time.Time) ([]Row, int) {
	byDate := map[string]Row{}
	dropped := 0
	for _, c := range cs {
		st := time.UnixMilli(c.BucketStart).UTC()
		if !st.Add(Day).Before(now) {
			dropped++
			continue
		}
		r := Row{
			Date:           st.In(Mexico).Format("2006-01-02"),
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
	out := make([]Row, 0, len(byDate))
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

var csvHeader = []string{"date", "book", "open", "high", "low", "close", "volume", "vwap", "trade_count", "bucket_start_utc"}

// WriteCSV writes rows in the format cmd/daily-research reads.
func WriteCSV(path, book string, rows []Row) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	_ = w.Write(csvHeader)
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

// ReadCSV reads a file written by WriteCSV. Rows are returned sorted by date.
func ReadCSV(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("%s: header: %w", path, err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[h] = i
	}
	for _, k := range []string{"date", "open", "high", "low", "close"} {
		if _, ok := col[k]; !ok {
			return nil, fmt.Errorf("%s: missing column %q", path, k)
		}
	}
	get := func(rec []string, k string) string {
		if i, ok := col[k]; ok && i < len(rec) {
			return rec[i]
		}
		return ""
	}
	var out []Row
	for line := 2; ; line++ {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		row := Row{Date: get(rec, "date"), Volume: get(rec, "volume"), VWAP: get(rec, "vwap")}
		if _, err := time.Parse("2006-01-02", row.Date); err != nil {
			return nil, fmt.Errorf("%s:%d: date %q: %w", path, line, row.Date, err)
		}
		for k, dst := range map[string]*float64{"open": &row.Open, "high": &row.High, "low": &row.Low, "close": &row.Close} {
			v, err := strconv.ParseFloat(get(rec, k), 64)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %s: %w", path, line, k, err)
			}
			*dst = v
		}
		row.Trades, _ = strconv.ParseInt(get(rec, "trade_count"), 10, 64)
		row.BucketStartUTC, _ = time.Parse(time.RFC3339, get(rec, "bucket_start_utc"))
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// Gaps describes missing calendar days between consecutive rows, or "".
func Gaps(rows []Row) string {
	var out []string
	for i := 1; i < len(rows); i++ {
		a, _ := time.Parse("2006-01-02", rows[i-1].Date)
		b, _ := time.Parse("2006-01-02", rows[i].Date)
		if n := int(b.Sub(a)/Day) - 1; n > 0 {
			out = append(out, fmt.Sprintf("%s..%s (%dd)", a.Add(Day).Format("2006-01-02"), b.Add(-Day).Format("2006-01-02"), n))
		}
	}
	if len(out) > 12 {
		return fmt.Sprintf("%d gaps, first: %v", len(out), out[:12])
	}
	return fmt.Sprint(out)[1 : len(fmt.Sprint(out))-1]
}
