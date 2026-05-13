package admin

import (
	"testing"
	"time"
)

func TestInstallStash_ReserveCaptureConsume(t *testing.T) {
	s := newInstallStash()
	s.reserve("install-1", "state-abc")

	got := s.capture("state-abc", "code-xyz")
	if got != "install-1" {
		t.Errorf("capture: got %q", got)
	}

	sc, ok := s.consume("install-1")
	if !ok {
		t.Fatal("consume: not found")
	}
	if sc.code != "code-xyz" {
		t.Errorf("code: %q", sc.code)
	}
}

func TestInstallStash_UnknownState(t *testing.T) {
	s := newInstallStash()
	got := s.capture("never-reserved", "code")
	if got != "" {
		t.Errorf("expected empty install_id for unknown state, got %q", got)
	}
}

func TestInstallStash_DoubleCallbackRejected(t *testing.T) {
	s := newInstallStash()
	s.reserve("install-1", "state-1")

	if got := s.capture("state-1", "code-1"); got != "install-1" {
		t.Errorf("first capture: %q", got)
	}
	// Second callback with the same state must not re-bind.
	if got := s.capture("state-1", "code-2"); got != "" {
		t.Errorf("double-callback should fail; got %q", got)
	}
}

func TestInstallStash_ConsumeOnce(t *testing.T) {
	s := newInstallStash()
	s.reserve("install-1", "state-1")
	s.capture("state-1", "code-1")

	if _, ok := s.consume("install-1"); !ok {
		t.Fatal("first consume failed")
	}
	if _, ok := s.consume("install-1"); ok {
		t.Errorf("consume should be single-use")
	}
}

func TestInstallStash_TTLExpiry(t *testing.T) {
	s := newInstallStash()
	now := time.Now()
	s.clock = func() time.Time { return now }
	s.reserve("install-1", "state-1")
	s.capture("state-1", "code-1")

	// Move clock past TTL.
	s.clock = func() time.Time { return now.Add(installStashTTL + time.Second) }
	if _, ok := s.consume("install-1"); ok {
		t.Errorf("expired entry should not consume")
	}
}
