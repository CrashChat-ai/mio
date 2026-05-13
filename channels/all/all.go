// Package all is a barrel package that blank-imports every channel adapter
// so a single `import _ "github.com/crashchat-ai/mio/channels/all"` activates
// all in-tree adapters via their init() registration blocks.
//
// Adding a new adapter:
//  1. Drop a Go package at channels/<name>/ with an init() that calls into
//     the gateway-side channel registry.
//  2. Append `_ "github.com/crashchat-ai/mio/channels/<name>"` here.
//  3. Done — gateway binaries import this package and pick the adapter up.
package all

import (
	_ "github.com/crashchat-ai/mio/channels/zohocliq"
)
