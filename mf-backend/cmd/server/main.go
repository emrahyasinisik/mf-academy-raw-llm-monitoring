// Command server is the entrypoint for the Raw LLM Monitoring & Decision
// Scoring backend. It wires configuration, the database, middleware and the
// module routers, then serves HTTP with graceful shutdown.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/emrah/mf-backend/internal/auth"
	"github.com/emrah/mf-backend/internal/common"
	"github.com/emrah/mf-backend/internal/config"
	"github.com/emrah/mf-backend/internal/docs"
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

	// Structured logging: JSON in production (parseable by Render/aggregators),
	// human-readable text locally.
	common.SetupLogger(cfg.IsProduction())

	// Refuse to serve on a configuration that cannot be secure. This runs before
	// anything binds a port or opens a connection, so a misconfigured deploy
	// fails its health check loudly instead of quietly accepting forged tokens.
	if err := cfg.Validate(); err != nil {
		slog.Error("refusing to start", "error", err)
		os.Exit(1)
	}
	for _, w := range cfg.Warnings() {
		slog.Warn("configuration warning", "detail", w)
	}

	ctx := context.Background()
	pool, err := common.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Apply the idempotent schema on boot. The SQL is embedded in the binary
	// (see package migrations), so this works regardless of working directory.
	statements, err := migrations.SQL()
	if err != nil {
		slog.Error("load migrations failed", "error", err)
		os.Exit(1)
	}
	if err := common.RunMigrations(ctx, pool, statements...); err != nil {
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	slog.Info("database ready, migrations applied")

	// ---- dependency wiring (constructor injection, Go Day 46) ----
	tokens := auth.NewTokenService(cfg.JWTSecret, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	authStore := auth.NewStore(pool)
	authHandler := auth.NewHandler(authStore, tokens, cfg.BcryptCost)

	llmStore := llm.NewStore(pool)
	// Server-side inference is optional: with LLM_BASE_URL unset the provider
	// reports itself unconfigured, POST /llm/generate answers 503, and the
	// browser path is unaffected. The inference host is someone's desktop, so
	// "switched off" has to be a supported state, not a boot failure.
	llmProvider := llm.NewOpenAIProvider(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMTimeout, cfg.LLMMaxTokens)
	llmHandler := llm.NewHandler(llmStore, llmProvider)

	cfgHandler := config.NewHandler(cfg)

	// Background workers share one context so shutdown stops all of them.
	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()

	// Per-IP rate limiter for sensitive auth endpoints: a burst of 10 then a
	// steady 1 request every 2s. Enough for real logins, hostile to brute force.
	authLimiter := common.NewRateLimiter(workerCtx, 0.5, 10)

	// ---- router ----
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	// RealIP rewrites RemoteAddr from X-Forwarded-For, which is only meaningful
	// — and only safe — when a proxy we control has already set that header.
	// Mounting it on a directly-exposed instance would hand every caller
	// control of their own apparent IP.
	if cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(common.RequestLogger)
	r.Use(common.Recover)
	r.Use(common.SecurityHeaders)
	r.Use(common.CORS(cfg.CORSOrigins))

	// Every request is still bounded before any handler runs — the deadline has
	// to reach the database driver so a stalled query releases its pooled
	// connection — but the bound is applied per subtree rather than at the root.
	//
	// It cannot be applied at the root any more. One route waits on a GPU across
	// a tunnel and needs tens of seconds, and a child context cannot extend a
	// parent's deadline: a root-level 5s bound silently won over the longer one
	// declared on that route, and generation was cut off mid-flight at exactly
	// 5s with the model still working on it.
	r.Group(func(pr chi.Router) {
		pr.Use(common.Timeout(cfg.RequestTimeout))

		// Config module
		pr.Get("/config", cfgHandler.Config)
		pr.Get("/version", cfgHandler.Version)

		// API documentation
		pr.Get("/openapi.yaml", docs.SpecYAML)
		pr.Get("/docs", docs.Reference)

		// Common module — liveness & readiness
		pr.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
			common.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		pr.Get("/ready", func(w http.ResponseWriter, req *http.Request) {
			pingCtx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
			defer cancel()
			if err := pool.Ping(pingCtx); err != nil {
				common.Error(w, common.ErrInternal("database unavailable"))
				return
			}
			common.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
		})

		pr.Mount("/auth", authHandler.Routes(tokens.Verify, authLimiter.Middleware))
	})

	// Mounted outside that group so its own routes can choose their bounds: the
	// short default for everything, the long one for generation.
	r.Mount("/llm", llmHandler.Routes(tokens.Verify, cfg.RequestTimeout, cfg.LLMTimeout))

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
		// Every phase of a connection needs a bound. ReadHeaderTimeout alone
		// leaves body reads and response writes unbounded, so a client that
		// stalls mid-transfer pins a goroutine and its database connection
		// indefinitely. WriteTimeout is deliberately wider than the per-request
		// timeout above, so handlers get to finish writing their error response
		// rather than having the connection cut from under them.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// ---- background: periodically reap expired/revoked sessions ----
	go sessionCleanup(workerCtx, authStore)

	// ---- serve with graceful shutdown (Go Day 36-40) ----
	go func() {
		slog.Info("server listening", "app", cfg.AppName, "port", cfg.Port, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	slog.Info("shutting down")

	stopWorker()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
	slog.Info("stopped")
}

// sessionCleanup deletes expired and long-revoked sessions on a fixed interval
// (and once at startup) so the sessions table doesn't grow without bound. It
// exits promptly when its context is cancelled during shutdown.
func sessionCleanup(ctx context.Context, store *auth.Store) {
	const interval = time.Hour

	reap := func() {
		reapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		n, err := store.DeleteExpiredSessions(reapCtx)
		if err != nil {
			slog.Warn("session cleanup failed", "error", err)
			return
		}
		if n > 0 {
			slog.Info("session cleanup", "deleted", n)
		}
	}

	reap() // run once at boot
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reap()
		}
	}
}
