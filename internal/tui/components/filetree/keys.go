package filetree

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type KeyMap struct {
	Up           key.Binding
	Down         key.Binding
	Expand       key.Binding
	Collapse     key.Binding
	Open         key.Binding
	Search       key.Binding
	Refresh      key.Binding
	CancelSearch key.Binding
}

var _ help.KeyMap = KeyMap{}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/↑", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/↓", "move down"),
		),
		Expand: key.NewBinding(
			key.WithKeys("l", "right"),
			key.WithHelp("l/→", "expand"),
		),
		Collapse: key.NewBinding(
			key.WithKeys("h", "left"),
			key.WithHelp("h/←", "collapse"),
		),
		Open: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open/toggle"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		CancelSearch: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "clear search"),
		),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Open, k.Search}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Expand, k.Collapse},
		{k.Open, k.Search, k.Refresh, k.CancelSearch},
	}
}
