"""MIO Echo Consumer — POC stand-in for the AI service (MIU).

Consumes from MESSAGES_INBOUND via sdk-py async iterator, produces an
echo SendCommand to MESSAGES_OUTBOUND, and acks each message.

Design invariants (locked — do not change in this file):
  - consume_inbound() async iterator is the ONLY consume surface used.
    The SDK owns the 5s pull-fetch loop; no manual fetch() here.
  - Signal handlers registered BEFORE the iterator opens (SIGTERM ≤6s drain).
  - No schema Verify on consume path (publish-only asymmetry per P2 contract).
  - All four-tier IDs (tenant_id, account_id, channel_type, conversation_id)
    preserved from inbound Message to outbound SendCommand unchanged.
  - Idempotency key on outbound: Nats-Msg-Id = "out:<cmd.id>" set by SDK.
    Do NOT set it manually here.
  - Metric labels: {channel_type, direction, outcome} only (no account_id etc).
"""

from __future__ import annotations

import asyncio
import logging
import os
import signal
import sys
import types

# ---------------------------------------------------------------------------
# Proto type loading — resolves namespace conflict between sdk-py and proto gen.
#
# proto/gen/py/mio/v1/ uses the "mio" top-level package name, same as sdk-py.
# Strategy:
#   1. Import sdk-py's 'mio' first so it owns sys.modules['mio'].
#   2. Graft 'mio.v1' as a sub-package that points to proto/gen/py/mio/v1/
#      by inserting it into sys.modules and patching mio.__path__ so Python
#      can resolve 'from mio.v1 import ...' inside the pb2 files.
#   3. Add proto/gen/py/mio/v1 to mio.__path__ ONLY — not proto/gen/py itself,
#      which would re-expose the shadowing 'mio' directory.
# ---------------------------------------------------------------------------

import mio as _mio_sdk  # sdk-py — must be first.

_REPO_ROOT = os.path.abspath(
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..")
)
_PROTO_V1_DIR = os.path.join(_REPO_ROOT, "proto", "gen", "py", "mio", "v1")


def _ns(name: str, path: list[str]) -> types.ModuleType:
    """Return (or create) a namespace package registered in sys.modules."""
    if name in sys.modules:
        return sys.modules[name]
    mod = types.ModuleType(name)
    mod.__path__ = path  # type: ignore[assignment]
    mod.__package__ = name
    sys.modules[name] = mod
    return mod


# Graft the generated proto hierarchy into the sdk-py 'mio' package so
# both 'from mio.v1.xxx_pb2 import ...' (echo consumer)
# and 'from mio.proto.gen.python.mio.v1.xxx_pb2 import ...' (sdk-py client.py)
# resolve to the same generated files.
#
# Hierarchy built:
#   mio.v1          → proto/gen/py/mio/v1/
#   mio.proto       → (namespace)
#   mio.proto.gen   → (namespace)
#   mio.proto.gen.python      → (namespace)
#   mio.proto.gen.python.mio  → (namespace pointing at proto/gen/py/mio/)
#   mio.proto.gen.python.mio.v1 → proto/gen/py/mio/v1/

_proto_mio_dir = os.path.join(_REPO_ROOT, "proto", "gen", "py", "mio")

_v1 = _ns("mio.v1", [_PROTO_V1_DIR])
setattr(_mio_sdk, "v1", _v1)

_proto = _ns("mio.proto", [])
setattr(_mio_sdk, "proto", _proto)

_gen = _ns("mio.proto.gen", [])
setattr(_proto, "gen", _gen)

_python = _ns("mio.proto.gen.python", [])
setattr(_gen, "python", _python)

_pgmio = _ns("mio.proto.gen.python.mio", [_proto_mio_dir])
setattr(_python, "mio", _pgmio)

_pgv1 = _ns("mio.proto.gen.python.mio.v1", [_PROTO_V1_DIR])
setattr(_pgmio, "v1", _pgv1)

# Now standard imports resolve via the grafted paths.
from mio.v1.message_pb2 import Message  # noqa: E402
from mio.v1.send_command_pb2 import SendCommand  # noqa: E402

import mio  # re-bind the name for use below  # noqa: E402
from ulid import ULID  # noqa: E402

# ---------------------------------------------------------------------------
# Config — all from environment
# ---------------------------------------------------------------------------

NATS_URL: str = os.environ.get("NATS_URL", "nats://localhost:4222")
# Consume from the enriched stream so attachment URLs are stable storage
# URLs (signed) instead of short-lived platform URLs. The attachment-
# downloader sidecar republishes every inbound message here, with
# ``Attachment.url`` rewritten and ``storage_key`` populated. See
# plans/260509-2328-attachment-persistence/ and docs/deployment.md.
INBOUND_SUBJECT: str = "mio.inbound_enriched.>"
DURABLE: str = "ai-consumer-enriched"  # new durable; old `ai-consumer` is left in place for rollback

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)-8s %(name)s %(message)s",
)
log = logging.getLogger("echo-consumer")


# ---------------------------------------------------------------------------
# Echo handler
# ---------------------------------------------------------------------------


async def handle(msg: Message, client: mio.Client) -> SendCommand:
    """Build and publish an echo SendCommand for every inbound Message.

    No schema validation here — that is the SDK publish path's responsibility
    (mio.Client.publish_outbound calls verify_command before writing to NATS).
    The consume path intentionally passes v2+ messages through untouched
    (P2 publish-only Verify asymmetry).
    """
    # Preserve thread context: if the inbound is a thread reply, keep the root;
    # for fresh DMs/channel messages without a thread root, use the inbound
    # source_message_id to root a new virtual thread in the reply.
    thread_root = msg.thread_root_message_id or msg.source_message_id

    cmd = SendCommand(
        id=str(ULID()),  # fresh ULID — idempotency addr: out:<id>
        schema_version=1,
        # Four-tier scope — preserved verbatim from inbound.
        tenant_id=msg.tenant_id,
        account_id=msg.account_id,
        channel_type=msg.channel_type,
        # Destination routing.
        conversation_id=msg.conversation_id,
        conversation_external_id=msg.conversation_external_id,
        parent_conversation_id=msg.parent_conversation_id,
        thread_root_message_id=thread_root,
        # Payload.
        text=f"echo: {msg.text}",
        # Fresh send — not an edit.
        edit_of_message_id="",
        edit_of_external_id="",
    )
    # attributes is a proto map<string,string> — assign via update().
    cmd.attributes.update({"replied_to": msg.id})
    # Cliq bot adapter (gateway sender) needs the channel unique_name to
    # address channelsbyname/{name}/message. The cliq inbound handler stored
    # it in msg.attributes["cliq_channel_name"]; propagate it onto the cmd.
    chan_name = msg.attributes.get("cliq_channel_name", "")
    if chan_name:
        cmd.attributes["cliq_channel_name"] = chan_name

    # SDK sets Nats-Msg-Id = "out:<cmd.id>" automatically; do NOT set manually.
    await client.publish_outbound(cmd)

    log.info(
        "echo published channel_type=%s account_id=%s conversation_id=%s "
        "cmd_id=%s replied_to=%s",
        cmd.channel_type,
        cmd.account_id,
        cmd.conversation_id,
        cmd.id,
        msg.id,
    )
    return cmd


# ---------------------------------------------------------------------------
# Main loop
# ---------------------------------------------------------------------------


async def main() -> None:
    # 1. Register signal handlers FIRST — before any long-running await.
    #    SIGTERM → stop.set() + cancel the consumer task so the fetch unblocks immediately.
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    consumer_task: asyncio.Task | None = None

    def _on_signal() -> None:
        stop.set()
        if consumer_task and not consumer_task.done():
            consumer_task.cancel()  # unblocks fetch immediately via CancelledError

    for sig in (signal.SIGINT, signal.SIGTERM):
        loop.add_signal_handler(sig, _on_signal)

    log.info(
        "connecting url=%s durable=%s subject=%s", NATS_URL, DURABLE, INBOUND_SUBJECT
    )

    # 2. Connect using sdk-py Client (owns pull-fetch, OTel, metrics).
    client = await mio.Client.connect(
        url=NATS_URL,
        name="echo-consumer",
        max_ack_pending=1,  # global ordering at POC scale; shard by subject at graduation
        ack_wait=30.0,
    )
    try:
        log.info("subscribed durable=%s subject=%s", DURABLE, INBOUND_SUBJECT)

        # 3. Wrap the consume loop in a Task so SIGTERM can cancel it immediately.
        #    The SDK handles CancelledError in its finally block (psub.unsubscribe()).
        async def _consume_loop() -> None:
            async for delivery in client.consume_inbound(INBOUND_SUBJECT, DURABLE):
                try:
                    await handle(delivery.msg, client)
                    await delivery.ack()
                except asyncio.CancelledError:
                    await delivery.nak(delay=5)
                    raise
                except Exception:
                    log.exception(
                        "handle failed msg_id=%s — nak delay=5s",
                        getattr(delivery.msg, "id", "?"),
                    )
                    await delivery.nak(delay=5)  # redelivered up to max_deliver=5

                if stop.is_set():
                    log.info("stop signal received — exiting iterator")
                    break  # iterator aclose() releases the pull subscription cleanly

        consumer_task = asyncio.create_task(_consume_loop())
        try:
            await consumer_task
        except asyncio.CancelledError:
            log.info("consumer task cancelled by signal")
    finally:
        await client.aclose()

    log.info("consumer closed durable=%s", DURABLE)


if __name__ == "__main__":
    asyncio.run(main())
