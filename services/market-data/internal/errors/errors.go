package errors

import (
	"fmt"
	"time"
)

// ErrorType represents the type of error
type ErrorType string

const (
	// ErrorTypeValidation represents validation errors
	ErrorTypeValidation ErrorType = "validation"

	// ErrorTypeNetwork represents network errors
	ErrorTypeNetwork ErrorType = "network"

	// ErrorTypeTimeout represents timeout errors
	ErrorTypeTimeout ErrorType = "timeout"

	// ErrorTypeRateLimit represents rate limiting errors
	ErrorTypeRateLimit ErrorType = "rate_limit"

	// ErrorTypeAuthentication represents authentication errors
	ErrorTypeAuthentication ErrorType = "authentication"

	// ErrorTypeAuthorization represents authorization errors
	ErrorTypeAuthorization ErrorType = "authorization"

	// ErrorTypeNotFound represents not found errors
	ErrorTypeNotFound ErrorType = "not_found"

	// ErrorTypeConflict represents conflict errors
	ErrorTypeConflict ErrorType = "conflict"

	// ErrorTypeInternal represents internal server errors
	ErrorTypeInternal ErrorType = "internal"

	// ErrorTypeExternal represents external service errors
	ErrorTypeExternal ErrorType = "external"

	// ErrorTypeCache represents cache errors
	ErrorTypeCache ErrorType = "cache"

	// ErrorTypeStorage represents storage errors
	ErrorTypeStorage ErrorType = "storage"

	// ErrorTypeWebSocket represents WebSocket errors
	ErrorTypeWebSocket ErrorType = "websocket"

	// ErrorTypeKafka represents Kafka errors
	ErrorTypeKafka ErrorType = "kafka"
)

// Severity represents the severity level of an error
type Severity string

const (
	// SeverityLow represents low severity errors
	SeverityLow Severity = "low"

	// SeverityMedium represents medium severity errors
	SeverityMedium Severity = "medium"

	// SeverityHigh represents high severity errors
	SeverityHigh Severity = "high"

	// SeverityCritical represents critical severity errors
	SeverityCritical Severity = "critical"
)

// MarketDataError represents a market data specific error
type MarketDataError struct {
	Type      ErrorType              `json:"type"`
	Severity  Severity               `json:"severity"`
	Message   string                 `json:"message"`
	Code      string                 `json:"code"`
	Timestamp time.Time              `json:"timestamp"`
	Context   map[string]interface{} `json:"context,omitempty"`
	Cause     error                  `json:"cause,omitempty"`
}

// Error implements the error interface
func (e *MarketDataError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Type, e.Severity, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Type, e.Severity, e.Message)
}

// Unwrap returns the underlying error
func (e *MarketDataError) Unwrap() error {
	return e.Cause
}

// NewMarketDataError creates a new market data error
func NewMarketDataError(errorType ErrorType, severity Severity, message string) *MarketDataError {
	return &MarketDataError{
		Type:      errorType,
		Severity:  severity,
		Cause:     nil,
		Message:   message,
		Timestamp: time.Now(),
		Context:   make(map[string]interface{}),
	}
}

// NewMarketDataErrorWithCause creates a new market data error with a cause
func NewMarketDataErrorWithCause(errorType ErrorType, severity Severity, message string, cause error) *MarketDataError {
	return &MarketDataError{
		Type:      errorType,
		Severity:  severity,
		Cause:     cause,
		Message:   message,
		Timestamp: time.Now(),
		Context:   make(map[string]interface{}),
	}
}

// WithContext adds context to the error
func (e *MarketDataError) WithContext(key string, value interface{}) *MarketDataError {
	e.Context[key] = value
	return e
}

// WithCode adds a code to the error
func (e *MarketDataError) WithCode(code string) *MarketDataError {
	e.Code = code
	return e
}

// IsValidationError checks if the error is a validation error
func (e *MarketDataError) IsValidationError() bool {
	return e.Type == ErrorTypeValidation
}

// IsNetworkError checks if the error is a network error
func (e *MarketDataError) IsNetworkError() bool {
	return e.Type == ErrorTypeNetwork
}

// IsTimeoutError checks if the error is a timeout error
func (e *MarketDataError) IsTimeoutError() bool {
	return e.Type == ErrorTypeTimeout
}

// IsRateLimitError checks if the error is a rate limit error
func (e *MarketDataError) IsRateLimitError() bool {
	return e.Type == ErrorTypeRateLimit
}

// IsCriticalError checks if the error is critical
func (e *MarketDataError) IsCriticalError() bool {
	return e.Severity == SeverityCritical
}

// IsHighSeverityError checks if the error is high severity
func (e *MarketDataError) IsHighSeverityError() bool {
	return e.Severity == SeverityHigh || e.Severity == SeverityCritical
}

// ErrorHandler handles errors in the market data service
type ErrorHandler struct {
	errorChannel chan *MarketDataError
	logger       Logger
	metrics      Metrics
}

// Logger defines the interface for logging
type Logger interface {
	Error(msg string, args ...interface{})
	Warn(msg string, args ...interface{})
	Info(msg string, args ...interface{})
	Debug(msg string, args ...interface{})
}

// Metrics defines the interface for metrics
type Metrics interface {
	RecordError(errorType ErrorType, severity Severity)
	RecordErrorCount(errorType ErrorType, severity Severity, count int64)
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(logger Logger, metrics Metrics) *ErrorHandler {
	return &ErrorHandler{
		errorChannel: make(chan *MarketDataError, 1000),
		logger:       logger,
		metrics:      metrics,
	}
}

// HandleError handles an error
func (eh *ErrorHandler) HandleError(err error) {
	if err == nil {
		return
	}

	var marketDataErr *MarketDataError
	if marketDataError, ok := err.(*MarketDataError); ok {
		marketDataErr = marketDataError
	} else {
		// Wrap generic error
		marketDataErr = NewMarketDataErrorWithCause(
			ErrorTypeInternal,
			SeverityMedium,
			"Internal error",
			err,
		)
	}

	// Log error
	eh.logError(marketDataErr)

	// Record metrics
	if eh.metrics != nil {
		eh.metrics.RecordError(marketDataErr.Type, marketDataErr.Severity)
	}

	// Send to error channel
	select {
	case eh.errorChannel <- marketDataErr:
	default:
		// Channel full, log warning
		eh.logger.Warn("Error channel full, dropping error: %v", marketDataErr)
	}
}

// GetErrorChannel returns the error channel
func (eh *ErrorHandler) GetErrorChannel() <-chan *MarketDataError {
	return eh.errorChannel
}

// logError logs an error based on its severity
func (eh *ErrorHandler) logError(err *MarketDataError) {
	switch err.Severity {
	case SeverityCritical:
		eh.logger.Error("CRITICAL ERROR: %v", err)
	case SeverityHigh:
		eh.logger.Error("HIGH SEVERITY ERROR: %v", err)
	case SeverityMedium:
		eh.logger.Warn("MEDIUM SEVERITY ERROR: %v", err)
	case SeverityLow:
		eh.logger.Info("LOW SEVERITY ERROR: %v", err)
	default:
		eh.logger.Error("UNKNOWN SEVERITY ERROR: %v", err)
	}
}

// ErrorRecovery handles error recovery strategies
type ErrorRecovery struct {
	maxRetries    int
	retryDelay    time.Duration
	backoffFactor float64
	maxDelay      time.Duration
}

// NewErrorRecovery creates a new error recovery instance
func NewErrorRecovery(maxRetries int, retryDelay time.Duration, backoffFactor float64, maxDelay time.Duration) *ErrorRecovery {
	return &ErrorRecovery{
		maxRetries:    maxRetries,
		retryDelay:    retryDelay,
		backoffFactor: backoffFactor,
		maxDelay:      maxDelay,
	}
}

// RetryWithBackoff retries an operation with exponential backoff
func (er *ErrorRecovery) RetryWithBackoff(operation func() error) error {
	var lastErr error

	for attempt := 0; attempt < er.maxRetries; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't retry on certain error types
		if marketDataErr, ok := err.(*MarketDataError); ok {
			if marketDataErr.IsCriticalError() || marketDataErr.IsRateLimitError() {
				return err
			}
		}

		// Calculate delay
		delay := er.calculateDelay(attempt)

		// Wait before retry
		time.Sleep(delay)
	}

	return lastErr
}

// calculateDelay calculates the delay for the given attempt
func (er *ErrorRecovery) calculateDelay(attempt int) time.Duration {
	delay := time.Duration(float64(er.retryDelay) * float64(attempt) * er.backoffFactor)
	if delay > er.maxDelay {
		delay = er.maxDelay
	}
	return delay
}

// ErrorValidator validates errors
type ErrorValidator struct {
	allowedErrorTypes map[ErrorType]bool
	maxErrorRate      float64
	errorWindow       time.Duration
}

// NewErrorValidator creates a new error validator
func NewErrorValidator(allowedErrorTypes []ErrorType, maxErrorRate float64, errorWindow time.Duration) *ErrorValidator {
	allowedTypes := make(map[ErrorType]bool)
	for _, errorType := range allowedErrorTypes {
		allowedTypes[errorType] = true
	}

	return &ErrorValidator{
		allowedErrorTypes: allowedTypes,
		maxErrorRate:      maxErrorRate,
		errorWindow:       errorWindow,
	}
}

// IsErrorAllowed checks if an error type is allowed
func (ev *ErrorValidator) IsErrorAllowed(errorType ErrorType) bool {
	return ev.allowedErrorTypes[errorType]
}

// ValidateErrorRate validates the error rate
func (ev *ErrorValidator) ValidateErrorRate(errorCount int64, totalCount int64) bool {
	if totalCount == 0 {
		return true
	}

	errorRate := float64(errorCount) / float64(totalCount)
	return errorRate <= ev.maxErrorRate
}

// ErrorContext provides context for errors
type ErrorContext struct {
	Service   string                 `json:"service"`
	Component string                 `json:"component"`
	Operation string                 `json:"operation"`
	RequestID string                 `json:"request_id,omitempty"`
	UserID    string                 `json:"user_id,omitempty"`
	Book      string                 `json:"book,omitempty"`
	TradeID   uint64                 `json:"trade_id,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// NewErrorContext creates a new error context
func NewErrorContext(service, component, operation string) *ErrorContext {
	return &ErrorContext{
		Service:   service,
		Component: component,
		Operation: operation,
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
}

// WithRequestID adds a request ID to the context
func (ec *ErrorContext) WithRequestID(requestID string) *ErrorContext {
	ec.RequestID = requestID
	return ec
}

// WithUserID adds a user ID to the context
func (ec *ErrorContext) WithUserID(userID string) *ErrorContext {
	ec.UserID = userID
	return ec
}

// WithBook adds a book to the context
func (ec *ErrorContext) WithBook(book string) *ErrorContext {
	ec.Book = book
	return ec
}

// WithTradeID adds a trade ID to the context
func (ec *ErrorContext) WithTradeID(tradeID uint64) *ErrorContext {
	ec.TradeID = tradeID
	return ec
}

// WithMetadata adds metadata to the context
func (ec *ErrorContext) WithMetadata(key string, value interface{}) *ErrorContext {
	ec.Metadata[key] = value
	return ec
}

// CreateErrorWithContext creates an error with context
func CreateErrorWithContext(errorType ErrorType, severity Severity, message string, context *ErrorContext) *MarketDataError {
	err := NewMarketDataError(errorType, severity, message)

	if context != nil {
		err.WithContext("service", context.Service)
		err.WithContext("component", context.Component)
		err.WithContext("operation", context.Operation)
		err.WithContext("timestamp", context.Timestamp)

		if context.RequestID != "" {
			err.WithContext("request_id", context.RequestID)
		}
		if context.UserID != "" {
			err.WithContext("user_id", context.UserID)
		}
		if context.Book != "" {
			err.WithContext("book", context.Book)
		}
		if context.TradeID != 0 {
			err.WithContext("trade_id", context.TradeID)
		}

		for key, value := range context.Metadata {
			err.WithContext(key, value)
		}
	}

	return err
}

// Common error constructors
func NewValidationError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeValidation, SeverityMedium, message, context)
}

func NewNetworkError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeNetwork, SeverityHigh, message, context)
}

func NewTimeoutError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeTimeout, SeverityMedium, message, context)
}

func NewRateLimitError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeRateLimit, SeverityMedium, message, context)
}

func NewAuthenticationError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeAuthentication, SeverityHigh, message, context)
}

func NewAuthorizationError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeAuthorization, SeverityHigh, message, context)
}

func NewNotFoundError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeNotFound, SeverityLow, message, context)
}

func NewConflictError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeConflict, SeverityMedium, message, context)
}

func NewInternalError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeInternal, SeverityHigh, message, context)
}

func NewExternalError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeExternal, SeverityMedium, message, context)
}

func NewCacheError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeCache, SeverityMedium, message, context)
}

func NewStorageError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeStorage, SeverityHigh, message, context)
}

func NewWebSocketError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeWebSocket, SeverityHigh, message, context)
}

func NewKafkaError(message string, context *ErrorContext) *MarketDataError {
	return CreateErrorWithContext(ErrorTypeKafka, SeverityHigh, message, context)
}
