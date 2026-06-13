package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestParseCORSOrigins(t *testing.T) {
	got := ParseCORSOrigins(" https://a.example.com/ , ,https://b.example.com ")
	if len(got) != 2 || got[0] != "https://a.example.com" || got[1] != "https://b.example.com" {
		t.Fatalf("origins: %#v", got)
	}
	if len(ParseCORSOrigins("")) != 0 {
		t.Fatal("empty spec should yield no origins")
	}
}

func TestCORSDisabledByDefault(t *testing.T) {
	h := CORS(CORSConfig{}, okHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no ACAO header, got %q", got)
	}
}

func TestCORSAllowedOrigin(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"https://app.example.com"}}, okHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("ACAO: %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("ACAC: %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary: %q", got)
	}
}

func TestCORSDeniedOrigin(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"https://app.example.com"}}, okHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no ACAO for denied origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("expected no ACAC for denied origin, got %q", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"https://app.example.com"}}, okHandler())
	req := httptest.NewRequest(http.MethodOptions, "/api/admin/tenants", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status: %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("preflight missing allow-methods")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("preflight missing allow-headers")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("preflight ACAO: %q", got)
	}
}
