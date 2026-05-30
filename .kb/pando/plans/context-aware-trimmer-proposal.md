# Proposal: Context-Aware Trimming for Self-Improvement Agent

**Date:** 2026-05-30  
**Status:** Proposal — not yet implemented  
**Priority:** Medium-High  
**Relates to:** `internal/evaluator/`, `internal/llm/prompt/`, `internal/llm/agent/`

---

## Problem Statement

The current self-improvement system operates **blindly with respect to the incoming task**:

1. **Skills are always fetched as `"general"`** — `builder.go:165` hardcodes `GetActiveSkills(ctx, "general")`, meaning task-specific skills (e.g. `debug` or `refactor`) are never used even though the DB can filter by `task_type`.

2. **All prompt sections are always included** — Every capability section (`capabilities/code_indexing`, `capabilities/web_search`, `capabilities/orchestration`, etc.) is added if the feature is enabled, regardless of whether the task needs it.

3. **Tool list is static and large** — `CoderAgentToolsWithMesnada()` can return 40–60 tools. All of them appear in every API call, consuming context window space even when a task only needs 5–10 tools (e.g. a pure "explain code" task needs View/Grep but not Bash/Browser/Mesnada).

4. **No pre-session task analysis** — The evaluator only acts post-session (scoring) and per-section (UCB selection). There is no mechanism to analyze the user's intent before building the prompt and tool list.

### Impact

- Wasted context window tokens (tool descriptions alone can cost 5–15k tokens)
- Skills injected may be irrelevant to the task (a `debug` skill injected for a `refactor` request)
- The LLM may be distracted by tools it cannot use for the current task

---

## Proposed Solution: ContextTrimmer

A new component `ContextTrimmer` that:
1. Analyzes the user's first message (task description)
2. Classifies the task type
3. Returns a `ContextProfile`: relevant tools, relevant skill types, sections to include
4. PromptBuilder and agent tool assembly use this profile to filter their outputs

---

## Implementation Phases

### Phase 0 — Fix Hardcoded "general" (Zero-cost, immediate)

**File:** `internal/llm/prompt/builder.go:165`

**Current:**
```go
if learnedSkills, err := b.evaluator.GetActiveSkills(ctx, "general"); err == nil
```

**Fix:** Detect task type from `b.data.UserRequest` (or equivalent first message field) using regex patterns, and pass the detected type to `GetActiveSkills`.

**Detection rules (regex on first user message):**
```
debug/fix    → contains: fix, bug, error, crash, broken, failing, exception, panic
refactor     → contains: refactor, rename, reorganize, clean up, extract, move
explain      → contains: explain, how does, what is, describe, understand, document
test         → contains: test, spec, coverage, assert, mock
write/create → contains: implement, create, add, write, build
search/find  → contains: find, search, where is, locate, grep for
general      → default fallback
```

**Changes required:**
- Add `classifyTaskType(text string) string` helper in `builder.go` or a new `internal/evaluator/classify.go`
- Expose via `PromptEvaluator` interface: `ClassifyTask(text string) string` (or just inline in builder)
- Pass `b.data.UserRequest` as input

**Risk:** Very low — only affects which skills are injected, skills injection is already best-effort.

---

### Phase 1 — Task Classification in EvaluatorService

**File:** `internal/evaluator/service.go` + `internal/evaluator/classify.go`

**New method:**
```go
// ClassifyTask returns a task type label from the user's first message.
// Uses compiled regex patterns from config, falls back to "general".
func (s *EvaluatorService) ClassifyTask(text string) string
```

**Config extension (`EvaluatorConfig`):**
```toml
[evaluator]
# task type classification patterns (regex → task_type)
taskPatterns = [
  { pattern = "fix|bug|error|crash|exception|failing", taskType = "debug" },
  { pattern = "refactor|rename|reorganize|clean|extract", taskType = "refactor" },
  { pattern = "explain|how does|what is|describe|understand", taskType = "explain" },
  { pattern = "test|spec|coverage|assert|mock", taskType = "test" },
]
```

**Expose in `PromptEvaluator` interface:**
```go
type PromptEvaluator interface {
    SelectTemplate(ctx context.Context, sectionName string) (*PromptEvaluatorTemplate, error)
    GetActiveSkills(ctx context.Context, taskType string) ([]PromptEvaluatorSkill, error)
    RecordTemplateSelection(ctx context.Context, sessionID, templateID string)
    ClassifyTask(text string) string  // NEW
}
```

**PromptBuilder integration:**
```go
// In Build(), before skills injection:
taskType := "general"
if b.evaluator != nil && b.data.UserRequest != "" {
    taskType = b.evaluator.ClassifyTask(b.data.UserRequest)
}
// ...
b.evaluator.GetActiveSkills(ctx, taskType)
```

---

### Phase 2 — LLM-Based ContextProfile (Pre-session Advisor)

**New file:** `internal/evaluator/context_trimmer.go`

**Purpose:** Use a cheap LLM call at session start to produce a `ContextProfile` that lists which tools and prompt sections are needed for the task.

**New type:**
```go
// ContextProfile is produced by the ContextTrimmer at session start.
// It guides the PromptBuilder and agent tool assembly to include only
// what is relevant for the current task.
type ContextProfile struct {
    TaskType             string   // "code", "debug", "refactor", "explain", "test", "general"
    RelevantToolNames    []string // tool names to keep (empty = keep all)
    SkipSections         []string // prompt section names to skip
    Confidence           float64  // 0.0-1.0; below 0.5 → ignore and use defaults
}
```

**ContextTrimmer struct:**
```go
type ContextTrimmer struct {
    judge *Judge  // reuses the same cheap model infrastructure
    cfg   EvaluatorConfig
}

func (ct *ContextTrimmer) ProfileTask(
    ctx context.Context,
    firstMessage string,
    availableTools []tools.ToolInfo,
    skills []Skill,
) (*ContextProfile, error)
```

**Trimmer prompt template:**
```
You are a task router for an AI coding assistant.
Analyze the user's first message and the available tools.
Return ONLY a JSON object (no markdown):
{
  "task_type": "one of: code|debug|refactor|explain|test|search|general",
  "relevant_tool_names": ["tool1", "tool2"],  // ONLY tools needed for this task
  "skip_sections": ["capabilities/web_search"],  // sections to omit from system prompt
  "confidence": 0.0-1.0
}

Always include these core tools regardless of task: bash, edit, view, glob, grep, write.

Available tools:
{{range .Tools}}- {{.Name}}: {{.Description}}
{{end}}

User's first message:
{{.FirstMessage}}
```

**Caching:** Hash `firstMessage + toolNames` → cache `ContextProfile` for identical inputs (within a session). Prevents re-calling LLM if the same task is retried.

**Latency mitigation:** The LLM call runs concurrently with other session init work. If it times out (>3s), fall back to defaults (all tools, all sections).

---

### Phase 3 — Tool List Trimming in Agent

This is the highest-value phase but requires changes to the agent's tool assembly path.

**Integration point:** `internal/llm/agent/agent.go` — where tools are passed to the provider.

**Mechanism:** Store `ContextProfile` in the session context. Agent reads it before building the tool list for each API call.

**Context key (new, in `internal/evaluator/types.go`):**
```go
const ContextProfileKey contextKey = "context_profile"
```

**In session init (e.g. `internal/session/session.go` or `app.go`):**
```go
// At session start, after first message arrives:
if trimmer != nil && firstMsg != "" {
    profile, err := trimmer.ProfileTask(ctx, firstMsg, allToolInfos, activeSkills)
    if err == nil && profile != nil && profile.Confidence >= 0.5 {
        ctx = context.WithValue(ctx, evaluator.ContextProfileKey, profile)
    }
}
```

**In agent tool assembly:**
```go
// In agent.go or tools.go, when building tool list for API call:
toolList := allTools
if profile, ok := ctx.Value(evaluator.ContextProfileKey).(*evaluator.ContextProfile); ok {
    toolList = filterTools(allTools, profile.RelevantToolNames)
}
```

**Safety constraint:** `filterTools` always keeps the "core safe set":
```go
var alwaysInclude = map[string]bool{
    "bash": true, "edit": true, "view": true,
    "glob": true, "grep": true, "write": true,
    "patch": true, "ls": true,
}
```

---

### Phase 4 — Skill Pruning (Skill Library Trim)

The judge's `new_skill` saves rules to the DB, but over time the skill library can fill with redundant or conflicting rules. The trimmer should also:

1. **Deduplicate skills** — When inserting a new skill, check semantic similarity against existing ones. If similarity > 0.85, update the existing skill instead of inserting.
2. **Prune low-confidence skills** — Periodically (`MaxSkills` enforcement already exists) deactivate skills with `success_rate < 0.3` AND `usage_count > 10`.
3. **Task-type affinity** — Improve skill extraction: ask the judge to assign `task_type` more precisely (currently inferred from `out.TaskType` which can be vague).

**Implementation:** Extend `saveSkillFromJudge` in `service.go` with semantic dedup check via remembrances embedding service.

---

## Key Files to Modify

| File | Change |
|---|---|
| `internal/evaluator/types.go` | Add `ContextProfile`, `ContextProfileKey` |
| `internal/evaluator/classify.go` | New: regex-based task classifier |
| `internal/evaluator/context_trimmer.go` | New: LLM-based ContextTrimmer |
| `internal/evaluator/service.go` | Add `ClassifyTask()`, `NewContextTrimmer()` |
| `internal/llm/prompt/builder.go` | Use task type for skills, use ContextProfile for sections |
| `internal/llm/prompt/prompt.go` | Extend `PromptEvaluator` interface with `ClassifyTask` |
| `internal/llm/agent/tools.go` | Add `FilterToolsByProfile(tools, profile)` function |
| `internal/llm/agent/agent.go` | Read ContextProfile from ctx, filter tool list |
| `internal/session/session.go` | Store ContextProfile in context at session start |
| `internal/app/app.go` | Create ContextTrimmer, wire into session |
| `internal/config/config.go` | Add `TaskPatterns` to `EvaluatorConfig` |

---

## Expected Benefits

| Metric | Current | With ContextTrimmer |
|---|---|---|
| Tool descriptions in context | 40–60 tools × avg 200 tokens = 8–12k tokens | 10–20 tools × 200 = 2–4k tokens |
| Skills injected | Always "general" skills | Task-relevant skills only |
| Prompt sections | All enabled capabilities always | Only sections relevant to task |
| Context window saved | baseline | ~6–10k tokens per session |

---

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| Over-filtering removes needed tool | High | Always include core safe set; fall back if confidence < 0.5 |
| LLM call adds latency at session start | Medium | Timeout 3s, async, cached by task hash |
| Regex classifier misclassifies task | Low | Fallback to "general" if no pattern matches |
| Import cycles | Medium | Use same adapter pattern as current evaluator (mirror types, adapters in app.go) |
| Judge prompt for trimming too verbose | Low | Keep prompt short; only list tool names + 1-line descriptions |

---

## Recommended Implementation Order

1. **Phase 0** — Fix hardcoded `"general"` in builder.go (1 file, ~10 lines, zero risk)
2. **Phase 1** — Add `ClassifyTask` with regex patterns to `EvaluatorService` (1 new file + interface change)
3. **Phase 2** — `ContextTrimmer` LLM-based profiler (new file, reuses Judge infrastructure)
4. **Phase 3** — Tool list filtering in agent (requires careful integration with agent.go)
5. **Phase 4** — Skill library dedup/pruning (requires remembrances embedding integration)

---

## Connection to Current Architecture

The judge prompt (`judge.go`) already evaluates 6 quality dimensions post-session. The ContextTrimmer is the **pre-session complement**: instead of "what went wrong?", it asks "what is needed?".

Both use the same cheap LLM model infrastructure (`Judge` struct with `provider.Provider`). The `ContextTrimmer` can reuse `Judge`'s `newJudge(cfg)` factory and add its own prompt rendering.

The `PromptEvaluator` interface (in `builder.go`) already has the right seam — extending it with `ClassifyTask` is minimal and backwards-compatible.
