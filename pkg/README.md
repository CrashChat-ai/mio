# pkg/

Shared internal Go libraries for the mio monorepo.

## Rules

- **No `utils/`, `common/`, or `helpers/`** sub-packages. Name by capability.
- Each sub-package solves one well-scoped concern and exposes a small, opinionated surface.
- Anything that grows multiple responsibilities should be split, not piled up under a generic name.
- Imported by services in `services/` and channel adapters in `channels/`. Should not import from those.

## Status

Empty placeholder. Code lands here only when at least two callers genuinely share it.
