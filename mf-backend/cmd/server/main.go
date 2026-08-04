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
	"github.com/emrah/mf-backend/internal/decision"
	"github.com/emrah/mf-backend/internal/docs"
	"github.com/emrah/mf-backend/internal/llm"
	"github.com/emrah/mf-backend/internal/mcp"
	"github.com/emrah/mf-backend/internal/obs"
	"github.com/emrah/mf-backend/internal/retention"
	"github.com/emrah/mf-backend/internal/settings"
	"github.com/emrah/mf-backend/internal/wiki"
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
	// The live-adapter control plane. Optional in the same way the provider is:
	// with no inference host there is nothing to swap on, the handler reports it
	// plainly, and activation still switches the compiled model id.
	//
	// Given its own short timeout rather than LLMTimeout, which is sized for
	// generation. These calls move no tokens; if one takes 25 seconds the engine
	// is wedged, and making the operator wait that long to learn it is not help.
	adapterRuntime := llm.NewAdapterRuntime(cfg.HotSwapURL(), cfg.LLMAPIKey, 15*time.Second)

	// The admin panel's charts.
	//
	// Sized against the hop, not against a local query: Prometheus is beside the
	// GPU, so every one of these crosses Cloudflare to a home connection, and
	// the four run together on connections that have usually gone idle since
	// the last visit. The first version gave them three seconds — under
	// RequestTimeout, which was the right instinct applied to the wrong number,
	// since it also sat under what the round trip legitimately costs.
	//
	// It stays comfortably below the route's own bound below, because that
	// ordering is what makes a switched-off box report itself as an unreachable
	// metrics store instead of as a request that ran out of time.
	metricsQuerier := obs.NewClient(cfg.MetricsQueryURL(), cfg.LLMAPIKey, 6*time.Second)
	adminHandler := admin.NewHandler(adminStore, settingsStore, adminStore, adminStore, adapterRuntime, metricsQuerier, cfg.BcryptCost)
	analysisStore := analysis.NewStore(pool)
	analysisHandler := analysis.NewHandler(analysisStore, llmProvider, settingsStore)

	// DeepKwiki: the searchable knowledge base and the grounded answers over it.
	// Shares the provider and the settings with the analysis engine so both
	// features follow the operator's model choice rather than each holding an
	// opinion about which model to use.
	wikiStore := wiki.NewStore(pool)
	wikiHandler := wiki.NewHandler(wikiStore, llmProvider, settingsStore)

	// The investment persona: one agent that researches a subject live and
	// reaches an investability verdict. It reuses the same inference host, the
	// same settings, and DeepKwiki's own store as a retrieval tool — it is an
	// orchestration, not a second model. Live web research is pluggable: Tavily
	// when SEARCH_PROVIDER/SEARCH_API_KEY are set, a keyless DuckDuckGo scrape
	// otherwise, so the persona runs on a fresh deployment with no account.
	//
	// The searcher gets a fraction of LLMTimeout, not the whole of it. Search and
	// generation are spent out of one route budget, so handing search the entire
	// deadline lets a slow provider consume it and leave nothing for the model —
	// and the error that surfaces is then the provider's own "the inference host
	// did not answer in time", which is a true statement about the clock and a
	// false one about the cause. Bounding search is what keeps that message
	// honest.
	searcher := decision.NewSearcher(cfg.SearchProvider, cfg.SearchAPIKey, cfg.SearchTimeout())
	// Say which provider won, at boot, because the fallback is silent by
	// design: asking for tavily without a key is not an error, it is a
	// DuckDuckGo scrape wearing the config of something else. The one place
	// that difference showed up was the research trail on a turn someone had
	// to run first — and only if they knew to look. `requested` is logged
	// beside `provider` so the two disagreeing is legible as the thing it is.
	slog.Info("live search configured",
		"provider", searcher.Name(),
		"requested", cfg.SearchProvider,
		"keyed", cfg.SearchAPIKey != "",
		"timeout", cfg.SearchTimeout())
	decisionAgent := decision.NewAgent(llmProvider, searcher, wikiStore, settingsStore, cfg.LLMMaxPromptTokens, cfg.LLMTimeout)
	decisionStore := decision.NewStore(pool)
	decisionHandler := decision.NewHandler(decisionAgent, decisionStore)

	// The analysis engine's second caller. It runs the same code the HTTP path
	// does rather than a parallel implementation: an MCP client and a browser
	// must get identical reports from identical input, and two implementations
	// would drift on the first change to the prompt, parser or scoring.
	mcpServer := mcp.NewServer(analysisHandler, wikiHandler, cfg.AppName, cfg.AppVersion)

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

		// Which MCP servers this client may connect to. Outside the admin gate
		// because an ordinary user's browser needs the answer, and deliberately
		// a different handler from the admin listing: that one carries config
		// blobs which can hold upstream credentials.
		pr.With(common.RequireAuth(tokens.Verify), common.RequirePasswordFresh).Get("/mcp-servers", adminHandler.ClientServers)
	})

	// Control plane. Mounted outside the group for the same reason as the
	// routers below rather than because anything here is slow: all but one of
	// its routes are database calls, and that one reads Prometheus across the
	// tunnel. Nested inside the short bound, the longer one it declares for
	// that route would have been silently overridden — the exact failure that
	// moved generation out of the group in the first place.
	//
	// Twelve seconds is the outer bound, above the six the query client is
	// given, so an unreachable box is described by the client's own error
	// rather than by a deadline landing on the request.
	r.Mount("/admin", adminHandler.Routes(tokens.Verify, cfg.RequestTimeout, 12*time.Second))

	// Mounted outside that group so its own routes can choose their bounds: the
	// short default for everything, the long one for generation.
	r.Mount("/llm", llmHandler.Routes(tokens.Verify, cfg.RequestTimeout, cfg.LLMTimeout))

	// Likewise: reads are short, one analysis waits on the GPU, and a trial
	// waits on it repeatedly. See analysis.Handler.Routes for the three bounds.
	r.Mount("/analysis", analysisHandler.Routes(tokens.Verify, cfg.RequestTimeout, cfg.LLMTimeout))

	// Same reason as analysis: /wiki/ask waits on the GPU, /wiki/search does
	// not, so the router picks its own bounds per route rather than inheriting
	// one that would have to be wrong for one of them.
	r.Mount("/wiki", wikiHandler.Routes(tokens.Verify, cfg.RequestTimeout, cfg.LLMTimeout))

	// The investment persona. One slow route — it researches live and then waits
	// on the GPU — so it takes the generation timeout throughout.
	r.Mount("/decision", decisionHandler.Routes(tokens.Verify, cfg.RequestTimeout, cfg.DecisionChatTimeout()))

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

	// ---- background: redact content past the retention period ----
	if cfg.RetentionEnabled() {
		go retentionCleanup(workerCtx, analysisStore, llmStore, decisionStore,
			time.Duration(cfg.RetentionDays)*24*time.Hour, cfg.RetentionSweepInterval)
	}

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

// retentionCleanup redacts content past the retention period, on the same
// shape as sessionCleanup: once at boot, then on a ticker, stopping with the
// worker context.
//
// Once at boot matters more here than for sessions. A deploy that has been down
// over the weekend comes back with rows already past their limit, and waiting a
// full interval to act on them would mean the stated period is only true when
// the process happens to have been running.
func retentionCleanup(
	ctx context.Context,
	a retention.AssessmentSweeper,
	r retention.RunSweeper,
	c retention.ConversationSweeper,
	age, interval time.Duration,
) {
	// Ten minutes, where sessionCleanup's reap gets thirty seconds, and the gap
	// is deliberate. That reap deletes by an indexed expiry and touches a table
	// that is trimmed hourly, so it is always small. This one issues two UPDATEs
	// that rewrite every row past the limit, and the first run after a deploy is
	// the whole backlog at once — on a database that has been collecting reports
	// for a month while this feature did not exist yet, that is every row in the
	// table. A bound sized for the steady state would cancel exactly the sweep
	// that matters most and leave the backlog for the next tick to fail at again.
	//
	// Bounded all the same: workerCtx only closes at shutdown, so an UPDATE that
	// blocks on a lock would otherwise hold a connection for the process's life
	// and take its lock with it.
	const sweepTimeout = 10 * time.Minute

	sweep := func() {
		sweepCtx, cancel := context.WithTimeout(ctx, sweepTimeout)
		defer cancel()
		res, err := retention.Sweep(sweepCtx, a, r, c, age, time.Now())
		if err != nil {
			slog.Error("retention sweep", "error", err)
		}
		if res.Assessments > 0 || res.Runs > 0 || res.Conversations > 0 {
			slog.Info("retention sweep",
				"assessments", res.Assessments, "runs", res.Runs,
				"conversations", res.Conversations)
		}
	}

	sweep()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweep()
		}
	}
}
