package etoro

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const publicAPIBaseURL = "https://public-api.etoro.com/api/v1"

// Client calls the eToro Public API using API-key authentication.
// ETORO_PUBLIC_KEY maps to x-api-key; ETORO_PRIVATE_KEY maps to x-user-key.
type Client struct {
	httpClient *http.Client
	apiKey     string
	userKey    string
	env        Environment
}

// Config holds credentials and runtime options for the eToro client.
type Config struct {
	PublicKey  string // x-api-key (partner API key)
	PrivateKey string // x-user-key (per-user key)
	Env        Environment
	HTTPClient *http.Client
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
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		httpClient: httpClient,
		apiKey:     cfg.PublicKey,
		userKey:    cfg.PrivateKey,
		env:        env,
	}, nil
}

// Environment returns the configured trading environment (demo or real).
func (c *Client) Environment() Environment {
	return c.env
}

// doJSON performs an authenticated JSON request against the Public API.
func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}

	url := publicAPIBaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("x-user-key", c.userKey)
	req.Header.Set("x-request-id", newRequestID())

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	payload, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg := strings.TrimSpace(string(payload))
		var errBody struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &errBody) == nil && errBody.Error != "" {
			msg = errBody.Error
		}
		return &APIError{StatusCode: res.StatusCode, Message: msg}
	}

	if out == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}
