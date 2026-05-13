"""Schema version enforcement for MIO SDK (publish-side only).

Asymmetry contract (locked — do not relax):
  Publish: verify() / verify_command() hard-reject schema mismatches.
  Consume: NO verify call. Consumers must tolerate unknown major versions to
           remain forward-compatible with future schema additions.
           A schema_version=2 message received on consume MUST pass through
           untouched — rejection is the publisher's responsibility.
"""

from mio.channeltypes import KNOWN, ALIASES

SCHEMA_VERSION: int = 1


def verify(msg: object) -> None:  # msg: mio.v1.Message (proto)
    """Validate a Message before publish.

    Checks (publish-side only — see module docstring for asymmetry):
      1. schema_version == SCHEMA_VERSION (== 1).
      2. tenant_id, account_id, channel_type, conversation_id must be non-empty.
      3. channel_type must be in the active registry.

    Raises ValueError on any violation.
    """
    if msg is None:
        raise ValueError("message is None")

    sv = getattr(msg, "schema_version", None)
    if sv != SCHEMA_VERSION:
        raise ValueError(
            f"schema_version mismatch: got {sv!r}, want {SCHEMA_VERSION}; "
            "upgrade the publisher or SDK"
        )

    for field in ("tenant_id", "account_id", "channel_type", "conversation_id"):
        val = getattr(msg, field, "")
        if not val:
            raise ValueError(
                f"required field {field!r} is empty; all four-tier IDs "
                "(tenant_id, account_id, channel_type, conversation_id) "
                "must be set at publish time"
            )

    ct = msg.channel_type  # type: ignore[attr-defined]
    if ct not in KNOWN and ct not in ALIASES:
        raise ValueError(
            f"channel_type {ct!r} not found in active registry "
            "(proto/channels.yaml); add it and re-run make proto-gen"
        )


def verify_command(cmd: object) -> None:  # cmd: mio.v1.SendCommand (proto)
    """Validate a SendCommand before publish.

    Same asymmetry rule: call only on publish; skip on consume.

    Raises ValueError on any violation.
    """
    if cmd is None:
        raise ValueError("send_command is None")

    sv = getattr(cmd, "schema_version", None)
    if sv != SCHEMA_VERSION:
        raise ValueError(
            f"schema_version mismatch: got {sv!r}, want {SCHEMA_VERSION}; "
            "upgrade the publisher or SDK"
        )

    for field in ("id", "tenant_id", "account_id", "channel_type", "conversation_id"):
        val = getattr(cmd, field, "")
        if not val:
            raise ValueError(f"required field {field!r} is empty in SendCommand")

    ct = cmd.channel_type  # type: ignore[attr-defined]
    if ct not in KNOWN and ct not in ALIASES:
        raise ValueError(
            f"channel_type {ct!r} not found in active registry "
            "(proto/channels.yaml); add it and re-run make proto-gen"
        )
