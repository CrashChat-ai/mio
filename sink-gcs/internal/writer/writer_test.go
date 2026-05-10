package writer_test

import (
	"bytes"
	"context"
	"sync"
	"testing"

	"github.com/crashchat-ai/mio/sink-gcs/internal/writer"
)

// memWriter is a test double that implements Writer in memory.
// It applies the same inflight → final rename contract as production backends.
type memWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	objects map[string][]byte // objectPath → content
}

func newMemWriter() *memWriter {
	return &memWriter{objects: make(map[string][]byte)}
}

func (m *memWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.Write(p)
}

func (m *memWriter) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.Len()
}

func (m *memWriter) Flush(_ context.Context, objectPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.buf.Len() == 0 {
		return nil
	}
	inflightPath := objectPath + ".inflight"
	data := make([]byte, m.buf.Len())
	copy(data, m.buf.Bytes())
	m.objects[inflightPath] = data   // step 1: write inflight
	m.objects[objectPath] = data     // step 2: copy to final
	delete(m.objects, inflightPath)  // step 3: delete inflight
	m.buf.Reset()
	return nil
}

func (m *memWriter) Close(ctx context.Context, objectPath string) error {
	return m.Flush(ctx, objectPath)
}

func (m *memWriter) Get(objectPath string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[objectPath]
	return b, ok
}

func (m *memWriter) HasInflight(objectPath string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[objectPath+".inflight"]
	return ok
}

// Verify memWriter satisfies the Writer interface at compile time.
var _ writer.Writer = (*memWriter)(nil)

func TestMemWriter_WriteFlush(t *testing.T) {
	ctx := context.Background()
	w := newMemWriter()

	line1 := []byte(`{"id":"msg-1"}` + "\n")
	line2 := []byte(`{"id":"msg-2"}` + "\n")

	if _, err := w.Write(line1); err != nil {
		t.Fatalf("Write line1: %v", err)
	}
	if _, err := w.Write(line2); err != nil {
		t.Fatalf("Write line2: %v", err)
	}

	if w.Len() != len(line1)+len(line2) {
		t.Errorf("Len() = %d; want %d", w.Len(), len(line1)+len(line2))
	}

	objectPath := "channel_type=zoho_cliq/date=2026-05-08/gcs-archiver-1-2.ndjson"
	if err := w.Flush(ctx, objectPath); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if w.Len() != 0 {
		t.Errorf("Len() after flush = %d; want 0", w.Len())
	}
	if w.HasInflight(objectPath) {
		t.Error("inflight object must be deleted after successful flush")
	}
	got, ok := w.Get(objectPath)
	if !ok {
		t.Fatalf("final object %q not found after flush", objectPath)
	}
	want := append(line1, line2...)
	if !bytes.Equal(got, want) {
		t.Errorf("final object content mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}

func TestMemWriter_FlushEmpty(t *testing.T) {
	ctx := context.Background()
	w := newMemWriter()
	// Flush with empty buffer must be a no-op (no object created).
	objectPath := "channel_type=zoho_cliq/date=2026-05-08/gcs-archiver-1-1.ndjson"
	if err := w.Flush(ctx, objectPath); err != nil {
		t.Fatalf("Flush empty: %v", err)
	}
	if _, ok := w.Get(objectPath); ok {
		t.Error("no object should be created on empty flush")
	}
}

func TestMemWriter_IdempotentOverwrite(t *testing.T) {
	// Simulates restart: same objectPath flushed twice with identical content.
	// Second flush must overwrite without error (idempotent).
	ctx := context.Background()
	w := newMemWriter()
	data := []byte(`{"id":"msg-1"}` + "\n")
	objectPath := "channel_type=zoho_cliq/date=2026-05-08/gcs-archiver-1-1.ndjson"

	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(ctx, objectPath); err != nil {
		t.Fatalf("first flush: %v", err)
	}

	// Re-write same data (simulates redelivery after crash).
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(ctx, objectPath); err != nil {
		t.Fatalf("second flush (idempotent): %v", err)
	}

	got, ok := w.Get(objectPath)
	if !ok {
		t.Fatal("object not found after second flush")
	}
	if !bytes.Equal(got, data) {
		t.Errorf("content mismatch after idempotent flush")
	}
}

func TestConfigFromEnv_MissingBucket(t *testing.T) {
	// SINK_BUCKET unset → error.
	t.Setenv("SINK_BACKEND", "minio")
	t.Setenv("SINK_BUCKET", "")
	if _, err := writer.ConfigFromEnv(); err == nil {
		t.Error("expected error when SINK_BUCKET is empty")
	}
}

func TestConfigFromEnv_InvalidBackend(t *testing.T) {
	t.Setenv("SINK_BACKEND", "s3")
	t.Setenv("SINK_BUCKET", "mio-messages")
	if _, err := writer.ConfigFromEnv(); err == nil {
		t.Error("expected error for unknown backend 's3'")
	}
}

func TestConfigFromEnv_Defaults(t *testing.T) {
	t.Setenv("SINK_BACKEND", "")
	t.Setenv("SINK_BUCKET", "mio-messages")
	cfg, err := writer.ConfigFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Backend != writer.BackendMinIO {
		t.Errorf("default backend = %q; want %q", cfg.Backend, writer.BackendMinIO)
	}
}
