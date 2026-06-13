package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/crashchat-ai/mio/ui/web/internal/auth"
)

func TestAuditListRequiresOperator(t *testing.T) {
	handler, _, _ := newTestFixture(t, auth.RoleViewer)
	cookie := loginCookie(t, handler)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuditListReturnsRecordedEvents(t *testing.T) {
	handler, _, _ := newTestFixture(t, auth.RoleOperator)
	cookie := loginCookie(t, handler)

	mutate := httptest.NewRequest(http.MethodPatch, "/api/admin/accounts",
		strings.NewReader(`{"accountId":"a1","displayName":"Cliq edit","externalId":"ext-9"}`))
	mutate.Header.Set("Content-Type", "application/json")
	mutate.AddCookie(cookie)
	handler.ServeHTTP(httptest.NewRecorder(), mutate)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Events []auditEventJSON `json:"events"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Events) == 0 {
		t.Fatalf("expected audit events, got none")
	}
	first := payload.Events[0]
	if first.Action != "account.update" || first.Result != "success" || first.CreatedAt == "" {
		t.Fatalf("event shape: %+v", first)
	}
}
