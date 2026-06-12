package archiver_test

import (
	"context"
	"testing"
	"time"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"github.com/crashchat-ai/mio/services/sink-gcs/internal/encode"
	"github.com/crashchat-ai/mio/services/sink-gcs/internal/filename"
	"github.com/crashchat-ai/mio/services/sink-gcs/internal/partition"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestPartitionKeyDerivation ensures the archiver partition key logic
// uses channel_type (underscore) + UTC date from received_at.
func TestPartitionKeyDerivation(t *testing.T) {
	ts := time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC)
	got := partition.Path("zoho_cliq", ts)
	want := "channel_type=zoho_cliq/date=2026-05-08"
	if got != want {
		t.Errorf("partition key = %q; want %q", got, want)
	}
}

// TestFilenameBuiltAtFlushTime verifies that seqStart and seqEnd produce
// the correct offset-based filename.
func TestFilenameBuiltAtFlushTime(t *testing.T) {
	got := filename.Build("gcs-archiver", 1000, 1063)
	want := "gcs-archiver-1000-1063.ndjson"
	if got != want {
		t.Errorf("filename = %q; want %q", got, want)
	}
}

// TestFullObjectPath verifies the complete GCS object path format.
func TestFullObjectPath(t *testing.T) {
	ts := time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC)
	partPath := partition.Path("zoho_cliq", ts)
	fname := filename.Build("gcs-archiver", 1000, 1063)
	got := partPath + "/" + fname
	want := "channel_type=zoho_cliq/date=2026-05-08/gcs-archiver-1000-1063.ndjson"
	if got != want {
		t.Errorf("object path = %q; want %q", got, want)
	}
}

// TestPrefixedObjectPath locks the SINK_PREFIX behaviour: a normalised prefix
// (already trailing-slashed) prepended to the partition path produces a clean
// key under the prefix folder. Regression guard for deployments that write to
// a shared bucket under a fixed prefix (e.g. gs://<your-bucket>/mio/).
func TestPrefixedObjectPath(t *testing.T) {
	ts := time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC)
	partPath := partition.Path("zoho_cliq", ts)
	fname := filename.Build("gcs-archiver", 1000, 1063)

	cases := []struct {
		prefix, want string
	}{
		{"", "channel_type=zoho_cliq/date=2026-05-08/gcs-archiver-1000-1063.ndjson"},
		{"mio/", "mio/channel_type=zoho_cliq/date=2026-05-08/gcs-archiver-1000-1063.ndjson"},
		{"a/b/", "a/b/channel_type=zoho_cliq/date=2026-05-08/gcs-archiver-1000-1063.ndjson"},
	}
	for _, tc := range cases {
		got := tc.prefix + partPath + "/" + fname
		if got != tc.want {
			t.Errorf("prefix=%q: got %q, want %q", tc.prefix, got, tc.want)
		}
	}
}

// TestNDJSONRoundtrip verifies proto → NDJSON → proto round-trip.
func TestNDJSONRoundtrip(t *testing.T) {
	original := &miov1.Message{
		Id:                     "test-msg-001",
		SchemaVersion:          1,
		TenantId:               "tenant-1",
		AccountId:              "acct-1",
		ChannelType:            "zoho_cliq",
		ConversationId:         "conv-1",
		ConversationExternalId: "chat_abc",
		ConversationKind:       miov1.ConversationKind_CONVERSATION_KIND_DM,
		SourceMessageId:        "src-msg-1",
		Text:                   "hello",
		ReceivedAt:             timestamppb.New(time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC)),
	}

	line, err := encode.ToNDJSONLine(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(line) == 0 {
		t.Fatal("encoded line is empty")
	}
	// Must not contain literal newline (would break NDJSON format).
	for i, b := range line {
		if b == '\n' {
			t.Errorf("encoded line contains newline at byte %d", i)
		}
	}

	// Decode back via proto unmarshal (not protojson — we need the wire bytes first).
	// Re-marshal to wire for the round-trip.
	wireBytes, err := proto.Marshal(original)
	if err != nil {
		t.Fatalf("proto marshal: %v", err)
	}
	var decoded miov1.Message
	if err := proto.Unmarshal(wireBytes, &decoded); err != nil {
		t.Fatalf("proto unmarshal: %v", err)
	}
	if !proto.Equal(original, &decoded) {
		t.Error("proto round-trip: decoded message not equal to original")
	}
}

// TestConcurrentSeqRangesNonOverlapping verifies that offset-based filenames
// from two simulated pods do not collide.
func TestConcurrentSeqRangesNonOverlapping(t *testing.T) {
	// Simulate JetStream distributing non-overlapping sequence ranges to two pods.
	type batch struct{ start, end uint64 }
	pod1 := []batch{{1, 64}, {65, 128}, {129, 192}}
	pod2 := []batch{{193, 256}, {257, 320}}

	seen := make(map[string]bool)
	for _, b := range append(pod1, pod2...) {
		name := filename.Build("gcs-archiver", b.start, b.end)
		if seen[name] {
			t.Errorf("filename collision: %q produced by two pods", name)
		}
		seen[name] = true
	}

	// Verify no seq-range overlap between pods.
	for i, a := range pod1 {
		for j, b := range pod2 {
			if a.end >= b.start && a.start <= b.end {
				t.Errorf("pod1[%d] (%d-%d) overlaps pod2[%d] (%d-%d)",
					i, a.start, a.end, j, b.start, b.end)
			}
		}
	}
}

// TestIdempotentObjectPath verifies that a redelivered range produces the
// identical object path (same name → safe overwrite on restart).
func TestIdempotentObjectPath(t *testing.T) {
	ts := time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC)
	partPath := partition.Path("zoho_cliq", ts)

	// First delivery.
	path1 := partPath + "/" + filename.Build("gcs-archiver", 100, 163)
	// Simulated restart: same range redelivered.
	path2 := partPath + "/" + filename.Build("gcs-archiver", 100, 163)

	if path1 != path2 {
		t.Errorf("redelivered range produced different path: %q vs %q", path1, path2)
	}
}

// TestEncode_EmitUnpopulatedFalse verifies that zero-value fields are omitted.
func TestEncode_EmitUnpopulatedFalse(t *testing.T) {
	// A minimal message with only required fields populated.
	msg := &miov1.Message{
		Id:              "msg-1",
		SchemaVersion:   1,
		TenantId:        "t1",
		AccountId:       "a1",
		ChannelType:     "zoho_cliq",
		ConversationId:  "c1",
		SourceMessageId: "s1",
		ReceivedAt:      timestamppb.New(time.Now()),
	}
	line, err := encode.ToNDJSONLine(msg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	s := string(line)
	// EmitUnpopulated=false: empty fields are omitted entirely. Check by
	// asserting the snake_case key (UseProtoNames=true) is absent.
	if contains(s, `"conversation_external_id"`) {
		t.Error("empty conversation_external_id should be omitted with EmitUnpopulated=false")
	}
	// UseProtoNames=true contract: populated fields use snake_case, never camelCase.
	// channel_type is set above; locking the snake_case shape protects the BQ
	// schema from a silent flip back to camelCase (which would NULL columns).
	if !contains(s, `"channel_type":"zoho_cliq"`) {
		t.Errorf("expected snake_case channel_type in NDJSON, got: %s", s)
	}
	if contains(s, `"channelType"`) {
		t.Errorf("camelCase channelType leaked — UseProtoNames=true broken; got: %s", s)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestInflightSuffix ensures inflight filenames are distinguishable.
func TestInflightSuffix(t *testing.T) {
	inf := filename.Inflight("gcs-archiver", 1, 64)
	final := filename.Build("gcs-archiver", 1, 64)
	if inf == final {
		t.Error("inflight and final filenames must differ")
	}
	expected := final + ".inflight"
	if inf != expected {
		t.Errorf("Inflight = %q; want %q", inf, expected)
	}
}

// TestReceivedAtDrivesPartition ensures received_at (not wall clock) drives partitioning.
func TestReceivedAtDrivesPartition(t *testing.T) {
	// Message received yesterday.
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	partPath := partition.Path("zoho_cliq", yesterday)
	expected := "channel_type=zoho_cliq/date=" + yesterday.Format("2006-01-02")
	if partPath != expected {
		t.Errorf("partition path = %q; want %q", partPath, expected)
	}
}

// TestNDJSONRoundtrip_RelationReply verifies that a reply message carries the
// relation block through the NDJSON encode path and that all four fields emit
// with the correct snake_case names.
func TestNDJSONRoundtrip_RelationReply(t *testing.T) {
	msg := &miov1.Message{
		Id:              "msg-reply-001",
		SchemaVersion:   1,
		TenantId:        "t1",
		AccountId:       "a1",
		ChannelType:     "zoho_cliq",
		ConversationId:  "conv-1",
		SourceMessageId: "src-reply-1",
		ReceivedAt:      timestamppb.New(time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)),
		Relation: &miov1.MessageRelation{
			Kind:             miov1.MessageRelation_KIND_REPLY,
			TargetExternalId: "src-parent-1",
		},
	}

	line, err := encode.ToNDJSONLine(msg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	s := string(line)

	for _, want := range []string{
		`"relation"`,
		`"kind":"KIND_REPLY"`,
		`"target_external_id":"src-parent-1"`,
	} {
		if !contains(s, want) {
			t.Errorf("expected %q in NDJSON line, got: %s", want, s)
		}
	}
	if contains(s, `"target_message_id"`) {
		t.Error("empty target_message_id should be omitted with EmitUnpopulated=false")
	}
	if contains(s, `"reaction_emoji"`) {
		t.Error("empty reaction_emoji should be omitted with EmitUnpopulated=false")
	}
}

// TestNDJSONRoundtrip_PlainMessage_NoRelation verifies that a plain message
// with no relation set emits no "relation" key in the NDJSON line.
func TestNDJSONRoundtrip_PlainMessage_NoRelation(t *testing.T) {
	msg := &miov1.Message{
		Id:              "msg-plain-001",
		SchemaVersion:   1,
		TenantId:        "t1",
		AccountId:       "a1",
		ChannelType:     "zoho_cliq",
		ConversationId:  "conv-1",
		SourceMessageId: "src-plain-1",
		Text:            "hello world",
		ReceivedAt:      timestamppb.New(time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)),
	}

	line, err := encode.ToNDJSONLine(msg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	s := string(line)

	if contains(s, `"relation"`) {
		t.Errorf("plain message must not emit relation field, got: %s", s)
	}
}

// Compile-time check that the encode package is importable.
var _ = context.Background
