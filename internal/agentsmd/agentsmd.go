// Package agentsmd builds the prompt used by the /improve-agents-md slash command.
//
// The command launches a normal agent turn whose job is to create, restructure,
// or reinforce an AGENTS.md file in the working project so it carries a canonical
// set of MANDATORY operating rules for AI agents (how to gather context, research,
// plan, implement, and document — using the project's KB, code-index, web-search,
// Context7 and browser tools).
//
// The canonical rules live in template.md and are embedded here so the command
// works uniformly across the TUI, Web UI and ACP surfaces without reading from
// disk at runtime.
package agentsmd

import (
	_ "embed"
	"strings"
)

// CanonicalTemplate is the project-agnostic MANDATORY ruleset that
// /improve-agents-md incorporates into a target AGENTS.md. It is delimited by the
// `pando:agents-md:begin` / `pando:agents-md:end` sentinel comments so an existing
// block can be detected and replaced in place on re-runs.
//
//go:embed template.md
var CanonicalTemplate string

// Sentinel markers delimiting the managed block inside a target AGENTS.md.
const (
	BeginMarker = "<!-- pando:agents-md:begin -->"
	EndMarker   = "<!-- pando:agents-md:end -->"
)

// Prompt returns the full instruction the agent runs for /improve-agents-md.
//
// extra carries any free-text argument the user passed after the command (for
// example a specific target path or emphasis); it is appended as additional
// guidance and may be empty.
func Prompt(extra string) string {
	var b strings.Builder
	b.WriteString(`You are improving the AGENTS.md file of the current working project.

GOAL
Make AGENTS.md carry a canonical, clearly-labelled set of MANDATORY operating
rules for AI agents, without losing any existing project-specific content.

FOLLOW THESE STEPS IN ORDER (and practice the rules while you apply them):

1. CONTEXT FIRST. Before editing, recover context: search the knowledge base
   with kb_search_documents (no tags filter) for anything about AGENTS.md, the
   project conventions, or prior agent-instruction work. Then locate and READ the
   existing AGENTS.md at the project root if one exists. Also check for a
   CLAUDE.md or similar instruction file whose content should be reconciled.

2. DECIDE create vs reinforce:
   - If no AGENTS.md exists, CREATE one. Start it with a short project-specific
     header (name + one-paragraph description, inferred from the repo / README /
     KB), then append the canonical MANDATORY block below.
   - If AGENTS.md exists, REINFORCE it: keep every project-specific section, fix
     structure/formatting where it helps, and ensure the canonical MANDATORY
     block is present and correct.

3. INSERT THE MANAGED BLOCK. The canonical rules are delimited by the sentinel
   comments `)
	b.WriteString(BeginMarker)
	b.WriteString(" and ")
	b.WriteString(EndMarker)
	b.WriteString(`.
   - If the existing file already contains a block between those markers, REPLACE
     that block in place with the canonical block below (preserve everything
     outside the markers).
   - Otherwise, append the canonical block (markers included) after the
     project-specific introduction.
   - Adapt the tool names inside the block ONLY if this project demonstrably uses
     different tool names; otherwise keep them verbatim. Replace any "<project>"
     placeholder in example paths with this project's KB path prefix.

4. PRESERVE INTENT. Never delete project-specific rules, build/test commands, or
   architectural notes already in AGENTS.md. Merge, do not overwrite.

5. VERIFY. Re-read the final AGENTS.md to confirm it is well-formed Markdown, the
   MANDATORY sections are intact and clearly labelled, and project content is
   preserved.

6. DOCUMENT. After writing AGENTS.md, record a short summary of what you changed
   with kb_add_document (what changed, files touched, why, how verified), per the
   project's documentation rule.

===== CANONICAL MANDATORY BLOCK (insert verbatim between the markers) =====

`)
	b.WriteString(strings.TrimSpace(CanonicalTemplate))
	b.WriteString("\n\n===== END CANONICAL MANDATORY BLOCK =====\n")

	if strings.TrimSpace(extra) != "" {
		b.WriteString("\nADDITIONAL USER GUIDANCE FOR THIS RUN:\n")
		b.WriteString(strings.TrimSpace(extra))
		b.WriteString("\n")
	}
	return b.String()
}
