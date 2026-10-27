package app

import (
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// The areas of the Web UI. Each is a placeholder until its package under
// app/<area> registers the real view with the same ID (P1-P4 of the plan).
func init() {
	for _, ar := range []Area{
		{ID: "chat", Path: "/chat", TitleKey: "nav.chat", Icon: kit.IconChat, Section: SectionMain, Order: 0},
		{ID: "simple-chat", Path: "/chat/simple", TitleKey: "nav.simpleChat", Icon: kit.IconSimpleChat, Section: SectionMain, Order: 1},
		{ID: "projects", Path: "/projects", TitleKey: "nav.projects", Icon: kit.IconProjects, Section: SectionMain, Order: 2},
		{ID: "editor", Path: "/editor", Patterns: []string{"/editor/{path...}"}, TitleKey: "nav.codeEditor", Icon: kit.IconEditor, Section: SectionTools, Order: 0},
		{ID: "terminal", Path: "/terminal", TitleKey: "nav.terminal", Icon: kit.IconTerminal, Section: SectionTools, Order: 1},
		{ID: "orchestrator", Path: "/orchestrator", TitleKey: "nav.orchestrator", Icon: kit.IconOrchestrate, Section: SectionTools, Order: 2},
		{ID: "design", Path: "/design", Patterns: []string{"/design/{id...}"}, TitleKey: "nav.design", Icon: kit.IconDesign, Section: SectionTools, Order: 3},
		{ID: "logs", Path: "/logs", TitleKey: "nav.logs", Icon: kit.IconLogs, Section: SectionTools, Order: 4},
		{ID: "agentvcs", Path: "/snapshots", Patterns: []string{"/snapshots/{id...}"}, TitleKey: "nav.agentVcs", Icon: kit.IconAgentVCS, Section: SectionTools, Order: 5},
		{ID: "evaluator", Path: "/evaluator", TitleKey: "nav.selfImprovement", Icon: kit.IconEvaluator, Section: SectionTools, Order: 6},
		{ID: "instances", Path: "/instances", TitleKey: "nav.instances", Icon: kit.IconInstances, Section: SectionServices, Order: 0},
		{ID: "extensions", Path: "/extensions", TitleKey: "Extensions", Icon: kit.IconExtensions, Section: SectionServices, Order: 1},
		{ID: "settings", Path: "/settings", Patterns: []string{"/settings/{section...}"}, TitleKey: "nav.settings", Icon: kit.IconSettings, Section: SectionBottom, Order: 0},
		{ID: "setup", Path: "/setup", TitleKey: "Setup", Icon: kit.IconSetup, Section: SectionHidden, Order: 0},
	} {
		ar.Placeholder = true
		title, icon := ar.TitleKey, ar.Icon
		ar.New = func(a *Context) View { return &placeholder{app: a, titleKey: title, icon: icon} }
		Register(ar)
	}
}

type placeholder struct {
	app      *Context
	titleKey string
	icon     string
}

func (p *placeholder) Build(c *ui.Context, r *ui.Route) {
	kit.EmptyState(c, p.icon, p.app.T(p.titleKey),
		"This area is not available in the native desktop yet. Use the browser UI (pando app) meanwhile.")
}
