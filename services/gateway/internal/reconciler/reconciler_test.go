package reconciler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

func TestRunner_ReconcilePublishesFreshAndSuppressesDuplicates(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	pub := &fakePublisher{}
	adapter := &fakeHistoryAdapter{
		page: channels.HistoryPage{
			Messages: []channels.HistoryMessage{
				{
					SourceMessageID:   "cliq-msg-1",
					SenderExternalID:  "bot-1",
					SenderDisplayName: "TobyTime",
					SenderIsBot:       true,
					Text:              "weekday schedule",
					SentAt:            time.Unix(1781194520, 0),
					Attributes:        map[string]string{"cliq_channel_name": "tobytimedev"},
				},
				{
					SourceMessageID:  "cliq-msg-2",
					SenderExternalID: "user-1",
					Text:             "already seen",
				},
			},
			NextCursor: "next-page",
		},
	}
	store.seen["cliq-msg-2"] = uuid.New()

	r := &Runner{
		Store:     store,
		Publisher: pub,
		Adapters:  map[string]channels.HistoryAdapter{"zoho_cliq": adapter},
	}
	res, err := r.Reconcile(ctx, Request{
		TenantID:    uuid.NewString(),
		AccountID:   uuid.NewString(),
		ChannelType: "zoho_cliq",
		Credential:  channels.Credential{AccessToken: "token"},
		Conversation: channels.HistoryConversation{
			ExternalID:  "CT_123",
			DisplayName: "TobyTimeDev",
			Kind:        "CONVERSATION_KIND_CHANNEL_PUBLIC",
			Attributes:  map[string]string{"cliq_channel_name": "tobytimedev"},
		},
		Limit: 100,
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Seen != 2 || res.Inserted != 1 || res.Duplicates != 1 || res.Published != 1 || res.NextCursor != "next-page" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if len(pub.messages) != 1 {
		t.Fatalf("published len = %d, want 1", len(pub.messages))
	}
	msg := pub.messages[0]
	if msg.GetSourceMessageId() != "cliq-msg-1" {
		t.Fatalf("source id = %q", msg.GetSourceMessageId())
	}
	if msg.GetSender().GetDisplayName() != "TobyTime" || !msg.GetSender().GetIsBot() {
		t.Fatalf("sender not preserved: %+v", msg.GetSender())
	}
	if msg.GetConversationExternalId() != "CT_123" {
		t.Fatalf("conversation external id = %q", msg.GetConversationExternalId())
	}
	if msg.GetAttributes()["cliq_channel_name"] != "tobytimedev" {
		t.Fatalf("missing cliq channel attr: %+v", msg.GetAttributes())
	}
	if msg.GetAttributes()["mio_reconciled"] != "true" {
		t.Fatalf("missing reconciled marker: %+v", msg.GetAttributes())
	}
}

func TestRunner_ReconcilePropagatesScopeMissing(t *testing.T) {
	errScope := &channels.ScopeMissingError{
		ChannelType: "zoho_cliq",
		Scope:       "ZohoCliq.Messages.READ",
	}
	r := &Runner{
		Store:     newFakeStore(),
		Publisher: &fakePublisher{},
		Adapters: map[string]channels.HistoryAdapter{
			"zoho_cliq": &fakeHistoryAdapter{err: errScope},
		},
	}
	_, err := r.Reconcile(context.Background(), Request{
		TenantID:    uuid.NewString(),
		AccountID:   uuid.NewString(),
		ChannelType: "zoho_cliq",
		Conversation: channels.HistoryConversation{
			ExternalID: "CT_123",
		},
	})
	if !channels.IsScopeMissing(err) {
		t.Fatalf("expected scope missing, got %T %v", err, err)
	}
}

type fakeHistoryAdapter struct {
	page channels.HistoryPage
	err  error
}

func (f *fakeHistoryAdapter) FetchHistory(_ context.Context, _ channels.HistoryRequest) (channels.HistoryPage, error) {
	return f.page, f.err
}

type fakePublisher struct {
	messages []*miov1.Message
}

func (f *fakePublisher) PublishInbound(_ context.Context, msg *miov1.Message) error {
	f.messages = append(f.messages, msg)
	return nil
}

type fakeStore struct {
	convID uuid.UUID
	seen   map[string]uuid.UUID
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		convID: uuid.New(),
		seen:   map[string]uuid.UUID{},
	}
}

func (f *fakeStore) EnsureConversation(
	_ context.Context,
	_ uuid.UUID,
	_, _, _, _, _ string,
	_ *uuid.UUID,
	_ *string,
	_ *string,
	_ map[string]string,
) (channels.Conversation, error) {
	return channels.Conversation{ID: f.convID}, nil
}

func (f *fakeStore) EnsureUniqueMessage(
	_ context.Context,
	id uuid.UUID,
	_, _, _ string,
	_ *string,
	sourceMessageID, _, _ string,
	_ map[string]string,
) (uuid.UUID, bool, error) {
	if existing, ok := f.seen[sourceMessageID]; ok {
		return existing, false, nil
	}
	f.seen[sourceMessageID] = id
	return id, true, nil
}

func (f *fakeStore) FindMessageBySource(
	_ context.Context,
	_, sourceMessageID string,
) (channels.MessageRef, bool, error) {
	id, ok := f.seen[sourceMessageID]
	if !ok {
		return channels.MessageRef{}, false, nil
	}
	return channels.MessageRef{ID: id, ThreadRootMessageID: id}, true, nil
}
