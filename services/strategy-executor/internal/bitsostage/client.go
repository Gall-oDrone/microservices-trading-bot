// Package bitsostage is a minimal, signed client for the few Bitso private
// endpoints the daily executor needs. It talks to Bitso STAGE only: New
// refuses any other base URL. Production support is deliberately absent; it
// will be added behind explicit flags only if a forward test passes.
//
// Endpoints (https://docs.bitso.com/bitso-api):
//
//	GET    /v3/ticker?book=             best bid / ask
//	GET    /v3/balance                  available balances
//	POST   /v3/orders                   place (limit + time_in_force=postonly, or market)
//	GET    /v3/open_orders?book=        open / partially filled orders
//	DELETE /v3/orders/{oid}             cancel
//	GET    /v3/order_trades?origin_id=  every trade ever made under a client id
//
// The last one is what makes the executor idempotent: Bitso only rejects a
// reused origin_id while the earlier order is still active, and order lookups
// forget finished orders after about an hour, but trades by origin_id are
// returned regardless of age.
package bitsostage

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// StageBaseURL is the only base URL New accepts.
const StageBaseURL = "https://stage.bitso.com/api"

// Client signs requests with an API key and secret.
type Client struct {
	base, key, secret string
	http              *http.Client

	mu        sync.Mutex
	lastNonce int64
}

// New returns a stage client. It fails for any base URL other than stage, or
// when credentials are missing.
func New(baseURL, key, secret string) (*Client, error) {
	if strings.TrimRight(baseURL, "/") != StageBaseURL {
		return nil, fmt.Errorf("bitsostage: refusing base URL %q: only %s is allowed", baseURL, StageBaseURL)
	}
	if key == "" || secret == "" {
		return nil, fmt.Errorf("bitsostage: API key and secret are required")
	}
	return &Client{base: StageBaseURL, key: key, secret: secret, http: &http.Client{Timeout: 20 * time.Second}}, nil
}

// APIError is an error returned by Bitso in its JSON envelope.
type APIError struct {
	HTTPStatus int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bitso: HTTP %d code %s: %s", e.HTTPStatus, e.Code, e.Message)
}

func (c *Client) nonce() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := time.Now().UnixNano()
	if n <= c.lastNonce {
		n = c.lastNonce + 1
	}
	c.lastNonce = n
	return strconv.FormatInt(n, 10)
}

// do sends a signed request and decodes the payload into out.
func (c *Client) do(method, path string, query url.Values, body any, out any) error {
	reqPath := "/api" + path
	if len(query) > 0 {
		reqPath += "?" + query.Encode()
	}
	var data []byte
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			return err
		}
	}
	nonce := c.nonce()
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write([]byte(nonce + method + reqPath + string(data)))
	sig := hex.EncodeToString(mac.Sum(nil))

	u := strings.TrimSuffix(c.base, "/api") + reqPath
	req, err := http.NewRequest(method, u, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bitso %s:%s:%s", c.key, nonce, sig))
	req.Header.Set("User-Agent", "microservices-trading-bot/daily-executor")
	req.Header.Set("Accept", "application/json")
	if data != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var env struct {
		Success bool            `json:"success"`
		Payload json.RawMessage `json:"payload"`
		Error   *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		snippet := string(raw)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return &APIError{HTTPStatus: resp.StatusCode, Code: "non-json", Message: snippet}
	}
	if !env.Success {
		e := &APIError{HTTPStatus: resp.StatusCode}
		if env.Error != nil {
			e.Code, e.Message = env.Error.Code, env.Error.Message
		}
		return e
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Payload, out)
}

func num(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// Quote is the top of the book.
type Quote struct{ Bid, Ask float64 }

// Ticker returns the best bid and ask. Public, but signed calls are fine.
func (c *Client) Ticker(book string) (Quote, error) {
	var p struct{ Bid, Ask string }
	if err := c.do("GET", "/v3/ticker/", url.Values{"book": {book}}, nil, &p); err != nil {
		return Quote{}, err
	}
	q := Quote{Bid: num(p.Bid), Ask: num(p.Ask)}
	if q.Bid <= 0 || q.Ask <= 0 || q.Bid >= q.Ask {
		return Quote{}, fmt.Errorf("bitsostage: bad quote for %s: bid %v ask %v", book, p.Bid, p.Ask)
	}
	return q, nil
}

// Balances returns available balances by lower-case currency.
func (c *Client) Balances() (map[string]float64, error) {
	var p struct {
		Balances []struct{ Currency, Available string } `json:"balances"`
	}
	if err := c.do("GET", "/v3/balance/", nil, nil, &p); err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(p.Balances))
	for _, b := range p.Balances {
		out[strings.ToLower(b.Currency)] = num(b.Available)
	}
	return out, nil
}

// OrderRequest is a new order. Major and Price are decimal strings, already
// rounded to the book's precision by the caller.
type OrderRequest struct {
	Book        string `json:"book"`
	Side        string `json:"side"` // "buy" | "sell"
	Type        string `json:"type"` // "limit" | "market"
	Major       string `json:"major"`
	Price       string `json:"price,omitempty"`
	TimeInForce string `json:"time_in_force,omitempty"` // "postonly" for maker-only
	OriginID    string `json:"origin_id"`
}

// PlaceOrder submits an order and returns its oid.
func (c *Client) PlaceOrder(o OrderRequest) (string, error) {
	var p struct{ Oid string }
	if err := c.do("POST", "/v3/orders/", nil, o, &p); err != nil {
		return "", err
	}
	if p.Oid == "" {
		return "", fmt.Errorf("bitsostage: order accepted without an oid")
	}
	return p.Oid, nil
}

// OpenOrder is an open or partially filled order.
type OpenOrder struct {
	Oid, OriginID, Book, Side, Status string
	Price, Original, Unfilled         float64
	CreatedAt                         time.Time
}

// OpenOrders lists open and partially filled orders on a book.
func (c *Client) OpenOrders(book string) ([]OpenOrder, error) {
	var p []struct {
		Oid            string `json:"oid"`
		OriginID       string `json:"origin_id"`
		Book           string `json:"book"`
		Side           string `json:"side"`
		Status         string `json:"status"`
		Price          string `json:"price"`
		OriginalAmount string `json:"original_amount"`
		UnfilledAmount string `json:"unfilled_amount"`
		CreatedAt      string `json:"created_at"`
	}
	if err := c.do("GET", "/v3/open_orders/", url.Values{"book": {book}}, nil, &p); err != nil {
		return nil, err
	}
	out := make([]OpenOrder, 0, len(p))
	for _, o := range p {
		t, _ := time.Parse("2006-01-02T15:04:05-0700", o.CreatedAt)
		if t.IsZero() {
			t, _ = time.Parse(time.RFC3339, o.CreatedAt)
		}
		out = append(out, OpenOrder{
			Oid: o.Oid, OriginID: o.OriginID, Book: o.Book, Side: o.Side, Status: o.Status,
			Price: num(o.Price), Original: num(o.OriginalAmount), Unfilled: num(o.UnfilledAmount), CreatedAt: t.UTC(),
		})
	}
	return out, nil
}

// CancelOrder cancels an order by oid.
func (c *Client) CancelOrder(oid string) error {
	return c.do("DELETE", "/v3/orders/"+url.PathEscape(oid)+"/", nil, nil, nil)
}

// Trade is one fill. Major and Minor are absolute amounts.
type Trade struct {
	Tid                 string
	Oid, OriginID, Book string
	Side, MakerSide     string
	Major, Minor, Price float64
	Fee                 float64
	FeeCurrency         string
	CreatedAt           time.Time
}

// Bitso answers "no trades yet" with HTTP 400 rather than an empty list:
// 0312 for an origin_id it has no record of, 0378 ("Order has not matched
// yet") for an active order without fills. TradesByOrigin maps both to nil.
const (
	codeNoTrades   = "0312"
	codeNotMatched = "0378"
)

// TradesByOrigin returns every trade made under a client order id.
func (c *Client) TradesByOrigin(originID string) ([]Trade, error) {
	var p []struct {
		Tid          json.Number `json:"tid"` // a string on stage; accept numbers too
		Oid          string      `json:"oid"`
		OriginID     string      `json:"origin_id"`
		Book         string      `json:"book"`
		Side         string      `json:"side"`
		MakerSide    string      `json:"maker_side"`
		Major        string      `json:"major"`
		Minor        string      `json:"minor"`
		Price        string      `json:"price"`
		FeesAmount   string      `json:"fees_amount"`
		FeesCurrency string      `json:"fees_currency"`
		CreatedAt    string      `json:"created_at"`
	}
	// No trailing slash: "/v3/order_trades/?origin_id=" is a generic 404.
	if err := c.do("GET", "/v3/order_trades", url.Values{"origin_id": {originID}}, nil, &p); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && (apiErr.Code == codeNoTrades || apiErr.Code == codeNotMatched) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Trade, 0, len(p))
	for _, t := range p {
		ts, _ := time.Parse("2006-01-02T15:04:05-0700", t.CreatedAt)
		if ts.IsZero() {
			ts, _ = time.Parse(time.RFC3339, t.CreatedAt)
		}
		mj, mn := num(t.Major), num(t.Minor)
		if mj < 0 {
			mj = -mj
		}
		if mn < 0 {
			mn = -mn
		}
		out = append(out, Trade{
			Tid: t.Tid.String(), Oid: t.Oid, OriginID: t.OriginID, Book: t.Book, Side: t.Side, MakerSide: t.MakerSide,
			Major: mj, Minor: mn, Price: num(t.Price), Fee: num(t.FeesAmount), FeeCurrency: t.FeesCurrency, CreatedAt: ts.UTC(),
		})
	}
	return out, nil
}
