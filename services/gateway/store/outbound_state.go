// Package store — in-memory outbound state map for two-step "thinking…" UX.
//
// OutboundState maps SendCommand.Id → platform external_id returned by the
// first Send call. The pool uses this to resolve Edit targets when only
// edit_of_message_id (the internal send command id) is known.
//
// Failure mode (documented, accepted for POC): gateway restart between the
// initial send and the final edit clears this map. The pool falls back to a
// fresh Send — the "thinking…" message is left dangling. Metric:
// mio_gateway_outbound_edit_fallback_total{reason="state_missing"}.
// Persistent storage is deferred until that metric goes non-zero in production.
package store

import (
	"container/list"
	"sync"
	"time"
)

const (
	outboundStateMaxSize = 10_000
	outboundStateTTL     = 10 * time.Minute
)

type outboundEntry struct {
	key        string
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

// Set stores (sendCommandID → externalID). Evicts the LRU entry if at capacity.
func (s *OutboundState) Set(sendCommandID, externalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update existing.
	if el, ok := s.items[sendCommandID]; ok {
		s.order.MoveToFront(el)
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
		externalID: externalID,
		insertedAt: time.Now(),
	})
	s.items[sendCommandID] = el
}

// Get returns (externalID, true) if found and not expired, else ("", false).
func (s *OutboundState) Get(sendCommandID string) (string, bool) {
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
	s.order.MoveToFront(el)
	return entry.externalID, true
}

// Len returns the current number of entries.
func (s *OutboundState) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.order.Len()
}
