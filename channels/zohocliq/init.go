package zohocliq

import (
	"github.com/crashchat-ai/mio/pkg/channels"
)

// init self-registers the Cliq adapter with the channels registry.
// main.go triggers this via: import _ "github.com/crashchat-ai/mio/channels/zohocliq"
//
// P9 litmus: adding a new channel = new package with its own init().
// dispatch.go has zero channel-specific branches (grep test in CI confirms this).
func init() {
	channels.RegisterAdapter(NewAdapter())
}
