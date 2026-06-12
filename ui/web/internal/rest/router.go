package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"

	"github.com/crashchat-ai/mio/ui/web/internal/adminclient"
	"github.com/crashchat-ai/mio/ui/web/internal/audit"
	"github.com/crashchat-ai/mio/ui/web/internal/auth"
)

type Config struct {
	Admin  adminclient.Admin
	Auth   *auth.Manager
	Audit  audit.Logger
	Assets http.Handler
	Logger *slog.Logger
}

type Server struct {
	admin  adminclient.Admin
	auth   *auth.Manager
	audit  audit.Logger
	assets http.Handler
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
		assets: cfg.Assets,
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
	adminMux.HandleFunc("/api/admin/channel-types", s.method(http.MethodGet, s.handleChannelTypes))
	adminMux.HandleFunc("/api/admin/accounts", s.handleAccounts)
	adminMux.HandleFunc("/api/admin/accounts/rate-limit", s.method(http.MethodPost, s.handleSetRateLimit))
	adminMux.HandleFunc("/api/admin/accounts/disable", s.method(http.MethodPost, s.handleDisableAccount))
	adminMux.HandleFunc("/api/admin/accounts/rotate-credential", s.method(http.MethodPost, s.handleRotateCredential))
	adminMux.HandleFunc("/api/admin/accounts/credential-metadata", s.method(http.MethodGet, s.handleCredentialMetadata))
	adminMux.HandleFunc("/api/admin/installs/start", s.method(http.MethodPost, s.handleStartInstall))
	adminMux.HandleFunc("/api/admin/installs/complete", s.method(http.MethodPost, s.handleCompleteInstall))
	adminMux.HandleFunc("/api/admin/messages/tail", s.method(http.MethodGet, s.handleTailMessages))
	adminMux.HandleFunc("/api/admin/accounts/webhook-info", s.method(http.MethodGet, s.handleWebhookInfo))
	adminMux.HandleFunc("/api/admin/stream-health", s.method(http.MethodGet, s.handleStreamHealth))
	mux.Handle("/api/admin/", s.auth.Require(adminMux))

	mux.Handle("/", cfg.Assets)
	return mux
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

func (s *Server) handleTenants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListTenants(w, r)
	case http.MethodPost:
		s.handleCreateTenant(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := s.admin.ListTenants(r.Context())
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	out := make([]tenantJSON, 0, len(tenants))
	for _, tenant := range tenants {
		out = append(out, tenantToJSON(tenant))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenants": out})
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	var req createTenantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Slug = strings.TrimSpace(req.Slug)
	if req.Slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slug_required"})
		return
	}
	session, ok := s.requireRole(w, r, auth.RoleOperator, "tenant.create", "tenant", req.Slug)
	if !ok {
		return
	}
	tenant, err := s.admin.CreateTenant(r.Context(), req.Slug, strings.TrimSpace(req.DisplayName))
	if err != nil {
		s.recordAudit(r.Context(), session, "tenant.create", "tenant", req.Slug, audit.ResultFailure, err.Error())
		s.writeAdminError(w, err)
		return
	}
	targetID := req.Slug
	if tenant.GetId() != "" {
		targetID = tenant.GetId()
	}
	if !s.recordAuditOrError(w, r.Context(), session, "tenant.create", "tenant", targetID, audit.ResultSuccess, "") {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"tenant": tenantToJSON(tenant)})
}

func (s *Server) handleChannelTypes(w http.ResponseWriter, r *http.Request) {
	channels, err := s.admin.ListChannelTypes(r.Context())
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	out := make([]channelTypeJSON, 0, len(channels))
	for _, channel := range channels {
		out = append(out, channelTypeToJSON(channel))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channelTypes": out})
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListAccounts(w, r)
	case http.MethodPatch:
		s.handleUpdateAccount(w, r)
	default:
		w.Header().Set("Allow", "GET, PATCH")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_id_required"})
		return
	}
	accounts, err := s.admin.ListAccounts(r.Context(), tenantID)
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	out := make([]accountJSON, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, accountToJSON(account))
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	var req updateAccountRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.AccountID = strings.TrimSpace(req.AccountID)
	if req.AccountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id_required"})
		return
	}
	session, ok := s.requireRole(w, r, auth.RoleOperator, "account.update", "account", req.AccountID)
	if !ok {
		return
	}
	account, err := s.admin.UpdateAccount(r.Context(), req.AccountID, strings.TrimSpace(req.DisplayName), strings.TrimSpace(req.ExternalID))
	if err != nil {
		s.recordAudit(r.Context(), session, "account.update", "account", req.AccountID, audit.ResultFailure, err.Error())
		s.writeAdminError(w, err)
		return
	}
	if !s.recordAuditOrError(w, r.Context(), session, "account.update", "account", req.AccountID, audit.ResultSuccess, "") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": accountToJSON(account)})
}

func (s *Server) handleSetRateLimit(w http.ResponseWriter, r *http.Request) {
	var req setRateLimitRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.AccountID = strings.TrimSpace(req.AccountID)
	req.RateLimitScope = strings.TrimSpace(req.RateLimitScope)
	if req.AccountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id_required"})
		return
	}
	if req.RateLimitPerSecond < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rate_limit_nonnegative_required"})
		return
	}
	session, ok := s.requireRole(w, r, auth.RoleOperator, "account.rate_limit.set", "account", req.AccountID)
	if !ok {
		return
	}
	account, err := s.admin.SetRateLimit(r.Context(), req.AccountID, req.RateLimitPerSecond, req.RateLimitScope)
	if err != nil {
		s.recordAudit(r.Context(), session, "account.rate_limit.set", "account", req.AccountID, audit.ResultFailure, err.Error())
		s.writeAdminError(w, err)
		return
	}
	if !s.recordAuditOrError(w, r.Context(), session, "account.rate_limit.set", "account", req.AccountID, audit.ResultSuccess, "") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": accountToJSON(account)})
}

func (s *Server) handleDisableAccount(w http.ResponseWriter, r *http.Request) {
	var req accountIDRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.AccountID = strings.TrimSpace(req.AccountID)
	if req.AccountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id_required"})
		return
	}
	session, ok := s.requireRole(w, r, auth.RoleOperator, "account.disable", "account", req.AccountID)
	if !ok {
		return
	}
	err := s.admin.DisableAccount(r.Context(), req.AccountID)
	if err != nil {
		s.recordAudit(r.Context(), session, "account.disable", "account", req.AccountID, audit.ResultFailure, err.Error())
		s.writeAdminError(w, err)
		return
	}
	if !s.recordAuditOrError(w, r.Context(), session, "account.disable", "account", req.AccountID, audit.ResultSuccess, "") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRotateCredential(w http.ResponseWriter, r *http.Request) {
	var req accountIDRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.AccountID = strings.TrimSpace(req.AccountID)
	if req.AccountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id_required"})
		return
	}
	session, ok := s.requireRole(w, r, auth.RoleCredentialAdmin, "credential.rotate", "account", req.AccountID)
	if !ok {
		return
	}
	err := s.admin.RotateCredential(r.Context(), req.AccountID)
	if err != nil {
		s.recordAudit(r.Context(), session, "credential.rotate", "account", req.AccountID, audit.ResultFailure, err.Error())
		s.writeAdminError(w, err)
		return
	}
	if !s.recordAuditOrError(w, r.Context(), session, "credential.rotate", "account", req.AccountID, audit.ResultSuccess, "") {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCredentialMetadata(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id_required"})
		return
	}
	meta, err := s.admin.GetCredentialMetadata(r.Context(), accountID)
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credential": credentialMetadataToJSON(meta)})
}

func (s *Server) handleStartInstall(w http.ResponseWriter, r *http.Request) {
	var req startInstallRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.ChannelType = strings.TrimSpace(req.ChannelType)
	req.Provider = strings.TrimSpace(req.Provider)
	if req.TenantID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_id_required"})
		return
	}
	if req.ChannelType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel_type_required"})
		return
	}
	session, ok := s.requireRole(w, r, auth.RoleOperator, "install.start", "tenant", req.TenantID)
	if !ok {
		return
	}
	install, err := s.admin.StartInstall(r.Context(), req.TenantID, req.ChannelType, req.Provider)
	if err != nil {
		s.recordAudit(r.Context(), session, "install.start", "tenant", req.TenantID, audit.ResultFailure, err.Error())
		s.writeAdminError(w, err)
		return
	}
	targetID := install.GetInstallId()
	if targetID == "" {
		targetID = req.TenantID
	}
	if !s.recordAuditOrError(w, r.Context(), session, "install.start", "install", targetID, audit.ResultSuccess, "") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installId":   install.GetInstallId(),
		"oauthUrl":    install.GetOauthUrl(),
		"redirectUri": install.GetRedirectUri(),
	})
}

func (s *Server) handleCompleteInstall(w http.ResponseWriter, r *http.Request) {
	var req completeInstallRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.InstallID = strings.TrimSpace(req.InstallID)
	if req.InstallID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "install_id_required"})
		return
	}
	session, ok := s.requireRole(w, r, auth.RoleOperator, "install.complete", "install", req.InstallID)
	if !ok {
		return
	}
	account, err := s.admin.CompleteInstall(r.Context(), req.InstallID)
	if err != nil {
		s.recordAudit(r.Context(), session, "install.complete", "install", req.InstallID, audit.ResultFailure, err.Error())
		s.writeAdminError(w, err)
		return
	}
	if !s.recordAuditOrError(w, r.Context(), session, "install.complete", "install", req.InstallID, audit.ResultSuccess, "") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": accountToJSON(account)})
}

func (s *Server) handleTailMessages(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id_required"})
		return
	}
	conversationID := strings.TrimSpace(r.URL.Query().Get("conversation_id"))
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming_not_supported"})
		return
	}
	stream, err := s.admin.TailMessages(r.Context(), accountID, conversationID)
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	defer stream.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	for stream.Receive() {
		payload, err := json.Marshal(tailMessageToJSON(stream.Msg()))
		if err != nil {
			s.logger.Warn("mio-web tail marshal failed", "error", err)
			continue
		}
		if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload); err != nil {
			return
		}
		flusher.Flush()
	}
	if err := stream.Err(); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Warn("mio-web tail stream failed", "error", err)
		writeSSEError(w, flusher, "tail_failed")
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

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close() //nolint:errcheck
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return false
	}
	return true
}

type createTenantRequest struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
}

type startInstallRequest struct {
	TenantID    string `json:"tenantId"`
	ChannelType string `json:"channelType"`
	Provider    string `json:"provider"`
}

type completeInstallRequest struct {
	InstallID string `json:"installId"`
}

type updateAccountRequest struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	ExternalID  string `json:"externalId"`
}

type setRateLimitRequest struct {
	AccountID          string `json:"accountId"`
	RateLimitPerSecond int32  `json:"rateLimitPerSecond"`
	RateLimitScope     string `json:"rateLimitScope"`
}

type accountIDRequest struct {
	AccountID string `json:"accountId"`
}

type tenantJSON struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
	DisabledAt  string `json:"disabledAt,omitempty"`
}

type accountJSON struct {
	ID                 string `json:"id"`
	TenantID           string `json:"tenantId"`
	ChannelType        string `json:"channelType"`
	Provider           string `json:"provider"`
	ExternalID         string `json:"externalId"`
	DisplayName        string `json:"displayName"`
	RateLimitPerSecond int32  `json:"rateLimitPerSecond"`
	RateLimitScope     string `json:"rateLimitScope"`
	CreatedAt          string `json:"createdAt"`
	DisabledAt         string `json:"disabledAt,omitempty"`
}

type channelTypeJSON struct {
	Slug            string   `json:"slug"`
	Status          string   `json:"status"`
	AuthKind        string   `json:"authKind"`
	SupportsThreads bool     `json:"supportsThreads"`
	SupportsEdit    bool     `json:"supportsEdit"`
	SupportsDelete  bool     `json:"supportsDelete"`
	AllowedKinds    []string `json:"allowedAttachmentKinds"`
	RateLimitScope  string   `json:"rateLimitScope"`
	RateLimitPerSec int32    `json:"rateLimitPerSecond"`
	MaxTextBytes    int32    `json:"maxTextBytes"`
}

type tailMessageJSON struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenantId"`
	AccountID      string `json:"accountId"`
	ConversationID string `json:"conversationId"`
	ChannelType    string `json:"channelType"`
	SenderDisplay  string `json:"senderDisplay"`
	Text           string `json:"text"`
	ReceivedAt     string `json:"receivedAt"`
}

type credentialMetadataJSON struct {
	AccountID     string `json:"accountId"`
	HasCredential bool   `json:"hasCredential"`
	AuthKind      string `json:"authKind,omitempty"`
	KeyVersion    int32  `json:"keyVersion,omitempty"`
	ExpiresAt     string `json:"expiresAt,omitempty"`
	RotatedAt     string `json:"rotatedAt,omitempty"`
}

type webhookInfoJSON struct {
	AccountID    string   `json:"accountId"`
	ChannelType  string   `json:"channelType"`
	WebhookURL   string   `json:"webhookUrl"`
	RouteAliases []string `json:"routeAliases"`
	AuthKind     string   `json:"authKind"`
	SetupHint    string   `json:"setupHint"`
}

type consumerHealthJSON struct {
	ConsumerName  string `json:"consumerName"`
	Stream        string `json:"stream"`
	NumPending    uint64 `json:"numPending"`
	NumAckPending uint64 `json:"numAckPending"`
	LastDelivered string `json:"lastDelivered,omitempty"`
}

func tenantToJSON(tenant *adminv1.Tenant) tenantJSON {
	if tenant == nil {
		return tenantJSON{}
	}
	return tenantJSON{
		ID:          tenant.GetId(),
		Slug:        tenant.GetSlug(),
		DisplayName: tenant.GetDisplayName(),
		Status:      tenant.GetStatus(),
		CreatedAt:   timestamp(tenant.GetCreatedAt()),
		DisabledAt:  timestamp(tenant.GetDisabledAt()),
	}
}

func accountToJSON(account *adminv1.Account) accountJSON {
	if account == nil {
		return accountJSON{}
	}
	return accountJSON{
		ID:                 account.GetId(),
		TenantID:           account.GetTenantId(),
		ChannelType:        account.GetChannelType(),
		Provider:           account.GetProvider(),
		ExternalID:         account.GetExternalId(),
		DisplayName:        account.GetDisplayName(),
		RateLimitPerSecond: account.GetRateLimitPerSecond(),
		RateLimitScope:     account.GetRateLimitScope(),
		CreatedAt:          timestamp(account.GetCreatedAt()),
		DisabledAt:         timestamp(account.GetDisabledAt()),
	}
}

func channelTypeToJSON(channel *adminv1.ChannelTypeInfo) channelTypeJSON {
	if channel == nil {
		return channelTypeJSON{}
	}
	caps := channel.GetCapabilities()
	out := channelTypeJSON{
		Slug:            channel.GetSlug(),
		Status:          channel.GetStatus(),
		AuthKind:        caps.GetAuthKind(),
		SupportsThreads: caps.GetSupportsThreads(),
		SupportsEdit:    caps.GetSupportsEdit(),
		SupportsDelete:  caps.GetSupportsDelete(),
		RateLimitScope:  caps.GetRateLimitScope(),
		RateLimitPerSec: caps.GetRateLimitPerSecond(),
		MaxTextBytes:    caps.GetMaxTextBytes(),
	}
	for _, kind := range caps.GetAllowedAttachments() {
		out.AllowedKinds = append(out.AllowedKinds, kind.String())
	}
	return out
}

func tailMessageToJSON(msg *adminv1.TailMessagesResponse) tailMessageJSON {
	if msg == nil {
		return tailMessageJSON{}
	}
	return tailMessageJSON{
		ID:             msg.GetId(),
		TenantID:       msg.GetTenantId(),
		AccountID:      msg.GetAccountId(),
		ConversationID: msg.GetConversationId(),
		ChannelType:    msg.GetChannelType(),
		SenderDisplay:  msg.GetSenderDisplay(),
		Text:           msg.GetText(),
		ReceivedAt:     timestamp(msg.GetReceivedAt()),
	}
}

func webhookInfoToJSON(info *adminv1.GetWebhookInfoResponse) webhookInfoJSON {
	if info == nil {
		return webhookInfoJSON{}
	}
	aliases := info.GetRouteAliases()
	if aliases == nil {
		aliases = []string{}
	}
	return webhookInfoJSON{
		AccountID:    info.GetAccountId(),
		ChannelType:  info.GetChannelType(),
		WebhookURL:   info.GetWebhookUrl(),
		RouteAliases: aliases,
		AuthKind:     info.GetAuthKind(),
		SetupHint:    info.GetSetupHint(),
	}
}

func consumerHealthToJSON(c *adminv1.ConsumerHealth) consumerHealthJSON {
	if c == nil {
		return consumerHealthJSON{}
	}
	return consumerHealthJSON{
		ConsumerName:  c.GetConsumerName(),
		Stream:        c.GetStream(),
		NumPending:    c.GetNumPending(),
		NumAckPending: c.GetNumAckPending(),
		LastDelivered: timestamp(c.GetLastDelivered()),
	}
}

func credentialMetadataToJSON(meta *adminv1.GetCredentialMetadataResponse) credentialMetadataJSON {
	if meta == nil {
		return credentialMetadataJSON{}
	}
	return credentialMetadataJSON{
		AccountID:     meta.GetAccountId(),
		HasCredential: meta.GetHasCredential(),
		AuthKind:      meta.GetAuthKind(),
		KeyVersion:    meta.GetKeyVersion(),
		ExpiresAt:     timestamp(meta.GetExpiresAt()),
		RotatedAt:     timestamp(meta.GetRotatedAt()),
	}
}

func timestamp(ts *timestamppb.Timestamp) string {
	if ts == nil || !ts.IsValid() {
		return ""
	}
	return ts.AsTime().UTC().Format(time.RFC3339Nano)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) handleWebhookInfo(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id_required"})
		return
	}
	info, err := s.admin.GetWebhookInfo(r.Context(), accountID)
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"webhookInfo": webhookInfoToJSON(info)})
}

func (s *Server) handleStreamHealth(w http.ResponseWriter, r *http.Request) {
	health, err := s.admin.GetStreamHealth(r.Context())
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	consumers := make([]consumerHealthJSON, 0, len(health.GetConsumers()))
	for _, c := range health.GetConsumers() {
		consumers = append(consumers, consumerHealthToJSON(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"consumers": consumers})
}

func writeSSEError(w http.ResponseWriter, flusher http.Flusher, code string) {
	payload, _ := json.Marshal(map[string]string{"error": code})
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
	flusher.Flush()
}
