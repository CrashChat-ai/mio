# ADR 0001: Web backend placement

- Status: Accepted (2026-06-14)

## Context

WHERE does the operator-console web backend live? Today three HTTP surfaces exist:

- **Data plane** — `services/gateway` (`cmd/gateway`): webhooks→NATS→sender pool, channel-blind, internet-facing, kept pure I/O by `gateway-dispatch-lint`.
- **Control plane** — `services/gateway/internal/admin` (`cmd/admin`): connect-go RPCs; `auth.go` itself states the loopback+CIDR check "is not real authn/authz" — internal-only v1. It already terminates HTTP and already hosts `/oauth/callback`.
- **BFF** — `ui/web` (`cmd/mio-web`): Google OAuth + cookie sessions + RBAC + audit + hand-written REST handlers that translate HTTP/JSON ↔ AdminService connect-rpc, plus a hand-maintained `api-contracts/openapi.yaml` and generated TS client (post-#54 decouple).

**Dual-surface smell:** the same admin ops are restated in ~5 layers — `proto/mio/admin/v1/admin.proto` → `adminclient.go` (24-method interface) → `rest/dto.go` (8 JSON structs) → `rest/json.go` (8 mappers) → `openapi.yaml` (~827 lines) → TS client. Every new RPC means six hand edits and risks an unprotected/invisible endpoint.

**Install-OAuth ≠ operator-login-OAuth.** The callback on `cmd/admin` (`oauth_callback.go`) is machine-driven channel-credential capture: no cookie, no session, no CSRF token, validated against a 60s server-side stash under the CIDR allowlist. The BFF's login is a long-lived authenticated human session (PKCE, HMAC state, HttpOnly session cookie, RBAC). The security argument hinges on this: "admin already does OAuth" does NOT license putting human sessions on the credential-bearing control plane.

## Decision

**Adopt Option A's boundary, then execute Option C: keep the operator console as a separate BFF process with all human auth/session/RBAC/audit OFF the control plane, and kill the hand-written drift by generating the REST/DTO/OpenAPI layer from `admin.proto`.** Never collapse human auth onto `cmd/admin` (B) and never fold the web backend into `cmd/gateway` (D).

## Consequences

**Positive**
- Human-auth blast surface (cookies/CSRF/CORS/sessions) stays physically separate from both the internet-facing data plane and the credential-minting control plane (age cipher, RotateCredential, CompleteInstall).
- Control plane keeps its honest "loopback is a sanity check, not a boundary" posture; no new browser ingress / CORS on the secret-holding process.
- C removes the drift hazard at the contract: one `.proto` sources both surfaces, so they cannot diverge; `buf.gen.yaml` is already v2 with the remote connectrpc plugin, making OpenAPI codegen a config-only addition.
- Console stays swappable; TUI and channel-pulse consumers are unaffected.

**Negative / accepted**
- Two deployables and a BFF→admin hop remain (vs B's one). The hop is plaintext `http://127.0.0.1:9090` trusting co-location — acceptable while truly co-located; **must add mTLS + caller identity before ever splitting pods/nodes.**
- C adds a codegen build-step; streaming RPCs (`TailMessages`, stream-health/SSE) map poorly to REST and stay hand-written — a small residual drift surface. Handler-level auth/audit boilerplate is not eliminated (~70% churn reduction, not 100%).
- Per-RPC `google.api.http` annotations may be needed so generated routes match existing URL shapes; must stay additive to avoid WIRE_JSON breakage for TUI/channel-pulse.

**Explicitly NOT doing**
- **NOT B:** co-locating browser sessions + CORS with decrypted credentials makes one CORS/CSRF/XSS bug a credential-plane compromise, inverts admin's documented intent, and forces re-securing the BFF→admin hop — net-new surface, not removed surface.
- **NOT D (and why):** folding the console into `cmd/gateway` welds CSRF/cookie/session deps to the sole deliberately internet-facing webhook process, scales auth deps with webhook HPA, and violates the channel-blind purity invariant guarded by `gateway-dispatch-lint`. Largest blast radius on every axis.

## Options considered

| Option | Sec | Ops | Maint | Product | steel-B | steel-keep | Verdict |
|---|---|---|---|---|---|---|---|
| A status quo | 5 | 4 | 1 | 4 | 3 | 4 | Safe boundary, but drift is real debt — interim/fallback |
| B connect-web collapse | 2 | 2 | 4 | 2 | 4 | 3 | Best DRY, but fuses human auth onto credential plane — rejected |
| C shared-contract BFF | 5 | 4 | 5 | 5 | 3 | 5 | **Chosen** — A's security + kills drift |
| D merge into gateway | 1 | 1 | 1 | 1 | 1 | 1 | Baseline loser — rejected |

Aggregate (sum): A=21, B=17, **C=27**, D=6.

## Follow-ups

1. ~~Add `protoc-gen-openapiv2` (and/or grpc-gateway / connect REST) to `buf.gen.yaml`; make `api-contracts/openapi.yaml` and the TS client generated output, not source.~~ **Resolved as drift-DETECTION — see Addendum 2026-06-14.**
2. Add per-RPC `google.api.http` annotations to `admin.proto` (additive only). ~~Collapse `adminclient.go` + `rest/dto.go` + `rest/json.go` into generated code.~~ **DTO/mapper collapse deferred indefinitely — see Addendum.**
3. Leave `TailMessages`/stream-health hand-written; document the residual seam.
4. **Harden the BFF→admin hop (mTLS + caller identity) as a prerequisite for any deployment that splits the two processes.** Until then, NetworkPolicy must keep `cmd/admin` unreachable except from the BFF.
5. Close the missing `mio-admin` Helm chart gap (admin is only modeled in docker-compose today) so A/C can deploy on k8s.
6. **Revisit B only if** the BFF→admin hop is hardened to a true authenticated boundary AND the org accepts browser ingress on the control plane — neither holds today.

## Addendum 2026-06-14 — Option C shipped as drift-DETECTION, not full generation

**Finding:** `buf.build/community/sudorandom-connect-openapi` successfully emits a proto-derived OpenAPI spec via `features=google.api.http`, but the output cannot reproduce the hand-written `openapi.yaml`/`schema.d.ts` without hand-encoding divergences into the assembler. The irreducible deltas are:

- `ChannelType` flattening: `json.go` hoists 7 nested `ChannelTypeInfo.capabilities.*` fields to top level; no plugin opt flattens them.
- camelCase fields vs snake_case query params are governed by a single `with-proto-names` flag — cannot satisfy both surfaces simultaneously.
- Fully-qualified schema names (`mio.admin.v1.Tenant` vs `Tenant`) and envelope wrappers (`{credential:…}`, `{webhookInfo:…}`) differ from the hand spec's projection.
- BFF-only routes (`/api/admin/audit`, `/healthz`, `/api/session`, `/auth/*`) are outside proto and must remain hand-authored.

**Resolution:** The hand-written `openapi.yaml` is an **intentional, friendlier REST-envelope projection** of the proto messages. Replacing it would require a breaking schema reshape and 16 frontend call-site updates — net negative. Full generation rejected.

**What was shipped instead (drift-DETECTION):**

- `buf.gen.yaml` now emits a proto-derived reference spec to `ui/web/api-contracts/gen/mio/admin/v1/admin.openapi.yaml` (committed for diff-visibility; regenerated by `make proto`).
- **Guard A (RPC↔REST route parity):** `TestAdminRouteParityWithProto` in `ui/web/internal/rest/parity_test.go` asserts set-equality between proto-annotated paths and `router.go` adminMux registrations (path params normalized). A new RPC with `google.api.http` but no router handler → FAIL. A router admin route with no proto annotation (and not in the explicit BFF-only set) → FAIL.
- **Guard B (auth/audit coverage):** `TestAdminHandlerAuthCoverage` statically inspects handler source files and asserts every admin mutation handler calls `requireRole` AND `recordAudit`/`recordAuditOrError`.
- `make ui-web-contract-parity` runs both guards; wired into `test-ui-web` CI job.
- The hand `openapi.yaml`, `schema.d.ts`, `dto.go`, `json.go`, and 16 frontend call sites are **unchanged** — zero consumer churn.

**The original ADR claim** ("C removes the drift hazard at the contract: one `.proto` sources both surfaces, so they cannot diverge") is partially correct: the proto now sources the route list (Guard A enforces this) and auth coverage (Guard B). Shape parity between proto messages and the REST envelopes is NOT enforced — the hand projection is a deliberate design choice that the guards document explicitly.
