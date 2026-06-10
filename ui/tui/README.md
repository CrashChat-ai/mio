# mio-tui

Minimal bubbletea TUI for inspecting an mio admin server. Read-only for v1.

## Run

```bash
# admin server running on loopback:9090 (default for cmd/admin)
make tui-run

# point at a remote / reverse-proxied admin:
ADMIN_URL=https://admin.example.com make tui-run
```

## Navigation

| Key | Action |
|---|---|
| `tab` | Next view (tenants → accounts → channels → tail) |
| `shift+tab` | Previous view |
| `↑` / `↓` / `j` / `k` | Move cursor in list views |
| `q` / `esc` / `ctrl+c` | Quit |

Selecting a tenant in the tenants view sets the filter for the accounts view;
selecting an account in the accounts view sets the filter for the tail view.

## Modules + workspace

`ui/tui/` lives in the root Go module. It consumes the in-repo generated proto
packages directly and stays alongside other human-facing operator surfaces under
`ui/`.

## What's missing

- Writes (StartInstall / CompleteInstall / RotateCredential) — read-only v1.
- TUI-driven OAuth dance — operators still use a browser for the consent URL.
- TLS/auth — admin is loopback-only by default; the TUI does not perform any
  authentication (it inherits the admin's posture).
