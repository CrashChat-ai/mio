package gdpr

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/crashchat-ai/mio/media-vault/internal/storage"
)

type fakeStore struct {
	mu      sync.Mutex
	objects map[string]storage.Object
	deletes []string
}

func newFake() *fakeStore { return &fakeStore{objects: map[string]storage.Object{}} }

func (f *fakeStore) Backend() string { return "fake" }
func (f *fakeStore) Put(ctx context.Context, key string, body io.Reader, _ int64, opts storage.PutOptions) error {
	b, _ := io.ReadAll(body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = storage.Object{
		Key:             key,
		Size:            int64(len(b)),
		TenantID:        opts.TenantID,
		AccountID:       opts.AccountID,
		ConversationID:  opts.ConversationID,
		SourceMessageID: opts.SourceMessageID,
	}
	return nil
}
func (f *fakeStore) Get(_ context.Context, _ string) (io.ReadCloser, *storage.Object, error) {
	return nil, nil, storage.ErrUnsupported
}
func (f *fakeStore) Stat(_ context.Context, key string) (*storage.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return &o, nil
}
func (f *fakeStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	f.deletes = append(f.deletes, key)
	return nil
}
func (f *fakeStore) List(_ context.Context, prefix string) (<-chan storage.Object, <-chan error) {
	out := make(chan storage.Object, 64)
	errCh := make(chan error, 1)
	go func() {
		f.mu.Lock()
		var matches []storage.Object
		for k, o := range f.objects {
			if strings.HasPrefix(k, prefix) {
				matches = append(matches, o)
			}
		}
		f.mu.Unlock()
		for _, o := range matches {
			out <- o
		}
		close(out)
		errCh <- nil
		close(errCh)
	}()
	return out, errCh
}
func (f *fakeStore) SignedURL(_ context.Context, key string, _ storage.SignOptions) (string, error) {
	return "", nil
}
func (f *fakeStore) SetLifecycle(_ context.Context, _ []storage.LifecycleRule) error { return nil }

func TestDeleteByAccountFiltersOnAccountID(t *testing.T) {
	f := newFake()
	for i := 0; i < 5; i++ {
		_ = f.Put(t.Context(), "p/a/"+itoa(i), strings.NewReader("x"), 1, storage.PutOptions{AccountID: "acc-1"})
		_ = f.Put(t.Context(), "p/b/"+itoa(i), strings.NewReader("x"), 1, storage.PutOptions{AccountID: "acc-2"})
	}
	stats, err := DeleteByAccount(t.Context(), f, "p/", "acc-1", false, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Listed != 10 || stats.Matched != 5 || stats.Deleted != 5 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(f.deletes) != 5 {
		t.Fatalf("expected 5 deletes, got %d", len(f.deletes))
	}
}

func TestDeleteByAccountDryRunDoesNotDelete(t *testing.T) {
	f := newFake()
	_ = f.Put(t.Context(), "p/x", strings.NewReader("x"), 1, storage.PutOptions{AccountID: "acc-1"})
	stats, err := DeleteByAccount(t.Context(), f, "p/", "acc-1", true, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Matched != 1 {
		t.Fatalf("expected 1 match, got %+v", stats)
	}
	if stats.Deleted != 0 {
		t.Fatalf("dry-run must not delete, got %d", stats.Deleted)
	}
	if len(f.deletes) != 0 {
		t.Fatalf("dry-run leaked Deletes: %v", f.deletes)
	}
}

func TestDeleteByAccountRequiresID(t *testing.T) {
	f := newFake()
	if _, err := DeleteByAccount(t.Context(), f, "p/", "", false, 1, nil); err == nil {
		t.Fatal("expected error on empty account_id")
	}
}

func TestDeleteByTenantFiltersOnTenantID(t *testing.T) {
	f := newFake()
	for i := 0; i < 4; i++ {
		_ = f.Put(t.Context(), "p/t1/"+itoa(i), strings.NewReader("x"), 1, storage.PutOptions{TenantID: "tenant-1", AccountID: "acc-x"})
		_ = f.Put(t.Context(), "p/t2/"+itoa(i), strings.NewReader("x"), 1, storage.PutOptions{TenantID: "tenant-2", AccountID: "acc-x"})
	}
	stats, err := DeleteByTenant(t.Context(), f, "p/", "tenant-1", false, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Listed != 8 || stats.Matched != 4 || stats.Deleted != 4 {
		t.Fatalf("stats = %+v", stats)
	}
	// Confirm tenant-2 survived.
	for i := 0; i < 4; i++ {
		if _, ok := f.objects["p/t2/"+itoa(i)]; !ok {
			t.Fatalf("tenant-2 object p/t2/%d was deleted", i)
		}
	}
}

func TestDeleteByTenantRequiresID(t *testing.T) {
	f := newFake()
	if _, err := DeleteByTenant(t.Context(), f, "p/", "", false, 1, nil); err == nil {
		t.Fatal("expected error on empty tenant_id")
	}
}

func TestDeleteByConversationFiltersOnConversationID(t *testing.T) {
	f := newFake()
	// Two conversations, three objects each, all under one account.
	for i := 0; i < 3; i++ {
		_ = f.Put(t.Context(), "p/c1/"+itoa(i), strings.NewReader("x"), 1, storage.PutOptions{AccountID: "acc-1", ConversationID: "conv-1"})
		_ = f.Put(t.Context(), "p/c2/"+itoa(i), strings.NewReader("x"), 1, storage.PutOptions{AccountID: "acc-1", ConversationID: "conv-2"})
	}
	stats, err := DeleteByConversation(t.Context(), f, "p/", "conv-1", false, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Listed != 6 || stats.Matched != 3 || stats.Deleted != 3 {
		t.Fatalf("stats = %+v", stats)
	}
	// Confirm conv-2 survived intact.
	for i := 0; i < 3; i++ {
		if _, ok := f.objects["p/c2/"+itoa(i)]; !ok {
			t.Fatalf("conv-2 object p/c2/%d was deleted", i)
		}
	}
}

func TestDeleteByConversationDryRunDoesNotDelete(t *testing.T) {
	f := newFake()
	_ = f.Put(t.Context(), "p/c/x", strings.NewReader("x"), 1, storage.PutOptions{ConversationID: "conv-x"})
	stats, err := DeleteByConversation(t.Context(), f, "p/", "conv-x", true, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Matched != 1 || stats.Deleted != 0 {
		t.Fatalf("dry-run stats = %+v", stats)
	}
	if len(f.deletes) != 0 {
		t.Fatalf("dry-run leaked Deletes: %v", f.deletes)
	}
}

func TestDeleteByConversationRequiresID(t *testing.T) {
	f := newFake()
	if _, err := DeleteByConversation(t.Context(), f, "p/", "", false, 1, nil); err == nil {
		t.Fatal("expected error on empty conversation_id")
	}
}

// TestDeleteByTenantSkipsPreEnrichmentObjects locks in the forward-only
// contract: objects written before the enrichment rollout have empty
// tenant_id, and a tenant filter must NOT match them (would silently delete
// legacy data belonging to a different tenant).
func TestDeleteByTenantSkipsPreEnrichmentObjects(t *testing.T) {
	f := newFake()
	// Pre-enrichment object: account_id only, no tenant_id.
	_ = f.Put(t.Context(), "p/legacy", strings.NewReader("x"), 1, storage.PutOptions{AccountID: "acc-1"})
	// New object: full metadata.
	_ = f.Put(t.Context(), "p/new", strings.NewReader("x"), 1, storage.PutOptions{TenantID: "tenant-1", AccountID: "acc-1"})

	stats, err := DeleteByTenant(t.Context(), f, "p/", "tenant-1", false, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Matched != 1 || stats.Deleted != 1 {
		t.Fatalf("expected only the enriched object to match, got %+v", stats)
	}
	if _, ok := f.objects["p/legacy"]; !ok {
		t.Fatal("legacy object (no tenant_id) was wrongly deleted on a tenant filter")
	}
}

func itoa(i int) string {
	const ds = "0123456789"
	if i == 0 {
		return "0"
	}
	out := ""
	for i > 0 {
		out = string(ds[i%10]) + out
		i /= 10
	}
	return out
}
