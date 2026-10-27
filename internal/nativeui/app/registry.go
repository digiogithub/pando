package app

import (
	"sort"
	"sync"

	"github.com/egoist/mygo/ui"
)

// DefaultPath is the route shown at start-up.
const DefaultPath = "/chat"

// Section groups sidebar entries.
type Section string

// Sidebar sections, top to bottom.
const (
	SectionMain     Section = "main"     // chat, projects
	SectionTools    Section = "tools"    // editor, terminal, orchestrator, ...
	SectionServices Section = "services" // instances, extensions
	SectionBottom   Section = "bottom"   // settings, pinned to the bottom
	SectionHidden   Section = "hidden"   // routable, not in the sidebar (setup)
)

// View is what an area shows. Build runs on the UI thread every frame the
// area is visible; r is the matched route (r.Param for {id} patterns).
type View interface {
	Build(c *ui.Context, r *ui.Route)
}

// Titled is implemented by views whose header title depends on state (the
// chat shows the session title). The area's TitleKey is used otherwise.
type Titled interface {
	Title() string
}

// HeaderActions is implemented by views that add controls to the header
// bar, on its right.
type HeaderActions interface {
	HeaderActions(c *ui.Context)
}

// Closer is implemented by views that own goroutines or streams to stop
// when the app shuts down.
type Closer interface {
	Close()
}

// Area is a top-level destination of the app (a sidebar entry and its
// routes).
type Area struct {
	// ID is unique and stable ("chat", "settings").
	ID string
	// Path is where the sidebar entry navigates ("/chat").
	Path string
	// Patterns are the router patterns the area builds; Path when empty.
	// Use "/settings/{section...}" style patterns for sub-pages.
	Patterns []string
	// TitleKey is the i18n key of the label and header title ("nav.chat").
	TitleKey string
	// Icon is a kit icon name.
	Icon    string
	Section Section
	// Order sorts entries within a section (ascending).
	Order int
	// New builds the area's view once, the first time it is shown; the view
	// then keeps its state across navigation.
	New func(a *Context) View
	// Placeholder marks areas not built yet.
	Placeholder bool
}

var (
	regMu sync.RWMutex
	areas = map[string]Area{}
)

// Register adds an area, replacing one with the same ID (so a real area
// replaces its placeholder regardless of init order: a non-placeholder
// registration always wins over a placeholder).
func Register(ar Area) {
	if ar.ID == "" || ar.New == nil {
		panic("app: Register needs an ID and New")
	}
	if ar.Path == "" {
		ar.Path = "/" + ar.ID
	}
	if len(ar.Patterns) == 0 {
		ar.Patterns = []string{ar.Path}
	}
	regMu.Lock()
	defer regMu.Unlock()
	if old, ok := areas[ar.ID]; ok && ar.Placeholder && !old.Placeholder {
		return
	}
	areas[ar.ID] = ar
}

// Areas returns the registered areas sorted by section then order.
func Areas() []Area {
	regMu.RLock()
	out := make([]Area, 0, len(areas))
	for _, a := range areas {
		out = append(out, a)
	}
	regMu.RUnlock()
	rank := map[Section]int{SectionMain: 0, SectionTools: 1, SectionServices: 2, SectionBottom: 3, SectionHidden: 4}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Section] != rank[out[j].Section] {
			return rank[out[i].Section] < rank[out[j].Section]
		}
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Lookup returns the area with id.
func Lookup(id string) (Area, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	a, ok := areas[id]
	return a, ok
}
