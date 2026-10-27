// Package design is the Design Studio area of the native desktop: the
// artifact and template galleries at /design and one artifact's Studio at
// /design/{id} (web-ui components/design + designStore).
//
// Native chrome (galleries, inspector, issues, timeline, export) is MyGo
// widgets. The preview canvas stays HTML served by the preview server and is
// shown in MyGo's embedded web view (kit.WebPane); the selection bridge talks
// to it through Page.Eval (canvas.go). Design events (/design/events) keep the
// store fresh for the whole life of the app, and a design the agent creates
// opens in this area (replacing the Wails per-artifact windows).
package design

import (
	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

func init() {
	app.Register(app.Area{
		ID: "design", Path: "/design", Patterns: []string{"/design", "/design/{id...}"},
		TitleKey: "nav.design", Icon: kit.IconDesign, Section: app.SectionTools, Order: 3,
		New: func(a *app.Context) app.View { return newView(a) },
	})
	app.OnStart(func(a *app.Context) { storeFor(a).start() })
}

// View is the area's view.
type View struct {
	a *app.Context
	s *store

	tab        int // 0 artifacts, 1 templates
	sideTab    int // 0 inspector, 1 versions
	inspTab    int // 0 structure, 1 issues
	filter     string
	nodeList   ui.ListState
	issueList  ui.ListState
	verList    ui.ListState
	tplScroll  ui.ScrollState
	artGrid    ui.GridState
	sideHidden bool

	cv *canvas
}

func newView(a *app.Context) *View {
	s := storeFor(a)
	return &View{a: a, s: s}
}

// Title shows the open artifact's title in the header.
func (v *View) Title() string {
	if v.s.activeID != "" {
		if art, ok := v.s.artifact(v.s.activeID); ok && art.Title != "" {
			return v.a.T("design.title") + " / " + art.Title
		}
	}
	return v.a.T("design.title")
}

// Close stops the canvas poller.
func (v *View) Close() {
	if v.cv != nil {
		v.cv.close()
	}
}

// Build routes between the gallery, the Studio and the disabled notice. The
// route is the source of truth for the open artifact.
func (v *View) Build(c *ui.Context, r *ui.Route) {
	t := kit.Current()
	s := v.s
	if s.status == nil {
		if s.statusErr != "" {
			kit.EmptyState(c, kit.IconError, v.a.T("design.title"), s.statusErr)
			return
		}
		ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() { kit.Spinner(c).TextColor(t.FgFaint) })
		return
	}
	if !s.status.Enabled {
		v.disabled(c, t)
		return
	}
	id := r.Param("id")
	if id != "" && id != s.activeID {
		s.openArtifact(id)
	} else if id == "" && s.activeID != "" {
		s.closeArtifact()
	}
	if id == "" {
		v.gallery(c, t)
		return
	}
	art, ok := s.artifact(id)
	if !ok {
		if s.loading || !s.loaded {
			ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() { kit.Spinner(c).TextColor(t.FgFaint) })
			return
		}
		ui.Column(c).Grow(1).Padding(32).Gap(12).Children(func() {
			ui.Text(c, v.a.T("design.notFound")).FontSize(t.TextSM).TextColor(t.FgMuted)
			if kit.Button(c.Key("nf-back"), kit.ButtonOpts{Icon: "arrow-left", Label: v.a.T("design.backToGallery")}).Clicked() {
				v.a.Navigate("/design")
			}
		})
		return
	}
	v.studio(c, t, art)
}

func (v *View) disabled(c *ui.Context, t *kit.Tokens) {
	ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(10).Padding(32).Children(func() {
		kit.Icon(c, kit.IconDesign).FontSize(32).TextColor(t.FgFaint)
		ui.Text(c, v.a.T("design.disabledTitle")).FontSize(t.TextLG).FontWeight(600)
		ui.Text(c, v.a.T("design.disabledBody")).FontSize(t.TextSM).TextColor(t.FgMuted).TextAlign(ui.Center).MaxWidth(520)
		kit.CodeBlock(c, "toml", "[design]\nEnabled = true", nil)
	})
}
