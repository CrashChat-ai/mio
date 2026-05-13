package sender

import (
	"slices"
	"sync"
)

var (
	regMu      sync.Mutex
	registered []Adapter
)

// RegisterAdapter registers an adapter for a given channel type.
// Called from each channel package's init() block before main() runs.
// Panics on duplicate ChannelType() slug — fail-fast at startup.
func RegisterAdapter(a Adapter) {
	regMu.Lock()
	defer regMu.Unlock()
	for _, existing := range registered {
		if existing.ChannelType() == a.ChannelType() {
			panic("sender: duplicate adapter registration for channel_type=" + a.ChannelType())
		}
	}
	registered = append(registered, a)
}

// RegisteredAdapters returns a snapshot of all registered adapters.
// Called once from main.go after all init() blocks have run to build
// the dispatcher's lookup map.
func RegisteredAdapters() []Adapter {
	regMu.Lock()
	defer regMu.Unlock()
	return slices.Clone(registered)
}
