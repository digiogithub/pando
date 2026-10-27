// Package editor is the code editor area of the native desktop: a file
// explorer, open-file tabs, a code editor (MyGo ui/editor, with find bar,
// folding, multi-cursor and syntax highlighting), image/PDF viewers and a
// status bar. It reaches parity with web-ui/src/components/editor (Monaco)
// over the same /api/v1/files endpoints.
//
// LSP: Pando exposes no diagnostics/hover/completion over the HTTP API (the
// language servers live in the agent's process and feed the agent only), so
// the editor shows syntax highlighting but no LSP hints. The hooks
// (State.SetDiagnostics, OnHover, OnCompletion) are ready for when an
// endpoint exists.
package editor

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
	mged "github.com/egoist/mygo/ui/editor"
)

// Icon names (files of kit/icons).
const (
	iconSave       = "save"
	iconFolderTree = "folder-tree"
	iconFileCode   = "file-code"
	iconImage      = "image"
	iconFolderPlus = "folder-plus"
	iconFilePlus   = "file-plus"
	iconEye        = "eye"
	iconEyeOff     = "eye-off"
)

// treeRefreshInterval is how often the explorer re-reads the tree (the Web
// UI polls every 3 s as well).
const treeRefreshInterval = 3 * time.Second

func init() {
	app.Register(app.Area{
		ID: "editor", Path: "/editor", Patterns: []string{"/editor", "/editor/{path...}"},
		TitleKey: "nav.codeEditor", Icon: kit.IconEditor, Section: app.SectionTools, Order: 0,
		New: func(a *app.Context) app.View { return New(a) },
	})
}

type fileKind int

const (
	kindText fileKind = iota
	kindImage
	kindPDF
)

// openFile is a tab.
type openFile struct {
	path    string
	kind    fileKind
	buf     *mged.Buffer
	st      *mged.State
	loading bool
	err     string
	// image viewer
	bitmap *ui.Bitmap
	// remote is the text last read from or written to the server, to
	// detect files changed outside the editor.
	remote  string
	changed bool // changed on disk while dirty
}

func (f *openFile) name() string { return path.Base(f.path) }

func (f *openFile) dirty() bool { return f.kind == kindText && f.buf != nil && f.buf.Modified() }

// modal is a name prompt or a confirmation.
type modal struct {
	open    bool
	title   string
	label   string
	message string
	value   string
	ok      string
	danger  bool
	confirm bool
	onOK    func(value string)
}

// Editor is the area's state; every field belongs to the UI thread.
type Editor struct {
	app *app.Context

	files  []*openFile
	active string

	tree         *fileTree
	explorerOpen bool
	explorerW    float32
	showHidden   bool
	filter       string

	gitBranch string
	saving    bool
	started   bool
	stop      context.CancelFunc
	lastBuilt time.Time
	pathParam string

	m modal
}

// New returns the editor view.
func New(a *app.Context) *Editor {
	e := &Editor{app: a, explorerOpen: true, explorerW: 260}
	e.tree = newFileTree(e)
	return e
}

// Close stops background work.
func (e *Editor) Close() {
	if e.stop != nil {
		e.stop()
	}
}

func (e *Editor) start() {
	if e.started {
		return
	}
	e.started = true
	if e.app.Client == nil {
		return
	}
	ctx, cancel := context.WithCancel(e.app.Ctx)
	e.stop = cancel
	e.tree.reload(".")
	e.app.Go(func(context.Context) {
		b := e.app.Client.GitBranch(ctx)
		e.app.Update(func() {
			if b != "" {
				e.gitBranch = b
			}
		})
	})
	// Poll the tree (and the open files) like the Web UI does, only while
	// the editor is on screen.
	e.app.Go(func(context.Context) {
		tk := time.NewTicker(treeRefreshInterval)
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
			}
			e.app.Update(func() {
				if time.Since(e.lastBuilt) > 2*time.Second {
					return
				}
				e.tree.refreshAll()
				e.checkExternalChanges()
			})
		}
	})
}

// Title is the header title.
func (e *Editor) Title() string { return e.app.T("nav.codeEditor") }

// HeaderActions adds Save, New file and the explorer toggles to the header.
func (e *Editor) HeaderActions(c *ui.Context) {
	if kit.Button(c.Key("ed:explorer"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: iconFolderTree,
		Tooltip: map[bool]string{true: "Hide file explorer", false: "Show file explorer"}[e.explorerOpen], Active: e.explorerOpen}).Clicked() {
		e.explorerOpen = !e.explorerOpen
	}
	hid := iconEyeOff
	tip := "Show hidden files"
	if e.showHidden {
		hid, tip = iconEye, "Hide hidden files"
	}
	if kit.Button(c.Key("ed:hidden"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: hid, Tooltip: tip, Active: e.showHidden}).Clicked() {
		e.showHidden = !e.showHidden
		e.tree.reset()
	}
	if f := e.activeFile(); f != nil && f.kind == kindText {
		v := kit.Secondary
		if f.dirty() {
			v = kit.Primary
		}
		label := e.app.T("common.save")
		if e.saving {
			label = e.app.T("common.saving")
		}
		if kit.Button(c.Key("ed:save"), kit.ButtonOpts{Variant: v, Size: kit.SizeSM, Icon: iconSave, Label: label,
			Disabled: e.saving || !f.dirty(), Tooltip: "Save file (Ctrl+S)"}).Clicked() {
			e.save(f)
		}
	}
	if kit.Button(c.Key("ed:new"), kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Icon: iconFilePlus, Label: "New File"}).Clicked() {
		e.promptNew(".", false)
	}
}

func (e *Editor) activeFile() *openFile {
	for _, f := range e.files {
		if f.path == e.active {
			return f
		}
	}
	return nil
}

// Build lays out explorer | tabs + editor, and the status bar.
func (e *Editor) Build(c *ui.Context, r *ui.Route) {
	e.start()
	e.lastBuilt = time.Now()
	t := kit.Current()
	if r != nil {
		if p := strings.Trim(r.Param("path"), "/"); p != "" && p != e.pathParam {
			e.pathParam = p
			e.Open(p)
		}
	}
	f := e.activeFile()
	if f != nil && f.kind == kindText && c.Shortcut(ui.Cmd, ui.KeyS) {
		e.save(f)
	}
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if e.explorerOpen {
				ui.Split(c, &e.explorerW, func() { e.explorer(c, t) }, func() { e.editorArea(c, t) }).Grow(1)
			} else {
				e.editorArea(c, t)
			}
		})
		e.statusBar(c, t)
	})
	e.modalDialog(c)
}

func (e *Editor) editorArea(c *ui.Context, t *kit.Tokens) {
	ui.Column(c).Grow(1).Shrink(1).MinWidth(0).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		e.tabs(c, t)
		f := e.activeFile()
		switch {
		case f == nil:
			kit.EmptyState(c, iconFileCode, "Open a file from the tree", "Select a file in the explorer to start editing")
		case f.loading:
			ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() { kit.Spinner(c).FontSize(22).TextColor(t.FgFaint) })
		case f.err != "":
			kit.EmptyState(c, kit.IconError, f.name(), f.err)
		case f.kind == kindImage:
			e.imageViewer(c, t, f)
		case f.kind == kindPDF:
			kit.EmptyState(c, kit.IconFileText, f.name(), "PDF preview is not available in the native editor. Open it with your system viewer.")
		default:
			e.codeEditor(c, t, f)
		}
	})
}

func (e *Editor) codeEditor(c *ui.Context, t *kit.Tokens, f *openFile) {
	if f.changed {
		ui.Row(c).Padding(6, 12).Gap(8).AlignItems(ui.Center).Background(t.WarningSoft).Children(func() {
			ui.Text(c, f.name()+" changed on disk.").FontSize(t.TextSM).Grow(1)
			if kit.Button(c.Key("ed:reload"), kit.ButtonOpts{Size: kit.SizeSM, Label: "Reload from disk"}).Clicked() {
				e.reload(f)
			}
			if kit.Button(c.Key("ed:keep"), kit.ButtonOpts{Size: kit.SizeSM, Variant: kit.Ghost, Label: "Keep mine"}).Clicked() {
				f.changed = false
			}
		})
	}
	mged.View(c.Key("code:"+f.path), f.st).Grow(1).FillWidth().MinHeight(0)
}

func (e *Editor) imageViewer(c *ui.Context, t *kit.Tokens, f *openFile) {
	ui.Column(c).Grow(1).AlignItems(ui.Center).Justify(ui.Center).Gap(8).Padding(16).Children(func() {
		if f.bitmap != nil {
			ui.Image(c, f.bitmap).Fit(ui.Contain).Grow(1).Shrink(1).MinHeight(0).MaxWidth(2000)
		} else {
			ui.Text(c, "Image unavailable").TextColor(t.FgMuted)
		}
		ui.Text(c, f.path).FontSize(t.TextXS).TextColor(t.FgFaint)
	})
}

func (e *Editor) tabs(c *ui.Context, t *kit.Tokens) {
	if len(e.files) == 0 {
		return
	}
	ui.Row(c).Height(36).Shrink(0).Background(t.BgShell).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).
		AlignItems(ui.Stretch).Children(func() {
		for _, f := range e.files {
			e.tab(c, t, f)
		}
	})
}

func (e *Editor) tab(c *ui.Context, t *kit.Tokens, f *openFile) {
	active := f.path == e.active
	dirty := f.dirty()
	cc := c.Key("tab:" + f.path)
	tab := ui.Row(cc).Gap(6).Padding(0, 10).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Label(f.name()).
		BorderWidth(0, 0, 2, 0).BorderColor(ui.RGBA(0, 0, 0, 0))
	if tab.Clicked() {
		e.active = f.path
	}
	if active {
		tab.Background(t.Bg).BorderColor(t.Accent)
	} else if tab.Hovered() {
		tab.Background(t.BgHover)
	}
	tab.Tooltip(f.path)
	tab.Children(func() {
		ui.Text(cc, f.name()).FontSize(t.TextSM).SingleLine().TextColor(map[bool]ui.Color{true: t.Fg, false: t.FgMuted}[active])
		if dirty {
			ui.Text(cc, "•").FontSize(t.TextMD).TextColor(t.Accent)
		}
		x := kit.Button(cc.Key("x"), kit.ButtonOpts{Variant: kit.Ghost, Icon: kit.IconClose, Size: kit.SizeSM, Tooltip: "Close tab"})
		x.Width(22).Height(22)
		if x.Clicked() {
			e.requestClose(f)
		}
	})
}

func (e *Editor) statusBar(c *ui.Context, t *kit.Tokens) {
	f := e.activeFile()
	ui.Row(c).Height(24).Shrink(0).Padding(0, 12).Gap(10).AlignItems(ui.Center).Background(t.BgShell).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		small := func(s string) ui.Element { return ui.Text(c, s).FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine() }
		kit.Icon(c, kit.IconAgentVCS).FontSize(12).TextColor(t.FgMuted)
		branch := e.gitBranch
		if branch == "" {
			branch = "main"
		}
		small(branch)
		if f != nil {
			small("|")
			small(f.path).Ellipsis("…").Shrink(1)
			if f.dirty() {
				ui.Text(c, "●").FontSize(t.TextXS).TextColor(t.Accent)
			}
		}
		ui.Box(c).Grow(1)
		if f == nil {
			small("No file open")
			return
		}
		lang := "Plain text"
		if f.kind == kindText && f.buf != nil {
			if l := mged.LanguageFor(f.path); l != nil {
				lang = l.Name
			}
			line, col := cursor(f)
			small(lang)
			small("|")
			small("UTF-8")
			small("|")
			small(fmt.Sprintf("Ln %d, Col %d", line, col))
			return
		}
		small(map[fileKind]string{kindImage: "Image", kindPDF: "PDF"}[f.kind])
	})
}

// cursor returns the 1-based line and column (in characters) of the caret.
func cursor(f *openFile) (int, int) {
	p := f.st.CursorPos()
	line := f.buf.Line(p.Line)
	col := p.Col
	if col > len(line) {
		col = len(line)
	}
	return p.Line + 1, utf8.RuneCountInString(line[:col]) + 1
}

// ---- opening, saving, closing ----

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true}

func kindOf(p string) fileKind {
	ext := strings.ToLower(path.Ext(p))
	switch {
	case imageExts[ext]:
		return kindImage
	case ext == ".pdf":
		return kindPDF
	}
	return kindText
}

// Open opens (or focuses) a file of the working directory.
func (e *Editor) Open(p string) {
	p = strings.TrimPrefix(path.Clean(p), "./")
	for _, f := range e.files {
		if f.path == p {
			e.active = p
			return
		}
	}
	f := &openFile{path: p, kind: kindOf(p), loading: true}
	e.files = append(e.files, f)
	e.active = p
	if e.app.Client == nil {
		f.loading = false
		return
	}
	e.app.Go(func(ctx context.Context) {
		switch f.kind {
		case kindText:
			text, err := e.app.Client.ReadFile(ctx, p)
			e.app.Update(func() {
				f.loading = false
				if err != nil {
					f.err = "Failed to open file: " + err.Error()
					return
				}
				e.initText(f, text)
			})
		case kindImage:
			data, err := e.app.Client.ReadRawFile(ctx, p)
			var bm *ui.Bitmap
			if err == nil {
				bm, err = ui.DecodeBitmap(data)
			}
			e.app.Update(func() {
				f.loading = false
				if err != nil {
					f.err = "Failed to load image: " + err.Error()
					return
				}
				f.bitmap = bm
			})
		default:
			e.app.Update(func() { f.loading = false })
		}
	})
}

func (e *Editor) initText(f *openFile, text string) {
	f.remote = text
	f.buf = mged.NewBuffer(text)
	f.buf.SetLanguage(mged.LanguageFor(f.path))
	f.st = &mged.State{Buffer: f.buf, WrapGuides: []int{100}, DiffBase: mged.NewBuffer(text)}
	f.st.Font = ui.Font{Family: kit.Current().MonoFont}
	if kit.Current().MonoFont == "" {
		f.st.Font = ui.Font{}
	}
}

func (e *Editor) save(f *openFile) {
	if f.kind != kindText || f.buf == nil || e.saving || !f.dirty() {
		return
	}
	var sb bytes.Buffer
	if _, err := f.buf.WriteTo(&sb); err != nil {
		e.app.Toasts.Error("Failed to save", err.Error())
		return
	}
	text := sb.String()
	e.saving = true
	e.app.Go(func(ctx context.Context) {
		err := e.app.Client.WriteFile(ctx, f.path, text)
		e.app.Update(func() {
			e.saving = false
			if err != nil {
				e.app.Toasts.Error("Failed to save", err.Error())
				return
			}
			f.buf.MarkSaved()
			f.remote = text
			f.changed = false
			f.st.DiffBase = mged.NewBuffer(text)
		})
	})
}

// reload replaces a tab's text with the file on disk.
func (e *Editor) reload(f *openFile) {
	e.app.Go(func(ctx context.Context) {
		text, err := e.app.Client.ReadFile(ctx, f.path)
		e.app.Update(func() {
			if err != nil {
				e.app.Toasts.Error("Failed to reload", err.Error())
				return
			}
			f.remote = text
			f.buf.Reset(text)
			f.buf.MarkSaved()
			f.st.DiffBase = mged.NewBuffer(text)
			f.changed = false
		})
	})
}

// checkExternalChanges re-reads clean open files so that what the agent
// edits shows up; dirty ones only get a banner.
func (e *Editor) checkExternalChanges() {
	if e.app.Client == nil {
		return
	}
	f := e.activeFile()
	if f == nil || f.kind != kindText || f.buf == nil || f.loading {
		return
	}
	e.app.Go(func(ctx context.Context) {
		text, err := e.app.Client.ReadFile(ctx, f.path)
		if err != nil {
			return
		}
		e.app.Update(func() {
			if text == f.remote {
				return
			}
			if !f.dirty() {
				f.remote = text
				f.buf.Reset(text)
				f.buf.MarkSaved()
				f.st.DiffBase = mged.NewBuffer(text)
				return
			}
			f.remote = text
			f.changed = true
		})
	})
}

func (e *Editor) requestClose(f *openFile) {
	if !f.dirty() {
		e.closeFile(f.path)
		return
	}
	e.m = modal{open: true, confirm: true, danger: true, title: "Unsaved changes",
		message: fmt.Sprintf("%q has unsaved changes. Close without saving?", f.name()), ok: "Close without saving",
		onOK: func(string) { e.closeFile(f.path) }}
}

func (e *Editor) closeFile(p string) {
	for i, f := range e.files {
		if f.path != p {
			continue
		}
		e.files = append(e.files[:i], e.files[i+1:]...)
		break
	}
	if e.active == p {
		e.active = ""
		if n := len(e.files); n > 0 {
			e.active = e.files[n-1].path
		}
	}
}

// ---- file operations ----

func parentOf(p string) string {
	d := path.Dir(p)
	if d == "" {
		return "."
	}
	return d
}

func join(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}

func (e *Editor) promptNew(dir string, isDir bool) {
	title, label := "New file", "File name"
	if isDir {
		title, label = "New folder", "Folder name"
	}
	e.m = modal{open: true, title: title, label: label, ok: "Create", onOK: func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		np := join(dir, name)
		e.app.Go(func(ctx context.Context) {
			if err := e.app.Client.CreateFile(ctx, np, isDir); err != nil {
				e.app.Toasts.Error("Failed to create", err.Error())
				return
			}
			e.app.Update(func() {
				e.tree.reload(dir)
				if !isDir {
					e.Open(np)
				}
			})
		})
	}}
}

func (e *Editor) promptRename(n *treeNode) {
	e.m = modal{open: true, title: fmt.Sprintf("Rename %q", n.name), label: "New name", value: n.name, ok: "Rename", onOK: func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || name == n.name {
			return
		}
		np := join(parentOf(n.path), name)
		e.app.Go(func(ctx context.Context) {
			if err := e.app.Client.RenameFile(ctx, n.path, np); err != nil {
				e.app.Toasts.Error("Failed to rename", err.Error())
				return
			}
			e.app.Update(func() {
				e.renameOpen(n.path, np)
				e.tree.reload(parentOf(n.path))
			})
		})
	}}
}

// renameOpen retargets open tabs of a renamed file or folder.
func (e *Editor) renameOpen(from, to string) {
	for _, f := range e.files {
		switch {
		case f.path == from:
			f.path = to
		case strings.HasPrefix(f.path, from+"/"):
			f.path = to + strings.TrimPrefix(f.path, from)
		default:
			continue
		}
		if f.kind == kindText && f.buf != nil {
			f.buf.SetLanguage(mged.LanguageFor(f.path))
		}
	}
	if e.active == from {
		e.active = to
	} else if strings.HasPrefix(e.active, from+"/") {
		e.active = to + strings.TrimPrefix(e.active, from)
	}
}

func (e *Editor) promptDelete(n *treeNode) {
	msg := fmt.Sprintf("Delete %q? This cannot be undone.", n.name)
	title := "Delete file"
	if n.isDir {
		title = "Delete folder"
		msg = fmt.Sprintf("Delete %q and everything inside it? This cannot be undone.", n.name)
	}
	e.m = modal{open: true, confirm: true, danger: true, title: title, message: msg, ok: "Delete", onOK: func(string) {
		e.app.Go(func(ctx context.Context) {
			if err := e.app.Client.DeleteFile(ctx, n.path); err != nil {
				e.app.Toasts.Error("Failed to delete", err.Error())
				return
			}
			e.app.Update(func() {
				var gone []string
				for _, f := range e.files {
					if f.path == n.path || strings.HasPrefix(f.path, n.path+"/") {
						gone = append(gone, f.path)
					}
				}
				for _, p := range gone {
					e.closeFile(p)
				}
				e.tree.reload(parentOf(n.path))
			})
		})
	}}
}

func (e *Editor) modalDialog(c *ui.Context) {
	m := &e.m
	kit.Dialog(c, &m.open, kit.DialogOpts{
		Title: m.title,
		Width: 420,
		Body: func() {
			t := kit.Current()
			if m.confirm {
				ui.Text(c, m.message).FontSize(t.TextSM).TextColor(t.FgMuted)
				return
			}
			ui.Text(c, m.label).FontSize(t.TextSM).TextColor(t.FgMuted)
			if kit.Input(c.Key("ed:modal"), &m.value, "").AutoFocus().Submitted() {
				m.open = false
				if m.onOK != nil {
					m.onOK(m.value)
				}
			}
		},
		Footer: func() {
			if kit.SecondaryButton(c, e.app.T("projects.view.common.cancel")).Clicked() {
				m.open = false
			}
			v := kit.Primary
			if m.danger {
				v = kit.Danger
			}
			if kit.Button(c.Key("ed:modal-ok"), kit.ButtonOpts{Variant: v, Label: m.ok}).Clicked() {
				m.open = false
				if m.onOK != nil {
					m.onOK(m.value)
				}
			}
		},
	})
}
