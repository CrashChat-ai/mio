package webembed

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected HTML content type, got %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("expected no-cache for index, got %q", rec.Header().Get("Cache-Control"))
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty index body")
	}
}

func TestHandlerFallsBackToIndexForClientRoutes(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tenants/acme/accounts", nil)

	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected HTML content type, got %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("expected no-cache for SPA fallback, got %q", rec.Header().Get("Cache-Control"))
	}
}

func TestCacheControl(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "hashed asset", path: "assets/index-BXQpY7QM.js", want: "public, max-age=31536000, immutable"},
		{name: "hashed css", path: "assets/index-BXQpY7QM.css", want: "public, max-age=31536000, immutable"},
		{name: "index", path: "index.html", want: "no-cache"},
		{name: "root level file", path: "favicon.ico", want: "no-cache"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cacheControl(tt.path); got != tt.want {
				t.Fatalf("cacheControl(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
