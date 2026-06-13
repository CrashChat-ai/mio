package slack

import (
	"github.com/crashchat-ai/mio/pkg/channels"
)

func init() {
	channels.RegisterAdapter(NewAdapter())
}
