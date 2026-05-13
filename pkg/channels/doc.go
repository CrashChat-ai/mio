// Package channels defines the contract every messaging-channel adapter must
// satisfy to plug into the mio gateway: outbound Send/Edit, inbound webhook
// verification + normalization, credential lifecycle, capability advertising,
// and the durable-state hooks (Store) the inbound path requires.
//
// Concrete adapters live under channels/<name>/ and register themselves in
// init() via RegisterAdapter. The gateway-internal sender package consumes
// the registered adapters to build its Dispatcher; the admin server consumes
// them to drive install/refresh flows. Adapters depend only on this package
// + proto/gen/go/mio/v1 — never on services/gateway/internal/*.
package channels
