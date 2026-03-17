package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type KeyMap struct {
	Global   GlobalKeys
	Chat     ChatKeys
	Editor   EditorKeys
	FileTree FileTreeKeys
	Dialog   DialogKeys
}

type GlobalKeys struct {
	Logs           key.Binding
	Orchestrator   key.Binding
	Snapshots      key.Binding
	Evaluator      key.Binding
	Quit           key.Binding
	Help           key.Binding
	Settings       key.Binding
	Filepicker     key.Binding
	SwitchTheme    key.Binding
	ToggleTerminal key.Binding
	NewTerminal    key.Binding
}

type ChatKeys struct {
	Send                 key.Binding
	NewLine              key.Binding
	Cancel               key.Binding
	PageDown             key.Binding
	PageUp               key.Binding
	HalfPageUp           key.Binding
	HalfPageDown         key.Binding
	SwitchSession        key.Binding
	NewSession           key.Binding
	Commands             key.Binding
	Models               key.Binding
	ShowCompletionDialog key.Binding
	ToggleSidebar        key.Binding
	NextPanel            key.Binding
}

type EditorKeys struct {
	Close        key.Binding
	Search       key.Binding
	NextTab      key.Binding
	PrevTab      key.Binding
	EditExternal key.Binding
	Save         key.Binding
}

type FileTreeKeys struct {
	Up      key.Binding
	Down    key.Binding
	Open    key.Binding
	Search  key.Binding
	Expand  key.Binding
	Collapse key.Binding
	Refresh  key.Binding
	NewFile  key.Binding
}

type DialogKeys struct {
	Confirm  key.Binding
	Cancel   key.Binding
	Navigate key.Binding
}

var (
	_ help.KeyMap = GlobalKeys{}
	_ help.KeyMap = ChatKeys{}
	_ help.KeyMap = EditorKeys{}
	_ help.KeyMap = FileTreeKeys{}
	_ help.KeyMap = DialogKeys{}
)

const quitKey = "q"

var (
	helpEsc = key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	)

	returnKey = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	)

	logsKeyReturnKey = key.NewBinding(
		key.WithKeys("esc", "backspace", quitKey),
		key.WithHelp("esc/q", "go back"),
	)
)

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Global: GlobalKeys{
			Logs: key.NewBinding(
				key.WithKeys("ctrl+l"),
				key.WithHelp("ctrl+l", "logs"),
			),
			Orchestrator: key.NewBinding(
				key.WithKeys("ctrl+m"),
				key.WithHelp("ctrl+m", "orchestrator"),
			),
			Snapshots: key.NewBinding(
				key.WithKeys("ctrl+shift+s"),
				key.WithHelp("ctrl+shift+s", "snapshots"),
			),
			Quit: key.NewBinding(
				key.WithKeys("ctrl+c"),
				key.WithHelp("ctrl+c", "quit"),
			),
			Help: key.NewBinding(
				key.WithKeys("ctrl+h", "ctrl+_"),
				key.WithHelp("ctrl+h", "toggle help"),
			),
			Settings: key.NewBinding(
				key.WithKeys("ctrl+g"),
				key.WithHelp("ctrl+g", "settings"),
			),
			Filepicker: key.NewBinding(
				key.WithKeys("ctrl+f"),
				key.WithHelp("ctrl+f", "select files to upload"),
			),
			SwitchTheme: key.NewBinding(
				key.WithKeys("ctrl+t"),
				key.WithHelp("ctrl+t", "switch theme"),
			),
			ToggleTerminal: key.NewBinding(
				key.WithKeys("ctrl+shift+t"),
				key.WithHelp("ctrl+shift+t", "terminal: open/focus/unfocus"),
			),
			NewTerminal: key.NewBinding(
				key.WithKeys("ctrl+alt+t"),
				key.WithHelp("ctrl+alt+t", "new terminal tab"),
			),
		},
		Chat: ChatKeys{
			Send: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "send message"),
			),
			NewLine: key.NewBinding(
				key.WithKeys("shift+enter", "ctrl+j"),
				key.WithHelp("shift+enter", "new line"),
			),
			Cancel: key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "cancel"),
			),
			PageDown: key.NewBinding(
				key.WithKeys("pgdown"),
				key.WithHelp("f/pgdn", "page down"),
			),
			PageUp: key.NewBinding(
				key.WithKeys("pgup"),
				key.WithHelp("b/pgup", "page up"),
			),
			HalfPageUp: key.NewBinding(
				key.WithKeys("ctrl+u"),
				key.WithHelp("ctrl+u", "½ page up"),
			),
			HalfPageDown: key.NewBinding(
				key.WithKeys("ctrl+d"),
				key.WithHelp("ctrl+d", "½ page down"),
			),
			SwitchSession: key.NewBinding(
				key.WithKeys("ctrl+s"),
				key.WithHelp("ctrl+s", "switch session"),
			),
			NewSession: key.NewBinding(
				key.WithKeys("ctrl+n"),
				key.WithHelp("ctrl+n", "new session"),
			),
			Commands: key.NewBinding(
				key.WithKeys("ctrl+k", "ctrl+p"),
				key.WithHelp("ctrl+k/ctrl+p", "commands"),
			),
			Models: key.NewBinding(
				key.WithKeys("ctrl+o"),
				key.WithHelp("ctrl+o", "model selection"),
			),
			ShowCompletionDialog: key.NewBinding(
				key.WithKeys("@"),
				key.WithHelp("@", "complete"),
			),
			ToggleSidebar: key.NewBinding(
				key.WithKeys("ctrl+b"),
				key.WithHelp("ctrl+b", "toggle sidebar"),
			),
			NextPanel: key.NewBinding(
				key.WithKeys("tab", "ctrl+["),
				key.WithHelp("tab", "switch panel"),
			),
		},
		Editor: EditorKeys{
			Close: key.NewBinding(
				key.WithKeys("esc", "ctrl+w"),
				key.WithHelp("esc/ctrl+w", "close editor/tab"),
			),
			Search: key.NewBinding(
				key.WithKeys("/", "ctrl+f"),
				key.WithHelp("/,ctrl+f", "search"),
			),
			NextTab: key.NewBinding(
				key.WithKeys("ctrl+tab", "ctrl+pgdown"),
				key.WithHelp("ctrl+tab", "next tab"),
			),
			PrevTab: key.NewBinding(
				key.WithKeys("ctrl+shift+tab", "shift+ctrl+tab", "ctrl+pgup"),
				key.WithHelp("ctrl+shift+tab", "prev tab"),
			),
			EditExternal: key.NewBinding(
				key.WithKeys("ctrl+e"),
				key.WithHelp("ctrl+e", "edit file inline"),
			),
			Save: key.NewBinding(
				key.WithKeys("ctrl+s"),
				key.WithHelp("ctrl+s", "save file"),
			),
		},
		FileTree: FileTreeKeys{
			Up: key.NewBinding(
				key.WithKeys("k", "up"),
				key.WithHelp("k/↑", "move up"),
			),
			Down: key.NewBinding(
				key.WithKeys("j", "down"),
				key.WithHelp("j/↓", "move down"),
			),
			Open: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("enter", "open"),
			),
			Search: key.NewBinding(
				key.WithKeys("/"),
				key.WithHelp("/", "search"),
			),
			Expand: key.NewBinding(
				key.WithKeys("l", "right"),
				key.WithHelp("l/→", "expand"),
			),
			Collapse: key.NewBinding(
				key.WithKeys("h", "left"),
				key.WithHelp("h/←", "collapse"),
			),
			Refresh: key.NewBinding(
				key.WithKeys("r"),
				key.WithHelp("r", "refresh"),
			),
			NewFile: key.NewBinding(
				key.WithKeys("ctrl+shift+n"),
				key.WithHelp("ctrl+shift+n", "new file"),
			),
		},
		Dialog: DialogKeys{},
	}
}

func (k GlobalKeys) ShortHelp() []key.Binding {
	return filterHelpBindings(
		k.Help,
		k.Logs,
		k.Orchestrator,
		k.Snapshots,
		k.Settings,
		k.Filepicker,
		k.SwitchTheme,
		k.ToggleTerminal,
		k.Quit,
	)
}

func (k GlobalKeys) FullHelp() [][]key.Binding {
	return compactHelpGroups(
		filterHelpBindings(k.Help, k.Quit),
		filterHelpBindings(k.Logs, k.Orchestrator, k.Snapshots, k.Settings),
		filterHelpBindings(k.Filepicker, k.SwitchTheme),
		filterHelpBindings(k.ToggleTerminal, k.NewTerminal),
	)
}

func (k GlobalKeys) Bindings() []key.Binding {
	return flattenHelpGroups(k.FullHelp())
}

func (k ChatKeys) ShortHelp() []key.Binding {
	return filterHelpBindings(
		k.Send,
		k.ShowCompletionDialog,
		k.ToggleSidebar,
		k.NextPanel,
		k.SwitchSession,
		k.Commands,
		k.Cancel,
	)
}

func (k ChatKeys) FullHelp() [][]key.Binding {
	return compactHelpGroups(
		filterHelpBindings(k.Send, k.NewLine, k.Cancel),
		filterHelpBindings(k.PageUp, k.PageDown, k.HalfPageUp, k.HalfPageDown),
		filterHelpBindings(k.SwitchSession, k.NewSession, k.Commands, k.Models, k.ShowCompletionDialog),
		filterHelpBindings(k.ToggleSidebar, k.NextPanel),
	)
}

func (k ChatKeys) Bindings() []key.Binding {
	return flattenHelpGroups(k.FullHelp())
}

func (k EditorKeys) ShortHelp() []key.Binding {
	return filterHelpBindings(k.Close, k.Search, k.NextTab, k.PrevTab, k.EditExternal, k.Save)
}

func (k EditorKeys) FullHelp() [][]key.Binding {
	return compactHelpGroups(k.ShortHelp())
}

func (k EditorKeys) Bindings() []key.Binding {
	return flattenHelpGroups(k.FullHelp())
}

func (k FileTreeKeys) ShortHelp() []key.Binding {
	return filterHelpBindings(k.Up, k.Down, k.Open, k.Search, k.NewFile)
}

func (k FileTreeKeys) FullHelp() [][]key.Binding {
	return compactHelpGroups(
		filterHelpBindings(k.Up, k.Down, k.Open, k.Search),
		filterHelpBindings(k.Expand, k.Collapse, k.Refresh, k.NewFile),
	)
}

func (k FileTreeKeys) Bindings() []key.Binding {
	return flattenHelpGroups(k.FullHelp())
}

func (k DialogKeys) ShortHelp() []key.Binding {
	return filterHelpBindings(k.Confirm, k.Cancel, k.Navigate)
}

func (k DialogKeys) FullHelp() [][]key.Binding {
	return compactHelpGroups(k.ShortHelp())
}

func (k DialogKeys) Bindings() []key.Binding {
	return flattenHelpGroups(k.FullHelp())
}

func filterHelpBindings(bindings ...key.Binding) []key.Binding {
	filtered := make([]key.Binding, 0, len(bindings))
	for _, binding := range bindings {
		if len(binding.Keys()) == 0 {
			continue
		}
		if binding.Help().Key == "" && binding.Help().Desc == "" {
			continue
		}
		filtered = append(filtered, binding)
	}
	return filtered
}

func compactHelpGroups(groups ...[]key.Binding) [][]key.Binding {
	compacted := make([][]key.Binding, 0, len(groups))
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		compacted = append(compacted, group)
	}
	return compacted
}

func flattenHelpGroups(groups [][]key.Binding) []key.Binding {
	var bindings []key.Binding
	for _, group := range groups {
		bindings = append(bindings, group...)
	}
	return bindings
}
