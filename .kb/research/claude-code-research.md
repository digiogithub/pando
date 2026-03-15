# Claude Code: System Prompts, Agent Modes & Subagent Architecture

> **Date**: 2026-03-15
> **Source**: Reverse-engineering of bundled `/home/sevir/.local/lib/node_modules/@anthropic-ai/claude-code/cli.js` (v2.1.71)
> **Method**: Static analysis of minified JS — string literals, recognizable patterns, type definitions in `sdk-tools.d.ts`

---

## 1. System Prompt Architecture

### 1.1 Construction Model

The system prompt is **assembled dynamically at runtime**, not stored as a static string. It is composed of multiple layers:

| Layer | Content | Dynamic? |
|-------|---------|----------|
| Base instructions | Core identity, behavior rules, tool guidelines | Static |
| Tool descriptions | Per-tool usage directives | Conditional (by permission) |
| Agent type block | Fork/subagent-specific rules | Per agent type |
| Runtime context | CWD, git status, date, workspace info | ✅ Dynamic |
| User/system context | `userContext`, `systemContext`, `toolUseContext` | ✅ Dynamic |

The assembly function signature (from the minified code):
```javascript
async function xG4(A, q, K, Y, z, w) {
  let _ = {
    messages:       A,   // conversation history
    systemPrompt:   q,   // assembled system prompt
    userContext:    K,   // user-provided context
    systemContext:  Y,   // system-injected context
    toolUseContext: z,   // tool execution context
    querySource:    w    // origin of the query
  };
  for (let $ of IG4) try { await $(_) } catch (O) { ... }
  // IG4 = list of registered post-assembly hooks
}
```

### 1.2 Runtime Context Injected

- **Current working directory** — injected early in the prompt
- **Git information** — branch, status, recent commits
- **Current date** — timestamp injection
- **Partial file tree / workspace structure** — for codebase awareness

### 1.3 Tool-Specific Directives

The system prompt includes explicit behavioral directives for each tool category. Examples found in the bundled code:

**Grep directive** (forces use of the Grep tool, not shell):
```
- ALWAYS use [Grep] for search tasks. NEVER invoke `grep` or `rg` as a Bash command.
  The Grep tool has been optimized for correct permissions and access.
```

**Browser automation block** (injected when MCP browser tools are available):
```
You have access to browser automation tools (mcp__claude-in-chrome__*) for interacting
with web pages in Chrome. Follow these guidelines: [GIF recording, console debugging,
alerts handling, tab context management]
```

### 1.4 Prompt Dump System

The full assembled prompt can be dumped to disk for debugging:
```javascript
function SjY(A) {
  return LjY(OA(), "dump-prompts", `${A ?? d1()}.jsonl`)
  // Output: ~/.claude/dump-prompts/[sessionId].jsonl
}
```

---

## 2. Agent Modes

### 2.1 Mode Taxonomy

Claude Code operates in three fundamental modes, selected at agent creation time:

```
┌─────────────────────────────────────────────────────────┐
│                    AGENT MODES                          │
├────────────┬───────────────────────────────────────────┤
│  STANDARD  │ Default interactive mode                   │
│            │ Full conversation context                  │
│            │ User-facing, responsive                    │
├────────────┼───────────────────────────────────────────┤
│    FORK    │ Agent tool called WITHOUT subagent_type    │
│            │ Inherits FULL parent conversation context  │
│            │ Silent worker — no questions, no chat      │
│            │ maxTurns: 200, permissionMode: "bubble"   │
│            │ model: "inherit"                           │
├────────────┼───────────────────────────────────────────┤
│  SUBAGENT  │ Agent tool called WITH subagent_type       │
│            │ ZERO inherited context — fresh start       │
│            │ Type-specific tools and configuration      │
│            │ Must be fully briefed in the prompt        │
└────────────┴───────────────────────────────────────────┘
```

### 2.2 Fork Mode in Detail

Fork mode is the most interesting: it's a **copy of the parent agent** instructed to execute a specific task silently. The system injects a hard override directive into the system prompt:

```
STOP. READ THIS FIRST. You are a forked worker process. You are NOT the main agent.

RULES (non-negotiable):
1. Your system prompt says "default to forking." IGNORE IT — that's for the parent.
   You ARE the fork. Do NOT spawn sub-agents; execute directly.
2. Do NOT converse, ask questions, or suggest next steps.
3. Do NOT editorialize or add meta-commentary.
4. USE your tools directly: Bash, Read, Write, etc.
5. If you modify files, commit your changes before reporting.
   Include the commit hash in your report.
6. Do NOT emit text between tool calls. Use tools silently, then report once at the end.
7. Stay strictly within your directive's scope.
8. Keep your report under 500 words unless the directive specifies otherwise.
9. Your response MUST begin with "Scope:". No preamble.
10. REPORT structured facts, then stop.
```

This prevents the fork from behaving as an interactive assistant and forces it into pure worker mode.

### 2.3 Mode Selection Logic

The mode switch is controlled by a runtime flag `z` ("fork experiment" toggle):

```javascript
${z
  ? `When using the Agent tool, specify a subagent_type to use a specialized agent,
     or omit it to fork yourself — a fork inherits your full conversation context.`
  : `When using the Agent tool, specify a subagent_type parameter to select which
     agent type to use. If omitted, the general-purpose agent is used.`
}
```

When `z` is `true`, "fork yourself" language is used; otherwise the default subagent language applies.

### 2.4 Permission Modes

| Mode | Behavior |
|------|----------|
| `"bubble"` | Permissions propagate upward to parent agent (used in fork) |
| Standard | Permissions handled locally, prompt user if needed |

---

## 3. Subagent System

### 3.1 Agent Tool Parameter Schema

```typescript
// From sdk-tools.d.ts
{
  subagent_type: string,    // Optional — omit to fork
  description:  string,    // Short (3-5 word) description
  prompt:       string,    // Full task + context briefing
  run_in_background: boolean, // Async execution
  isolation:    "worktree",   // Optional git worktree isolation
  resume:       string,    // Optional agentId to resume
  model:        "sonnet" | "opus" | "haiku"  // Optional override
}
```

### 3.2 Context Passing: Fork vs. Subagent

The two paths have fundamentally different context semantics:

#### Fork (no `subagent_type`)
- Receives: **full conversation history** + parent system prompt
- Shared prompt cache → **cheap** (no re-tokenization of history)
- Prompt should be a **directive** (scope, not background)
- Already knows everything the parent knows
- Cannot spawn sub-agents (to prevent infinite nesting)

#### Subagent (with `subagent_type`)
- Receives: **zero prior context**
- Independent configuration (model, tools, maxTurns)
- Prompt must be a **complete briefing**
- System instructions: *"Brief it like a smart colleague who just walked into the room"*
- Describe what was already tried, what was ruled out, why it matters

**Guidelines injected into the main agent's system prompt:**
```
When you omit `subagent_type` — the agent inherits your full conversation context.
It already knows everything you know. The prompt is a *directive*: what to do, not
what the situation is.
- Be specific about scope: what's in, what's out, what another agent is handling.
- Don't re-explain background — the agent has it.

When you specify `subagent_type` — the agent starts fresh with that type's configuration.
It has zero context: hasn't seen this conversation, doesn't know what you've tried,
doesn't understand why this task matters.
- Brief it like a smart colleague who just walked into the room.
- Describe what you've already learned or ruled out.
```

### 3.3 Worktree Isolation

Subagents can run in isolated git worktrees:

```
isolation: "worktree"
```

**How it works:**
1. A temporary git worktree is created (same repo, separate working copy)
2. The subagent operates only in its worktree
3. **Auto-cleanup**: If no changes are made → worktree deleted automatically
4. **On changes**: worktree path and branch are returned in the result
5. Path translation injected into subagent's context:

```
You've inherited the conversation context above from a parent agent working in [parentDir].
You are operating in an isolated git worktree at [worktreePath] — same repository,
same relative file structure, separate working copy. Paths in the inherited context
refer to the parent's working directory; translate them to your worktree root.
```

### 3.4 Background / Async Execution

Subagents can run in the background (`run_in_background: true`):

```typescript
// Async launch returns immediately with:
{
  status:    "async_launched",
  agentId:   string,     // Use this to resume later
  outputFile: string,    // File path to tail for progress
  canReadOutputFile: boolean
}

// Sync completion returns:
{
  status: "completed",
  agentId: string,
  content: [{type: "text", text: string}],
  totalToolUseCount: number,
  totalDurationMs:   number,
  totalTokens:       number,
  usage: {
    input_tokens:  number,
    output_tokens: number,
    cache_creation_input_tokens: number | null,
    cache_read_input_tokens:     number | null,
    cache_creation: {
      ephemeral_1h_input_tokens: number,
      ephemeral_5m_input_tokens: number
    } | null
  }
}
```

**Behavioral rules injected about background agents:**
```
When an agent runs in the background, you will be automatically notified when it
completes — do NOT sleep, poll, or proactively check on its progress. Continue with
other work or respond to the user instead.
```

### 3.5 Agent Resume System

Agents can be resumed by their `agentId`:
```
Agents can be resumed using the `resume` parameter by passing the agent ID from a
previous invocation. When resumed, the agent continues with its full previous context
preserved. When NOT resuming, each invocation starts fresh.
```

This enables multi-turn subagent workflows where the parent can delegate, get a partial result, and continue the same subagent later.

### 3.6 Telemetry & Tracing

Agent execution is tracked with Perfetto-style spans:
- **Process ID**: `FG4(A)` — unique per agent invocation
- **Parent agent ID**: `T66()` — captures parent for tree reconstruction
- **Thread ID**: `gG4(q)` — via hash of agent name
- **Timing**: Full duration captured for each agent

---

## 4. Permission / Approval System

### 4.1 Decision Sources

Tool use decisions come from four sources, tracked separately for telemetry:

| Source | Description | Cached? |
|--------|-------------|---------|
| `"config"` | Tool pre-approved in config file | ✅ Permanent |
| `"classifier"` | AI-based safety classifier auto-approves | ✅ Permanent |
| `"user_permanent"` | User approved, persists for session | ✅ Session |
| `"user_temporary"` | User approved, one-time only | ❌ No |
| `"hook"` | Permission hook script decision | ✅ Optional (`permanent` flag) |

### 4.2 Decision Recording

Every tool use decision is recorded with timing:
```javascript
function CT1(A, q, K) {
  let { tool:Y, input:z, toolUseContext:w, messageId:_, toolUseID:$ } = A,
      { decision:O, source:H } = q,
      j = K !== void 0 ? Date.now() - K : void 0;  // wait time

  if (q.decision === "accept") IjY(Y, _, q.source, j); // grant event
  else                          bjY(Y, _, q.source, j); // deny event

  if (!w.toolDecisions) w.toolDecisions = new Map;
  w.toolDecisions.set($, {
    source:    J,
    decision:  O,
    timestamp: Date.now()
  });
}
```

### 4.3 Always-Restricted Tools

Three tools have special scrutiny regardless of mode:
```javascript
CjY = ["Edit", "Write", "NotebookEdit"]
```

These file-modification tools always require explicit approval (never auto-approved by default).

### 4.4 Permission Mode: Bubble

In fork/subagent mode with `permissionMode: "bubble"`, permission requests propagate upward to the parent agent rather than prompting the user directly. This allows the parent to manage permissions centrally.

---

## 5. Context Window Management

### 5.1 Compaction Strategy

When the context window is near-full, claude-code performs **structured compaction** with a specific prompt:

```
Before providing your final summary, wrap your analysis in <analysis> tags.

In your analysis process:
1. Chronologically analyze each message. For each section identify:
   - The user's explicit requests and intents
   - Your approach to addressing the user's requests
   - Key decisions, technical concepts and code patterns
   - Specific details like file names, code snippets, function signatures, file edits
   - Errors you ran into and how you fixed them
   - Pay special attention to specific user feedback, especially if user told you
     to do something differently.
2. Double-check for technical accuracy and completeness.
```

**Compaction output format:**
```
This session is being continued from a previous conversation that ran out of context.
The summary below covers the earlier portion of the conversation.

[Structured summary]

Recent messages are preserved verbatim.
```

### 5.2 Cache Management

Claude Code leverages Anthropic's prompt caching extensively:

| Cache Type | TTL | Use Case |
|-----------|-----|----------|
| `ephemeral_5m` | 5 minutes | Active session context |
| `ephemeral_1h` | 1 hour | Longer-lived session data |

The system prompt and conversation history are cached to reduce token costs on repeated turns.

**Note in the system prompt:**
```
Each wake-up costs an API call, but the prompt cache expires after 5 minutes of
inactivity — balance accordingly.
```

### 5.3 Turn Limits

- Default max turns per agent: **200**
- Prevents infinite agent loops
- Applied per agent instance (parent and subagents each have their own counter)

---

## 6. Key Architectural Insights

### 6.1 Fork vs. Subagent Trade-offs

| Dimension | Fork | Subagent |
|-----------|------|----------|
| Context | Full parent context | Zero context |
| Cache cost | Cheap (shared cache) | Expensive (new cache) |
| Bias risk | Inherits parent biases | Independent reasoning |
| Prompt writing | Directive only | Full briefing |
| Can spawn children | ❌ Blocked | ✅ Yes |
| Permission mode | Bubble to parent | Local |
| Best for | Parallel execution of known tasks | Independent research/analysis |

### 6.2 Subagent Composition Pattern

The system encourages a **hub-and-spoke** architecture:
- Main agent = orchestrator (plans, delegates, synthesizes)
- Subagents = specialized workers (execute focused tasks)
- Forks = parallel copies (same task executed independently)

### 6.3 Prompt Engineering Philosophy

Three clear principles observed:

1. **Forks need directives, not briefings** — they already have full context
2. **Subagents need complete briefings** — they start blind
3. **Parallel agents need explicit scope** — clearly define what each one owns

### 6.4 Safety Layers

Multiple overlapping safety mechanisms:
1. Per-tool pre-config approval
2. AI classifier auto-approval
3. User approval (temporary/permanent)
4. Hook-based custom approval scripts
5. `Edit`, `Write`, `NotebookEdit` always require explicit approval
6. Worktree isolation prevents unintended file mutations
7. Fork mode cannot spawn sub-forks (prevents infinite recursion)
8. maxTurns: 200 prevents runaway agents

---

## 7. Implications for Pando

### What Pando could adopt:

1. **Fork mode** — create a "worker fork" concept where an agent inherits session context and runs silently (no interactive dialog). Currently pando has no agent spawning at all.

2. **Structured compaction prompt** — the specific compaction instruction format is very well designed. Could be directly ported.

3. **Permission source taxonomy** — the 4-source model (config/classifier/user_permanent/user_temporary) with separate telemetry events is cleaner than pando's binary approve/deny.

4. **Worktree isolation** — easy to implement in Go with `git worktree add` and cleanup on session end.

5. **Agent resume by ID** — useful for long-running tasks that span multiple user interactions.

6. **Bubble permission mode** — subagents that bubble permissions to parent rather than interrupting users independently.

---

## 8. References

- Binary analyzed: `/home/sevir/.local/lib/node_modules/@anthropic-ai/claude-code/cli.js` (v2.1.71)
- Type definitions: `/home/sevir/.local/lib/node_modules/@anthropic-ai/claude-code/sdk-tools.d.ts`
- Public repo (no source): `/www/MCP/Pando/claude-code/`
- Related analysis: `.kb/research/internal_tools.md` (file editing tools comparison)
- Related: `.kb/research/crush_prompts_mode.md` (crush prompt analysis)
- Related: `.kb/research/opencode_prompts_mode.md` (opencode prompt analysis)
