package config

import (
	"errors"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	Environment string
	LogLevel    slog.Level
	Port        string
	DatabaseURL string
}

func Load() (Config, error) {
	config := Config{
		Environment: valueOrDefault("APP_ENV", "development"),
		Port:        valueOrDefault("PORT", "8080"),
		DatabaseURL: strings.TrimSpace(os.Getenv("DATABASE_URL")),
	}

	if config.DatabaseURL == "" && strings.TrimSpace(os.Getenv("PGHOST")) == "" {
		return Config{}, errors.New("DATABASE_URL or PostgreSQL PG* variables are required")
	}

	switch strings.ToLower(valueOrDefault("LOG_LEVEL", "info")) {
	case "debug":
		config.LogLevel = slog.LevelDebug
	case "info":
		config.LogLevel = slog.LevelInfo
	case "warn":
		config.LogLevel = slog.LevelWarn
	case "error":
		config.LogLevel = slog.LevelError
	default:
		return Config{}, errors.New("LOG_LEVEL must be debug, info, warn, or error")
	}

	return config, nil
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
