// Package httpapi constructs the Go backend's HTTP routes.
package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
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

	return protectUnsafeRequests(mux)
}

func protectUnsafeRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		if site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); site == "cross-site" {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}

		if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
			if !sameOrigin(origin, r.Host) {
				http.Error(w, "cross-origin request rejected", http.StatusForbidden)
				return
			}
		} else if referer := strings.TrimSpace(r.Referer()); referer != "" && !sameOrigin(referer, r.Host) {
			http.Error(w, "cross-origin request rejected", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func sameOrigin(rawURL string, host string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return strings.EqualFold(parsed.Host, host)
}
