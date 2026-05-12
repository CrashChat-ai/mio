package config

import (
	"strings"
	"testing"
)

// setRequired populates the minimum required env so Load() succeeds; tests
// that focus on MIO_ENV layer additional setenv on top.
func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("MIO_TENANT_ID", "11111111-1111-1111-1111-111111111111")
	t.Setenv("MIO_ACCOUNT_ID", "22222222-2222-2222-2222-222222222222")
	t.Setenv("MIO_POSTGRES_DSN", "postgres://localhost/test")
}

func TestEnvValidation_DefaultsToDev(t *testing.T) {
	setRequired(t)
	t.Setenv("MIO_ENV", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Env != "dev" {
		t.Errorf("expected default 'dev', got %q", cfg.Env)
	}
}

func TestEnvValidation_AcceptsAllowed(t *testing.T) {
	for _, e := range []string{"dev", "staging", "prod"} {
		t.Run(e, func(t *testing.T) {
			setRequired(t)
			t.Setenv("MIO_ENV", e)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load(%s): %v", e, err)
			}
			if cfg.Env != e {
				t.Errorf("got %q, want %q", cfg.Env, e)
			}
		})
	}
}

func TestEnvValidation_RejectsBogus(t *testing.T) {
	setRequired(t)
	t.Setenv("MIO_ENV", "production")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for MIO_ENV=production")
	}
	if !strings.Contains(err.Error(), "MIO_ENV") {
		t.Errorf("error should mention MIO_ENV: %v", err)
	}
}
