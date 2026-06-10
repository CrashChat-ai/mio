package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	ModeGoogle = "google"
	ModeDev    = "dev"
)

type Identity struct {
	Email     string
	Name      string
	AvatarURL string
}

type Provider interface {
	AuthCodeURL(state, codeVerifier string) (string, error)
	Exchange(ctx context.Context, code, codeVerifier string) (Identity, error)
}

type Allowlist struct {
	emails  map[string]struct{}
	domains map[string]struct{}
}

func ParseAllowlist(emailsSpec, domainsSpec string) Allowlist {
	return Allowlist{
		emails:  parseSet(emailsSpec, false),
		domains: parseSet(domainsSpec, true),
	}
}

func (a Allowlist) Allows(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	if _, ok := a.emails[email]; ok {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	_, ok := a.domains[email[at+1:]]
	return ok
}

type Config struct {
	Mode              string
	Provider          Provider
	Store             Store
	Allowlist         Allowlist
	DevIdentity       Identity
	SessionTTL        time.Duration
	SessionCookieName string
	StateCookieName   string
	CookieSecure      bool
	StateSecret       []byte
	Logger            *slog.Logger
}

type Manager struct {
	mode              string
	provider          Provider
	store             Store
	allowlist         Allowlist
	devIdentity       Identity
	sessionTTL        time.Duration
	sessionCookieName string
	stateCookieName   string
	cookieSecure      bool
	stateSecret       []byte
	logger            *slog.Logger
}

func NewManager(cfg Config) (*Manager, error) {
	if cfg.Mode == "" {
		cfg.Mode = ModeGoogle
	}
	if cfg.Mode != ModeGoogle && cfg.Mode != ModeDev {
		return nil, fmt.Errorf("auth: unsupported mode %q", cfg.Mode)
	}
	if cfg.Store == nil {
		return nil, errors.New("auth: store required")
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	if cfg.SessionCookieName == "" {
		cfg.SessionCookieName = "mio_web_session"
	}
	if cfg.StateCookieName == "" {
		cfg.StateCookieName = "mio_web_oauth"
	}
	if len(cfg.StateSecret) == 0 {
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("auth: generate state secret: %w", err)
		}
		cfg.StateSecret = secret
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Manager{
		mode:              cfg.Mode,
		provider:          cfg.Provider,
		store:             cfg.Store,
		allowlist:         cfg.Allowlist,
		devIdentity:       cfg.DevIdentity,
		sessionTTL:        cfg.SessionTTL,
		sessionCookieName: cfg.SessionCookieName,
		stateCookieName:   cfg.StateCookieName,
		cookieSecure:      cfg.CookieSecure,
		stateSecret:       cfg.StateSecret,
		logger:            cfg.Logger,
	}, nil
}

func (m *Manager) Mode() string {
	return m.mode
}

func (m *Manager) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := m.CurrentSession(r.Context(), r)
		if err != nil {
			writeAuthJSON(w, http.StatusUnauthorized, map[string]any{
				"error": "unauthorized",
			})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionContextKey{}, session)))
	})
}

func (m *Manager) CurrentSession(ctx context.Context, r *http.Request) (Session, error) {
	cookie, err := r.Cookie(m.sessionCookieName)
	if err != nil || cookie.Value == "" {
		return Session{}, ErrNoSession
	}
	return m.store.Get(ctx, cookie.Value)
}

func SessionFromContext(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(sessionContextKey{}).(Session)
	return session, ok
}

func (m *Manager) HandleSession(w http.ResponseWriter, r *http.Request) {
	session, err := m.CurrentSession(r.Context(), r)
	if err != nil {
		writeAuthJSON(w, http.StatusOK, map[string]any{
			"authenticated": false,
			"authMode":      m.mode,
		})
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"authMode":      m.mode,
		"operator":      session.Operator,
	})
}

func (m *Manager) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeAuthJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	if m.mode == ModeDev {
		identity := m.devIdentity
		if identity.Email == "" {
			identity.Email = "operator@localhost"
		}
		if identity.Name == "" {
			identity.Name = identity.Email
		}
		if err := m.createSession(w, r, identity); err != nil {
			m.logger.Warn("mio-web dev login rejected", "email", identity.Email, "error", err)
			writeAuthJSON(w, http.StatusForbidden, map[string]any{"error": "operator_not_allowed"})
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if m.provider == nil {
		writeAuthJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "google_login_not_configured"})
		return
	}
	state, err := randomURLToken(32)
	if err != nil {
		writeAuthJSON(w, http.StatusInternalServerError, map[string]any{"error": "state_failed"})
		return
	}
	verifier, err := randomURLToken(64)
	if err != nil {
		writeAuthJSON(w, http.StatusInternalServerError, map[string]any{"error": "verifier_failed"})
		return
	}
	loginURL, err := m.provider.AuthCodeURL(state, verifier)
	if err != nil {
		writeAuthJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "google_login_not_configured"})
		return
	}
	m.setStateCookie(w, oauthState{
		State:        state,
		CodeVerifier: verifier,
		ExpiresAt:    time.Now().UTC().Add(10 * time.Minute).Unix(),
	})
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func (m *Manager) HandleCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeAuthJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
		m.clearStateCookie(w)
		writeAuthJSON(w, http.StatusUnauthorized, map[string]any{"error": oauthErr})
		return
	}
	if m.provider == nil {
		writeAuthJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "google_login_not_configured"})
		return
	}
	stateCookie, err := r.Cookie(m.stateCookieName)
	if err != nil {
		writeAuthJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing_state"})
		return
	}
	state, err := m.decodeState(stateCookie.Value)
	if err != nil {
		m.clearStateCookie(w)
		writeAuthJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_state"})
		return
	}
	if queryState := r.URL.Query().Get("state"); queryState == "" || !hmac.Equal([]byte(queryState), []byte(state.State)) {
		m.clearStateCookie(w)
		writeAuthJSON(w, http.StatusUnauthorized, map[string]any{"error": "state_mismatch"})
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		m.clearStateCookie(w)
		writeAuthJSON(w, http.StatusBadRequest, map[string]any{"error": "missing_code"})
		return
	}
	identity, err := m.provider.Exchange(r.Context(), code, state.CodeVerifier)
	if err != nil {
		m.clearStateCookie(w)
		m.logger.Warn("mio-web google login failed", "error", err)
		writeAuthJSON(w, http.StatusUnauthorized, map[string]any{"error": "login_failed"})
		return
	}
	if err := m.createSession(w, r, identity); err != nil {
		m.clearStateCookie(w)
		m.logger.Warn("mio-web operator rejected", "email", identity.Email, "error", err)
		writeAuthJSON(w, http.StatusForbidden, map[string]any{"error": "operator_not_allowed"})
		return
	}
	m.clearStateCookie(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (m *Manager) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAuthJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	if cookie, err := r.Cookie(m.sessionCookieName); err == nil && cookie.Value != "" {
		if err := m.store.Delete(r.Context(), cookie.Value); err != nil {
			m.logger.Warn("mio-web logout delete failed", "error", err)
		}
	}
	m.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) createSession(w http.ResponseWriter, r *http.Request, identity Identity) error {
	identity.Email = strings.ToLower(strings.TrimSpace(identity.Email))
	if !m.allowlist.Allows(identity.Email) {
		return fmt.Errorf("email %q not in operator allowlist", identity.Email)
	}
	raw, session, err := m.store.Create(r.Context(), identity, m.sessionTTL)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     m.sessionCookieName,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  session.Operator.ExpiresAt,
		MaxAge:   int(time.Until(session.Operator.ExpiresAt).Seconds()),
	})
	return nil
}

func (m *Manager) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   m.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (m *Manager) setStateCookie(w http.ResponseWriter, state oauthState) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.stateCookieName,
		Value:    m.encodeState(state),
		Path:     "/auth/callback",
		HttpOnly: true,
		Secure:   m.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(state.ExpiresAt, 0).UTC(),
		MaxAge:   int(time.Until(time.Unix(state.ExpiresAt, 0)).Seconds()),
	})
}

func (m *Manager) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.stateCookieName,
		Value:    "",
		Path:     "/auth/callback",
		HttpOnly: true,
		Secure:   m.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

type oauthState struct {
	State        string `json:"state"`
	CodeVerifier string `json:"codeVerifier"`
	ExpiresAt    int64  `json:"expiresAt"`
}

func (m *Manager) encodeState(state oauthState) string {
	payload, _ := json.Marshal(state)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, m.stateSecret)
	_, _ = mac.Write([]byte(encodedPayload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedPayload + "." + sig
}

func (m *Manager) decodeState(raw string) (oauthState, error) {
	payload, sig, ok := strings.Cut(raw, ".")
	if !ok {
		return oauthState{}, errors.New("missing signature")
	}
	mac := hmac.New(sha256.New, m.stateSecret)
	_, _ = mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return oauthState{}, errors.New("bad signature")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return oauthState{}, err
	}
	var state oauthState
	if err := json.Unmarshal(decoded, &state); err != nil {
		return oauthState{}, err
	}
	if state.State == "" || state.CodeVerifier == "" || time.Now().UTC().Unix() > state.ExpiresAt {
		return oauthState{}, errors.New("expired or incomplete state")
	}
	return state, nil
}

func randomURLToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func parseSet(spec string, trimAt bool) map[string]struct{} {
	out := map[string]struct{}{}
	for _, raw := range strings.Split(spec, ",") {
		item := strings.ToLower(strings.TrimSpace(raw))
		if item == "" {
			continue
		}
		if trimAt {
			item = strings.TrimPrefix(item, "@")
		}
		out[item] = struct{}{}
	}
	return out
}

func writeAuthJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type sessionContextKey struct{}
