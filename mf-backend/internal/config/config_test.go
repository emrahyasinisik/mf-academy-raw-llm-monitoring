package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func prodConfig() Config {
	return Config{
		Env:            "production",
		JWTSecret:      strings.Repeat("k", 48),
		CORSOrigins:    []string{"https://app.example.com"},
		TrustProxy:     true,
		AccessTokenTTL: 15 * time.Minute,
	}
}

// The failure this guards against is silent: without it the service starts
// happily on a secret published in this repository, and anyone who reads it can
// sign a token for any user.
func TestValidateRejectsDefaultSecretInProduction(t *testing.T) {
	cfg := prodConfig()
	cfg.JWTSecret = InsecureDefaultSecret

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error for the development default secret")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("error %q does not mention JWT_SECRET", err)
	}
}

func TestValidateRejectsShortAndEmptySecrets(t *testing.T) {
	for name, secret := range map[string]string{
		"empty": "",
		"short": "tooshort",
	} {
		t.Run(name, func(t *testing.T) {
			cfg := prodConfig()
			cfg.JWTSecret = secret
			if err := cfg.Validate(); err == nil {
				t.Errorf("Validate() = nil for a %s secret, want an error", name)
			}
		})
	}
}

func TestValidateRejectsWildcardCORS(t *testing.T) {
	cfg := prodConfig()
	cfg.CORSOrigins = []string{"https://app.example.com", "*"}

	if err := cfg.Validate(); err == nil {
		t.Error("Validate() = nil for a wildcard origin, want an error")
	}
}

func TestValidateAcceptsSoundProductionConfig(t *testing.T) {
	if err := prodConfig().Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// Development must stay frictionless: the whole point of the default secret is
// that a contributor can clone and run without configuring anything.
func TestValidateIgnoresDevelopment(t *testing.T) {
	cfg := prodConfig()
	cfg.Env = "development"
	cfg.JWTSecret = InsecureDefaultSecret

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v in development, want nil", err)
	}
}

// TRUST_PROXY being wrong breaks rate limiting rather than authentication, so
// it warns instead of refusing to boot — but it must not pass unnoticed.
func TestWarningsFlagUntrustedProxyInProduction(t *testing.T) {
	cfg := prodConfig()
	cfg.TrustProxy = false

	warnings := cfg.Warnings()
	if len(warnings) == 0 {
		t.Fatal("Warnings() is empty, want a note about TRUST_PROXY")
	}
	if !strings.Contains(strings.Join(warnings, " "), "TRUST_PROXY") {
		t.Errorf("warnings %v do not mention TRUST_PROXY", warnings)
	}
}

func TestDecisionChatTimeoutCoversSearchAndGeneration(t *testing.T) {
	cfg := Config{LLMTimeout: 120 * time.Second}
	search := cfg.SearchTimeout()
	got := cfg.DecisionChatTimeout()
	want := cfg.LLMTimeout + search + 15*time.Second
	if got != want {
		t.Fatalf("DecisionChatTimeout() = %v, want %v", got, want)
	}
	if got <= cfg.LLMTimeout {
		t.Fatalf("DecisionChatTimeout() = %v must exceed LLMTimeout %v", got, cfg.LLMTimeout)
	}
}

// The analysis screen sizes its case field against this number, and the case
// field is the first thing anyone touches. Published rather than duplicated on
// the client: a client-side copy drifts silently until a case that looked
// acceptable comes back as a raw 400 from the engine.
// Env must be "production": Warnings() returns nil outside it by design, so a
// zero-value Config would pass this test while saying nothing at all.
func TestRetentionDisabledIsWarnedAbout(t *testing.T) {
	c := Config{Env: "production", RetentionDays: 0}
	if c.RetentionEnabled() {
		t.Error("RetentionEnabled() = true for 0 days")
	}
	var found bool
	for _, w := range c.Warnings() {
		if strings.Contains(w, "RETENTION_DAYS") {
			found = true
		}
	}
	if !found {
		t.Error("Warnings() says nothing about retention being off")
	}
}

func TestRetentionEnabledByDefaultValue(t *testing.T) {
	if !(Config{RetentionDays: 30}).RetentionEnabled() {
		t.Error("RetentionEnabled() = false for 30 days")
	}
}

func TestConfigHandlerPublishesPromptWindow(t *testing.T) {
	h := NewHandler(Config{AppName: "mf", AppVersion: "test", Env: "test", LLMMaxPromptTokens: 1200})

	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	rec := httptest.NewRecorder()
	h.Config(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Limits struct {
			MaxPromptTokens int `json:"max_prompt_tokens"`
		} `json:"limits"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Limits.MaxPromptTokens != 1200 {
		t.Errorf("max_prompt_tokens = %d, want 1200", body.Limits.MaxPromptTokens)
	}
}

// time.NewTicker panics on a non-positive duration and the sweep's ticker lives
// in a goroutine with no recover, so RETENTION_SWEEP_INTERVAL=0 — which
// time.ParseDuration accepts — would take the process down on boot and keep
// taking it down. RETENTION_DAYS=0 is the documented off switch; this one is
// not, and Load has to make that safe rather than trusting the operator to
// notice the difference.
func TestSweepIntervalIsFlooredToAPositiveDuration(t *testing.T) {
	for _, raw := range []string{"0", "0s", "-1h"} {
		t.Setenv("RETENTION_SWEEP_INTERVAL", raw)
		got := Load().RetentionSweepInterval
		if got <= 0 {
			t.Errorf("RETENTION_SWEEP_INTERVAL=%q gave %v; time.NewTicker panics on this", raw, got)
		}
		if got != minSweepInterval {
			t.Errorf("RETENTION_SWEEP_INTERVAL=%q gave %v, want the floor %v", raw, got, minSweepInterval)
		}
	}
}

// The floor must not quietly override an operator who asked for something
// slower than it — it is a floor, not a default.
func TestSweepIntervalKeepsAGenerousSetting(t *testing.T) {
	t.Setenv("RETENTION_SWEEP_INTERVAL", "12h")
	if got := Load().RetentionSweepInterval; got != 12*time.Hour {
		t.Errorf("RetentionSweepInterval = %v, want 12h", got)
	}
}
