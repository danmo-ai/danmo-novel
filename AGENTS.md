# Danmo Novel — Agent Guide

## What this is

Danmo Novel — controlled AI novel production workbench. Go backend + Vue 3 frontend. No Agent Loop: fixed task contracts, bounded LLM calls, candidate-accept workflow.

| Item | Location |
|------|----------|
| API entry | `cmd/danmo-novel/main.go` |
| Router | `core/adapter/http/router.go` |
| LLM port | `core/port/llm.go` |
| LLM adapters | `core/adapter/llm/` (OpenAI / Responses / Anthropic / mock + dialects) |
| LLM config | `core/service/llmconfig/` |
| Novel domain | `core/domain/novel.go`, `core/domain/llm_config.go` |
| Book loader | `core/service/books/` (compatible with work `novel/<book-id>/`) |
| Context builder | `core/service/context/` |
| Pipeline | `core/service/pipeline/` |
| Gates | `core/service/gates/` |
| Runs/accept | `core/service/runs/` |
| SQLite | `core/store/sqlite/` |
| Frontend | `frontend/` |
| Desktop (Tauri) | `desktop/` |
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

## Commands

```bash
make dev                 # backend :7804 + vite :5804
make dev-desktop         # backend + Tauri webview
make test
make build
make pack-macos-desktop  # also pack-linux-desktop / pack-windows-desktop
```

## Reused assets

- LLM client/dialects migrated from Danmo Work (`core/adapter/llm`).
- Novel five-stage contracts inspired by `core/resource/plugins/novel` in Danmo Work.
- Prompt knowledge: `core/resource/novel/` (built-in Markdown in `knowledge/`, embedded by `knowledge.go`).

## Product references

Sudowrite Story Bible, Novelcrafter Write/Codex, NarraLume candidate-accept, Chaptora inline diff.
