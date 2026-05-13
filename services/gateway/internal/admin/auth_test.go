package admin

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseAllowedCIDRs_LoopbackImplicit(t *testing.T) {
	a, err := ParseAllowedCIDRs("")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, ip := range []string{"127.0.0.1", "127.0.0.5", "::1"} {
		if !a.Contains(net.ParseIP(ip)) {
			t.Errorf("loopback %s not allowed", ip)
		}
	}
	if a.Contains(net.ParseIP("192.168.1.1")) {
		t.Errorf("192.168 unexpectedly allowed")
	}
}

func TestParseAllowedCIDRs_OperatorList(t *testing.T) {
	a, err := ParseAllowedCIDRs("10.0.0.0/8, 192.168.1.0/24")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, ip := range []string{"10.5.5.5", "192.168.1.100", "127.0.0.1"} {
		if !a.Contains(net.ParseIP(ip)) {
			t.Errorf("%s should be allowed", ip)
		}
	}
	if a.Contains(net.ParseIP("172.16.1.1")) {
		t.Errorf("172.16 should NOT be allowed")
	}
}

func TestParseAllowedCIDRs_BadEntry(t *testing.T) {
	if _, err := ParseAllowedCIDRs("not-a-cidr"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthMiddleware_AllowsLoopback(t *testing.T) {
	allowed, _ := ParseAllowedCIDRs("")
	mw := AuthMiddleware(allowed, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("loopback: got %d, want 200", rec.Code)
	}
}

func TestAuthMiddleware_RejectsExternal(t *testing.T) {
	allowed, _ := ParseAllowedCIDRs("")
	mw := AuthMiddleware(allowed, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	req.RemoteAddr = "192.168.1.50:54321"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("external: got %d, want 403", rec.Code)
	}
}

func TestAuthMiddleware_MalformedRemote(t *testing.T) {
	allowed, _ := ParseAllowedCIDRs("")
	mw := AuthMiddleware(allowed, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/foo", nil)
	req.RemoteAddr = "not-an-address"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("malformed: got %d, want 403", rec.Code)
	}
}
