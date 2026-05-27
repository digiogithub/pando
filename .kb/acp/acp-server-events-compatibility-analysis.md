# ACP server events in Pando: why we currently use `session_info_update` metadata instead of a native `session_update` type

## Context

Pando now emits internal execution-status information for ACP clients during flows such as:

- automatic persona selection
- context compaction
- compaction fallback / retry with an alternate model
- other internal status events that are not part of the assistant's natural-language answer

The current implementation does **not** introduce a new native ACP `session_update` variant. Instead, it publishes these events through:

- `session_info_update`
- `_meta["pando:serverEvents"]`

This document explains why that decision was taken, what trade-offs it implies, and what would be required to migrate to a future native ACP event/update type.

---

## Short answer

We avoided creating or depending on a new native `session_update` type because:

1. **ACP clients may ignore or reject unknown update variants**
2. **Current SDK and generated types already support extensible metadata safely**
3. **`_meta` is the compatibility-preserving extension point explicitly intended for protocol evolution**
4. **We wanted immediate interoperability without forcing all clients to upgrade**
5. **A native type is still a valid future direction, but it should come with negotiated capability support and a compatibility fallback**

In other words: we chose the path that is least likely to break existing ACP clients while still allowing Pando to ship richer execution-state telemetry today.

---

## The compatibility problem

ACP session updates are currently represented in the Go SDK as a tagged union / discriminated variant model. A `SessionUpdate` can be one of a finite set of known update kinds, such as:

- `agent_message_chunk`
- `agent_thought_chunk`
- `tool_call`
- `tool_call_update`
- `plan`
- `current_mode_update`
- `config_option_update`
- `session_info_update`
- `usage_update`

This matters because many clients are implemented with one of these assumptions:

### 1. Strict variant decoding
A client may deserialize `sessionUpdate` into a finite enum or generated union and expect only the variants it knows.

If the server starts sending a brand-new update kind like:

- `server_event`
- `status_event`
- `progress_event`

then an older client might:

- fail decoding the notification
- log an error and drop the whole event
- terminate the stream if it considers the message invalid
- silently ignore the event but also lose the payload structure

### 2. UI assumptions tied to known variants
Even if a client does not crash, it may only render known update categories. For example, it may display:

- text chunks in the transcript
- tool calls in a tools panel
- plans in a plan panel
- usage in a footer

A new native update kind would not automatically appear anywhere in the UI unless the client explicitly implements it.

### 3. Unknown update handling is not guaranteed uniformly
The ACP protocol may conceptually allow extensibility, but in practice each client and SDK decides how tolerant it is:

- some are permissive
- some are generated from strict schemas
- some are only tested against the currently published variant set

So the operational question is not just "does the spec allow it?" but "will real clients survive it today?"

---

## Why `_meta` is the safest extension point

ACP already defines `_meta` as the protocol-sanctioned extensibility channel for additional data that implementations should not make strong assumptions about.

Using:

- `session_info_update`
- with `_meta["pando:serverEvents"]`

gives us several benefits.

### 1. Structural compatibility
The outer update type remains one the client already knows:

- `session_info_update`

That means existing decoders continue to parse the notification successfully.

### 2. Tolerant consumption
Clients that do not know about `pando:serverEvents` can ignore it safely.

Clients that do know about it can start rendering it without requiring a protocol-wide breaking change.

### 3. Incremental adoption
This allows a staged rollout:

- **today**: Pando emits metadata
- **future client**: client starts reading and displaying metadata
- **later**: protocol may add a first-class event type

No hard coordination is required for the first step.

### 4. SDK support already exists
The current ACP Go SDK already supports:

- `SessionInfoUpdate.Meta map[string]any`

So we can ship richer status data without forking the SDK, regenerating schema bindings, or inventing an unsupported custom wire shape.

---

## Why we did not overload `agent_message_chunk` as the main mechanism

Another option would have been to send these status messages as normal assistant-visible text chunks. We did partially do that already for some internal agent events in the streaming path.

However, using only text chunks has downsides:

- they pollute the user-facing transcript
- they are harder to distinguish from actual assistant output
- they are not structured
- clients cannot easily separate "status telemetry" from "answer content"

By contrast, `_meta["pando:serverEvents"]` gives us structured, machine-readable status data while preserving compatibility.

---

## Specific reasoning for Pando's current implementation

The current Pando implementation stores last-run internal system messages in the LLM agent layer and flushes them to ACP at prompt completion via:

- `session_info_update`
- `_meta["pando:serverEvents"] = []string{...}`

This design was chosen because it satisfies three constraints at once:

### Constraint A: preserve current ACP client compatibility
We do not want existing editor integrations to break just because Pando added richer internal telemetry.

### Constraint B: avoid premature protocol divergence
Adding a new update variant locally would create a Pando-specific dialect of ACP unless the official protocol and SDK also support it.

### Constraint C: keep a migration path open
By using structured metadata under a namespaced key, we keep the door open to:

- future native ACP support
- capability negotiation
- dual-publishing during migration

---

## Risks of introducing a native update type too early

If Pando were to introduce a native event type before ACP clients support it, the risks include:

### 1. Broken editor integrations
Some integrations may fail hard on unknown session updates, especially if they use generated model bindings without fallback logic.

### 2. Fragmented ACP behavior
Pando would effectively be speaking a superset of ACP that only some clients understand. That leads to interoperability confusion:

- works in Continue
- partially works in Cline
- fails in other clients

### 3. Higher maintenance burden
We would need to maintain:

- custom SDK patches
- custom codegen changes
- client-specific workarounds
- additional regression coverage

### 4. Harder rollback path
Once clients start depending on a Pando-only native event, changing or removing it becomes harder than changing a namespaced metadata payload.

---

## What a future native ACP event/update type should look like

A native ACP solution is still desirable long term because it would be cleaner and more explicit.

A future first-class type could look conceptually like one of these:

- `server_event`
- `status_update`
- `progress_update`
- `runtime_event`

And it would likely carry fields such as:

- `type` or `kind` (`status`, `progress`, `retry`, `warning`, `debug`)
- `message`
- `phase` (`persona_selection`, `compaction`, `provider_retry`, `self_improvement`)
- `attempt`
- `max_attempts`
- optional `details`
- optional `_meta`

Example conceptual shape:

```json
{
  "sessionUpdate": "server_event",
  "kind": "retry",
  "phase": "provider_call",
  "message": "Provider failed, retrying with fallback model",
  "attempt": 2,
  "maxAttempts": 3,
  "_meta": {
    "provider": "anthropic",
    "fallback": "copilot.gpt-5.4"
  }
}
```

This would be semantically better than overloading `session_info_update` metadata.

---

## What would be needed before adopting a native type

Before switching Pando to a native ACP event type, the following conditions should ideally be met.

### 1. Protocol-level definition
The ACP spec should define:

- the update name
- its schema
- rendering expectations
- whether clients may ignore it safely

### 2. SDK support
The ACP SDKs used by Pando and clients should support the new variant in their generated types.

For Go, this means `SessionUpdate` should gain the new field/variant explicitly.

### 3. Capability negotiation
There should be a way for a client to indicate:

- it understands native server events
- or it only supports metadata fallback

Without capability negotiation, the server cannot know whether sending the new type is safe.

### 4. Dual-publish migration period
The safest migration plan would be:

1. add native type support in protocol + SDK
2. client advertises support
3. server emits native type **only when client declares support**
4. otherwise server falls back to `_meta["pando:serverEvents"]`

This avoids a flag day.

---

## Recommended future migration strategy

If we decide later to implement this as a native ACP update type, the migration should look like this:

### Phase 1 — current state
- emit `session_info_update` metadata
- key: `pando:serverEvents`
- clients may optionally read and render it

### Phase 2 — protocol extension available
- extend ACP spec with first-class event/status update
- extend SDKs
- introduce client capability flag like:
  - `clientCapabilities.session.serverEvents = true`
  - or equivalent negotiated metadata

### Phase 3 — dual transport
When client supports native events:
- send native event update

When client does not:
- send `session_info_update._meta["pando:serverEvents"]`

Optionally for a transition window:
- send both, if duplication can be managed safely

### Phase 4 — optional deprecation
Once native support is broad enough:
- keep metadata fallback for old clients, or
- deprecate the metadata path only if compatibility guarantees allow it

---

## Why this decision is not a dead end

Choosing metadata now does **not** mean Pando is locked into metadata forever.

In fact, it creates a clean bridge:

- payload is already structured and namespaced
- semantics are already separated from assistant text
- clients can start consuming it today
- later it can be mapped to a first-class ACP variant with minimal conceptual churn

So this is best seen as a **compatibility-first staging decision**, not as a rejection of native ACP support.

---

## Practical recommendation for future work

If native ACP server-event updates become interesting later, the next investigation should answer these questions:

1. Do the latest ACP spec and SDKs define a first-class runtime/server event update?
2. If not, should Pando propose one upstream rather than inventing a private variant?
3. Do target clients tolerate unknown `sessionUpdate` discriminators safely?
4. Can clients advertise support explicitly?
5. What UI should editors render for status/progress/retry events?

Until those answers are solid, the current `_meta` strategy is the safest production choice.

---

## Final conclusion

Pando currently uses `session_info_update` metadata for ACP server events because it is the most compatible and least disruptive way to expose internal runtime status without breaking older or strict ACP clients.

A native ACP event/update type would be cleaner in the long run, but it should only be adopted when:

- the protocol defines it
- SDKs support it
- clients can negotiate support
- Pando can preserve a fallback path for older clients

So the current implementation is a deliberate **compatibility-first design**, not an architectural limitation.
