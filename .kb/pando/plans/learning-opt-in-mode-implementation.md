---
created_at: 2026-07-15T13:50:12.750307183Z
updated_at: 2026-07-15T13:50:12.750307183Z
tags:
    - plan
    - learning
    - slash-commands
    - prompt-injection
    - kb
    - memory
    - architecture
---
# Learning Opt-in Session Mode — Implementation Plan

## STATUS: COMPLETE (Phases 0-6, 2026-07-15) — see [[learning_mode]]

## Objective
Add an internal, opt-in Pando session mode enabled by `/learning` and disabled by
`/learning-finish`, mirroring the architecture of Superpowers, Caveman and Ponytail
([[superpowers-opt-in-mode-implementation]], [[caveman-persistent-output-brevity-mode]],
[[ponytail_mode_implementation_plan]]). When active, a per-turn prompt harness makes the
agent behave as a **deliberate learner and documentarian**:

1. **Read the KB first, always.** Lean much harder on `kb_search_documents` /
   `hybrid_search_remembrances` before acting, and on the code-index tools for structure.
2. **Document what it discovers.** Persist non-trivial findings, decisions and analyses as
   living KB documents with `kb_add_document`, wiki-linked into the graph.
3. **Use the memory system actively.** `recall` relevant facts before working; `remember`
   short, durable, key-identified facts that are NOT trivially re-derivable from code or the
   current project state.
4. **Be curious and ask.** Prefer asking the user via the **`AskUserQuestion`** tool (Pando's
   user-feedback tool) whenever a decision, preference or unstated requirement matters, rather
   than guessing.
5. **Keep documentation honest.** Actively update stale KB docs it encounters and mark
   superseded ones as outdated/obsolete.

This mode must not change normal Pando behavior when off, must be session-scoped, ephemeral
(process-bounded), concurrency-safe, and must compose cleanly with the other session policies.

## Research and decisions (verified against HEAD, 2026-07-15)

### Reference architecture (the pattern we copy)
The Superpowers mode is the exact template. Confirmed wiring at HEAD:
- **Core, dependency-free package** `internal/superpowers/superpowers.go`: `State` struct,
  `SetEnabled`/`Enabled` over a `sync.Map` keyed by normalized session ID (presence = active,
  no configured default), and pure string builders `Instructions()`, `FinishPrompt()`,
  `ActivationMessage(objective)`, plus `AlreadyActiveMessage` / `NotActiveMessage` constants.
  Imports only `strings` + `sync` — this is what breaks the import cycle
  (`superpowers → llm/tools → mesnada/acp → superpowers`). **Keep the learning core equally
  import-free.**
- **Agent session bridge** `internal/llm/agent/superpowers_session.go`: `SetSuperpowersMode` /
  `SuperpowersMode` delegate to the core; `superpowersEnabledForContext(ctx)` resolves the
  session id via the agent's existing `sessionIDFromContext`; `RunSuperpowersFinish` runs the
  closing turn as a normal `svc.Run` and clears the mode only on a successful terminal
  `AgentEventTypeResponse{Done, Error==nil}` event (cancel/error retains the mode).
- **Prompt composition** `internal/llm/agent/agent.go`:
  - `prepareProvider` fast path is gated by `sessionPolicyActive(ctx)` (agent.go:2202).
  - `sessionPolicyActive(ctx)` (agent.go:2227) ORs `ponytailModeForContext`,
    `superpowersEnabledForContext`, `cavemanModeForContext`.
  - `sessionPolicyInstructions(ctx)` (agent.go:2239) injects, in order:
    **superpowers → caveman → ponytail**, each via `prompt.InjectSkillInstructions(name, body)`.
- **Shared slash registry** `internal/commands/registry.go` (lines 31-32: `superpowers`
  AcceptsArgs=true, `superpowers-finish` AcceptsArgs=false).
- **ACP surface** `internal/mesnada/acp/`: `slash_commands.go` (kinds/tokens/specs/parser),
  `superpowers_commands.go` (`processSuperpowersCommand` synchronous, `processSuperpowersFinishCommand`
  streams a real turn), `types_interfaces.go` (`AgentService` gains `SetSuperpowersMode`,
  `SuperpowersMode`, `SuperpowersFinish`), and BOTH adapters implement it:
  `cmd/root.go:acpAgentAdapter` (~line 899) and `internal/app/app.go:appACPAgentAdapter` (~line 2375),
  each forwarding to `agent.RunSuperpowersFinish`.
- **Web UI surface** `internal/api/handlers_chat.go` (SSE dispatch cases `"superpowers"` /
  `"superpowers-finish"`, ~lines 861-876).
- **TUI surface** `internal/tui/page/chat.go` (`handleSuperpowersCommand`, dispatched ~line 856;
  activation is a synchronous toast, finish drains `RunSuperpowersFinish`).

### Decisions specific to Learning
- **Finish runs a real consolidation turn** (like superpowers-finish, unlike caveman-finish
  which is a synchronous toggle). Learning is about capturing knowledge, so `/learning-finish`
  should run one final agent turn that consolidates the session's learnings into KB/memory,
  flags any stale docs it touched, and summarizes — then clear the mode only on success. This
  reuses the `RunSuperpowersFinish` success-only pattern verbatim.
- **Ephemeral, session-scoped, no config default** in the first release (matches Superpowers).
  A durable default (`[Learning] DefaultMode`) and a status badge are deferred.
- **Composition order:** inject **learning right after superpowers**, before caveman/ponytail:
  order becomes `superpowers → learning → caveman → ponytail`. Rationale: superpowers and
  learning both govern *how work is approached*; caveman/ponytail govern *how the output is
  written / how much is built*.
- **Caveman coexistence caveat (must be stated in the harness):** Caveman constrains **chat
  prose** brevity; Learning demands **rich KB/memory documentation**. These target different
  surfaces and can coexist — the harness will explicitly say "brevity policies apply to chat
  replies, NOT to the depth of KB documents you write."
- **The user-feedback tool is `AskUserQuestion`** (`internal/llm/tools/ask_user_question.go`,
  `const AskUserQuestionToolName = "AskUserQuestion"`; multiSelect + free-text "Other"; can be
  disabled via `[InternalTools] AskUserQuestionDisabled`). The harness references it by name and
  degrades gracefully to a plain end-of-turn question if the tool is disabled.

### Gap found: no tool marks a KB doc "outdated"
`KBStore.MarkDocumentOutdated` (`internal/rag/kb/memory.go:328`) flips `outdated=1` in
`kb_documents`, updates the FS mirror frontmatter (`internal/rag/kb/frontmatter.go` `Outdated`
field) and is honored by `kb_search_documents`'s `exclude_outdated` (default true). **But it is
only ever called by the memory GC** (`internal/rag/kb/memory_gc.go:70`) — there is **no MCP tool**
exposing it. Today an agent can only `forget` / `kb_delete_document` (hard delete) or overwrite
via `kb_add_document`. Since the Learning harness explicitly instructs the agent to *mark obsolete*
(soft, reversible, preserves history), Phase 5 adds a thin `kb_mark_outdated` tool. Without it the
harness cannot honor that requirement with a non-destructive action.

## Behavioral contract

### `/learning [optional focus]`
- Synchronous, no LLM turn, idempotent. Enables the policy and returns a confirmation. Optional
  `focus` text is echoed in the confirmation only, not persisted.
- Re-invoking reports the mode is already active (`AlreadyActiveMessage`).

### Active mode (the harness, injected every turn)
The injected ruleset instructs the agent to:
1. **Recover context first** — run `kb_search_documents` (untagged) / `hybrid_search_remembrances`
   and `recall` before any task that builds on prior work; use the code-index tools for structure.
   Never guess an unstated requirement.
2. **Ask, don't assume** — when a preference, decision, or unstated requirement would change the
   outcome, ask the user through the `AskUserQuestion` tool. Batch related questions; keep them
   answerable.
3. **Capture discoveries** — persist non-trivial findings, analyses and decisions as
   `kb_add_document` documents under clear `file_path`s, wiki-linked (`[[...]]`) into the graph.
   Store short durable facts with `remember` under a stable key; never use `remember` for long
   content. Do NOT document what is trivially re-derivable from code or git history.
4. **Keep docs honest** — when you encounter a KB doc that is stale or contradicted by reality,
   update it in place; when it is superseded, mark it outdated with `kb_mark_outdated` (or delete
   with `kb_delete_document` only when truly junk). State what you changed and why.
5. **Precedence** — a direct user instruction, AGENTS.md/CLAUDE.md and the permission system
   outrank this policy. Do not over-question trivial read-only asks; do not block emergency fixes;
   state any deviation in one line.
6. **Coexistence** — brevity policies (Caveman/Ponytail) govern chat replies and how much code is
   built, NOT the depth of KB documents; keep documenting richly regardless.

### `/learning-finish`
- If inactive: reply `NotActiveMessage`, no turn.
- If active: run one final normal agent turn with a **consolidation prompt** that (a) reviews what
  was learned this session, (b) writes/updates the KB documents and memories that capture it,
  (c) flags/marks any stale docs, and (d) summarizes what was captured and what is still open —
  taking no destructive git action.
- Clear the mode only after the turn reaches a successful terminal result; cancel/error retains it.

## Phases

### Phase 0 — Architecture & acceptance spec (this document). Reserve built-in names `learning`, `learning-finish`.

### Phase 1 — Core package `internal/learning/learning.go` (+ `_test.go`)
Dependency-free (`strings` + `sync` only). Mirror `internal/superpowers`:
- `type State struct { Enabled bool }`, `var sessions sync.Map`.
- `SetEnabled(sessionID, enabled)`, `Enabled(sessionID)`, `normalizeSessionID`.
- `Instructions() string` — the learner/documentarian harness above.
- `FinishPrompt() string` — the consolidation prompt.
- `ActivationMessage(focus string) string`, `const AlreadyActiveMessage`, `const NotActiveMessage`.
- Tests: enable/disable/idempotency, presence semantics, concurrency, non-empty policy strings that
  name the required tools (`kb_search_documents`, `kb_add_document`, `remember`/`recall`,
  `AskUserQuestion`, `kb_mark_outdated`).
Exit: `go test ./internal/learning` green; package imports only `strings`+`sync`.

### Phase 2 — Agent bridge & prompt composition
- New `internal/llm/agent/learning_session.go`: `SetLearningMode`, `LearningMode`,
  `learningEnabledForContext(ctx)` (reuse `sessionIDFromContext`), `RunLearningFinish` (clone of
  `RunSuperpowersFinish`, success-only clear) + `ErrLearningNotActive`.
- Edit `agent.go`: add `learningEnabledForContext(ctx)` to `sessionPolicyActive`; in
  `sessionPolicyInstructions`, inject `prompt.InjectSkillInstructions("learning", learning.Instructions())`
  immediately after the superpowers block (order: superpowers → learning → caveman → ponytail).
- Tests `internal/llm/agent/learning_session_test.go`: injection when enabled, absent when disabled,
  clean-mode suppression, composition order incl. learning, `RunLearningFinish` inactive→error/no-run,
  success clears, cancel/error retains. Use a `fakeFinishService` embedding `Service` (as superpowers does).
Exit: `go test ./internal/llm/agent -run 'Learning|SessionPolicy'` green, `-race` clean.

### Phase 3 — Shared registry
- `internal/commands/registry.go`: add `{Name:"learning", AcceptsArgs:true}` and
  `{Name:"learning-finish", AcceptsArgs:false}` with descriptions. Update
  `internal/commands/registry_test.go` counts/assertions.

### Phase 4 — Slash commands on every surface (parity with superpowers)
- **ACP**: `slash_commands.go` (new kinds `slashCommandLearning`/`slashCommandLearningFinish`, tokens,
  specs with usage, parser cases); new `internal/mesnada/acp/learning_commands.go`
  (`processLearningCommand` synchronous, `processLearningFinishCommand` streams the finish turn,
  reporting "Close cancelled. Learning mode stays active." on `StopReasonCancelled`);
  `types_interfaces.go` `AgentService` gains `SetLearningMode`/`LearningMode`/`LearningFinish`;
  implement in BOTH adapters (`cmd/root.go`, `internal/app/app.go`) via `agent.RunLearningFinish`.
  Update `agent_pando_test.go` advertised-command count (11 → 13) and new tokens; add
  `learning_commands_test.go`.
- **Web UI**: `internal/api/handlers_chat.go` SSE cases `"learning"` / `"learning-finish"` mirroring
  the superpowers cases (activation synchronous; finish streams `RunLearningFinish` on a bg ctx).
- **TUI**: `internal/tui/page/chat.go` — `handleLearningCommand` (synchronous toast on activate; drain
  `RunLearningFinish` on finish; `ErrLearningNotActive` → NotActiveMessage), dispatched alongside
  `handleSuperpowersCommand`.

### Phase 5 — `kb_mark_outdated` MCP tool (closes the gap)
- Add a tool in `internal/llm/tools/remembrances_kb.go` (or a sibling) wrapping
  `KBStore.MarkDocumentOutdated(ctx, filePath)`: input `file_path` (required), returns confirmation;
  idempotent; usable on any KB doc, not only memory docs. Register it wherever the KB toolset is
  assembled. Add a test. The Learning harness references it by name; if you prefer to avoid a new
  tool, the fallback is documenting via `kb_delete_document`, but that is destructive and loses
  history — the soft flag is the right primitive and is already fully supported by the store + search.

### Phase 6 — Verification, docs, KB record
- `go build ./...`, `go vet`, targeted tests, `-race` on new code. Expect only the known pre-existing
  HEAD failures (`internal/mcpgateway` TOML tests; 2 `internal/llm/agent` data races) — see
  [[pando_repo_pitfalls]].
- README: add a **Learning** subsection under Built-in Slash Commands + a Features bullet.
- On completion, write `pando/features/learning_mode.md` and per-phase `pando/changes/learning-*.md`,
  and update this plan's STATUS to COMPLETE.

## Risks & mitigations
- **Over-questioning** annoys users → harness explicitly scopes `AskUserQuestion` to decisions that
  change outcomes; never for trivial/read-only asks; batch questions.
- **Doc spam / duplication** → harness says update-in-place over new docs, skip trivially-derivable
  content, and wiki-link; rely on `kb_search_documents` before adding.
- **Caveman/Learning tension** → explicit coexistence clause (brevity = chat surface only).
- **Prompt bloat** → keep the harness compact; inject only for enabled sessions (fast-path gated).
- **Destructive "obsolete" action** → provide the soft `kb_mark_outdated` instead of forcing delete.
- **Duplicate command registries** → Phase 4 adds parity tests (registry + ACP advertised count).

## Deferred follow-ups
- `[Learning] DefaultMode` durable default + status badge/UI in TUI/Web.
- i18n for messages; settings-panel toggle.
- Structured "learning log" session artifact and metrics (docs written, questions asked, docs marked outdated).
- Unify the native + ACP slash-command specs (shared debt across all modes).