// Command server is the entrypoint for the Raw LLM Monitoring & Decision
// Scoring backend. It wires configuration, the database, middleware and the
// module routers, then serves HTTP with graceful shutdown.
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

	"github.com/emrah/mf-backend/internal/auth"
	"github.com/emrah/mf-backend/internal/common"
	"github.com/emrah/mf-backend/internal/config"
	"github.com/emrah/mf-backend/internal/llm"
	"github.com/emrah/mf-backend/migrations"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env for local development (ignored if the file is absent, e.g. on
	// Render where real environment variables are provided).
	_ = godotenv.Load()

	cfg := config.Load()

	ctx := context.Background()
	pool, err := common.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	// Apply the idempotent schema on boot. The SQL is embedded in the binary
	// (see package migrations), so this works regardless of working directory.
	statements, err := migrations.SQL()
	if err != nil {
		log.Fatalf("load migrations: %v", err)
	}
	if err := common.RunMigrations(ctx, pool, statements...); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	log.Println("database ready, migrations applied")

	// ---- dependency wiring (constructor injection, Go Day 46) ----
	tokens := auth.NewTokenService(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	authStore := auth.NewStore(pool)
	authHandler := auth.NewHandler(authStore, tokens)

	llmStore := llm.NewStore(pool)
	llmHandler := llm.NewHandler(llmStore)

	cfgHandler := config.NewHandler(cfg)

	// ---- router ----
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(common.Recover)
	r.Use(common.CORS(cfg.CORSOrigins))

	// Config module
	r.Get("/config", cfgHandler.Config)
	r.Get("/version", cfgHandler.Version)

	// Common module — liveness & readiness
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		common.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/ready", func(w http.ResponseWriter, req *http.Request) {
		pingCtx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(pingCtx); err != nil {
			common.Error(w, common.ErrInternal("database unavailable"))
			return
		}
		common.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	// Feature modules
	r.Mount("/auth", authHandler.Routes(tokens.Verify))
	r.Mount("/llm", llmHandler.Routes(tokens.Verify))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// ---- serve with graceful shutdown (Go Day 36-40) ----
	go func() {
		log.Printf("%s listening on :%s (env=%s)", cfg.AppName, cfg.Port, cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	log.Println("stopped")
}
