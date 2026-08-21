package extension

import "io/fs"

// The frontend contract is expressed as io/fs plus plain data. An extension
// ships a *built* frontend — the output of whatever toolchain it likes — and
// core serves those files next to its own. Nothing here knows about React,
// Vite or bundlers, and core never builds an extension's assets.
//
// Three mechanisms, deliberately distinct, because they fail in very different
// ways and must be debuggable apart:
//
//   - FrontendProvider adds files and panels under a path the extension owns.
//     It cannot affect any core asset.
//   - FrontendOverlay shadows a *named, explicit* list of core asset paths.
//     Branding: logo, theme, favicon.
//   - FrontendReplacer replaces the whole asset root with a different frontend.
//
// A build-tag swap of the embedded assets is NOT one of the options, even
// though it looks like the obvious one: //go:embed cannot reach files in
// another Go module, and an enterprise frontend lives in an enterprise module.
// FrontendReplacer exists precisely because of that constraint.

// UI slots a panel can mount into. The set is closed on purpose: every slot is
// a place the shell actually reserves, and an unknown slot is dropped rather
// than guessed at.
const (
	// SlotSidebar adds a top-level navigation entry with its own page.
	SlotSidebar = "sidebar"
	// SlotSettings adds a section to the settings screen.
	SlotSettings = "settings"
	// SlotChatSide adds a panel to the chat information sidebar.
	SlotChatSide = "chat-side"
	// SlotStatusBar adds a small indicator to the status bar.
	SlotStatusBar = "status-bar"
)

// PanelManifest describes one panel an extension contributes to the core WebUI.
//
// The shell fetches the merged manifest at boot and dynamically imports each
// Entry as an ES module. Panels are additive: an extension that only wants to
// re-skin the product wants FrontendOverlay or FrontendReplacer instead.
type PanelManifest struct {
	// ID is unique within the extension. Core namespaces it with the extension
	// ID before handing it to the shell, so two extensions may use the same one.
	ID string
	// Title is the label shown to the user.
	Title string
	// Slot is where the panel mounts. Use one of the Slot* constants; an
	// unrecognised value is dropped with a log line rather than guessed at.
	Slot string
	// Entry is the ES module entry point, relative to the extension's asset
	// root and without a leading slash: "panels/reports.js". Core turns it into
	// an absolute URL for the shell.
	Entry string
	// Icon is an optional icon name the shell understands.
	Icon string
	// Order sorts panels inside a slot; equal values fall back to extension ID.
	Order int
}

// FrontendProvider is implemented by extensions that add UI to the core WebUI.
//
// Assets are served read-only under /ext/<AssetPath>/, alongside core's own
// static files and therefore *outside* the API token check — exactly like
// core's JavaScript, and necessarily so: a browser cannot attach headers to a
// dynamic import(). Never serve anything from Assets that is not safe to hand
// to an unauthenticated client; private data belongs behind an
// HTTPEndpointProvider route, which the panel then calls with the token.
type FrontendProvider interface {
	Extension
	// AssetPath is the single path segment this extension owns under /ext/.
	// Same rules as HTTPEndpointProvider.BasePath: lowercase letters, digits,
	// '_' or '-', unique in the build.
	AssetPath() string
	// Assets is the built frontend to serve, typically an //go:embed of the
	// extension's own dist directory. Returning nil contributes no files, which
	// is valid for an extension whose panels live in a shared bundle.
	Assets() fs.FS
	// Panels returns the panels to register. It may return nil: an extension
	// may want to serve assets without declaring a panel.
	Panels() []PanelManifest
}

// FrontendOverlay is implemented by extensions that replace individual core
// assets — branding, mostly: logo, theme stylesheet, favicon.
//
// Overrides must be listed explicitly. A blanket shadow of the core asset tree
// would make every upgrade undebuggable, because a stale extension file would
// silently win over a new core one with no way to see it happening.
type FrontendOverlay interface {
	Extension
	// OverlayAssets holds the replacement files, addressed by the same paths
	// they have in the core asset tree ("assets/logo.svg").
	OverlayAssets() fs.FS
	// Overrides lists the core asset paths this extension shadows, without a
	// leading slash. Files present in OverlayAssets but absent here are ignored.
	Overrides() []string
}

// FrontendReplacer is implemented by an extension that ships an entire
// alternative frontend, replacing the core WebUI wholesale.
//
// This is the mechanism for a differently-branded product built on the same
// API. The replacement must be a complete asset root — an index.html and
// everything it references — because core stops consulting its own assets
// except as a fallback for paths the replacement does not have.
//
// At most one replacer may be active. Two is a build mistake, not a precedence
// question: core keeps its own frontend and logs the conflict, because
// arbitrarily picking one would ship a customer the wrong product.
type FrontendReplacer interface {
	Extension
	// ReplaceFrontend returns the asset root to serve instead of core's.
	// Returning nil declines, which lets configuration decide at run time.
	ReplaceFrontend() fs.FS
}
