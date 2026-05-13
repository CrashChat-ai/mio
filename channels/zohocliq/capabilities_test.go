package zohocliq

import (
	"testing"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
	"google.golang.org/protobuf/proto"
)

// TestCapabilities_Verbatim is a regression gate. The Cliq adapter
// advertises a hard-coded ChannelCapabilities struct; this test reconstructs
// the expectation field-by-field so that any silent drift (e.g. adding
// SupportsTyping in code without updating consumers' expectations) trips
// CI loudly. Adding NEW capability fields requires deliberately updating
// this struct.
func TestCapabilities_Verbatim(t *testing.T) {
	a := &Adapter{}
	got := a.Capabilities()

	want := &miov1.ChannelCapabilities{
		SupportsEdit:      true,
		SupportsDelete:    false,
		SupportsReactions: true,
		SupportsThreads:   true,
		SupportsTyping:    false,
		SupportsPresence:  false,
		AllowedAttachments: []miov1.Attachment_Kind{
			miov1.Attachment_KIND_IMAGE,
			miov1.Attachment_KIND_FILE,
			miov1.Attachment_KIND_AUDIO,
			miov1.Attachment_KIND_VIDEO,
			miov1.Attachment_KIND_LINK,
		},
		MaxTextBytes:        32_000,
		RateLimitPerSecond:  10,
		RateLimitScope:      "account",
		AuthKind:            "oauth2_refresh",
		EditWindowSeconds:   0,
		DeleteWindowSeconds: 0,
	}

	if !proto.Equal(got, want) {
		t.Fatalf("ChannelCapabilities drift detected.\n got:  %+v\n want: %+v", got, want)
	}
}
