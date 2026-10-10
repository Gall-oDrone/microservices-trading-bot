// Package yahoodaily fetches daily bars from Yahoo Finance's chart API
// (query1.finance.yahoo.com/v8/finance/chart) for research only: index
// history (^NDX, ^GSPC) decades longer than eToro's 1000-bar window, ETF
// benchmarks with dividend-adjusted closes (QQQ, SPY) and the 13-week T-bill
// yield (^IRX) used as the financing reference rate. No live decision may
// depend on it: it is unofficial and unauthenticated.
//
// Bars are labelled with their exchange calendar date (America/New_York for
// US listings). A bar whose session has not closed yet at fetch time is
// dropped. CSVs keep the research column layout (bitsodaily.ReadCSV reads
// them) plus adj_close.
package yahoodaily

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"bitso-trading-platform/shared/pkg/dailyrule"
	"bitso-trading-platform/shared/pkg/mktcal"
)

// Row is one daily bar.
type Row struct {
	Date                   string // exchange calendar date
	Open, High, Low, Close float64
	AdjClose               float64 // dividend/split adjusted close (= Close for indices)
	Volume                 int64
}

type chartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol               string `json:"symbol"`
				ExchangeTimezoneName string `json:"exchangeTimezoneName"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*float64 `json:"volume"`
				} `json:"quote"`
				AdjClose []struct {
					AdjClose []*float64 `json:"adjclose"`
				} `json:"adjclose"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

// BaseURL is the chart endpoint (overridable in tests).
var BaseURL = "https://query1.finance.yahoo.com/v8/finance/chart/"

// Fetch downloads the full daily history of symbol and parses it with Parse.
func Fetch(ctx context.Context, c *http.Client, symbol string, now time.Time) ([]Row, error) {
	q := url.Values{
		"period1": {"0"}, "period2": {strconv.FormatInt(now.Unix(), 10)},
		"interval": {"1d"}, "events": {"div,split"}, "includeAdjustedClose": {"true"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL+url.PathEscape(symbol)+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (research; microservices-trading-bot)")
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo %s: HTTP %d", symbol, res.StatusCode)
	}
	return Parse(raw, now)
}

// Parse decodes a chart response. Rows with a missing or non-positive close
// are skipped; a missing open/high/low falls back to the close (^IRX). The
// bar of a session still open at now is dropped.
func Parse(raw []byte, now time.Time) ([]Row, error) {
	var cr chartResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return nil, fmt.Errorf("yahoo: decode: %w", err)
	}
	if cr.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo: %s: %s", cr.Chart.Error.Code, cr.Chart.Error.Description)
	}
	if len(cr.Chart.Result) == 0 || len(cr.Chart.Result[0].Indicators.Quote) == 0 {
		return nil, fmt.Errorf("yahoo: empty result")
	}
	r := cr.Chart.Result[0]
	loc := mktcal.NewYork
	if r.Meta.ExchangeTimezoneName != "" {
		if l, err := time.LoadLocation(r.Meta.ExchangeTimezoneName); err == nil {
			loc = l
		}
	}
	q := r.Indicators.Quote[0]
	var adj []*float64
	if len(r.Indicators.AdjClose) > 0 {
		adj = r.Indicators.AdjClose[0].AdjClose
	}
	at := func(s []*float64, i int) (float64, bool) {
		if i < len(s) && s[i] != nil {
			return *s[i], true
		}
		return 0, false
	}
	byDate := map[string]Row{}
	for i, ts := range r.Timestamp {
		c, ok := at(q.Close, i)
		if !ok || !(c > 0) {
			continue
		}
		day := time.Unix(ts, 0).In(loc)
		civil := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
		if _, closeAt, open := mktcal.Session(civil); open && closeAt.After(now) {
			continue // session not closed yet
		}
		row := Row{Date: civil.Format("2006-01-02"), Close: c, AdjClose: c}
		row.Open, _ = at(q.Open, i)
		row.High, _ = at(q.High, i)
		row.Low, _ = at(q.Low, i)
		for _, p := range []*float64{&row.Open, &row.High, &row.Low} {
			if !(*p > 0) {
				*p = c
			}
		}
		if row.High < row.Close {
			row.High = row.Close
		}
		if row.Low > row.Close {
			row.Low = row.Close
		}
		if a, ok := at(adj, i); ok && a > 0 {
			row.AdjClose = a
		}
		if v, ok := at(q.Volume, i); ok {
			row.Volume = int64(v)
		}
		byDate[row.Date] = row
	}
	rows := make([]Row, 0, len(byDate))
	for _, v := range byDate {
		rows = append(rows, v)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Date < rows[j].Date })
	return rows, nil
}

var header = []string{"date", "book", "open", "high", "low", "close", "volume", "vwap", "trade_count", "bucket_start_utc", "adj_close"}

// WriteCSV writes rows in the research layout plus adj_close.
func WriteCSV(path, symbol string, rows []Row) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	_ = w.Write(header)
	ff := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	for _, r := range rows {
		_ = w.Write([]string{r.Date, symbol, ff(r.Open), ff(r.High), ff(r.Low), ff(r.Close),
			strconv.FormatInt(r.Volume, 10), "", "0", "", ff(r.AdjClose)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadCSV reads a file written by WriteCSV.
func ReadCSV(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("%s: empty", path)
	}
	col := map[string]int{}
	for i, h := range recs[0] {
		col[h] = i
	}
	num := func(rec []string, k string) float64 {
		if i, ok := col[k]; ok && i < len(rec) {
			v, _ := strconv.ParseFloat(rec[i], 64)
			return v
		}
		return 0
	}
	var out []Row
	for _, rec := range recs[1:] {
		r := Row{Date: rec[col["date"]], Open: num(rec, "open"), High: num(rec, "high"), Low: num(rec, "low"),
			Close: num(rec, "close"), AdjClose: num(rec, "adj_close"), Volume: int64(num(rec, "volume"))}
		if r.AdjClose <= 0 {
			r.AdjClose = r.Close
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// Bars converts rows to price bars.
func Bars(rows []Row) []dailyrule.Bar {
	out := make([]dailyrule.Bar, 0, len(rows))
	for _, r := range rows {
		d, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			continue
		}
		out = append(out, dailyrule.Bar{Date: d, Open: r.Open, High: r.High, Low: r.Low, Close: r.Close})
	}
	return out
}

// TotalReturnBars scales each bar's OHLC by AdjClose/Close, so holding the
// series earns dividends (what an ETF holder receives).
func TotalReturnBars(rows []Row) []dailyrule.Bar {
	out := make([]dailyrule.Bar, 0, len(rows))
	for _, r := range rows {
		d, err := time.Parse("2006-01-02", r.Date)
		if err != nil || r.Close <= 0 {
			continue
		}
		k := r.AdjClose / r.Close
		out = append(out, dailyrule.Bar{Date: d, Open: r.Open * k, High: r.High * k, Low: r.Low * k, Close: r.AdjClose})
	}
	return out
}

// Series looks up a value by date, carrying the last known value forward.
type Series struct {
	dates []time.Time
	vals  []float64
}

// NewSeries builds a series from rows using fn to pick the value.
func NewSeries(rows []Row, fn func(Row) float64) Series {
	var s Series
	for _, r := range rows {
		d, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			continue
		}
		s.dates = append(s.dates, d)
		s.vals = append(s.vals, fn(r))
	}
	return s
}

// At returns the last value on or before d.
func (s Series) At(d time.Time) (float64, bool) {
	i := sort.Search(len(s.dates), func(i int) bool { return s.dates[i].After(d) }) - 1
	if i < 0 {
		return 0, false
	}
	return s.vals[i], true
}

// Last returns the most recent value and its date.
func (s Series) Last() (time.Time, float64, bool) {
	if len(s.dates) == 0 {
		return time.Time{}, 0, false
	}
	return s.dates[len(s.dates)-1], s.vals[len(s.vals)-1], true
}
