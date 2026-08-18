package config

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

const developmentReportHMACKey = "development-only-report-key-change-me"

type Config struct {
	Environment         string
	LogLevel            slog.Level
	Port                string
	DatabaseURL         string
	GameDataPath        string
	GameManifestPath    string
	CookieSecure        bool
	AdminOrigin         string
	ReportHMACKey       string
	SecretEncryptionKey []byte
}

func Load() (Config, error) {
	config := Config{
		Environment:      valueOrDefault("APP_ENV", "development"),
		Port:             valueOrDefault("PORT", "8080"),
		DatabaseURL:      strings.TrimSpace(os.Getenv("DATABASE_URL")),
		GameDataPath:     valueOrDefault("MHED_GAME_DATA_PATH", "/game-data/mh3g.sqlite"),
		GameManifestPath: valueOrDefault("MHED_GAME_MANIFEST_PATH", "/game-data/manifest.json"),
		AdminOrigin:      strings.TrimRight(valueOrDefault("MHED_ADMIN_ORIGIN", "http://127.0.0.1:18102"), "/"),
		ReportHMACKey:    valueOrDefault("MHED_REPORT_HMAC_KEY", developmentReportHMACKey),
	}
	config.CookieSecure = strings.EqualFold(valueOrDefault("MHED_COOKIE_SECURE", "false"), "true")
	masterValue := strings.TrimSpace(os.Getenv("MHED_SECRET_ENCRYPTION_KEY"))
	if masterValue == "" && !strings.EqualFold(config.Environment, "production") {
		masterValue = base64.RawStdEncoding.EncodeToString([]byte("mhed-development-email-key-v1!!!"))
	}
	if masterValue != "" {
		decoded, err := base64.RawStdEncoding.DecodeString(masterValue)
		if err != nil {
			decoded, err = base64.StdEncoding.DecodeString(masterValue)
		}
		if err != nil || len(decoded) != 32 {
			return Config{}, errors.New("MHED_SECRET_ENCRYPTION_KEY must be base64-encoded 32 bytes")
		}
		config.SecretEncryptionKey = decoded
	}

	if config.DatabaseURL == "" && strings.TrimSpace(os.Getenv("PGHOST")) == "" {
		return Config{}, errors.New("DATABASE_URL or PostgreSQL PG* variables are required")
	}
	if strings.EqualFold(config.Environment, "production") {
		if len(config.SecretEncryptionKey) != 32 {
			return Config{}, errors.New("MHED_SECRET_ENCRYPTION_KEY is required in production")
		}
		if !config.CookieSecure {
			return Config{}, errors.New("MHED_COOKIE_SECURE must be true in production")
		}
		adminOrigin, err := url.Parse(config.AdminOrigin)
		if err != nil || adminOrigin.Scheme != "https" || adminOrigin.Host == "" || adminOrigin.Path != "" {
			return Config{}, errors.New("MHED_ADMIN_ORIGIN must be an HTTPS origin without a path in production")
		}
		if config.ReportHMACKey == developmentReportHMACKey || len(config.ReportHMACKey) < 32 {
			return Config{}, errors.New("MHED_REPORT_HMAC_KEY must be a non-default secret of at least 32 bytes in production")
		}
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
