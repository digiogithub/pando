---
created_at: 2026-09-14T19:04:55.581199728Z
updated_at: 2026-09-14T19:04:55.581199728Z
tags:
    - feature
    - security
    - agui
    - mcp
    - cmd
---

# Persist and reuse the generated AG-UI and MCP listener tokens

Implements [[PANDO-US-0030]] (`.kb/.pmngr/stories/PANDO-US-0030-persist-and-reuse-generated-listener-tokens.md`),
follow-up of [[PANDO-US-0023]] / [[PANDO-US-0025]] (see
[[mcp_http_bearer_token_and_cors_allowlist]]).

## Problem

Both listeners generated a bearer token on first start and printed it exactly once to a stream
(MCP to stderr via `runMCPServerMode`, AG-UI to stdout via `runAGUIServe`), then forgot it. Under a
process supervisor, a wrapper script, or any scrolled terminal, that line was unrecoverable — the
only way back in was to stop the process and configure a token by hand.

## Decision

The generated token is now **stable**, not per-start: generate once, persist it, and read it back
on every later start with no configured token. The token lives on disk until removed — accepted
deliberately per the story.

## What changed

- **New file `cmd/listener_token.go`** (shared by both listeners, one mechanism/one file format):
  - `mcpTokenKind = "mcp"`, `aguiTokenKind = "agui"` — also the stored file's basename suffix.
  - `listenerTokenFilePath(kind)` — `<config.GlobalConfigDir()>/<kind>-token`, e.g.
    `~/.config/pando/mcp-token` / `~/.config/pando/agui-token`. Reuses the existing
    `config.GlobalConfigDir()` (`internal/config/global_projects.go:39`); **no changes to
    `internal/config`** were needed.
  - `loadStoredListenerToken(path)` — returns not-found for a missing or whitespace-only file;
    **refuses** (naming the file and its `%04o` mode) a file whose mode has the group- or
    world-READ bit set (`mode.Perm() & 0o044 != 0`), e.g. `0644`. The mode is never silently
    repaired. Owner-only execute/write bits (e.g. `0611`) do not trigger the refusal — only the
    read bits, per the story's literal wording.
  - `storeListenerToken(path, token)` — `os.MkdirAll(dir, 0o700)` then an atomic write (temp file
    `0o600` + rename), mirroring `saveGlobalProjects`'s existing pattern.
  - `generateListenerToken()` — 32 random bytes, hex-encoded (64 chars); replaces the two
    near-duplicate generators that existed before (`randomMCPHTTPToken`, `randomToken` — both
    removed).
  - `resolveStoredOrGeneratedListenerToken(kind)` — the shared **tail** of both listeners'
    precedence: read stored token if present, else generate + persist. Never inspects any explicit
    source itself — callers only reach it after ruling all of those out.
  - `printStoredListenerToken(kind)` — implements `--print-token`: reads ONLY the stored file,
    prints just the token to stdout, returns a (non-nil) error when nothing is stored yet (never
    generates, never starts anything).

- **`cmd/mcp_server.go`**:
  - `ensureMCPHTTPToken(host, configuredToken)` — same 3-value signature/tests preserved. New tail:
    configured token wins (unchanged) → **non-loopback refusal stays exactly where it was**, before
    ever touching the stored file (a stored token must never satisfy a non-loopback bind) → falls
    through to `resolveStoredOrGeneratedListenerToken(mcpTokenKind)`.
  - `randomMCPHTTPToken` removed (dead code, replaced by the shared generator); `crypto/rand`,
    `encoding/hex` imports dropped accordingly.
  - `runMCPServerMode`: added `--print-token` flag, handled as the very first statement (returns
    before `config.Load`, `db.Connect`, `app.New`). Startup printing changed: when the token isn't
    explicitly configured, the token file **path** is always printed to stderr; the token value
    itself is printed only when `tokenGenerated` is true (first start).

- **`cmd/agui_serve.go`**:
  - `resolveAGUIToken(...)` — same signature/tests preserved. Precedence unchanged for `--token`,
    `--token-file`, `PANDO_AGUI_TOKEN` (still identified by `*Set`, not emptiness, so an explicitly
    empty source still errors instead of falling through). Tail now calls
    `resolveStoredOrGeneratedListenerToken(aguiTokenKind)` instead of generating unconditionally.
  - `randomToken` removed; `crypto/rand`, `encoding/hex` imports dropped.
  - `runAGUIServe`: added `--print-token` flag (first statement, same early-return shape as MCP).
    Tracks a new `explicitToken bool` (flag/file/env `*Set`) to decide whether to print
    `Token file: <path>` to stdout; `Token:   <value>` is still printed only when generated.
  - `Long` command description and `Example` block updated to document the stored-file step and
    `--print-token`.

- **Tests** (all new/updated tests call `config.IsolateForTests(t)` so they never touch the
  developer's real `~/.config/pando`):
  - `cmd/listener_token_test.go` (new) — unit tests for every helper in `listener_token.go`,
    including the `0644`/`0640`/`0604`/`0664`/`0666` refusal table and the execute/write-bits-only
    exemption (`0611` accepted), atomic write + secure modes, kind independence, and
    `printStoredListenerToken` (stdout captured via `os.Pipe`).
  - `cmd/mcp_server_test.go` — added `TestEnsureMCPHTTPToken_PersistsAndReusesTheStoredToken`,
    `_RefusesGroupOrWorldReadableStoredFile`, `_NonLoopbackRefusalIgnoresStoredFile`,
    `TestRunMCPServerMode_PrintTokenFlagPrintsAndExitsWithoutStartingServer`. **Rewrote**
    `TestEnsureMCPHTTPToken_GeneratedTokensAreNotIdentical` (previously called the function twice in
    the same environment expecting different tokens — now false, since persistence is the whole
    point; rewritten to use two independently isolated config dirs) and restructured
    `TestEnsureMCPHTTPToken_LoopbackWithNoTokenGeneratesOne` to isolate per sub-test (all 4 loopback
    spellings shared one config dir before, which would have made only the first sub-test actually
    generate).
  - `cmd/agui_serve_test.go` — mirror additions: `_PersistsAndReusesTheStoredToken`,
    `_ExplicitTokenNeverWrittenToStoredFile` (also asserts the stored file's bytes are unchanged
    after an explicit token is used), `_RefusesGroupOrWorldReadableStoredFile`,
    `TestRunAGUIServe_PrintTokenFlagPrintsAndExitsWithoutStartingServer`. Same rewrite of
    `_GeneratedTokensAreNotIdentical` for the same reason as MCP.

## Final precedence order

- **AG-UI**: `--token` → `--token-file` → `PANDO_AGUI_TOKEN` → stored `agui-token` file → generate
  + persist.
- **MCP**: `MCPServer.HttpToken` → *(non-loopback bind refusal, unchanged, before the file is ever
  consulted)* → stored `mcp-token` file → generate + persist.

## Where the token lives now

`<config.GlobalConfigDir()>/mcp-token` and `<config.GlobalConfigDir()>/agui-token` (XDG-aware:
`$XDG_CONFIG_HOME/pando` or `~/.config/pando`), directory `0700`, file `0600` — readable only by the
owning user. `pando mcp-server --print-token` / `pando agui-serve --print-token` read it back
without starting a listener (exit non-zero, nothing on stdout, if none is stored).

## Verification

- `go build ./...`, `go vet ./...` — clean.
- `gofmt -l` on every touched file — clean (repo-wide `gofmt -l` still lists ~17 pre-existing
  unrelated files, none touched by this change).
- `go test ./cmd/... ./internal/config/...` — pass.
- `go test ./...` (full repo) — pass, zero `FAIL` lines, including
  `internal/agui.TestNewNeverLogsTheToken` (unaffected — `internal/agui` was not touched).
- Manual CLI smoke test with an isolated `$HOME`: `--print-token` before any token exists exits 1
  with an actionable error; a `0644` stored `mcp-token` is refused by `--print-token`, naming the
  file and `0644`, and does not get silently repaired.

## Not touched

`internal/agui/`, `internal/rag/`, `internal/api/`, `internal/mesnada/`, `sdk/`, and
`internal/config/` (no changes needed there — `config.GlobalConfigDir()` already existed and was
exported). No commit was created; the calling coordinator owns that step.