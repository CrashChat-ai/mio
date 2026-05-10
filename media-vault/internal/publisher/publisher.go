// Package publisher writes enriched Messages to the MESSAGES_INBOUND_ENRICHED
// JetStream stream so AI consumers can read attachment-rewritten messages.
package publisher

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// StreamName is the enriched-output JetStream stream.
const StreamName = "MESSAGES_INBOUND_ENRICHED"

// SubjectPrefix mirrors the inbound shape with a different verb.
const SubjectPrefix = "mio.inbound_enriched"

// Publisher writes proto-marshaled enriched Messages to NATS.
type Publisher struct {
	js jetstream.JetStream
}

// New constructs a Publisher.
func New(js jetstream.JetStream) *Publisher { return &Publisher{js: js} }

// EnsureStream provisions MESSAGES_INBOUND_ENRICHED idempotently. Mirrors
// gateway/internal/store EnsureStreams shape.
func EnsureStream(ctx context.Context, js jetstream.JetStream, replicas int) error {
	cfg := jetstream.StreamConfig{
		Name:        StreamName,
		Subjects:    []string{SubjectPrefix + ".>"},
		Retention:   jetstream.LimitsPolicy,
		MaxAge:      7 * 24 * time.Hour,
		Storage:     jetstream.FileStorage,
		Replicas:    replicas,
		Duplicates:  2 * time.Minute,
		Description: "Inbound messages with attachment URLs rewritten to stable storage URLs",
	}
	_, err := js.CreateOrUpdateStream(ctx, cfg)
	if err != nil {
		return fmt.Errorf("publisher: ensure stream %q: %w", StreamName, err)
	}
	return nil
}

// Subject returns the per-message subject under SubjectPrefix.
func Subject(channelType, accountID, conversationID string) string {
	return fmt.Sprintf("%s.%s.%s.%s", SubjectPrefix, channelType, accountID, conversationID)
}

// Publish marshals and publishes msg under its enriched subject.
// Sets Nats-Msg-Id = "enr:<msg.id>" so JetStream's DuplicateWindow drops
// re-deliveries within 2m.
func (p *Publisher) Publish(ctx context.Context, msg *miov1.Message) error {
	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("publisher: marshal: %w", err)
	}
	subj := Subject(msg.GetChannelType(), msg.GetAccountId(), msg.GetConversationId())
	natsMsg := &nats.Msg{
		Subject: subj,
		Data:    data,
		Header:  nats.Header{},
	}
	if id := msg.GetId(); id != "" {
		natsMsg.Header.Set("Nats-Msg-Id", "enr:"+id)
	}
	if _, err := p.js.PublishMsg(ctx, natsMsg); err != nil {
		return fmt.Errorf("publisher: publish %s: %w", subj, err)
	}
	return nil
}
