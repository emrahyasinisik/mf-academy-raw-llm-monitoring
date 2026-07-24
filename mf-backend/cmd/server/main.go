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

	"github.com/emrah/mf-backend/internal/admin"
	"github.com/emrah/mf-backend/internal/analysis"
	"github.com/emrah/mf-backend/internal/auth"
	"github.com/emrah/mf-backend/internal/common"
	"github.com/emrah/mf-backend/internal/config"
	"github.com/emrah/mf-backend/internal/docs"
	"github.com/emrah/mf-backend/internal/llm"
	"github.com/emrah/mf-backend/internal/mcp"
	"github.com/emrah/mf-backend/internal/settings"
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

	// Promotes the account named by ADMIN_EMAIL, if it exists. Runs after
	// migrations so the role CHECK constraint is already in place, and does
	// nothing when the variable is unset — the normal state locally.
	//
	// Deliberately the only code path in the service that writes this column.
	// An HTTP route that grants admin is a privilege-escalation bug waiting for
	// its first authentication flaw; requiring a deployment variable plus a
	// restart means the blast radius of any such flaw stops at ordinary users.
	if cfg.AdminEmail != "" {
		promoted, err := common.PromoteAdmin(ctx, pool, cfg.AdminEmail)
		switch {
		case err != nil:
			slog.Error("admin bootstrap failed", "email", cfg.AdminEmail, "error", err)
		case promoted:
			slog.Info("admin bootstrap: account promoted", "email", cfg.AdminEmail)
		default:
			// Covers both "already admin" and "no such account". Logged either
			// way, because a typo in ADMIN_EMAIL is otherwise completely silent
			// and only discovered when the panel returns 403.
			slog.Info("admin bootstrap: no change", "email", cfg.AdminEmail)
		}
	}

	llmStore := llm.NewStore(pool)
	// Server-side inference is optional: with LLM_BASE_URL unset the provider
	// reports itself unconfigured, POST /llm/generate answers 503, and the
	// browser path is unaffected. The inference host is someone's desktop, so
	// "switched off" has to be a supported state, not a boot failure.
	llmProvider := llm.NewOpenAIProvider(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMTimeout, cfg.LLMMaxTokens)
	llmHandler := llm.NewHandler(llmStore, llmProvider)

	// Runtime settings the admin panel edits and the analysis path reads on
	// every request. Its own package so neither of those two has to import the
	// other; see internal/settings.
	settingsStore := settings.NewStore(pool)

	adminStore := admin.NewStore(pool)
	adminHandler := admin.NewHandler(adminStore, settingsStore, adminStore)
	analysisHandler := analysis.NewHandler(analysis.NewStore(pool), llmProvider, settingsStore)

	// The analysis engine's second caller. It runs the same code the HTTP path
	// does rather than a parallel implementation: an MCP client and a browser
	// must get identical reports from identical input, and two implementations
	// would drift on the first change to the prompt, parser or scoring.
	mcpServer := mcp.NewServer(analysisHandler, cfg.AppName, cfg.AppVersion)

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
	// Above the timeout groups so every request is counted, including the ones
	// that time out — those are precisely the ones worth seeing on a dashboard.
	r.Use(common.Metrics)

	// Prometheus exposition. Outside the timed groups: a scrape is cheap, but
	// binding it to the same 5s bound as a database call would make the metrics
	// disappear exactly when the service is unhealthy enough to need them.
	if cfg.MetricsEnabled() {
		r.Get("/metrics", common.MetricsHandler(cfg.MetricsToken).ServeHTTP)
	}

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

		// Control plane. Every route inside is gated on the admin role by the
		// subtree's own middleware, so mounting it here — inside the short
		// default bound — is safe: nothing in it waits on the GPU.
		pr.Mount("/admin", adminHandler.Routes(tokens.Verify, cfg.RequestTimeout))

		// Which MCP servers this client may connect to. Outside the admin gate
		// because an ordinary user's browser needs the answer, and deliberately
		// a different handler from the admin listing: that one carries config
		// blobs which can hold upstream credentials.
		pr.With(common.RequireAuth(tokens.Verify)).Get("/mcp-servers", adminHandler.ClientServers)
	})

	// Mounted outside that group so its own routes can choose their bounds: the
	// short default for everything, the long one for generation.
	r.Mount("/llm", llmHandler.Routes(tokens.Verify, cfg.RequestTimeout, cfg.LLMTimeout))

	// Likewise: reads are short, one analysis waits on the GPU, and a trial
	// waits on it repeatedly. See analysis.Handler.Routes for the three bounds.
	r.Mount("/analysis", analysisHandler.Routes(tokens.Verify, cfg.RequestTimeout, cfg.LLMTimeout))

	// Model Context Protocol. Outside the short-bound group for the same reason
	// as the two above: a tools/call can run an analysis and wait on the GPU.
	r.Mount("/mcp", mcpServer.Routes(tokens.Verify, cfg.LLMTimeout))

	// WriteTimeout has to clear the longest legitimate handler, and one route
	// now runs ten generations back to back.
	//
	// This bit three trial runs before it was found, and the failure gives no
	// clue: the handler completes normally and logs its result, but the
	// connection was already cut, so the client sees an empty reply and the
	// server logs a success. Nothing anywhere says "write deadline". Deriving
	// the value from the route's own bound is what keeps the two from drifting
	// apart again the next time LLM_TIMEOUT is tuned.
	//
	// The precise per-route bounds come from common.Timeout; this is only the
	// coarse backstop that stops a stalled client holding a connection forever.
	writeTimeout := 30 * time.Second
	if longest := analysis.TrialTimeout(cfg.LLMTimeout) + time.Minute; longest > writeTimeout {
		writeTimeout = longest
	}

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
		WriteTimeout:      writeTimeout,
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
