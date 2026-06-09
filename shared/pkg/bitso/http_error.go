package bitso

import (
	"errors"
	"fmt"
)

// HTTPError is returned when Bitso (or an upstream proxy) responds with a non-2xx HTTP status.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("http %d", e.StatusCode)
	}
	return fmt.Sprintf("http %d: %s", e.StatusCode, e.Body)
}

// IsRetryable reports whether the error is likely transient (proxy/upstream blip).
func IsRetryable(err error) bool {
	var he *HTTPError
	if errors.As(err, &he) {
		switch he.StatusCode {
		case 502, 503, 504, 429:
			return true
		default:
			return false
		}
	}
	return false
}
