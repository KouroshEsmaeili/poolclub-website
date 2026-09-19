package config

import "os"

const defaultPort = "8080"

// Config contains the server configuration.
type Config struct {
	Port string
}

// Load reads configuration from the environment.
func Load() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	return Config{Port: port}
}
