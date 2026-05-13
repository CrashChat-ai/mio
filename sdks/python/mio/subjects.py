"""Subject builder for MIO NATS subjects.

Grammar (locked from P2 plan + arch-doc §5):
    mio.<direction>.<channel_type>.<account_id>.<conversation_id>[.<message_id>]

Token rules: only [a-zA-Z0-9_-] allowed. Dots split the NATS subject hierarchy.
UUIDs and ULIDs are safe; free-text platform IDs must be normalised first.

The 6th segment (message_id) is outbound edit/delete only.
Inbound subjects are always 5 tokens (mio + 4 segments).
"""

import re
from mio.channeltypes import KNOWN, ALIASES

_TOKEN_RE = re.compile(r"^[a-zA-Z0-9_-]+$")


def _validate_token(t: str, field: str = "token") -> None:
    """Raise ValueError on empty or dot-containing tokens."""
    if not t:
        raise ValueError(f"subject {field} must not be empty")
    if not _TOKEN_RE.match(t):
        raise ValueError(
            f"subject {field} {t!r} contains illegal characters; "
            "only [a-zA-Z0-9_-] allowed (dots split NATS subjects)"
        )


def _validate_channel_type(channel_type: str) -> None:
    """Raise ValueError if channel_type is not in the active registry."""
    if channel_type in KNOWN:
        return
    if channel_type in ALIASES:
        return
    raise ValueError(
        f"channel_type {channel_type!r} not found in active registry "
        "(proto/channels.yaml); add it and re-run make proto-gen"
    )


def inbound(channel_type: str, account_id: str, conversation_id: str) -> str:
    """Build a 4-token inbound subject.

    Returns: mio.inbound.<channel_type>.<account_id>.<conversation_id>

    The 5th segment (message_id) is reserved for outbound edit/delete only.
    """
    _validate_channel_type(channel_type)
    for tok, field in [
        (channel_type, "channel_type"),
        (account_id, "account_id"),
        (conversation_id, "conversation_id"),
    ]:
        _validate_token(tok, field)
    return f"mio.inbound.{channel_type}.{account_id}.{conversation_id}"


def inbound_enriched(channel_type: str, account_id: str, conversation_id: str) -> str:
    """Build the enriched-stream inbound subject.

    Emitted by the attachment-downloader sidecar after attachment URLs have
    been rewritten to stable storage URLs. Same shape as ``inbound`` but with
    the ``inbound_enriched`` verb.

    Returns: mio.inbound_enriched.<channel_type>.<account_id>.<conversation_id>
    """
    _validate_channel_type(channel_type)
    for tok, field in [
        (channel_type, "channel_type"),
        (account_id, "account_id"),
        (conversation_id, "conversation_id"),
    ]:
        _validate_token(tok, field)
    return f"mio.inbound_enriched.{channel_type}.{account_id}.{conversation_id}"


def outbound(
    channel_type: str,
    account_id: str,
    conversation_id: str,
    message_id: str | None = None,
) -> str:
    """Build an outbound subject.

    Returns:
        mio.outbound.<channel_type>.<account_id>.<conversation_id>
        mio.outbound.<channel_type>.<account_id>.<conversation_id>.<message_id>

    The message_id segment is used only for edit/delete commands.
    """
    _validate_channel_type(channel_type)
    for tok, field in [
        (channel_type, "channel_type"),
        (account_id, "account_id"),
        (conversation_id, "conversation_id"),
    ]:
        _validate_token(tok, field)
    base = f"mio.outbound.{channel_type}.{account_id}.{conversation_id}"
    if message_id:
        _validate_token(message_id, "message_id")
        return f"{base}.{message_id}"
    return base
