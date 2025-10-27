package logger

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Logger interface defines the logging operations
type Logger interface {
	Debug(msg string, fields map[string]interface{})
	Info(msg string, fields map[string]interface{})
	Warn(msg string, fields map[string]interface{})
	Error(msg string, fields map[string]interface{})
	Fatal(msg string, fields map[string]interface{})
	With(fields map[string]interface{}) Logger
}

// Config holds logger configuration
type Config struct {
	Level  string // trace, debug, info, warn, error, fatal
	Format string // json, console
	Output string // stdout, stderr, or file path
}

// ZerologLogger implements Logger interface using zerolog
type ZerologLogger struct {
	logger zerolog.Logger
}

// New creates a new logger with the given configuration
func New(cfg *Config) *ZerologLogger {
	// Parse log level
	level, err := zerolog.ParseLevel(strings.ToLower(cfg.Level))
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)
	
	// Configure output writer
	var output io.Writer
	switch strings.ToLower(cfg.Output) {
	case "stderr":
		output = os.Stderr
	case "stdout", "":
		output = os.Stdout
	default:
		// File output
		file, err := os.OpenFile(cfg.Output, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			// Fallback to stdout
			output = os.Stdout
		} else {
			output = file
		}
	}
	
	// Configure format
	var logger zerolog.Logger
	if strings.ToLower(cfg.Format) == "console" {
		// Pretty console output
		output = zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: time.RFC3339,
		}
	}
	
	logger = zerolog.New(output).
		With().
		Timestamp().
		Caller().
		Logger()
	
	return &ZerologLogger{logger: logger}
}

// Debug logs a debug message
func (l *ZerologLogger) Debug(msg string, fields map[string]interface{}) {
	event := l.logger.Debug()
	addFields(event, fields)
	event.Msg(msg)
}

// Info logs an info message
func (l *ZerologLogger) Info(msg string, fields map[string]interface{}) {
	event := l.logger.Info()
	addFields(event, fields)
	event.Msg(msg)
}

// Warn logs a warning message
func (l *ZerologLogger) Warn(msg string, fields map[string]interface{}) {
	event := l.logger.Warn()
	addFields(event, fields)
	event.Msg(msg)
}

// Error logs an error message
func (l *ZerologLogger) Error(msg string, fields map[string]interface{}) {
	event := l.logger.Error()
	addFields(event, fields)
	event.Msg(msg)
}

// Fatal logs a fatal message and exits
func (l *ZerologLogger) Fatal(msg string, fields map[string]interface{}) {
	event := l.logger.Fatal()
	addFields(event, fields)
	event.Msg(msg)
}

// With creates a new logger with additional fields
func (l *ZerologLogger) With(fields map[string]interface{}) Logger {
	ctx := l.logger.With()
	for key, value := range fields {
		ctx = ctx.Interface(key, value)
	}
	return &ZerologLogger{logger: ctx.Logger()}
}

// addFields adds fields to a zerolog event
func addFields(event *zerolog.Event, fields map[string]interface{}) *zerolog.Event {
	if fields == nil {
		return event
	}
	
	for key, value := range fields {
		switch v := value.(type) {
		case string:
			event = event.Str(key, v)
		case int:
			event = event.Int(key, v)
		case int64:
			event = event.Int64(key, v)
		case float64:
			event = event.Float64(key, v)
		case bool:
			event = event.Bool(key, v)
		case time.Time:
			event = event.Time(key, v)
		case time.Duration:
			event = event.Dur(key, v)
		case error:
			event = event.AnErr(key, v)
		default:
			event = event.Interface(key, v)
		}
	}
	
	return event
}

// NewDefault creates a logger with default settings
func NewDefault() *ZerologLogger {
	return New(&Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
}

