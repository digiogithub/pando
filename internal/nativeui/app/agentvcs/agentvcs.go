// Package agentvcs is the Agent VCS area (route /snapshots, as in the Web
// UI): the sessions that have agent commits, their commit log, the files each
// commit changed with a side-by-side diff viewer (MyGo ui/editor DiffView),
// revert of a commit or of single files, and the named project snapshots.
package agentvcs

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// Icon files (kit/icons).
const (
	iconLayers = "layers"
	iconUndo   = "rotate-ccw"
	iconCode   = "file-code"
	iconTrash  = "trash"
)

const refreshEvery = 30 * time.Second

func init() {
	app.Register(app.Area{
		ID: "agentvcs", Path: "/snapshots", Patterns: []string{"/snapshots", "/snapshots/{id...}"},
		TitleKey: "nav.agentVcs", Icon: kit.IconAgentVCS, Section: app.SectionTools, Order: 5,
		New: func(a *app.Context) app.View { return New(a) },
	})
}

// modal is a confirmation or a name prompt.
type modal struct {
	open    bool
	title   string
	message string
	value   string
	prompt  bool
	ok      string
	danger  bool
	onOK    func(value string)
}

// View is the area's state (UI thread only).
type View struct {
	app *app.Context

	tab int // 0 commits, 1 snapshots

	sessions    []client.VCSSession
	sessLoading bool
	sessList    ui.ListState
	selSession  string

	commits    []client.VCSCommit // newest first
	logLoading bool
	commitList ui.ListState
	selCommit  string
	detail     *client.VCSCommitDetail
	detailBusy bool
	selected   map[string]bool
	fileList   ui.ListState
	loadErr    string
	logSeq     int
	detailSeq  int

	diff        *diffView
	pendingFile string

	snapshots   []client.Snapshot
	snapLoading bool
	snapList    ui.ListState

	started   bool
	lastBuilt time.Time
	param     string
	m         modal
}

// New returns the Agent VCS view.
func New(a *app.Context) *View {
	v := &View{app: a, selected: map[string]bool{}}
	v.sessList.Key = func(i int) any { return v.sessions[i].SessionID }
	v.commitList.Key = func(i int) any { return v.commits[i].ID }
	v.fileList.Key = func(i int) any {
		if v.detail != nil && i < len(v.detail.Diff) {
			return v.detail.Diff[i].Path
		}
		return i
	}
	v.snapList.Key = func(i int) any { return v.snapshots[i].ID }
	return v
}

func (v *View) start() {
	if v.started || v.app.Client == nil {
		return
	}
	v.started = true
	v.fetchSessions()
	v.fetchSnapshots()
	v.app.Go(func(ctx context.Context) {
		tk := time.NewTicker(refreshEvery)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
			}
			v.app.Update(func() {
				if time.Since(v.lastBuilt) > 2*time.Second {
					return
				}
				v.fetchSessions()
				v.fetchSnapshots()
			})
		}
	})
}

func (v *View) fetchSessions() {
	v.sessLoading = len(v.sessions) == 0
	v.app.Go(func(ctx context.Context) {
		list, err := v.app.Client.VCSSessions(ctx)
		v.app.Update(func() {
			v.sessLoading = false
			if err != nil {
				v.sessions = nil
				return
			}
			v.sessions = list
		})
	})
}

func (v *View) fetchSnapshots() {
	v.snapLoading = len(v.snapshots) == 0
	v.app.Go(func(ctx context.Context) {
		list, err := v.app.Client.Snapshots(ctx)
		v.app.Update(func() {
			v.snapLoading = false
			if err != nil {
				v.snapshots = nil
				return
			}
			v.snapshots = list
		})
	})
}

// SelectSession shows the commit log of a session.
func (v *View) SelectSession(id string) {
	v.selSession, v.selCommit, v.detail = id, "", nil
	v.selected = map[string]bool{}
	v.commits, v.loadErr = nil, ""
	v.logLoading = true
	v.logSeq++
	seq := v.logSeq
	v.app.Go(func(ctx context.Context) {
		log, err := v.app.Client.VCSLog(ctx, id)
		v.app.Update(func() {
			if seq != v.logSeq {
				return
			}
			v.logLoading = false
			if err != nil {
				v.loadErr = err.Error()
				return
			}
			// Newest first.
			for i, j := 0, len(log)-1; i < j; i, j = i+1, j-1 {
				log[i], log[j] = log[j], log[i]
			}
			v.commits = log
		})
	})
}

// openLink follows a deep link: "<session>", "<session>/<commit>" or
// "<session>/<commit>/<file path>" (the last opens the diff of that file).
func (v *View) openLink(id string) {
	parts := strings.SplitN(id, "/", 3)
	v.tab = 0
	if parts[0] != v.selSession {
		v.SelectSession(parts[0])
	}
	if len(parts) > 1 && parts[1] != "" {
		v.pendingFile = ""
		if len(parts) > 2 {
			v.pendingFile = parts[2]
		}
		v.SelectCommit(parts[1])
	}
}

// SelectCommit loads the files a commit changed.
func (v *View) SelectCommit(id string) {
	v.selCommit = id
	v.selected = map[string]bool{}
	v.detailBusy = true
	v.detailSeq++
	seq := v.detailSeq
	v.app.Go(func(ctx context.Context) {
		d, err := v.app.Client.VCSCommitDetail(ctx, id)
		v.app.Update(func() {
			if seq != v.detailSeq {
				return
			}
			v.detailBusy = false
			if err != nil {
				v.detail = nil
				v.loadErr = err.Error()
				return
			}
			v.detail = &d
			if f := v.pendingFile; f != "" {
				v.pendingFile = ""
				for _, e := range d.Diff {
					if e.Path == f {
						v.openDiff(d.Commit.ID, e)
					}
				}
			}
		})
	})
}

// Title is the header title.
func (v *View) Title() string { return v.app.T("nav.agentVcs") }

// Build lays out the area.
func (v *View) Build(c *ui.Context, r *ui.Route) {
	v.start()
	v.lastBuilt = time.Now()
	if r != nil {
		if id := strings.Trim(r.Param("id"), "/"); id != "" && id != v.param {
			v.param = id
			v.openLink(id)
		}
	}
	t := kit.Current()
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		if v.diff != nil {
			v.diff.build(c, t, v)
			return
		}
		ui.Row(c).Padding(8, 16).Gap(12).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			ui.Segmented(c.Key("vcs:tab"), &v.tab, "Commits", "Snapshots").Label("View")
			ui.Box(c).Grow(1)
			if v.tab == 1 {
				if kit.Button(c.Key("vcs:newsnap"), kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Icon: kit.IconPlus, Label: "Create Snapshot"}).Clicked() {
					v.promptSnapshot()
				}
			}
		})
		if v.tab == 1 {
			v.snapshotsView(c, t)
			return
		}
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			v.sessionsPanel(c, t)
			v.commitsPanel(c, t)
			v.detailPanel(c, t)
		})
	})
	v.modalDialog(c)
}

func panel(c *ui.Context, t *kit.Tokens, w float32) ui.Element {
	e := ui.Column(c).AlignItems(ui.Stretch).Shrink(0).BorderWidth(0, 1, 0, 0).BorderColor(t.Border)
	if w > 0 {
		e.Width(w).Background(t.BgShell)
	}
	return e
}

func panelHeader(c *ui.Context, t *kit.Tokens, icon, text string) {
	ui.Row(c).Gap(6).Padding(10, 12).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		kit.Icon(c, icon).FontSize(12).TextColor(t.FgFaint)
		ui.Text(c, strings.ToUpper(text)).FontSize(t.TextXS).FontWeight(600).LetterSpacing(0.6).TextColor(t.FgFaint)
	})
}

func (v *View) sessionsPanel(c *ui.Context, t *kit.Tokens) {
	panel(c, t, 220).Children(func() {
		panelHeader(c, t, iconLayers, fmt.Sprintf("Sessions (%d)", len(v.sessions)))
		if v.sessLoading && len(v.sessions) == 0 {
			kit.Spinner(c).FontSize(20).TextColor(t.FgFaint).Margin(16)
			return
		}
		if len(v.sessions) == 0 {
			ui.Text(c, "No sessions with commits yet.").FontSize(t.TextSM).TextColor(t.FgMuted).Padding(12)
			return
		}
		ui.List(c.Key("vcs:sessions"), &v.sessList, len(v.sessions), func(i int) {
			s := v.sessions[i]
			active := s.SessionID == v.selSession
			row := ui.Row(c).Gap(6).Padding(7, 10).Radius(t.RadiusSM).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Label(s.SessionID)
			if row.Clicked() {
				v.SelectSession(s.SessionID)
			}
			if active {
				row.Background(t.AccentSoft)
			} else if row.Hovered() {
				row.Background(t.BgHover)
			}
			row.Children(func() {
				id := s.SessionID
				if len(id) > 8 {
					id = id[:8] + "…"
				}
				col := ui.Column(c).Grow(1).Shrink(1).MinWidth(0)
				col.Children(func() {
					tx := ui.Text(c, id).FontSize(t.TextSM).Font("monospace").SingleLine()
					if active {
						tx.FontWeight(600).TextColor(t.Accent)
					}
					ui.Text(c, plural(s.CommitCount, "commit")).FontSize(t.TextXS).TextColor(t.FgFaint)
				})
			})
		}).Grow(1).Padding(4, 6).Gap(1)
	})
}

func plural(n int, w string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, w)
	}
	return fmt.Sprintf("%d %ss", n, w)
}

func (v *View) commitsPanel(c *ui.Context, t *kit.Tokens) {
	panel(c, t, 340).Children(func() {
		panelHeader(c, t, kit.IconClock, fmt.Sprintf("Commit Log (%d)", len(v.commits)))
		switch {
		case v.logLoading:
			kit.Spinner(c).FontSize(20).TextColor(t.FgFaint).Margin(16)
		case len(v.commits) == 0:
			msg := "Select a session to view its commit history."
			if v.loadErr != "" {
				msg = v.loadErr
			}
			ui.Text(c, "No commits").FontSize(t.TextBase).FontWeight(600).Padding(16, 12, 2, 12)
			ui.Text(c, msg).FontSize(t.TextSM).TextColor(t.FgMuted).Padding(0, 12)
		default:
			ui.List(c.Key("vcs:commits"), &v.commitList, len(v.commits), func(i int) { v.commitRow(c, t, i) }).Grow(1).Padding(4, 6).Gap(1)
		}
	})
}

func (v *View) commitRow(c *ui.Context, t *kit.Tokens, i int) {
	cm := v.commits[i]
	sel := cm.ID == v.selCommit
	baseline := cm.ParentID == ""
	row := ui.Row(c).Gap(10).Padding(8, 8).Radius(t.RadiusSM).Cursor(ui.CursorPointer).Focusable().Label(cm.ShortID + " " + cm.Description)
	if row.Clicked() {
		v.SelectCommit(cm.ID)
	}
	if sel {
		row.Background(t.AccentSoft)
	} else if row.Hovered() {
		row.Background(t.BgHover)
	}
	row.Children(func() {
		// Timeline: dot and line.
		ui.Column(c).Width(12).Shrink(0).AlignItems(ui.Center).Children(func() {
			col := t.BorderStrong
			switch {
			case sel:
				col = t.Accent
			case baseline:
				col = t.Success
			case i == 0:
				col = t.Info
			}
			ui.Box(c).Size(9, 9).Radius(5).Background(col).Margin(4, 0, 0, 0)
			if i < len(v.commits)-1 {
				ui.Box(c).Width(1).Grow(1).MinHeight(16).Background(t.Border)
			}
		})
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, cm.ShortID).Font("monospace").FontSize(t.TextXS).TextColor(t.Accent)
				if baseline {
					kit.Badge(c, "BASELINE", kit.ToneSuccess)
				} else if i == 0 {
					kit.Badge(c, "HEAD", kit.ToneAccent)
				}
			})
			desc := cm.Description
			if desc == "" {
				desc = "No description"
			}
			ui.Text(c, desc).FontSize(t.TextSM).MaxLines(2).Ellipsis("…")
			meta := formatDate(cm.CreatedAt)
			if cm.IsBaseline || baseline {
				meta += "  ·  session start state — no changes"
			} else {
				meta += fmt.Sprintf("  ·  %d files changed  ·  %s", cm.ChangedFiles, formatSize(cm.ChangedTotalSize))
			}
			ui.Text(c, meta).FontSize(t.TextXS).TextColor(t.FgFaint)
		})
	})
}

func (v *View) detailPanel(c *ui.Context, t *kit.Tokens) {
	ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
		if v.detailBusy {
			ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() { kit.Spinner(c).FontSize(24).TextColor(t.FgFaint) })
			return
		}
		if v.detail == nil {
			kit.EmptyState(c, kit.IconAgentVCS, "Select a commit", "Click on a commit in the timeline to view its details and file changes.")
			return
		}
		d := v.detail
		cm := d.Commit
		ui.Column(c).Padding(12, 16).Gap(6).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				kit.Icon(c, kit.IconAgentVCS).FontSize(14).TextColor(t.Accent)
				ui.Text(c, cm.ShortID).Font("monospace").FontWeight(600)
				tone := kit.ToneWarning
				if cm.Type == "start" {
					tone = kit.ToneSuccess
				}
				kit.Badge(c, strings.ToUpper(cm.Type), tone)
				ui.Box(c).Grow(1)
				if kit.Button(c.Key("vcs:revertall"), kit.ButtonOpts{Variant: kit.Danger, Size: kit.SizeSM, Icon: iconUndo, Label: "Revert All"}).Clicked() {
					v.confirmRevert(cm.ID)
				}
			})
			ui.Text(c, cm.Name).FontSize(t.TextSM)
			meta := fmt.Sprintf("%s  ·  %d tracked files  ·  %s", formatDate(cm.CreatedAt), cm.FilesCount, formatSize(cm.Size))
			if cm.ParentID != "" {
				meta += "  ·  parent: " + short(cm.ParentID, 12)
			}
			ui.Text(c, meta).FontSize(t.TextXS).TextColor(t.FgFaint)
		})
		ui.Row(c).Padding(8, 16).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			kit.Icon(c, iconCode).FontSize(12).TextColor(t.FgFaint)
			ui.Text(c, fmt.Sprintf("CHANGED FILES (%d)", len(d.Diff))).FontSize(t.TextXS).FontWeight(600).LetterSpacing(0.6).TextColor(t.FgFaint).Grow(1)
			if n := v.selectedCount(); n > 0 {
				if kit.Button(c.Key("vcs:revertsel"), kit.ButtonOpts{Size: kit.SizeSM, Icon: iconUndo, Label: fmt.Sprintf("Revert %d selected", n)}).Clicked() {
					v.revertSelected()
				}
			}
		})
		if len(d.Diff) == 0 {
			msg := "No file changes in this commit."
			if cm.Type == "baseline" || cm.Type == "start" {
				msg = "Session baseline — reference state captured at session start, not a change set."
			}
			ui.Text(c, msg).FontSize(t.TextSM).TextColor(t.FgMuted).Padding(16)
			return
		}
		ui.List(c.Key("vcs:files"), &v.fileList, len(d.Diff), func(i int) { v.fileRow(c, t, cm.ID, d.Diff[i]) }).Grow(1).Padding(4, 8).Gap(1)
	})
}

func (v *View) selectedCount() int {
	n := 0
	for _, on := range v.selected {
		if on {
			n++
		}
	}
	return n
}

func (v *View) fileRow(c *ui.Context, t *kit.Tokens, commitID string, e client.VCSDiffEntry) {
	name := path.Base(e.Path)
	dir := ""
	if i := strings.LastIndex(e.Path, "/"); i >= 0 {
		dir = e.Path[:i]
	}
	cc := c.Key("f:" + e.Path)
	row := ui.Row(cc).Gap(8).Padding(5, 8).Radius(t.RadiusSM).AlignItems(ui.Center)
	if v.selected[e.Path] {
		row.Background(t.AccentSoft)
	} else if row.Hovered() {
		row.Background(t.BgHover)
	}
	row.Children(func() {
		on := v.selected[e.Path]
		if ui.Checkbox(cc.Key("cb"), &on, "").Label("Select " + name).Changed() {
			v.selected[e.Path] = on
		}
		kit.Badge(cc, diffLabel(e.Type), diffTone(e.Type))
		main := ui.Row(cc).Gap(6).AlignItems(ui.Center).Grow(1).Shrink(1).MinWidth(0).Cursor(ui.CursorPointer).Label("View diff: " + e.Path)
		if main.Clicked() {
			v.openDiff(commitID, e)
		}
		main.Tooltip("View diff: " + e.Path)
		main.Children(func() {
			kit.Icon(cc, iconCode).FontSize(13).TextColor(t.FgMuted)
			ui.Text(cc, name).FontSize(t.TextSM).FontWeight(500).SingleLine()
			if dir != "" {
				ui.Text(cc, dir).FontSize(t.TextXS).TextColor(t.FgFaint).SingleLine().Ellipsis("…").Shrink(1)
			}
		})
		size := e.NewSize
		if size == 0 {
			size = e.OldSize
		}
		if size > 0 {
			ui.Text(cc, formatSize(size)).FontSize(t.TextXS).TextColor(t.FgFaint).SingleLine()
		}
		if kit.Button(cc.Key("rv"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: iconUndo, Tooltip: "Revert " + name}).Width(26).Height(26).Clicked() {
			v.confirmRevertFile(commitID, e.Path)
		}
	})
}

// ---- reverts ----

func (v *View) confirmRevert(commitID string) {
	v.m = modal{open: true, danger: true, title: "Revert to commit", ok: "Revert",
		message: "This will restore all files to the state of this commit. A safety backup will be created automatically. Continue?",
		onOK: func(string) {
			v.app.Go(func(ctx context.Context) {
				err := v.app.Client.VCSRevert(ctx, commitID)
				v.app.Update(func() {
					if err != nil {
						v.app.Toasts.Error("Revert failed", err.Error())
						return
					}
					v.app.Toasts.Success("Reverted to commit "+short(commitID, 12), "")
					if v.selSession != "" {
						v.SelectSession(v.selSession)
					}
				})
			})
		}}
}

func (v *View) confirmRevertFile(commitID, file string) {
	v.m = modal{open: true, danger: true, title: "Revert file", ok: "Revert",
		message: fmt.Sprintf("Restore %q to its state in this commit?", file),
		onOK:    func(string) { v.revertFiles(commitID, []string{file}) }}
}

func (v *View) revertSelected() {
	if v.detail == nil {
		return
	}
	var files []string
	for p, on := range v.selected {
		if on {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	v.revertFiles(v.detail.Commit.ID, files)
}

func (v *View) revertFiles(commitID string, files []string) {
	v.app.Go(func(ctx context.Context) {
		restored, err := v.app.Client.VCSRevertFiles(ctx, commitID, files)
		v.app.Update(func() {
			if err != nil {
				v.app.Toasts.Error("Revert files failed", err.Error())
				return
			}
			v.app.Toasts.Success(fmt.Sprintf("Restored %d file(s)", len(restored)), "")
			v.selected = map[string]bool{}
		})
	})
}

func (v *View) modalDialog(c *ui.Context) {
	m := &v.m
	kit.Dialog(c, &m.open, kit.DialogOpts{
		Title: m.title, Width: 440,
		Body: func() {
			t := kit.Current()
			if !m.prompt {
				ui.Text(c, m.message).FontSize(t.TextSM).TextColor(t.FgMuted)
				return
			}
			ui.Text(c, m.message).FontSize(t.TextSM).TextColor(t.FgMuted)
			if kit.Input(c.Key("vcs:modal"), &m.value, "e.g. before-refactor").AutoFocus().Submitted() && strings.TrimSpace(m.value) != "" {
				m.open = false
				m.onOK(strings.TrimSpace(m.value))
			}
		},
		Footer: func() {
			if kit.SecondaryButton(c, v.app.T("projects.view.common.cancel")).Clicked() {
				m.open = false
			}
			variant := kit.Primary
			if m.danger {
				variant = kit.Danger
			}
			if kit.Button(c.Key("vcs:modal-ok"), kit.ButtonOpts{Variant: variant, Label: m.ok, Disabled: m.prompt && strings.TrimSpace(m.value) == ""}).Clicked() {
				m.open = false
				m.onOK(strings.TrimSpace(m.value))
			}
		},
	})
}

// ---- helpers ----

func short(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func formatSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
}

func formatDate(s string) string {
	ts, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	d := time.Since(ts)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return ts.Local().Format("Jan 2, 15:04")
}

func diffLabel(t string) string {
	switch t {
	case "added":
		return "A"
	case "deleted":
		return "D"
	}
	return "M"
}

func diffTone(t string) kit.Tone {
	switch t {
	case "added":
		return kit.ToneSuccess
	case "deleted":
		return kit.ToneDanger
	}
	return kit.ToneWarning
}
