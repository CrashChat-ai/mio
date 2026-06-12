# MIO Python SDK
#
# Async-only: nats-py has no sync API. Callers must use asyncio.
# See mio/client.py for connection setup.
#
# Usage:
#   client = await mio.Client.connect("nats://localhost:4222", name="my-service")
#   await client.publish_inbound(msg)
#   async for delivery in client.consume_inbound("my-durable"):
#       ...
#       await delivery.ack()
#   await client.aclose()
from pathlib import Path
from pkgutil import extend_path

from mio.channeltypes import ALIASES, KNOWN
from mio.client import Client, CommandDelivery, Delivery
from mio.subjects import inbound, outbound
from mio.version import SCHEMA_VERSION, verify, verify_command

# Allow local generated stubs from `buf generate` at repo-root/proto/gen/py/mio
# to coexist with this SDK package during development and tests.
__path__ = extend_path(__path__, __name__)  # type: ignore[name-defined]
_parents = Path(__file__).resolve().parents
# parents[3] exists only in the repo layout (sdks/python/mio/); container
# images copy the SDK shallower — skip the dev convenience there.
if len(_parents) > 3:
    _repo_generated = _parents[3] / "proto" / "gen" / "py" / "mio"
    if _repo_generated.exists():
        __path__.append(str(_repo_generated))  # type: ignore[name-defined]

__all__ = [
    "Client",
    "Delivery",
    "CommandDelivery",
    "SCHEMA_VERSION",
    "verify",
    "verify_command",
    "inbound",
    "outbound",
    "KNOWN",
    "ALIASES",
]
