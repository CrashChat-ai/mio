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
	"github.com/crashchat-ai/mio/ui/web/internal/auth"
)

type Config struct {
	Admin  adminclient.Admin
	Auth   *auth.Manager
	Assets http.Handler
	Logger *slog.Logger
}

type Server struct {
	admin  adminclient.Admin
	auth   *auth.Manager
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
		assets: cfg.Assets,
		logger: cfg.Logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", Health)
	mux.HandleFunc("/api/session", s.method(http.MethodGet, s.auth.HandleSession))
	mux.HandleFunc("/auth/login", s.auth.HandleLogin)
	mux.HandleFunc("/auth/callback", s.auth.HandleCallback)
	mux.HandleFunc("/auth/logout", s.auth.HandleLogout)

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/api/admin/tenants", s.method(http.MethodGet, s.handleTenants))
	adminMux.HandleFunc("/api/admin/channel-types", s.method(http.MethodGet, s.handleChannelTypes))
	adminMux.HandleFunc("/api/admin/accounts", s.method(http.MethodGet, s.handleAccounts))
	adminMux.HandleFunc("/api/admin/messages/tail", s.method(http.MethodGet, s.handleTailMessages))
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

type tenantJSON struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
	DisabledAt  string `json:"disabledAt,omitempty"`
}

type accountJSON struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	ChannelType string `json:"channelType"`
	Provider    string `json:"provider"`
	ExternalID  string `json:"externalId"`
	DisplayName string `json:"displayName"`
	CreatedAt   string `json:"createdAt"`
	DisabledAt  string `json:"disabledAt,omitempty"`
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
		ID:          account.GetId(),
		TenantID:    account.GetTenantId(),
		ChannelType: account.GetChannelType(),
		Provider:    account.GetProvider(),
		ExternalID:  account.GetExternalId(),
		DisplayName: account.GetDisplayName(),
		CreatedAt:   timestamp(account.GetCreatedAt()),
		DisabledAt:  timestamp(account.GetDisabledAt()),
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

func writeSSEError(w http.ResponseWriter, flusher http.Flusher, code string) {
	payload, _ := json.Marshal(map[string]string{"error": code})
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload)
	flusher.Flush()
}
