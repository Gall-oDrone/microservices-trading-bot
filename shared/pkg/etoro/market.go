package etoro

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// SearchResult is a single instrument match from /market-data/search.
type SearchResult struct {
	InstrumentID int64  `json:"instrumentId"`
	SymbolFull   string `json:"symbolFull"`
}

type searchResponse struct {
	Items []SearchResult `json:"items"`
}

// SearchInstrument resolves a ticker symbol (e.g. AAPL, BTC) to an instrument ID.
func (c *Client) SearchInstrument(ctx context.Context, symbol string) (SearchResult, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return SearchResult{}, fmt.Errorf("symbol is required")
	}
	q := url.Values{}
	q.Set("internalSymbolFull", symbol)
	var resp searchResponse
	if err := c.doJSON(ctx, httpMethodGet, "/market-data/search?"+q.Encode(), nil, &resp); err != nil {
		return SearchResult{}, err
	}
	if len(resp.Items) == 0 {
		return SearchResult{}, fmt.Errorf("no instrument found for symbol %s", symbol)
	}
	return resp.Items[0], nil
}

// Rate is a live quote for an instrument.
type Rate struct {
	InstrumentID   int64   `json:"instrumentID"`
	Ask            float64 `json:"ask"`
	Bid            float64 `json:"bid"`
	LastExecution  float64 `json:"lastExecution"`
}

type ratesResponse struct {
	Rates []Rate `json:"rates"`
}

// GetRates fetches bid/ask for one or more instrument IDs.
// instrumentIds must be comma-separated in the query string (not URL-encoded commas).
func (c *Client) GetRates(ctx context.Context, instrumentIDs ...int64) ([]Rate, error) {
	if len(instrumentIDs) == 0 {
		return nil, fmt.Errorf("at least one instrument ID is required")
	}
	ids := make([]string, len(instrumentIDs))
	for i, id := range instrumentIDs {
		ids[i] = strconv.FormatInt(id, 10)
	}
	path := "/market-data/instruments/rates?instrumentIds=" + strings.Join(ids, ",")
	var resp ratesResponse
	if err := c.doJSON(ctx, httpMethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Rates, nil
}

const httpMethodGet = "GET"
