# ACP tool-call flow analysis: opencode/claude-agent-acp vs Pando

> Date: 2026-05-29
> Scope: Analyze the ACP message flow used for tool invocations, compare the reference adapter behavior with Pando, and outline a concrete change plan focused on Zed rendering of tool details.
> Sources:
> - Local indexed code: `claude-agent-acp/src/acp-agent.ts`, `claude-agent-acp/src/tools.ts`
> - Local indexed code: `pando/internal/mesnada/acp/prompt_handler.go`, `pando/internal/mesnada/acp/client.go`, `pando/internal/mesnada/acp/agent_pando_test.go`
> - Existing KB notes: `pando/plans/acp-message-standardization-plan-2026-05-29.md`, `research/acp-opencode-examples-es.md`
> - DeepWiki: retried multiple times against `sst/opencode` with an increased timeout (2 minutes per request), but the service still timed out in this session. No additional repository-grounded details were returned, so conclusions below still rely on the local ACP reference adapter plus existing KB research.

## Update after DeepWiki retry

A second verification pass was performed after increasing the DeepWiki timeout. Result: both repository questions still timed out after 2 minutes, so there is still no additional opencode-specific evidence from DeepWiki to incorporate. This is itself useful to record because it means the current document should be treated as:

- **confirmed by local ACP reference code and Pando code**, and
- **not yet additionally validated by DeepWiki against `sst/opencode` source**.

Because DeepWiki returned no new facts, the practical conclusions and plan remain unchanged. The highest-value next step is still to compare exact emitted ACP payloads from Pando against the locally indexed ACP reference adapter.

---

## 1. What the ACP reference flow looks like in practice

Although the user asked specifically for opencode, the clearest local ACP reference available in indexed code is `claude-agent-acp`, which follows the same client-facing ACP rendering contract that Zed consumes. The important part for this investigation is not brand-specific internals but the observable ACP lifecycle and payload shape.

### 1.1 Canonical lifecycle for a normal tool invocation

From `claude-agent-acp/src/acp-agent.ts:toAcpNotifications` the runtime sequence is:

1. The model emits a `tool_use` chunk.
2. On first encounter for that `toolUseId`, the adapter emits:
   - `sessionUpdate: "tool_call"`
   - `toolCallId`
   - `status: "pending"`
   - `rawInput`
   - `title`, `kind`, optional `locations`, optional `content`
   - `_meta.terminal_info` for Bash when terminal output capability exists
3. If the same tool use is seen again later with richer accumulated input, the adapter emits:
   - `sessionUpdate: "tool_call_update"`
   - same `toolCallId`
   - refined `rawInput`
   - refined `title`, `kind`, `locations`, `content`
4. When the corresponding `tool_result` arrives, the adapter emits one or more updates:
   - optional `tool_call_update` carrying `_meta.terminal_output`
   - final `tool_call_update` with:
     - `status: "completed"` or `"failed"`
     - `rawOutput`
     - final `content`
     - final `title`, `kind`, `rawInput`
     - optional `locations`
     - optional `_meta.terminal_exit`
5. For plan tools (`TodoWrite`) there is no normal `tool_call`; instead it emits:
   - `sessionUpdate: "plan"`
   - `entries: [...]`

### 1.2 Why this matters for Zed

Zed appears to rely on the following invariants:

- A `tool_call` must exist before updates for the same `toolCallId`, or the card may never render.
- `rawInput` should be present as early as possible, ideally on the initial `tool_call`.
- `title`, `kind`, and `locations` are not cosmetic; they are the primary rendering hints for showing the file path, command, regex, and tool identity.
- Rich `content` is used for diff previews, terminal references, and readable result blocks.
- Bash output in terminal-capable clients is split across `_meta.terminal_info`, `_meta.terminal_output`, and `_meta.terminal_exit` rather than only using text content.
- Plans are rendered from `sessionUpdate: "plan"`, not from a generic tool card.

---

## 2. Reference payload details by tool type

From `claude-agent-acp/src/tools.ts`:

### 2.1 Read
- `title`: `Read <relative-path> (line-range)` when available
- `kind`: `read`
- `locations`: `[{ path: input.file_path, line: input.offset ?? 1 }]`
- `rawInput`: full structured JSON object
- result `content`: text block with file contents

### 2.2 Write
- `title`: `Write <relative-path>`
- `kind`: `edit`
- `locations`: `[{ path: input.file_path }]`
- start `content`: optimistic diff block using `oldText: null`, `newText: input.content`
- later updates may replace that optimistic diff using hook-derived structured patches

### 2.3 Edit
- `title`: `Edit <relative-path>`
- `kind`: `edit`
- `locations`: `[{ path: input.file_path }]`
- start `content`: diff block using `old_string` and `new_string`
- result content may later be refined from tool hook output

### 2.4 Bash
- `title`: command string (`input.command`) rather than a generic tool name
- `kind`: `execute`
- start `content`:
  - terminal reference if terminal capability exists
  - otherwise a descriptive text fallback
- `_meta.terminal_info` on `tool_call`
- `_meta.terminal_output` and `_meta.terminal_exit` on later updates

### 2.5 Grep
- `title`: synthetic grep-like command assembled from options, including pattern/path/type/limits
- `kind`: `search`
- no `locations` unless explicitly provided in helper logic for the tool family
- critical for UX: the regexp/pattern is embedded in the title itself

### 2.6 TodoWrite / task tools
- `TodoWrite` suppresses the normal tool card entirely
- emits `sessionUpdate: "plan"` with normalized `entries`
- task-based variants (`TaskCreate`, `TaskUpdate`) also converge to `plan` updates

---

## 3. What Pando currently does

From `pando/internal/mesnada/acp/prompt_handler.go` and associated tests, Pando already mirrors much of this behavior.

### 3.1 Streaming tool-call path in Pando

`processAgentEventStream` currently does the following:

1. On `AgentEventTypeToolCall`:
   - parses `rawInput`
   - computes `kind`, `title`, `content`, `locations`
   - builds start metadata via `toolMeta`
2. If the tool is `TodoWrite`:
   - stores pending input
   - marks the tool as started
   - emits `UpdatePlan(...)` directly when parseable
   - skips normal `tool_call`
3. If tool input is not finished and this is the first start:
   - emits `StartToolCall(... status=pending, rawInput, kind, content, locations, meta ...)`
4. If the tool is seen again while input is still streaming:
   - emits `UpdateToolCall(... status=in_progress or pending when promoting enriched input ... rawInput, title, kind, content, locations ...)`
5. On `AgentEventTypeToolResult`:
   - synthesizes a missing start if necessary
   - rebuilds `rawInput`
   - builds `rawOutput`
   - emits optional terminal output meta update
   - emits final `UpdateToolCall(... status=completed/failed, rawInput, rawOutput, title, kind, content, locations ...)`

### 3.2 History / assembled-response path in Pando

`processAgentResponse` also:

- guarantees a `StartToolCall` exists even for non-streaming providers
- issues corrective `UpdateToolCall` when a streamed start happened with empty input and the full assembled message later contains richer input
- suppresses `TodoWrite` tool cards and emits `plan` instead
- sends `rawInput`, `title`, `kind`, `locations`, `content`, and final `rawOutput`

### 3.3 Pando client-side consumption

`internal/mesnada/acp/client.go:processToolCallUpdate` stores:

- `Status`
- `Title`
- `Kind`
- `RawInput`
- `Locations`
- `RawOutput`
- `Content`
- extracted diffs

So from the client-consumer side, Pando already expects the same ACP metadata fields that the reference adapter emits.

---

## 4. Direct comparison: likely causes of the remaining Zed rendering mismatch

This is the most relevant section for the requested plan.

### 4.1 Areas where Pando appears aligned

Pando already has these key behaviors aligned with the reference flow:

- `tool_call` precedes `tool_call_update`
- initial status is `pending`
- final status is `completed` / `failed`
- `rawInput` is included on start and updates when parseable
- `TodoWrite` is converted into `plan`
- Bash terminal meta lifecycle is split into `terminal_info`, `terminal_output`, `terminal_exit`
- `Edit`/`Write` result content can include diff content
- synthetic start exists when streaming start was lost
- there are tests specifically about enriched pending updates for empty initial inputs

### 4.2 Most plausible remaining mismatch: exact field shaping, not broad lifecycle

Given the current code and tests, the issue is unlikely to be the high-level ordering alone. The most likely remaining mismatches are more subtle:

#### A. Exact title formatting differences
Zed may be showing data primarily from `title` rather than `rawInput` for some tool cards.

This especially affects:
- Bash command visibility
- Read/Write/Edit file display
- Grep regexp visibility

Pando has tests indicating command/path-aware titles now exist, but the exact helpers used by all tool aliases (`read` vs `view`, `edit` vs `Edit`, `bash` vs `Bash`, etc.) must be verified exhaustively.

#### B. Tool-name / alias normalization differences
The reference adapter uses canonical tool names such as `Read`, `Write`, `Edit`, `Bash`, `Grep`, `TodoWrite`.

Pando often maps internal names like:
- `view`
- lowercase `write`, `edit`, `bash`

If any rendering helper or metadata helper still depends on a different casing/path than the actual emitted tool name, Zed may receive valid ACP messages but with degraded title/content/location derivation.

#### C. `locations` population gaps for some search tools
The reference rendering strongly benefits from `locations`, especially for read/write/edit and sometimes grep/glob.

Pando likely covers read/write/edit, but grep/glob should be verified end-to-end. If `Grep` lacks `locations` while the reference adapter exposes path-context in title or locations, Zed may not show the target file/folder clearly.

#### D. `content` shape differences
Zed may render some cards from `content` rather than from `rawInput`. Important examples:

- `Write` / `Edit`: diff blocks vs plain text fallback
- `Bash`: terminal ref vs text block fallback
- partial write/edit inputs: path hint content

Pando has tests for partial write/edit path hints and terminal refs, but those should be verified against actual emitted payloads for every path, especially history replay vs live stream.

#### E. `TodoWrite` live-vs-history parity may still have edge cases
Previous event history notes indicate a prior bug in live plan rendering was already fixed. Even so, if the `plan` update is emitted only when partial JSON becomes parseable, there may still be edge cases where the first visible plan update is delayed or omitted for some providers.

That would explain the user report that the “correct message” seems to be sent but Zed still does not show the generated plan in the expected way.

#### F. Meta namespace differences
The reference adapter uses `_meta.claudeCode.toolName` while Pando uses `_meta.pando.toolName` plus terminal metadata. If Zed happens to use adapter-specific `_meta` keys for some enhanced rendering path, Pando could still diverge despite standard ACP fields being present.

This is speculative, but worth testing because the standard ACP fields seem largely present already.

---

## 5. Concrete verification matrix to run next in code/tests

To isolate the mismatch, the next changes should focus on message-shape verification rather than broad refactors.

### 5.1 Add golden tests for exact ACP payloads per tool
Create ACP serialization tests that assert the exact emitted `tool_call` / `tool_call_update` JSON for:

1. Read / view
   - path shown in `title`
   - `locations[0].path`
   - `rawInput.file_path`
2. Write
   - path shown in `title`
   - start diff content present
   - `locations` present
3. Edit
   - path shown in `title`
   - diff content present
   - `locations` present
4. Bash
   - command shown in `title`
   - initial status `pending`
   - terminal ref in `content`
   - terminal meta updates emitted separately
5. Grep
   - regexp/pattern included in `title`
   - path scope included in `title` and/or `locations`
6. TodoWrite
   - no `tool_call`
   - emits `plan` with expected entries during streaming and replay

### 5.2 Add parity tests against reference adapter semantics
Not byte-for-byte identical across implementations, but assert the same rendering contract:

- same lifecycle order
- same visible title semantics
- same availability of file path / command / regex in first renderable event
- same plan suppression behavior for TodoWrite

### 5.3 Log real outgoing ACP payloads during a Zed reproduction
Introduce or use targeted debug logging to capture the exact ACP notification JSON emitted by Pando during:

- Read
- Write
- Edit
- Bash
- Grep
- TodoWrite

Then compare against the known-good shape from the reference adapter.

This is likely the fastest route to identify the last mismatch.

---

## 6. Recommended plan of changes

### Phase 1 — Audit and normalize render helpers
Audit all helper functions used to derive:
- `toolDisplayTitle`
- `toolCallContent`
- `toLocations`
- `toolMeta`
- `mapToolKind`

Goals:
- ensure every tool alias/casing produces the same rendering metadata
- ensure Bash title is always the command when available
- ensure Grep title always includes the regexp/pattern and path scope
- ensure Read/Write/Edit always surface file paths

### Phase 2 — Add exact-payload regression tests
Expand `internal/mesnada/acp/agent_pando_test.go` with table-driven payload assertions for:
- start event
- enrichment update
- final result update
- live TodoWrite plan update
- replay TodoWrite plan update

### Phase 3 — Compare `_meta` behavior with the reference adapter
Validate whether Zed depends only on ACP-standard fields or also on adapter-specific meta.

If needed:
- preserve existing `_meta.pando`
- optionally add a compatibility meta shape alongside it for editor interoperability

### Phase 4 — Capture a real ACP transcript from Pando and compare with reference
For one invocation of each tool type, save the literal outgoing ACP messages and diff them against the reference semantics.

This should answer definitively whether the issue is:
- ordering
- status transitions
- title/location/content shaping
- meta differences
- or a Zed-side assumption

---

## 7. Main conclusion

Based on the local code comparison, Pando is already much closer to the opencode/claude-agent-acp tool-call lifecycle than the user suspected. The likely remaining problem is not the broad ACP flow but one of these narrower issues:

1. exact title construction for tool-specific UI rendering
2. alias/casing normalization across helper functions
3. locations/content not populated in one of the live or replay paths
4. TodoWrite plan edge cases during partial streaming
5. adapter-specific `_meta` differences that Zed happens to consume

That means the right next step is a message-shape audit with exact payload regression tests, not a full ACP rewrite.
