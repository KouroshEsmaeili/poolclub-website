package main

import (
	"log"
	"net/http"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/auth"
	"github.com/KouroshEsmaeili/poolclub-website/internal/booking"
	"github.com/KouroshEsmaeili/poolclub-website/internal/classes"
	"github.com/KouroshEsmaeili/poolclub-website/internal/config"
	"github.com/KouroshEsmaeili/poolclub-website/internal/database"
	"github.com/KouroshEsmaeili/poolclub-website/internal/events"
	"github.com/KouroshEsmaeili/poolclub-website/internal/httpapi"
	"github.com/KouroshEsmaeili/poolclub-website/internal/membership"
	"github.com/KouroshEsmaeili/poolclub-website/internal/user"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
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

	sessionTTL, err := time.ParseDuration(cfg.SessionTTL)
	if err != nil || sessionTTL <= 0 {
		log.Fatalf("invalid SESSION_TTL %q", cfg.SessionTTL)
	}
	sessions := auth.NewSessionStore(sessionTTL)
	authHandler, err := auth.NewHandler(
		user.NewStore(db),
		sessions,
		cfg.SessionCookieSecure,
	)
	if err != nil {
		log.Fatalf("create authentication handler: %v", err)
	}
	walletService := wallet.NewService(db)
	walletHandler := wallet.NewHandler(walletService, authHandler)
	bookingService := booking.NewService(db, walletService, booking.LoadPrices("data/prices.json"))
	bookingHandler := booking.NewHandler(bookingService, authHandler)
	membershipService := membership.NewService(db, walletService, membership.LoadPlans("data/memberships.json"))
	membershipHandler := membership.NewHandler(membershipService, authHandler)
	classService := classes.NewService(db, walletService, classes.LoadCatalog("data/classes.json"))
	classHandler := classes.NewHandler(classService, authHandler)
	eventService := events.NewService(db, walletService, events.LoadCatalog("data/events.json"))
	eventHandler := events.NewHandler(eventService, authHandler)
	handler := httpapi.NewRouter(authHandler, walletHandler, bookingHandler, membershipHandler, classHandler, eventHandler)

	address := ":" + cfg.Port
	log.Printf("server listening at http://localhost:%s", cfg.Port)
	if err := http.ListenAndServe(address, handler); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
