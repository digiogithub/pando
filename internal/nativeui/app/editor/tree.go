package editor

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// Placeholder children keep an unloaded or empty folder expandable.
const (
	loadingSuffix = "\x00loading"
	emptySuffix   = "\x00empty"
)

type treeNode struct {
	name, path string
	isDir      bool
}

// fileTree is the explorer's data: directories are read lazily when opened
// and re-read on every poll.
type fileTree struct {
	ed       *Editor
	children map[string][]string // dir -> child paths
	nodes    map[string]*treeNode
	loaded   map[string]bool
	loading  map[string]bool
	state    ui.OutlineState[string]
	row      int
}

func newFileTree(ed *Editor) *fileTree {
	t := &fileTree{ed: ed}
	t.reset()
	return t
}

func (t *fileTree) reset() {
	t.children = map[string][]string{}
	t.nodes = map[string]*treeNode{".": {name: ".", path: ".", isDir: true}}
	t.loaded = map[string]bool{}
	t.loading = map[string]bool{}
	t.row = -1
	t.state.Open.Clear()
	if t.ed.started {
		t.reload(".")
	}
}

// reload (re)reads directory dir.
func (t *fileTree) reload(dir string) {
	if t.ed.app.Client == nil || t.loading[dir] {
		return
	}
	t.loading[dir] = true
	hidden := t.ed.showHidden
	t.ed.app.Go(func(ctx context.Context) {
		entries, err := t.ed.app.Client.ListFiles(ctx, dir, &hidden)
		t.ed.app.Update(func() {
			delete(t.loading, dir)
			if hidden != t.ed.showHidden {
				return
			}
			if err != nil {
				t.ed.app.Log.Warn("list files failed", "dir", dir, "error", err)
				t.loaded[dir] = true
				return
			}
			t.set(dir, entries)
		})
	})
}

func (t *fileTree) set(dir string, entries []client.FileEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	kids := make([]string, 0, len(entries))
	for _, en := range entries {
		p := path.Clean(en.Path)
		t.nodes[p] = &treeNode{name: en.Name, path: p, isDir: en.IsDir}
		kids = append(kids, p)
	}
	t.children[dir] = kids
	t.loaded[dir] = true
}

// refreshAll re-reads the root and every open folder.
func (t *fileTree) refreshAll() {
	t.reload(".")
	for p := range t.state.Open.All() {
		if n := t.nodes[p]; n != nil && n.isDir {
			t.reload(p)
		}
	}
}

func (t *fileTree) matches(p string) bool {
	f := strings.ToLower(t.ed.filter)
	if f == "" {
		return true
	}
	n := t.nodes[p]
	if n != nil && strings.Contains(strings.ToLower(n.name), f) {
		return true
	}
	for _, k := range t.children[p] {
		if t.matches(k) {
			return true
		}
	}
	return false
}

func (t *fileTree) kids(p string) []string {
	if strings.HasSuffix(p, loadingSuffix) || strings.HasSuffix(p, emptySuffix) {
		return nil
	}
	n := t.nodes[p]
	if n == nil || !n.isDir {
		return nil
	}
	if !t.loaded[p] {
		return []string{p + loadingSuffix}
	}
	var out []string
	for _, k := range t.children[p] {
		if t.matches(k) {
			out = append(out, k)
		}
	}
	if len(out) == 0 && t.ed.filter == "" {
		return []string{p + emptySuffix}
	}
	return out
}

func (e *Editor) explorer(c *ui.Context, th *kit.Tokens) {
	t := e.tree
	ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Background(th.BgShell).Children(func() {
		ui.Row(c).Padding(8, 8, 4, 8).Children(func() {
			kit.Input(c.Key("ed:filter"), &e.filter, "Filter files…").Grow(1)
		})
		ui.Row(c).Padding(2, 8, 2, 12).AlignItems(ui.Center).Gap(2).Children(func() {
			ui.Text(c, "EXPLORER").FontSize(th.TextXS).FontWeight(600).TextColor(th.FgFaint).Grow(1)
			if kit.Button(c.Key("ex:nf"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: iconFilePlus, Tooltip: "New file"}).Width(24).Height(24).Clicked() {
				e.promptNew(".", false)
			}
			if kit.Button(c.Key("ex:nd"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: iconFolderPlus, Tooltip: "New folder"}).Width(24).Height(24).Clicked() {
				e.promptNew(".", true)
			}
			if kit.Button(c.Key("ex:rf"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: kit.IconRefresh, Tooltip: "Refresh"}).Width(24).Height(24).Clicked() {
				t.refreshAll()
			}
		})
		roots := t.kids(".")
		if len(roots) == 0 {
			msg := "No files in working directory"
			if !t.loaded["."] {
				msg = e.app.T("common.loading")
			}
			ui.Text(c, msg).FontSize(th.TextSM).TextColor(th.FgMuted).Padding(12)
			return
		}
		ui.Outline(c.Key("ex:tree"), &t.state, roots, t.kids, func(p string) { e.treeRow(c, th, p) }).Grow(1).Padding(0, 4)
		// Load folders the user just opened.
		for p := range t.state.Open.All() {
			if n := t.nodes[p]; n != nil && n.isDir && !t.loaded[p] {
				t.reload(p)
			}
		}
	})
}

func (e *Editor) treeRow(c *ui.Context, th *kit.Tokens, p string) {
	if strings.HasSuffix(p, loadingSuffix) {
		ui.Text(c, e.app.T("common.loading")).FontSize(th.TextXS).TextColor(th.FgFaint)
		return
	}
	if strings.HasSuffix(p, emptySuffix) {
		ui.Text(c, "Empty folder").FontSize(th.TextXS).TextColor(th.FgFaint)
		return
	}
	n := e.tree.nodes[p]
	if n == nil {
		return
	}
	cc := c.Key("tr:" + p)
	row := ui.Row(cc).Grow(1).Gap(6).AlignItems(ui.Center).Padding(3, 4).Radius(th.RadiusSM).Cursor(ui.CursorPointer).Label(n.name)
	if row.Clicked() {
		if n.isDir {
			if e.tree.state.Open.Has(p) {
				e.tree.state.Open.Remove(p)
			} else {
				e.tree.state.Open.Add(p)
			}
		} else {
			e.Open(p)
		}
	}
	if !n.isDir && p == e.active {
		row.Background(th.AccentSoft)
	} else if row.Hovered() {
		row.Background(th.BgHover)
	}
	row.ContextMenu(func(m *ui.Menu) {
		dir := parentOf(p)
		if n.isDir {
			dir = p
		}
		if m.Item("New File").Chosen() {
			e.promptNew(dir, false)
		}
		if m.Item("New Folder").Chosen() {
			e.promptNew(dir, true)
		}
		if m.Item("Rename").Chosen() {
			e.promptRename(n)
		}
		if m.Item("Delete").Chosen() {
			e.promptDelete(n)
		}
	})
	row.Children(func() {
		kit.Icon(cc, treeIcon(n, e.tree.state.Open.Has(p))).FontSize(15).TextColor(th.FgMuted)
		ui.Text(cc, n.name).FontSize(th.TextSM).SingleLine().Ellipsis("…").Shrink(1)
	})
}

func treeIcon(n *treeNode, open bool) string {
	if n.isDir {
		if open {
			return kit.IconProjects // folder-open
		}
		return kit.IconFolder
	}
	switch strings.ToLower(path.Ext(n.name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".ico", ".avif":
		return iconImage
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".css", ".html", ".sh", ".bash", ".rs", ".sql", ".lua", ".toml":
		return iconFileCode
	case ".md", ".txt", ".yaml", ".yml", ".json":
		return kit.IconFileText
	}
	return kit.IconFile
}
