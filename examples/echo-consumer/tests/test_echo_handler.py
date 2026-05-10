"""Unit tests for the echo consumer handler.

Tests cover (no live NATS required):
  - handle() builds correct SendCommand from inbound Message
  - Four-tier scope (tenant_id, account_id, channel_type, conversation_id) preserved
  - thread_root_message_id preserved when set; falls back to source_message_id
  - text is prefixed with "echo: "
  - fresh ULID generated per call (non-empty, unique)
  - schema_version=1 always set on outbound
  - attributes["replied_to"] == inbound msg.id
  - edit_of_* fields empty (fresh send, not edit)
  - Schema-version mismatch on PUBLISH raises ValueError (SDK Verify; handler not called)
  - Consume-side does NOT validate schema_version (v2 passes through to handle())
"""

from __future__ import annotations

import os
import sys
from unittest.mock import AsyncMock

import pytest

# Resolve proto gen path relative to repo root before any echo import.
_REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
_PROTO_GEN_PY = os.path.join(_REPO_ROOT, "proto", "gen", "py")
if _PROTO_GEN_PY not in sys.path:
    sys.path.insert(0, _PROTO_GEN_PY)

from mio.v1.message_pb2 import Message  # noqa: E402
from mio.v1.send_command_pb2 import SendCommand  # noqa: E402
from mio.version import SCHEMA_VERSION, verify_command  # noqa: E402

# Import the handler under test.
from echo import handle  # noqa: E402


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_msg(**overrides) -> Message:
    """Build a minimal valid inbound Message proto."""
    defaults = dict(
        id="msg-01",
        schema_version=SCHEMA_VERSION,
        tenant_id="tenant-uuid-01",
        account_id="acct-uuid-02",
        channel_type="zoho_cliq",
        conversation_id="conv-uuid-03",
        conversation_external_id="ext-conv-01",
        parent_conversation_id="",
        source_message_id="src-msg-01",
        thread_root_message_id="",
        text="hello world",
    )
    defaults.update(overrides)
    msg = Message(**defaults)
    return msg


class _FakeClient:
    """Minimal sdk-py Client stand-in — records published SendCommands."""

    def __init__(self):
        self.published: list[SendCommand] = []
        self.publish_outbound = AsyncMock(side_effect=self._capture)

    async def _capture(self, cmd: SendCommand) -> None:
        self.published.append(cmd)


# ---------------------------------------------------------------------------
# Happy path
# ---------------------------------------------------------------------------


async def test_handle_basic_echo():
    """handle() produces a SendCommand with echo text and correct fields."""
    msg = _make_msg(text="hi there")
    client = _FakeClient()

    cmd = await handle(msg, client)

    assert cmd.text == "echo: hi there"
    assert cmd.schema_version == 1
    assert cmd.tenant_id == msg.tenant_id
    assert cmd.account_id == msg.account_id
    assert cmd.channel_type == msg.channel_type
    assert cmd.conversation_id == msg.conversation_id


async def test_handle_four_tier_scope_preserved():
    """Four-tier IDs copied verbatim: tenant, account, channel, conversation."""
    msg = _make_msg(
        tenant_id="T1",
        account_id="A1",
        channel_type="zoho_cliq",
        conversation_id="C1",
    )
    client = _FakeClient()

    cmd = await handle(msg, client)

    assert cmd.tenant_id == "T1"
    assert cmd.account_id == "A1"
    assert cmd.channel_type == "zoho_cliq"
    assert cmd.conversation_id == "C1"


# ---------------------------------------------------------------------------
# Thread root fallback
# ---------------------------------------------------------------------------


async def test_handle_thread_root_preserved():
    """When thread_root_message_id is set, outbound carries it unchanged."""
    msg = _make_msg(
        thread_root_message_id="root-msg-999",
        source_message_id="src-msg-001",
    )
    client = _FakeClient()

    cmd = await handle(msg, client)

    assert cmd.thread_root_message_id == "root-msg-999"


async def test_handle_thread_root_fallback_to_source():
    """When thread_root_message_id is empty, falls back to source_message_id."""
    msg = _make_msg(
        thread_root_message_id="",
        source_message_id="src-msg-fresh",
    )
    client = _FakeClient()

    cmd = await handle(msg, client)

    assert cmd.thread_root_message_id == "src-msg-fresh"


# ---------------------------------------------------------------------------
# Idempotency / uniqueness
# ---------------------------------------------------------------------------


async def test_handle_fresh_ulid_per_call():
    """Each handle() call produces a unique cmd.id (fresh ULID)."""
    msg = _make_msg()
    client = _FakeClient()

    cmd1 = await handle(msg, client)
    cmd2 = await handle(msg, client)

    assert cmd1.id != ""
    assert cmd2.id != ""
    assert cmd1.id != cmd2.id, "each call must produce a distinct ULID"


# ---------------------------------------------------------------------------
# Attributes
# ---------------------------------------------------------------------------


async def test_handle_replied_to_attribute():
    """attributes['replied_to'] == inbound msg.id."""
    msg = _make_msg(id="inbound-id-42")
    client = _FakeClient()

    cmd = await handle(msg, client)

    assert cmd.attributes.get("replied_to") == "inbound-id-42"


# ---------------------------------------------------------------------------
# Fresh send semantics (not an edit)
# ---------------------------------------------------------------------------


async def test_handle_edit_fields_empty():
    """edit_of_message_id and edit_of_external_id are empty for echo sends."""
    msg = _make_msg()
    client = _FakeClient()

    cmd = await handle(msg, client)

    assert cmd.edit_of_message_id == ""
    assert cmd.edit_of_external_id == ""


# ---------------------------------------------------------------------------
# Publish call verified
# ---------------------------------------------------------------------------


async def test_handle_calls_publish_outbound_once():
    """publish_outbound called exactly once per message."""
    msg = _make_msg()
    client = _FakeClient()

    await handle(msg, client)

    client.publish_outbound.assert_awaited_once()
    published_cmd = client.publish_outbound.call_args[0][0]
    assert published_cmd.channel_type == "zoho_cliq"


# ---------------------------------------------------------------------------
# Schema-version asymmetry
# ---------------------------------------------------------------------------


async def test_consume_side_v2_passes_through_to_handle():
    """A v2 Message reaches handle() untouched — consume does NOT validate schema.

    This is the intentional P2 publish-only asymmetry: older consumers must
    tolerate forward-compatible additions from future schema versions.
    """
    # Construct a v2 Message directly — bypassing SDK publish (which would reject it).
    msg = _make_msg(schema_version=2, text="future payload")
    client = _FakeClient()

    # handle() must not raise — it is schema-version agnostic.
    cmd = await handle(msg, client)

    assert cmd.text == "echo: future payload"


def test_publish_side_schema_mismatch_raises():
    """SDK verify_command raises ValueError for schema_version != 1 on publish.

    This confirms the publish-side gate works; the echo handler itself does
    not call verify (that is sdk-py's responsibility in publish_outbound).
    """
    bad_cmd = SendCommand(
        id="cmd-bad",
        schema_version=2,  # not SCHEMA_VERSION
        tenant_id="T1",
        account_id="A1",
        channel_type="zoho_cliq",
        conversation_id="C1",
    )
    with pytest.raises(ValueError, match="schema_version mismatch"):
        verify_command(bad_cmd)


# ---------------------------------------------------------------------------
# conversation_external_id preserved
# ---------------------------------------------------------------------------


async def test_handle_conversation_external_id_preserved():
    """conversation_external_id forwarded to SendCommand unchanged."""
    msg = _make_msg(conversation_external_id="EXT-CONV-XYZ")
    client = _FakeClient()

    cmd = await handle(msg, client)

    assert cmd.conversation_external_id == "EXT-CONV-XYZ"
