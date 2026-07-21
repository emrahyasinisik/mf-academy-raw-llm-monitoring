package config

import (
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
