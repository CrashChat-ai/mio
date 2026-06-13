package rest

import (
	"net/http"
	"strings"

	"github.com/crashchat-ai/mio/ui/web/internal/audit"
	"github.com/crashchat-ai/mio/ui/web/internal/auth"
)

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
