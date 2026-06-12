package store

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestPickAccount(t *testing.T) {
	a := resolverAccount{tenantID: "t1", accountID: "a1", externalID: "org-1"}
	b := resolverAccount{tenantID: "t2", accountID: "a2", externalID: "org-2"}

	tests := []struct {
		name     string
		accounts []resolverAccount
		key      string
		want     string
		ok       bool
	}{
		{"single account wins with empty key", []resolverAccount{a}, "", "a1", true},
		{"single account wins with matching key", []resolverAccount{a}, "org-1", "a1", true},
		{"single account REJECTS mismatched key", []resolverAccount{a}, "org-9", "", false},
		{"single account w/ empty external_id accepts any key", []resolverAccount{{tenantID: "t3", accountID: "a3", externalID: ""}}, "org-9", "a3", true},
		{"multi: key match routes", []resolverAccount{a, b}, "org-2", "a2", true},
		{"multi: no key → unresolved", []resolverAccount{a, b}, "", "", false},
		{"multi: wrong key → unresolved", []resolverAccount{a, b}, "org-9", "", false},
		{"empty set → unresolved", nil, "org-1", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pickAccount(tt.accounts, tt.key)
			if ok != tt.ok || got.accountID != tt.want {
				t.Fatalf("got (%q, %v), want (%q, %v)", got.accountID, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestAccountResolver_DB(t *testing.T) {
	pool := durableTestPool(t)
	ctx := context.Background()

	tenant, err := EnsureTenant(ctx, pool, uuid.New(), "rsv-tenant", "Resolver Tenant")
	if err != nil {
		t.Fatal(err)
	}
	acctA, err := CreateAccount(ctx, pool, uuid.New(), tenant.ID, "rsv_chan", "default", "org-aaa", "A", nil)
	if err != nil {
		t.Fatal(err)
	}
	acctB, err := CreateAccount(ctx, pool, uuid.New(), tenant.ID, "rsv_chan", "default", "org-bbb", "B", nil)
	if err != nil {
		t.Fatal(err)
	}

	r := NewAccountResolver(pool, nil)
	res, ok, err := r.Resolve(ctx, "rsv_chan", "org-bbb")
	if err != nil || !ok || res.AccountID != acctB.ID.String() {
		t.Fatalf("want acctB, got %+v ok=%v err=%v", res, ok, err)
	}
	if _, ok, _ := r.Resolve(ctx, "rsv_chan", ""); ok {
		t.Fatal("two accounts + empty key must not resolve")
	}

	if err := DisableAccount(ctx, pool, acctB.ID); err != nil {
		t.Fatal(err)
	}
	// Cache still holds both; fresh resolver sees the single survivor.
	r2 := NewAccountResolver(pool, nil)
	res, ok, err = r2.Resolve(ctx, "rsv_chan", "")
	if err != nil || !ok || res.AccountID != acctA.ID.String() {
		t.Fatalf("single enabled account must auto-route, got %+v ok=%v err=%v", res, ok, err)
	}
}
