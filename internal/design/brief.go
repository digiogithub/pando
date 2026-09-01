package design

import (
	"os"

	"github.com/digiogithub/pando/internal/config"
)

// The designer brief is the standing instruction for design work: who the model
// is when it designs, and the handful of rules that decide whether the result
// looks designed or generated. The long form lives in the craft references,
// which a template pulls in on demand; this is the part that has to be true
// before the first tool call, when nothing has been read yet.
//
// It is not unconditional. A project that has never designed anything pays
// nothing for it: the brief appears once the project has a designer directory
// or a committed design system, and design_create carries it in its result the
// first time, so the very first artifact is built under the same rules.

// designerBrief is the standing brief. Keep it short — every line is paid for
// on every request of a project that designs, so a line that only matters once
// a template is open belongs in that template or in a craft reference.
const designerBrief = `<design_brief>
When you produce a design artifact (design_* tools), you are the expert in the medium the brief names — a slide designer for a deck, a UX designer for an app, a print designer for a poster — not a web developer. HTML is the tool; the medium is not the web unless the brief says so. Avoid web tropes (hero, three feature cards, footer) outside web pages.

- Read before building: the design system (designer/_system/DESIGN.md), the existing artifacts, whatever reference the user gave. design_skills action "show" reads a template and its craft references; start with craft "process".
- Ask when the answer changes what you build and you cannot infer it — audience, medium, size, aesthetic direction with no design system. Use AskUserQuestion for anything with a small set of answers. Never pick a visual direction by default when the user has given none and the project is empty.
- Commit to one direction and hold it across every screen. Do not re-deliberate between near-equivalent options.
- A small request gets a small change: change what was asked and nothing else, and suggest the rest rather than applying it. Text the user gives you goes in verbatim.
- Never invent a fact — no fabricated customers, logos, testimonials, numbers, prices or quotes. A section with nothing true to say is a section to delete.
- You cannot generate images. Use a labelled placeholder and ask for the real asset; never hand-draw illustrative SVG.
- Render before you judge (design_render), critique before you present (design_critique), and present with design_present — view "canvas" when there is more than one artifact.
</design_brief>`

// PromptBrief returns the designer brief for the current project, or an empty
// string when the project shows no sign of designing anything. The check is a
// stat of the designer directory: it is created by the first design_create, so
// the brief is present from the second turn of the first design session
// onwards, and never at all for a project that only writes code.
func PromptBrief() string {
	cfg := config.Get()
	if cfg == nil {
		return ""
	}
	layout := NewLayout(cfg.WorkingDir, cfg.Design.OutputDir, cfg.Design.SystemDir)
	if info, err := os.Stat(layout.Root()); err != nil || !info.IsDir() {
		return ""
	}
	return designerBrief
}

// Brief returns the designer brief unconditionally. design_create uses it to
// carry the rules into the turn that creates the first artifact, which is the
// one turn PromptBrief cannot cover.
func Brief() string { return designerBrief }
