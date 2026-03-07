package diff

import (
	"github.com/charmbracelet/lipgloss"
	tuistyles "github.com/digiogithub/pando/internal/tui/styles"
	tuitheme "github.com/digiogithub/pando/internal/tui/theme"
)

// DiffStyles contains the lipgloss styles used by DiffView.
type DiffStyles struct {
	Header        lipgloss.Style
	HeaderPath    lipgloss.Style
	HeaderStats   lipgloss.Style
	HeaderMode    lipgloss.Style
	HunkHeader    lipgloss.Style
	Gap           lipgloss.Style
	AddedLine     lipgloss.Style
	RemovedLine   lipgloss.Style
	ContextLine   lipgloss.Style
	AddedNumber   lipgloss.Style
	RemovedNumber lipgloss.Style
	ContextNumber lipgloss.Style
	AddedMarker   lipgloss.Style
	RemovedMarker lipgloss.Style
	ContextMarker lipgloss.Style
	Separator     lipgloss.Style
	Placeholder   lipgloss.Style
}

// NewDiffStyles builds styles using the current TUI theme.
func NewDiffStyles(theme tuitheme.Theme) DiffStyles {
	if theme == nil {
		theme = tuitheme.CurrentTheme()
	}

	base := tuistyles.BaseStyle()
	return DiffStyles{
		Header: base.
			Background(theme.BackgroundSecondary()).
			Foreground(theme.Text()).
			Padding(0, 1),
		HeaderPath: base.
			Background(theme.BackgroundSecondary()).
			Foreground(theme.TextEmphasized()).
			Bold(true),
		HeaderStats: base.
			Background(theme.BackgroundSecondary()).
			Foreground(theme.TextMuted()),
		HeaderMode: base.
			Background(theme.BackgroundSecondary()).
			Foreground(theme.Primary()).
			Bold(true),
		HunkHeader: base.
			Background(theme.BackgroundSecondary()).
			Foreground(theme.DiffHunkHeader()).
			Padding(0, 1),
		Gap: base.
			Foreground(theme.TextMuted()).
			Padding(0, 1),
		AddedLine: base.
			Background(theme.DiffAddedBg()).
			Foreground(theme.DiffAdded()),
		RemovedLine: base.
			Background(theme.DiffRemovedBg()).
			Foreground(theme.DiffRemoved()),
		ContextLine: base.
			Background(theme.Background()).
			Foreground(theme.DiffContext()),
		AddedNumber: base.
			Background(theme.DiffAddedLineNumberBg()).
			Foreground(theme.DiffLineNumber()),
		RemovedNumber: base.
			Background(theme.DiffRemovedLineNumberBg()).
			Foreground(theme.DiffLineNumber()),
		ContextNumber: base.
			Background(theme.Background()).
			Foreground(theme.DiffLineNumber()),
		AddedMarker: base.
			Background(theme.DiffAddedBg()).
			Foreground(theme.DiffAdded()).
			Bold(true),
		RemovedMarker: base.
			Background(theme.DiffRemovedBg()).
			Foreground(theme.DiffRemoved()).
			Bold(true),
		ContextMarker: base.
			Background(theme.Background()).
			Foreground(theme.TextMuted()),
		Separator: base.
			Foreground(theme.BorderDim()),
		Placeholder: base.
			Foreground(theme.TextMuted()).
			Padding(0, 1),
	}
}
