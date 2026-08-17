package config

import (
	"testing"
	"time"
)

func TestGetEnvHelpers(t *testing.T) {
	t.Run("getEnv", func(t *testing.T) {
		t.Setenv("CART_STR", "hello")
		if got := getEnv("CART_STR", "def"); got != "hello" {
			t.Errorf("getEnv set = %q, want hello", got)
		}
		if got := getEnv("CART_STR_UNSET", "def"); got != "def" {
			t.Errorf("getEnv unset = %q, want def", got)
		}
	})

	t.Run("getEnvBool", func(t *testing.T) {
		t.Setenv("CART_BOOL_YES", "yes")
		if !getEnvBool("CART_BOOL_YES", false) {
			t.Error(`getEnvBool("yes") = false, want true`)
		}
		t.Setenv("CART_BOOL_ZERO", "0")
		if getEnvBool("CART_BOOL_ZERO", true) {
			t.Error(`getEnvBool("0") = true, want false`)
		}
		if !getEnvBool("CART_BOOL_UNSET", true) {
			t.Error("getEnvBool unset = false, want default true")
		}
	})

	t.Run("getEnvInt", func(t *testing.T) {
		t.Setenv("CART_INT_OK", "42")
		if got := getEnvInt("CART_INT_OK", 7); got != 42 {
			t.Errorf("getEnvInt valid = %d, want 42", got)
		}
		t.Setenv("CART_INT_BAD", "nope")
		if got := getEnvInt("CART_INT_BAD", 7); got != 7 {
			t.Errorf("getEnvInt bad = %d, want default 7", got)
		}
		if got := getEnvInt("CART_INT_UNSET", 3); got != 3 {
			t.Errorf("getEnvInt unset = %d, want default 3", got)
		}
	})

	t.Run("getEnvFloat", func(t *testing.T) {
		t.Setenv("CART_FLOAT_OK", "0.25")
		if got := getEnvFloat("CART_FLOAT_OK", 1.0); got != 0.25 {
			t.Errorf("getEnvFloat valid = %v, want 0.25", got)
		}
		t.Setenv("CART_FLOAT_BAD", "x")
		if got := getEnvFloat("CART_FLOAT_BAD", 1.5); got != 1.5 {
			t.Errorf("getEnvFloat bad = %v, want default 1.5", got)
		}
		if got := getEnvFloat("CART_FLOAT_UNSET", 2.5); got != 2.5 {
			t.Errorf("getEnvFloat unset = %v, want default 2.5", got)
		}
	})
}

// getEnvDurationSeconds / getEnvDurationSecondsWithMax are generic helpers.
// Exercise them with varied keys AND defaults (not the production constants) so
// every branch is covered and the params are genuinely tested — which also keeps
// unparam quiet (it flags params that always receive the same value).
func TestGetEnvDurationSeconds(t *testing.T) {
	if got := getEnvDurationSeconds("DUR_UNSET", 10); got != 10 {
		t.Errorf("unset = %d, want default 10", got)
	}
	t.Setenv("DUR_VALID", "20s")
	if got := getEnvDurationSeconds("DUR_VALID", 7); got != 20 {
		t.Errorf("valid = %d, want 20", got)
	}
	t.Setenv("DUR_BAD", "bad")
	if got := getEnvDurationSeconds("DUR_BAD", 5); got != 5 {
		t.Errorf("invalid = %d, want default 5", got)
	}
	t.Setenv("DUR_OVER", "999s") // > 60s cap
	if got := getEnvDurationSeconds("DUR_OVER", 3); got != 3 {
		t.Errorf("over-max = %d, want default 3", got)
	}
}

func TestGetEnvDurationSecondsWithMax(t *testing.T) {
	if got := getEnvDurationSecondsWithMax("DURM_UNSET", 5, 30); got != 5 {
		t.Errorf("unset = %d, want default 5", got)
	}
	t.Setenv("DURM_VALID", "12s")
	if got := getEnvDurationSecondsWithMax("DURM_VALID", 2, 30); got != 12 {
		t.Errorf("valid = %d, want 12", got)
	}
	t.Setenv("DURM_BAD", "bad")
	if got := getEnvDurationSecondsWithMax("DURM_BAD", 4, 20); got != 4 {
		t.Errorf("invalid = %d, want default 4", got)
	}
	t.Setenv("DURM_OVER", "99s") // > 30s max
	if got := getEnvDurationSecondsWithMax("DURM_OVER", 6, 30); got != 6 {
		t.Errorf("over-max = %d, want default 6", got)
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  bool
	}{
		{"present case-insensitive", []string{"alpha", "Beta"}, "BETA", true},
		{"absent", []string{"alpha", "beta"}, "gamma", false},
		{"empty slice", nil, "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(tt.slice, tt.item); got != tt.want {
				t.Errorf("contains(%v, %q) = %v, want %v", tt.slice, tt.item, got, tt.want)
			}
		})
	}
}

func TestDurationGetters(t *testing.T) {
	c := &Config{}
	c.ShutdownTimeout = 15
	if got := c.GetShutdownTimeoutDuration(); got != 15*time.Second {
		t.Errorf("GetShutdownTimeoutDuration() = %v, want 15s", got)
	}
	c.ReadinessDrainDelay = 8
	if got := c.GetReadinessDrainDelayDuration(); got != 8*time.Second {
		t.Errorf("GetReadinessDrainDelayDuration() = %v, want 8s", got)
	}
}

func TestLoadAndValidate(t *testing.T) {
	t.Setenv("SERVICE_NAME", "cart")
	t.Setenv("PORT", "8080")
	t.Setenv("ENV", "production")
	t.Setenv("OTEL_COLLECTOR_ENDPOINT", "otel:4318")
	t.Setenv("OTEL_SAMPLE_RATE", "0.1")
	t.Setenv("PYROSCOPE_ENDPOINT", "pyro:4040")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("DB_HOST", "") // empty → database validation is skipped
	t.Setenv("SHUTDOWN_TIMEOUT", "12s")

	cfg := Load()
	if cfg.Service.Name != "cart" {
		t.Errorf("Service.Name = %q, want cart", cfg.Service.Name)
	}
	if cfg.ShutdownTimeout != 12 {
		t.Errorf("ShutdownTimeout = %d, want 12", cfg.ShutdownTimeout)
	}
	if cfg.Tracing.SampleRate != 0.1 {
		t.Errorf("Tracing.SampleRate = %v, want 0.1", cfg.Tracing.SampleRate)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() of a valid config = %v, want nil", err)
	}
}

func TestValidateDisabledTracingProfiling(t *testing.T) {
	c := &Config{
		Service:   ServiceConfig{Name: "cart", Port: "8080", Env: "dev"},
		Tracing:   TracingConfig{Enabled: false},
		Profiling: ProfilingConfig{Enabled: false},
		Logging:   LoggingConfig{Level: "debug", Format: "console"},
		Database:  DatabaseConfig{Host: ""},
	}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() with tracing/profiling disabled = %v, want nil", err)
	}
}

func TestValidateInvalid(t *testing.T) {
	// Every field is wrong, so each sub-validator contributes an error.
	c := &Config{
		Service:   ServiceConfig{Name: "", Port: "abc", Env: "nope"},
		Tracing:   TracingConfig{Enabled: true, Endpoint: "", SampleRate: 2.0, ServiceName: ""},
		Profiling: ProfilingConfig{Enabled: true, Endpoint: "", ServiceName: ""},
		Logging:   LoggingConfig{Level: "loud", Format: "xml"},
		Database:  DatabaseConfig{Host: "db", Name: "", User: "", Password: "", Port: "abc"},
	}
	if err := c.Validate(); err == nil {
		t.Fatal("Validate() of an invalid config = nil, want error")
	}
}

func TestEnvPredicates(t *testing.T) {
	tests := []struct {
		env       string
		dev, prod bool
	}{
		{"dev", true, false},
		{"development", true, false},
		{"prod", false, true},
		{"production", false, true},
		{"staging", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			c := &Config{Service: ServiceConfig{Env: tt.env}}
			if c.IsDevelopment() != tt.dev || c.IsProduction() != tt.prod {
				t.Errorf("env %q: IsDevelopment=%v IsProduction=%v, want dev=%v prod=%v",
					tt.env, c.IsDevelopment(), c.IsProduction(), tt.dev, tt.prod)
			}
		})
	}
}

// TestLoadOIDCDefaults pins the Keycloak realm defaults (ADR-041) so a
// regression back to the pre-OIDC auth-service defaults fails fast.
func TestLoadOIDCDefaults(t *testing.T) {
	t.Setenv("OIDC_ISSUER", "")
	t.Setenv("OIDC_AUDIENCE", "")
	t.Setenv("OIDC_JWKS_URL", "")
	cfg := Load()
	if want := "https://id.duynh.me/realms/duynhlab"; cfg.OIDCIssuer != want {
		t.Errorf("default OIDCIssuer = %q, want %q", cfg.OIDCIssuer, want)
	}
	if want := "duynhlab-platform"; cfg.OIDCAudience != want {
		t.Errorf("default OIDCAudience = %q, want %q", cfg.OIDCAudience, want)
	}
	if cfg.OIDCJWKSURL != "" {
		t.Errorf("default OIDCJWKSURL = %q, want empty (derived from issuer)", cfg.OIDCJWKSURL)
	}
}

func TestLoadOIDCOverrides(t *testing.T) {
	t.Setenv("OIDC_ISSUER", "http://keycloak.local-stack:8081/realms/duynhlab")
	t.Setenv("OIDC_AUDIENCE", "local-audience")
	t.Setenv("OIDC_JWKS_URL", "http://keycloak.local-stack:8081/realms/duynhlab/protocol/openid-connect/certs")

	cfg := Load()
	if want := "http://keycloak.local-stack:8081/realms/duynhlab"; cfg.OIDCIssuer != want {
		t.Errorf("OIDCIssuer = %q, want %q", cfg.OIDCIssuer, want)
	}
	if want := "local-audience"; cfg.OIDCAudience != want {
		t.Errorf("OIDCAudience = %q, want %q", cfg.OIDCAudience, want)
	}
	if want := "http://keycloak.local-stack:8081/realms/duynhlab/protocol/openid-connect/certs"; cfg.OIDCJWKSURL != want {
		t.Errorf("OIDCJWKSURL = %q, want %q", cfg.OIDCJWKSURL, want)
	}
}
