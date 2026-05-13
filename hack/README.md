# hack/

Dev-only scratch space: spikes, playground experiments, throwaway scripts.

## Rules

- **Not shipped**, **not tested in CI**, **not part of any release artefact**.
- Contents may break at any time; no compatibility guarantees.
- Service code in `services/` and adapter code in `channels/` must not import from here.
- Use this directory for one-off explorations that aren't ready (or never will be) for `services/`, `tools/`, or `examples/`.

## Conventions

- `hack/playground/` — long-running personal scratch space (gitignored sub-trees allowed).
- Other sub-directories should be named for the spike (`hack/<spike-name>/`) and removed once the experiment ends.
