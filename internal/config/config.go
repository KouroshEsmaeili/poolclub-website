package config

import (
	"os"
	"strconv"
)

const (
	defaultPort                = "8080"
	defaultDatabaseURL         = "poolclub.db"
	defaultSessionTTL          = "24h"
	defaultSessionCookieSecure = false
)

// Config contains the server configuration.
type Config struct {
	Port                string
	DatabaseURL         string
	SessionTTL          string
	SessionCookieSecure bool
}

// Load reads configuration from the environment.
func Load() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = defaultDatabaseURL
	}

	sessionTTL := os.Getenv("SESSION_TTL")
	if sessionTTL == "" {
		sessionTTL = defaultSessionTTL
	}

	sessionCookieSecure := defaultSessionCookieSecure
	if value := os.Getenv("SESSION_COOKIE_SECURE"); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			sessionCookieSecure = parsed
		}
	}

	return Config{
		Port:                port,
		DatabaseURL:         databaseURL,
		SessionTTL:          sessionTTL,
		SessionCookieSecure: sessionCookieSecure,
	}
}
