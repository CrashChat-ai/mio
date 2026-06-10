package zohocliq

import (
	"testing"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/google/uuid"
)

func TestBuildInboundProtoMessage_ThreadReplyResolvedParent(t *testing.T) {
	t.Parallel()

	msgID := uuid.New()
	convID := uuid.New()
	threadRootID := uuid.New()
	targetMsgID := uuid.New()

	msg := buildInboundProtoMessage(
		normalizedThreadReply(),
		msgID,
		"tenant-001",
		"account-001",
		convID,
		threadRootID.String(),
		targetMsgID.String(),
	)

	if msg.GetThreadRootMessageId() != threadRootID.String() {
		t.Fatalf("thread_root_message_id = %q, want %q", msg.GetThreadRootMessageId(), threadRootID.String())
	}
	relation := msg.GetRelation()
	if relation == nil {
		t.Fatal("relation is nil")
	}
	if relation.GetKind() != miov1.MessageRelation_KIND_REPLY {
		t.Errorf("relation.kind = %s, want KIND_REPLY", relation.GetKind())
	}
	if relation.GetTargetMessageId() != targetMsgID.String() {
		t.Errorf("relation.target_message_id = %q, want %q", relation.GetTargetMessageId(), targetMsgID.String())
	}
	if relation.GetTargetExternalId() != "parent-cliq-msg-001" {
		t.Errorf("relation.target_external_id = %q, want parent-cliq-msg-001", relation.GetTargetExternalId())
	}
}

func TestBuildInboundProtoMessage_ThreadReplyUnresolvedParent(t *testing.T) {
	t.Parallel()

	msg := buildInboundProtoMessage(
		normalizedThreadReply(),
		uuid.New(),
		"tenant-001",
		"account-001",
		uuid.New(),
		"",
		"",
	)

	if msg.GetThreadRootMessageId() != "" {
		t.Fatalf("thread_root_message_id = %q, want empty", msg.GetThreadRootMessageId())
	}
	relation := msg.GetRelation()
	if relation == nil {
		t.Fatal("relation is nil")
	}
	if relation.GetKind() != miov1.MessageRelation_KIND_REPLY {
		t.Errorf("relation.kind = %s, want KIND_REPLY", relation.GetKind())
	}
	if relation.GetTargetMessageId() != "" {
		t.Errorf("relation.target_message_id = %q, want empty", relation.GetTargetMessageId())
	}
	if relation.GetTargetExternalId() != "parent-cliq-msg-001" {
		t.Errorf("relation.target_external_id = %q, want parent-cliq-msg-001", relation.GetTargetExternalId())
	}
}

func normalizedThreadReply() *NormalizedMessage {
	return &NormalizedMessage{
		ConversationExternalID:  "cliq-chat-001",
		ConversationKind:        "CONVERSATION_KIND_THREAD",
		ConversationDisplayName: "#General",
		ParentExternalID:        "parent-cliq-msg-001",
		SourceMessageID:         "child-cliq-msg-001",
		SenderExternalID:        "user-001",
		SenderDisplayName:       "Alice",
		Text:                    "replying here",
		Attributes:              map[string]string{"cliq_replied_message_id": "parent-cliq-msg-001"},
	}
}
