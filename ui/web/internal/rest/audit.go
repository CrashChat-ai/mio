package rest

import (
	"net/http"
	"strconv"
	"time"

	"github.com/crashchat-ai/mio/ui/web/internal/audit"
	"github.com/crashchat-ai/mio/ui/web/internal/auth"
)

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, auth.RoleOperator, "audit.list", "audit", ""); !ok {
		return
	}
	limit := audit.DefaultListLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	events, err := s.audit.List(r.Context(), limit)
	if err != nil {
		s.logger.Warn("mio-web audit list failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "audit_unavailable"})
		return
	}
	out := make([]auditEventJSON, 0, len(events))
	for _, e := range events {
		out = append(out, auditEventToJSON(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

func auditEventToJSON(e audit.Event) auditEventJSON {
	created := ""
	if !e.CreatedAt.IsZero() {
		created = e.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	return auditEventJSON{
		OperatorEmail: e.OperatorEmail,
		OperatorRole:  e.OperatorRole,
		Action:        e.Action,
		TargetType:    e.TargetType,
		TargetID:      e.TargetID,
		Result:        e.Result,
		Error:         e.Error,
		CreatedAt:     created,
	}
}
