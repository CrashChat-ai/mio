# ee/

Reserved directory for the commercial overlay.

## Rules

- Code under `ee/` is **build-tag-gated** (e.g. `//go:build ee`) and **not part of the OSS Apache-2.0 distribution**.
- The OSS build (default tags) must compile and run with `ee/` absent or empty.
- No OSS code may import from `ee/`. The dependency direction is strictly `ee/ → services/, pkg/, sdks/`, never the reverse.
- Licensing for `ee/` code is determined separately; do not assume Apache-2.0.

## Status

Empty placeholder reserved during the Option B layout migration so future commercial features have a known home and the boundary is encoded in the tree itself.
