package etoro

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Instrument is one row of /api/v1/market-data/search. The search route
// uses internal* field names (internalInstrumentId, internalSymbolFull); the
// previous client decoded "instrumentId" and so always got 0.
type Instrument struct {
	InstrumentID     int64  `json:"internalInstrumentId"`
	Symbol           string `json:"internalSymbolFull"`
	DisplayName      string `json:"internalInstrumentDisplayName"`
	AssetClassID     int    `json:"internalAssetClassId"`
	AssetClass       string `json:"internalAssetClassName"`
	ExchangeID       int    `json:"internalExchangeId"`
	Exchange         string `json:"internalExchangeName"`
	HiddenFromClient bool   `json:"isHiddenFromClient"`
}

type searchResponse struct {
	Page       int          `json:"page"`
	PageSize   int          `json:"pageSize"`
	TotalItems int          `json:"totalItems"`
	Items      []Instrument `json:"items"`
}

// SearchInstruments returns the search route's matches for symbol. The
// route matches loosely (NSDQ100 also returns other instruments), so use
// ResolveSymbol for an exact lookup.
func (c *Client) SearchInstruments(ctx context.Context, symbol string) ([]Instrument, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	var resp searchResponse
	q := url.Values{"internalSymbolFull": {symbol}}
	if err := c.do(ctx, call{method: "GET", path: pathSearch, query: q}, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// ResolveSymbol returns the one visible instrument whose symbol equals
// symbol (case-insensitive). No match or more than one is an error: an
// order must never go to a guessed instrument.
func (c *Client) ResolveSymbol(ctx context.Context, symbol string) (Instrument, error) {
	items, err := c.SearchInstruments(ctx, symbol)
	if err != nil {
		return Instrument{}, err
	}
	var found []Instrument
	for _, it := range items {
		if strings.EqualFold(it.Symbol, strings.TrimSpace(symbol)) && !it.HiddenFromClient && it.InstrumentID > 0 {
			found = append(found, it)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return Instrument{}, fmt.Errorf("etoro: no instrument with symbol %q (%d loose matches)", symbol, len(items))
	default:
		return Instrument{}, fmt.Errorf("etoro: %d instruments with symbol %q", len(found), symbol)
	}
}

// Rate is a live bid/ask from /api/v2/market-data/rates.
type Rate struct {
	InstrumentID int64   `json:"instrumentId"`
	Bid          float64 `json:"bid"`
	Ask          float64 `json:"ask"`
	// Date is the quote time as sent (UTC, no zone suffix).
	Date      string `json:"date"`
	QuoteType string `json:"quoteType"`
}

// Mid is (bid+ask)/2.
func (r Rate) Mid() float64 { return (r.Bid + r.Ask) / 2 }

// Spread is ask-bid.
func (r Rate) Spread() float64 { return r.Ask - r.Bid }

// Time parses Date as UTC; zero when absent or unparsable.
func (r Rate) Time() time.Time { return parseAPITime(r.Date) }

type ratesResponse struct {
	Results []Rate `json:"results"`
}

// GetRates fetches bid/ask for one or more instrument IDs (v2 route).
func (c *Client) GetRates(ctx context.Context, instrumentIDs ...int64) ([]Rate, error) {
	if len(instrumentIDs) == 0 {
		return nil, fmt.Errorf("at least one instrument ID is required")
	}
	var resp ratesResponse
	q := url.Values{"instrumentIds": {joinIDs(instrumentIDs)}}
	if err := c.do(ctx, call{method: "GET", path: pathRates, query: q}, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

// Rate returns the quote for one instrument.
func (c *Client) Rate(ctx context.Context, instrumentID int64) (Rate, error) {
	rs, err := c.GetRates(ctx, instrumentID)
	if err != nil {
		return Rate{}, err
	}
	for _, r := range rs {
		if r.InstrumentID == instrumentID {
			return r, nil
		}
	}
	return Rate{}, fmt.Errorf("etoro: no rate for instrument %d", instrumentID)
}

// CandleInterval is an interval of the live market-data candles route.
type CandleInterval string

// Intervals used by this repository (the route accepts more).
const (
	OneMinute   CandleInterval = "OneMinute"
	FiveMinutes CandleInterval = "FiveMinutes"
	OneHour     CandleInterval = "OneHour"
	OneDay      CandleInterval = "OneDay"
)

// MaxCandles is the most candles the live route returns per call.
const MaxCandles = 1000

// Candle is one bar of the live candles route. Daily bars are UTC days
// (fromDate 00:00Z); see etorodaily for how they become trading-day bars.
type Candle struct {
	InstrumentID int64     `json:"instrumentID"`
	FromDate     time.Time `json:"fromDate"`
	Open         float64   `json:"open"`
	High         float64   `json:"high"`
	Low          float64   `json:"low"`
	Close        float64   `json:"close"`
	Volume       *float64  `json:"volume"`
}

type candlesResponse struct {
	Interval string `json:"interval"`
	Candles  []struct {
		InstrumentID int64    `json:"instrumentId"`
		Candles      []Candle `json:"candles"`
	} `json:"candles"`
}

// Candles returns the newest count (≤ MaxCandles) bars of an instrument,
// oldest first. The route is live: the current, unfinished bar is included.
func (c *Client) Candles(ctx context.Context, instrumentID int64, interval CandleInterval, count int) ([]Candle, error) {
	if count <= 0 || count > MaxCandles {
		return nil, fmt.Errorf("candle count must be 1..%d, got %d", MaxCandles, count)
	}
	var resp candlesResponse
	path := fmt.Sprintf(pathCandlesFmt, instrumentID, "desc", interval, count)
	if err := c.do(ctx, call{method: "GET", path: path}, &resp); err != nil {
		return nil, err
	}
	var out []Candle
	for _, g := range resp.Candles {
		if g.InstrumentID == 0 || g.InstrumentID == instrumentID {
			out = append(out, g.Candles...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FromDate.Before(out[j].FromDate) })
	return out, nil
}

// HistCandle is one bar of the DataPlatform history route
// (/api/v1/data/instruments/{id}/candles). That view is rebuilt by a batch
// job and LAGS the market (by ~11 days on 2026-10-10): use it for research
// history, never for a live decision. Its 1d bars are sessions that open at
// 21:00/22:00 UTC, unlike the live route's UTC days.
type HistCandle struct {
	Time   time.Time `json:"time"`
	Open   flexFloat `json:"open"`
	High   flexFloat `json:"high"`
	Low    flexFloat `json:"low"`
	Close  flexFloat `json:"close"`
	Volume *float64  `json:"volume"`
}

type histResponse struct {
	InstrumentID int64  `json:"instrumentId"`
	Symbol       string `json:"symbol"`
	Interval     string `json:"interval"`
	Pagination   struct {
		HasNext    bool    `json:"hasNext"`
		NextCursor *string `json:"nextCursor"`
	} `json:"pagination"`
	Results []HistCandle `json:"results"`
}

// HistoryCandles walks the history route for [from, to) (zero values: the
// route's defaults) following nextCursor, at most maxPages pages of up to
// 2000 bars, and returns the bars oldest first. interval is the route's own
// enum: 1m 5m 10m 15m 30m 1h 4h 1d 1w.
func (c *Client) HistoryCandles(ctx context.Context, instrumentID int64, interval string, from, to time.Time, maxPages int) ([]HistCandle, error) {
	if maxPages <= 0 {
		maxPages = 1
	}
	q := url.Values{"interval": {interval}, "limit": {"2000"}}
	if !from.IsZero() {
		q.Set("from", from.UTC().Format(time.RFC3339))
	}
	if !to.IsZero() {
		q.Set("to", to.UTC().Format(time.RFC3339))
	}
	path := fmt.Sprintf(pathHistoryFmt, instrumentID)
	seen := map[time.Time]bool{}
	var out []HistCandle
	for page := 0; page < maxPages; page++ {
		var resp histResponse
		if err := c.do(ctx, call{method: "GET", path: path, query: q}, &resp); err != nil {
			return nil, err
		}
		for _, h := range resp.Results {
			if !seen[h.Time] {
				seen[h.Time] = true
				out = append(out, h)
			}
		}
		if !resp.Pagination.HasNext || resp.Pagination.NextCursor == nil || *resp.Pagination.NextCursor == "" {
			break
		}
		q.Set("cursor", *resp.Pagination.NextCursor)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}

// flexFloat decodes a JSON number or a numeric string.
type flexFloat float64

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("flexFloat %q: %w", s, err)
	}
	*f = flexFloat(v)
	return nil
}

// MarshalJSON writes a plain number.
func (f flexFloat) MarshalJSON() ([]byte, error) { return json.Marshal(float64(f)) }

// Float returns the value.
func (f flexFloat) Float() float64 { return float64(f) }

func joinIDs(ids []int64) string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(s, ",")
}

// parseAPITime parses the API's timestamps: RFC 3339 with or without a zone
// and with or without fractional seconds; a missing zone means UTC.
func parseAPITime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05Z07:00", "2006-01-02 15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
