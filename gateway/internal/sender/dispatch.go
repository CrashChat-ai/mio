package sender

import (
	miov1 "github.com/crashchat-ai/mio/proto/gen/go/mio/v1"
)

// Dispatcher routes outbound commands to the correct channel adapter.
// Built once in main.go after all adapter init() blocks have run.
// Zero adapter-specific branches — P9 litmus is enforced by make gateway-dispatch-lint.
type Dispatcher struct {
	byChannel map[string]Adapter
}

// New constructs a Dispatcher from the given adapters.
// Panics on duplicate ChannelType() slug (defensive: registry already panics,
// but New is called with a snapshot so double-check here).
func New(adapters []Adapter) *Dispatcher {
	m := make(map[string]Adapter, len(adapters))
	for _, a := range adapters {
		slug := a.ChannelType()
		if _, dup := m[slug]; dup {
			panic("sender: Dispatcher.New: duplicate channel_type=" + slug)
		}
		m[slug] = a
	}
	return &Dispatcher{byChannel: m}
}

// ForCommand returns the adapter for the command's channel_type.
// Returns nil if no adapter is registered for that type — callers must
// handle nil by terminating the message with reason="other".
func (d *Dispatcher) ForCommand(cmd *miov1.SendCommand) Adapter {
	return d.byChannel[cmd.GetChannelType()]
}
