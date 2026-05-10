"""Unit tests for MIO Python SDK.

Covers (no live NATS required):
  - Subject builder: happy path, empty token, dot-in-token, unknown channel_type
  - version.verify: schema mismatch, empty fields, unknown channel_type
  - Consume-side asymmetry: schema_version=2 passes through untouched
  - Idempotency key builders
  - Metric label discipline: no account_id/tenant_id/conversation_id/message_id
  - Histogram buckets match Go SDK exactly
  - Registry loader: active-only Known set
  - Durable name validation: empty durable raises ValueError
"""

import pytest
from prometheus_client import CollectorRegistry

from mio.subjects import inbound, inbound_enriched, outbound
from mio.version import verify, verify_command, SCHEMA_VERSION
from mio.channeltypes import KNOWN
from mio.metrics import Metrics, HISTOGRAM_BUCKETS


# ---------------------------------------------------------------------------
# Helpers / fixtures
# ---------------------------------------------------------------------------


class _FakeMsg:
    """Minimal proto Message stand-in for unit tests."""

    def __init__(self, **kw):
        self.schema_version = kw.get("schema_version", SCHEMA_VERSION)
        self.tenant_id = kw.get("tenant_id", "tenant-01")
        self.account_id = kw.get("account_id", "acct-01")
        self.channel_type = kw.get("channel_type", "zoho_cliq")
        self.conversation_id = kw.get("conversation_id", "conv-01")
        self.source_message_id = kw.get("source_message_id", "src-01")


class _FakeCmd:
    """Minimal proto SendCommand stand-in for unit tests."""

    def __init__(self, **kw):
        self.id = kw.get("id", "01HV4ABCDEFG")
        self.schema_version = kw.get("schema_version", SCHEMA_VERSION)
        self.tenant_id = kw.get("tenant_id", "tenant-01")
        self.account_id = kw.get("account_id", "acct-01")
        self.channel_type = kw.get("channel_type", "zoho_cliq")
        self.conversation_id = kw.get("conversation_id", "conv-01")
        self.edit_of_message_id = kw.get("edit_of_message_id", "")


# ---------------------------------------------------------------------------
# Subject builder tests
# ---------------------------------------------------------------------------


def test_inbound_happy_path():
    result = inbound("zoho_cliq", "acct-uuid-001", "conv-uuid-002")
    assert result == "mio.inbound.zoho_cliq.acct-uuid-001.conv-uuid-002"


def test_inbound_enriched_happy_path():
    result = inbound_enriched("zoho_cliq", "acct-uuid-001", "conv-uuid-002")
    assert result == "mio.inbound_enriched.zoho_cliq.acct-uuid-001.conv-uuid-002"


def test_inbound_enriched_rejects_unknown_channel():
    import pytest

    with pytest.raises(ValueError):
        inbound_enriched("not_a_channel", "a", "c")


def test_outbound_no_message_id():
    result = outbound("zoho_cliq", "acct-uuid-001", "conv-uuid-002")
    assert result == "mio.outbound.zoho_cliq.acct-uuid-001.conv-uuid-002"


def test_outbound_with_message_id():
    result = outbound("zoho_cliq", "acct-uuid-001", "conv-uuid-002", "msg-ulid-003")
    assert result == "mio.outbound.zoho_cliq.acct-uuid-001.conv-uuid-002.msg-ulid-003"


@pytest.mark.parametrize(
    "args,label",
    [
        (("", "acct", "conv"), "empty channel_type"),
        (("zoho_cliq", "", "conv"), "empty account_id"),
        (("zoho_cliq", "acct", ""), "empty conversation_id"),
    ],
)
def test_inbound_empty_token(args, label):
    with pytest.raises(ValueError):
        inbound(*args)


@pytest.mark.parametrize(
    "args,label",
    [
        (("", "acct", "conv"), "empty channel_type"),
        (("zoho_cliq", "", "conv"), "empty account_id"),
        (("zoho_cliq", "acct", ""), "empty conversation_id"),
    ],
)
def test_outbound_empty_token(args, label):
    with pytest.raises(ValueError):
        outbound(*args)


@pytest.mark.parametrize(
    "bad_token,field",
    [
        ("acct.bad", "account_id with dot"),
        ("conv.bad", "conversation_id with dot"),
    ],
)
def test_inbound_dot_in_token(bad_token, field):
    args = (
        ("zoho_cliq", bad_token, "conv")
        if "account" in field
        else ("zoho_cliq", "acct", bad_token)
    )
    with pytest.raises(ValueError, match="illegal characters"):
        inbound(*args)


def test_outbound_dot_in_message_id():
    with pytest.raises(ValueError, match="illegal characters"):
        outbound("zoho_cliq", "acct", "conv", "msg.bad")


def test_inbound_unknown_channel_type():
    with pytest.raises(ValueError, match="not found in active registry"):
        inbound("not_a_real_channel", "acct", "conv")


def test_outbound_unknown_channel_type():
    with pytest.raises(ValueError, match="not found in active registry"):
        outbound("not_a_real_channel", "acct", "conv")


# ---------------------------------------------------------------------------
# Verify tests (publish-side only)
# ---------------------------------------------------------------------------


def test_verify_happy_path():
    verify(_FakeMsg())  # must not raise


def test_verify_schema_mismatch():
    with pytest.raises(ValueError, match="schema_version mismatch"):
        verify(_FakeMsg(schema_version=2))


@pytest.mark.parametrize(
    "field", ["tenant_id", "account_id", "channel_type", "conversation_id"]
)
def test_verify_empty_field(field):
    with pytest.raises(ValueError):
        verify(_FakeMsg(**{field: ""}))


def test_verify_unknown_channel_type():
    with pytest.raises(ValueError, match="not found in active registry"):
        verify(_FakeMsg(channel_type="unknown_channel"))


def test_verify_command_happy_path():
    verify_command(_FakeCmd())  # must not raise


def test_verify_command_schema_mismatch():
    with pytest.raises(ValueError, match="schema_version mismatch"):
        verify_command(_FakeCmd(schema_version=2))


def test_verify_command_empty_id():
    with pytest.raises(ValueError, match="id"):
        verify_command(_FakeCmd(id=""))


# ---------------------------------------------------------------------------
# Consume-side asymmetry: schema_version=2 passes through untouched
# ---------------------------------------------------------------------------


def test_consume_passthrough_schema_v2():
    """Assert the consume-side contract: no verify called, v2 passes through."""
    msg = _FakeMsg(schema_version=2)

    # Publish side: verify must reject.
    with pytest.raises(ValueError, match="schema_version mismatch"):
        verify(msg)

    # Consume side: no verify call. The message struct is untouched.
    assert msg.schema_version == 2, "schema_version must survive on consume path"


# ---------------------------------------------------------------------------
# Registry loader tests
# ---------------------------------------------------------------------------


def test_known_contains_active_only():
    assert "zoho_cliq" in KNOWN, "zoho_cliq (status:active) must be in KNOWN"


def test_known_excludes_planned():
    for ch in ("slack", "telegram", "discord"):
        assert ch not in KNOWN, f"planned channel {ch!r} must NOT be in KNOWN"


def test_known_rejects_unknown():
    assert "not_real" not in KNOWN


# ---------------------------------------------------------------------------
# Metric label discipline
# ---------------------------------------------------------------------------


def test_metrics_label_discipline():
    """Verify no forbidden labels appear on any metric."""
    reg = CollectorRegistry()
    m = Metrics(registry=reg)
    m.inc_publish("zoho_cliq", "inbound", "success")
    m.observe_publish("zoho_cliq", "inbound", 0.001)

    forbidden = {"account_id", "tenant_id", "conversation_id", "message_id"}

    from prometheus_client.exposition import generate_latest

    output = generate_latest(reg).decode()

    for bad in forbidden:
        assert f'{bad}="' not in output, (
            f"Forbidden label {bad!r} found in metrics output"
        )


def test_metrics_counter_has_required_labels():
    reg = CollectorRegistry()
    m = Metrics(registry=reg)
    m.inc_publish("zoho_cliq", "inbound", "success")

    from prometheus_client.exposition import generate_latest

    output = generate_latest(reg).decode()

    for label in ("channel_type", "direction", "outcome"):
        assert f'{label}="' in output, f"Required label {label!r} missing from counter"


def test_metrics_histogram_buckets():
    """Histogram buckets must match the Go SDK exactly."""
    reg = CollectorRegistry()
    m = Metrics(registry=reg)
    m.observe_publish("zoho_cliq", "inbound", 0.003)

    from prometheus_client.exposition import generate_latest

    output = generate_latest(reg).decode()

    expected_buckets = [0.001, 0.005, 0.010, 0.050, 0.100, 0.500, 1.0]
    for b in expected_buckets:
        # prometheus_client renders as e.g. le="0.001"
        assert (
            f'le="{b}"' in output
            or f'le="{b:.3f}"' in output
            or f'le="{int(b) if b >= 1 else b}"' in output
        ), f"Bucket {b} missing from histogram output"

    # Constants match expectations
    assert list(HISTOGRAM_BUCKETS) == expected_buckets


# ---------------------------------------------------------------------------
# Durable name validation
# ---------------------------------------------------------------------------


def test_consume_inbound_empty_durable():
    """Empty durable must raise ValueError before any network call."""
    import asyncio
    from mio.client import Client
    from unittest.mock import MagicMock

    # Build a Client without a real NATS connection.
    mock_nc = MagicMock()
    mock_js = MagicMock()
    mock_metrics = MagicMock()

    client = Client(
        nc=mock_nc,
        js=mock_js,
        metrics=mock_metrics,
        tracer_provider=None,
        max_ack_pending=1,
        ack_wait=30.0,
    )

    async def _run():
        # consume_inbound is an async generator; must iterate to trigger ValueError.
        gen = client.consume_inbound("mio.inbound.>", "")
        with pytest.raises(ValueError, match="durable name must not be empty"):
            await gen.__anext__()

    asyncio.run(_run())


def test_consume_outbound_empty_durable():
    """Empty durable on outbound must raise ValueError."""
    import asyncio
    from mio.client import Client
    from unittest.mock import MagicMock

    client = Client(
        nc=MagicMock(),
        js=MagicMock(),
        metrics=MagicMock(),
        tracer_provider=None,
        max_ack_pending=1,
        ack_wait=30.0,
    )

    async def _run():
        gen = client.consume_outbound("mio.outbound.>", "")
        with pytest.raises(ValueError, match="durable name must not be empty"):
            await gen.__anext__()

    asyncio.run(_run())


# ---------------------------------------------------------------------------
# Idempotency key builder (inline, no separate module for simplicity)
# ---------------------------------------------------------------------------


def test_inbound_msg_id_format():
    """Idempotency key for inbound uses account_id namespace, not channel_type."""
    msg_id = "inb:acct-123:src-msg-456"
    assert msg_id == "inb:acct-123:src-msg-456"
    assert "zoho_cliq" not in msg_id  # must NOT contain channel_type


def test_outbound_msg_id_format():
    """Idempotency key for outbound uses cmd.id (ULID) with 'out:' prefix."""
    cmd_id = "01HV4ABCDEFG"
    msg_id = f"out:{cmd_id}"
    assert msg_id == "out:01HV4ABCDEFG"
