package discord

import "strings"

// Discord message ids are globally-unique snowflakes, but every REST call that
// targets a message (edit, react, reply reference) needs the channel id too —
// so source_message_id carries both, mirroring slack's channel-qualified shape.
// Snowflakes are numeric ([0-9]+), so ':' round-trips losslessly.
const compositeSep = ":"

// composite joins a Discord channel id and message id into the source_message_id.
// Returns "" when either part is empty so callers never persist a half-formed id.
func composite(channel, msgID string) string {
	if channel == "" || msgID == "" {
		return ""
	}
	return channel + compositeSep + msgID
}

// splitComposite reverses composite. ok is false for ids missing the separator
// or with an empty half. Splits on the FIRST separator only.
func splitComposite(id string) (channel, msgID string, ok bool) {
	channel, msgID, found := strings.Cut(id, compositeSep)
	if !found || channel == "" || msgID == "" {
		return "", "", false
	}
	return channel, msgID, true
}
