package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/auth"
	"github.com/KouroshEsmaeili/poolclub-website/internal/booking"
	"github.com/KouroshEsmaeili/poolclub-website/internal/classes"
	"github.com/KouroshEsmaeili/poolclub-website/internal/config"
	"github.com/KouroshEsmaeili/poolclub-website/internal/database"
	"github.com/KouroshEsmaeili/poolclub-website/internal/events"
	"github.com/KouroshEsmaeili/poolclub-website/internal/httpapi"
	"github.com/KouroshEsmaeili/poolclub-website/internal/info"
	"github.com/KouroshEsmaeili/poolclub-website/internal/membership"
	"github.com/KouroshEsmaeili/poolclub-website/internal/user"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"github.com/KouroshEsmaeili/poolclub-website/internal/web"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
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
	userStore := user.NewStore(db)
	authHandler, err := auth.NewHandler(
		userStore,
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
	rankingsClient := info.NewDefaultRankingsClient()
	infoHandler := info.NewHandler("data/pools.json", "data/programmes.json", rankingsClient)
	webHandler, err := web.NewHandler(web.Dependencies{
		Auth: authHandler, Users: userStore, Wallet: walletService,
		Bookings: bookingService, Memberships: membershipService,
		Classes: classService, Events: eventService, Rankings: rankingsClient,
		DataDir: "data", StaticDir: "static", CookieSecure: cfg.SessionCookieSecure,
	})
	if err != nil {
		log.Fatalf("create web handler: %v", err)
	}
	handler := httpapi.NewRouter(authHandler, walletHandler, bookingHandler, membershipHandler, classHandler, eventHandler, infoHandler, webHandler)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-signalContext.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		log.Print("server shutdown requested")
		if err := server.Shutdown(shutdownContext); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
			if closeErr := server.Close(); closeErr != nil {
				log.Printf("force server close failed: %v", closeErr)
			}
		}
	}()

	log.Printf("server listening at http://localhost:%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}
