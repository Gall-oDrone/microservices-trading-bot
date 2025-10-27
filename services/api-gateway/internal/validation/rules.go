package validation

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var (
	// bookRegex matches valid book format (e.g., btc_mxn)
	bookRegex = regexp.MustCompile(`^[a-z]{3,10}_[a-z]{3,10}$`)
	
	// orderIDRegex matches valid order ID format
	orderIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	
	// strategyNameRegex matches valid strategy names
	strategyNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// ValidateBook validates a trading book parameter
func ValidateBook(book string) error {
	if book == "" {
		return fmt.Errorf("book is required")
	}

	if !bookRegex.MatchString(book) {
		return fmt.Errorf("invalid book format: %s (expected format: major_minor, e.g., btc_mxn)", book)
	}

	return nil
}

// ValidateLimit validates a limit parameter
func ValidateLimit(limit int) error {
	if limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}

	if limit > 10000 {
		return fmt.Errorf("limit must not exceed 10000")
	}

	return nil
}

// ValidateOffset validates an offset parameter
func ValidateOffset(offset int) error {
	if offset < 0 {
		return fmt.Errorf("offset must be non-negative")
	}

	return nil
}

// ValidateOrderID validates an order ID
func ValidateOrderID(orderID string) error {
	if orderID == "" {
		return fmt.Errorf("order ID is required")
	}

	if !orderIDRegex.MatchString(orderID) {
		return fmt.Errorf("invalid order ID format: %s", orderID)
	}

	if len(orderID) > 100 {
		return fmt.Errorf("order ID too long (max 100 characters)")
	}

	return nil
}

// ValidateTradeID validates a trade ID
func ValidateTradeID(tradeID uint64) error {
	if tradeID == 0 {
		return fmt.Errorf("trade ID must be positive")
	}

	return nil
}

// ValidateStrategyName validates a strategy name
func ValidateStrategyName(name string) error {
	if name == "" {
		return fmt.Errorf("strategy name is required")
	}

	if !strategyNameRegex.MatchString(name) {
		return fmt.Errorf("invalid strategy name format: %s", name)
	}

	if len(name) > 50 {
		return fmt.Errorf("strategy name too long (max 50 characters)")
	}

	return nil
}

// ValidateTimeRange validates a time range
func ValidateTimeRange(start, end time.Time) error {
	if !start.IsZero() && !end.IsZero() {
		if start.After(end) {
			return fmt.Errorf("start time must be before end time")
		}

		// Check if range is too large (e.g., more than 1 year)
		if end.Sub(start) > 365*24*time.Hour {
			return fmt.Errorf("time range too large (max 1 year)")
		}
	}

	return nil
}

// ValidateStatus validates an order or position status
func ValidateStatus(status string) error {
	if status == "" {
		return nil // Status is optional
	}

	validStatuses := map[string]bool{
		"pending":          true,
		"validated":        true,
		"submitted":        true,
		"accepted":         true,
		"partially_filled": true,
		"filled":           true,
		"cancelled":        true,
		"rejected":         true,
		"open":             true,
		"closed":           true,
	}

	if !validStatuses[status] {
		return fmt.Errorf("invalid status: %s", status)
	}

	return nil
}

// ValidateSide validates an order side
func ValidateSide(side string) error {
	if side == "" {
		return nil // Side is optional
	}

	validSides := map[string]bool{
		"buy":  true,
		"sell": true,
	}

	if !validSides[side] {
		return fmt.Errorf("invalid side: %s (must be 'buy' or 'sell')", side)
	}

	return nil
}

// ValidateSortOrder validates sort order
func ValidateSortOrder(order string) error {
	if order == "" {
		return nil // Optional
	}

	validOrders := map[string]bool{
		"asc":  true,
		"desc": true,
	}

	if !validOrders[order] {
		return fmt.Errorf("invalid sort order: %s (must be 'asc' or 'desc')", order)
	}

	return nil
}

// ParseIntParam parses an integer parameter from string
func ParseIntParam(value string, defaultValue int) (int, error) {
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue, fmt.Errorf("invalid integer value: %s", value)
	}

	return parsed, nil
}

// ParseUint64Param parses a uint64 parameter from string
func ParseUint64Param(value string) (uint64, error) {
	if value == "" {
		return 0, fmt.Errorf("value is required")
	}

	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid uint64 value: %s", value)
	}

	return parsed, nil
}

// ParseTimeParam parses a time parameter from string (RFC3339 format)
func ParseTimeParam(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time format: %s (expected RFC3339)", value)
	}

	return parsed, nil
}

