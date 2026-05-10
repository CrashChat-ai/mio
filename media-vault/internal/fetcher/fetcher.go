// Package fetcher streams attachment bytes from a channel platform.
// Implementations live under sub-packages (zohocliq, slack, ...).
package fetcher

import (
	"context"
	"fmt"
	"io"
	"sync"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// Fetcher fetches attachment bytes from a single platform.
type Fetcher interface {
	// ChannelType returns the registry slug ("zoho_cliq").
	ChannelType() string

	// Fetch streams bytes for the attachment to dst. Honours ctx deadline.
	// Returns FetchError with .Code populated when the platform-side state
	// is the cause (expired, forbidden, not found, too large). Otherwise,
	// transient errors bubble up so the worker can Nak.
	Fetch(ctx context.Context, att *miov1.Attachment, dst io.Writer) (Result, error)
}

// Result is what Fetch returns on success.
type Result struct {
	Bytes       int64
	SHA256Hex   string
	ContentType string
}

// Error wraps platform errors with a bounded ErrorCode the worker maps to
// Attachment.error_code.
type Error struct {
	Code miov1.Attachment_ErrorCode
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("fetch %s: %s", e.Code.String(), e.Msg) }

// IsTerminal reports whether the error indicates the message should be
// terminated (no point retrying — the platform-side state won't change).
func IsTerminal(err error) bool {
	if e, ok := err.(*Error); ok {
		switch e.Code {
		case miov1.Attachment_ERROR_CODE_EXPIRED,
			miov1.Attachment_ERROR_CODE_FORBIDDEN,
			miov1.Attachment_ERROR_CODE_NOT_FOUND,
			miov1.Attachment_ERROR_CODE_TOO_LARGE:
			return true
		}
	}
	return false
}

// Registry maps channel_type → Fetcher. Adapter packages register themselves
// from init(); the binary blank-imports them in main.go.
var (
	registryMu sync.RWMutex
	registry   = map[string]Fetcher{}
)

// Register makes f available to Lookup. Safe to call from init().
func Register(f Fetcher) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[f.ChannelType()] = f
}

// Lookup returns the fetcher for channelType, or nil if none registered.
func Lookup(channelType string) Fetcher {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[channelType]
}
