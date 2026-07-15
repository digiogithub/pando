// Package learning implements Pando's opt-in "learner and documentarian" session
// mode: a per-session policy that makes the agent lean much harder on the
// knowledge base and memory subsystem, capture non-trivial discoveries as living
// documentation, actively ask the user what it needs to know, and keep existing
// documentation honest (updating stale docs and marking superseded ones outdated).
//
// It mirrors the architecture of internal/superpowers, internal/caveman and
// internal/ponytail: the policy is delivered through the agent's existing
// per-turn skill injection (see internal/llm/agent.prepareProvider), so it works
// uniformly across the TUI, Web UI and ACP surfaces, and it composes with
// user/project skills and with the other session policies.
//
// The mode is enabled with /learning and disabled with /learning-finish. State is
// session-scoped, ephemeral (process-bounded) and safe for concurrent sessions.
// There is no configured default: a session that never invoked /learning behaves
// exactly as before.
package learning

import (
	"strings"
	"sync"
)

// State is the per-session Learning state. It is a struct rather than a bare bool
// so later phases can carry richer session data (e.g. a running learning log)
// without changing the storage shape.
type State struct {
	Enabled bool
}

// sessions holds the per-session state, keyed by normalized session ID. It
// mirrors the per-session, ctx-threaded design of superpowers.sessions and
// ponytailModes so concurrent sessions never clobber each other's mode.
//
// Presence semantics: an entry exists only while the mode is enabled; disabling
// deletes it. Entries are process-bounded (no session-close hook to hang cleanup
// on yet), one small struct per session that opted in.
var sessions sync.Map // map[string]State

// SetEnabled enables or disables Learning mode for a session. It is safe for
// concurrent use and is the single mutation point used by every surface that
// exposes the /learning commands (ACP, Web UI, TUI).
func SetEnabled(sessionID string, enabled bool) {
	sessionID = normalizeSessionID(sessionID)
	if sessionID == "" {
		return
	}
	if enabled {
		sessions.Store(sessionID, State{Enabled: true})
		return
	}
	sessions.Delete(sessionID)
}

// Enabled reports whether Learning mode is active for a session.
func Enabled(sessionID string) bool {
	sessionID = normalizeSessionID(sessionID)
	if sessionID == "" {
		return false
	}
	value, ok := sessions.Load(sessionID)
	if !ok {
		return false
	}
	state, ok := value.(State)
	return ok && state.Enabled
}

func normalizeSessionID(sessionID string) string {
	return strings.TrimSpace(sessionID)
}

// Instructions returns the learner/documentarian policy injected into the system
// prompt on every turn of an enabled session.
func Instructions() string {
	return instructions
}

// FinishPrompt returns the prompt for the closing turn run by /learning-finish.
// It is a normal agent turn: it consolidates what was learned into KB/memory,
// flags stale docs, and reports. It performs no git side effect.
func FinishPrompt() string {
	return finishPrompt
}

// ActivationMessage is the confirmation shown when the mode is switched on. The
// focus, when given, is echoed back but not persisted: it only frames the
// confirmation.
func ActivationMessage(focus string) string {
	msg := activationMessage
	if focus = strings.TrimSpace(focus); focus != "" {
		msg += "\n\nFocus: " + focus
	}
	return msg
}

// AlreadyActiveMessage is the idempotent response to re-invoking /learning.
const AlreadyActiveMessage = "Learning mode is already active. It stays on until /learning-finish."

// NotActiveMessage is the response to /learning-finish outside the mode.
const NotActiveMessage = "Learning mode is not active. Nothing to finish — you are already in normal mode."

const activationMessage = `Learning mode enabled. Normal behavior is unchanged until the next request; from now
on I work as a deliberate learner and documentarian: I read the knowledge base and memory
before acting, capture non-trivial discoveries as living documentation, ask you what I need
to know instead of guessing, and keep existing docs honest (update the stale, mark the
superseded outdated).

Trivial and read-only requests are answered directly. Your instructions always take precedence.

Run /learning-finish to consolidate what was learned and return to normal mode.`

const finishPrompt = `Close out Learning mode for this session. Consolidate what was learned. Do exactly this,
and nothing that changes git state:

1. Review what this session actually discovered that was NOT trivially derivable from the
   code or git history: design decisions, non-obvious constraints, gotchas, how a subsystem
   really behaves, choices the user made.
2. Persist it as living documentation: write or UPDATE the relevant kb_add_document entries
   (clear file_path, wiki-linked with [[...]]), and store short durable facts with remember
   under stable keys. Prefer updating an existing document over creating a duplicate — search
   with kb_search_documents first.
3. Reconcile existing documentation: if you touched or relied on a KB doc that is now stale,
   update it; if a doc was superseded, mark it outdated (kb_mark_outdated) rather than deleting
   it, unless it is truly junk.
4. Summarize, concisely: what you captured (documents written/updated, memories stored, docs
   marked outdated) and what is still open or unverified.

Do NOT commit, merge, push, open a pull request, create or delete branches, discard changes,
or modify git configuration. Documenting and reporting only.`

const instructions = `Learning mode is ACTIVE for this session. Work as a deliberate learner and
documentarian. It stays active until ` + "`/learning-finish`" + `.

PRECEDENCE. A direct user instruction, AGENTS.md/CLAUDE.md, and the permission
system all outrank this policy. Do not apply it to simple read-only questions,
lookups or one-line factual answers, and never block an emergency user-directed
fix; if you deviate, say so in one line and move on.

1. RECOVER CONTEXT BEFORE ACTING.
   Before any task that builds on prior work, search the knowledge base first with
   ` + "`kb_search_documents`" + ` (untagged, so anything related surfaces) or
   ` + "`hybrid_search_remembrances`" + `, and ` + "`recall`" + ` any facts you may
   have stored. Use the code-index tools (code_find_symbol, code_get_symbols_overview,
   code_hybrid_search, code_search_pattern) for structure. Do not guess at an unstated
   requirement and build on the guess.

2. ASK — DON'T ASSUME.
   Be curious. When a preference, a decision, or an unstated requirement would change
   what you build, ASK THE USER with the ` + "`AskUserQuestion`" + ` tool instead of
   guessing. Batch related questions, keep them answerable, and offer concrete options.
   Reserve this for things that actually matter — never interrogate the user over
   trivial or read-only work. If the tool is unavailable, ask in plain text and end the
   turn.

3. CAPTURE WHAT YOU DISCOVER.
   Persist non-trivial findings, analyses and decisions as living documentation with
   ` + "`kb_add_document`" + ` under a clear file_path, wiki-linked ([[...]]) into the
   graph. Store short, durable, key-identified facts with ` + "`remember`" + ` (never
   long content). Do NOT document what is trivially re-derivable from the code, tests or
   git history — capture the reasoning, the gotchas and the decisions, not the obvious.
   Update an existing document in place rather than creating a duplicate.

4. KEEP DOCUMENTATION HONEST.
   When you encounter a KB document that is stale or contradicted by the current code,
   update it and say what you changed and why. When a document has been superseded, mark
   it outdated with ` + "`kb_mark_outdated`" + ` (a soft, reversible flag that hides it
   from default searches) — reach for ` + "`kb_delete_document`" + ` only when a document
   is truly junk.

5. DOCUMENTATION DEPTH IS SEPARATE FROM OUTPUT BREVITY.
   If an output-brevity policy (Caveman/Ponytail) is also active, it governs your CHAT
   replies and how much code you build — it does NOT limit the depth of the KB documents
   and memories you write. Keep documenting thoroughly regardless.`
