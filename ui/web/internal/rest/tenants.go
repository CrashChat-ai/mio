package rest

import (
	"net/http"
	"strings"

	"github.com/crashchat-ai/mio/ui/web/internal/audit"
	"github.com/crashchat-ai/mio/ui/web/internal/auth"
)

type createTenantRequest struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
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

func (s *Server) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.PathValue("id"))
	if tenantID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_id_required"})
		return
	}
	tenant, err := s.admin.GetTenant(r.Context(), tenantID)
	if err != nil {
		s.writeAdminError(w, err)
		return
	}
	if tenant == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "tenant_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenant": tenantToJSON(tenant)})
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
