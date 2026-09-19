package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/KouroshEsmaeili/poolclub-website/internal/config"
)

func main() {
	cfg := config.Load()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]string{"status": "ok"}); err != nil {
			log.Printf("write health response: %v", err)
		}
	})

	address := ":" + cfg.Port
	log.Printf("server listening at http://localhost:%s", cfg.Port)
	if err := http.ListenAndServe(address, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
