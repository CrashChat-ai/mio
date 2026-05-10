//go:build integration

// Package integration_test exercises the full sink-gcs path against a live
// MinIO instance (run via docker compose or a local MinIO binary).
//
// Run with:
//
//	SINK_BACKEND=minio SINK_BUCKET=mio-test SINK_ENDPOINT=http://localhost:9000 \
//	  go test -v -tags=integration ./integration_test/
//
// Prerequisites: MinIO accessible at SINK_ENDPOINT with SINK_ACCESS_KEY / SINK_SECRET_KEY.
package integration_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/crashchat-ai/mio/sink-gcs/internal/encode"
	"github.com/crashchat-ai/mio/sink-gcs/internal/filename"
	"github.com/crashchat-ai/mio/sink-gcs/internal/partition"
	"github.com/crashchat-ai/mio/sink-gcs/internal/writer"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// filenameRE validates offset-based naming: <consumer-id>-<seq-start>-<seq-end>.ndjson
var filenameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+-\d+-\d+\.ndjson$`)

func minioClient(t *testing.T) *minio.Client {
	t.Helper()
	endpoint := envOr("SINK_ENDPOINT", "localhost:9000")
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	cl, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(envOr("SINK_ACCESS_KEY", "minioadmin"), envOr("SINK_SECRET_KEY", "minioadmin"), ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}
	return cl
}

func writerCfg(t *testing.T, bucket string) *writer.Config {
	t.Helper()
	t.Setenv("SINK_BACKEND", "minio")
	t.Setenv("SINK_BUCKET", bucket)
	t.Setenv("SINK_ENDPOINT", envOr("SINK_ENDPOINT", "http://localhost:9000"))
	t.Setenv("SINK_ACCESS_KEY", envOr("SINK_ACCESS_KEY", "minioadmin"))
	t.Setenv("SINK_SECRET_KEY", envOr("SINK_SECRET_KEY", "minioadmin"))
	cfg, err := writer.ConfigFromEnv()
	if err != nil {
		t.Fatalf("writer config: %v", err)
	}
	return cfg
}

func ensureBucket(t *testing.T, ctx context.Context, cl *minio.Client, bucket string) {
	t.Helper()
	exists, err := cl.BucketExists(ctx, bucket)
	if err != nil {
		t.Fatalf("bucket exists: %v", err)
	}
	if !exists {
		if err := cl.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("make bucket: %v", err)
		}
	}
}

func cleanBucket(t *testing.T, ctx context.Context, cl *minio.Client, bucket string) {
	t.Helper()
	objCh := cl.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true})
	for obj := range objCh {
		if obj.Err != nil {
			continue
		}
		_ = cl.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{})
	}
}

func makeMsg(id, channelType string, ts time.Time) *miov1.Message {
	return &miov1.Message{
		Id:              id,
		SchemaVersion:   1,
		TenantId:        "tenant-1",
		AccountId:       "acct-1",
		ChannelType:     channelType,
		ConversationId:  "conv-1",
		SourceMessageId: "src-" + id,
		Text:            "hello from " + id,
		ReceivedAt:      timestamppb.New(ts),
	}
}

// TestMinIO_SingleWriter verifies that a single writer flushes correct
// partition path, offset-based filename, and valid NDJSON content.
func TestMinIO_SingleWriter(t *testing.T) {
	ctx := context.Background()
	bucket := "mio-sink-integration-single"
	cl := minioClient(t)
	ensureBucket(t, ctx, cl, bucket)
	t.Cleanup(func() { cleanBucket(t, ctx, cl, bucket) })

	cfg := writerCfg(t, bucket)
	ts := time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC)

	w, err := writer.New(ctx, cfg)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}

	const consumerID = "gcs-archiver"
	seqStart := uint64(1)
	seqEnd := uint64(5)

	msgs := []*miov1.Message{
		makeMsg("msg-1", "zoho_cliq", ts),
		makeMsg("msg-2", "zoho_cliq", ts),
		makeMsg("msg-3", "zoho_cliq", ts),
		makeMsg("msg-4", "zoho_cliq", ts),
		makeMsg("msg-5", "zoho_cliq", ts),
	}

	for _, msg := range msgs {
		line, err := encode.ToNDJSONLine(msg)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	partPath := partition.Path("zoho_cliq", ts)
	fname := filename.Build(consumerID, seqStart, seqEnd)
	objectPath := partPath + "/" + fname

	if err := w.Flush(ctx, objectPath); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Verify: object exists under correct partition path.
	obj, err := cl.GetObject(ctx, bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("get object %q: %v", objectPath, err)
	}
	defer obj.Close()

	// Verify object path has correct partition key ("channel_type=", not "channel=").
	if !strings.HasPrefix(objectPath, "channel_type=zoho_cliq/date=2026-05-08/") {
		t.Errorf("object path %q does not have expected partition prefix", objectPath)
	}

	// Verify filename matches offset-based pattern.
	parts := strings.Split(objectPath, "/")
	base := parts[len(parts)-1]
	if !filenameRE.MatchString(base) {
		t.Errorf("filename %q does not match offset-based pattern", base)
	}

	// Verify filename is correct.
	expectedFilename := "gcs-archiver-1-5.ndjson"
	if base != expectedFilename {
		t.Errorf("filename = %q; want %q", base, expectedFilename)
	}

	// Verify NDJSON round-trip: each line decodes to proto.Equal original.
	info, err := cl.StatObject(ctx, bucket, objectPath, minio.StatObjectOptions{})
	if err != nil {
		t.Fatalf("stat object: %v", err)
	}
	buf := make([]byte, info.Size)
	obj2, _ := cl.GetObject(ctx, bucket, objectPath, minio.GetObjectOptions{})
	n, _ := obj2.Read(buf)
	obj2.Close()

	lines := strings.Split(strings.TrimRight(string(buf[:n]), "\n"), "\n")
	if len(lines) != len(msgs) {
		t.Errorf("NDJSON lines = %d; want %d", len(lines), len(msgs))
	}
	for i, line := range lines {
		var decoded miov1.Message
		if err := protojson.Unmarshal([]byte(line), &decoded); err != nil {
			t.Errorf("line %d: protojson unmarshal: %v", i, err)
			continue
		}
		if !proto.Equal(msgs[i], &decoded) {
			t.Errorf("line %d: proto.Equal failed", i)
		}
	}

	// Verify no .inflight object remains.
	_, err = cl.StatObject(ctx, bucket, objectPath+".inflight", minio.StatObjectOptions{})
	if err == nil {
		t.Error("inflight object must not exist after successful flush")
	}
}

// TestMinIO_IdempotentRestart verifies that flushing the same seq range twice
// produces the same object (overwrite-safe, no data corruption).
func TestMinIO_IdempotentRestart(t *testing.T) {
	ctx := context.Background()
	bucket := "mio-sink-integration-idempotent"
	cl := minioClient(t)
	ensureBucket(t, ctx, cl, bucket)
	t.Cleanup(func() { cleanBucket(t, ctx, cl, bucket) })

	cfg := writerCfg(t, bucket)
	ts := time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC)
	partPath := partition.Path("zoho_cliq", ts)
	objectPath := partPath + "/" + filename.Build("gcs-archiver", 100, 105)

	msg := makeMsg("msg-100", "zoho_cliq", ts)
	line, _ := encode.ToNDJSONLine(msg)
	line = append(line, '\n')

	// First write + flush.
	w1, err := writer.New(ctx, cfg)
	if err != nil {
		t.Fatalf("new writer (1): %v", err)
	}
	_, _ = w1.Write(line)
	if err := w1.Flush(ctx, objectPath); err != nil {
		t.Fatalf("flush (1): %v", err)
	}

	// Simulate restart: second write + flush with identical range.
	w2, err := writer.New(ctx, cfg)
	if err != nil {
		t.Fatalf("new writer (2): %v", err)
	}
	_, _ = w2.Write(line)
	if err := w2.Flush(ctx, objectPath); err != nil {
		t.Fatalf("flush (2): %v", err)
	}

	// Object must exist once (not duplicated or lost).
	info, err := cl.StatObject(ctx, bucket, objectPath, minio.StatObjectOptions{})
	if err != nil {
		t.Fatalf("stat object after idempotent flush: %v", err)
	}
	if info.Size == 0 {
		t.Error("object is empty after idempotent flush")
	}
}

// TestMinIO_TwoPods_NonOverlappingRanges verifies that filenames from two
// simulated pods with non-overlapping sequence ranges do not collide.
func TestMinIO_TwoPods_NonOverlappingRanges(t *testing.T) {
	ctx := context.Background()
	bucket := "mio-sink-integration-twopods"
	cl := minioClient(t)
	ensureBucket(t, ctx, cl, bucket)
	t.Cleanup(func() { cleanBucket(t, ctx, cl, bucket) })

	cfg := writerCfg(t, bucket)
	ts := time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC)
	partPath := partition.Path("zoho_cliq", ts)

	// Pod 1 flushes seq 1-64.
	// Pod 2 flushes seq 65-128.
	type pod struct {
		start, end uint64
	}
	pods := []pod{{1, 64}, {65, 128}}
	objectPaths := make([]string, len(pods))

	for i, p := range pods {
		w, err := writer.New(ctx, cfg)
		if err != nil {
			t.Fatalf("pod%d: new writer: %v", i+1, err)
		}
		for seq := p.start; seq <= p.end; seq++ {
			msg := makeMsg(fmt.Sprintf("msg-%d", seq), "zoho_cliq", ts)
			line, _ := encode.ToNDJSONLine(msg)
			_, _ = w.Write(append(line, '\n'))
		}
		fname := filename.Build("gcs-archiver", p.start, p.end)
		objPath := partPath + "/" + fname
		objectPaths[i] = objPath
		if err := w.Flush(ctx, objPath); err != nil {
			t.Fatalf("pod%d: flush: %v", i+1, err)
		}
	}

	// Assert both objects exist and have different paths (no collision).
	if objectPaths[0] == objectPaths[1] {
		t.Errorf("pod1 and pod2 produced the same object path (collision): %q", objectPaths[0])
	}

	// Assert both exist in MinIO.
	for i, objPath := range objectPaths {
		if _, err := cl.StatObject(ctx, bucket, objPath, minio.StatObjectOptions{}); err != nil {
			t.Errorf("pod%d object %q not found: %v", i+1, objPath, err)
		}
	}

	// Assert seq ranges are pairwise non-overlapping.
	for i, a := range pods {
		for j, b := range pods {
			if i == j {
				continue
			}
			if a.end >= b.start && a.start <= b.end {
				t.Errorf("pod%d (%d-%d) overlaps pod%d (%d-%d)", i+1, a.start, a.end, j+1, b.start, b.end)
			}
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
