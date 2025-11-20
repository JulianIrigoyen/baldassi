package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	// Global logger instance
	Logger zerolog.Logger
)

// LogLevel represents log levels
type LogLevel string

const (
	DebugLevel LogLevel = "debug"
	InfoLevel  LogLevel = "info"
	WarnLevel  LogLevel = "warn"
	ErrorLevel LogLevel = "error"
)

// Config holds logger configuration
type Config struct {
	Level      LogLevel
	Pretty     bool   // Human-readable output
	TimeFormat string // Time format for logs
}

// Initialize sets up the global logger
func Initialize(config Config) {
	// Set global log level
	level := parseLevel(config.Level)
	zerolog.SetGlobalLevel(level)

	// Configure time format
	if config.TimeFormat == "" {
		config.TimeFormat = time.RFC3339
	}
	zerolog.TimeFieldFormat = config.TimeFormat

	// Create logger
	if config.Pretty {
		// Human-readable console output for development
		Logger = zerolog.New(zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "15:04:05",
		}).With().Timestamp().Logger()
	} else {
		// JSON output for production
		Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	}

	// Set as global default
	log.Logger = Logger
}

// parseLevel converts string to zerolog level
func parseLevel(level LogLevel) zerolog.Level {
	switch level {
	case DebugLevel:
		return zerolog.DebugLevel
	case InfoLevel:
		return zerolog.InfoLevel
	case WarnLevel:
		return zerolog.WarnLevel
	case ErrorLevel:
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// Get returns the global logger
func Get() *zerolog.Logger {
	return &Logger
}

// WithComponent creates a child logger with component field
func WithComponent(component string) zerolog.Logger {
	return Logger.With().Str("component", component).Logger()
}

// WithBlock creates a child logger with block number
func WithBlock(blockNumber uint64) zerolog.Logger {
	return Logger.With().Uint64("block", blockNumber).Logger()
}

// Debug logs a debug message
func Debug() *zerolog.Event {
	return Logger.Debug()
}

// Info logs an info message
func Info() *zerolog.Event {
	return Logger.Info()
}

// Warn logs a warning message
func Warn() *zerolog.Event {
	return Logger.Warn()
}

// Error logs an error message
func Error() *zerolog.Event {
	return Logger.Error()
}

// Fatal logs a fatal message and exits
func Fatal() *zerolog.Event {
	return Logger.Fatal()
}
