"""MIO Python SDK client — async-only.

nats-py has no sync API. This is a deliberate constraint: MIO's AI consumer
is already asyncio-based (LangGraph). Callers that need sync must run their
own event loop (asyncio.run). The SDK will NOT provide a sync facade — KISS.

Usage:
    client = await Client.connect("nats://localhost:4222", name="echo-consumer")
    async for delivery in client.consume_inbound("MESSAGES_INBOUND", "echo-durable"):
        msg = delivery.msg
        await delivery.ack()
    await client.aclose()
"""

from __future__ import annotations

import asyncio
import time
from typing import AsyncIterator

import nats
import nats.errors
from nats.aio.client import Client as NatsClient
from nats.js import JetStreamContext
from opentelemetry.trace import TracerProvider
from prometheus_client import CollectorRegistry

from mio.metrics import (
    Metrics,
    OUTCOME_SUCCESS,
    OUTCOME_ERROR,
    OUTCOME_DEDUP,
    OUTCOME_INVALID,
)
from mio.subjects import inbound as build_inbound, outbound as build_outbound
from mio.tracing import inject_trace, extract_trace
from mio.version import verify, verify_command


class Delivery:
    """Wraps a raw nats-py JetStream message with typed Message accessors.

    Callers must call ack(), nak(), or term() exactly once.

    Schema verification is intentionally SKIPPED on the consume path.
    Consumers must tolerate forward-compatible additions (schema_version=2, etc).
    See version.py for the publish-only asymmetry contract.
    """

    def __init__(self, msg: object, raw: object, span: object) -> None:
        self._msg = msg
        self._raw = raw  # nats.aio.msg.Msg
        self._span = span

    @property
    def msg(self) -> object:
        """Decoded proto Message."""
        return self._msg

    async def ack(self) -> None:
        """Acknowledge successful processing."""
        self._span.end()  # type: ignore[union-attr]
        await self._raw.ack()  # type: ignore[union-attr]

    async def nak(self, delay: float = 0.0) -> None:
        """Negatively acknowledge. delay in seconds before redelivery."""
        self._span.end()  # type: ignore[union-attr]
        await self._raw.nak(delay=delay)  # type: ignore[union-attr]

    async def term(self) -> None:
        """Permanently terminate — no redelivery."""
        self._span.end()  # type: ignore[union-attr]
        await self._raw.term()  # type: ignore[union-attr]


class CommandDelivery:
    """Wraps a raw nats-py JetStream message with typed SendCommand accessors.

    Same consume-side schema asymmetry as Delivery — no verify_command called.
    """

    def __init__(self, cmd: object, raw: object, span: object) -> None:
        self._cmd = cmd
        self._raw = raw
        self._span = span

    @property
    def cmd(self) -> object:
        """Decoded proto SendCommand."""
        return self._cmd

    async def ack(self) -> None:
        self._span.end()  # type: ignore[union-attr]
        await self._raw.ack()  # type: ignore[union-attr]

    async def nak(self, delay: float = 0.0) -> None:
        self._span.end()  # type: ignore[union-attr]
        await self._raw.nak(delay=delay)  # type: ignore[union-attr]

    async def term(self) -> None:
        self._span.end()  # type: ignore[union-attr]
        await self._raw.term()  # type: ignore[union-attr]


class Client:
    """Async MIO SDK client wrapping nats-py + JetStream.

    Construct via Client.connect() — never call __init__ directly.

    SDK does NOT create streams or consumers. Gateway startup (P3) is the
    authoritative provisioner. The SDK only publishes to existing streams
    and attaches to existing durable consumers.
    """

    def __init__(
        self,
        nc: NatsClient,
        js: JetStreamContext,
        metrics: Metrics,
        tracer_provider: TracerProvider | None,
        max_ack_pending: int,
        ack_wait: float,
    ) -> None:
        self._nc = nc
        self._js = js
        self._metrics = metrics
        self._tp = tracer_provider
        self._max_ack_pending = max_ack_pending
        self._ack_wait = ack_wait

    @classmethod
    async def connect(
        cls,
        url: str,
        *,
        name: str | None = None,
        creds: str | None = None,
        tracer_provider: TracerProvider | None = None,
        metrics_registry: CollectorRegistry | None = None,
        max_ack_pending: int = 1,
        ack_wait: float = 30.0,
    ) -> "Client":
        """Connect to NATS and return a ready Client.

        max_ack_pending=1 enforces per-conversation ordering at POC scale.
        Graduation: shard by subject prefix when load-test forces it.
        """
        opts: dict = {}
        if name:
            opts["name"] = name
        if creds:
            opts["user_credentials"] = creds

        nc = await nats.connect(url, **opts)
        js = nc.jetstream()

        metrics = Metrics(registry=metrics_registry)

        return cls(
            nc=nc,
            js=js,
            metrics=metrics,
            tracer_provider=tracer_provider,
            max_ack_pending=max_ack_pending,
            ack_wait=ack_wait,
        )

    async def aclose(self) -> None:
        """Drain and close the NATS connection."""
        await self._nc.drain()

    # ------------------------------------------------------------------
    # Publish helpers
    # ------------------------------------------------------------------

    async def publish_inbound(self, msg: object) -> None:
        """Validate and publish a Message to the inbound stream.

        Idempotency key: Nats-Msg-Id = "inb:<account_id>:<source_message_id>"
        Namespace by account_id to isolate tenants across channel installs.

        Raises ValueError on verification failure; re-raises nats errors.
        """
        from mio.proto.gen.python.mio.v1.message_pb2 import Message  # noqa: F401 (lazy import)

        # Step 1: publish-side Verify (schema version + required IDs).
        try:
            verify(msg)
        except ValueError:
            self._metrics.inc_publish(
                getattr(msg, "channel_type", "unknown"), "inbound", OUTCOME_INVALID
            )
            raise

        ct = msg.channel_type  # type: ignore[union-attr]
        acct = msg.account_id  # type: ignore[union-attr]
        conv = msg.conversation_id  # type: ignore[union-attr]
        src = msg.source_message_id  # type: ignore[union-attr]

        # Step 2: idempotency key.
        msg_id = f"inb:{acct}:{src}"

        # Step 3: build subject.
        subject = build_inbound(ct, acct, conv)

        # Step 4: marshal proto.
        payload = msg.SerializeToString()  # type: ignore[union-attr]

        # Step 5: build headers with Nats-Msg-Id + inject traceparent.
        headers: dict[str, str] = {"Nats-Msg-Id": msg_id}
        _ctx, span = inject_trace(headers, subject, msg_id, self._tp)

        # Step 6: publish with dedup header.
        start = time.monotonic()
        try:
            ack = await self._js.publish(subject, payload, headers=headers)
            elapsed = time.monotonic() - start

            # nats-py signals dedup via PubAck.duplicate flag.
            if getattr(ack, "duplicate", False):
                span.end()
                self._metrics.inc_publish(ct, "inbound", OUTCOME_DEDUP)
                self._metrics.observe_publish(ct, "inbound", elapsed)
                return

            span.end()
            self._metrics.inc_publish(ct, "inbound", OUTCOME_SUCCESS)
            self._metrics.observe_publish(ct, "inbound", elapsed)

        except Exception as exc:
            elapsed = time.monotonic() - start
            span.record_exception(exc)  # type: ignore[union-attr]
            span.end()
            self._metrics.inc_publish(ct, "inbound", OUTCOME_ERROR)
            self._metrics.observe_publish(ct, "inbound", elapsed)
            raise

    async def publish_outbound(self, cmd: object) -> None:
        """Validate and publish a SendCommand to the outbound stream.

        Idempotency key: Nats-Msg-Id = "out:<cmd.id>" (ULID, globally unique).
        """
        # Step 1: publish-side VerifyCommand.
        try:
            verify_command(cmd)
        except ValueError:
            self._metrics.inc_publish(
                getattr(cmd, "channel_type", "unknown"), "outbound", OUTCOME_INVALID
            )
            raise

        ct = cmd.channel_type  # type: ignore[union-attr]
        acct = cmd.account_id  # type: ignore[union-attr]
        conv = cmd.conversation_id  # type: ignore[union-attr]
        edit_id = getattr(cmd, "edit_of_message_id", "") or ""

        # Step 2: idempotency key.
        msg_id = f"out:{cmd.id}"  # type: ignore[union-attr]

        # Step 3: build subject — include message_id segment only for edit/delete.
        subject = build_outbound(ct, acct, conv, edit_id if edit_id else None)

        # Step 4: marshal proto.
        payload = cmd.SerializeToString()  # type: ignore[union-attr]

        # Step 5: headers + traceparent.
        headers: dict[str, str] = {"Nats-Msg-Id": msg_id}
        _ctx, span = inject_trace(headers, subject, msg_id, self._tp)

        # Step 6: publish.
        start = time.monotonic()
        try:
            ack = await self._js.publish(subject, payload, headers=headers)
            elapsed = time.monotonic() - start

            if getattr(ack, "duplicate", False):
                span.end()
                self._metrics.inc_publish(ct, "outbound", OUTCOME_DEDUP)
                self._metrics.observe_publish(ct, "outbound", elapsed)
                return

            span.end()
            self._metrics.inc_publish(ct, "outbound", OUTCOME_SUCCESS)
            self._metrics.observe_publish(ct, "outbound", elapsed)

        except Exception as exc:
            elapsed = time.monotonic() - start
            span.record_exception(exc)  # type: ignore[union-attr]
            span.end()
            self._metrics.inc_publish(ct, "outbound", OUTCOME_ERROR)
            self._metrics.observe_publish(ct, "outbound", elapsed)
            raise

    # ------------------------------------------------------------------
    # Consume helpers
    # ------------------------------------------------------------------

    async def consume_inbound(
        self, subject: str, durable: str
    ) -> AsyncIterator[Delivery]:
        """Pull-consume from the inbound stream.

        Caller must supply a non-empty durable name — SDK never auto-generates.
        Consumer must already exist; gateway startup (P3) provisions consumers.

        Schema verification is SKIPPED on consume (publish-only asymmetry).

        Signal handling: fetch uses an explicit 5-second timeout so
        KeyboardInterrupt / asyncio.CancelledError interrupts within ≤5s.

        Usage:
            async for delivery in client.consume_inbound("mio.inbound.>", "echo"):
                await delivery.ack()
        """
        if not durable:
            raise ValueError(
                "durable name must not be empty; caller must supply an explicit durable"
            )

        from mio.proto.gen.python.mio.v1.message_pb2 import Message

        psub = await self._js.pull_subscribe(subject, durable=durable)
        try:
            while True:
                try:
                    # Explicit 5s timeout — allows SIGINT / CancelledError within ≤5s.
                    msgs = await psub.fetch(batch=1, timeout=5.0)
                except nats.errors.TimeoutError:
                    # No messages this window — loop and try again.
                    continue
                except asyncio.CancelledError:
                    break

                for raw in msgs:
                    start = time.monotonic()
                    # Decode — skip verify (consume-side asymmetry).
                    msg = Message()
                    try:
                        msg.ParseFromString(raw.data)
                    except Exception:
                        await raw.term()
                        self._metrics.inc_consume(
                            _channel_type_from_subject(raw.subject),
                            "inbound",
                            OUTCOME_ERROR,
                        )
                        continue

                    ct = msg.channel_type or _channel_type_from_subject(raw.subject)

                    # Extract OTel context (CONSUMER span).
                    raw_headers: dict[str, str] = dict(raw.headers or {})
                    _ctx, span = extract_trace(raw_headers, raw.subject, self._tp)

                    elapsed = time.monotonic() - start
                    self._metrics.inc_consume(ct, "inbound", OUTCOME_SUCCESS)
                    self._metrics.observe_consume(ct, "inbound", elapsed)

                    yield Delivery(msg=msg, raw=raw, span=span)
        finally:
            await psub.unsubscribe()

    async def consume_outbound(
        self, subject: str, durable: str
    ) -> AsyncIterator[CommandDelivery]:
        """Pull-consume from the outbound stream.

        Same signal-handling and asymmetry rules as consume_inbound.
        Consumer must already exist (gateway startup provisions it).
        """
        if not durable:
            raise ValueError(
                "durable name must not be empty; caller must supply an explicit durable"
            )

        from mio.proto.gen.python.mio.v1.send_command_pb2 import SendCommand

        psub = await self._js.pull_subscribe(subject, durable=durable)
        try:
            while True:
                try:
                    msgs = await psub.fetch(batch=1, timeout=5.0)
                except nats.errors.TimeoutError:
                    continue
                except asyncio.CancelledError:
                    break

                for raw in msgs:
                    start = time.monotonic()
                    cmd = SendCommand()
                    try:
                        cmd.ParseFromString(raw.data)
                    except Exception:
                        await raw.term()
                        self._metrics.inc_consume(
                            _channel_type_from_subject(raw.subject),
                            "outbound",
                            OUTCOME_ERROR,
                        )
                        continue

                    ct = cmd.channel_type or _channel_type_from_subject(raw.subject)

                    raw_headers: dict[str, str] = dict(raw.headers or {})
                    _ctx, span = extract_trace(raw_headers, raw.subject, self._tp)

                    elapsed = time.monotonic() - start
                    self._metrics.inc_consume(ct, "outbound", OUTCOME_SUCCESS)
                    self._metrics.observe_consume(ct, "outbound", elapsed)

                    yield CommandDelivery(cmd=cmd, raw=raw, span=span)
        finally:
            await psub.unsubscribe()


def _channel_type_from_subject(subject: str) -> str:
    """Extract channel_type token from subject for metrics when proto decode fails.

    subject = mio.<dir>.<channel_type>.<acct>.<conv>[.<msg>]
    """
    parts = subject.split(".")
    if len(parts) >= 3:
        return parts[2]
    return "unknown"
