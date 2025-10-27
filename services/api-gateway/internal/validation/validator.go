package validation

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"bitso-trading-platform/api-gateway/internal/logger"
)

// Validator handles request validation
type Validator struct {
	logger *logger.Logger
}

// NewValidator creates a new validator
func NewValidator(logger *logger.Logger) *Validator {
	return &Validator{
		logger: logger.WithComponent("validator"),
	}
}

// ValidateQueryParams validates query parameters
func (v *Validator) ValidateQueryParams(r *http.Request, rules map[string]ValidationRule) error {
	query := r.URL.Query()

	for param, rule := range rules {
		value := query.Get(param)

		// Check if required
		if rule.Required && value == "" {
			return fmt.Errorf("parameter '%s' is required", param)
		}

		// Skip validation if optional and not provided
		if !rule.Required && value == "" {
			continue
		}

		// Run custom validator if provided
		if rule.Validator != nil {
			if err := rule.Validator(value); err != nil {
				return fmt.Errorf("validation failed for parameter '%s': %w", param, err)
			}
		}
	}

	return nil
}

// ValidateJSON validates and parses JSON request body
func (v *Validator) ValidateJSON(r *http.Request, target interface{}) error {
	// Check content type
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" && contentType != "application/json; charset=utf-8" {
		return fmt.Errorf("invalid content type: %s (expected application/json)", contentType)
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}
	defer r.Body.Close()

	// Check if body is empty
	if len(body) == 0 {
		return fmt.Errorf("request body is empty")
	}

	// Parse JSON
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	return nil
}

// ValidatePathParam validates a path parameter
func (v *Validator) ValidatePathParam(param string, rule ValidationRule) error {
	// Check if required
	if rule.Required && param == "" {
		return fmt.Errorf("path parameter is required")
	}

	// Run custom validator if provided
	if rule.Validator != nil {
		if err := rule.Validator(param); err != nil {
			return fmt.Errorf("validation failed: %w", err)
		}
	}

	return nil
}

// ValidationRule defines a validation rule
type ValidationRule struct {
	Required  bool
	Validator func(value string) error
}

// Log logs validation errors
func (v *Validator) Log(msg string, fields map[string]interface{}) {
	v.logger.Warn(msg, fields)
}

