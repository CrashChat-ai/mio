#!/usr/bin/env python3
"""Python half of the mio.v1 proto round-trip test.

Protocol:
  - Reads raw proto bytes from stdin (written by the Go half via main.go).
  - Decodes as mio.v1.Message, re-encodes to proto wire format, writes to stdout.
  - The Go half then decodes the re-serialised bytes and checks field equality.

Run via: uv run --project sdk-py tools/proto-roundtrip/roundtrip.py
(No separate requirements.txt — uses sdk-py/pyproject.toml pinned protobuf.)

Subject-token validator:
  validate_subject_token(token) raises ValueError if the token contains
  characters outside [a-zA-Z0-9_-] (e.g. a dot would split a NATS subject).
"""

import re
import sys
import os

# Add proto/gen/py to path so we can import generated types without installing.
_REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
sys.path.insert(0, os.path.join(_REPO_ROOT, "proto", "gen", "py"))

from mio.v1 import message_pb2  # type: ignore[import]

# ---------------------------------------------------------------------------
# Subject-token validator (seed for SDK publish-time validation in P2)
# ---------------------------------------------------------------------------
_TOKEN_RE = re.compile(r"^[a-zA-Z0-9_-]+$")


def validate_subject_token(token: str) -> None:
    """Reject any NATS subject token that contains illegal characters.

    NATS splits subjects on '.'; a dot inside a token would silently create
    extra hierarchy levels and break subject filters (e.g. mio.inbound.*.>).
    We reject rather than sanitize — callers must normalise before calling.

    Args:
        token: A single NATS subject segment (no dots).

    Raises:
        ValueError: If the token contains characters outside [a-zA-Z0-9_-].
    """
    if not _TOKEN_RE.match(token):
        raise ValueError(
            f"Subject token {token!r} contains illegal characters. "
            "Only [a-zA-Z0-9_-] are allowed. Dots are forbidden — they split NATS subjects."
        )


def _run_roundtrip() -> None:
    """Read proto bytes from stdin, decode Message, re-encode, write to stdout."""
    raw = sys.stdin.buffer.read()
    msg = message_pb2.Message()
    msg.ParseFromString(raw)

    # Validate the subject tokens that would be derived from this message.
    # This mirrors what the SDK will enforce at publish time (P2).
    for field_name, value in [
        ("channel_type", msg.channel_type),
        ("account_id", msg.account_id),
        ("conversation_id", msg.conversation_id),
    ]:
        try:
            validate_subject_token(value)
        except ValueError as exc:
            print(f"FAIL: subject-token validation error on {field_name}: {exc}", file=sys.stderr)
            sys.exit(1)

    # Re-serialise and write back for Go to verify field equality.
    sys.stdout.buffer.write(msg.SerializeToString())


if __name__ == "__main__":
    _run_roundtrip()
