// Package filename builds offset-based GCS object names for the MIO archive sink.
//
// Naming scheme (locked in P6 plan):
//
//	<consumer-id>-<seq-start>-<seq-end>.ndjson
//
// Example: gcs-archiver-1000-1063.ndjson
//
// Why offset-based (not timestamp-based):
//   - Two pods consuming the same durable cannot be assigned overlapping sequence
//     ranges by JetStream — collision-impossible by construction.
//   - Pod restart safe: sequences come from JetStream state, not per-pod counters
//     that reset to zero.
//   - Replay-friendly: locate any record by stream sequence with one `ls`.
//
// This function is the ONLY place that builds filenames; covered by golden fixture.
package filename

import "fmt"

// Build returns the offset-based NDJSON filename for a batch.
//
//	Build("gcs-archiver", 1000, 1063) → "gcs-archiver-1000-1063.ndjson"
//
// seqStart and seqEnd are JetStream stream sequence numbers (1-based).
// seqStart must be <= seqEnd.
func Build(consumerID string, seqStart, seqEnd uint64) string {
	return fmt.Sprintf("%s-%d-%d.ndjson", consumerID, seqStart, seqEnd)
}

// Inflight returns the in-progress object name used during the write phase.
// The final file is produced by the copy-then-delete atomic rename.
//
//	Inflight("gcs-archiver", 1000, 1063) → "gcs-archiver-1000-1063.ndjson.inflight"
func Inflight(consumerID string, seqStart, seqEnd uint64) string {
	return Build(consumerID, seqStart, seqEnd) + ".inflight"
}
