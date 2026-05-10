package sdk

import (
	"errors"
	"fmt"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// SchemaVersion is the current wire schema version enforced on publish.
//
// Asymmetry contract (locked — do not relax):
//   - Publish side: Verify() hard-rejects any mismatch.
//   - Consume side: NO Verify call. Consumers must tolerate unknown major versions
//     to remain forward-compatible with future schema additions.
//     Defense-in-depth rejection (e.g., gateway outbound) is a per-phase concern.
const SchemaVersion = 1

// ErrSchemaMismatch is returned when a message carries a schema_version != SchemaVersion.
type ErrSchemaMismatch struct {
	Got int32
}

func (e *ErrSchemaMismatch) Error() string {
	return fmt.Sprintf("schema_version mismatch: got %d, want %d; upgrade the publisher or SDK", e.Got, SchemaVersion)
}

// ErrMissingField is returned when a required four-tier ID is empty.
type ErrMissingField struct {
	Field string
}

func (e *ErrMissingField) Error() string {
	return fmt.Sprintf("required field %q is empty; all four-tier IDs (tenant_id, account_id, channel_type, conversation_id) must be set at publish time", e.Field)
}

// Verify validates a Message before publish.
//
// Checks performed (publish-side only — see asymmetry note above):
//  1. schema_version must equal SchemaVersion (== 1).
//  2. tenant_id, account_id, channel_type, conversation_id must all be non-empty.
//  3. channel_type must be present in the active registry (Known map).
//
// Returns nil on success; a typed error on any violation.
func Verify(msg *miov1.Message) error {
	if msg == nil {
		return errors.New("message is nil")
	}
	if msg.SchemaVersion != SchemaVersion {
		return &ErrSchemaMismatch{Got: msg.SchemaVersion}
	}
	for field, val := range map[string]string{
		"tenant_id":       msg.TenantId,
		"account_id":      msg.AccountId,
		"channel_type":    msg.ChannelType,
		"conversation_id": msg.ConversationId,
	} {
		if val == "" {
			return &ErrMissingField{Field: field}
		}
	}
	if !Known[msg.ChannelType] {
		// Also accept deprecated aliases.
		if _, ok := Aliases[msg.ChannelType]; !ok {
			return &ErrUnknownChannelType{ChannelType: msg.ChannelType}
		}
	}
	return nil
}

// VerifyCommand validates a SendCommand before publish.
//
// Same asymmetry rule applies: call only on publish; skip on consume.
func VerifyCommand(cmd *miov1.SendCommand) error {
	if cmd == nil {
		return errors.New("send_command is nil")
	}
	if cmd.SchemaVersion != SchemaVersion {
		return &ErrSchemaMismatch{Got: cmd.SchemaVersion}
	}
	for field, val := range map[string]string{
		"id":              cmd.Id,
		"tenant_id":       cmd.TenantId,
		"account_id":      cmd.AccountId,
		"channel_type":    cmd.ChannelType,
		"conversation_id": cmd.ConversationId,
	} {
		if val == "" {
			return &ErrMissingField{Field: field}
		}
	}
	if !Known[cmd.ChannelType] {
		if _, ok := Aliases[cmd.ChannelType]; !ok {
			return &ErrUnknownChannelType{ChannelType: cmd.ChannelType}
		}
	}
	return nil
}
