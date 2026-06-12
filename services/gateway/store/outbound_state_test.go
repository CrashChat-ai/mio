package store

import (
	"testing"
	"time"
)

func TestOutboundState_SetGet(t *testing.T) {
	s := NewOutboundState()
	s.Set(t.Context(), "cmd-1", "acct-test", "ext-abc")

	got, ok := s.Get(t.Context(), "cmd-1")
	if !ok {
		t.Fatal("expected entry to exist")
	}
	if got != "ext-abc" {
		t.Fatalf("expected ext-abc, got %s", got)
	}
}

func TestOutboundState_GetMissing(t *testing.T) {
	s := NewOutboundState()
	_, ok := s.Get(t.Context(), "nonexistent")
	if ok {
		t.Fatal("expected miss for nonexistent key")
	}
}

func TestOutboundState_TTLExpiry(t *testing.T) {
	s := NewOutboundState()
	s.ttl = 1 * time.Millisecond
	s.Set(t.Context(), "cmd-ttl", "acct-test", "ext-xyz")

	time.Sleep(5 * time.Millisecond)

	_, ok := s.Get(t.Context(), "cmd-ttl")
	if ok {
		t.Fatal("expected entry to be evicted after TTL")
	}
	if s.Len() != 0 {
		t.Fatalf("expected 0 entries after TTL eviction, got %d", s.Len())
	}
}

func TestOutboundState_LRUEviction(t *testing.T) {
	s := NewOutboundState()
	s.maxSize = 3

	s.Set(t.Context(), "a", "acct-test", "ext-a")
	s.Set(t.Context(), "b", "acct-test", "ext-b")
	s.Set(t.Context(), "c", "acct-test", "ext-c")
	// Adding a 4th should evict LRU (a, since b and c were accessed later).
	s.Set(t.Context(), "d", "acct-test", "ext-d")

	if s.Len() != 3 {
		t.Fatalf("expected 3 entries after LRU eviction, got %d", s.Len())
	}
	// "a" was LRU at insertion of "d"; should be gone.
	_, ok := s.Get(t.Context(), "a")
	if ok {
		t.Fatal("expected 'a' to be evicted as LRU")
	}
	// "d" must be present.
	got, ok := s.Get(t.Context(), "d")
	if !ok || got != "ext-d" {
		t.Fatal("expected 'd' to be present after insertion")
	}
}

func TestOutboundState_UpdateExisting(t *testing.T) {
	s := NewOutboundState()
	s.Set(t.Context(), "cmd-1", "acct-test", "ext-v1")
	s.Set(t.Context(), "cmd-1", "acct-test", "ext-v2") // update

	got, ok := s.Get(t.Context(), "cmd-1")
	if !ok {
		t.Fatal("expected entry after update")
	}
	if got != "ext-v2" {
		t.Fatalf("expected ext-v2, got %s", got)
	}
	if s.Len() != 1 {
		t.Fatalf("expected 1 entry after update, got %d", s.Len())
	}
}

func TestOutboundState_Len(t *testing.T) {
	s := NewOutboundState()
	if s.Len() != 0 {
		t.Fatalf("expected 0 initial length")
	}
	s.Set(t.Context(), "x", "acct-test", "1")
	s.Set(t.Context(), "y", "acct-test", "2")
	if s.Len() != 2 {
		t.Fatalf("expected 2, got %d", s.Len())
	}
}
