// Package httpapi constructs the Go backend's HTTP routes.
package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
)

// RouteRegistrar adds one domain's routes to the application mux.
type RouteRegistrar interface {
	RegisterRoutes(*http.ServeMux)
}

// NewRouter builds the application HTTP handler.
func NewRouter(registrars ...RouteRegistrar) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
			log.Printf("write health response: %v", err)
		}
	})
	for _, registrar := range registrars {
		registrar.RegisterRoutes(mux)
	}

	return mux
}
