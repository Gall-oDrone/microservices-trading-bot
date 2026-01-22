package api

import (
	"encoding/json"
	"net/http"
	"time"

	"bitso-trading-platform/api-gateway/internal/middleware"
)

// APIResponse represents a standard API response
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Meta    *Metadata   `json:"meta,omitempty"`
	Error   *ErrorInfo  `json:"error,omitempty"`
}

// Metadata represents response metadata
type Metadata struct {
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"request_id,omitempty"`
	Version   string    `json:"version"`
}

// ErrorInfo represents error information
type ErrorInfo struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details,omitempty"`
}

// SuccessResponse writes a successful JSON response
func SuccessResponse(w http.ResponseWriter, r *http.Request, data interface{}) {
	response := APIResponse{
		Success: true,
		Data:    data,
		Meta: &Metadata{
			Timestamp: time.Now().UTC(),
			RequestID: middleware.RequestIDFromContext(r.Context()),
			Version:   "v1",
		},
	}

	writeJSONResponse(w, http.StatusOK, response)
}

// SuccessResponseWithStatus writes a successful JSON response with custom status
func SuccessResponseWithStatus(w http.ResponseWriter, r *http.Request, statusCode int, data interface{}) {
	response := APIResponse{
		Success: true,
		Data:    data,
		Meta: &Metadata{
			Timestamp: time.Now().UTC(),
			RequestID: middleware.RequestIDFromContext(r.Context()),
			Version:   "v1",
		},
	}

	writeJSONResponse(w, statusCode, response)
}

// ErrorResponse writes an error JSON response
func ErrorResponse(w http.ResponseWriter, r *http.Request, statusCode int, code, message string) {
	response := APIResponse{
		Success: false,
		Meta: &Metadata{
			Timestamp: time.Now().UTC(),
			RequestID: middleware.RequestIDFromContext(r.Context()),
			Version:   "v1",
		},
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
		},
	}

	writeJSONResponse(w, statusCode, response)
}

// ErrorResponseWithDetails writes an error JSON response with details
func ErrorResponseWithDetails(w http.ResponseWriter, r *http.Request, statusCode int, code, message string, details map[string]interface{}) {
	response := APIResponse{
		Success: false,
		Meta: &Metadata{
			Timestamp: time.Now().UTC(),
			RequestID: middleware.RequestIDFromContext(r.Context()),
			Version:   "v1",
		},
		Error: &ErrorInfo{
			Code:    code,
			Message: message,
			Details: details,
		},
	}

	writeJSONResponse(w, statusCode, response)
}

// InternalErrorResponse writes a 500 Internal Server Error response
func InternalErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	message := "An internal server error occurred"
	if err != nil {
		// Don't expose internal error details to clients
		// Log the actual error separately
		message = "Internal server error"
	}

	ErrorResponse(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", message)
}

// BadRequestResponse writes a 400 Bad Request response
func BadRequestResponse(w http.ResponseWriter, r *http.Request, message string) {
	ErrorResponse(w, r, http.StatusBadRequest, "BAD_REQUEST", message)
}

// NotFoundResponse writes a 404 Not Found response
func NotFoundResponse(w http.ResponseWriter, r *http.Request, message string) {
	ErrorResponse(w, r, http.StatusNotFound, "NOT_FOUND", message)
}

// UnauthorizedResponse writes a 401 Unauthorized response
func UnauthorizedResponse(w http.ResponseWriter, r *http.Request) {
	ErrorResponse(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
}

// ForbiddenResponse writes a 403 Forbidden response
func ForbiddenResponse(w http.ResponseWriter, r *http.Request, message string) {
	ErrorResponse(w, r, http.StatusForbidden, "FORBIDDEN", message)
}

// ServiceUnavailableResponse writes a 503 Service Unavailable response
func ServiceUnavailableResponse(w http.ResponseWriter, r *http.Request, service string) {
	message := "Service temporarily unavailable"
	if service != "" {
		message = service + " service is temporarily unavailable"
	}
	ErrorResponse(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", message)
}

// GatewayTimeoutResponse writes a 504 Gateway Timeout response
func GatewayTimeoutResponse(w http.ResponseWriter, r *http.Request) {
	ErrorResponse(w, r, http.StatusGatewayTimeout, "GATEWAY_TIMEOUT", "Request timeout exceeded")
}

// TooManyRequestsResponse writes a 429 Too Many Requests response
func TooManyRequestsResponse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", "60")
	ErrorResponse(w, r, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "Too many requests. Please try again later.")
}

// MethodNotAllowedResponse writes a 405 Method Not Allowed response
func MethodNotAllowedResponse(w http.ResponseWriter, r *http.Request) {
	ErrorResponse(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "HTTP method not allowed")
}

// writeJSONResponse writes a JSON response with proper headers
func writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)

	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			// If encoding fails, there's not much we can do at this point
			// The headers have already been sent
			return
		}
	}
}

// ProxyResponse forwards a response from a backend service
func ProxyResponse(w http.ResponseWriter, r *http.Request, data interface{}, err error) {
	if err != nil {
		// Handle different error types
		InternalErrorResponse(w, r, err)
		return
	}

	SuccessResponse(w, r, data)
}
