package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/farmsense/api/internal/config"
	"github.com/farmsense/api/internal/db"
	"github.com/farmsense/api/internal/handlers"
	authmw "github.com/farmsense/api/internal/middleware"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slog.Info("loading config from Vault...")
	cfg, err := config.Load(ctx)
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}
	slog.Info("config loaded", "influx_url", cfg.InfluxURL, "kafka", cfg.KafkaBrokers)

	slog.Info("connecting to database...")
	pool, err := db.New(ctx, cfg.PostgresDSN)
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("database connected and schema migrated")

	authHandler := handlers.NewAuthHandler(pool, cfg.JWTSecret)
	farmHandler := handlers.NewFarmHandler(pool)
	hubHandler := handlers.NewHubHandler(pool)
	nodeHandler := handlers.NewNodeHandler(pool, cfg.InfluxURL, cfg.InfluxToken, cfg.InfluxOrg, cfg.InfluxBucket)

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)

	// CORS middleware — allow any origin (behind Cloudflare Tunnel)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	r.Route("/api", func(r chi.Router) {
		// Public routes
		r.Post("/auth/register", authHandler.Register)
		r.Post("/auth/login", authHandler.Login)

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(authmw.JWTMiddleware(cfg.JWTSecret))

			r.Get("/farms", farmHandler.List)
			r.Post("/farms", farmHandler.Create)
			r.Get("/farms/{farmId}", farmHandler.Get)
			r.Get("/farms/{farmId}/stats", farmHandler.Stats)
			r.Get("/farms/{farmId}/hubs", hubHandler.List)
			r.Post("/farms/{farmId}/hubs", hubHandler.Create)

			r.Get("/hubs/{hubId}", hubHandler.Get)
			r.Get("/hubs/{hubId}/provision", hubHandler.Provision)
			r.Get("/hubs/{hubId}/nodes", nodeHandler.List)
			r.Post("/hubs/{hubId}/nodes", nodeHandler.Create)

			r.Get("/nodes/{nodeId}/readings", nodeHandler.Readings)
		})
	})

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("api listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	slog.Info("shutting down gracefully...")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
}
