// Package superpowers implements Pando's opt-in disciplined development
// workflow: a per-session policy that forces long or risky work through an
// explicit lifecycle (clarify/design -> approved plan -> test-first
// implementation -> verification/review -> explicit finish) before any code is
// changed.
//
// It is inspired by the workflow principles of the Superpowers plugin by Jesse
// Vincent (https://github.com/obra/superpowers, MIT licensed), but it is a
// Pando-owned policy rather than a vendored copy: there is no plugin runtime,
// no telemetry, no automatic worktrees or subagents, and no destructive git
// operation. The policy is delivered through the agent's existing per-turn skill
// injection (see internal/llm/agent.prepareProvider), so it works uniformly
// across the TUI, Web UI and ACP surfaces, and it composes with user/project
// skills and with ponytail.
//
// The mode is enabled with /superpowers and disabled with /superpowers-finish.
// State is session-scoped, ephemeral (process-bounded) and safe for concurrent
// sessions.
package superpowers

import (
	"strings"
	"sync"
)

// State is the per-session Superpowers state. It is a struct rather than a bare
// bool so later phases can carry workflow stages (design-approved,
// plan-approved, implementing) without changing the storage shape.
type State struct {
	Enabled bool
}

// sessions holds the per-session state, keyed by normalized session ID. It
// mirrors the per-session, ctx-threaded design of ponytailModes and
// sessionLLMOverrides so concurrent sessions never clobber each other's mode.
//
// Presence semantics: an entry exists only while the mode is enabled; disabling
// deletes it. There is no configured default — a session that never invoked
// /superpowers behaves exactly as before.
//
// Entries are process-bounded: there is currently no session-close hook to hang
// cleanup on, and the map holds one small struct per session that opted in, so
// eviction is deferred (see the plan's deferred follow-ups).
var sessions sync.Map // map[string]State

// SetEnabled enables or disables Superpowers mode for a session. It is safe for
// concurrent use and is the single mutation point used by every surface that
// exposes the /superpowers commands (ACP, Web UI, TUI).
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

// Enabled reports whether Superpowers mode is active for a session.
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

// Instructions returns the lifecycle policy injected into the system prompt on
// every turn of an enabled session.
func Instructions() string {
	return instructions
}

// FinishPrompt returns the prompt for the closing turn run by
// /superpowers-finish. It is a normal agent turn: it verifies and reports, and
// deliberately performs no git side effect (no commit, merge, push, PR, branch
// or worktree operation) — those stay user-directed.
func FinishPrompt() string {
	return finishPrompt
}

// ActivationMessage is the confirmation shown when the mode is switched on. The
// objective, when given, is echoed back but not persisted: it only frames the
// confirmation.
func ActivationMessage(objective string) string {
	msg := activationMessage
	if objective = strings.TrimSpace(objective); objective != "" {
		msg += "\n\nObjective: " + objective
	}
	return msg
}

// AlreadyActiveMessage is the idempotent response to re-invoking /superpowers.
const AlreadyActiveMessage = "Superpowers mode is already active. It stays on until /superpowers-finish."

// NotActiveMessage is the response to /superpowers-finish outside the mode.
const NotActiveMessage = "Superpowers mode is not active. Nothing to finish — you are already in normal mode."

const activationMessage = `Superpowers mode enabled. Normal behavior is unchanged until the next request; from
now on work runs through: clarify/design -> approved plan -> test-first implementation ->
verification with evidence -> explicit finish.

Long tasks get a written, prioritized plan before any code changes. Trivial and read-only
requests are answered directly. Your instructions always take precedence.

Run /superpowers-finish to verify, report and return to normal mode.`

const finishPrompt = `Close out the Superpowers workflow for this session. Do exactly this, and nothing that
changes git state:

1. Inspect the current state of the work (git status and the diff of what changed).
2. Run the narrowest verification that covers the changes (the relevant tests/build). Report
   the real output. If you cannot run it, say so explicitly and explain why — do not claim
   anything passed that you did not observe passing.
3. Summarize what changed and why, file by file.
4. State what is NOT done: gaps, known risks, shortcuts taken, and anything left unverified.
5. Offer the sensible next actions, but do not take them.

Do NOT commit, merge, push, open a pull request, create or delete branches or worktrees,
discard changes, or modify git configuration. Reporting only.`

const instructions = `Superpowers mode is ACTIVE for this session. Work through the lifecycle below
instead of jumping straight to code. It stays active until ` + "`/superpowers-finish`" + `.

PRECEDENCE. A direct user instruction, AGENTS.md/CLAUDE.md, and the permission
system all outrank this policy. If the user tells you to skip a gate or ships an
emergency fix, comply, say in one line which gate you skipped, and still verify
the result. Do not apply the gates to simple read-only questions, lookups or
one-line factual answers: use them for work that changes behavior, or for any
task large enough to need more than a couple of steps.

1. UNDERSTAND BEFORE DESIGNING.
   Read the relevant code and prior context first (KB search, code index, tests).
   State the requirements and constraints you extracted, and ask about the
   ambiguities that would change the design. Do not guess at an unstated
   requirement and build on the guess.

2. DESIGN, THEN GET APPROVAL.
   For anything non-trivial, present a bounded design: what changes, which
   alternatives you considered and why you rejected them, what you deliberately
   leave out, and the risks. Wait for the user's approval before changing code.

3. PLAN LONG WORK EXPLICITLY, AND PRIORITIZE IT.
   A task that spans multiple files, subsystems or sessions gets a written plan
   before implementation: ordered phases, each with the exact files/symbols it
   touches, its exit criteria, and the command that verifies it. Order the phases
   by risk and dependency — the load-bearing, most uncertain part goes first, so a
   wrong assumption surfaces early and cheap. Keep the plan durable (KB document)
   and update it as reality diverges from it.

4. IMPLEMENT TEST-FIRST, IN SMALL INCREMENTS.
   Where a test can express the behavior, write the failing test before the
   change, and watch it fail for the right reason. Ship one focused increment at
   a time and keep the tree green between increments.

5. FOR BUGS: REPRODUCE, THEN FIND THE ROOT CAUSE.
   Get a reproducible failure before you touch behavior, explain the actual cause,
   and fix that cause — not the symptom. No speculative fixes.

6. VERIFY WITH EVIDENCE, NOT CLAIMS.
   Run the narrowest relevant tests/build and report the real output. If you could
   not run something, say so explicitly. Never claim work is done, passing or
   verified without evidence; if a test fails, report the failure.

7. REVIEW BEFORE DECLARING READY.
   Re-read your own diff for correctness, leftovers and scope creep before you
   hand it back.`
