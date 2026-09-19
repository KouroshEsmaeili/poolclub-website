package config

import "os"

const (
	defaultPort        = "8080"
	defaultDatabaseURL = "poolclub.db"
)

// Config contains the server configuration.
type Config struct {
	Port        string
	DatabaseURL string
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

	return Config{
		Port:        port,
		DatabaseURL: databaseURL,
	}
}
