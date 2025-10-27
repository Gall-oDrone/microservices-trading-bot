package logger

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// Logger wraps zerolog logger with additional functionality
type Logger struct {
	logger *zerolog.Logger
	level  zerolog.Level
	format string
}

// Config holds logger configuration
type Config struct {
	Level  string // trace, debug, info, warn, error, fatal, panic
	Format string // json or console
	Output string // stdout or stderr
}

// New creates a new logger with the given configuration
func New(cfg *Config) *Logger {
	// Parse log level
	level := parseLevel(cfg.Level)

	// Set global log level
	zerolog.SetGlobalLevel(level)

	// Configure time format
	zerolog.TimeFieldFormat = time.RFC3339Nano

	// Choose output
	var output io.Writer
	switch strings.ToLower(cfg.Output) {
	case "stderr":
		output = os.Stderr
	case "stdout":
		output = os.Stdout
	default:
		output = os.Stdout
	}

	// Create logger based on format
	var zLogger zerolog.Logger
	if strings.ToLower(cfg.Format) == "console" {
		// Console format with colors
		output = zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: time.RFC3339,
			NoColor:    false,
		}
		zLogger = zerolog.New(output).With().Timestamp().Caller().Logger()
	} else {
		// JSON format
		zLogger = zerolog.New(output).With().Timestamp().Caller().Logger()
	}

	return &Logger{
		logger: &zLogger,
		level:  level,
		format: cfg.Format,
	}
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields map[string]interface{}) {
	event := l.logger.Debug()
	l.addFields(event, fields)
	event.Msg(msg)
}

// Info logs an info message
func (l *Logger) Info(msg string, fields map[string]interface{}) {
	event := l.logger.Info()
	l.addFields(event, fields)
	event.Msg(msg)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields map[string]interface{}) {
	event := l.logger.Warn()
	l.addFields(event, fields)
	event.Msg(msg)
}

// Error logs an error message
func (l *Logger) Error(msg string, fields map[string]interface{}) {
	event := l.logger.Error()
	l.addFields(event, fields)
	event.Msg(msg)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(msg string, fields map[string]interface{}) {
	event := l.logger.Fatal()
	l.addFields(event, fields)
	event.Msg(msg)
}

// Panic logs a panic message and panics
func (l *Logger) Panic(msg string, fields map[string]interface{}) {
	event := l.logger.Panic()
	l.addFields(event, fields)
	event.Msg(msg)
}

// WithFields returns a new logger with additional fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	ctx := l.logger.With()
	for key, value := range fields {
		ctx = ctx.Interface(key, value)
	}
	newLogger := ctx.Logger()

	return &Logger{
		logger: &newLogger,
		level:  l.level,
		format: l.format,
	}
}

// WithRequestID returns a new logger with request ID
func (l *Logger) WithRequestID(requestID string) *Logger {
	return l.WithFields(map[string]interface{}{
		"request_id": requestID,
	})
}

// WithService returns a new logger with service name
func (l *Logger) WithService(serviceName string) *Logger {
	return l.WithFields(map[string]interface{}{
		"service": serviceName,
	})
}

// WithComponent returns a new logger with component name
func (l *Logger) WithComponent(componentName string) *Logger {
	return l.WithFields(map[string]interface{}{
		"component": componentName,
	})
}

// addFields adds fields to a zerolog event
func (l *Logger) addFields(event *zerolog.Event, fields map[string]interface{}) {
	if fields == nil {
		return
	}

	for key, value := range fields {
		switch v := value.(type) {
		case string:
			event.Str(key, v)
		case int:
			event.Int(key, v)
		case int64:
			event.Int64(key, v)
		case float64:
			event.Float64(key, v)
		case bool:
			event.Bool(key, v)
		case error:
			event.Err(v)
		case time.Duration:
			event.Dur(key, v)
		case time.Time:
			event.Time(key, v)
		default:
			event.Interface(key, v)
		}
	}
}

// parseLevel parses log level string to zerolog.Level
func parseLevel(level string) zerolog.Level {
	switch strings.ToLower(level) {
	case "trace":
		return zerolog.TraceLevel
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}

// GetLevel returns the current log level
func (l *Logger) GetLevel() string {
	return l.level.String()
}

// SetLevel sets the log level
func (l *Logger) SetLevel(level string) {
	l.level = parseLevel(level)
	zerolog.SetGlobalLevel(l.level)
}

// IsDebugEnabled returns true if debug level is enabled
func (l *Logger) IsDebugEnabled() bool {
	return l.level <= zerolog.DebugLevel
}

// IsTraceEnabled returns true if trace level is enabled
func (l *Logger) IsTraceEnabled() bool {
	return l.level <= zerolog.TraceLevel
}

