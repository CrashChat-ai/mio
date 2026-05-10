// Package sdk provides publish/consume helpers for the MIO NATS JetStream bus.
//
// Subject grammar (locked from P2 plan + arch-doc §5):
//
//	mio.<direction>.<channel_type>.<account_id>.<conversation_id>[.<message_id>]
//
// The 6th segment (message_id) is reserved for outbound edit/delete commands only.
// Inbound subjects are always 5 tokens (4-segment key after "mio").
//
// Token rules: only [a-zA-Z0-9_-] allowed. Dots split the NATS subject hierarchy.
// UUIDs and ULIDs are safe; free-text platform identifiers must be normalized first.
package sdk

import (
	"errors"
	"fmt"
	"regexp"
)

// tokenRE matches valid NATS subject tokens.
// Only alphanumeric, underscore, hyphen allowed.
// Dots are forbidden — they split the subject hierarchy.
var tokenRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ErrEmptyToken is returned when a subject token is the empty string.
var ErrEmptyToken = errors.New("subject token must not be empty")

// ErrInvalidToken is returned when a subject token contains illegal characters.
type ErrInvalidToken struct {
	Token string
}

func (e *ErrInvalidToken) Error() string {
	return fmt.Sprintf("subject token %q contains illegal characters; only [a-zA-Z0-9_-] allowed (dots split NATS subjects)", e.Token)
}

// ErrUnknownChannelType is returned when channel_type is not in the active registry.
type ErrUnknownChannelType struct {
	ChannelType string
}

func (e *ErrUnknownChannelType) Error() string {
	return fmt.Sprintf("channel_type %q not found in active registry (proto/channels.yaml); add it and re-run make proto-gen", e.ChannelType)
}

// validateToken rejects empty tokens and tokens with illegal characters.
func validateToken(t string) error {
	if t == "" {
		return ErrEmptyToken
	}
	if !tokenRE.MatchString(t) {
		return &ErrInvalidToken{Token: t}
	}
	return nil
}

// validateChannelType rejects channel_type values not in the active registry.
// Accepts deprecated aliases as well (maps to current slug internally).
func validateChannelType(ct string) error {
	if Known[ct] {
		return nil
	}
	// Check aliases — accept both alias and current name.
	if _, ok := Aliases[ct]; ok {
		return nil
	}
	return &ErrUnknownChannelType{ChannelType: ct}
}

// Inbound builds a 4-token inbound subject:
//
//	mio.inbound.<channel_type>.<account_id>.<conversation_id>
//
// Rejects empty tokens, tokens containing dots, and unknown channel_type.
// This is the only inbound subject form; the 5th segment is outbound-only.
func Inbound(channelType, accountID, conversationID string) (string, error) {
	if err := validateChannelType(channelType); err != nil {
		return "", err
	}
	for _, tok := range []string{channelType, accountID, conversationID} {
		if err := validateToken(tok); err != nil {
			return "", fmt.Errorf("inbound subject: %w", err)
		}
	}
	return fmt.Sprintf("mio.inbound.%s.%s.%s", channelType, accountID, conversationID), nil
}

// InboundEnriched builds the enriched-stream inbound subject:
//
//	mio.inbound_enriched.<channel_type>.<account_id>.<conversation_id>
//
// Emitted by attachment-downloader after attachment URLs are rewritten to
// stable storage URLs. Same shape as Inbound but with the "_enriched" verb.
func InboundEnriched(channelType, accountID, conversationID string) (string, error) {
	if err := validateChannelType(channelType); err != nil {
		return "", err
	}
	for _, tok := range []string{channelType, accountID, conversationID} {
		if err := validateToken(tok); err != nil {
			return "", fmt.Errorf("inbound_enriched subject: %w", err)
		}
	}
	return fmt.Sprintf("mio.inbound_enriched.%s.%s.%s", channelType, accountID, conversationID), nil
}

// Outbound builds an outbound subject:
//
//	mio.outbound.<channel_type>.<account_id>.<conversation_id>           (no messageID)
//	mio.outbound.<channel_type>.<account_id>.<conversation_id>.<msg_id>  (edit/delete)
//
// The optional 6th segment (messageID) is used only for edit/delete commands.
func Outbound(channelType, accountID, conversationID string, messageID ...string) (string, error) {
	if err := validateChannelType(channelType); err != nil {
		return "", err
	}
	for _, tok := range []string{channelType, accountID, conversationID} {
		if err := validateToken(tok); err != nil {
			return "", fmt.Errorf("outbound subject: %w", err)
		}
	}
	base := fmt.Sprintf("mio.outbound.%s.%s.%s", channelType, accountID, conversationID)
	if len(messageID) > 0 && messageID[0] != "" {
		msgID := messageID[0]
		if err := validateToken(msgID); err != nil {
			return "", fmt.Errorf("outbound subject message_id: %w", err)
		}
		return base + "." + msgID, nil
	}
	return base, nil
}
