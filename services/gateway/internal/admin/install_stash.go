package admin

import (
	"sync"
	"time"
)

// installStashTTL is how long a captured OAuth code may sit between
// /oauth/callback and CompleteInstall. Short by design — if the operator
// can't click "complete" within a minute the install should restart
// rather than persist a dangling code.
const installStashTTL = 60 * time.Second

// stashedCode binds a captured OAuth `code` to its install_id with an
// expiry. After expiry CompleteInstall returns FailedPrecondition.
type stashedCode struct {
	code      string
	state     string
	expiresAt time.Time
}

// installStash is the in-memory short-lived store mapping install_id →
// (code, state, expiry). Safe for concurrent use; survives only within a
// single admin process — restarts force the operator to re-run StartInstall.
type installStash struct {
	mu      sync.Mutex
	byID    map[string]stashedCode // keyed by install_id (UUID)
	byState map[string]string      // state → install_id (callback lookup)
	clock   func() time.Time       // injectable for tests
}

func newInstallStash() *installStash {
	return &installStash{
		byID:    map[string]stashedCode{},
		byState: map[string]string{},
		clock:   time.Now,
	}
}

// reserve records the state nonce for a freshly-issued StartInstall so the
// callback handler can find the matching install_id.
func (s *installStash) reserve(installID, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byState[state] = installID
}

// capture stores (code, state) under the install_id resolved from state.
// Returns the install_id on success, or "" if state has no matching reservation
// (replay / brute-force / late callback after a restart).
func (s *installStash) capture(state, code string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	installID, ok := s.byState[state]
	if !ok {
		return ""
	}
	delete(s.byState, state) // single-use; defend against double-callback
	s.byID[installID] = stashedCode{
		code:      code,
		state:     state,
		expiresAt: s.clock().Add(installStashTTL),
	}
	return installID
}

// consume removes and returns the stashed code for install_id. Returns
// (_, false) if the install_id has no captured code or the entry expired.
func (s *installStash) consume(installID string) (stashedCode, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc, ok := s.byID[installID]
	if !ok {
		return stashedCode{}, false
	}
	delete(s.byID, installID)
	if s.clock().After(sc.expiresAt) {
		return stashedCode{}, false
	}
	return sc, true
}

// purgeExpired removes entries past their TTL. Called opportunistically
// from capture/consume; safe to call from a background goroutine too.
func (s *installStash) purgeExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	for id, sc := range s.byID {
		if now.After(sc.expiresAt) {
			delete(s.byID, id)
		}
	}
}
