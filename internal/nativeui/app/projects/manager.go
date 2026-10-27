// Package projects is the native Projects area and the project workspaces:
// the registry of projects (add, rename, stop, delete, init) and the tabs
// that open a project's child instance inside the shell.
//
// A workspace tab does what the Web UI does with an iframe, without a web
// view: the parent server runs the project as a child `pando serve` and
// proxies it under /api/v1/projects/{id}/web/. The tab derives a client for
// that prefix (client.Client.Derive: same transport and parent token, the
// proxy swaps in the child's token), builds a child app.Context with
// Context.ForClient and shows a full embedded app.Root (sidebar, chat,
// files, ...) against it. Any area works unchanged inside a workspace
// because areas only talk to Context.Client.
package projects

import (
	"context"
	"os"
	"sort"
	"sync"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

func init() {
	app.RegisterShellExt(func(a *app.Context) app.ShellExt { return managerFor(a) })
	app.OnStart(func(a *app.Context) { managerFor(a).start() })
}

var (
	mgrMu sync.Mutex
	mgrs  = map[*app.Context]*manager{}
)

// managerFor returns the manager of an app context, creating it once.
func managerFor(a *app.Context) *manager {
	mgrMu.Lock()
	defer mgrMu.Unlock()
	if m, ok := mgrs[a]; ok {
		return m
	}
	m := &manager{a: a}
	mgrs[a] = m
	return m
}

// Workspace tab states (web-ui ProjectTab.state).
const (
	stateStarting = "starting"
	stateRunning  = "running"
	stateStopped  = "stopped"
	stateError    = "error"
)

type wsTab struct {
	id, name, path string
	state          string
	errMsg         string
	port           int
	delegations    int

	root   *app.Root
	cancel context.CancelFunc
}

// manager holds the projects list and the open workspace tabs of one window.
// Every field is touched on the UI thread only.
type manager struct {
	a *app.Context

	started   bool
	devOpened bool
	projects  []client.Project
	target    string // delegation target project ID
	loaded    bool
	loading   bool
	loadErr   string

	tabs   []*wsTab
	active string // focused tab ID, "" = main

	// Dialogs owned by the manager (drawn through Dialogs).
	initFor     *client.Project
	initOpen    bool
	initBusy    bool
	closeFor    string
	closeOpen   bool
	closeChoice string // "stop" | "keep" | "" (ask)
}

// start loads the open instances and follows the project events (once).
func (m *manager) start() {
	if m.started {
		return
	}
	m.started = true
	m.refresh()
	m.a.Go(func(ctx context.Context) {
		inst, err := m.a.Client.ListProjectWebInstances(ctx)
		if err == nil {
			m.a.Update(func() { m.restore(inst) })
		}
		_ = m.a.Client.SubscribeProjectEvents(ctx, func(ev client.ProjectEvent) {
			m.a.Update(func() { m.applyEvent(ev) })
		})
	})
}

// restore adopts the instances already running (a reopened window).
func (m *manager) restore(inst []client.ProjectWebInstance) {
	for _, in := range inst {
		if m.find(in.ProjectID) != nil || (in.State != stateRunning && in.State != stateStarting) {
			continue
		}
		m.tabs = append(m.tabs, &wsTab{id: in.ProjectID, name: orID(in.Name, in.ProjectID), path: in.Path,
			state: in.State, port: in.WebPort, delegations: in.Delegations})
	}
}

// refresh reloads the project list and the delegation target.
func (m *manager) refresh() {
	if m.loading {
		return
	}
	m.loading = true
	m.a.Go(func(ctx context.Context) {
		ps, err := m.a.Client.ListProjects(ctx)
		var target string
		if err == nil {
			if p, e := m.a.Client.ActiveProject(ctx); e == nil && p != nil {
				target = p.ID
			}
		}
		m.a.Update(func() {
			m.loading, m.loaded = false, true
			if err != nil {
				m.loadErr = err.Error()
				return
			}
			m.loadErr = ""
			sort.SliceStable(ps, func(i, j int) bool { return ps[i].LastOpened > ps[j].LastOpened })
			m.projects, m.target = ps, target
			if want := os.Getenv("PANDO_NATIVE_OPEN_PROJECT"); want != "" && !m.devOpened {
				// Development aid: open this project (name, path or ID) at start.
				for _, p := range ps {
					if p.Name == want || p.Path == want || p.ID == want {
						m.devOpened = true
						m.Open(p.ID)
					}
				}
			}
			for _, p := range ps {
				if t := m.find(p.ID); t != nil {
					t.delegations = p.Delegations
					t.name = orID(p.Name, p.ID)
				}
			}
		})
	})
}

func (m *manager) applyEvent(ev client.ProjectEvent) {
	switch ev.Type {
	case "web_stopped":
		if t := m.find(ev.ProjectID); t != nil && t.state != stateStarting {
			m.dropRuntime(t)
			t.state = stateStopped
		}
	case "web_error":
		if t := m.find(ev.ProjectID); t != nil {
			m.dropRuntime(t)
			t.state, t.errMsg = stateError, ev.Error
		}
	}
	m.refresh()
}

func (m *manager) find(id string) *wsTab {
	for _, t := range m.tabs {
		if t.id == id {
			return t
		}
	}
	return nil
}

func (m *manager) project(id string) (client.Project, bool) {
	for _, p := range m.projects {
		if p.ID == id {
			return p, true
		}
	}
	return client.Project{}, false
}

func orID(name, id string) string {
	if name != "" {
		return name
	}
	return id
}

// dropRuntime stops the embedded app of a tab (streams, goroutines).
func (m *manager) dropRuntime(t *wsTab) {
	if t.root != nil {
		t.root.Close()
		t.root = nil
	}
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
}

// Open starts (or focuses) the workspace tab of a project.
func (m *manager) Open(id string) {
	t := m.find(id)
	if t != nil && (t.state == stateRunning || t.state == stateStarting) {
		m.active = id
		return
	}
	if t == nil {
		p, _ := m.project(id)
		t = &wsTab{id: id, name: orID(p.Name, id), path: p.Path}
		m.tabs = append(m.tabs, t)
	}
	t.state, t.errMsg = stateStarting, ""
	m.active = id
	m.a.Go(func(ctx context.Context) {
		res, err := m.a.Client.OpenProjectWeb(ctx, id)
		m.a.Update(func() { m.opened(id, res, err) })
	})
}

func (m *manager) opened(id string, res client.ProjectOpenResult, err error) {
	t := m.find(id)
	if t == nil {
		return
	}
	if err == nil {
		t.state, t.port = stateRunning, res.WebPort
		m.refresh()
		return
	}
	pe := client.ParseProjectError(err)
	switch pe.Error {
	case "project_needs_init":
		p, ok := m.project(id)
		if !ok {
			p = client.Project{ID: id, Name: t.name, Path: orID(pe.Path, t.path)}
		}
		m.initFor, m.initOpen, m.initBusy = &p, true, false
	case "web_instance_limit":
		m.a.Toasts.Error(m.a.T("projects.tabs.limitReached", "limit", pe.Limit), "")
		m.removeTab(id)
	default:
		t.state = stateError
		t.errMsg = orID(pe.Detail, pe.Error)
		m.a.Toasts.Error(m.a.T("projects.tabs.error"), t.errMsg)
	}
}

// confirmInit initializes the project of the init dialog, then opens it.
func (m *manager) confirmInit() {
	p := m.initFor
	if p == nil || m.initBusy {
		return
	}
	m.initBusy = true
	m.a.Go(func(ctx context.Context) {
		err := m.a.Client.InitProject(ctx, p.ID)
		m.a.Update(func() {
			m.initBusy = false
			if err != nil {
				m.a.Toasts.Error(m.a.T("projects.tabs.error"), err.Error())
				return
			}
			m.initOpen, m.initFor = false, nil
			m.refresh()
			if t := m.find(p.ID); t != nil {
				t.state = stateStopped
			}
			m.Open(p.ID)
		})
	})
}

// cancelInit gives up opening an uninitialized project.
func (m *manager) cancelInit() {
	if m.initFor != nil {
		m.removeTab(m.initFor.ID)
	}
	m.initOpen, m.initFor = false, nil
}

func (m *manager) removeTab(id string) {
	for i, t := range m.tabs {
		if t.id == id {
			m.dropRuntime(t)
			m.tabs = append(m.tabs[:i], m.tabs[i+1:]...)
			break
		}
	}
	if m.active == id {
		m.active = ""
	}
}

// CloseAndStop removes the tab and stops its child instance.
func (m *manager) closeWith(id string, stop bool) {
	m.removeTab(id)
	if !stop {
		return
	}
	m.a.Go(func(ctx context.Context) {
		if err := m.a.Client.CloseProjectWeb(ctx, id); err != nil {
			m.a.Toasts.Error(m.a.T("projects.tabs.error"), err.Error())
			return
		}
		m.a.Toasts.Info(m.a.T("projects.tabs.stopped"), "")
		m.a.Update(m.refresh)
	})
}

// StopProject stops every instance of a project and closes its tab.
func (m *manager) StopProject(id string, done func(error)) {
	m.a.Go(func(ctx context.Context) {
		n, err := m.a.Client.StopProject(ctx, id)
		m.a.Update(func() {
			if err == nil {
				if n > 0 {
					m.a.Toasts.Info(m.a.T("projects.toasts.cancelledDelegations", "count", n), "")
				} else {
					m.a.Toasts.Success(m.a.T("projects.toasts.stopped"), "")
				}
				m.removeTab(id)
				m.refresh()
			}
			if done != nil {
				done(err)
			}
		})
	})
}

// Tabs implements app.ShellExt.
func (m *manager) Tabs() []app.ShellTab {
	out := make([]app.ShellTab, 0, len(m.tabs))
	for _, t := range m.tabs {
		out = append(out, app.ShellTab{ID: t.id, Title: t.name, Detail: t.path, State: t.state, Busy: t.delegations > 0})
	}
	return out
}

// Active implements app.ShellExt.
func (m *manager) Active() string { return m.active }

// Select implements app.ShellExt.
func (m *manager) Select(id string) {
	if id == "" || m.find(id) != nil {
		m.active = id
	}
}

// Close implements app.ShellExt: ask stop/keep for a live workspace.
func (m *manager) Close(id string) {
	t := m.find(id)
	if t == nil {
		return
	}
	if t.state != stateRunning && t.state != stateStarting {
		m.removeTab(id)
		return
	}
	switch m.closeChoice {
	case "stop":
		m.closeWith(id, true)
	case "keep":
		m.closeWith(id, false)
	default:
		m.closeFor, m.closeOpen = id, true
	}
}

// ensureRoot builds the embedded app of a running tab.
func (m *manager) ensureRoot(t *wsTab) *app.Root {
	if t.root != nil {
		return t.root
	}
	ctx, cancel := context.WithCancel(m.a.Ctx)
	child := m.a.ForClient(m.a.Client.Derive(client.ProjectWebPrefix(t.id)), t.path)
	child.Ctx = ctx
	if p := os.Getenv("PANDO_NATIVE_CHILD_PATH"); p != "" {
		child.Router.Replace(p) // development aid: open this route in workspaces
	}
	t.root, t.cancel = app.NewEmbeddedRoot(child, t.name), cancel
	return t.root
}

// Build implements app.ShellExt.
func (m *manager) Build(c *ui.Context, id string) { m.buildTab(c, id) }
