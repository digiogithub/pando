package extensions

import (
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/digiogithub/pando/pkg/extension"
)

// Each frontend capability gets its own type. A single struct implementing all
// three would be handed to every code path with nil function fields — the same
// trap the tool middleware tests document.

type frontExt struct {
	baseExt
	assetPath string
	assets    fs.FS
	panels    []extension.PanelManifest
	panicOn   bool
}

func (e *frontExt) ExtensionInfo() extension.Info { return e.info(e) }
func (e *frontExt) AssetPath() string             { return e.assetPath }
func (e *frontExt) Panels() []extension.PanelManifest {
	return e.panels
}

func (e *frontExt) Assets() fs.FS {
	if e.panicOn {
		panic("assets exploded")
	}
	return e.assets
}

type overlayExt struct {
	baseExt
	assets    fs.FS
	overrides []string
}

func (e *overlayExt) ExtensionInfo() extension.Info { return e.info(e) }
func (e *overlayExt) OverlayAssets() fs.FS          { return e.assets }
func (e *overlayExt) Overrides() []string           { return e.overrides }

type replaceExt struct {
	baseExt
	assets fs.FS
}

func (e *replaceExt) ExtensionInfo() extension.Info { return e.info(e) }
func (e *replaceExt) ReplaceFrontend() fs.FS        { return e.assets }

func coreFS() fs.FS {
	return fstest.MapFS{
		"index.html":      {Data: []byte("core index")},
		"assets/logo.svg": {Data: []byte("core logo")},
		"assets/app.js":   {Data: []byte("core app")},
	}
}

func read(t *testing.T, fsys fs.FS, name string) string {
	t.Helper()
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

func mustNotExist(t *testing.T, fsys fs.FS, name string) {
	t.Helper()
	if _, err := fs.Stat(fsys, name); err == nil {
		t.Fatalf("%s should not be served", name)
	}
}

func TestFrontendWithoutExtensionsReturnsCore(t *testing.T) {
	core := coreFS()
	mgr := managerWith(t)

	if HasFrontendExtensions(mgr) {
		t.Fatal("a build with no frontend extensions must report none")
	}
	if got := Frontend(mgr, core); got == nil {
		t.Fatal("Frontend returned nil")
	}
	if got := Frontend(nil, core); read(t, got, "index.html") != "core index" {
		t.Fatal("a nil manager must pass core through")
	}
}

func TestFrontendMountsProviderAssets(t *testing.T) {
	mgr := managerWith(t, &frontExt{
		baseExt:   baseExt{id: "ui.acme"},
		assetPath: "acme",
		assets: fstest.MapFS{
			"panels/reports.js": {Data: []byte("export default 1")},
		},
	})

	fsys := Frontend(mgr, coreFS())

	if got := read(t, fsys, "ext/acme/panels/reports.js"); got != "export default 1" {
		t.Fatalf("panel asset = %q", got)
	}
	// Core assets are untouched by a provider.
	if got := read(t, fsys, "index.html"); got != "core index" {
		t.Fatalf("index.html = %q", got)
	}
	if !HasFrontendExtensions(mgr) {
		t.Fatal("HasFrontendExtensions = false")
	}
}

func TestFrontendRejectsInvalidAssetPath(t *testing.T) {
	mgr := managerWith(t, &frontExt{
		baseExt:   baseExt{id: "ui.acme"},
		assetPath: "acme/evil",
		assets:    fstest.MapFS{"x.js": {Data: []byte("x")}},
	})

	fsys := Frontend(mgr, coreFS())
	mustNotExist(t, fsys, "ext/acme/evil/x.js")
	mustNotExist(t, fsys, "ext/acme/x.js")
}

// An overlay may shadow only the paths it declared. Anything else it ships is
// invisible, so a stale extension file cannot quietly beat a new core one.
func TestFrontendOverlayIsLimitedToDeclaredPaths(t *testing.T) {
	mgr := managerWith(t, &overlayExt{
		baseExt: baseExt{id: "ui.brand"},
		assets: fstest.MapFS{
			"assets/logo.svg": {Data: []byte("brand logo")},
			"assets/app.js":   {Data: []byte("hijacked app")},
		},
		overrides: []string{"assets/logo.svg"},
	})

	fsys := Frontend(mgr, coreFS())

	if got := read(t, fsys, "assets/logo.svg"); got != "brand logo" {
		t.Fatalf("logo = %q, want the overlay copy", got)
	}
	if got := read(t, fsys, "assets/app.js"); got != "core app" {
		t.Fatalf("app.js = %q, want the core copy", got)
	}
}

func TestFrontendOverlayDuplicateOverrideIsRefused(t *testing.T) {
	first := &overlayExt{
		baseExt:   baseExt{id: "ui.a"},
		assets:    fstest.MapFS{"assets/logo.svg": {Data: []byte("a logo")}},
		overrides: []string{"assets/logo.svg"},
	}
	second := &overlayExt{
		baseExt:   baseExt{id: "ui.b"},
		assets:    fstest.MapFS{"assets/logo.svg": {Data: []byte("b logo")}},
		overrides: []string{"assets/logo.svg"},
	}

	fsys := Frontend(managerWith(t, first, second), coreFS())

	// Whichever extension is ordered first owns the path; the second is
	// dropped rather than layered, so the result cannot depend on lookup order.
	if got := read(t, fsys, "assets/logo.svg"); got != "a logo" && got != "b logo" {
		t.Fatalf("logo = %q", got)
	}
}

func TestFrontendOverlayEscapingPathIsIgnored(t *testing.T) {
	mgr := managerWith(t, &overlayExt{
		baseExt:   baseExt{id: "ui.brand"},
		assets:    fstest.MapFS{"assets/logo.svg": {Data: []byte("brand logo")}},
		overrides: []string{"../../etc/passwd", ""},
	})

	if got := read(t, Frontend(mgr, coreFS()), "assets/logo.svg"); got != "core logo" {
		t.Fatalf("logo = %q, want the core copy", got)
	}
}

func TestFrontendReplacerWins(t *testing.T) {
	mgr := managerWith(t, &replaceExt{
		baseExt: baseExt{id: "ui.alt"},
		assets: fstest.MapFS{
			"index.html": {Data: []byte("alt index")},
		},
	})

	fsys := Frontend(mgr, coreFS())

	if got := read(t, fsys, "index.html"); got != "alt index" {
		t.Fatalf("index.html = %q", got)
	}
	// The replacement is a complete root: core assets it does not carry are
	// gone, not merged in behind it.
	mustNotExist(t, fsys, "assets/app.js")
}

// Two replacers is a build mistake. Picking one would ship the wrong product,
// so core keeps its own frontend.
func TestFrontendTwoReplacersKeepCore(t *testing.T) {
	a := &replaceExt{baseExt: baseExt{id: "ui.a"}, assets: fstest.MapFS{"index.html": {Data: []byte("a")}}}
	b := &replaceExt{baseExt: baseExt{id: "ui.b"}, assets: fstest.MapFS{"index.html": {Data: []byte("b")}}}

	fsys := Frontend(managerWith(t, a, b), coreFS())

	if got := read(t, fsys, "index.html"); got != "core index" {
		t.Fatalf("index.html = %q, want the core copy", got)
	}
}

func TestFrontendReplacerDeclining(t *testing.T) {
	mgr := managerWith(t, &replaceExt{baseExt: baseExt{id: "ui.alt"}})

	if got := read(t, Frontend(mgr, coreFS()), "index.html"); got != "core index" {
		t.Fatalf("index.html = %q", got)
	}
}

func TestFrontendPanickingProviderIsContained(t *testing.T) {
	mgr := managerWith(t, &frontExt{
		baseExt:   baseExt{id: "ui.bad"},
		assetPath: "bad",
		panicOn:   true,
	})

	if got := read(t, Frontend(mgr, coreFS()), "index.html"); got != "core index" {
		t.Fatalf("index.html = %q", got)
	}
}

func TestPanels(t *testing.T) {
	mgr := managerWith(t,
		&frontExt{
			baseExt:   baseExt{id: "ui.b"},
			assetPath: "b",
			panels: []extension.PanelManifest{
				{ID: "one", Title: "One", Slot: extension.SlotSidebar, Entry: "one.js", Order: 5},
			},
		},
		&frontExt{
			baseExt:   baseExt{id: "ui.a"},
			assetPath: "a",
			panels: []extension.PanelManifest{
				{ID: "two", Title: "Two", Slot: extension.SlotSidebar, Entry: "./two.js", Order: 1},
				{ID: "three", Title: "Three", Slot: extension.SlotSettings, Entry: "three.js"},
			},
		},
	)

	got := Panels(mgr)
	if len(got) != 3 {
		t.Fatalf("panels = %+v", got)
	}
	// Slots group alphabetically ("settings" before "sidebar"); inside a slot,
	// Order decides and the extension ID breaks ties.
	if got[0].ID != "ui.a.three" || got[1].ID != "ui.a.two" || got[2].ID != "ui.b.one" {
		t.Fatalf("order = %s, %s, %s", got[0].ID, got[1].ID, got[2].ID)
	}
	// "./two.js" normalises to a rooted URL under the extension's asset path.
	if got[1].Entry != "/ext/a/two.js" {
		t.Fatalf("entry = %q", got[1].Entry)
	}
	if got[0].Extension != "ui.a" {
		t.Fatalf("extension = %q", got[0].Extension)
	}
}

func TestPanelsDropsInvalidEntries(t *testing.T) {
	mgr := managerWith(t, &frontExt{
		baseExt:   baseExt{id: "ui.acme"},
		assetPath: "acme",
		panels: []extension.PanelManifest{
			{ID: "", Title: "No id", Slot: extension.SlotSidebar, Entry: "a.js"},
			{ID: "no-entry", Slot: extension.SlotSidebar},
			{ID: "bad-slot", Slot: "nowhere", Entry: "a.js"},
			{ID: "escaping", Slot: extension.SlotSidebar, Entry: "../../secret.js"},
			{ID: "good", Slot: extension.SlotChatSide, Entry: "good.js"},
		},
	})

	got := Panels(mgr)
	if len(got) != 1 || got[0].ID != "ui.acme.good" {
		t.Fatalf("panels = %+v", got)
	}
}

func TestPanelsNilManager(t *testing.T) {
	if got := Panels(nil); got != nil {
		t.Fatalf("Panels(nil) = %+v", got)
	}
}

func TestSubtreeFSReadDir(t *testing.T) {
	s := subtreeFS{prefix: "ext/acme", fsys: fstest.MapFS{
		"panels/a.js": {Data: []byte("a")},
	}}

	entries, err := fs.ReadDir(s, "ext/acme")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "panels" {
		t.Fatalf("entries = %+v", entries)
	}
	if _, err := fs.ReadDir(s, "somewhere/else"); err == nil {
		t.Fatal("paths outside the prefix must not resolve")
	}
}
