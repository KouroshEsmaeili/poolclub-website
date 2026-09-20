package config

import "testing"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SESSION_TTL", "")
	t.Setenv("SESSION_COOKIE_SECURE", "")

	cfg := Load()

	if cfg.Port != defaultPort {
		t.Fatalf("Port = %q, want %q", cfg.Port, defaultPort)
	}
	if cfg.DatabaseURL != defaultDatabaseURL {
		t.Fatalf("DatabaseURL = %q, want %q", cfg.DatabaseURL, defaultDatabaseURL)
	}
	if cfg.SessionTTL != defaultSessionTTL {
		t.Fatalf("SessionTTL = %q, want %q", cfg.SessionTTL, defaultSessionTTL)
	}
	if cfg.SessionCookieSecure != defaultSessionCookieSecure {
		t.Fatalf("SessionCookieSecure = %t, want %t", cfg.SessionCookieSecure, defaultSessionCookieSecure)
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

func TestLoadSessionConfiguration(t *testing.T) {
	t.Setenv("SESSION_TTL", "48h")
	t.Setenv("SESSION_COOKIE_SECURE", "true")

	cfg := Load()

	if cfg.SessionTTL != "48h" {
		t.Fatalf("SessionTTL = %q, want %q", cfg.SessionTTL, "48h")
	}
	if !cfg.SessionCookieSecure {
		t.Fatal("SessionCookieSecure = false, want true")
	}
}
