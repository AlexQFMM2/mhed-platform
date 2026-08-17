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
