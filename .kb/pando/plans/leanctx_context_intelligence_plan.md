---
created_at: 2026-06-30T07:37:50.709088797Z
updated_at: 2026-06-30T07:56:19.724066172Z
tags:
    - plan
    - lean-ctx
    - token-optimization
    - context-intelligence
    - read-modes
    - property-graph
    - analytics
    - config-ui
---
# Implementation Plan: lean-ctx Context-Intelligence Features in Pando

**Date:** 2026-06-30 (updated 2026-06-30 with config/UI + view-reuse feedback)
**Author:** Claude (Opus 4.8)
**Goal:** Port the genuinely *distinctive*, high-ROI ideas from lean-ctx
(https://github.com/yvgude/lean-ctx) into Pando — the ones that go **beyond** the
already-shipped RTK shell-output compression — to push token reduction past the
`bash` boundary and into file reads, the code index, and session continuity.

## Context & Prior Work

- **Already shipped (RTK-style):** `internal/llm/tools/outputfilter/` — declarative
  TOML 8-stage pipeline that compresses `bash` command output (git/build/test/lint)
  60-90%, fail-safe, exit-code preserving, on by default. Phases 1,2,4,5,6 done
  (analytics phase 3 was skipped). Toggle: `Bash.OutputFilterDisabled`. See
  `pando/features/output_filters.md`.
- **Already shipped (`view` tool optimizations) — MUST be preserved, not regressed:**
  `internal/llm/tools/view.go` already returns **line numbers** (`addLineNumbers`),
  **paginated reads** via `offset`/`limit`/`end_line`, **streaming reads** for large
  files (`readTextFileStreaming`), **long-line truncation** (`MaxLineLength` 2000),
  a **2000-line default window** (`DefaultReadLimit`), and a **continuation hint**
  ("File has more lines. Use 'offset' …"). These are real token optimizations and the
  new read-modes are **strictly additive on top of them** (see Phase 1).
- **Prior reference analyses:** `pando/analysis/lean-ctx-deep-analysis.md`,
  `pando/analysis/rtk-deep-analysis.md`, and the (now partly superseded) combined
  sketch `pando/analysis/rtk-leanctx-integration-plan.md`. **This document replaces
  the lean-ctx-specific portions of that sketch.**
- **DeepWiki study (2026-06-30)** of `yvgude/lean-ctx`: multi-mode `ctx_read` with an
  `auto` resolver (`resolve_auto_mode` / Context Gate), content-hash session cache
  emitting ~13-token "F-references", SQLite property graph (`graph.db`) with weighted
  BFS for impact/related, append-only savings ledger (`ledger.jsonl`, `o200k_base`
  tokenizer), an adaptive layer (ModePredictor + Thompson bandit + bounce tracker),
  and deterministic transcript compaction.

### What Pando already has (so we DON'T rebuild it)

| lean-ctx capability | Pando equivalent today | Verdict |
|---|---|---|
| Shell output compression | `outputfilter/` (RTK port) | **Done** |
| Paginated reads + line numbers + streaming + truncation | `view.go` (`offset/limit/end_line`, `addLineNumbers`, streaming, `MaxLineLength`) | **Reuse + build on** |
| tree-sitter AST, 18 langs | `internal/rag/treesitter` (~17 langs) | **Reuse** |
| Per-symbol signature + byte offsets | `treesitter.CodeSymbol{Signature, StartByte, EndByte, NamePath, ParentID}` | **Reuse** |
| Contentless index, hydrate from disk | `internal/rag/code` (FTS5 contentless + offset hydrate) | **Reuse** |
| Session cache + pagination | `internal/llm/tools/cache.go`, `cache_read`, `cache_stats` | **Extend** |
| Cross-session memory | memory system + KB | **Reuse** |
| Realtime token counter | `AgentEventTypeTokenUsage` live counter | **Reuse** |

### The genuine gaps lean-ctx exposes

1. **No semantic compression of file reads.** `view` paginates and numbers lines but
   always returns *raw bytes* of the window — no "signatures-only" / "map" mode and no
   `auto` mode that picks fidelity by size/type/task. This is lean-ctx's biggest win
   and Pando already owns the AST that makes it cheap. **Goal: add a higher
   optimization tier on top of the existing pagination, never replace it.**
2. **Re-reads cost full tokens.** Re-reading the same unchanged window pays full price
   each time; lean-ctx returns a ~13-token F-reference (or a diff on change).
3. **No relationship graph** (imports/calls/exports edges) → no impact analysis,
   related-files hint, or repomap.
4. **No token-savings visibility** (RTK analytics phase was skipped).
5. **No prompt-cache-safe transcript compaction / compact CCP session resume.**

## Design Principles (Pando house style)

- **Additive, never regressive.** The new read-modes compose *with* the existing
  `offset`/`limit`/`end_line` pagination, line numbers, streaming and truncation —
  they add an optional higher compression tier, they do not remove or weaken any
  current behavior. `full` stays the default and behaves exactly as today.
- **Reuse the AST we already have.** Extend `internal/rag/treesitter` +
  `internal/rag/code`; no new tree-sitter dependency.
- **Fail-safe & opt-out, like the RTK filter.** Any compression path falls back to the
  current raw paginated read on error and **never emits more tokens than that raw read**
  (hard cap, mirroring lean-ctx `safeguard_ratio`).
- **Default-off for behaviour-changing phases**, default-on only for strictly
  additive/cheap ones.
- **Deterministic first.** Adaptive/bandit learning is opt-in (`*Learning` flags),
  never breaking reproducibility.
- **Single configuration home.** All knobs live under one new `[TokenOptimization]`
  config namespace and one new **"Token Optimization"** settings section in TUI/WebUI
  (+ 7-locale i18n) — see "Configuration & UI" below.

---

## Phase 1 — Smart file-read modes on `view`, **on top of** existing pagination (HIGH)

**Goal:** Add a higher optimization tier — `signatures` / `map` / `auto` — that cuts
50-90% of tokens on large source files, **while keeping every current `view`
optimization** (line numbers, `offset`/`limit`/`end_line` windowing, streaming,
long-line truncation, continuation hint).

**Reuse of existing `view` improvements (explicit, non-negotiable):**
- New `mode` is an **optional** `ViewParams` field; absent ⇒ `full` ⇒ **byte-identical
  to today's output** (same line numbers, same pagination, same hints).
- The modes **compose with** `offset`/`limit`/`end_line`: e.g. `signatures` of the
  current window only, or `map` of a 2000-line page. The default-window/streaming
  path (`readFileContent` / `readTextFileStreaming`) is reused unchanged to obtain the
  bytes before semantic rendering.
- **Line numbers are preserved in every mode:** each emitted signature / outline entry
  carries its real source line (from `CodeSymbol.StartLine`) so the agent can
  immediately follow up with `offset`/`limit` to pull the full body — i.e. the new
  modes *feed* the existing pagination instead of bypassing it.
- `lines:N-M` is simply the **existing** `offset`/`limit` path surfaced as a named mode
  (no new mechanism) — it remains the canonical "I know exactly where to look" read.

**New modes**
- `signatures`: render `CodeSymbol.Signature` + `NamePath` + docstring + line number,
  grouped by parent — pulled from the index (no re-parse), or parsed on the fly via the
  existing extractor for un-indexed files.
- `map`: imports/exports + top-level signatures (structural outline) with line numbers;
  for md/json/yaml/toml use the existing structural extractors.
- `auto`: deterministic resolver (`internal/llm/tools/readmode/resolver.go`) mirroring
  lean-ctx `resolve_inner`: instruction files (`AGENTS.md/CLAUDE.md/*.cursorrules`),
  binary, diagnostic-active, task-named, small (≤~200 tok), and config/data files →
  `full`; otherwise medium/large code → `map` then `signatures` by size.

**Hard safeguards:** compressed ≥ raw-window ⇒ return the raw paginated window; any
parse error ⇒ raw paginated window; always append the escape-hatch footer reusing the
existing hint style (`use mode=full` / `offset=<line>`).

**Files:** `view.go` (add `mode`, keep all current paths), new
`internal/llm/tools/readmode/` (resolver + renderers), reuse `internal/rag/treesitter`
+ `internal/rag/code`.

**Config:** `[TokenOptimization] ReadModeDefault = "full"` (default `full` ⇒ no change;
`auto` opt-in), env `PANDO_READ_MODE_DEFAULT`.

**Verify:** `mode` absent ⇒ output byte-identical to current `view` (regression test);
signatures/map fixtures (Go + TS) keep correct line numbers; safeguard test (tiny file
→ full); compose test (`signatures` + `offset/limit`); `go test ./internal/llm/tools/...
./internal/rag/...`.

**Risk:** medium (agent may need bytes it didn't get) — mitigated by line-numbered
entries + footer + `full` default + Phase 3 bounce tracker.

---

## Phase 2 — Content-hash F-references for unchanged re-reads (HIGH, cheap)

**Goal:** A second `view` of an unchanged window collapses to a ~1-line stub instead of
re-sending content — additive on top of the existing session cache.

**What to build**
- On every `view`, hash the bytes (keyed by normalized abs path + window) and store
  `{hash, lastMode, tokenEst, label}` in `cache.go`.
- Re-read, unchanged + already delivered this session ⇒
  `[unchanged: <path> lines a-b — see earlier read F<n>]` (~10-15 tok). Changed ⇒
  compact unified diff (`readmode/diff.go`). Stable short labels `F1, F2, …`.
- Pagination-aware: dedup is per delivered window, so `offset`/`limit` reads still work
  and only collapse when the same-or-superset window was already returned.

**Files:** `cache.go` (+content-hash field), `view.go` (check-before-emit),
`readmode/diff.go`.

**Config:** `[TokenOptimization] ReadDedupDisabled = false` (on by default — additive
and safe; honors existing cache-disable knobs).

**Verify:** read-twice ⇒ stub; modify-then-read ⇒ diff; `cache_stats` shows dedup hit
(mirrors lean-ctx `verify-cache`).

**Risk:** low — the stub always references an earlier in-context read.

---

## Phase 3 — Adaptive auto-mode safety: bounce tracker (MEDIUM)

**Goal:** Make `auto` safe to enable by default by learning when compression backfires.

- `BounceTracker` (`readmode/bounce.go`): a "bounce" = compressed `view` of a path
  immediately followed by a `full` view of it. Per-extension + per-path counters in the
  session store (optionally persisted per project); high-bounce paths/exts upgrade
  `signatures→map→full` on the next auto read; bounce waste is subtracted from reported
  savings.
- Optional `ModePredictor`-style learning (Thompson/Beta over modes) behind
  `[TokenOptimization] ReadModeLearning = false` — deterministic by default.

**Verify:** simulate compressed→full ⇒ next auto read of that ext upgraded; `-race` on
the shared tracker.

**Risk:** medium; isolated behind the `auto` opt-in.

---

## Phase 4 — Code property graph: impact & related-file intelligence (MEDIUM-HIGH)

**Goal:** Turn the symbol index into a relationship graph: "what breaks if I change X",
"what relates to this file", and a cheap `[related: …]` read hint.

- Extend per-language extractors to emit **edges** (`imports`, `calls`, `defines`,
  `exports`, `type_ref`) during indexing — start Go + TS/JS, then Python/Rust. Store in
  a new `edges` table in the existing code-index SQLite DB.
- New tools: `code_impact_analysis(symbol|file)` (reverse-edge BFS) and
  `code_related_files(file)` (weighted BFS: `imports`=1.0, `calls`=0.8, `type_ref`=0.5).
- Optional token-bounded `[related: …]` footer on `view`/`code_hybrid_search`.
- Stretch: `code_repomap` via personalized PageRank, budget-fitted to a token cap.

**Files:** `internal/rag/treesitter/*_extractor.go`, `internal/rag/code/graph.go`
(+migration), `internal/llm/tools/remembrances_code.go`, MCP registration.

**Config:** `[TokenOptimization] BuildCodeGraph = true` (additive; incremental via
`git diff --name-only`). UI: graph/index controls surface in the **existing
"KB & Code Index"** TUI section, cross-linked from the Token Optimization section.

**Verify:** fixture project; impact set + related scores; incremental update;
`go test ./internal/rag/... ./internal/llm/tools`.

**Risk:** medium; ship per-language, never block indexing on edge-extraction failure.

---

## Phase 5 — Token-savings analytics ledger + `pando gain` (MEDIUM)

**Goal:** Make everything measurable (filter + read-modes + dedup) — closes the skipped
RTK analytics gap.

- Append-only JSONL ledger (`<data-dir>/savings/ledger.jsonl`):
  `{ts, source(view|bash|search), command/path, baseline_tokens, actual_tokens, mode,
  saved}`; estimate via Pando's existing token counter (consistent tokenizer).
- Emit points: `outputfilter` apply, `view` read-modes, F-reference hits.
- `pando gain`/`pando stats` CLI (summary, per-source, est. USD, daily rollup) +
  `pando_stats` MCP tool + a savings widget in the **Token Optimization** settings
  section.

**Config:** `[TokenOptimization] SavingsLedgerDisabled = false` (on; bounded/rotated).

**Risk:** low; observational.

---

## Phase 6 — (Optional / advanced) Transcript compaction, session brief, budget guard

- **6a.** Deterministic transcript compaction (`ctx_transcript_compact` analogue):
  system preamble + fresh tail verbatim, summarize old turns, offload raw turns to
  session memory; **byte-stable** (prompt-cache safe), **never split tool_call/result**.
- **6b.** CCP-style ~400-token structured "session brief" (task/findings/decisions/
  files_touched) injected on cold start — a compact projection over memory + KB.
- **6c.** Budget guard: cumulative tool-output token meter with warn/block thresholds,
  integrated with the live token counter.

Each behind its own default-off flag under `[TokenOptimization]`. Strictly optional.

---

## Configuration & UI: unified "Token Optimization" section (NEW, cross-cutting)

Rationale: these knobs (plus the existing RTK toggle) are one domain — context/token
optimization. Scattering them across `Bash`/`Tools`/`CodeIndex` would bloat existing
panels. Pando's settings are already organized as discrete sections (WebUI:
`*Settings.tsx` registered in `SettingsView.tsx`; TUI: `Section`s grouped via
`withGroup(..., "Group")`), so a dedicated section is the established pattern.

**config.go** — new top-level `TokenOptimizationConfig` struct (TOML
`[TokenOptimization]`) holding the **new** knobs:
`ReadModeDefault` ("full"), `ReadDedupDisabled` (false), `ReadModeLearning` (false),
`BuildCodeGraph` (true), `RelatedFilesHint` (bool), `SavingsLedgerDisabled` (false),
plus Phase-6 flags. **Do NOT move `Bash.OutputFilterDisabled`** (would break existing
configs) — instead the UI section *surfaces* it (a settings panel is a view and may
read/write fields from any config struct).

**WebUI** — new `web-ui/src/components/settings/TokenOptimizationSettings.tsx`, imported
+ nav-registered in `SettingsView.tsx`. Sub-sections (using `subSectionTitle` like
`MesnadaSettings`):
- *Shell output (RTK)* — **enable/disable the RTK output-filter proxy**
  (`Bash.OutputFilterDisabled`, inverted to a friendly "Enable output compression"
  toggle) + extra filter paths (`Bash.OutputFilterPaths`).
- *File reads* — `ReadModeDefault` (full/auto/signatures/map), `ReadDedupDisabled`,
  `ReadModeLearning`.
- *Code graph* — `BuildCodeGraph`, `RelatedFilesHint` (cross-link to KB & Code Index).
- *Savings* — `SavingsLedgerDisabled` + the `pando gain` savings widget (Phase 5).

**TUI** — new `buildTokenOptimizationSection(cfg)` in `internal/tui/page/settings.go`,
`withGroup(..., "Token Optimization")` (or under "Services"); same fields including the
**RTK enable/disable** toggle. Code-graph/index controls remain in the existing
"KB & Code Index" section, cross-referenced.

**i18n** — new `tokenOptimization.*` namespace in all 7 locales.

**Verify:** toggling RTK from the new section flips `Bash.OutputFilterDisabled` and
hot-reloads the filter engine; `ReadModeDefault=auto` activates Phase-1 auto; round-trip
persists in TOML; tsc + `go build ./...`.

---

## Explicitly deferred / not porting (and why)

| lean-ctx feature | Decision | Reason |
|---|---|---|
| Token Dense Dialect (λ§∂τε) | Skip for now | Risk of confusing the model; revisit as opt-in renderer on Phase-1 signatures. |
| LITM-aware reordering (P-tiers) | Defer | High complexity, unclear ROI; reconsider after Phase-5 measurement. |
| Lean4 formal verification | Skip | Research-grade; off Pando's Go stack. |
| RBAC roles / policy / PathJail / DLP | Out of scope here | Security governance, not token reduction; possible separate epic. |
| 77+ MCP tools, HTTP Context-OS, cloud sync, editor exts | Skip | Product breadth, not the token-intelligence core. |

## Suggested sequencing & ROI

1. **Phase 1 + Phase 2** — highest ROI, reuse AST + existing `view` pagination/cache,
   low risk. Ship the **Token Optimization** config section alongside them.
2. **Phase 5** — make savings visible (validates 1+2, closes RTK gap).
3. **Phase 3** — unlock a safe `auto` default.
4. **Phase 4** — graph intelligence (independent value).
5. **Phase 6** — optional polish.

## Expected impact of P1 + P2 (set expectations)

- `signatures`/`map` on a large source file: ~75-90% fewer tokens **on that read** (a
  ~6-8k-token `full` Go file → ~0.8-1.5k), while still giving line numbers to jump back
  to full via existing pagination.
- Exploration-heavy sessions: ~40-70% fewer file-read tokens (auto only compresses when
  safe; small/config/instruction/diagnostic files stay full).
- Unchanged re-reads: thousands of tokens → ~15-token stub (P2, on by default).
- Net: larger effective context window, fewer compactions/less context loss, lower cost,
  no correctness loss (line-numbered entries + footer + hard cap guarantee recovery).
- Caveat: the big *automatic* gain lands once `auto` is enabled (made safe by P3); P2 is
  the immediate safe win.

## Success criteria

- P1: `mode` absent ⇒ byte-identical to today's `view`; `signatures`/`map` ≥50% fewer
  tokens than `full`, never exceeds the raw window, line numbers preserved.
- P2: unchanged re-read collapses to <20-token stub (`cache_stats` confirms).
- P3: a compressed→full bounce upgrades the next auto read of that extension.
- P4: `code_impact_analysis` + `code_related_files` work for Go and TS fixtures.
- P5: `pando gain` reports cumulative tokens saved across filter + read-modes.
- Config: new Token Optimization section in TUI/WebUI toggles RTK + all new knobs,
  persists to `[TokenOptimization]`, 7-locale i18n.
