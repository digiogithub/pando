// Package vulnhunter builds the prompts used by the security-audit slash
// commands (/vulnhunt, /vulnhunter-fix, /vulnhunt-fix-verify).
//
// The commands are a port of Capital One's VulnHunter Claude Code skills
// (https://github.com/capitalone/VulnHunter), adapted to Pando's in-editor
// model. Each command expands into a full workflow instruction that is then run
// as a normal agent turn (with the session's currently selected model) — exactly
// like /improve-agents-md — so it streams, steers and persists like any other
// message. They share no state and are not persistent "modes": each run is a
// self-contained security workflow.
//
// The three workflows, and how they were adapted from the upstream skills:
//
//   - Hunt (/vulnhunt): recon → parallel class-group hunt → exploitability
//     verify → adversarial disprove → capability-filtered report. Upstream
//     dispatches subagents per vulnerability class; the Pando port instructs the
//     agent to spawn parallel hunt subagents with mesnada_spawn_agent. Findings
//     are persisted to the knowledge base with kb_add_document instead of the
//     upstream harness JSON/OUT directories.
//
//   - Fix (/vulnhunter-fix): test-driven remediation — exploit proof → failing
//     security test (RED) → fix (GREEN) → verify exploit blocked without
//     regressions → commit. The upstream GitHub-issue/fork harness (detect_mode,
//     preflight.py, private forks, PR gates) is dropped; findings are read from
//     the KB and the fix lands in the working tree.
//
//   - Verify (/vulnhunt-fix-verify): read-only, code-is-source-of-truth
//     verification of claimed fixes, emitting a per-finding verdict
//     (FIXED / PARTIAL / NOT_FIXED / INCONCLUSIVE).
//
// The workflow bodies live in the embedded *.md templates and are never read
// from disk at runtime, so the commands work uniformly across the TUI, Web UI
// and ACP surfaces.
package vulnhunter

import (
	_ "embed"
	"strings"
)

//go:embed hunt.md
var huntWorkflow string

//go:embed fix.md
var fixWorkflow string

//go:embed verify.md
var verifyWorkflow string

// HuntPrompt returns the instruction the agent runs for /vulnhunt.
//
// extra carries any free-text argument the user passed after the command (for
// example a target subdirectory, package, or emphasis); it is appended as
// additional scope guidance and may be empty.
func HuntPrompt(extra string) string {
	return withScope(huntWorkflow, extra)
}

// FixPrompt returns the instruction the agent runs for /vulnhunter-fix.
//
// extra may name a specific finding, KB document, or cluster to remediate; when
// empty the agent remediates the confirmed findings it can recover from the KB.
func FixPrompt(extra string) string {
	return withScope(fixWorkflow, extra)
}

// VerifyPrompt returns the instruction the agent runs for /vulnhunt-fix-verify.
//
// extra may name the findings or fixes to validate; when empty the agent
// verifies the fixes it can recover from the KB and working tree.
func VerifyPrompt(extra string) string {
	return withScope(verifyWorkflow, extra)
}

// withScope appends the user's free-text argument to a workflow body as
// additional scope guidance, if any was supplied.
func withScope(workflow, extra string) string {
	body := strings.TrimSpace(workflow)
	if strings.TrimSpace(extra) == "" {
		return body + "\n"
	}
	var b strings.Builder
	b.WriteString(body)
	b.WriteString("\n\n===== ADDITIONAL SCOPE / GUIDANCE FOR THIS RUN =====\n")
	b.WriteString(strings.TrimSpace(extra))
	b.WriteString("\n")
	return b.String()
}
