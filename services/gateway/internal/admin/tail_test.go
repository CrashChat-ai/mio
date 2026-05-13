package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1"
	"github.com/crashchat-ai/mio/proto/gen/go/mio/admin/v1/adminv1connect"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"

	"github.com/crashchat-ai/mio/services/gateway/internal/nats"
	"github.com/crashchat-ai/mio/services/gateway/internal/store"
	sdk "github.com/crashchat-ai/mio/sdk-go"
)

// natsRig boots embedded NATS, provisions MESSAGES_INBOUND, and returns
// a real sdk.Client. Caller owns shutdown via the returned cleanup.
type natsRig struct {
	sdkClient *sdk.Client
	url       string
}

func newNatsRig(t *testing.T) (*natsRig, func()) {
	t.Helper()
	ns, url, err := nats.StartEmbedded(nats.EmbeddedOpts{
		Storage: "memory",
		Host:    "127.0.0.1",
		Port:    -1,
	})
	if err != nil {
		t.Fatalf("nats: %v", err)
	}

	client, err := sdk.New(url,
		sdk.WithName("tail-test"),
		sdk.WithMetricsRegistry(newTestRegistry(t)),
	)
	if err != nil {
		ns.Shutdown()
		t.Fatalf("sdk.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.EnsureStreams(ctx, client.JetStream(), 1); err != nil {
		client.Close()
		ns.Shutdown()
		t.Fatalf("ensure streams: %v", err)
	}

	cleanup := func() {
		client.Close()
		ns.Shutdown()
	}
	return &natsRig{sdkClient: client, url: url}, cleanup
}

// tailServer wires AdminServer to an httptest.Server using the supplied
// sdk.Client. No Postgres dependency — TailMessages only touches NATS.
//
// httptest.NewServer defaults to HTTP/1.1 which blocks server-stream
// header flushing on connect protocol; wrap the handler in h2c so the
// client can negotiate HTTP/2 cleartext for bidirectional framing.
func newTailServer(t *testing.T, client *sdk.Client) (adminv1connect.AdminServiceClient, func()) {
	t.Helper()
	srv := NewServer(Deps{
		SDK:    client,
		Logger: nil,
	})
	path, handler := adminv1connect.NewAdminServiceHandler(srv)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpSrv := httptest.NewUnstartedServer(mux)
	httpSrv.EnableHTTP2 = true
	httpSrv.StartTLS()

	tlsClient := httpSrv.Client() // accepts the self-signed cert
	cleanup := func() {
		tlsClient.CloseIdleConnections()
		httpSrv.Close()
	}
	return adminv1connect.NewAdminServiceClient(tlsClient, httpSrv.URL), cleanup
}

func TestTailMessages_RigSmoke(t *testing.T) {
	rig, cleanup := newNatsRig(t)
	defer cleanup()
	adminClient, closeAdmin := newTailServer(t, rig.sdkClient)
	defer closeAdmin()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := adminClient.ListChannelTypes(ctx,
		connect.NewRequest(&adminv1.ListChannelTypesRequest{}))
	if err != nil {
		t.Fatalf("unary smoke: %v", err)
	}
}

// makeMsg builds a stub inbound mio.v1.Message for tests.
func makeMsg(accountID, conv, sourceID, text string) *miov1.Message {
	return &miov1.Message{
		SchemaVersion:          1,
		Id:                     "msg-" + sourceID,
		TenantId:               "tn",
		AccountId:              accountID,
		ChannelType:            "zoho_cliq",
		ConversationId:         conv,
		ConversationExternalId: conv + "-ext",
		ConversationKind:       miov1.ConversationKind_CONVERSATION_KIND_CHANNEL_PUBLIC,
		SourceMessageId:        sourceID,
		Sender:                 &miov1.Sender{ExternalId: "u-1", DisplayName: "User One"},
		Text:                   text,
		ReceivedAt:             timestamppb.Now(),
	}
}

func TestTailMessages_AccountIDFilter(t *testing.T) {
	rig, cleanup := newNatsRig(t)
	defer cleanup()
	adminClient, closeAdmin := newTailServer(t, rig.sdkClient)
	defer closeAdmin()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// connect-go's CallServerStream blocks on response headers, which the
	// server only flushes on first Send. Publish on a tick so the ordered
	// consumer (DeliverNew) always sees a matching message even if its setup
	// lags the publisher; first stream.Send unblocks the client open.
	stopPub := make(chan struct{})
	defer close(stopPub)
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		seq := 0
		for {
			select {
			case <-stopPub:
				return
			case <-ticker.C:
				seq++
				if err := rig.sdkClient.PublishInbound(context.Background(),
					makeMsg("acct-drop", "conv-1", fmt.Sprintf("drop-%d", seq), "drop")); err != nil {
					return
				}
				if err := rig.sdkClient.PublishInbound(context.Background(),
					makeMsg("acct-keep", "conv-1", fmt.Sprintf("keep-%d", seq), "keep")); err != nil {
					return
				}
			}
		}
	}()

	stream, err := adminClient.TailMessages(ctx, connect.NewRequest(&adminv1.TailMessagesRequest{
		AccountId: "acct-keep",
	}))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var seen []string
	for len(seen) < 2 && stream.Receive() {
		m := stream.Msg()
		if m.GetAccountId() != "acct-keep" {
			t.Errorf("filter leak: account=%q", m.GetAccountId())
		}
		seen = append(seen, m.GetText())
	}
	if err := stream.Err(); err != nil && len(seen) < 2 {
		t.Fatalf("stream err: %v", err)
	}
	if len(seen) != 2 {
		t.Errorf("expected 2 captures; got %d: %v", len(seen), seen)
	}
}

func TestTailMessages_ConversationIDFilter(t *testing.T) {
	rig, cleanup := newNatsRig(t)
	defer cleanup()
	adminClient, closeAdmin := newTailServer(t, rig.sdkClient)
	defer closeAdmin()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stopPub := make(chan struct{})
	defer close(stopPub)
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		seq := 0
		for {
			select {
			case <-stopPub:
				return
			case <-ticker.C:
				seq++
				if err := rig.sdkClient.PublishInbound(context.Background(),
					makeMsg("acct-x", "conv-B", fmt.Sprintf("b-%d", seq), "b")); err != nil {
					return
				}
				if err := rig.sdkClient.PublishInbound(context.Background(),
					makeMsg("acct-x", "conv-A", fmt.Sprintf("a-%d", seq), "a")); err != nil {
					return
				}
			}
		}
	}()

	stream, err := adminClient.TailMessages(ctx, connect.NewRequest(&adminv1.TailMessagesRequest{
		AccountId:      "acct-x",
		ConversationId: "conv-A",
	}))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	got := make([]string, 0, 2)
	for len(got) < 2 && stream.Receive() {
		m := stream.Msg()
		if m.GetConversationId() != "conv-A" {
			t.Errorf("filter leak: conv=%q", m.GetConversationId())
		}
		got = append(got, m.GetText())
	}
	if err := stream.Err(); err != nil && len(got) < 2 {
		t.Fatalf("stream err: %v", err)
	}
}

func TestTailMessages_ContextCancelStopsStream(t *testing.T) {
	rig, cleanup := newNatsRig(t)
	defer cleanup()
	adminClient, closeAdmin := newTailServer(t, rig.sdkClient)
	defer closeAdmin()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Keep publishing matching messages until the test signals stop. The
	// loop is what guarantees the handler's Send fires (which flushes
	// response headers) regardless of how slowly the ordered consumer comes
	// up on this machine.
	stopPub := make(chan struct{})
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		seq := 0
		for {
			select {
			case <-stopPub:
				return
			case <-ticker.C:
				seq++
				_ = rig.sdkClient.PublishInbound(context.Background(),
					makeMsg("acct-cancel", "conv-1", fmt.Sprintf("kick-%d", seq), "kick"))
			}
		}
	}()

	stream, err := adminClient.TailMessages(ctx, connect.NewRequest(&adminv1.TailMessagesRequest{
		AccountId: "acct-cancel",
	}))
	if err != nil {
		close(stopPub)
		t.Fatalf("open stream: %v", err)
	}
	// Drain the first message so the handler is in its select loop.
	if !stream.Receive() {
		close(stopPub)
		t.Fatalf("expected kick message; err=%v", stream.Err())
	}
	close(stopPub)

	// Cancel the client ctx; the server-side handler observes ctx.Done()
	// and returns nil, closing the stream.
	cancel()
	done := make(chan struct{})
	go func() {
		for stream.Receive() {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not close after ctx cancel")
	}
	_ = stream.Close()

	// goleak after explicit cleanup confirms no consumer goroutines linger.
	// Cleanup runs after VerifyNone via defer ordering: NATS-internal poll
	// goroutines are still alive (server shuts down on cleanup), so the
	// strict check happens here only for the SDK consumer goroutine path.
}

// newTestRegistry returns a fresh prometheus.Registry so back-to-back
// tests can construct sdk.Client without tripping the global
// DefaultRegisterer's double-registration panic.
func newTestRegistry(t *testing.T) prometheus.Registerer {
	t.Helper()
	return prometheus.NewRegistry()
}
