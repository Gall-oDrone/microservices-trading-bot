package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"time"
)

// LogLevel represents the log level
type LogLevel int

const (
	// LogLevelDebug represents debug level
	LogLevelDebug LogLevel = iota
	// LogLevelInfo represents info level
	LogLevelInfo
	// LogLevelWarn represents warn level
	LogLevelWarn
	// LogLevelError represents error level
	LogLevelError
	// LogLevelFatal represents fatal level
	LogLevelFatal
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	case LogLevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// ParseLogLevel parses a log level from string
func ParseLogLevel(level string) LogLevel {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return LogLevelDebug
	case "INFO":
		return LogLevelInfo
	case "WARN":
		return LogLevelWarn
	case "ERROR":
		return LogLevelError
	case "FATAL":
		return LogLevelFatal
	default:
		return LogLevelInfo
	}
}

// LogEntry represents a log entry
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     LogLevel               `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Caller    string                 `json:"caller,omitempty"`
	TraceID   string                 `json:"trace_id,omitempty"`
	SpanID    string                 `json:"span_id,omitempty"`
}

// Logger defines the logger interface
type Logger interface {
	Debug(msg string, fields ...map[string]interface{})
	Info(msg string, fields ...map[string]interface{})
	Warn(msg string, fields ...map[string]interface{})
	Error(msg string, fields ...map[string]interface{})
	Fatal(msg string, fields ...map[string]interface{})
	WithFields(fields map[string]interface{}) Logger
	WithContext(ctx context.Context) Logger
}

// StructuredLogger implements the Logger interface
type StructuredLogger struct {
	level  LogLevel
	output io.Writer
	fields map[string]interface{}
}

// NewStructuredLogger creates a new structured logger
func NewStructuredLogger(level LogLevel, output io.Writer) *StructuredLogger {
	if output == nil {
		output = os.Stdout
	}

	return &StructuredLogger{
		level:  level,
		output: output,
		fields: make(map[string]interface{}),
	}
}

// Debug logs a debug message
func (l *StructuredLogger) Debug(msg string, fields ...map[string]interface{}) {
	l.log(LogLevelDebug, msg, fields...)
}

// Info logs an info message
func (l *StructuredLogger) Info(msg string, fields ...map[string]interface{}) {
	l.log(LogLevelInfo, msg, fields...)
}

// Warn logs a warn message
func (l *StructuredLogger) Warn(msg string, fields ...map[string]interface{}) {
	l.log(LogLevelWarn, msg, fields...)
}

// Error logs an error message
func (l *StructuredLogger) Error(msg string, fields ...map[string]interface{}) {
	l.log(LogLevelError, msg, fields...)
}

// Fatal logs a fatal message and exits
func (l *StructuredLogger) Fatal(msg string, fields ...map[string]interface{}) {
	l.log(LogLevelFatal, msg, fields...)
	os.Exit(1)
}

// WithFields returns a new logger with additional fields
func (l *StructuredLogger) WithFields(fields map[string]interface{}) Logger {
	newFields := make(map[string]interface{})

	// Copy existing fields
	for k, v := range l.fields {
		newFields[k] = v
	}

	// Add new fields
	for k, v := range fields {
		newFields[k] = v
	}

	return &StructuredLogger{
		level:  l.level,
		output: l.output,
		fields: newFields,
	}
}

// WithContext returns a new logger with context fields
func (l *StructuredLogger) WithContext(ctx context.Context) Logger {
	fields := make(map[string]interface{})

	// Extract trace ID from context
	if traceID := ctx.Value("trace_id"); traceID != nil {
		fields["trace_id"] = traceID
	}

	// Extract span ID from context
	if spanID := ctx.Value("span_id"); spanID != nil {
		fields["span_id"] = spanID
	}

	// Extract user ID from context
	if userID := ctx.Value("user_id"); userID != nil {
		fields["user_id"] = userID
	}

	// Extract request ID from context
	if requestID := ctx.Value("request_id"); requestID != nil {
		fields["request_id"] = requestID
	}

	return l.WithFields(fields)
}

// log logs a message with the specified level
func (l *StructuredLogger) log(level LogLevel, msg string, fields ...map[string]interface{}) {
	if level < l.level {
		return
	}

	// Get caller information
	_, file, line, ok := runtime.Caller(2)
	caller := ""
	if ok {
		parts := strings.Split(file, "/")
		caller = fmt.Sprintf("%s:%d", parts[len(parts)-1], line)
	}

	// Create log entry
	entry := LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Message:   msg,
		Caller:    caller,
		Fields:    make(map[string]interface{}),
	}

	// Copy logger fields
	for k, v := range l.fields {
		entry.Fields[k] = v
	}

	// Add additional fields
	for _, fieldMap := range fields {
		for k, v := range fieldMap {
			entry.Fields[k] = v
		}
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(entry)
	if err != nil {
		// Fallback to simple format
		fmt.Fprintf(l.output, "%s [%s] %s\n", entry.Timestamp.Format(time.RFC3339), level.String(), msg)
		return
	}

	// Write to output
	fmt.Fprintln(l.output, string(jsonData))
}

// SimpleLogger implements a simple logger for backward compatibility
type SimpleLogger struct {
	*log.Logger
	level LogLevel
}

// NewSimpleLogger creates a new simple logger
func NewSimpleLogger(level LogLevel, prefix string) *SimpleLogger {
	return &SimpleLogger{
		Logger: log.New(os.Stdout, prefix, log.LstdFlags|log.Lshortfile),
		level:  level,
	}
}

// Debug logs a debug message
func (l *SimpleLogger) Debug(msg string, fields ...map[string]interface{}) {
	if LogLevelDebug >= l.level {
		l.Logger.Printf("[DEBUG] %s", msg)
	}
}

// Info logs an info message
func (l *SimpleLogger) Info(msg string, fields ...map[string]interface{}) {
	if LogLevelInfo >= l.level {
		l.Logger.Printf("[INFO] %s", msg)
	}
}

// Warn logs a warn message
func (l *SimpleLogger) Warn(msg string, fields ...map[string]interface{}) {
	if LogLevelWarn >= l.level {
		l.Logger.Printf("[WARN] %s", msg)
	}
}

// Error logs an error message
func (l *SimpleLogger) Error(msg string, fields ...map[string]interface{}) {
	if LogLevelError >= l.level {
		l.Logger.Printf("[ERROR] %s", msg)
	}
}

// Fatal logs a fatal message and exits
func (l *SimpleLogger) Fatal(msg string, fields ...map[string]interface{}) {
	if LogLevelFatal >= l.level {
		l.Logger.Printf("[FATAL] %s", msg)
		os.Exit(1)
	}
}

// WithFields returns a new logger with additional fields
func (l *SimpleLogger) WithFields(fields map[string]interface{}) Logger {
	// For simple logger, we just return the same instance
	return l
}

// WithContext returns a new logger with context fields
func (l *SimpleLogger) WithContext(ctx context.Context) Logger {
	// For simple logger, we just return the same instance
	return l
}

// DefaultLogger returns the default logger
func DefaultLogger() Logger {
	level := ParseLogLevel(os.Getenv("LOG_LEVEL"))
	return NewStructuredLogger(level, os.Stdout)
}
