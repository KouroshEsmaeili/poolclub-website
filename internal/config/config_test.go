package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")

	cfg := Load()

	if cfg.Port != defaultPort {
		t.Fatalf("Port = %q, want %q", cfg.Port, defaultPort)
	}
	if cfg.DatabaseURL != defaultDatabaseURL {
		t.Fatalf("DatabaseURL = %q, want %q", cfg.DatabaseURL, defaultDatabaseURL)
	}
}

func TestLoadCustomPort(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "")

	cfg := Load()

	if cfg.Port != "9090" {
		t.Fatalf("Port = %q, want %q", cfg.Port, "9090")
	}
}

func TestLoadCustomDatabaseURL(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "file:test.db?mode=ro")

	cfg := Load()

	if cfg.DatabaseURL != "file:test.db?mode=ro" {
		t.Fatalf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "file:test.db?mode=ro")
	}
}
