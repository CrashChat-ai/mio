package filename_test

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/crashchat-ai/mio/services/sink-gcs/internal/filename"
)

// filenameRE validates the offset-based naming scheme.
// Pattern: <consumer-id>-<seq-start>-<seq-end>.ndjson
var filenameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]+-\d+-\d+\.ndjson$`)

func TestBuild_GoldenFixtures(t *testing.T) {
	f, err := os.Open("testdata/golden_filenames.txt")
	if err != nil {
		t.Fatalf("open golden fixtures: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 4 {
			t.Errorf("line %d: expected 4 fields, got %d: %q", line, len(fields), text)
			continue
		}
		consumerID := fields[0]
		seqStart, err1 := strconv.ParseUint(fields[1], 10, 64)
		seqEnd, err2 := strconv.ParseUint(fields[2], 10, 64)
		want := fields[3]
		if err1 != nil || err2 != nil {
			t.Errorf("line %d: parse seq numbers: %v %v", line, err1, err2)
			continue
		}

		got := filename.Build(consumerID, seqStart, seqEnd)
		if got != want {
			t.Errorf("line %d: Build(%q, %d, %d) = %q; want %q", line, consumerID, seqStart, seqEnd, got, want)
		}
		if !filenameRE.MatchString(got) {
			t.Errorf("line %d: %q does not match offset-based filename pattern", line, got)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan golden fixtures: %v", err)
	}
}

func TestBuild_PatternConformance(t *testing.T) {
	cases := []struct {
		consumerID string
		seqStart   uint64
		seqEnd     uint64
	}{
		{"gcs-archiver", 1, 64},
		{"gcs-archiver", 1000, 1063},
		{"my-consumer", 500, 563},
	}
	for _, tc := range cases {
		got := filename.Build(tc.consumerID, tc.seqStart, tc.seqEnd)
		if !filenameRE.MatchString(got) {
			t.Errorf("Build(%q, %d, %d) = %q does not match pattern %s",
				tc.consumerID, tc.seqStart, tc.seqEnd, got, filenameRE)
		}
	}
}

func TestInflight_HasSuffix(t *testing.T) {
	got := filename.Inflight("gcs-archiver", 1000, 1063)
	want := "gcs-archiver-1000-1063.ndjson.inflight"
	if got != want {
		t.Errorf("Inflight = %q; want %q", got, want)
	}
}

func TestBuild_SeqRangesNonOverlapping(t *testing.T) {
	// Simulate two pods consuming non-overlapping JetStream sequence ranges.
	// Verify the output filenames are disjoint (no two files share a seq range).
	type batch struct {
		seqStart uint64
		seqEnd   uint64
	}
	pod1 := []batch{{1, 64}, {65, 128}, {129, 192}}
	pod2 := []batch{{193, 256}, {257, 320}}

	seen := make(map[string]bool)
	all := append(pod1, pod2...)
	for _, b := range all {
		name := filename.Build("gcs-archiver", b.seqStart, b.seqEnd)
		if seen[name] {
			t.Errorf("filename collision: %q appears twice", name)
		}
		seen[name] = true
	}

	// Cross-pod: verify no seq-range overlap.
	for i, a := range pod1 {
		for j, b := range pod2 {
			if a.seqEnd >= b.seqStart && a.seqStart <= b.seqEnd {
				t.Errorf("pod1[%d] (%d-%d) overlaps pod2[%d] (%d-%d)",
					i, a.seqStart, a.seqEnd, j, b.seqStart, b.seqEnd)
			}
		}
	}
	_ = fmt.Sprintf // suppress import if unused
}
