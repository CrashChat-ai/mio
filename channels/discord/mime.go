package discord

import (
	"strings"

	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

func attachmentKindFromMime(mime string) miov1.Attachment_Kind {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return miov1.Attachment_KIND_IMAGE
	case strings.HasPrefix(mime, "audio/"):
		return miov1.Attachment_KIND_AUDIO
	case strings.HasPrefix(mime, "video/"):
		return miov1.Attachment_KIND_VIDEO
	case mime == "":
		return miov1.Attachment_KIND_UNSPECIFIED
	default:
		return miov1.Attachment_KIND_FILE
	}
}
