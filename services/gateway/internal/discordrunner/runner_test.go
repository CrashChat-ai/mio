package discordrunner

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	_ "github.com/crashchat-ai/mio/channels/discord" // register the adapter for lookupInbound
	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"log/slog"
)

type fakeIngester struct {
	mu   sync.Mutex
	msgs []*miov1.Message
}

func (f *fakeIngester) Ingest(_ context.Context, msg *miov1.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg)
	return "persisted", nil
}

func (f *fakeIngester) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

func discordInbound(t *testing.T) channels.InboundAdapter {
	t.Helper()
	inbound := lookupInbound()
	if inbound == nil {
		t.Fatal("discord adapter not registered (blank import missing)")
	}
	return inbound
}

func drive(t *testing.T, events []*discordgo.Event) *fakeIngester {
	t.Helper()
	ing := &fakeIngester{}
	r := &runner{ing: ing, inbound: discordInbound(t), logger: slog.Default()}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := make(chan []byte, ingestQueueDepth)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.ingestLoop(ctx, queue)
	}()
	for _, evt := range events {
		r.enqueue(ctx, evt, queue)
	}
	// Cancel-driven shutdown (the queue is never closed — see Start); poll
	// until the serial loop has drained what we enqueued.
	deadline := time.Now().Add(5 * time.Second)
	for len(queue) > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ingest loop did not stop")
	}
	return ing
}

func rawEvent(t *testing.T, typ string, d map[string]any) *discordgo.Event {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return &discordgo.Event{Type: typ, RawData: raw}
}

func TestMessageCreateFlowsToIngester(t *testing.T) {
	ing := drive(t, []*discordgo.Event{
		rawEvent(t, "MESSAGE_CREATE", map[string]any{
			"id": "1210000000000000001", "channel_id": "1200000000000000001",
			"guild_id": "1190000000000000001",
			"author":   map[string]any{"id": "980000000000000001", "username": "alice"},
			"content":  "hi",
		}),
	})
	if ing.count() != 1 {
		t.Fatalf("ingested = %d, want 1", ing.count())
	}
	msg := ing.msgs[0]
	if msg.GetChannelType() != "discord" || msg.GetSourceMessageId() != "1200000000000000001:1210000000000000001" {
		t.Errorf("wrong message: type=%q source=%q", msg.GetChannelType(), msg.GetSourceMessageId())
	}
}

func TestUnwantedAndBotEventsDropped(t *testing.T) {
	ing := drive(t, []*discordgo.Event{
		rawEvent(t, "TYPING_START", map[string]any{"channel_id": "c"}),
		rawEvent(t, "PRESENCE_UPDATE", map[string]any{}),
		rawEvent(t, "MESSAGE_CREATE", map[string]any{ // bot echo → soft drop
			"id": "1", "channel_id": "2",
			"author": map[string]any{"id": "9", "bot": true},
		}),
		{Type: "MESSAGE_CREATE"}, // empty RawData → dropped at enqueue
	})
	if ing.count() != 0 {
		t.Fatalf("ingested = %d, want 0", ing.count())
	}
}

func TestStartRequiresToken(t *testing.T) {
	err := Start(context.Background(), Deps{})
	if err == nil {
		t.Fatal("Start without token must error")
	}
}
