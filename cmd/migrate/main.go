package main

import (
	"context"
	"log"

	"github.com/KouroshEsmaeili/poolclub-website/internal/config"
	"github.com/KouroshEsmaeili/poolclub-website/internal/database"
	"github.com/KouroshEsmaeili/poolclub-website/migrations"
)

func main() {
	cfg := config.Load()
	db, err := database.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
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

	applied, err := migrations.Apply(context.Background(), db)
	if err != nil {
		log.Fatalf("apply migrations: %v", err)
	}
	if len(applied) == 0 {
		log.Print("database is up to date")
		return
	}
	for _, version := range applied {
		log.Printf("applied migration %s", version)
	}
}
