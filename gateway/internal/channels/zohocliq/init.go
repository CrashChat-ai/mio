package zohocliq

import (
	"github.com/crashchat-ai/mio/gateway/internal/sender"
)

// init self-registers the Cliq adapter with the sender registry.
// main.go triggers this via: import _ "github.com/crashchat-ai/mio/gateway/internal/channels/zohocliq"
//
// P9 litmus: adding a new channel = new package with its own init().
// dispatch.go has zero channel-specific branches (grep test in CI confirms this).
func init() {
	sender.RegisterAdapter(NewAdapter())
}
