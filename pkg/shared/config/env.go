package config

import (
	"cmp"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Get returns the environment variable for key or def if unset.
func Get(key, def string) string {
	return cmp.Or(os.Getenv(key), def)
}

// GetInt parses an integer from the environment, logging a warning and returning def on failure.
func GetInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("config_parse_int_failed", "key", key, "value", v, "err", err, "fallback", def)
		return def
	}
	return n
}

// GetDuration parses a duration from the environment, logging a warning and returning def on failure.
func GetDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("config_parse_duration_failed", "key", key, "value", v, "err", err, "fallback", def.String())
		return def
	}
	return d
}

// GetFloat parses a float64 from the environment, logging a warning and returning def on failure.
func GetFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		slog.Warn("config_parse_float_failed", "key", key, "value", v, "err", err, "fallback", def)
		return def
	}
	return f
}
