package etoro

import "fmt"

// APIError represents a non-success response from the eToro Public API.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("etoro api error (status %d): %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("etoro api error (status %d)", e.StatusCode)
}

// IsInsufficientPermissions reports whether the API rejected the call for environment mismatch.
func IsInsufficientPermissions(err error) bool {
	if err == nil {
		return false
	}
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == 403 && containsIgnoreCase(apiErr.Message, "InsufficientPermissions")
	}
	return false
}

func containsIgnoreCase(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && stringContainsFold(s, sub)))
}

func stringContainsFold(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
