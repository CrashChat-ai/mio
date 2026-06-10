package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeProvider struct {
	identity Identity
}

func (f fakeProvider) AuthCodeURL(state, verifier string) (string, error) {
	return "/fake-google?state=" + state + "&verifier=" + verifier, nil
}

func (f fakeProvider) Exchange(context.Context, string, string) (Identity, error) {
	return f.identity, nil
}

func TestAllowlist(t *testing.T) {
	allowlist := ParseAllowlist("Alice@Example.com", "@ops.example.com")
	if !allowlist.Allows("alice@example.com") {
		t.Fatal("exact email should be allowed")
	}
	if !allowlist.Allows("bob@ops.example.com") {
		t.Fatal("domain should be allowed")
	}
	if allowlist.Allows("eve@example.net") {
		t.Fatal("unexpected allow")
	}
}

func TestMemoryStoreLifecycle(t *testing.T) {
	store := NewMemoryStore()
	raw, session, err := store.Create(context.Background(), Identity{Email: "a@example.com"}, time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if raw == "" || session.TokenHash == "" {
		t.Fatal("empty token")
	}
	got, err := store.Get(context.Background(), raw)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Operator.Email != "a@example.com" {
		t.Fatalf("email: %q", got.Operator.Email)
	}
	if err := store.Delete(context.Background(), raw); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(context.Background(), raw); err != ErrNoSession {
		t.Fatalf("Get after delete: %v", err)
	}
}

func TestManagerDevLoginRequiresAllowlist(t *testing.T) {
	manager, err := NewManager(Config{
		Mode:        ModeDev,
		Store:       NewMemoryStore(),
		Allowlist:   ParseAllowlist("operator@example.com", ""),
		DevIdentity: Identity{Email: "operator@example.com", Name: "Operator"},
		StateSecret: []byte("test-secret-test-secret-test-secret"),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	rec := httptest.NewRecorder()
	manager.HandleLogin(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("session cookie not set")
	}
}

func TestManagerGoogleCallbackCreatesAllowedSession(t *testing.T) {
	manager, err := NewManager(Config{
		Mode:      ModeGoogle,
		Provider:  fakeProvider{identity: Identity{Email: "allowed@example.com", Name: "Allowed"}},
		Store:     NewMemoryStore(),
		Allowlist: ParseAllowlist("allowed@example.com", ""),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	loginReq := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	loginRec := httptest.NewRecorder()
	manager.HandleLogin(loginRec, loginReq)
	if loginRec.Code != http.StatusFound {
		t.Fatalf("login status: %d body=%s", loginRec.Code, loginRec.Body.String())
	}
	stateCookie := findCookie(t, loginRec.Result().Cookies(), manager.stateCookieName)
	location := loginRec.Result().Header.Get("Location")
	idx := strings.Index(location, "state=")
	if idx < 0 {
		t.Fatalf("login redirect missing state: %s", location)
	}
	state := location[idx+len("state="):]
	if cut := strings.IndexByte(state, '&'); cut >= 0 {
		state = state[:cut]
	}

	callbackReq := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+state+"&code=abc", nil)
	callbackReq.AddCookie(stateCookie)
	callbackRec := httptest.NewRecorder()
	manager.HandleCallback(callbackRec, callbackReq)
	if callbackRec.Code != http.StatusFound {
		t.Fatalf("callback status: %d body=%s", callbackRec.Code, callbackRec.Body.String())
	}
	sessionCookie := findCookie(t, callbackRec.Result().Cookies(), manager.sessionCookieName)

	sessionReq := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	sessionReq.AddCookie(sessionCookie)
	sessionRec := httptest.NewRecorder()
	manager.HandleSession(sessionRec, sessionReq)
	if sessionRec.Code != http.StatusOK {
		t.Fatalf("session status: %d", sessionRec.Code)
	}
	if !strings.Contains(sessionRec.Body.String(), `"authenticated":true`) {
		t.Fatalf("session body: %s", sessionRec.Body.String())
	}
}

func findCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q not found", name)
	return nil
}
