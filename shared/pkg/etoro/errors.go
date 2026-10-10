package etoro

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// APIError is a non-2xx response from the Public API.
type APIError struct {
	StatusCode int
	// Message is the most specific text the body carried (ProblemDetails
	// detail/title, errorMessage, error) or the raw body.
	Message string
	// Code is eToro's numeric errorCode when the body carried one (e.g. 623
	// user-level trade block, 631/632 position already closed/closing).
	Code      int
	Method    string
	Path      string
	RequestID string
	// RetryAfter comes from Retry-After (or RateLimit-Reset on a 429).
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "etoro api error (status %d", e.StatusCode)
	if e.Code != 0 {
		fmt.Fprintf(&b, ", code %d", e.Code)
	}
	b.WriteString(")")
	if e.Method != "" {
		fmt.Fprintf(&b, " %s %s", e.Method, e.Path)
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	return b.String()
}

// TransportError is a request that got no HTTP response (DNS, TLS, reset,
// timeout). For a write the outcome is unknown: resolve it by reference.
type TransportError struct {
	Method, Path, RequestID string
	Err                     error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("etoro %s %s (request %s): %v", e.Method, e.Path, e.RequestID, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

func newAPIError(status int, h http.Header, payload []byte, method, path, rid string) *APIError {
	e := &APIError{StatusCode: status, Method: method, Path: path, RequestID: rid}
	var body struct {
		Error        string          `json:"error"`
		Title        string          `json:"title"`
		Detail       string          `json:"detail"`
		ErrorMessage string          `json:"errorMessage"`
		Message      string          `json:"message"`
		ErrorCode    json.RawMessage `json:"errorCode"`
	}
	if json.Unmarshal(payload, &body) == nil {
		for _, m := range []string{body.Detail, body.ErrorMessage, body.Message, body.Error, body.Title} {
			if strings.TrimSpace(m) != "" {
				e.Message = strings.TrimSpace(m)
				break
			}
		}
		if len(body.ErrorCode) > 0 {
			raw := strings.Trim(string(body.ErrorCode), `"`)
			e.Code, _ = strconv.Atoi(raw)
		}
	}
	if e.Message == "" {
		msg := strings.TrimSpace(string(payload))
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		e.Message = msg
	}
	if s := strings.TrimSpace(h.Get("Retry-After")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			e.RetryAfter = time.Duration(n) * time.Second
		} else if t, err := http.ParseTime(s); err == nil {
			e.RetryAfter = time.Until(t)
		}
	}
	if e.RetryAfter <= 0 && status == http.StatusTooManyRequests {
		if n, err := strconv.Atoi(strings.TrimSpace(h.Get("RateLimit-Reset"))); err == nil && n > 0 {
			e.RetryAfter = time.Duration(n) * time.Second
		}
	}
	return e
}

func asAPIError(err error) (*APIError, bool) {
	var ae *APIError
	ok := errors.As(err, &ae)
	return ae, ok
}

// StatusCode returns the HTTP status of an APIError, or 0.
func StatusCode(err error) int {
	if ae, ok := asAPIError(err); ok {
		return ae.StatusCode
	}
	return 0
}

// IsNotFound reports a 404.
func IsNotFound(err error) bool { return StatusCode(err) == http.StatusNotFound }

// IsDuplicateReference reports eToro's rejection of an open order whose
// x-request-id was already used by an earlier order ("Validation failed:
// ReferenceID <id> may already exists for CID <cid> and OrderID <n>", HTTP
// 400, seen on demo 2026-10-10). It means the earlier order with that
// reference LANDED: never treat it as a plain rejection.
func IsDuplicateReference(err error) bool {
	ae, ok := asAPIError(err)
	if !ok || ae.StatusCode != http.StatusBadRequest {
		return false
	}
	m := strings.ToLower(ae.Message)
	return strings.Contains(m, "referenceid") && strings.Contains(m, "already exist")
}

// IsInsufficientPermissions reports whether the API rejected the call for
// environment mismatch or a missing scope (403 InsufficientPermissions).
func IsInsufficientPermissions(err error) bool {
	ae, ok := asAPIError(err)
	return ok && ae.StatusCode == http.StatusForbidden &&
		strings.Contains(strings.ToLower(ae.Message), "insufficientpermissions")
}

// IsRetryable reports whether err is transient: rate limited (429), a 5xx,
// or a transport error (no API response at all). 4xx validation, auth and
// permission errors are not retryable. A retryable error on a WRITE still
// must not be blindly re-sent: resolve the order by reference first.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if ae, ok := asAPIError(err); ok {
		return ae.StatusCode == http.StatusTooManyRequests || ae.StatusCode >= 500
	}
	var te *TransportError
	if errors.As(err, &te) {
		return true
	}
	return false
}
