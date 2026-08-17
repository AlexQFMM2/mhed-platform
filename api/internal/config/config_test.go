package config

import "testing"

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PGHOST", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted missing database configuration")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://test")
	t.Setenv("PGHOST", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("PORT", "")
	t.Setenv("LOG_LEVEL", "")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load returned an error: %v", err)
	}
	if config.Environment != "development" || config.Port != "8080" {
		t.Fatalf("unexpected defaults: %#v", config)
	}
}

func TestLoadAcceptsPostgresEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PGHOST", "db")
	if _, err := Load(); err != nil {
		t.Fatalf("Load rejected PostgreSQL environment variables: %v", err)
	}
}

func TestProductionRejectsUnsafeSettings(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("PGHOST", "db")
	t.Setenv("MHED_ADMIN_ORIGIN", "http://admin.example.test")
	t.Setenv("MHED_COOKIE_SECURE", "false")
	t.Setenv("MHED_REPORT_HMAC_KEY", developmentReportHMACKey)
	if _, err := Load(); err == nil {
		t.Fatal("production configuration accepted unsafe defaults")
	}
}

func TestProductionAcceptsSecureSettings(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("PGHOST", "db")
	t.Setenv("MHED_ADMIN_ORIGIN", "https://mhed.admin.65h26i.top")
	t.Setenv("MHED_COOKIE_SECURE", "true")
	t.Setenv("MHED_REPORT_HMAC_KEY", "0123456789abcdef0123456789abcdef")
	config, err := Load()
	if err != nil {
		t.Fatalf("secure production configuration was rejected: %v", err)
	}
	if !config.CookieSecure {
		t.Fatal("secure cookie setting was not retained")
	}
}
