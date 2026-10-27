package design

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

// selection is the element picked in the canvas or in the inspector (the
// Web UI's DesignSelection).
type selection struct {
	NodeID    string
	Selection string // design://<node_id>
	Tag       string
	Text      string
	Slide     int
}

// thumb is the screenshot of an artifact card.
type thumb struct {
	bmp    *ui.Bitmap
	failed bool
}

// store is the Design Studio state (web-ui designStore). Every field is owned
// by the UI thread; goroutines come back through app.Context.Update.
type store struct {
	a *app.Context

	status    *client.DesignStatus
	statusErr string

	artifacts []client.DesignArtifact
	loading   bool
	loaded    bool

	activeID string
	versions []client.DesignVersion
	nodes    []client.DesignNode
	nodesTot int
	sel      *selection
	slide    int

	rendering bool
	exporting bool
	// reloadNonce is bumped on every version or render event; the canvas
	// reloads when it changes.
	reloadNonce int

	critique        *client.DesignCritique
	decision        *client.DesignGateDecision
	critiqueSet     *client.DesignCritiqueSettings
	critiqueRunning bool
	critiqueLoaded  bool

	templates       []client.DesignTemplate
	craft           []string
	templatesLoaded bool
	templatesBusy   bool

	thumbs map[string]*thumb

	// autoOpened remembers artifacts the UI already navigated to on
	// design.created, so a reconnect or a repeated event does not steal the
	// view twice.
	autoOpened map[string]bool
	connected  bool
}

var (
	storesMu sync.Mutex
	stores   = map[*app.Context]*store{}
)

// storeFor returns the store of an app context, creating it.
func storeFor(a *app.Context) *store {
	storesMu.Lock()
	defer storesMu.Unlock()
	s, ok := stores[a]
	if !ok {
		s = &store{a: a, thumbs: map[string]*thumb{}, autoOpened: map[string]bool{}}
		stores[a] = s
	}
	return s
}

// start runs at the first frame: it reads the status and follows the design
// events for the whole life of the app, so a design created by the agent
// opens in the native UI whichever area is showing.
func (s *store) start() {
	s.fetchStatus()
	s.a.Go(func(ctx context.Context) {
		_ = s.a.Client.SubscribeDesignEvents(ctx, func(ev client.DesignEvent) {
			s.a.Update(func() { s.onEvent(ev) })
		})
	})
}

func (s *store) enabled() bool { return s.status != nil && s.status.Enabled }

func (s *store) fetchStatus() {
	s.a.Go(func(ctx context.Context) {
		st, err := s.a.Client.DesignStatus(ctx)
		s.a.Update(func() {
			if err != nil {
				s.statusErr = err.Error()
				return
			}
			s.status = &st
			s.statusErr = ""
			if st.Enabled {
				s.fetchArtifacts()
			}
		})
	})
}

func (s *store) fetchArtifacts() {
	s.loading = true
	s.a.Go(func(ctx context.Context) {
		list, err := s.a.Client.DesignArtifacts(ctx)
		s.a.Update(func() {
			s.loading = false
			s.loaded = true
			if err != nil {
				s.artifacts = nil
				return
			}
			sort.SliceStable(list, func(i, j int) bool { return list[i].CreatedAt > list[j].CreatedAt })
			s.artifacts = list
		})
	})
}

// artifact returns the artifact with id.
func (s *store) artifact(id string) (client.DesignArtifact, bool) {
	for _, a := range s.artifacts {
		if a.ID == id {
			return a, true
		}
	}
	return client.DesignArtifact{}, false
}

// openArtifact makes id the open artifact and loads versions, nodes and
// critique. The route drives it, never the other way round.
func (s *store) openArtifact(id string) {
	s.activeID = id
	s.sel, s.slide = nil, 0
	s.nodes, s.versions, s.nodesTot = nil, nil, 0
	s.critique, s.decision, s.critiqueLoaded = nil, nil, false
	s.a.Go(func(ctx context.Context) {
		art, err := s.a.Client.DesignArtifact(ctx, id)
		if err == nil {
			s.a.Update(func() { s.upsertArtifact(art) })
		}
	})
	s.fetchVersions(id)
	s.fetchNodes(id, "")
	s.fetchCritique(id)
}

func (s *store) closeArtifact() {
	s.activeID = ""
	s.sel, s.slide = nil, 0
	s.nodes, s.versions, s.nodesTot = nil, nil, 0
	s.critique, s.decision, s.critiqueLoaded = nil, nil, false
}

func (s *store) upsertArtifact(art client.DesignArtifact) {
	for i := range s.artifacts {
		if s.artifacts[i].ID == art.ID {
			s.artifacts[i] = art
			return
		}
	}
	s.artifacts = append([]client.DesignArtifact{art}, s.artifacts...)
}

func (s *store) fetchVersions(id string) {
	s.a.Go(func(ctx context.Context) {
		vs, err := s.a.Client.DesignVersions(ctx, id)
		s.a.Update(func() {
			if s.activeID != id {
				return
			}
			if err != nil {
				s.versions = nil
				return
			}
			// Newest first: the timeline reads top-down.
			sort.SliceStable(vs, func(i, j int) bool { return vs[i].Number > vs[j].Number })
			s.versions = vs
		})
	})
}

// fetchNodes reads the structure index; nodeID narrows it to one node whose
// styles are wanted (the result is merged into the list).
func (s *store) fetchNodes(id, nodeID string) {
	s.a.Go(func(ctx context.Context) {
		q := client.DesignNodesQuery{NodeID: nodeID, Styles: nodeID != ""}
		nodes, total, err := s.a.Client.DesignNodes(ctx, id, q)
		s.a.Update(func() {
			if s.activeID != id {
				return
			}
			if err != nil {
				if nodeID == "" {
					s.nodes, s.nodesTot = nil, 0
				}
				return
			}
			if nodeID == "" {
				s.nodes, s.nodesTot = nodes, total
				return
			}
			for _, n := range nodes {
				for i := range s.nodes {
					if s.nodes[i].NodeID == n.NodeID {
						s.nodes[i] = n
					}
				}
			}
		})
	})
}

func (s *store) fetchCritique(id string) {
	s.a.Go(func(ctx context.Context) {
		st, err := s.a.Client.DesignCritique(ctx, id)
		s.a.Update(func() {
			if s.activeID != id {
				return
			}
			s.critiqueLoaded = true
			if err != nil {
				return
			}
			s.critique = nil
			if st.Exists {
				s.critique = st.Critique
			}
			s.decision = st.Decision
			settings := st.Settings
			s.critiqueSet = &settings
		})
	})
}

func (s *store) runCritique(id string) {
	if s.critiqueRunning {
		return
	}
	s.critiqueRunning = true
	s.a.Go(func(ctx context.Context) {
		rep, err := s.a.Client.DesignRunCritique(ctx, id, "")
		s.a.Update(func() {
			s.critiqueRunning = false
			if err != nil {
				s.a.Toasts.Error("Critique failed", err.Error())
				return
			}
			if s.activeID == id {
				crit, dec, set := rep.Critique, rep.Decision, rep.Settings
				s.critique, s.decision, s.critiqueSet, s.critiqueLoaded = &crit, &dec, &set, true
			}
			if !rep.Rendered && rep.RenderError != "" {
				// A pass that could not render scored only part of the
				// artifact: say so.
				s.a.Toasts.Info("Partial critique", rep.RenderError)
			}
		})
	})
}

func (s *store) render(id string) {
	if s.rendering {
		return
	}
	s.rendering = true
	s.a.Go(func(ctx context.Context) {
		err := s.a.Client.DesignRender(ctx, id)
		s.a.Update(func() {
			s.rendering = false
			if err != nil {
				s.a.Toasts.Error("Render failed", err.Error())
				return
			}
			s.fetchNodes(id, "")
			s.reloadNonce++
		})
	})
}

func (s *store) checkout(id string, version int) {
	s.a.Go(func(ctx context.Context) {
		art, err := s.a.Client.DesignCheckout(ctx, id, version)
		s.a.Update(func() {
			if err != nil {
				s.a.Toasts.Error("Checkout failed", err.Error())
				return
			}
			s.upsertArtifact(art)
			s.reloadNonce++
			s.sel = nil
			s.fetchVersions(id)
			s.fetchNodes(id, "")
			s.a.Toasts.Success("Checked out version", "v"+itoa(version))
		})
	})
}

func (s *store) fetchTemplates() {
	if s.templatesBusy {
		return
	}
	s.templatesBusy = true
	s.a.Go(func(ctx context.Context) {
		tpls, craft, err := s.a.Client.DesignTemplates(ctx)
		s.a.Update(func() {
			s.templatesBusy = false
			s.templatesLoaded = true
			if err != nil {
				s.templates, s.craft = nil, nil
				return
			}
			s.templates, s.craft = tpls, craft
		})
	})
}

func (s *store) installTemplate(name string) {
	s.a.Go(func(ctx context.Context) {
		err := s.a.Client.DesignInstallTemplate(ctx, name, "project")
		s.a.Update(func() {
			if err != nil {
				s.a.Toasts.Error("Install failed", err.Error())
				return
			}
			s.a.Toasts.Success("Installed", name)
			s.templatesBusy = false
			s.fetchTemplates()
		})
	})
}

// onEvent reacts to /design/events (UI thread).
func (s *store) onEvent(ev client.DesignEvent) {
	switch ev.Kind {
	case client.DesignEventCreated:
		if ev.ArtifactID != "" && !s.autoOpened[ev.ArtifactID] {
			s.autoOpened[ev.ArtifactID] = true
			title := strings.TrimSpace(ev.Title)
			if title == "" {
				title = ev.ArtifactID
			}
			s.a.Toasts.Info(s.a.T("design.title"), title)
			path := "/design/" + ev.ArtifactID
			if s.a.Router.Path() != path {
				s.a.Navigate(path)
			}
		}
		s.fetchArtifacts()
		return
	case client.DesignEventVersion, client.DesignEventRender:
		if ev.ArtifactID != "" && ev.ArtifactID == s.activeID {
			// The document on disk moved under the open canvas: reload it
			// and re-read the index the agent just produced, or a click
			// would resolve against stale ids.
			s.fetchVersions(ev.ArtifactID)
			s.fetchNodes(ev.ArtifactID, "")
			s.reloadNonce++
		}
	case client.DesignEventCritique:
		if ev.ArtifactID != "" && ev.ArtifactID == s.activeID {
			s.fetchCritique(ev.ArtifactID)
		}
	}
	if ev.ArtifactID != "" {
		// Drop the card screenshot: the version changed.
		for k := range s.thumbs {
			if strings.HasPrefix(k, ev.ArtifactID+"@") {
				delete(s.thumbs, k)
			}
		}
	}
	s.fetchArtifacts()
}

// thumbFor returns the card screenshot of an artifact, fetching it once per
// version (nil while loading or when the server has no headless browser).
func (s *store) thumbFor(art client.DesignArtifact) *thumb {
	key := art.ID + "@" + itoa(art.CurrentVersion) + "@" + art.UpdatedAt
	if th, ok := s.thumbs[key]; ok {
		return th
	}
	th := &thumb{}
	s.thumbs[key] = th
	s.a.Go(func(ctx context.Context) {
		data, err := s.a.Client.DesignScreenshot(ctx, art.ID)
		s.a.Update(func() {
			if err != nil || len(data) == 0 {
				th.failed = true
				return
			}
			bmp, derr := ui.DecodeBitmap(data)
			if derr != nil {
				th.failed = true
				return
			}
			th.bmp = bmp
		})
	})
	return th
}
