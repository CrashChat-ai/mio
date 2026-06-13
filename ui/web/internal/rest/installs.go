package rest

import (
	"net/http"
	"strings"

	"github.com/crashchat-ai/mio/ui/web/internal/audit"
	"github.com/crashchat-ai/mio/ui/web/internal/auth"
)

type startInstallRequest struct {
	TenantID    string `json:"tenantId"`
	ChannelType string `json:"channelType"`
	Provider    string `json:"provider"`
}

type completeInstallRequest struct {
	InstallID string `json:"installId"`
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
