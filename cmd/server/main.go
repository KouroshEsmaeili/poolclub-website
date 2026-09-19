package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/KouroshEsmaeili/poolclub-website/internal/config"
	"github.com/KouroshEsmaeili/poolclub-website/internal/database"
)

func main() {
	cfg := config.Load()
	db, err := database.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database startup check failed: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("get database connection: %v", err)
	}
	defer func() {
		if err := sqlDB.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()

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
