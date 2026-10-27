package kit

import (
	"embed"
	"sort"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// icons holds a subset of Lucide (ISC, see icons/LICENSE), the set the Web UI
// uses through lucide-react. Add an icon by copying its SVG from lucide into
// ./icons; names are lucide's file names.
//
//go:embed icons/*.svg
var iconFS embed.FS

// Icon names used by the shell and the views. Prefer these constants over
// raw strings so a missing icon fails at review, not at run time.
const (
	IconChat        = "message-square"
	IconSimpleChat  = "message-square-plus"
	IconProjects    = "folder-open"
	IconFolder      = "folder"
	IconFile        = "file"
	IconFileText    = "file-text"
	IconEditor      = "code"
	IconTerminal    = "square-terminal"
	IconOrchestrate = "network"
	IconDesign      = "palette"
	IconLogs        = "scroll-text"
	IconAgentVCS    = "git-branch"
	IconEvaluator   = "sparkles"
	IconInstances   = "server"
	IconSettings    = "settings"
	IconExtensions  = "puzzle"
	IconSetup       = "rocket"
	IconPlus        = "plus"
	IconSend        = "send"
	IconStop        = "square"
	IconClose       = "x"
	IconCheck       = "check"
	IconCopy        = "copy"
	IconTrash       = "trash"
	IconEdit        = "pencil"
	IconChevronDown = "chevron-down"
	IconChevronUp   = "chevron-up"
	IconChevronR    = "chevron-right"
	IconChevronL    = "chevron-left"
	IconSearch      = "search"
	IconRefresh     = "refresh-cw"
	IconSpinner     = "loader-circle"
	IconError       = "circle-alert"
	IconWarning     = "triangle-alert"
	IconInfo        = "info"
	IconSuccess     = "circle-check"
	IconThinking    = "brain"
	IconTool        = "wrench"
	IconSun         = "sun"
	IconMoon        = "moon"
	IconMonitor     = "monitor"
	IconPanelLeft   = "panel-left"
	IconPanelRight  = "panel-right"
	IconMore        = "ellipsis"
	IconArrowDown   = "arrow-down"
	IconArrowUp     = "arrow-up"
	IconExternal    = "external-link"
	IconBot         = "bot"
	IconUser        = "user"
	IconClock       = "clock"
	IconTodo        = "list-todo"
	IconHome        = "house"
	IconGlobe       = "globe"
)

var (
	iconMu    sync.Mutex
	iconCache = map[string]*ui.SVG{}
)

// SVG returns the embedded icon name, parsed once. An unknown name returns
// the "circle-alert" icon so a typo shows instead of crashing.
func SVG(name string) *ui.SVG {
	iconMu.Lock()
	defer iconMu.Unlock()
	if s, ok := iconCache[name]; ok {
		return s
	}
	data, err := iconFS.ReadFile("icons/" + name + ".svg")
	if err != nil {
		if name == IconError {
			return nil
		}
		iconMu.Unlock()
		s := SVG(IconError)
		iconMu.Lock()
		return s
	}
	s, err := ui.ParseSVG(data)
	if err != nil {
		return nil
	}
	iconCache[name] = s
	return s
}

// HasIcon reports whether an icon is embedded.
func HasIcon(name string) bool {
	_, err := iconFS.ReadFile("icons/" + name + ".svg")
	return err == nil
}

// IconNames lists the embedded icons.
func IconNames() []string {
	entries, _ := iconFS.ReadDir("icons")
	var out []string
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".svg"); ok {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Icon shows an embedded icon in the current text colour, sized to the
// font size unless resized with FontSize or Size.
func Icon(c *ui.Context, name string) ui.Element {
	return ui.Icon(c, SVG(name))
}
