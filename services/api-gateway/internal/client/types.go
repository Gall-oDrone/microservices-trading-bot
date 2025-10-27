package client

import (
	"encoding/json"
	"fmt"
	"time"
)

// ClientConfig holds HTTP client configuration
type ClientConfig struct {
	BaseURL            string
	Timeout            time.Duration
	MaxRetries         int
	RetryDelay         time.Duration
	MaxIdleConns       int
	IdleConnTimeout    time.Duration
	MaxConnsPerHost    int
	DisableKeepAlives  bool
	DisableCompression bool
}

// APIResponse represents a standard API response
type APIResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data,omitempty"`
	Meta    *Metadata       `json:"meta,omitempty"`
	Error   *ErrorDetail    `json:"error,omitempty"`
}

// Metadata represents response metadata
type Metadata struct {
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"request_id,omitempty"`
	Version   string    `json:"version,omitempty"`
}

// ErrorDetail represents error details in API response
type ErrorDetail struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// APIError represents a client API error
type APIError struct {
	StatusCode int
	Code       string
	Message    string
	Details    map[string]interface{}
	Err        error
}

// Error implements the error interface
func (e *APIError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("API error (status %d, code %s): %s - %v", e.StatusCode, e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("API error (status %d, code %s): %s", e.StatusCode, e.Code, e.Message)
}

// Unwrap returns the wrapped error
func (e *APIError) Unwrap() error {
	return e.Err
}

// IsRetryable returns true if the error is retryable
func (e *APIError) IsRetryable() bool {
	// Retry on 5xx errors and some 4xx errors
	return e.StatusCode >= 500 || e.StatusCode == 429 || e.StatusCode == 408
}

// NewAPIError creates a new API error
func NewAPIError(statusCode int, code, message string, err error) *APIError {
	return &APIError{
		StatusCode: statusCode,
		Code:       code,
		Message:    message,
		Err:        err,
	}
}

// HealthStatus represents service health status
type HealthStatus struct {
	Status    string                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Checks    map[string]interface{} `json:"checks,omitempty"`
	Version   string                 `json:"version,omitempty"`
	Service   string                 `json:"service,omitempty"`
}

// PaginationParams represents pagination parameters
type PaginationParams struct {
	Limit  int
	Offset int
}

// SortParams represents sorting parameters
type SortParams struct {
	SortBy    string
	SortOrder string // asc or desc
}

// TimeRangeParams represents time range parameters
type TimeRangeParams struct {
	From time.Time
	To   time.Time
}

// RequestOptions represents additional request options
type RequestOptions struct {
	Headers map[string]string
	Timeout time.Duration
}

