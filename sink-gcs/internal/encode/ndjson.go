// Package encode converts mio.v1.Message proto to NDJSON lines.
//
// Format: one JSON object per line, no trailing newline on the final line.
// BQ NEWLINE_DELIMITED_JSON requires each line to be a complete JSON object.
//
// protojson defaults (per P6 plan §Record format):
//   - EmitUnpopulated: false  — skip zero values; smaller NDJSON
//   - UseEnumNumbers:  false  — emit enum string names → SQL-friendly
//   - AllowPartial:    false  — strict
package encode

import (
	"bytes"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

var marshaler = protojson.MarshalOptions{
	EmitUnpopulated: false, // skip zero/unset fields — smaller NDJSON
	UseEnumNumbers:  false, // emit enum string names (e.g. "CONVERSATION_KIND_DM") — SQL-friendly
	AllowPartial:    false, // strict — reject messages missing required fields
}

// ToNDJSONLine encodes a single Message to a JSON line (no trailing newline).
// The caller appends "\n" when writing to the buffer.
func ToNDJSONLine(msg *miov1.Message) ([]byte, error) {
	b, err := marshaler.Marshal(msg)
	if err != nil {
		return nil, err
	}
	// Remove any embedded newlines that protojson may add in multi-line mode.
	// The default marshaler produces compact JSON (no indentation), but guard anyway.
	b = bytes.ReplaceAll(b, []byte("\n"), []byte(" "))
	return b, nil
}
