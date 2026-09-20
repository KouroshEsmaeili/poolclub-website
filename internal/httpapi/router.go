// Package httpapi constructs the Go backend's HTTP routes.
package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/KouroshEsmaeili/poolclub-website/internal/auth"
)

// NewRouter builds the application HTTP handler.
func NewRouter(authHandler *auth.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
			log.Printf("write health response: %v", err)
		}
	})
	authHandler.RegisterRoutes(mux)

	return mux
}
