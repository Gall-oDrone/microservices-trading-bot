package api

import (
	"encoding/json"
	"net/http"
)

// APIResponse represents a standard API response
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
}

// APIError represents an API error
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// SendJSON sends a JSON response
func SendJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	response := APIResponse{
		Success: statusCode >= 200 && statusCode < 300,
		Data:    data,
	}
	
	json.NewEncoder(w).Encode(response)
}

// SendError sends an error response
func SendError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	response := APIResponse{
		Success: false,
		Error: &APIError{
			Code:    code,
			Message: message,
		},
	}
	
	json.NewEncoder(w).Encode(response)
}

// SendSuccess sends a success response with data
func SendSuccess(w http.ResponseWriter, data interface{}) {
	SendJSON(w, http.StatusOK, data)
}

// ParseRequest parses a JSON request body
func ParseRequest(r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return err
	}
	defer r.Body.Close()
	return nil
}

