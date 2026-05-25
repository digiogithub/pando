# Goal/Autopilot Mode Implementation Plan for Pando

## Objective

Implement a "Goal" (or "Autopilot") mode in Pando that enables an agent to work autonomously toward a persistent objective across multiple turns until completion, similar to `/goal` in Claude Code and Codex CLI. This mode should be available in all surfaces: TUI, ACP (editors), and WebUI.

---

## Research: How Other Agents Implement Goal/Autopilot Mode

### Claude Code `/goal`

Key characteristics from analysis of Claude Code documentation and user reports:

- `/goal <objective>` defines a **persistent completion condition**, not just a single-prompt task.
- The agent continues working **multi-turn without user intervention**.
- Core mechanics:
  1. Maintain a **persistent objective state** across turns.
  2. **Iterate internally**: execute an action, evaluate result, decide next step.
  3. **Verify completion**: check if the goal condition is satisfied.
  4. **Continue** until the goal is achieved or genuinely blocked.
- Uses built-in plan tracking (`TodoWrite` equivalent).
- Auto-approves tool calls during goal execution (no permission prompts per tool).
- Can be interrupted with `Ctrl+C`; state is preserved.
- Separated conceptually from "auto-approval" — goal is about **autonomy + completion criteria**, auto-approval is just **no permission prompts**.

### OpenAI Codex CLI Goal Mode

From OpenAI documentation and experiments:

- A Goal is **bigger than a single prompt but smaller than a full project**.
- Includes structured metadata:
  - What should be true when finished.
  - How success should be verified.
  - Constraints that must hold.
- Codex keeps working across turns until confident the goal is reached.
- Interruption-friendly: `Ctrl+C` preserves Goal state, resumes when re-entering TUI.
- Can run for hours without user input.
- Supports "watchdogs" and execution limits.

### ACP Ecosystem & Protocol Compatibility

The Agent Client Protocol (ACP) already supports:

- **Session Modes**: extensible mode system (currently `agent`, `ask` in Pando).
- **Slash Commands**: protocol-level support for agent-side slash commands.
- **Agent Plan**: `TodoWrite` tool calls with plan entries streamed to clients.
- **Session Updates**: real-time `session/update` notifications for state changes.
- **Tool Calls**: full streaming tool call lifecycle (start, update, result).

Multiple agents support ACP, including Claude Code, Codex CLI, Gemini CLI, and Cursor, all of which have some form of autonomous goal-driven behavior.

---

## Current State Analysis of Pando

### What Pando Already Has (Foundation for Goal Mode)

#### 1. Behavior Autonomous Base

Location: `internal/llm/agent/agent.go`

- `globalNonInteractive bool` — global flag for autonomous execution.
- `SetNonInteractiveMode(enabled bool)` — configures agents to run without user input.
- `nonInteractiveInstructions` — system prompt instructions for autonomous behavior:
  - Complete tasks autonomously without asking for clarification.
  - Make reasonable assumptions; document them.
  - Never pause, prompt, or wait for user input.
  - Stop only for destructive actions requiring explicit confirmation.
  - Produce a concise summary when done.

This is a **very strong foundation**. It already tells the agent to be autonomous. What's missing is the **goal persistence and loop logic**.

#### 2. ACP Session Modes

Location: `internal/mesnada/acp/session_state.go`

- `defaultACPMode = "agent"`
- `askModeID = "ask"`
- `agentModeID = "agent"`
- `availableModes()` — returns available modes to ACP clients.
- `buildSessionModeState()` — constructs mode state for ACP responses.
- `buildSessionConfigOptions()` — includes mode selector in config options.

Location: `internal/mesnada/acp/agent.go`

- `SetSessionMode()` — handles mode changes per session.
- `validateModeID()` — validates mode against `agent` and `ask`.
- Prompt processing checks `mode` and applies `askModeInstruction` for "ask" mode.
- `sendCurrentModeUpdate()` — sends mode change to ACP client.
- `sendSessionConfigOptionsUpdate()` — sends config options to ACP client.

This is the **exact extension point** for goal mode.

#### 3. TUI Plan/Progress Rendering

Location: `internal/tui/components/chat/sidebar.go`

- Sidebar renders a `todosSection()` showing plan items.
- `todos []tools.TodoItem` — stores latest plan entries.
- Updates on `TodosUpdatedMsg` messages.

Location: `internal/tui/components/chat/chat.go`

- `TodosUpdatedMsg` type — dispatched when TodoWrite tool updates plan.

Location: `internal/llm/agent/agent.go`

- `AgentEventTypeTodosUpdated` — emitted on TodoWrite tool success.

#### 4. API/SSE Plan and Tool Streaming

Location: `internal/api/handlers_chat.go`

- `AgentEventTypeTodosUpdated` case → emits `todos_update` SSE event.
- `writeSSETodoWritePlan()` → emits `plan_update` SSE event from TodoWrite input.
- Full streaming of `tool_call`, `tool_result`, `content_delta` events.

#### 5. ACP Plan Rendering

Location: `internal/mesnada/acp/prompt_handler.go`

- Parses `TodoWrite` in streaming path, emits `UpdatePlan` to ACP clients.
- History replay also reconstructs full plan from persisted tool calls.

Location: `internal/mesnada/acp/agent.go`

- `processPromptWithAgent()` — streaming event loop for ACP.

### What Pando Is Missing for Goal Mode

#### Core Gaps

1. **No `goal` session mode** — only `agent` and `ask` exist.
2. **No persistent goal state per session** — goals aren't saved to DB or memory.
3. **No goal execution loop** — agent runs once per prompt, then stops.
4. **No completion/termination criteria evaluation** — no way to know if goal is done.
5. **No execution limits** — no max iterations, duration, or cost controls.
6. **No slash command parsing** — `/goal` text is sent directly to the model.
7. **No goal-specific events** — SSE/ACP don't emit goal state changes.

---

## Design Recommendations

### 1. Introduce a New Session Mode: `goal`

The cleanest design is to treat goal/autopilot as a **persistent session mode**, not just a prompt prefix. This aligns with ACP protocol conventions and Pando's existing mode system.

#### ACP Layer

File: `internal/mesnada/acp/session_state.go`

Add constants and register the mode:

```go
const (
    goalModeID = "goal"
    // ... alongside existing agentModeID, askModeID
)
```

Update `availableModes()` to include goal mode.

Update `buildSessionModeState()` — already works generically with current mode ID.

Update `buildSessionConfigOptions()` — add `goal` to the mode selector values.

Update `validateModeID()` — accept `goalModeID` as valid.

Update `SetSessionMode()` in `internal/mesnada/acp/agent.go` — handle goal mode:

```go
case goalModeID:
    acpSession.SetMode(goalModeID)
    if !acpSession.PermissionConfigured() {
        acpSession.SetAskPermission(false) // default: auto-approve in goal mode
    }
```

#### TUI Layer

When the user activates goal mode:

- Session switches to `goal` mode.
- Next prompt becomes the persistent objective.
- Sidebar shows "GOAL" badge with objective text and status.

#### WebUI / ACP (Slash Command)

Use `/goal <objective>` as the primary activation mechanism:

- If input starts with `/goal `:
  - Switch session mode to `goal`.
  - Persist the objective.
  - Start the goal execution loop.
- If input is just `/goal`:
  - Show help/status of current goal.
- Alias: `/autopilot <objective>` does the same as `/goal`.

### 2. Persistent Goal State per Session

#### Database Schema Change

Create a separate `session_goals` table (more scalable):

```sql
CREATE TABLE IF NOT EXISTS session_goals (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    objective TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'running',
    iteration INTEGER NOT NULL DEFAULT 0,
    max_iterations INTEGER NOT NULL DEFAULT 20,
    max_duration_seconds INTEGER NOT NULL DEFAULT 3600,
    started_at INTEGER NOT NULL,
    completed_at INTEGER,
    last_progress TEXT,
    next_step TEXT,
    blocked_reason TEXT,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_session_goals_session_id ON session_goals(session_id);
```

#### Go Model

File: `internal/db/models.go`

```go
// SessionGoal represents a persisted goal/autopilot objective.
type SessionGoal struct {
    ID                 string        `json:"id"`
    SessionID          string        `json:"session_id"`
    Objective          string        `json:"objective"`
    Status             string        `json:"status"`
    Iteration          int64         `json:"iteration"`
    MaxIterations      int64         `json:"max_iterations"`
    MaxDurationSeconds int64         `json:"max_duration_seconds"`
    StartedAt          int64         `json:"started_at"`
    CompletedAt        sql.NullInt64 `json:"completed_at"`
    LastProgress       sql.NullString `json:"last_progress"`
    NextStep           sql.NullString `json:"next_step"`
    BlockedReason      sql.NullString `json:"blocked_reason"`
    CreatedAt          int64         `json:"created_at"`
}
```

#### DB Migration

File: `internal/db/migrations/20260525000001_add_session_goals.sql`

Follow existing goose migration pattern from `internal/db/migrations/`.

#### SQL Queries

File: `internal/db/sql/session_goals.sql`

```sql
-- name: InsertSessionGoal :one
INSERT INTO session_goals (id, session_id, objective, status, max_iterations, max_duration_seconds, started_at)
VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: GetSessionGoal :one
SELECT * FROM session_goals WHERE session_id = ? AND status = 'running' LIMIT 1;

-- name: UpdateSessionGoalStatus :exec
UPDATE session_goals SET status = ?, iteration = ?,
    completed_at = ?, last_progress = ?, next_step = ?, blocked_reason = ?
WHERE id = ?;

-- name: IncrementSessionGoalIteration :exec
UPDATE session_goals SET iteration = iteration + 1 WHERE id = ?;

-- name: ListSessionGoals :many
SELECT * FROM session_goals WHERE session_id = ? ORDER BY created_at DESC;

-- name: DeleteSessionGoal :exec
DELETE FROM session_goals WHERE id = ?;
```

Generate with sqlc alongside existing `sessions.sql.go`, `messages.sql.go`, etc.

#### In-Memory Goal State

File: `internal/mesnada/acp/session.go`

Add `goal` field to `ACPServerSession`:

```go
type ACPServerSession struct {
    // ... existing fields ...
    goal *GoalState
}

type GoalStatus string

const (
    GoalStatusIdle     GoalStatus = "idle"
    GoalStatusRunning  GoalStatus = "running"
    GoalStatusCompleted GoalStatus = "completed"
    GoalStatusBlocked  GoalStatus = "blocked"
    GoalStatusCancelled GoalStatus = "cancelled"
    GoalStatusTimedOut GoalStatus = "timed_out"
    GoalStatusFailed   GoalStatus = "failed"
)

type GoalState struct {
    Objective       string
    Status          GoalStatus
    Iteration       int
    StartedAt       time.Time
    CompletedAt     time.Time
    LastProgress    string
    NextStep        string
    BlockedReason   string
    MaxIterations   int
    MaxDuration     time.Duration
}
```

Add methods to `ACPServerSession`:

- `SetGoal(objective string, maxIterations int, maxDuration time.Duration)` — starts a new goal.
- `Goal() *GoalState` — returns current goal state.
- `HasActiveGoal() bool` — returns true when goal status is `running`.
- `UpdateGoalProgress(progress, nextStep string)` — updates progress fields.
- `CompleteGoal(reason string)` — marks goal as completed.
- `BlockGoal(reason string)` — marks goal as blocked.
- `CancelGoal()` — marks goal as cancelled.
- `CanContinue() bool` — checks if execution limits are still within bounds.
- `IncrementIteration() int` — increments and returns new iteration count.

### 3. Create GoalRunner (Autopilot Execution Loop)

This is the **core** of the feature. It wraps the existing agent `Run()` method to create an autonomous multi-turn loop.

#### Location

**New file**: `internal/llm/agent/goal_runner.go`

#### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        GoalRunner                            │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  Start(objective) ───► Run Loop:                            │
│                          │                                   │
│                          ▼                                   │
│                    ┌───────────┐                             │
│                    │ Check     │───── completed ───► Stop    │
│                    │ Complete? │───── blocked ──────► Stop   │
│                    └─────┬─────┘───── limits ───────► Stop   │
│                          │ continue                        │
│                          ▼                                   │
│                    ┌───────────────┐                        │
│                    │ Build prompt  │                        │
│                    │ (iteration N) │                        │
│                    └───────┬───────┘                        │
│                          │                                   │
│                          ▼                                   │
│                    ┌───────────────┐                        │
│                    │ agent.Run()   │── emit events ──► UI   │
│                    │ (single turn) │                        │
│                    └───────┬───────┘                        │
│                          │                                   │
│                          ▼                                   │
│                    ┌───────────────┐                        │
│                    │ Evaluate      │                        │
│                    │ result        │                        │
│                    └───────┬───────┘                        │
│                          │                                   │
│                          └─────────────────────────────────┘
```

#### Implementation Strategy

**Do NOT rewrite the provider or agent loop.** Instead, wrap the existing `agent.Run()`.

Each iteration of GoalRunner:

1. **Build Prompt**:
   - **Iteration 1**: Full objective + `goalModeInstruction` (see below).
   - **Iteration N+1**: Continuation prompt referencing existing conversation history + objective.

   Continuation prompt example:
   ```text
   Continue working toward the active goal.

   Current goal:
   <objective>

   Current iteration: N

   Before acting:
   1. Briefly assess whether the goal is already complete.
   2. If not complete, pick the highest-priority next action.
   3. Update the plan with TodoWrite if the plan changed.
   4. Execute the next action.
   5. Verify progress.

   Do not ask the user for input unless you are genuinely blocked.
   ```

2. **Run Agent Turn**:
   ```go
   eventChan, err := runner.agent.Run(ctx, sessionID, promptText)
   ```

3. **Stream Events**:
   - All events (deltas, tool calls, results, todos) are emitted to the existing event channel.
   - ACP clients and WebUI receive real-time updates via existing paths.

4. **Evaluate Result** (at end of each turn):
   - Check last message content for completion indicator.
   - Use heuristic: phrases like "goal completed", "done", "implemented and verified".
   - Optional: a structured evaluation call (see Phase 3).

5. **Check Limits**:
   - Max iterations reached?
   - Max duration exceeded?
   - No progress for N consecutive iterations?

6. **Decision**:
   - If complete → stop, update goal status to `completed`.
   - If blocked → stop, update goal status to `blocked` + reason.
   - If limits hit → stop, update goal status to `timed_out` or `max_iterations`.
   - If still in progress → increment iteration and continue loop.

#### GoalRunner Key Interface

```go
type GoalRunner struct {
    agent       AgentService          // Pando agent (agent.Run method)
    goalStore   GoalStore             // DB persistence
    cancelFunc  context.CancelFunc    // For user cancellation
}

// Run starts the goal execution loop. It blocks until the goal completes or fails.
// Returns the final goal status and a summary string.
func (gr *GoalRunner) Run(ctx context.Context, sessionID string, objective string, opts GoalOptions) (<-chan AgentEvent, error)

type GoalOptions struct {
    MaxIterations int           // default 20
    MaxDuration   time.Duration // default 1h
    InitialPrompt string        // additional initial instructions
}
```

The `Run` method returns an event channel that multiplexes the agent event channels from each turn. This allows ACP `processPromptWithAgent()` and TUI/WebUI handlers to consume a single channel.

#### Goal Mode System Prompt

Add a `goalModeInstruction` that is prepended to the objective:

```text
You are in Goal mode.
You are working toward a persistent objective and must continue autonomously until the goal is completed or you are genuinely blocked.

Objectives you MUST follow:
- Always maintain and update a plan with TodoWrite.
- After each meaningful action, evaluate whether the goal is complete.
- If incomplete, continue with the next concrete step without waiting for user input.
- Keep your work scoped to the stated objective; do not add unrelated features or changes.
- Use tools (read files, search, write files, run commands, browser) as needed.
- Verify your changes work correctly before declaring completion.

Only stop when:
- The goal is satisfied and you have verified it works.
- You are blocked by missing information, credentials, or permissions that only the user can provide.
- You encounter a critical error that prevents further progress.

When complete, declare: "GOAL COMPLETED: <brief summary of what was achieved>."
When blocked, declare: "GOAL BLOCKED: <what is needed to continue>."
```

---

## Implementation Phases

### Phase 1: Core Infrastructure (Foundation)

**Estimated effort**: 3-4 days  
**Dependencies**: None  
**Files to modify/create**:

| File | Action | Description |
|------|--------|-------------|
| `internal/db/migrations/20260601000001_add_session_goals.sql` | Create | DB migration for `session_goals` table |
| `internal/db/sql/session_goals.sql` | Create | SQL queries for goal persistence |
| `internal/db/session_goals.sql.go` | Generate | sqlc-generated code |
| `internal/db/models.go` | Modify | Add `SessionGoal` struct |
| `internal/llm/agent/goal_state.go` | Create | Goal state types and constants |
| `internal/llm/agent/goal_runner.go` | Create | Core GoalRunner implementation |
| `internal/llm/agent/goal_prompts.go` | Create | Goal mode system prompts |

**Deliverables**:
1. ✅ Database schema and migrations for `session_goals`
2. ✅ `SessionGoal` model and sqlc queries
3. ✅ `GoalState` type with status constants
4. ✅ `GoalRunner` struct with basic Run loop
5. ✅ Goal mode system prompts (initial and continuation)

**Acceptance Criteria**:
- Goal can be created and persisted to DB
- GoalRunner can execute a single iteration
- Goal state transitions work correctly
- Unit tests pass for goal state machine

---

### Phase 2: ACP Integration (Protocol Layer)

**Estimated effort**: 2-3 days  
**Dependencies**: Phase 1  
**Files to modify**:

| File | Action | Description |
|------|--------|-------------|
| `internal/mesnada/acp/session_state.go` | Modify | Add `goalModeID` constant, update `availableModes()`, `validateModeID()` |
| `internal/mesnada/acp/session.go` | Modify | Add `goal *GoalState` field, goal methods |
| `internal/mesnada/acp/agent.go` | Modify | Handle `/goal` command, integrate GoalRunner, update `SetSessionMode()` |
| `internal/mesnada/acp/prompt_handler.go` | Modify | Emit goal state events during streaming |
| `internal/mesnada/acp/slash_commands.go` | Create | Parse `/goal`, `/autopilot`, `/goal-status`, `/goal-cancel` |

**Deliverables**:
1. ✅ `goal` mode registered in ACP mode system
2. ✅ Slash command parsing for `/goal <objective>`
3. ✅ GoalRunner integration with `processPromptWithAgent()`
4. ✅ Goal state notifications via ACP `session/update`
5. ✅ Goal cancellation via `/goal-cancel` or `Ctrl+C`

**New ACP Session Update Fields**:

```go
type GoalStateUpdate struct {
    Objective     string `json:"objective"`
    Status        string `json:"status"`
    Iteration     int    `json:"iteration"`
    MaxIterations int    `json:"max_iterations"`
    Progress      string `json:"progress,omitempty"`
    NextStep      string `json:"next_step,omitempty"`
    ElapsedTime   string `json:"elapsed_time,omitempty"`
}
```

Emitted in `session/update` as:

```json
{
  "type": "session/update",
  "sessionUpdate": {
    "goal": {
      "objective": "Implement user authentication",
      "status": "running",
      "iteration": 3,
      "maxIterations": 20,
      "progress": "Created login form",
      "nextStep": "Add form validation",
      "elapsedTime": "5m32s"
    }
  }
}
```

**Acceptance Criteria**:
- `/goal <objective>` starts goal execution in ACP clients
- Goal status visible in editor sidebar
- Iteration progress updates in real-time
- Goal can be cancelled with `/goal-cancel`
- Works with Zed, VSCode, and other ACP clients

---

### Phase 3: TUI Integration

**Estimated effort**: 2 days  
**Dependencies**: Phase 1, Phase 2  
**Files to modify**:

| File | Action | Description |
|------|--------|-------------|
| `internal/tui/components/chat/chat.go` | Modify | Handle GoalStartMsg, GoalUpdateMsg, GoalCompleteMsg |
| `internal/tui/components/chat/sidebar.go` | Modify | Add goal status section, show objective + progress |
| `internal/tui/components/chat/input.go` | Modify | Parse `/goal` command, disable input during goal execution |
| `internal/tui/components/chat/messages.go` | Modify | Add goal-specific message types |
| `internal/tui/page/chat/chat.go` | Modify | Integrate GoalRunner, handle Ctrl+C cancellation |

**TUI Messages**:

```go
type GoalStartMsg struct {
    Objective     string
    MaxIterations int
}

type GoalUpdateMsg struct {
    Iteration    int
    Progress     string
    NextStep     string
}

type GoalCompleteMsg struct {
    Status  GoalStatus
    Summary string
}
```

**Sidebar Goal Section**:

```
┌─────────────────────────────┐
│ 🎯 GOAL (iteration 3/20)    │
├─────────────────────────────┤
│ Objective:                  │
│ Implement user auth...      │
│                             │
│ Progress:                   │
│ ✓ Created login form        │
│ → Adding validation         │
│                             │
│ Elapsed: 5m32s              │
│ Press Ctrl+C to cancel      │
└─────────────────────────────┘
```

**Acceptance Criteria**:
- `/goal <objective>` in TUI starts goal mode
- Sidebar shows goal status with live updates
- Input is disabled during goal execution (shows "Goal in progress...")
- Ctrl+C cancels goal and returns to normal mode
- Goal completion/failure shows appropriate message

---

### Phase 4: WebUI/API Integration

**Estimated effort**: 1-2 days  
**Dependencies**: Phase 1, Phase 2  
**Files to modify**:

| File | Action | Description |
|------|--------|-------------|
| `internal/api/handlers_chat.go` | Modify | Handle GoalRunner events, emit goal SSE events |
| `internal/api/webui/src/components/Chat.tsx` | Modify | Render goal status, handle goal SSE events |
| `internal/api/webui/src/components/GoalStatus.tsx` | Create | Goal status component |
| `internal/api/webui/src/hooks/useGoal.ts` | Create | Goal state management hook |

**SSE Events**:

```typescript
// goal_start
{
  "type": "goal_start",
  "objective": "Implement user authentication",
  "maxIterations": 20
}

// goal_update
{
  "type": "goal_update",
  "iteration": 3,
  "progress": "Created login form",
  "nextStep": "Add form validation"
}

// goal_complete
{
  "type": "goal_complete",
  "status": "completed",
  "summary": "User authentication implemented with login/logout flows"
}
```

**Acceptance Criteria**:
- `/goal <objective>` in WebUI starts goal mode
- Goal status displayed in chat interface
- Real-time progress updates via SSE
- Cancel button stops goal execution

---

### Phase 5: Completion Detection & Heuristics

**Estimated effort**: 2 days  
**Dependencies**: Phase 1  
**Files to create/modify**:

| File | Action | Description |
|------|--------|-------------|
| `internal/llm/agent/goal_evaluator.go` | Create | Completion detection logic |

**Completion Heuristics**:

1. **Explicit Declaration**: Agent message contains "GOAL COMPLETED" or "GOAL BLOCKED"
2. **Pattern Matching**: Phrases like "successfully implemented", "all tests pass", "verified working"
3. **TodoWrite State**: All plan items marked as completed
4. **No Progress**: Same state for N consecutive iterations (stall detection)

**GoalEvaluator Interface**:

```go
type GoalEvaluator interface {
    // Evaluate checks if the goal is complete or blocked based on the last turn.
    Evaluate(ctx context.Context, goal *GoalState, lastMessage string, todos []TodoItem) (EvaluationResult, error)
}

type EvaluationResult struct {
    Complete     bool
    Blocked      bool
    Reason       string
    Progress     string
    NextStep     string
    Confidence   float64
}
```

**Future Enhancement** (Phase 5b - Optional):

Structured evaluation via separate LLM call:

```go
func (e *LLMGoalEvaluator) Evaluate(ctx context.Context, goal *GoalState, history []Message) (EvaluationResult, error) {
    prompt := fmt.Sprintf(`Given the following goal and conversation history, evaluate:
1. Is the goal complete? (yes/no)
2. Is the agent blocked? (yes/no)
3. What progress has been made?
4. What is the recommended next step?

Goal: %s

History:
%s

Respond in JSON format.`, goal.Objective, formatHistory(history))
    
    // Use a smaller/cheaper model for evaluation
    resp, err := e.evaluator.Complete(ctx, prompt)
    // Parse JSON response...
}
```

**Acceptance Criteria**:
- Explicit completion declarations detected correctly
- Stall detection triggers after N iterations with no progress
- Blocked state detected from agent messages
- False positive rate < 5% in test cases

---

### Phase 6: Execution Limits & Safety

**Estimated effort**: 1 day  
**Dependencies**: Phase 1  
**Files to modify**:

| File | Action | Description |
|------|--------|-------------|
| `internal/llm/agent/goal_runner.go` | Modify | Add limit checks |
| `internal/config/config.go` | Modify | Add goal mode configuration |

**Configuration Options**:

```toml
[Goal]
# Maximum iterations before automatic stop
MaxIterations = 20

# Maximum duration before timeout
MaxDuration = "1h"

# Cost limit (in tokens or dollars)
MaxCost = 1000000  # tokens

# Stall detection threshold
StallIterations = 3

# Auto-approve tool calls (default: true in goal mode)
AutoApprove = true

# Dangerous commands that require confirmation even in goal mode
DangerousPatterns = [
    "rm -rf",
    "DROP TABLE",
    "git push --force",
]
```

**Safety Checks**:

```go
func (gr *GoalRunner) checkLimits(goal *GoalState) error {
    // Iteration limit
    if goal.Iteration >= goal.MaxIterations {
        return ErrMaxIterationsReached
    }
    
    // Duration limit
    if time.Since(goal.StartedAt) > goal.MaxDuration {
        return ErrMaxDurationExceeded
    }
    
    // Cost limit (if implemented)
    if gr.costTracker != nil && gr.costTracker.TotalCost() > gr.config.MaxCost {
        return ErrMaxCostExceeded
    }
    
    return nil
}
```

**Acceptance Criteria**:
- Goals stop at max iterations with clear message
- Goals timeout after max duration
- Dangerous commands still require confirmation
- Configuration options work correctly

---

### Phase 7: CLI Flag & Non-Interactive Mode

**Estimated effort**: 1 day  
**Dependencies**: Phase 1  
**Files to modify**:

| File | Action | Description |
|------|--------|-------------|
| `cmd/pando/cmd_run.go` | Modify | Add `--goal` flag |
| `internal/llm/agent/agent.go` | Modify | Integrate goal mode with non-interactive |

**CLI Usage**:

```bash
# Start pando in goal mode
pando run --goal "Implement user authentication with JWT tokens"

# With options
pando run --goal "Fix all linting errors" --max-iterations 10 --max-duration 30m

# Equivalent using prompt file
pando run --goal-file ./goal.txt
```

**Implementation**:

```go
var runCmd = &cobra.Command{
    Use:   "run",
    Short: "Run Pando agent",
    RunE: func(cmd *cobra.Command, args []string) error {
        goal, _ := cmd.Flags().GetString("goal")
        if goal != "" {
            // Enable non-interactive mode
            agent.SetNonInteractiveMode(true)
            
            // Create GoalRunner
            runner := agent.NewGoalRunner(agentService, goalStore)
            
            // Run goal
            eventChan, err := runner.Run(ctx, sessionID, goal, agent.GoalOptions{
                MaxIterations: maxIterations,
                MaxDuration:   maxDuration,
            })
            
            // Stream events to stdout
            for event := range eventChan {
                handleEvent(event)
            }
        }
        // ... existing run logic
    },
}
```

**Acceptance Criteria**:
- `--goal` flag starts goal mode from CLI
- Works with piped input for scripting
- Exit code reflects goal status (0=complete, 1=blocked, 2=timeout)
- Progress logged to stdout/stderr

---

## Testing Strategy

### Unit Tests

```go
// internal/llm/agent/goal_runner_test.go
func TestGoalRunner_SingleIteration(t *testing.T)
func TestGoalRunner_CompletionDetection(t *testing.T)
func TestGoalRunner_MaxIterationsLimit(t *testing.T)
func TestGoalRunner_TimeoutLimit(t *testing.T)
func TestGoalRunner_Cancellation(t *testing.T)
func TestGoalRunner_StallDetection(t *testing.T)

// internal/llm/agent/goal_evaluator_test.go
func TestGoalEvaluator_ExplicitCompletion(t *testing.T)
func TestGoalEvaluator_ImplicitCompletion(t *testing.T)
func TestGoalEvaluator_BlockedDetection(t *testing.T)

// internal/db/session_goals_test.go
func TestSessionGoal_CreateAndRetrieve(t *testing.T)
func TestSessionGoal_StatusTransitions(t *testing.T)
```

### Integration Tests

```python
# tests/test_goal_mode.py
def test_goal_mode_acp_activation():
    """Test /goal command via ACP."""
    
def test_goal_mode_completion_flow():
    """Test full goal execution to completion."""
    
def test_goal_mode_cancellation():
    """Test goal cancellation via /goal-cancel."""
    
def test_goal_mode_timeout():
    """Test goal timeout behavior."""
    
def test_goal_mode_persistence():
    """Test goal state survives restart."""
```

### Manual Testing Checklist

- [ ] ACP: Start goal with `/goal <objective>` in Zed
- [ ] ACP: Goal status visible in sidebar
- [ ] ACP: Cancel goal with `/goal-cancel`
- [ ] TUI: Start goal with `/goal <objective>`
- [ ] TUI: Sidebar shows goal progress
- [ ] TUI: Ctrl+C cancels goal
- [ ] WebUI: Start goal via chat
- [ ] WebUI: Progress updates in real-time
- [ ] CLI: `pando run --goal "..."` works
- [ ] Resume: Goal resumes after TUI restart

---

## Timeline Summary

| Phase | Description | Effort | Dependencies |
|-------|-------------|--------|--------------|
| 1 | Core Infrastructure | 3-4 days | None |
| 2 | ACP Integration | 2-3 days | Phase 1 |
| 3 | TUI Integration | 2 days | Phase 1, 2 |
| 4 | WebUI/API Integration | 1-2 days | Phase 1, 2 |
| 5 | Completion Detection | 2 days | Phase 1 |
| 6 | Execution Limits | 1 day | Phase 1 |
| 7 | CLI Flag | 1 day | Phase 1 |

**Total estimated effort**: 12-15 days

**Recommended order**: 1 → 2 → 5 → 6 → 3 → 4 → 7

Phases 3, 4, and 7 can be parallelized after Phase 2 is complete.

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Infinite loops / runaway execution | Medium | High | Hard limits on iterations, duration, cost |
| False completion detection | Medium | Medium | Conservative heuristics, user verification |
| ACP protocol compatibility | Low | Medium | Test with multiple ACP clients |
| Performance degradation | Low | Low | Profile multi-turn execution |
| Database migration issues | Low | Medium | Careful migration testing |

---

## Success Metrics

1. **Adoption**: % of sessions using goal mode (target: 10% within 1 month)
2. **Completion Rate**: % of goals that reach `completed` status (target: 70%)
3. **Average Iterations**: Mean iterations per successful goal (target: < 10)
4. **User Satisfaction**: Feedback on goal mode usefulness (qualitative)
5. **Error Rate**: % of goals ending in `failed` status (target: < 5%)

---

## Future Enhancements (Post-MVP)

1. **Structured Goals**: JSON schema for goal definition with success criteria
2. **Goal Templates**: Pre-defined goals for common tasks (refactor, test, docs)
3. **Multi-Agent Goals**: Spawn sub-agents for parallel goal execution via Mesnada
4. **Goal History**: View and analyze past goals per project
5. **Cost Tracking**: Track and limit token/API costs per goal
6. **Watchdogs**: External verification of goal progress (e.g., run tests periodically)
7. **Goal Sharing**: Export/import goal definitions

---

## References

- [Claude Code Documentation - Goal Mode](https://docs.anthropic.com/claude-code/goals)
- [OpenAI Codex CLI - Autonomous Goals](https://platform.openai.com/docs/codex/goals)
- [ACP Protocol Specification](https://github.com/anthropics/acp-spec)
- [Pando Architecture Overview](/.kb/pando/architecture/overview.md)