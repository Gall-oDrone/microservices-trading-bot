package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog for structured logging
type Logger struct {
	logger *zerolog.Logger
}

// Config holds logger configuration
type Config struct {
	Level  string // trace, debug, info, warn, error, fatal
	Format string // json, console
	Output string // stdout, stderr
}

// New creates a new logger with the given configuration
func New(config *Config) *Logger {
	// Parse log level
	level := parseLogLevel(config.Level)
	zerolog.SetGlobalLevel(level)

	// Determine output writer
	var output io.Writer
	switch config.Output {
	case "stderr":
		output = os.Stderr
	default:
		output = os.Stdout
	}

	// Create logger with format
	var zlog zerolog.Logger
	if config.Format == "console" {
		output = zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: time.RFC3339,
		}
	}

	zlog = zerolog.New(output).
		With().
		Timestamp().
		Caller().
		Logger()

	return &Logger{
		logger: &zlog,
	}
}

// DefaultLogger creates a logger with default configuration
func DefaultLogger() *Logger {
	return New(&Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields map[string]interface{}) {
	event := l.logger.Debug()
	for k, v := range fields {
		event = event.Interface(k, v)
	}
	event.Msg(msg)
}

// Info logs an info message
func (l *Logger) Info(msg string, fields map[string]interface{}) {
	event := l.logger.Info()
	for k, v := range fields {
		event = event.Interface(k, v)
	}
	event.Msg(msg)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields map[string]interface{}) {
	event := l.logger.Warn()
	for k, v := range fields {
		event = event.Interface(k, v)
	}
	event.Msg(msg)
}

// Error logs an error message
func (l *Logger) Error(msg string, fields map[string]interface{}) {
	event := l.logger.Error()
	for k, v := range fields {
		event = event.Interface(k, v)
	}
	event.Msg(msg)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(msg string, fields map[string]interface{}) {
	event := l.logger.Fatal()
	for k, v := range fields {
		event = event.Interface(k, v)
	}
	event.Msg(msg)
}

// Debugf logs a formatted debug message
func (l *Logger) Debugf(format string, args ...interface{}) {
	l.logger.Debug().Msgf(format, args...)
}

// Infof logs a formatted info message
func (l *Logger) Infof(format string, args ...interface{}) {
	l.logger.Info().Msgf(format, args...)
}

// Warnf logs a formatted warning message
func (l *Logger) Warnf(format string, args ...interface{}) {
	l.logger.Warn().Msgf(format, args...)
}

// Errorf logs a formatted error message
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.logger.Error().Msgf(format, args...)
}

// Fatalf logs a formatted fatal message and exits
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.logger.Fatal().Msgf(format, args...)
}

// With creates a child logger with additional fields
func (l *Logger) With(fields map[string]interface{}) *Logger {
	ctx := l.logger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	newLogger := ctx.Logger()
	return &Logger{logger: &newLogger}
}

// WithField creates a child logger with an additional field
func (l *Logger) WithField(key string, value interface{}) *Logger {
	newLogger := l.logger.With().Interface(key, value).Logger()
	return &Logger{logger: &newLogger}
}

// GetZerolog returns the underlying zerolog logger
func (l *Logger) GetZerolog() *zerolog.Logger {
	return l.logger
}

// parseLogLevel parses the log level string
func parseLogLevel(level string) zerolog.Level {
	switch level {
	case "trace":
		return zerolog.TraceLevel
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	default:
		return zerolog.InfoLevel
	}
}
