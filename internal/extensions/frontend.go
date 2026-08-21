package extensions

import (
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/pkg/extension"
)

// AssetPrefix is where extension frontend assets live in the served asset
// tree. They are folded into the *static* layer rather than mounted on the API
// mux for one concrete reason: a browser cannot attach an Authorization header
// to a dynamic import(), so a panel bundle behind the API token check could
// never be loaded. Extension assets are therefore public, exactly like core's
// own JavaScript — see the warning on extension.FrontendProvider.
const AssetPrefix = "ext"

// Panel is one entry of the merged UI manifest served to the shell. It is the
// wire shape: the extension declares a relative Entry, core resolves it to a
// URL the browser can import.
type Panel struct {
	ID        string `json:"id"`
	Extension string `json:"extension"`
	Title     string `json:"title"`
	Slot      string `json:"slot"`
	Entry     string `json:"entry"`
	Icon      string `json:"icon,omitempty"`
	Order     int    `json:"order"`
}

// knownSlots is closed on purpose: every slot here is one the shell actually
// reserves. A panel asking for anything else is dropped rather than mounted
// somewhere plausible.
var knownSlots = map[string]struct{}{
	extension.SlotSidebar:   {},
	extension.SlotSettings:  {},
	extension.SlotChatSide:  {},
	extension.SlotStatusBar: {},
}

// Frontend returns the asset tree to serve: core's own assets, with any
// extension replacement, overlay and asset subtree applied.
//
// It is the single place the three frontend mechanisms meet, so their
// precedence is decided once and is readable in one function:
//
//	overlay files  >  base (replacement, else core)  >  extension subtrees
//
// A nil manager returns core unchanged, so callers need no guard.
func Frontend(mgr *extension.Manager, core fs.FS) fs.FS {
	if mgr == nil {
		return core
	}

	base := replacement(mgr, core)

	layers := make([]fs.FS, 0, 4)
	layers = append(layers, overlays(mgr)...)
	if base != nil {
		layers = append(layers, base)
	}
	layers = append(layers, assetSubtrees(mgr)...)

	switch len(layers) {
	case 0:
		return core
	case 1:
		return layers[0]
	default:
		return unionFS(layers)
	}
}

// replacement resolves the FrontendReplacer, if exactly one is active.
func replacement(mgr *extension.Manager, core fs.FS) fs.FS {
	type candidate struct {
		id   extension.ID
		fsys fs.FS
	}

	var found []candidate
	for _, r := range extension.Capability[extension.FrontendReplacer](mgr) {
		id := r.ExtensionInfo().ID
		fsys := safeFS(id, "ReplaceFrontend", r.ReplaceFrontend)
		if fsys == nil {
			continue // declined, which is a valid configuration-time answer
		}
		found = append(found, candidate{id: id, fsys: fsys})
	}

	switch len(found) {
	case 0:
		return core
	case 1:
		logging.Info("Core WebUI replaced by extension", "extension", found[0].id)
		return found[0].fsys
	default:
		// Picking one would ship a customer the wrong product. Refusing is the
		// only answer that cannot be silently wrong.
		ids := make([]string, 0, len(found))
		for _, c := range found {
			ids = append(ids, string(c.id))
		}
		logging.Error("Multiple extensions replace the WebUI; keeping the core frontend",
			"extensions", strings.Join(ids, ", "))
		return core
	}
}

// overlays collects the branding overlays, each restricted to the paths its
// extension declared.
func overlays(mgr *extension.Manager) []fs.FS {
	var out []fs.FS
	claimed := make(map[string]extension.ID)

	for _, o := range extension.Capability[extension.FrontendOverlay](mgr) {
		id := o.ExtensionInfo().ID
		fsys := safeFS(id, "OverlayAssets", o.OverlayAssets)
		if fsys == nil {
			continue
		}

		allowed := make(map[string]struct{})
		for _, p := range o.Overrides() {
			clean := cleanAssetPath(p)
			if clean == "" {
				continue
			}
			if owner, dup := claimed[clean]; dup {
				logging.Error("Duplicate WebUI asset override ignored",
					"path", clean, "extension", id, "already_owned_by", owner)
				continue
			}
			claimed[clean] = id
			allowed[clean] = struct{}{}
		}
		if len(allowed) == 0 {
			continue
		}
		logging.Info("Extension overrides WebUI assets", "extension", id, "count", len(allowed))
		out = append(out, restrictedFS{fsys: fsys, allowed: allowed})
	}
	return out
}

// assetSubtrees mounts each FrontendProvider's assets under ext/<AssetPath>/.
func assetSubtrees(mgr *extension.Manager) []fs.FS {
	var out []fs.FS
	for _, p := range extension.Capability[extension.FrontendProvider](mgr) {
		id := p.ExtensionInfo().ID
		seg := strings.Trim(strings.TrimSpace(p.AssetPath()), "/")
		if !validBase(seg) {
			logging.Error("Extension asset path rejected",
				"extension", id, "asset_path", p.AssetPath(),
				"reason", "must be a single segment of lowercase letters, digits, '_' or '-'")
			continue
		}
		fsys := safeFS(id, "Assets", p.Assets)
		if fsys == nil {
			continue
		}
		out = append(out, subtreeFS{prefix: path.Join(AssetPrefix, seg), fsys: fsys})
	}
	return out
}

// Panels returns the merged UI manifest of every loaded FrontendProvider,
// sorted by slot, then declared order, then extension ID, so the shell renders
// the same layout on every start.
//
// A nil manager returns nil, which the endpoint serves as an empty list.
func Panels(mgr *extension.Manager) []Panel {
	if mgr == nil {
		return nil
	}

	var out []Panel
	for _, p := range extension.Capability[extension.FrontendProvider](mgr) {
		id := p.ExtensionInfo().ID
		seg := strings.Trim(strings.TrimSpace(p.AssetPath()), "/")
		if !validBase(seg) {
			continue // already reported by assetSubtrees
		}
		for _, m := range p.Panels() {
			if m.ID == "" || m.Entry == "" {
				logging.Error("Extension panel ignored", "extension", id,
					"reason", "id and entry are required")
				continue
			}
			if _, ok := knownSlots[m.Slot]; !ok {
				logging.Error("Extension panel ignored", "extension", id,
					"panel", m.ID, "slot", m.Slot, "reason", "unknown slot")
				continue
			}
			entry := cleanAssetPath(m.Entry)
			if entry == "" {
				logging.Error("Extension panel ignored", "extension", id,
					"panel", m.ID, "entry", m.Entry, "reason", "entry escapes the asset root")
				continue
			}
			out = append(out, Panel{
				// Namespacing here is what lets two extensions use the same
				// panel ID without either having to know about the other.
				ID:        string(id) + "." + m.ID,
				Extension: string(id),
				Title:     m.Title,
				Slot:      m.Slot,
				Entry:     "/" + path.Join(AssetPrefix, seg, entry),
				Icon:      m.Icon,
				Order:     m.Order,
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Slot != out[j].Slot {
			return out[i].Slot < out[j].Slot
		}
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// HasFrontendExtensions reports whether anything would change the asset tree,
// so callers can skip the composition entirely on a standard build.
func HasFrontendExtensions(mgr *extension.Manager) bool {
	if mgr == nil {
		return false
	}
	return len(extension.Capability[extension.FrontendProvider](mgr)) > 0 ||
		len(extension.Capability[extension.FrontendOverlay](mgr)) > 0 ||
		len(extension.Capability[extension.FrontendReplacer](mgr)) > 0
}

// safeFS calls an extension method that returns an fs.FS, containing a panic.
// A broken extension must not take the WebUI down with it.
func safeFS(id extension.ID, method string, fn func() fs.FS) (fsys fs.FS) {
	defer func() {
		if r := recover(); r != nil {
			logging.Error("Extension frontend method panicked",
				"extension", id, "method", method, "error", errFromPanic(r))
			fsys = nil
		}
	}()
	return fn()
}

// cleanAssetPath normalises a declared asset path and rejects anything that
// escapes the asset root.
//
// The leading slash is stripped *before* cleaning, not after: path.Clean
// resolves "/../../secret.js" to "/secret.js", so cleaning an absolute form
// would turn an escape attempt into a valid root path instead of rejecting it.
// fs.ValidPath then refuses everything that is still not rooted and relative.
func cleanAssetPath(p string) string {
	clean := path.Clean(strings.TrimLeft(strings.TrimSpace(p), "/"))
	if clean == "." || !fs.ValidPath(clean) {
		return ""
	}
	return clean
}

// unionFS serves the first layer that has a given path.
type unionFS []fs.FS

func (u unionFS) Open(name string) (fs.File, error) {
	var firstErr error
	for _, layer := range u {
		f, err := layer.Open(name)
		if err == nil {
			return f, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr == nil {
		firstErr = &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return nil, firstErr
}

// ReadDir merges the layers so a directory that exists in more than one is
// listed once, with the first layer winning per entry — the same precedence
// Open uses.
func (u unionFS) ReadDir(name string) ([]fs.DirEntry, error) {
	seen := make(map[string]struct{})
	var out []fs.DirEntry
	var firstErr error

	for _, layer := range u {
		entries, err := fs.ReadDir(layer, name)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, e := range entries {
			if _, dup := seen[e.Name()]; dup {
				continue
			}
			seen[e.Name()] = struct{}{}
			out = append(out, e)
		}
	}
	if out == nil && firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// restrictedFS exposes only an explicit set of paths from an overlay.
type restrictedFS struct {
	fsys    fs.FS
	allowed map[string]struct{}
}

func (r restrictedFS) Open(name string) (fs.File, error) {
	if _, ok := r.allowed[name]; !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return r.fsys.Open(name)
}

// subtreeFS mounts an extension's assets under a prefix in the served tree.
type subtreeFS struct {
	prefix string
	fsys   fs.FS
}

func (s subtreeFS) Open(name string) (fs.File, error) {
	inner, ok := s.strip(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return s.fsys.Open(inner)
}

func (s subtreeFS) ReadDir(name string) ([]fs.DirEntry, error) {
	inner, ok := s.strip(name)
	if !ok {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return fs.ReadDir(s.fsys, inner)
}

// strip maps a served path to a path inside the extension's own tree. Paths
// outside the prefix are not ours, and the prefix itself maps to the root.
func (s subtreeFS) strip(name string) (string, bool) {
	switch {
	case name == s.prefix:
		return ".", true
	case strings.HasPrefix(name, s.prefix+"/"):
		return strings.TrimPrefix(name, s.prefix+"/"), true
	default:
		return "", false
	}
}

// compile-time assertions: the static handler reads through fs.ReadFile and
// fs.Stat, both of which fall back to Open, but http.FileServer wants ReadDir.
var (
	_ fs.ReadDirFS = unionFS(nil)
	_ fs.FS        = restrictedFS{}
	_ fs.ReadDirFS = subtreeFS{}
)
