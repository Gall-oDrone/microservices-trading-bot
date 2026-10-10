// Package etoro is a client for the eToro Public API (https://api-portal.etoro.com).
//
// Authentication is API-key based: every request carries x-api-key (the
// application key, ETORO_PUBLIC_KEY), x-user-key (the user's key,
// ETORO_PRIVATE_KEY) and a UUID x-request-id.
//
// Environments. A user key is created for one environment, Virtual (demo) or
// Real. Market-data routes are shared; trading and account routes differ per
// environment, and the naming is not uniform across API versions (see
// paths.go). The client defaults to demo.
//
// Idempotency. eToro rejects an open order whose x-request-id was already
// used (HTTP 400 "ReferenceID ... may already exists", IsDuplicateReference),
// so re-sending an order with the SAME request id can never open twice.
// Callers that must not double-open pass RequestIDFor(clientRef) to
// OpenOrder and, after a timeout or an unknown outcome, re-send with the same
// id: success means the first attempt never landed, a duplicate-reference
// rejection means it did (find its position in the portfolio). Note that
// orders:lookup?referenceId= does NOT find API-placed orders (it answers 404
// "No external operation was found"); look orders up by orderId. The client
// never retries a state-changing call on its own; reads are retried on
// 429/5xx/transport errors, honouring Retry-After.
//
// Rate limits are per user key over a rolling minute: 60/min for most reads,
// 20/min for trading writes (docs: core/getting-started/rate-limits). The
// client paces itself below both with two token buckets.
//
// Route facts verified against the demo API on 2026-10-10 are recorded in
// docs/etoro/evidence-2026-10-10/README.md.
package etoro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the Public API host (paths carry /api/v1 or /api/v2).
const DefaultBaseURL = "https://public-api.etoro.com"

// Default pacing, kept under the documented 60 reads / 20 writes per minute.
const (
	DefaultReadsPerMinute  = 55
	DefaultWritesPerMinute = 18
	defaultReadRetries     = 2
	maxRetryWait           = 60 * time.Second
	maxResponseBytes       = 64 << 20
)

// Client calls the eToro Public API. It is safe for concurrent use.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	userKey     string
	env         Environment
	reads       *limiter
	writes      *limiter
	readRetries int
	sleep       func(context.Context, time.Duration) error
}

// Config holds credentials and runtime options for the client.
type Config struct {
	PublicKey  string // x-api-key (application key)
	PrivateKey string // x-user-key (per-user key)
	Env        Environment
	HTTPClient *http.Client
	// BaseURL overrides DefaultBaseURL (tests).
	BaseURL string
	// ReadsPerMinute / WritesPerMinute override the pacing; 0 keeps the default.
	ReadsPerMinute  int
	WritesPerMinute int
	// ReadRetries is how many times a failed read is retried; 0 keeps the
	// default (2), a negative value disables retries.
	ReadRetries int
}

// NewClient creates an authenticated Public API client.
func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.PublicKey) == "" || strings.TrimSpace(cfg.PrivateKey) == "" {
		return nil, fmt.Errorf("etoro public and private API keys are required")
	}
	env := cfg.Env
	if env == "" {
		env = EnvDemo
	}
	if env != EnvDemo && env != EnvReal {
		return nil, fmt.Errorf("invalid etoro environment %q: use demo or real", env)
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	reads, writes := cfg.ReadsPerMinute, cfg.WritesPerMinute
	if reads <= 0 {
		reads = DefaultReadsPerMinute
	}
	if writes <= 0 {
		writes = DefaultWritesPerMinute
	}
	retries := cfg.ReadRetries
	switch {
	case retries == 0:
		retries = defaultReadRetries
	case retries < 0:
		retries = 0
	}
	return &Client{
		httpClient:  httpClient,
		baseURL:     base,
		apiKey:      cfg.PublicKey,
		userKey:     cfg.PrivateKey,
		env:         env,
		reads:       newLimiter(reads, time.Now),
		writes:      newLimiter(writes, time.Now),
		readRetries: retries,
		sleep:       sleepCtx,
	}, nil
}

// Environment returns the configured trading environment (demo or real).
func (c *Client) Environment() Environment { return c.env }

// call describes one request.
type call struct {
	method    string
	path      string // absolute API path, e.g. /api/v2/market-data/rates
	query     url.Values
	body      any
	requestID string // empty: a random UUID
	// write marks a state-changing request: it spends the write budget and
	// is never retried by the client.
	write bool
}

// do runs c with pacing and, for reads, bounded retries.
func (c *Client) do(ctx context.Context, cl call, out any) error {
	attempts := 1
	if !cl.write {
		attempts += c.readRetries
	}
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		lim := c.reads
		if cl.write {
			lim = c.writes
		}
		if werr := lim.wait(ctx, c.sleep); werr != nil {
			return werr
		}
		err = c.once(ctx, cl, out)
		if err == nil || cl.write || !IsRetryable(err) || attempt == attempts-1 {
			return err
		}
		if serr := c.sleep(ctx, retryWait(err, attempt)); serr != nil {
			return serr
		}
	}
	return err
}

// retryWait is Retry-After when the API sent one, else exponential backoff.
func retryWait(err error, attempt int) time.Duration {
	if ae, ok := asAPIError(err); ok && ae.RetryAfter > 0 {
		if ae.RetryAfter > maxRetryWait {
			return maxRetryWait
		}
		return ae.RetryAfter
	}
	return time.Duration(1<<attempt) * time.Second
}

func (c *Client) once(ctx context.Context, cl call, out any) error {
	var bodyReader io.Reader
	if cl.body != nil {
		raw, err := json.Marshal(cl.body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	u := c.baseURL + cl.path
	if len(cl.query) > 0 {
		u += "?" + encodeQuery(cl.query)
	}
	req, err := http.NewRequestWithContext(ctx, cl.method, u, bodyReader)
	if err != nil {
		return err
	}
	rid := cl.requestID
	if rid == "" {
		rid = NewRequestID()
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("x-user-key", c.userKey)
	req.Header.Set("x-request-id", rid)
	req.Header.Set("Accept", "application/json")
	if cl.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return &TransportError{Method: cl.method, Path: cl.path, RequestID: rid, Err: err}
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return &TransportError{Method: cl.method, Path: cl.path, RequestID: rid, Err: err}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return newAPIError(res.StatusCode, res.Header, payload, cl.method, cl.path, rid)
	}
	if out == nil || len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", cl.method, cl.path, err)
	}
	return nil
}

// encodeQuery is url.Values.Encode but keeps commas literal: the rates and
// eligibility routes expect instrumentIds=27,28, not 27%2C28.
func encodeQuery(q url.Values) string {
	return strings.ReplaceAll(q.Encode(), "%2C", ",")
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
