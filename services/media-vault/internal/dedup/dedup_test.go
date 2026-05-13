package dedup

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crashchat-ai/mio/services/media-vault/internal/storage"
)

// fakeStorage is a minimal in-memory backend used only by dedup tests.
type fakeStorage struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFake() *fakeStorage { return &fakeStorage{objects: map[string][]byte{}} }

func (f *fakeStorage) Backend() string { return "fake" }

func (f *fakeStorage) Put(_ context.Context, key string, body io.Reader, _ int64, opts storage.PutOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if opts.IfNotExists {
		if _, exists := f.objects[key]; exists {
			return fmt.Errorf("%w", storage.ErrAlreadyExists)
		}
	}
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.objects[key] = b
	return nil
}

func (f *fakeStorage) Get(_ context.Context, key string) (io.ReadCloser, *storage.Object, error) {
	return nil, nil, storage.ErrUnsupported
}
func (f *fakeStorage) Stat(_ context.Context, key string) (*storage.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return &storage.Object{Key: key, Size: int64(len(b)), ModifiedAt: time.Now()}, nil
}
func (f *fakeStorage) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	return nil
}
func (f *fakeStorage) List(_ context.Context, prefix string) (<-chan storage.Object, <-chan error) {
	out := make(chan storage.Object)
	errCh := make(chan error, 1)
	close(out)
	errCh <- nil
	close(errCh)
	return out, errCh
}
func (f *fakeStorage) SignedURL(_ context.Context, key string, _ storage.SignOptions) (string, error) {
	return "https://fake.test/" + key, nil
}
func (f *fakeStorage) SetLifecycle(_ context.Context, _ []storage.LifecycleRule) error {
	return nil
}

func TestPersistIfAbsentWritesWhenKeyMissing(t *testing.T) {
	s := newFake()
	called := atomic.Int32{}
	res, err := PersistIfAbsent(t.Context(), s, "k", func() error {
		called.Add(1)
		return s.Put(t.Context(), "k", strings.NewReader("data"), 4, storage.PutOptions{IfNotExists: true})
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if res.AlreadyExisted || res.CollisionResolved {
		t.Fatalf("expected fresh write, got %+v", res)
	}
	if called.Load() != 1 {
		t.Fatalf("writeFn called %d times, want 1", called.Load())
	}
}

func TestPersistIfAbsentSkipsWriteWhenStatHits(t *testing.T) {
	s := newFake()
	_ = s.Put(t.Context(), "k", strings.NewReader("x"), 1, storage.PutOptions{})
	called := atomic.Int32{}
	res, err := PersistIfAbsent(t.Context(), s, "k", func() error {
		called.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyExisted {
		t.Fatalf("expected AlreadyExisted, got %+v", res)
	}
	if called.Load() != 0 {
		t.Fatalf("writeFn must not be called when Stat hits, got %d", called.Load())
	}
}

func TestPersistIfAbsentResolvesCollision(t *testing.T) {
	s := newFake()
	// Pre-populate to simulate the race winner finishing between our Stat and Put.
	// Stat in PersistIfAbsent will miss (we'll delete after), then Put with
	// IfNotExists collides.
	called := atomic.Int32{}
	res, err := PersistIfAbsent(t.Context(), s, "k", func() error {
		called.Add(1)
		// Simulate the race winner having stored bytes between our Stat and Put.
		_ = s.Put(t.Context(), "k", strings.NewReader("racer"), 5, storage.PutOptions{})
		return s.Put(t.Context(), "k", strings.NewReader("loser"), 5, storage.PutOptions{IfNotExists: true})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.CollisionResolved {
		t.Fatalf("expected CollisionResolved, got %+v", res)
	}
}
