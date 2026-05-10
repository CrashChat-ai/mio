package storage

import (
	"context"
	"testing"
)

// TestSentinelErrors guards against accidental sentinel renames.
// All backends compare with errors.Is against these symbols.
func TestSentinelErrors(t *testing.T) {
	for _, e := range []error{ErrNotFound, ErrAlreadyExists, ErrUnsupported} {
		if e == nil {
			t.Fatal("sentinel must not be nil")
		}
	}
}

func TestFactoryRejectsMissingBucket(t *testing.T) {
	t.Setenv(envBackend, "gcs")
	t.Setenv(envBucket, "")
	if _, err := New(context.Background()); err == nil {
		t.Fatal("expected error for missing bucket")
	}
}

func TestFactoryRejectsUnknownBackend(t *testing.T) {
	t.Setenv(envBackend, "doesnotexist")
	t.Setenv(envBucket, "any")
	if _, err := New(context.Background()); err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

func TestFactoryDispatchesToRegisteredBackend(t *testing.T) {
	called := false
	Register("memtest", func(_ context.Context, bucket string) (Storage, error) {
		called = true
		return nil, nil
	})
	t.Setenv(envBackend, "memtest")
	t.Setenv(envBucket, "ok")
	if _, err := New(context.Background()); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !called {
		t.Fatal("registered factory was not invoked")
	}
}
