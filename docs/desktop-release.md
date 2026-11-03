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
3. Signs macOS bundles with Developer ID and notarizes them when the `APPLE_*` secrets are set
4. Uploads GitHub Release assets and writes `latest.json`

Required repo secrets (all optional except the updater key if you want auto-update):

| Secret | Purpose |
|--------|---------|
| `TAURI_SIGNING_PRIVATE_KEY` | Minisign private key (one-line base64 contents of `updater.key`) |
| `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` | Optional password |
| `APPLE_CERTIFICATE` | Developer ID Application `.p12`, single-line base64 |
| `APPLE_CERTIFICATE_PASSWORD` | Password used when exporting the `.p12` |
| `APPLE_SIGNING_IDENTITY` | e.g. `Developer ID Application: Foo (TEAMID)` — must match the cert CN |
| `APPLE_ID` | Apple ID email for notarization |
| `APPLE_PASSWORD` | App-specific password (`abcd-efgh-ijkl-mnop`) |
| `APPLE_TEAM_ID` | 10-character Team ID |

Set them all interactively (p12 path as the only argument):

```bash
make set-release-secrets P12=~/certs/developer-id.p12
# or
scripts/gh_set_release_secrets.sh ~/certs/developer-id.p12
```

Without the `APPLE_*` secrets the macOS job still builds — the bundle is ad-hoc signed, so
the DMG ships with right-click-to-open instructions and the app is not distributable.

Signing flow on macOS (`scripts/pack_desktop_macos.sh`):

- `scripts/ci_import_apple_cert.sh` imports the p12 into a throwaway keychain on the runner.
- The Go sidecar is signed **before** `tauri build`, because Tauri copies it into
  `Contents/MacOS/`; touching a binary inside a signed bundle afterwards invalidates the
  signature (the "app is damaged" failure mode).
- After the build the signature is verified (`codesign --verify --deep --strict`) and the
  job fails if it is not a Developer ID signature.
- Our custom DMG (with the Chinese install notes) is built after Tauri's own, so it is
  notarized and stapled separately.

Updater endpoint (clients poll this):

`https://github.com/danmo-ai/danmo-novel/releases/latest/download/latest.json`

## Dev desktop

```bash
make dev-desktop
```

Starts the Go backend via scripts and launches Tauri with Vite HMR. Set `SKIP_BACKEND=1` to attach an external debugger on `:7804`.
