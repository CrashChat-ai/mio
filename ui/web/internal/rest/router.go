package rest

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	"github.com/crashchat-ai/mio/ui/web/internal/adminclient"
	"github.com/crashchat-ai/mio/ui/web/internal/audit"
	"github.com/crashchat-ai/mio/ui/web/internal/auth"
)

type Config struct {
	Admin  adminclient.Admin
	Auth   *auth.Manager
	Audit  audit.Logger
	Logger *slog.Logger
}

type Server struct {
	admin  adminclient.Admin
	auth   *auth.Manager
	audit  audit.Logger
	logger *slog.Logger
}

func New(cfg Config) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	s := &Server{
		admin:  cfg.Admin,
		auth:   cfg.Auth,
		audit:  cfg.Audit,
		logger: cfg.Logger,
	}
	if s.audit == nil {
		s.audit = audit.NewMemoryLogger()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", Health)
	mux.HandleFunc("/api/session", s.method(http.MethodGet, s.auth.HandleSession))
	mux.HandleFunc("/auth/login", s.auth.HandleLogin)
	mux.HandleFunc("/auth/callback", s.auth.HandleCallback)
	mux.HandleFunc("/auth/logout", s.auth.HandleLogout)

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/api/admin/tenants", s.handleTenants)
	adminMux.HandleFunc("/api/admin/tenants/{id}", s.method(http.MethodGet, s.handleGetTenant))
	adminMux.HandleFunc("/api/admin/channel-types", s.method(http.MethodGet, s.handleChannelTypes))
	adminMux.HandleFunc("/api/admin/accounts", s.handleAccounts)
	adminMux.HandleFunc("/api/admin/accounts/{id}", s.method(http.MethodGet, s.handleGetAccount))
	adminMux.HandleFunc("/api/admin/accounts/rate-limit", s.method(http.MethodPost, s.handleSetRateLimit))
	adminMux.HandleFunc("/api/admin/accounts/disable", s.method(http.MethodPost, s.handleDisableAccount))
	adminMux.HandleFunc("/api/admin/accounts/rotate-credential", s.method(http.MethodPost, s.handleRotateCredential))
	adminMux.HandleFunc("/api/admin/accounts/credential-metadata", s.method(http.MethodGet, s.handleCredentialMetadata))
	adminMux.HandleFunc("/api/admin/accounts/webhook-info", s.method(http.MethodGet, s.handleWebhookInfo))
	adminMux.HandleFunc("/api/admin/installs/start", s.method(http.MethodPost, s.handleStartInstall))
	adminMux.HandleFunc("/api/admin/installs/complete", s.method(http.MethodPost, s.handleCompleteInstall))
	adminMux.HandleFunc("/api/admin/messages/tail", s.method(http.MethodGet, s.handleTailMessages))
	adminMux.HandleFunc("/api/admin/stream-health", s.method(http.MethodGet, s.handleStreamHealth))
	adminMux.HandleFunc("/api/admin/audit", s.method(http.MethodGet, s.handleListAudit))
	mux.Handle("/api/admin/", s.auth.Require(adminMux))

	mux.HandleFunc("/", notFound)
	return mux
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
}

func (s *Server) method(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			w.Header().Set("Allow", method)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
			return
		}
		handler(w, r)
	}
}

func (s *Server) writeAdminError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	code := "admin_unavailable"
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		code = connectErr.Code().String()
		switch connectErr.Code() {
		case connect.CodeInvalidArgument:
			status = http.StatusBadRequest
		case connect.CodeUnauthenticated:
			status = http.StatusUnauthorized
		case connect.CodePermissionDenied:
			status = http.StatusForbidden
		case connect.CodeNotFound:
			status = http.StatusNotFound
		case connect.CodeUnimplemented:
			status = http.StatusNotImplemented
		default:
			status = http.StatusBadGateway
		}
	}
	s.logger.Warn("mio-web admin request failed", "status", status, "error", err)
	writeJSON(w, status, map[string]string{"error": code})
}

func (s *Server) requireRole(w http.ResponseWriter, r *http.Request, required auth.Role, action, targetType, targetID string) (auth.Session, bool) {
	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return auth.Session{}, false
	}
	if auth.RoleAllows(session.Operator.Role, required) {
		return session, true
	}
	if err := s.recordAudit(r.Context(), session, action, targetType, targetID, audit.ResultDenied, "insufficient_role"); err != nil {
		s.logger.Warn("mio-web audit denied event failed", "error", err)
	}
	writeJSON(w, http.StatusForbidden, map[string]string{
		"error":         "insufficient_role",
		"required_role": string(required),
	})
	return session, false
}

func (s *Server) recordAuditOrError(w http.ResponseWriter, ctx context.Context, session auth.Session, action, targetType, targetID, result, errorText string) bool {
	if err := s.recordAudit(ctx, session, action, targetType, targetID, result, errorText); err != nil {
		s.logger.Error("mio-web audit write failed", "error", err, "action", action, "target_type", targetType, "target_id", targetID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "audit_failed"})
		return false
	}
	return true
}

func (s *Server) recordAudit(ctx context.Context, session auth.Session, action, targetType, targetID, result, errorText string) error {
	return s.audit.Record(ctx, audit.Event{
		OperatorEmail: session.Operator.Email,
		OperatorRole:  string(session.Operator.Role),
		Action:        action,
		TargetType:    targetType,
		TargetID:      targetID,
		Result:        result,
		Error:         errorText,
	})
}
