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
from mio.client import Client, Delivery, CommandDelivery
from mio.version import SCHEMA_VERSION, verify, verify_command
from mio.subjects import inbound, outbound
from mio.channeltypes import KNOWN, ALIASES

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
