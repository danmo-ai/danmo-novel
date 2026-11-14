# Danmo Novel — Agent Guide

## What this is

Danmo Novel — controlled AI novel production workbench. Go backend + Vue 3 desktop frontend; native SwiftUI/Compose mobile apps. No Agent Loop: fixed task contracts, bounded LLM calls, candidate-accept workflow.

| Item | Location |
|------|----------|
| API entry | `cmd/danmo-novel/main.go` |
| Router | `core/adapter/http/router.go` |
| LLM port | `core/port/llm.go` |
| LLM adapters | `core/adapter/llm/` (OpenAI / Responses / Anthropic / mock + dialects) |
| LLM config | `core/service/llmconfig/` |
| Novel domain | `core/domain/novel.go`, `core/domain/llm_config.go` |
| Book loader | `core/service/books/` (standard book tree only) |
| Context builder | `core/service/context/` |
| Pipeline | `core/service/pipeline/` |
| Gates | `core/service/gates/` |
| Runs/accept | `core/service/runs/` |
| SQLite | `core/store/sqlite/` |
| Frontend | `frontend/` (Vue 3, desktop only) |
| Desktop (Tauri) | `desktop/` |
| Native mobile | `native-ios/` (SwiftUI) · `native-android/` (Compose) |
| Gomobile binding | `mobile/` (xcframework / aar for native apps) |
| Home | `~/.danmo-novel/` (`NOVEL_HOME`) |
| Dev ports | API 7804 · UI 5804 |
| Release CI | `.github/workflows/release.yml` |

## Hard rules

1. **No tools in LLM calls.** `DefaultLLMProviderClient` rejects non-empty `Tools`.
2. **Candidates, not commits.** Generation never writes canon/prose. Only `Accept` writes files and advances status.
3. **Fixed budgets.** `CallPolicy.MaxCalls`; at most one structure repair where allowed.
4. **No silent provider fallback.**
5. **API never returns API key plaintext.** Blank key on edit = keep.
6. **Status flow:** `proposed → accepted → drafted → reviewed`.
7. **Books are Markdown/YAML on disk; SQLite holds configs/runs/candidates.**
8. **REST API changes ship with mobile.** Any REST API change must update the affected mobile code (`native-ios/`, `native-android/`) in the same change.
9. **iOS is the mobile baseline.** New mobile features go out as an iOS test build first; once settled, Android replicates the iOS version.

## Commands

```bash
make dev                 # backend :7804 + vite :5804
make dev-desktop         # backend + Tauri webview
make test
make build
make pack-macos-desktop  # also pack-linux-desktop / pack-windows-desktop
make pack-ios / pack-android # native SwiftUI / Compose + in-process Go REST
```

## Reused assets

- LLM client/dialects migrated from Danmo Work (`core/adapter/llm`).
- Novel five-stage contracts inspired by `core/resource/plugins/novel` in Danmo Work.
- Prompt knowledge: `core/resource/novel/` (built-in Markdown in `knowledge/`, embedded by `knowledge.go`).

## Product references

Sudowrite Story Bible, Novelcrafter Write/Codex, NarraLume candidate-accept, Chaptora inline diff.
