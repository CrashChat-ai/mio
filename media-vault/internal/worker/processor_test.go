package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"

	"github.com/crashchat-ai/mio/media-vault/internal/fetcher"
	"github.com/crashchat-ai/mio/media-vault/internal/storage"
)

// stubFetcher satisfies fetcher.Fetcher with a fixed payload + content-type.
type stubFetcher struct {
	channel string
	body    []byte
	ct      string
	failErr error
}

func (s *stubFetcher) ChannelType() string { return s.channel }
func (s *stubFetcher) Fetch(_ context.Context, _ *miov1.Attachment, dst io.Writer) (fetcher.Result, error) {
	if s.failErr != nil {
		return fetcher.Result{}, s.failErr
	}
	if _, err := dst.Write(s.body); err != nil {
		return fetcher.Result{}, err
	}
	h := sha256.Sum256(s.body)
	return fetcher.Result{Bytes: int64(len(s.body)), SHA256Hex: hex.EncodeToString(h[:]), ContentType: s.ct}, nil
}

// memStorage is a tiny in-memory backend that records writes (used to assert
// dedup behaviour and that PutOptions metadata is wired correctly). Not a
// faithful GCS — just enough surface for the processor.
type memStorage struct {
	mu       sync.Mutex
	objects  map[string][]byte
	lastOpts storage.PutOptions
	puts     int
}

func newMem() *memStorage { return &memStorage{objects: map[string][]byte{}} }

func (m *memStorage) Backend() string { return "mem" }
func (m *memStorage) Put(_ context.Context, key string, body io.Reader, _ int64, opts storage.PutOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if opts.IfNotExists {
		if _, ok := m.objects[key]; ok {
			return storage.ErrAlreadyExists
		}
	}
	b, _ := io.ReadAll(body)
	m.objects[key] = b
	m.lastOpts = opts
	m.puts++
	return nil
}
func (m *memStorage) Get(_ context.Context, _ string) (io.ReadCloser, *storage.Object, error) {
	return nil, nil, storage.ErrUnsupported
}
func (m *memStorage) Stat(_ context.Context, key string) (*storage.Object, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objects[key]; !ok {
		return nil, storage.ErrNotFound
	}
	return &storage.Object{Key: key}, nil
}
func (m *memStorage) Delete(_ context.Context, _ string) error { return nil }
func (m *memStorage) List(_ context.Context, _ string) (<-chan storage.Object, <-chan error) {
	out, errCh := make(chan storage.Object), make(chan error, 1)
	close(out)
	errCh <- nil
	close(errCh)
	return out, errCh
}
func (m *memStorage) SignedURL(_ context.Context, key string, _ storage.SignOptions) (string, error) {
	return "https://signed.test/" + key, nil
}
func (m *memStorage) SetLifecycle(_ context.Context, _ []storage.LifecycleRule) error { return nil }

type recPub struct {
	mu    sync.Mutex
	calls []*miov1.Message
}

func (r *recPub) Publish(_ context.Context, m *miov1.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, m)
	return nil
}

func newProc(t *testing.T, store storage.Storage, pub Publisher) *EnrichingProcessor {
	t.Helper()
	return &EnrichingProcessor{
		Storage:       store,
		Publisher:     pub,
		StorageBucket: "test-bucket",
		StoragePrefix: "mio/attachments/",
		Log:           slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

func registerStub(t *testing.T, channel string, body []byte) {
	t.Helper()
	fetcher.Register(&stubFetcher{channel: channel, body: body, ct: "image/png"})
}

func TestProcessRewritesAttachmentAndPublishes(t *testing.T) {
	registerStub(t, "ch_a", []byte("payload"))
	mem := newMem()
	pub := &recPub{}
	p := newProc(t, mem, pub)

	msg := &miov1.Message{
		Id:          "m1",
		ChannelType: "ch_a",
		AccountId:   "acc",
		ReceivedAt:  timestamppb.New(time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)),
		Attachments: []*miov1.Attachment{{Url: "https://platform/x"}},
	}
	if err := p.Process(t.Context(), msg); err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	att := msg.Attachments[0]
	wantKeyPrefix := "gs://test-bucket/mio/attachments/channel_type=ch_a/"
	if att.StorageKey == "" || !strings.HasPrefix(att.StorageKey, wantKeyPrefix) {
		t.Fatalf("storage_key not set/shape: %q (want prefix %q)", att.StorageKey, wantKeyPrefix)
	}
	if att.ContentSha256 == "" {
		t.Fatal("content_sha256 unset")
	}
	if att.ErrorCode != miov1.Attachment_ERROR_CODE_OK {
		t.Fatalf("error_code = %v", att.ErrorCode)
	}
	// url must be preserved as the original platform URL, not overwritten with a signed GCS URL.
	if att.Url != "https://platform/x" {
		t.Fatalf("url must be original platform url, got: %q", att.Url)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("publish calls = %d, want 1", len(pub.calls))
	}
}

func TestProcessStampsOwnerIdentifiersOnPutOptions(t *testing.T) {
	registerStub(t, "ch_meta", []byte("payload-meta"))
	mem := newMem()
	pub := &recPub{}
	p := newProc(t, mem, pub)

	msg := &miov1.Message{
		Id:              "m-meta",
		ChannelType:     "ch_meta",
		TenantId:        "tenant-T",
		AccountId:       "account-A",
		ConversationId:  "conversation-C",
		SourceMessageId: "src-msg-S",
		ReceivedAt:      timestamppb.New(time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)),
		Attachments:     []*miov1.Attachment{{Url: "https://platform/x"}},
	}
	if err := p.Process(t.Context(), msg); err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	opts := mem.lastOpts
	if opts.TenantID != "tenant-T" {
		t.Errorf("PutOptions.TenantID = %q; want tenant-T", opts.TenantID)
	}
	if opts.AccountID != "account-A" {
		t.Errorf("PutOptions.AccountID = %q; want account-A", opts.AccountID)
	}
	if opts.ConversationID != "conversation-C" {
		t.Errorf("PutOptions.ConversationID = %q; want conversation-C", opts.ConversationID)
	}
	if opts.SourceMessageID != "src-msg-S" {
		t.Errorf("PutOptions.SourceMessageID = %q; want src-msg-S", opts.SourceMessageID)
	}
	if !opts.IfNotExists {
		t.Error("PutOptions.IfNotExists should remain true (dedup contract)")
	}
}

func TestProcessDedupsRepeatedSameSha(t *testing.T) {
	registerStub(t, "ch_b", []byte("same-bytes"))
	mem := newMem()
	pub := &recPub{}
	p := newProc(t, mem, pub)

	for i := 0; i < 2; i++ {
		msg := &miov1.Message{
			Id: "m", ChannelType: "ch_b", AccountId: "acc",
			ReceivedAt:  timestamppb.New(time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)),
			Attachments: []*miov1.Attachment{{Url: "https://platform/x"}},
		}
		if err := p.Process(t.Context(), msg); err != nil {
			t.Fatal(err)
		}
	}
	if mem.puts != 1 {
		t.Fatalf("dedup failed: puts=%d, want 1", mem.puts)
	}
	if len(pub.calls) != 2 {
		t.Fatalf("expected 2 publishes (every message regardless of dedup), got %d", len(pub.calls))
	}
}

func TestProcessHandlesFetcherErrorWithoutCrash(t *testing.T) {
	fetcher.Register(&stubFetcher{
		channel: "ch_c", failErr: &fetcher.Error{Code: miov1.Attachment_ERROR_CODE_EXPIRED, Msg: "expired"},
	})
	mem := newMem()
	pub := &recPub{}
	p := newProc(t, mem, pub)

	msg := &miov1.Message{
		ChannelType: "ch_c", AccountId: "acc", Id: "m_exp",
		ReceivedAt:  timestamppb.New(time.Now().UTC()),
		Attachments: []*miov1.Attachment{{Url: "https://platform/x"}},
	}
	if err := p.Process(t.Context(), msg); err != nil {
		t.Fatalf("processor must publish even on partial failures: %v", err)
	}
	if msg.Attachments[0].ErrorCode != miov1.Attachment_ERROR_CODE_EXPIRED {
		t.Fatalf("expected EXPIRED, got %v", msg.Attachments[0].ErrorCode)
	}
	if mem.puts != 0 {
		t.Fatalf("must not write on fetcher error, puts=%d", mem.puts)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("must still publish enriched msg, got %d", len(pub.calls))
	}
}

func TestProcessTextOnlyMessagePublishesUnchanged(t *testing.T) {
	mem := newMem()
	pub := &recPub{}
	p := newProc(t, mem, pub)
	msg := &miov1.Message{
		ChannelType: "ch_text", AccountId: "acc", Id: "m_text",
		ReceivedAt: timestamppb.New(time.Now().UTC()),
		// no attachments — text-only ping
	}
	if err := p.Process(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	if mem.puts != 0 {
		t.Fatalf("text-only msg must not write storage, puts=%d", mem.puts)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("text-only msg must publish exactly once, got %d", len(pub.calls))
	}
}

func TestProcessAlreadyEnrichedMessageRepublishesUnchanged(t *testing.T) {
	mem := newMem()
	pub := &recPub{}
	p := newProc(t, mem, pub)
	msg := &miov1.Message{
		ChannelType: "ch_enr", AccountId: "acc", Id: "m_enr",
		ReceivedAt: timestamppb.New(time.Now().UTC()),
		Attachments: []*miov1.Attachment{
			{StorageKey: "preexisting/key", Url: "https://signed.test/preexisting/key"},
		},
	}
	if err := p.Process(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	if mem.puts != 0 {
		t.Fatalf("already-enriched attachments must not be re-fetched, puts=%d", mem.puts)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("must still publish replay, got %d", len(pub.calls))
	}
}

func TestProcessTransientFetchErrorMarksTimeout(t *testing.T) {
	fetcher.Register(&stubFetcher{channel: "ch_transient", failErr: errors.New("network blip")})
	mem := newMem()
	pub := &recPub{}
	p := newProc(t, mem, pub)
	msg := &miov1.Message{
		ChannelType: "ch_transient", AccountId: "acc", Id: "m_t",
		ReceivedAt:  timestamppb.New(time.Now().UTC()),
		Attachments: []*miov1.Attachment{{Url: "https://platform/x"}},
	}
	if err := p.Process(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	if msg.Attachments[0].ErrorCode != miov1.Attachment_ERROR_CODE_TIMEOUT {
		t.Fatalf("expected TIMEOUT for transient fetch err, got %v", msg.Attachments[0].ErrorCode)
	}
	if len(pub.calls) != 1 {
		t.Fatalf("must still publish, got %d", len(pub.calls))
	}
}

func TestProcessUnknownChannelMarksForbidden(t *testing.T) {
	mem := newMem()
	pub := &recPub{}
	p := newProc(t, mem, pub)
	msg := &miov1.Message{
		ChannelType: "unregistered_channel", AccountId: "acc", Id: "m_unreg",
		Attachments: []*miov1.Attachment{{Url: "https://platform/x"}},
	}
	if err := p.Process(t.Context(), msg); err != nil {
		t.Fatal(err)
	}
	if msg.Attachments[0].ErrorCode != miov1.Attachment_ERROR_CODE_FORBIDDEN {
		t.Fatalf("expected FORBIDDEN, got %v", msg.Attachments[0].ErrorCode)
	}
}

func TestProcessPropagatesPublishError(t *testing.T) {
	registerStub(t, "ch_d", []byte("x"))
	mem := newMem()
	pub := &failingPub{err: errors.New("boom")}
	p := newProc(t, mem, pub)
	msg := &miov1.Message{
		ChannelType: "ch_d", AccountId: "acc", Id: "m_d",
		ReceivedAt:  timestamppb.New(time.Now().UTC()),
		Attachments: []*miov1.Attachment{{Url: "https://platform/x"}},
	}
	err := p.Process(t.Context(), msg)
	if err == nil {
		t.Fatal("expected publish err to bubble up so worker Naks")
	}
}

type failingPub struct{ err error }

func (f *failingPub) Publish(_ context.Context, _ *miov1.Message) error { return f.err }

// silence unused import warnings if test bodies omit them.
var _ = bytes.NewReader
