// Package config reads settings from environment variables (12-factor), so
// the same images run unchanged on a laptop, lean servers or AWS.
package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

func String(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Int(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func Bool(key string, def bool) bool {
	if v, err := strconv.ParseBool(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func Duration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

// Must returns the variable or exits with a clear message.
func Must(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("missing required environment variable", "name", key)
		os.Exit(2)
	}
	return v
}

// Logger returns a JSON logger tagged with the service name.
func Logger(service string) *slog.Logger {
	lvl := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		lvl = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})).With("service", service)
}
