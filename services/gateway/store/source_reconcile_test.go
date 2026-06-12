package store

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSourceReconcileCursorRoundTrip(t *testing.T) {
	pool, cleanup := requirePool(t)
	defer cleanup()
	ctx := context.Background()

	tenantID, slug := newTenantID(t, "source-reconcile")
	if _, err := EnsureTenant(ctx, pool, tenantID, slug, "Source Reconcile Co"); err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	accountID := uuid.New()
	if _, err := CreateAccount(ctx, pool, accountID, tenantID, "zoho_cliq", "default", "ext-1", "Cliq", nil); err != nil {
		t.Fatalf("create account: %v", err)
	}

	if _, found, err := GetSourceReconcileCursor(ctx, pool, accountID, "CT_123"); err != nil || found {
		t.Fatalf("initial cursor found=%v err=%v", found, err)
	}
	if err := RecordSourceReconcileSuccess(ctx, pool, accountID, "zoho_cliq", "CT_123", "1001"); err != nil {
		t.Fatalf("record success: %v", err)
	}
	row, found, err := GetSourceReconcileCursor(ctx, pool, accountID, "CT_123")
	if err != nil {
		t.Fatalf("get cursor: %v", err)
	}
	if !found || row.Cursor != "1001" || row.LastSuccessAt == nil || row.LastError != "" {
		t.Fatalf("unexpected success cursor: found=%v row=%+v", found, row)
	}

	if err := RecordSourceReconcileError(ctx, pool, accountID, "zoho_cliq", "CT_123", errBoom{}); err != nil {
		t.Fatalf("record error: %v", err)
	}
	row, found, err = GetSourceReconcileCursor(ctx, pool, accountID, "CT_123")
	if err != nil {
		t.Fatalf("get cursor after error: %v", err)
	}
	if !found || row.Cursor != "1001" || row.LastErrorAt == nil || !strings.Contains(row.LastError, "boom") {
		t.Fatalf("unexpected error cursor: found=%v row=%+v", found, row)
	}

	if err := RecordSourceReconcileSuccess(ctx, pool, accountID, "zoho_cliq", "CT_123", "1002"); err != nil {
		t.Fatalf("record second success: %v", err)
	}
	row, found, err = GetSourceReconcileCursor(ctx, pool, accountID, "CT_123")
	if err != nil {
		t.Fatalf("get cursor after second success: %v", err)
	}
	if !found || row.Cursor != "1002" || row.LastError != "" || row.LastErrorAt != nil {
		t.Fatalf("unexpected cleared cursor: found=%v row=%+v", found, row)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }
