package config

import (
	"log/slog"
	"os"
	"strings"
)

// SetupLogging configures the global slog logger based on the LOG_LEVEL
// environment variable. Valid values: debug, info, warn, error.
// Defaults to info if unset or unrecognized.
func SetupLogging() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: LogLevelFromEnv(),
	})))
}

// LogLevelFromEnv reads the LOG_LEVEL environment variable and returns the
// corresponding slog.Level. Use this when you need the level without
// replacing the global logger (e.g. when wrapping the handler).
func LogLevelFromEnv() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
