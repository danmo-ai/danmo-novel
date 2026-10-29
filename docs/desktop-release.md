# Desktop packaging & auto-update

Danmo Novel ships a Tauri 2 desktop shell that embeds the Vue UI and spawns the Go API as a sidecar on `127.0.0.1:7804`.

## Local pack

```bash
# macOS (arm64/x86_64 host)
make pack-macos-desktop RELEASE_VERSION=0.1.0

# Linux x86_64
make pack-linux-desktop RELEASE_VERSION=0.1.0

# Windows x86_64 (Git Bash / MSYS)
make pack-windows-desktop RELEASE_VERSION=0.1.0
```

Artifacts land in `out/desktop/bundle/` (DMG / AppImage+deb / NSIS).

For updater signatures, place the private key at `desktop/src-tauri/keys/updater.key` (gitignored) or set:

- `TAURI_SIGNING_PRIVATE_KEY`
- `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` (optional; empty if key has no password)

Generate a new keypair:

```bash
cd desktop && npx tauri signer generate -w src-tauri/keys/updater.key
# put the printed public key into desktop/src-tauri/tauri.conf.json → plugins.updater.pubkey
```

## CI release

Push a tag `v*` (or run **Build Release Artifacts** workflow dispatch). The workflow:

1. Builds macOS / Linux / Windows desktop packages
2. Signs updater payloads when `TAURI_SIGNING_PRIVATE_KEY` is set
3. Uploads GitHub Release assets and writes `latest.json`

Required repo secrets:

| Secret | Purpose |
|--------|---------|
| `TAURI_SIGNING_PRIVATE_KEY` | Minisign private key (one-line base64 contents of `updater.key`) |
| `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` | Optional password |

```bash
gh secret set TAURI_SIGNING_PRIVATE_KEY < desktop/src-tauri/keys/updater.key
```

Updater endpoint (clients poll this):

`https://github.com/danmo-ai/danmo-novel/releases/latest/download/latest.json`

## Dev desktop

```bash
make dev-desktop
```

Starts the Go backend via scripts and launches Tauri with Vite HMR. Set `SKIP_BACKEND=1` to attach an external debugger on `:7804`.
