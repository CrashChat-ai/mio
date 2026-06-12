// Package store — outbound state maps for two-step "thinking…" UX.
//
// OutboundState maps SendCommand.Id → platform external_id returned by the
// first Send call. The pool uses this to resolve Edit targets when only
// edit_of_message_id (the internal send command id) is known.
//
// OutboundState alone is the in-memory LRU; DurableOutboundState wraps it
// with Postgres persistence (survives restarts, multi-replica safe).
package store

import (
	"container/list"
	"context"
	"sync"
	"time"
)

const (
	outboundStateMaxSize = 10_000
	outboundStateTTL     = 10 * time.Minute
)

type outboundEntry struct {
	key        string
	accountID  string
	externalID string
	insertedAt time.Time
}

// OutboundState is a bounded LRU cache of (send_command_id → external_id).
// Capacity: 10k entries; TTL: 10 minutes. Safe for concurrent use.
type OutboundState struct {
	mu      sync.Mutex
	items   map[string]*list.Element
	order   *list.List // LRU: front = most recently used
	maxSize int
	ttl     time.Duration
}

// NewOutboundState returns a ready OutboundState with the default cap and TTL.
func NewOutboundState() *OutboundState {
	return &OutboundState{
		items:   make(map[string]*list.Element),
		order:   list.New(),
		maxSize: outboundStateMaxSize,
		ttl:     outboundStateTTL,
	}
}

// Set stores (sendCommandID → externalID) owned by accountID. Evicts the
// LRU entry if at capacity.
func (s *OutboundState) Set(_ context.Context, sendCommandID, accountID, externalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update existing.
	if el, ok := s.items[sendCommandID]; ok {
		s.order.MoveToFront(el)
		el.Value.(*outboundEntry).accountID = accountID
		el.Value.(*outboundEntry).externalID = externalID
		el.Value.(*outboundEntry).insertedAt = time.Now()
		return
	}

	// Evict LRU if at capacity.
	if s.order.Len() >= s.maxSize {
		tail := s.order.Back()
		if tail != nil {
			entry := tail.Value.(*outboundEntry)
			delete(s.items, entry.key)
			s.order.Remove(tail)
		}
	}

	el := s.order.PushFront(&outboundEntry{
		key:        sendCommandID,
		accountID:  accountID,
		externalID: externalID,
		insertedAt: time.Now(),
	})
	s.items[sendCommandID] = el
}

// Get returns (externalID, true) if found, unexpired, and owned by
// accountID — a correlator from another account never resolves.
func (s *OutboundState) Get(_ context.Context, sendCommandID, accountID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	el, ok := s.items[sendCommandID]
	if !ok {
		return "", false
	}
	entry := el.Value.(*outboundEntry)
	if time.Since(entry.insertedAt) > s.ttl {
		s.order.Remove(el)
		delete(s.items, sendCommandID)
		return "", false
	}
	if entry.accountID != accountID {
		return "", false
	}
	s.order.MoveToFront(el)
	return entry.externalID, true
}

// Len returns the current number of entries.
func (s *OutboundState) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.order.Len()
}
