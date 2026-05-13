# channels/

In-tree messaging-channel adapters. Each sub-directory is a Go package that
maps an external chat platform (Zoho Cliq today; Slack, Telegram, Discord,
Pancake, Facebook Messenger, Zalo, WhatsApp planned) onto the mio gateway's
normalised webhook + outbound dispatch interfaces.

## Convention

- One sub-package per adapter: `channels/<name>/`.
- The adapter registers itself in an `init()` block — typically by calling
  the gateway-side registry with handler, signature verifier, normaliser,
  and outbound dispatcher.
- A binary activates every in-tree adapter with a single barrel import:
  ```go
  import _ "github.com/crashchat-ai/mio/channels/all"
  ```
  `channels/all/all.go` blank-imports every adapter package.

## Adding a new channel adapter

1. Create `channels/<name>/` with an `init()` that registers the adapter.
2. Append `_ "github.com/crashchat-ai/mio/channels/<name>"` to
   `channels/all/all.go`.
3. Add the new channel to `proto/channels.yaml` (status `active` or
   `planned`) and regenerate type maps: `make proto-gen`.
4. Wire any channel-specific config + secrets into the gateway via the
   existing per-channel config blocks; do **not** add adapter-specific
   branches to `services/gateway/internal/sender/dispatch.go` (see the
   `gateway-dispatch-lint` Make target).

## Boundary rules

- Adapter packages should depend on:
  - `proto/gen/go/mio/v1` for canonical types
  - `pkg/` for genuinely shared helpers (today: empty)
  - The gateway-side registry interface (where one is exposed)
- Adapter packages should NOT depend on `services/gateway/internal/*` —
  if shared types are needed, lift them into `pkg/channels/` first.
