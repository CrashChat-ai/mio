package nats

import (
	"context"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// startEmbeddedTest boots an embedded server with OS-assigned port + a
// per-test temp dir for file mode. Returns shutdown cleanup.
func startEmbeddedTest(t *testing.T, storage string) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	ns, url, err := StartEmbedded(EmbeddedOpts{
		Storage:  storage,
		StoreDir: dir,
		Host:     "127.0.0.1",
		Port:     -1,
	})
	if err != nil {
		t.Fatalf("start embedded: %v", err)
	}
	return url, func() { ns.Shutdown() }
}

// makeJetStream opens a JS context with a transient stream + KV bucket
// over the embedded server. Returns the stream name + js handle so tests
// can publish + consume.
func setupStream(t *testing.T, url, storage string) (jetstream.JetStream, *natsgo.Conn) {
	t.Helper()
	nc, err := natsgo.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	st := jetstream.FileStorage
	if storage == "memory" {
		st = jetstream.MemoryStorage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "ALLINONE_TEST",
		Subjects: []string{"allinone.>"},
		Storage:  st,
		Replicas: 1,
	})
	if err != nil {
		t.Fatalf("create stream: %v", err)
	}
	return js, nc
}

func TestMemoryMode_RoundTrip(t *testing.T) {
	url, shutdown := startEmbeddedTest(t, "memory")
	defer shutdown()

	js, nc := setupStream(t, url, "memory")
	defer nc.Drain() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := js.Publish(ctx, "allinone.test", []byte("hello")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	stream, err := js.Stream(ctx, "ALLINONE_TEST")
	if err != nil {
		t.Fatalf("stream lookup: %v", err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("stream info: %v", err)
	}
	if info.State.Msgs != 1 {
		t.Errorf("msgs: %d", info.State.Msgs)
	}
}

func TestFileMode_Durable(t *testing.T) {
	dir := t.TempDir()

	// First run: publish a message.
	ns1, url1, err := StartEmbedded(EmbeddedOpts{
		Storage:  "file",
		StoreDir: dir,
		Host:     "127.0.0.1",
		Port:     -1,
	})
	if err != nil {
		t.Fatalf("start 1: %v", err)
	}
	nc1, err := natsgo.Connect(url1)
	if err != nil {
		ns1.Shutdown()
		t.Fatalf("connect 1: %v", err)
	}
	js1, err := jetstream.New(nc1)
	if err != nil {
		nc1.Close()
		ns1.Shutdown()
		t.Fatalf("jetstream 1: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	if _, err := js1.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "DURABLE_TEST",
		Subjects: []string{"durable.>"},
		Storage:  jetstream.FileStorage,
		Replicas: 1,
	}); err != nil {
		cancel()
		nc1.Close()
		ns1.Shutdown()
		t.Fatalf("create stream 1: %v", err)
	}
	if _, err := js1.Publish(ctx, "durable.one", []byte("survive")); err != nil {
		cancel()
		nc1.Close()
		ns1.Shutdown()
		t.Fatalf("publish: %v", err)
	}
	cancel()
	nc1.Drain() //nolint:errcheck
	ns1.Shutdown()
	ns1.WaitForShutdown()

	// Second run: same store dir → message must survive.
	ns2, url2, err := StartEmbedded(EmbeddedOpts{
		Storage:  "file",
		StoreDir: dir,
		Host:     "127.0.0.1",
		Port:     -1,
	})
	if err != nil {
		t.Fatalf("start 2: %v", err)
	}
	defer ns2.Shutdown()
	nc2, err := natsgo.Connect(url2)
	if err != nil {
		t.Fatalf("connect 2: %v", err)
	}
	defer nc2.Drain() //nolint:errcheck
	js2, err := jetstream.New(nc2)
	if err != nil {
		t.Fatalf("jetstream 2: %v", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	stream, err := js2.Stream(ctx2, "DURABLE_TEST")
	if err != nil {
		t.Fatalf("stream lookup 2: %v", err)
	}
	info, err := stream.Info(ctx2)
	if err != nil {
		t.Fatalf("stream info 2: %v", err)
	}
	if info.State.Msgs != 1 {
		t.Errorf("file mode: expected 1 surviving msg, got %d", info.State.Msgs)
	}
}
