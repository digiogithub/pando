// Package agentsmd builds the prompt used by the /improve-agents-md slash command.
//
// The command launches a normal agent turn (with the session's currently selected
// model) whose job is to create, restructure, or reinforce an AGENTS.md file in
// the working project so it carries a canonical set of MANDATORY operating rules
// for AI agents (how to gather context, research, plan, implement, and document —
// using the project's KB, code-index, web-search, Context7 and browser tools).
//
// The canonical rules live in template.md and are embedded here. They are NOT
// pasted verbatim: the agent is instructed to evaluate each canonical MANDATORY
// clause against the current AGENTS.md and merge them intelligently, preserving
// the project's own wording and structure while guaranteeing every clause is
// represented. The package works uniformly across the TUI, Web UI and ACP surfaces
// without reading from disk at runtime.
package agentsmd

import (
	_ "embed"
	"strings"
)

// CanonicalTemplate is the project-agnostic set of MANDATORY clauses that
// /improve-agents-md ensures a target AGENTS.md covers. It is delimited by the
// `pando:agents-md:begin` / `pando:agents-md:end` sentinel comments so the
// reference content can be isolated from the surrounding file.
//
//go:embed template.md
var CanonicalTemplate string

// Sentinel markers delimiting the canonical reference inside template.md. They are
// stripped before the clauses are shown to the agent (see canonicalClauses) so the
// agent never copies the markers into a target file.
const (
	BeginMarker = "<!-- pando:agents-md:begin -->"
	EndMarker   = "<!-- pando:agents-md:end -->"
)

// canonicalClauses returns the embedded canonical content with the sentinel marker
// lines removed and surrounding whitespace trimmed, ready to be presented to the
// agent as a checklist of required MANDATORY clauses.
func canonicalClauses() string {
	s := CanonicalTemplate
	s = strings.ReplaceAll(s, BeginMarker, "")
	s = strings.ReplaceAll(s, EndMarker, "")
	return strings.TrimSpace(s)
}

// Prompt returns the full instruction the agent runs for /improve-agents-md.
//
// The agent is told to perform an EVALUATIVE MERGE (clause by clause), not a
// verbatim insertion: it reviews both the canonical clauses below and the current
// AGENTS.md, and reconciles them so every MANDATORY clause is satisfied while the
// project's existing content, wording and structure are preserved.
//
// extra carries any free-text argument the user passed after the command (for
// example a specific target path or emphasis); it is appended as additional
// guidance and may be empty.
func Prompt(extra string) string {
	var b strings.Builder
	b.WriteString(`You are improving the AGENTS.md file of the current working project.

GOAL
Ensure AGENTS.md carries a clearly-labelled set of MANDATORY operating rules for
AI agents. Do this by EVALUATING and MERGING the canonical MANDATORY clauses
(reference, below) with whatever the current AGENTS.md already says — NOT by
pasting the reference block verbatim.

FOLLOW THESE STEPS IN ORDER (and practice the rules while you apply them):

1. CONTEXT FIRST. Before editing, recover context: search the knowledge base with
   kb_search_documents (no tags filter) for anything about AGENTS.md, the project
   conventions, or prior agent-instruction work. Then locate and READ the current
   AGENTS.md at the project root if one exists, and any CLAUDE.md or similar
   instruction file whose content should be reconciled. You must understand the
   existing document before you change it.

2. EVALUATE CLAUSE BY CLAUSE. Treat the canonical block below as the AUTHORITATIVE
   CHECKLIST of MANDATORY clauses that the final AGENTS.md must cover — it is a
   reference of REQUIREMENTS, not text to copy literally. For EACH canonical
   MANDATORY clause, judge whether the current AGENTS.md already covers it:
   - ALREADY COVERED adequately -> keep the project's existing wording. Only tidy
     it and make sure it is explicitly labelled MANDATORY. Do NOT duplicate it.
   - COVERED BUT WEAKER / VAGUER than the canonical requirement -> strengthen the
     existing text so it meets the canonical requirement, in the document's own
     voice. Never weaken a clause below the canonical bar.
   - MISSING -> add it, written to fit the document's tone and the project's
     ACTUAL tools and conventions (adapt tool names if this project uses different
     ones; otherwise use the canonical names).

3. MERGE, DON'T REPLACE. Produce one coherent document. Integrate the MANDATORY
   clauses into the existing structure rather than appending a redundant block.
   Preserve every project-specific section: build/test commands, architecture
   notes, style rules, tool conventions. Restructure or reorder only when it
   improves clarity. If a previous run already left a managed MANDATORY section,
   refine it in place instead of adding a second one.

4. CREATE IF ABSENT. If no AGENTS.md exists, create one: a short project-specific
   header (name + one-paragraph description inferred from the repo / README / KB),
   followed by the MANDATORY clauses adapted to this project.

5. NON-NEGOTIABLE. The final AGENTS.md MUST represent every MANDATORY clause from
   the canonical checklist (in substance), each clearly labelled MANDATORY, with
   no project-specific content lost.

6. VERIFY. Re-read the final AGENTS.md to confirm it is well-formed Markdown,
   every MANDATORY clause is present and labelled, and project content is intact.

7. DOCUMENT. After writing AGENTS.md, record a short summary of what you changed
   with kb_add_document (what changed, files touched, why, how verified), per the
   project's documentation rule.

===== CANONICAL MANDATORY CLAUSES (reference checklist — evaluate & merge, do NOT paste verbatim) =====

`)
	b.WriteString(canonicalClauses())
	b.WriteString("\n\n===== END CANONICAL MANDATORY CLAUSES =====\n")

	if strings.TrimSpace(extra) != "" {
		b.WriteString("\nADDITIONAL USER GUIDANCE FOR THIS RUN:\n")
		b.WriteString(strings.TrimSpace(extra))
		b.WriteString("\n")
	}
	return b.String()
}
