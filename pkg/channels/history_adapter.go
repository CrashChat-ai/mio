package channels

import (
	"context"
	"errors"
	"fmt"
	"time"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// ErrScopeMissing marks a platform history fetch that failed because the
// stored credential does not have the read scope required for reconciliation.
var ErrScopeMissing = errors.New("channels: history scope missing")

// ScopeMissingError carries the adapter-specific scope/operator hint while
// still allowing callers to branch with errors.Is(err, ErrScopeMissing).
type ScopeMissingError struct {
	ChannelType string
	Scope       string
	Err         error
}

func (e *ScopeMissingError) Error() string {
	if e == nil {
		return ErrScopeMissing.Error()
	}
	if e.Scope == "" {
		return fmt.Sprintf("%s: %s", e.ChannelType, ErrScopeMissing)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: missing scope %s: %v", e.ChannelType, e.Scope, e.Err)
	}
	return fmt.Sprintf("%s: missing scope %s", e.ChannelType, e.Scope)
}

func (e *ScopeMissingError) Unwrap() error {
	if e != nil && e.Err != nil {
		return errors.Join(ErrScopeMissing, e.Err)
	}
	return ErrScopeMissing
}

// IsScopeMissing reports whether err is a platform read-scope failure.
func IsScopeMissing(err error) bool {
	return errors.Is(err, ErrScopeMissing)
}

// HistoryAdapter is an optional channel capability for platforms whose
// webhooks are not a complete source of truth. It is deliberately outside the
// hot-path Adapter interface so webhook/outbound-only channels do not need to
// implement history.
type HistoryAdapter interface {
	FetchHistory(ctx context.Context, req HistoryRequest) (HistoryPage, error)
}

// HistoryConversation identifies the platform conversation being reconciled.
// ExternalID is the API-native conversation/chat ID, not the MIO UUID.
type HistoryConversation struct {
	ExternalID  string
	DisplayName string
	Kind        string
	Attributes  map[string]string
}

// HistoryRequest is one bounded pull from a provider history API.
type HistoryRequest struct {
	Credential   Credential
	Conversation HistoryConversation
	Cursor       string
	Since        time.Time
	Until        time.Time
	Limit        int
}

// HistoryPage contains normalized provider history rows plus the provider
// cursor for the next pull, when supported.
type HistoryPage struct {
	Messages   []HistoryMessage
	NextCursor string
}

// HistoryMessage is the channel-agnostic normalized result of a provider
// history row. Reconciler code assigns MIO UUIDs and publishes the final proto.
type HistoryMessage struct {
	SourceMessageID   string
	SenderExternalID  string
	SenderDisplayName string
	SenderIsBot       bool
	Text              string
	SentAt            time.Time
	ParentExternalID  string
	Attributes        map[string]string
	Attachments       []*miov1.Attachment
}
