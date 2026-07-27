// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the server.
type Config struct {
	Env             string
	Port            string
	DatabaseURL     string
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	CORSOrigins     []string
	AppName         string
	AppVersion      string
	// TrustProxy states whether a proxy we control sits in front of this
	// process. Only then may X-Forwarded-For be believed: the header is
	// client-supplied, so trusting it on a directly-exposed instance lets any
	// caller forge a new identity per request and slip the rate limiter.
	TrustProxy bool
	// RequestTimeout bounds a single request end to end, including its database
	// work. See common.Timeout.
	RequestTimeout time.Duration
	// BcryptCost is the password hashing work factor. Tunable because the right
	// value depends on the hardware: each step doubles the CPU spent per login,
	// so a throttled shared vCPU may warrant a lower setting than a dedicated
	// one. The auth package enforces its own floor regardless of what is set.
	BcryptCost int

	// ---- Server-side inference (optional) ----
	//
	// Empty LLMBaseURL disables the server-side path entirely; the browser path
	// keeps working. That is deliberate — the inference host is a desktop
	// machine, and the API must not require it to be switched on.

	// LLMBaseURL is the root of an OpenAI-compatible API (no trailing /v1).
	LLMBaseURL string
	// LLMAPIKey is the shared secret the inference gateway checks.
	LLMAPIKey string
	// LLMHotSwapURL is the root of the runtime that can change LoRA adapters on
	// a live server (llama.cpp's /lora-adapters). Empty disables live swapping;
	// activation then only switches which compiled model is requested.
	//
	// Defaults to LLMBaseURL + "/rt", which is where the gateway routes it, so
	// the common deployment needs no extra variable. It is still overridable
	// because the two engines do not have to live behind the same gateway.
	LLMHotSwapURL string
	// LLMTimeout bounds a single generation. It is far larger than
	// RequestTimeout and is applied only to the generation route: raising the
	// global bound to suit the slowest endpoint would strip every other one of
	// its protection.
	LLMTimeout time.Duration
	// LLMMaxTokens caps generated length, bounding both the time a request can
	// occupy the GPU and the size of the row it writes.
	LLMMaxTokens int

	// AdminEmail names the one account promoted to the admin role at boot.
	//
	// Deliberately an environment variable and not an API call. Granting admin
	// over HTTP would mean any authentication flaw, anywhere, escalates to full
	// control of the inference host's settings; requiring a deployment variable
	// plus a restart caps the damage of such a flaw at ordinary user access.
	// Empty leaves every account unprivileged, which is the correct default for
	// a fresh database.
	AdminEmail string

	// MetricsToken guards GET /metrics. The Prometheus exposition is a map of
	// the service — every route, its traffic and its latency — so it is not
	// something to publish. Empty leaves the endpoint open, which is right for
	// local development and refused in production by Validate.
	MetricsToken string

	// SearchProvider selects how the decision agent researches live. "tavily"
	// uses the Tavily API (needs SearchAPIKey and returns extracted page
	// content); anything else falls back to a keyless DuckDuckGo HTML scrape,
	// which needs no account but returns only snippets and is best-effort.
	SearchProvider string
	// SearchAPIKey is the key for a keyed SearchProvider. Empty forces the
	// keyless fallback regardless of SearchProvider, so the agent still runs on
	// a fresh deployment — just with thinner evidence.
	SearchAPIKey string
}

// InsecureDefaultSecret is the development JWT secret. It is a known constant —
// it appears in .env.example and therefore in the repository's history — so a
// production process must never run with it. Exported so Validate and its tests
// refer to the same value rather than repeating the literal.
const InsecureDefaultSecret = "dev-insecure-secret-change-me"

// defaultBcryptCost mirrors auth.DefaultHashCost. It is duplicated rather than
// imported so configuration stays a leaf package that no feature package sits
// beneath; auth enforces its own floor on whatever value arrives.
const defaultBcryptCost = 12

// minSecretBytes is the floor for an HMAC key. HS256 keys shorter than the hash
// output weaken the signature, and a short secret is also a guessable one.
const minSecretBytes = 32

// Load reads configuration from the environment, applying sensible defaults
// for local development. Only DATABASE_URL and JWT_SECRET are effectively
// required in production.
func Load() Config {
	return Config{
		Env:             getEnv("APP_ENV", "development"),
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://emrah@localhost:5432/mf_monitoring?sslmode=disable"),
		JWTSecret:       getEnv("JWT_SECRET", InsecureDefaultSecret),
		AccessTokenTTL:  getDuration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: getDuration("REFRESH_TOKEN_TTL", 7*24*time.Hour),
		CORSOrigins:     getList("CORS_ORIGINS", []string{"http://localhost:3000"}),
		AppName:         getEnv("APP_NAME", "MasterFabric Raw LLM Monitoring & Decision Scoring"),
		AppVersion:      getEnv("APP_VERSION", "0.1.0"),
		// Defaults to false: a direct-exposed process is the unsafe case, so
		// the safe behaviour is what you get without configuring anything.
		// Render (and any reverse proxy deployment) should set TRUST_PROXY=true.
		TrustProxy:     getBool("TRUST_PROXY", false),
		RequestTimeout: getDuration("REQUEST_TIMEOUT", 5*time.Second),
		BcryptCost:     getInt("BCRYPT_COST", defaultBcryptCost),

		LLMBaseURL:    getEnv("LLM_BASE_URL", ""),
		LLMAPIKey:     getEnv("LLM_API_KEY", ""),
		LLMHotSwapURL: getEnv("LLM_HOTSWAP_URL", ""),
		// Provisional. The real figure comes from measuring the inference host;
		// it must stay below the server's WriteTimeout in cmd/server/main.go and
		// below the gateway's proxy timeouts in mf-inference/gateway/Caddyfile.
		LLMTimeout: getDuration("LLM_TIMEOUT", 25*time.Second),
		// A ceiling, not a default — the provider still falls back to a modest
		// 512 when a caller expresses no preference. Raised from 512 because
		// rubric analysis legitimately needs a few thousand output tokens
		// (roughly 260 per criterion), and the old ceiling silently truncated
		// every one of them.
		LLMMaxTokens: getInt("LLM_MAX_TOKENS", 4096),

		SearchProvider: getEnv("SEARCH_PROVIDER", ""),
		SearchAPIKey:   getEnv("SEARCH_API_KEY", ""),

		AdminEmail:   getEnv("ADMIN_EMAIL", ""),
		MetricsToken: getEnv("METRICS_TOKEN", ""),
	}
}

// ServerInferenceEnabled reports whether a server-side inference host is wired.
func (c Config) ServerInferenceEnabled() bool { return c.LLMBaseURL != "" }

// HotSwapURL is the effective address of the live-adapter runtime.
//
// Derived rather than required, because the gateway already routes /rt to it —
// so the ordinary deployment gets hot-swap by setting nothing, and an operator
// who has never heard of the variable still gets the feature. Returning ""
// when there is no inference host at all keeps the degradation consistent: no
// engine means no swapping, not a client pointed at "/rt".
func (c Config) HotSwapURL() string {
	if c.LLMHotSwapURL != "" {
		return c.LLMHotSwapURL
	}
	if c.LLMBaseURL == "" {
		return ""
	}
	return strings.TrimRight(c.LLMBaseURL, "/") + "/rt"
}

// MetricsEnabled reports whether GET /metrics should be served at all.
//
// Outside production an open endpoint is convenient and harmless. In production
// it is only served when a token guards it — the endpoint disappears rather
// than the process refusing to boot, so forgetting the variable degrades
// observability instead of taking the service down.
func (c Config) MetricsEnabled() bool {
	return !c.IsProduction() || c.MetricsToken != ""
}

// IsProduction reports whether the app runs in a production-like environment.
func (c Config) IsProduction() bool {
	return c.Env == "production"
}

// Validate refuses to start a production process whose configuration cannot be
// secure, and warns about settings that are merely suspect.
//
// The failure this exists to prevent is silent: with JWT_SECRET unset the
// service starts cleanly on a secret published in this repository, and anyone
// who reads it can sign a token for any user. Nothing in the logs or the health
// check would show it. A process that refuses to boot is recoverable in a way
// that a process serving forged tokens is not — so this is fatal, not a warning.
//
// Warnings, by contrast, cover configurations that are wrong in some
// deployments and correct in others, where refusing to start would be
// presumptuous.
func (c Config) Validate() error {
	if !c.IsProduction() {
		return nil
	}

	var problems []string
	switch {
	case c.JWTSecret == "":
		problems = append(problems, "JWT_SECRET is empty")
	case c.JWTSecret == InsecureDefaultSecret:
		problems = append(problems, "JWT_SECRET is still the development default, which is public in this repository")
	case len(c.JWTSecret) < minSecretBytes:
		problems = append(problems,
			fmt.Sprintf("JWT_SECRET is %d bytes; at least %d are required", len(c.JWTSecret), minSecretBytes))
	}

	for _, o := range c.CORSOrigins {
		if o == "*" {
			problems = append(problems, "CORS_ORIGINS contains '*', which allows any site to call this API")
		}
	}

	if c.MetricsToken != "" && len(c.MetricsToken) < minSecretBytes {
		problems = append(problems,
			fmt.Sprintf("METRICS_TOKEN is %d bytes; at least %d are required", len(c.MetricsToken), minSecretBytes))
	}

	if len(problems) > 0 {
		return fmt.Errorf("insecure production configuration: %s", strings.Join(problems, "; "))
	}
	return nil
}

// Warnings returns configuration that is probably wrong but not provably
// unsafe, so the operator sees it at boot without the process refusing to run.
func (c Config) Warnings() []string {
	if !c.IsProduction() {
		return nil
	}
	var w []string
	if !c.TrustProxy {
		w = append(w, "TRUST_PROXY is false: if a proxy fronts this service, every "+
			"client shares one rate-limit bucket and one abuser can lock everyone out")
	}
	if c.AccessTokenTTL > time.Hour {
		w = append(w, "ACCESS_TOKEN_TTL exceeds one hour; access tokens cannot be revoked before they expire")
	}
	if c.MetricsToken == "" {
		w = append(w, "METRICS_TOKEN is empty: the metrics endpoint is not served at all in production, "+
			"because publishing every route, its traffic and its latency is reconnaissance handed out for free")
	}
	if c.ServerInferenceEnabled() {
		if c.LLMAPIKey == "" {
			w = append(w, "LLM_BASE_URL is set without LLM_API_KEY: if the inference host "+
				"checks a secret this service cannot pass it, and if it does not, the GPU is open to anyone who finds the URL")
		}
		if strings.HasPrefix(c.LLMBaseURL, "http://") {
			w = append(w, "LLM_BASE_URL is plain http: the shared secret and every prompt cross the network in the clear")
		}
	}
	return w
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		// allow a plain integer number of seconds
		if secs, err := strconv.Atoi(v); err == nil {
			return time.Duration(secs) * time.Second
		}
	}
	return fallback
}

func getList(key string, fallback []string) []string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return fallback
}
