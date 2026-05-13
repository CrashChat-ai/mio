package admin

import (
	"log/slog"
	"net/http"
)

// OAuthCallbackHandler returns an http.HandlerFunc that lands the OAuth
// `code` query param against the install_id resolved from `state`. It
// shares the auth middleware with the connect-go listener so prod deploys
// must front this with TLS — `code` and `state` are URL-query params and
// MUST NOT be logged.
//
// Response shape:
//   - 200 + plain text "Install captured — return to the admin UI/TUI."
//   - 400 on missing/unknown state
//
// CompleteInstall (a separate RPC) consumes the stashed code. The window
// is installStashTTL (60s); operators that take longer must restart the
// install via StartInstall.
func OAuthCallbackHandler(server *AdminServer, logger *slog.Logger) http.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")
		if state == "" || code == "" {
			// Do not log state/code values.
			logger.Warn("admin: /oauth/callback missing state or code",
				"has_state", state != "",
				"has_code", code != "")
			http.Error(w, "missing state or code", http.StatusBadRequest)
			return
		}
		installID := server.Stash().capture(state, code)
		if installID == "" {
			// Unknown state → 400. Either operator hit refresh, the install
			// expired, or this is a CSRF probe. Do not echo state/code.
			logger.Warn("admin: /oauth/callback unknown state",
				"remote_addr", r.RemoteAddr)
			if server.Metrics != nil {
				server.Metrics.OAuthTotal.WithLabelValues("unknown", "callback_bad_state").Inc()
			}
			http.Error(w, "unknown or expired state", http.StatusBadRequest)
			return
		}
		if server.Metrics != nil {
			server.Metrics.OAuthTotal.WithLabelValues("unknown", "callback").Inc()
		}
		logger.Info("admin: /oauth/callback captured", "install_id", installID)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Install captured — return to the admin UI/TUI to confirm via CompleteInstall."))
	}
}
