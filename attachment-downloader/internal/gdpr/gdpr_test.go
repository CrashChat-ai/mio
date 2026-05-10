package gdpr

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/crashchat-ai/mio/attachment-downloader/internal/storage"
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
	f.objects[key] = storage.Object{Key: key, Size: int64(len(b)), AccountID: opts.AccountID}
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
