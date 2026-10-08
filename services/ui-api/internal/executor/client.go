// Package executor is ui-api's small client for strategy-executor's strategy
// API (/api/v1/strategies). ui-api lists strategies for the Strategies page
// and forwards audited operator start/stop requests; it never sends ticks,
// signals or orders.
package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Hold is an operator hold (persisted stop) on a strategy.
type Hold struct {
	Reason string `json:"reason"`
	By     string `json:"by"`
	At     string `json:"at"`
}

// State is the subset of strategy-executor's StrategyState the UI shows.
type State struct {
	HasPosition     bool    `json:"has_position"`
	PositionSide    string  `json:"position_side,omitempty"`
	PositionSize    float64 `json:"position_size"`
	EntryPrice      float64 `json:"entry_price,omitempty"`
	UnrealizedPnL   float64 `json:"unrealized_pnl"`
	LastSignalTime  string  `json:"last_signal_time,omitempty"`
	SignalCount     int64   `json:"signal_count"`
	TradeCount      int64   `json:"trade_count"`
	ConsecutiveLoss int     `json:"consecutive_loss"`
	PendingBuy      bool    `json:"pending_buy,omitempty"`
	PendingSell     bool    `json:"pending_sell,omitempty"`
}

// Metrics is the subset of StrategyMetrics the UI shows.
type Metrics struct {
	TotalPnL    float64 `json:"total_pnl"`
	DailyPnL    float64 `json:"daily_pnl"`
	MaxDrawdown float64 `json:"max_drawdown"`
	WinRate     float64 `json:"win_rate"`
}

// Strategy is one entry of GET /api/v1/strategies.
type Strategy struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	Version    string                 `json:"version"`
	Book       string                 `json:"book"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Running    bool                   `json:"running"`
	Enabled    bool                   `json:"enabled"`
	State      State                  `json:"state"`
	Metrics    Metrics                `json:"metrics"`
	Hold       *Hold                  `json:"hold,omitempty"`
}

// List is GET /api/v1/strategies. Holds is absent on executors older than
// the hold list.
type List struct {
	Strategies []Strategy      `json:"strategies"`
	Count      int             `json:"count"`
	Holds      map[string]Hold `json:"holds"`
}

// Client talks to one strategy-executor.
type Client struct {
	base *url.URL
	http *http.Client
}

// New checks raw and returns a client. Until ui-api has real auth the
// executor must be on a loopback address (its API has no auth of its own).
func New(raw string) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil {
		return nil, fmt.Errorf("strategy-executor URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("strategy-executor URL %q: want http:// or https://", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("strategy-executor URL %q: no user info, query or fragment", raw)
	}
	h := u.Hostname()
	ip := net.ParseIP(h)
	if h != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("strategy-executor URL %q: must be a loopback host while ui-api has no TLS/OIDC", raw)
	}
	return &Client{base: u, http: &http.Client{Timeout: 5 * time.Second}}, nil
}

// URL is the executor base URL, for display and audit.
func (c *Client) URL() string { return c.base.String() }

func (c *Client) endpoint(parts ...string) string {
	p := c.base.Path + "/api/v1/strategies"
	for _, s := range parts {
		p += "/" + url.PathEscape(s)
	}
	u := *c.base
	u.Path = p
	u.RawPath = ""
	return u.String()
}

// Error is a non-2xx answer from the executor.
type Error struct {
	Status int
	Code   string // strategy-executor's error code (e.g. "held"), if any
	Msg    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("strategy-executor answered %d: %s", e.Status, e.Msg)
}

func readError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	e := &Error{Status: resp.StatusCode, Msg: strings.TrimSpace(string(b))}
	var body struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	if json.Unmarshal(b, &body) == nil && body.Error != "" {
		e.Msg, e.Code = body.Error, body.Code
	}
	if len(e.Msg) > 300 {
		e.Msg = e.Msg[:300]
	}
	return e
}

func (c *Client) do(ctx context.Context, method, u string, body interface{}, out interface{}) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return readError(resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
		return fmt.Errorf("strategy-executor response: %w", err)
	}
	return nil
}

// List returns every registered strategy and the hold list.
func (c *Client) List(ctx context.Context) (List, error) {
	var l List
	err := c.do(ctx, http.MethodGet, c.endpoint(), nil, &l)
	if l.Strategies == nil {
		l.Strategies = []Strategy{}
	}
	return l, err
}

// Get returns one strategy. A missing strategy is an *Error with Status 404.
func (c *Client) Get(ctx context.Context, name string) (Strategy, error) {
	var s Strategy
	err := c.do(ctx, http.MethodGet, c.endpoint(name), nil, &s)
	return s, err
}

// LifecycleRequest is the operator body strategy-executor accepts on start
// and stop (see its lifecycleRequest).
type LifecycleRequest struct {
	By          string `json:"by,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Hold        bool   `json:"hold,omitempty"`
	ReleaseHold bool   `json:"release_hold,omitempty"`
}

// Stop is the operator stop: it asks the executor to hold the strategy
// (persisted, survives restarts) and stop it if running.
func (c *Client) Stop(ctx context.Context, name, by, reason string) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	err := c.do(ctx, http.MethodPost, c.endpoint(name, "stop"), LifecycleRequest{By: by, Reason: reason, Hold: true}, &out)
	return out, err
}

// Start is the operator start: it releases any hold and starts the strategy.
func (c *Client) Start(ctx context.Context, name, by, reason string) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	err := c.do(ctx, http.MethodPost, c.endpoint(name, "start"), LifecycleRequest{By: by, Reason: reason, ReleaseHold: true}, &out)
	return out, err
}

// StatusOf returns the executor's HTTP status for err (0 when the executor
// did not answer).
func StatusOf(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}
