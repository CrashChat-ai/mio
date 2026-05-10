# MIO Echo Consumer — reference container (python:3.12-slim, single-stage)
# Multi-stage distroless deferred to P7 production deployment.
#
# Build context MUST be the repo root (not examples/echo-consumer/) so both
# sdk-py/ and proto/gen/py/ are accessible:
#   docker build -f examples/echo-consumer/Dockerfile -t mio/echo-consumer:dev .

FROM python:3.12-slim

WORKDIR /app

# Copy the SDK and proto-gen directories needed at runtime.
COPY sdk-py/ /sdk-py/
COPY proto/gen/py/ /proto/gen/py/

# Copy the echo-consumer package.
COPY examples/echo-consumer/pyproject.toml /app/pyproject.toml
COPY examples/echo-consumer/echo.py /app/echo.py

# Install sdk-py first (editable so SDK source reflects without rebuild in dev),
# then echo-consumer itself. pip install order matters for editable deps.
# --no-cache-dir keeps image layers lean (pip cache is never reused in immutable images).
RUN pip install --no-cache-dir -e /sdk-py && \
    pip install --no-cache-dir -e /app

# Runtime environment defaults — overridden by docker-compose or K8s.
ENV NATS_URL=nats://nats:4222

# Do NOT set PYTHONPATH=/proto/gen/py — it injects /proto/gen/py/mio as a
# namespace package that shadows the sdk-py 'mio' module on first import,
# losing mio.Client. echo.py performs its own namespace grafting from
# /proto/gen/py/mio/v1/ at runtime, so the explicit PYTHONPATH is unnecessary.

CMD ["python", "echo.py"]
