package models

import (
	"fmt"
	"strings"
)

// ValidationError represents a single validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// Error implements the error interface
func (ve *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s (code: %s)", ve.Field, ve.Message, ve.Code)
}

// ValidationResult holds the result of a validation operation
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

// NewValidationResult creates a new validation result
func NewValidationResult() *ValidationResult {
	return &ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
	}
}

// AddError adds a validation error
func (vr *ValidationResult) AddError(field, message, code string) {
	vr.Valid = false
	vr.Errors = append(vr.Errors, ValidationError{
		Field:   field,
		Message: message,
		Code:    code,
	})
}

// AddFieldError adds a validation error for a field
func (vr *ValidationResult) AddFieldError(field, message string) {
	vr.AddError(field, message, "INVALID_FIELD")
}

// AddRangeError adds a range validation error
func (vr *ValidationResult) AddRangeError(field string, value, min, max float64) {
	message := fmt.Sprintf("Value %f is out of range [%f, %f]", value, min, max)
	vr.AddError(field, message, "OUT_OF_RANGE")
}

// AddRequiredError adds a required field error
func (vr *ValidationResult) AddRequiredError(field string) {
	vr.AddError(field, fmt.Sprintf("%s is required", field), "REQUIRED")
}

// HasErrors returns true if there are any errors
func (vr *ValidationResult) HasErrors() bool {
	return !vr.Valid
}

// Error returns a formatted error message with all validation errors
func (vr *ValidationResult) Error() string {
	if vr.Valid {
		return ""
	}

	var messages []string
	for _, err := range vr.Errors {
		messages = append(messages, err.Error())
	}

	return fmt.Sprintf("Validation failed: %s", strings.Join(messages, "; "))
}

// GetErrorCodes returns all error codes
func (vr *ValidationResult) GetErrorCodes() []string {
	codes := make([]string, 0, len(vr.Errors))
	for _, err := range vr.Errors {
		codes = append(codes, err.Code)
	}
	return codes
}

// GetFieldErrors returns errors for a specific field
func (vr *ValidationResult) GetFieldErrors(field string) []ValidationError {
	var fieldErrors []ValidationError
	for _, err := range vr.Errors {
		if err.Field == field {
			fieldErrors = append(fieldErrors, err)
		}
	}
	return fieldErrors
}

// Merge merges another validation result into this one
func (vr *ValidationResult) Merge(other *ValidationResult) {
	if other == nil || other.Valid {
		return
	}

	vr.Valid = false
	vr.Errors = append(vr.Errors, other.Errors...)
}

// ValidationErrorCode defines common validation error codes
const (
	ErrorCodeRequired      = "REQUIRED"
	ErrorCodeInvalid       = "INVALID"
	ErrorCodeOutOfRange    = "OUT_OF_RANGE"
	ErrorCodeTooLarge      = "TOO_LARGE"
	ErrorCodeTooSmall      = "TOO_SMALL"
	ErrorCodeDuplicate     = "DUPLICATE"
	ErrorCodeNotFound      = "NOT_FOUND"
	ErrorCodeInvalidFormat = "INVALID_FORMAT"
	ErrorCodeInvalidType   = "INVALID_TYPE"
)

// RiskViolation represents a risk management violation
type RiskViolation struct {
	Rule         string  `json:"rule"`
	Description  string  `json:"description"`
	CurrentValue float64 `json:"current_value"`
	Limit        float64 `json:"limit"`
	Severity     string  `json:"severity"` // "warning", "error", "critical"
}

// Error implements the error interface
func (rv *RiskViolation) Error() string {
	return fmt.Sprintf("Risk violation [%s]: %s (current: %f, limit: %f, severity: %s)",
		rv.Rule, rv.Description, rv.CurrentValue, rv.Limit, rv.Severity)
}

// RiskCheckResult holds the result of risk checks
type RiskCheckResult struct {
	Passed     bool            `json:"passed"`
	Violations []RiskViolation `json:"violations,omitempty"`
}

// NewRiskCheckResult creates a new risk check result
func NewRiskCheckResult() *RiskCheckResult {
	return &RiskCheckResult{
		Passed:     true,
		Violations: make([]RiskViolation, 0),
	}
}

// AddViolation adds a risk violation
func (rcr *RiskCheckResult) AddViolation(rule, description string, currentValue, limit float64, severity string) {
	rcr.Passed = false
	rcr.Violations = append(rcr.Violations, RiskViolation{
		Rule:         rule,
		Description:  description,
		CurrentValue: currentValue,
		Limit:        limit,
		Severity:     severity,
	})
}

// AddWarning adds a warning-level violation
func (rcr *RiskCheckResult) AddWarning(rule, description string, currentValue, limit float64) {
	rcr.AddViolation(rule, description, currentValue, limit, "warning")
}

// AddError adds an error-level violation
func (rcr *RiskCheckResult) AddError(rule, description string, currentValue, limit float64) {
	rcr.AddViolation(rule, description, currentValue, limit, "error")
}

// AddCritical adds a critical-level violation
func (rcr *RiskCheckResult) AddCritical(rule, description string, currentValue, limit float64) {
	rcr.AddViolation(rule, description, currentValue, limit, "critical")
}

// HasViolations returns true if there are any violations
func (rcr *RiskCheckResult) HasViolations() bool {
	return !rcr.Passed
}

// HasCriticalViolations returns true if there are any critical violations
func (rcr *RiskCheckResult) HasCriticalViolations() bool {
	for _, v := range rcr.Violations {
		if v.Severity == "critical" {
			return true
		}
	}
	return false
}

// Error returns a formatted error message with all violations
func (rcr *RiskCheckResult) Error() string {
	if rcr.Passed {
		return ""
	}

	var messages []string
	for _, v := range rcr.Violations {
		messages = append(messages, v.Error())
	}

	return fmt.Sprintf("Risk check failed: %s", strings.Join(messages, "; "))
}

// Merge merges another risk check result into this one
func (rcr *RiskCheckResult) Merge(other *RiskCheckResult) {
	if other == nil || other.Passed {
		return
	}

	rcr.Passed = false
	rcr.Violations = append(rcr.Violations, other.Violations...)
}
