package socketrunner

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crashchat-ai/mio/pkg/channels"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/slack-go/slack/socketmode"
	"log/slog"
)

type fakeSocket struct {
	events chan socketmode.Event
	runs   chan error

	mu       sync.Mutex
	acked    []string
	runCalls int
}

func newFakeSocket() *fakeSocket {
	return &fakeSocket{
		events: make(chan socketmode.Event, 16),
		runs:   make(chan error, 16),
	}
}

func (f *fakeSocket) Run(ctx context.Context) error {
	f.mu.Lock()
	f.runCalls++
	f.mu.Unlock()
	select {
	case err := <-f.runs:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeSocket) Events() <-chan socketmode.Event { return f.events }

func (f *fakeSocket) Ack(req socketmode.Request) {
	f.mu.Lock()
	f.acked = append(f.acked, req.EnvelopeID)
	f.mu.Unlock()
}

func (f *fakeSocket) ackCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.acked)
}

func (f *fakeSocket) runCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runCalls
}

// fakeInbound normalizes a payload into a Message whose SourceMessageId is the
// raw payload bytes, so the dedup test can key on payload identity.
type fakeInbound struct{ soft bool }

func (f *fakeInbound) VerifySignature(http.Header, []byte) error { return nil }
func (f *fakeInbound) HandleHandshake(http.ResponseWriter, *http.Request) bool {
	return false
}

func (f *fakeInbound) Normalize(raw []byte) (*miov1.Message, error) {
	if f.soft {
		return nil, channels.ErrNormalizeSoft
	}
	return &miov1.Message{SourceMessageId: string(raw)}, nil
}

// fakeIngester dedups on SourceMessageId, mirroring EnsureUniqueMessage.
type fakeIngester struct {
	mu        sync.Mutex
	seen      map[string]bool
	published int32
	ingested  int32
}

func newFakeIngester() *fakeIngester { return &fakeIngester{seen: map[string]bool{}} }

func (f *fakeIngester) Ingest(_ context.Context, msg *miov1.Message) (string, error) {
	atomic.AddInt32(&f.ingested, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seen[msg.GetSourceMessageId()] {
		return "dedup", nil
	}
	f.seen[msg.GetSourceMessageId()] = true
	atomic.AddInt32(&f.published, 1)
	return "success", nil
}

func newRunner(sock socketClient, inb channels.InboundAdapter, ing ingester) *runner {
	return &runner{client: sock, inbound: inb, ing: ing, logger: slog.Default(), backoff: time.Millisecond}
}

func eventsAPI(envID string, payload string) socketmode.Event {
	return socketmode.Event{
		Type:    socketmode.EventTypeEventsAPI,
		Request: &socketmode.Request{EnvelopeID: envID, Payload: []byte(payload)},
	}
}

func TestRunner_AcksAndIngests(t *testing.T) {
	sock := newFakeSocket()
	ing := newFakeIngester()
	r := newRunner(sock, &fakeInbound{}, ing)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.run(ctx); close(done) }()

	sock.events <- eventsAPI("env-1", `{"a":1}`)

	waitFor(t, func() bool { return atomic.LoadInt32(&ing.published) == 1 && sock.ackCount() == 1 })
	cancel()
	<-done
}

func TestRunner_NoDuplicatePublishOnReplay(t *testing.T) {
	sock := newFakeSocket()
	ing := newFakeIngester()
	r := newRunner(sock, &fakeInbound{}, ing)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.run(ctx); close(done) }()

	// Same envelope payload delivered twice (Socket reconnect redelivery).
	sock.events <- eventsAPI("env-1", `{"id":"X"}`)
	sock.events <- eventsAPI("env-1-retry", `{"id":"X"}`)

	waitFor(t, func() bool { return atomic.LoadInt32(&ing.ingested) == 2 })
	if got := atomic.LoadInt32(&ing.published); got != 1 {
		t.Fatalf("published = %d, want 1 (idempotency must dedup the replay)", got)
	}
	if sock.ackCount() != 2 {
		t.Fatalf("ack count = %d, want 2 (both envelopes acked even when deduped)", sock.ackCount())
	}
	cancel()
	<-done
}

func TestRunner_SoftNormalizeDropped(t *testing.T) {
	sock := newFakeSocket()
	ing := newFakeIngester()
	r := newRunner(sock, &fakeInbound{soft: true}, ing)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.run(ctx); close(done) }()

	sock.events <- eventsAPI("env-1", `{}`)
	waitFor(t, func() bool { return sock.ackCount() == 1 })
	time.Sleep(20 * time.Millisecond)
	if got := atomic.LoadInt32(&ing.published); got != 0 {
		t.Fatalf("soft normalize must not publish, got %d", got)
	}
	cancel()
	<-done
}

func TestConnectLoop_ReconnectsOnTransient(t *testing.T) {
	sock := newFakeSocket()
	r := newRunner(sock, &fakeInbound{}, newFakeIngester())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { r.connectLoop(ctx); close(done) }()

	sock.runs <- errors.New("connection reset by peer")
	waitFor(t, func() bool { return sock.runCount() >= 2 })
	cancel()
	<-done
}

func TestConnectLoop_StopsOnPermanentAuth(t *testing.T) {
	sock := newFakeSocket()
	r := newRunner(sock, &fakeInbound{}, newFakeIngester())

	done := make(chan struct{})
	go func() { r.connectLoop(context.Background()); close(done) }()

	sock.runs <- errors.New("invalid_auth")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connectLoop must stop on permanent auth error")
	}
	if got := sock.runCount(); got != 1 {
		t.Fatalf("run called %d times, want 1 (no retry on permanent auth)", got)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}
