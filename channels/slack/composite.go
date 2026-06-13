package slack

import "strings"

// Slack ts values are unique only per-channel, so the mio (account_id,
// source_message_id) invariant requires a channel-qualified composite. Channel
// IDs never contain ':' (they are [CGD]XXXX); ts is "<seconds>.<micros>" — also
// ':'-free. SplitN on the first ':' therefore round-trips losslessly.
const compositeSep = ":"

// composite joins a Slack channel id and message ts into the source_message_id.
// Returns "" when either part is empty so callers never persist a half-formed id.
func composite(channel, ts string) string {
	if channel == "" || ts == "" {
		return ""
	}
	return channel + compositeSep + ts
}

// splitComposite reverses composite. ok is false for ids missing the separator
// or with an empty channel/ts half. Splits on the FIRST separator only.
func splitComposite(id string) (channel, ts string, ok bool) {
	channel, ts, found := strings.Cut(id, compositeSep)
	if !found || channel == "" || ts == "" {
		return "", "", false
	}
	return channel, ts, true
}
