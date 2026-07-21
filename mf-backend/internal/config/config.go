// Package config loads application configuration from environment variables.
package config

import (
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
}

// Load reads configuration from the environment, applying sensible defaults
// for local development. Only DATABASE_URL and JWT_SECRET are effectively
// required in production.
func Load() Config {
	return Config{
		Env:             getEnv("APP_ENV", "development"),
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://emrah@localhost:5432/mf_monitoring?sslmode=disable"),
		JWTSecret:       getEnv("JWT_SECRET", "dev-insecure-secret-change-me"),
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
	}
}

// IsProduction reports whether the app runs in a production-like environment.
func (c Config) IsProduction() bool {
	return c.Env == "production"
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
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
