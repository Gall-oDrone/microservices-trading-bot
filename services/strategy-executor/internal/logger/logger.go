package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Logger wraps zerolog.Logger with additional functionality
type Logger struct {
	logger zerolog.Logger
}

// Config holds logger configuration
type Config struct {
	Level  string `json:"level"`
	Format string `json:"format"`
	Output string `json:"output"`
}

// New creates a new logger instance
func New(config *Config) *Logger {
	// Set log level
	level := parseLevel(config.Level)
	zerolog.SetGlobalLevel(level)

	// Configure output format
	var output zerolog.ConsoleWriter
	if config.Format == "json" {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	} else {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
			NoColor:    false,
		}
	}

	// Create logger
	logger := zerolog.New(output).With().
		Timestamp().
		Caller().
		Logger()

	return &Logger{
		logger: logger,
	}
}

// NewDefault creates a logger with default configuration
func NewDefault() *Logger {
	return New(&Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
}

// Debug logs a debug message
func (l *Logger) Debug(msg string) {
	l.logger.Debug().Msg(msg)
}

// Debugf logs a debug message with formatting
func (l *Logger) Debugf(format string, v ...interface{}) {
	l.logger.Debug().Msgf(format, v...)
}

// Info logs an info message
func (l *Logger) Info(msg string) {
	l.logger.Info().Msg(msg)
}

// Infof logs an info message with formatting
func (l *Logger) Infof(format string, v ...interface{}) {
	l.logger.Info().Msgf(format, v...)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string) {
	l.logger.Warn().Msg(msg)
}

// Warnf logs a warning message with formatting
func (l *Logger) Warnf(format string, v ...interface{}) {
	l.logger.Warn().Msgf(format, v...)
}

// Error logs an error message
func (l *Logger) Error(msg string) {
	l.logger.Error().Msg(msg)
}

// Errorf logs an error message with formatting
func (l *Logger) Errorf(format string, v ...interface{}) {
	l.logger.Error().Msgf(format, v...)
}

// Fatal logs a fatal message and exits
func (l *Logger) Fatal(msg string) {
	l.logger.Fatal().Msg(msg)
}

// Fatalf logs a fatal message with formatting and exits
func (l *Logger) Fatalf(format string, v ...interface{}) {
	l.logger.Fatal().Msgf(format, v...)
}

// WithField adds a field to the logger
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return &Logger{
		logger: l.logger.With().Interface(key, value).Logger(),
	}
}

// WithFields adds multiple fields to the logger
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	logger := l.logger.With()
	for key, value := range fields {
		logger = logger.Interface(key, value)
	}
	return &Logger{
		logger: logger.Logger(),
	}
}

// WithError adds an error field to the logger
func (l *Logger) WithError(err error) *Logger {
	return &Logger{
		logger: l.logger.With().Err(err).Logger(),
	}
}

// WithStr adds a string field to the logger
func (l *Logger) WithStr(key, value string) *Logger {
	return &Logger{
		logger: l.logger.With().Str(key, value).Logger(),
	}
}

// WithInt adds an integer field to the logger
func (l *Logger) WithInt(key string, value int) *Logger {
	return &Logger{
		logger: l.logger.With().Int(key, value).Logger(),
	}
}

// WithFloat64 adds a float64 field to the logger
func (l *Logger) WithFloat64(key string, value float64) *Logger {
	return &Logger{
		logger: l.logger.With().Float64(key, value).Logger(),
	}
}

// WithBool adds a boolean field to the logger
func (l *Logger) WithBool(key string, value bool) *Logger {
	return &Logger{
		logger: l.logger.With().Bool(key, value).Logger(),
	}
}

// WithTime adds a time field to the logger
func (l *Logger) WithTime(key string, value time.Time) *Logger {
	return &Logger{
		logger: l.logger.With().Time(key, value).Logger(),
	}
}

// WithDuration adds a duration field to the logger
func (l *Logger) WithDuration(key string, value time.Duration) *Logger {
	return &Logger{
		logger: l.logger.With().Dur(key, value).Logger(),
	}
}

// GetZerologLogger returns the underlying zerolog.Logger
func (l *Logger) GetZerologLogger() zerolog.Logger {
	return l.logger
}

// SetGlobalLogger sets the global logger
func SetGlobalLogger(logger *Logger) {
	log.Logger = logger.GetZerologLogger()
}

// GetGlobalLogger returns the global logger
func GetGlobalLogger() *Logger {
	return &Logger{
		logger: log.Logger,
	}
}

// parseLevel parses the log level string
func parseLevel(level string) zerolog.Level {
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
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}

// ServiceLogger creates a logger with service context
func ServiceLogger(serviceName, version string) *Logger {
	return &Logger{
		logger: log.With().
			Str("service", serviceName).
			Str("version", version).
			Logger(),
	}
}

// ComponentLogger creates a logger with component context
func ComponentLogger(serviceName, component string) *Logger {
	return &Logger{
		logger: log.With().
			Str("service", serviceName).
			Str("component", component).
			Logger(),
	}
}

// RequestLogger creates a logger with request context
func RequestLogger(serviceName, requestID string) *Logger {
	return &Logger{
		logger: log.With().
			Str("service", serviceName).
			Str("request_id", requestID).
			Logger(),
	}
}
