package admin

import (
	"context"
	"sync"
	"time"
)

// installStashTTL is how long a captured OAuth code may sit between
// /oauth/callback and CompleteInstall. Short by design — if the operator
// can't click "complete" within a minute the install should restart
// rather than persist a dangling code.
const installStashTTL = 60 * time.Second

// installStashPurgeInterval is how often the background sweeper deletes
// expired entries from both maps. Slightly less than the TTL so a
// stranded entry vanishes within ~2x TTL.
const installStashPurgeInterval = 30 * time.Second

// stashedCode binds a captured OAuth `code` to its install_id with an
// expiry. After expiry CompleteInstall returns FailedPrecondition.
type stashedCode struct {
	code      string
	state     string
	expiresAt time.Time
}

// stashedReserve binds a state nonce to its install_id with an expiry.
// Both maps carry an expiresAt so the background purger can sweep
// abandoned StartInstalls that never reached /oauth/callback.
type stashedReserve struct {
	installID string
	expiresAt time.Time
}

// installStash is the in-memory short-lived store mapping install_id →
// (code, state, expiry). Safe for concurrent use; survives only within a
// single admin process — restarts force the operator to re-run StartInstall.
type installStash struct {
	mu      sync.Mutex
	byID    map[string]stashedCode    // install_id → captured code
	byState map[string]stashedReserve // state nonce → install_id (callback lookup)
	clock   func() time.Time          // injectable for tests
}

func newInstallStash() *installStash {
	return &installStash{
		byID:    map[string]stashedCode{},
		byState: map[string]stashedReserve{},
		clock:   time.Now,
	}
}

// reserve records the state nonce for a freshly-issued StartInstall so the
// callback handler can find the matching install_id. The reservation
// itself carries the same TTL as the post-capture stash so abandoned
// installs don't leak state nonces forever.
func (s *installStash) reserve(installID, state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byState[state] = stashedReserve{
		installID: installID,
		expiresAt: s.clock().Add(installStashTTL),
	}
}

// capture stores (code, state) under the install_id resolved from state.
// Returns the install_id on success, or "" if state has no matching
// reservation, the reservation already expired, or this is a replay.
func (s *installStash) capture(state, code string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.byState[state]
	if !ok {
		return ""
	}
	delete(s.byState, state) // single-use; defend against double-callback
	if s.clock().After(r.expiresAt) {
		return ""
	}
	s.byID[r.installID] = stashedCode{
		code:      code,
		state:     state,
		expiresAt: s.clock().Add(installStashTTL),
	}
	return r.installID
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

// purgeExpired removes entries past their TTL from both maps. Safe to
// call concurrently with reserve/capture/consume. Run from a background
// ticker by AdminServer.startStashPurger.
func (s *installStash) purgeExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	for id, sc := range s.byID {
		if now.After(sc.expiresAt) {
			delete(s.byID, id)
		}
	}
	for st, r := range s.byState {
		if now.After(r.expiresAt) {
			delete(s.byState, st)
		}
	}
}

// startStashPurger drives purgeExpired on a fixed-interval ticker until
// ctx is cancelled. Spawned as a goroutine from cmd/admin once the
// listener is up.
func (s *installStash) startStashPurger(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = installStashPurgeInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.purgeExpired()
		}
	}
}
