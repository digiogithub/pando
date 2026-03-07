package editor

import "github.com/charmbracelet/bubbles/key"

// ViewerKeyMap defines the keybindings for the file viewer component.
type ViewerKeyMap struct {
	Down         key.Binding
	Up           key.Binding
	HalfPageDown key.Binding
	HalfPageUp   key.Binding
	Top          key.Binding
	Bottom       key.Binding
	Search       key.Binding
	SearchNext   key.Binding
	SearchPrev   key.Binding
	CancelSearch key.Binding
	Close        key.Binding
}

// DefaultViewerKeyMap returns the default keybindings for the file viewer.
func DefaultViewerKeyMap() ViewerKeyMap {
	return ViewerKeyMap{
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/k", "scroll"),
		),
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("", ""),
		),
		HalfPageDown: key.NewBinding(
			key.WithKeys("ctrl+d"),
			key.WithHelp("ctrl+d/u", "half page"),
		),
		HalfPageUp: key.NewBinding(
			key.WithKeys("ctrl+u"),
			key.WithHelp("", ""),
		),
		Top: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g/G", "top/bottom"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G", "shift+g"),
			key.WithHelp("", ""),
		),
		Search: key.NewBinding(
			key.WithKeys("/", "ctrl+f"),
			key.WithHelp("/,ctrl+f", "search"),
		),
		SearchNext: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n/N", "next/prev match"),
		),
		SearchPrev: key.NewBinding(
			key.WithKeys("N", "shift+n"),
			key.WithHelp("", ""),
		),
		CancelSearch: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel search"),
		),
		Close: key.NewBinding(
			key.WithKeys("esc", "q"),
			key.WithHelp("esc/q", "close viewer"),
		),
	}
}
