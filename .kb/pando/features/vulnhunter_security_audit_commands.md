---
created_at: 2026-07-20T09:44:47.74561349Z
updated_at: 2026-07-20T09:44:47.74561349Z
tags:
    - feature
    - security
    - slash-commands
    - vulnhunter
    - tui
    - webui
    - acp
---
# VulnHunter security-audit slash commands (2026-07-20)

Port of Capital One's [VulnHunter](https://github.com/capitalone/VulnHunter) Claude
Code skills into Pando as three workflow slash commands, available on TUI, WebUI
and ACP (no CLI flags, no new config). Follows the `/improve-agents-md` archetype:
a slash command that expands into a full embedded workflow prompt run as a normal
agent turn (streams, steers, persists) — NOT a persistent state mode like
[[feature_ponytail_mode]] / caveman / superpowers.

## Commands
- `/vulnhunt [scope]` — adversarial security audit. Recon (map entry points) →
  parallel class-group hunt → exploitability verify → adversarial disprove →
  capability-filtered report. Hunt runs in parallel via `mesnada_spawn_agent`
  (engine claude / model sonnet), one subagent per vuln class.
- `/vulnhunter-fix [finding]` — test-driven remediation. TDD gate: no source edit
  until a failing security test (RED) exists on disk; then fix (GREEN), regression
  check, commit. Uses TaskCreate/TaskUpdate for multi-finding clusters.
- `/vulnhunt-fix-verify [findings]` — read-only independent verification; per-finding
  verdict FIXED / PARTIAL / NOT_FIXED / INCONCLUSIVE / INVALID_INPUT. Code is
  source of truth, no bridging, fail-closed.

## Adaptations vs upstream
Dropped the upstream harness (detect_mode.sh, preflight.py, Opus-only gate, GitHub
issue harvesting, private forks, PR delivery gates, OUT dirs, JSON schemas).
Findings flow through the KB instead: each command reports via `kb_add_document`
under `pando/security/vulnhunt-*.md` and recovers prior context via
`kb_search_documents`; recon uses `code_hybrid_search`/`code_search_pattern`/grep.

## Files
- `internal/vulnhunter/` NEW: `vulnhunter.go` (HuntPrompt/FixPrompt/VerifyPrompt +
  withScope for the free-text arg), embedded `hunt.md` / `fix.md` / `verify.md`,
  `vulnhunter_test.go`.
- `internal/commands/registry.go` — 3 entries in BuiltinCommands (AcceptsArgs true).
  This also feeds TUI autocomplete via `commands.AllCommands` (no completions edit).
- `internal/api/handlers_chat.go:handleSlashCommandStream` — case for the 3 names,
  `bgRunner.Submit(vhSvc.Run(ctx, sid, prompt))` + streamSessionEvents (WebUI+TUI).
- `internal/tui/page/chat.go` — `expandVulnhunterCommand` (matched before normal
  Run/Steer flow; `/vulnhunter-fix` checked before `/vulnhunt` for prefix safety).
- ACP: `session_state.go` (3 tokens), `slash_commands.go` (3 kinds + 3 specs),
  `goal_commands.go` (3 dispatch cases + `processVulnhunterCommand` calling
  `processPromptWithAgent`).

## Verification
`go build ./...` clean. `go test ./internal/vulnhunter ./internal/commands
./internal/api ./internal/mesnada/acp ./internal/llm/agent` all pass. Updated
`TestAvailableCommands_ExposeGoalSlashCommands` (15→18 commands + 3 tokens in both
assert lists).
