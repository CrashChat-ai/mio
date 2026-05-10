// Package partition builds GCS partition paths per the MIO archive contract.
//
// Partition schema (locked in system-architecture.md §8 + P6 plan):
//
//	gs://mio-messages/channel_type=<slug>/date=YYYY-MM-DD/<file>
//
// Key invariants:
//   - Key name is "channel_type" (underscore), never "channel".
//   - Slug value is the proto/channels.yaml registry value (e.g. "zoho_cliq"),
//     never the URL-slug form ("zoho-cliq").
//   - Date is derived from msg.received_at in UTC, not wall-clock at write time.
//   - This function is the ONLY place that builds the directory path.
package partition

import (
	"fmt"
	"time"
)

// Path returns the GCS directory path (no trailing slash) for an
// (channelType, ts) pair.
//
// Example:
//
//	Path("zoho_cliq", t) → "channel_type=zoho_cliq/date=2026-05-08"
//
// The caller appends "/<filename>.ndjson" to form the full object path.
func Path(channelType string, ts time.Time) string {
	utc := ts.UTC()
	return fmt.Sprintf("channel_type=%s/date=%s", channelType, utc.Format("2006-01-02"))
}
