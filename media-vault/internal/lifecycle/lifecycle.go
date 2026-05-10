// Package lifecycle defines the default object-storage lifecycle rules for
// MIO attachments. Aligned with JetStream MaxAge (7d for inbound enriched).
package lifecycle

import "github.com/crashchat-ai/mio/media-vault/internal/storage"

// DefaultRules returns the lifecycle rules applied to the storage prefix on
// startup. We do not yet add a separate outbound rule — outbound has no
// attachments today (the gateway sender ignores SendCommand.attachments for
// Cliq).
func DefaultRules(prefix string, ageDays int) []storage.LifecycleRule {
	return []storage.LifecycleRule{
		{Prefix: prefix, AgeDays: ageDays},
	}
}
