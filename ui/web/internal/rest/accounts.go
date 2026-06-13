package rest

import (
	"net/http"
	"strings"

	"github.com/crashchat-ai/mio/ui/web/internal/audit"
	"github.com/crashchat-ai/mio/ui/web/internal/auth"
)

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

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.PathValue("id"))
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account_id_required"})
		return
	}
	account, err := s.admin.GetAccount(r.Context(), accountID)
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	if account == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": accountToJSON(account)})
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
