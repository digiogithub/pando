package dialog

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

type helpCmp struct {
	width    int
	height   int
	keys     []key.Binding
	sections []HelpSection
	viewport viewport.Model
	ready    bool
}

type HelpSection struct {
	Title    string
	Bindings []key.Binding
}

func (h *helpCmp) Init() tea.Cmd {
	return nil
}

func (h *helpCmp) SetBindings(k []key.Binding) {
	h.keys = k
	h.sections = nil
}

func (h *helpCmp) SetSections(sections []HelpSection) {
	h.sections = sections
	h.keys = nil
}

func (h *helpCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width = 90
		h.height = msg.Height
		maxHeight := min(h.height-8, 40)
		if !h.ready {
			h.viewport = viewport.New(h.width-4, maxHeight)
			h.viewport.MouseWheelEnabled = true
			h.ready = true
		} else {
			h.viewport.Width = h.width - 4
			h.viewport.Height = maxHeight
		}
	case tea.KeyMsg:
		h.viewport, cmd = h.viewport.Update(msg)
		return h, cmd
	case tea.MouseMsg:
		h.viewport, cmd = h.viewport.Update(msg)
		return h, cmd
	}
	return h, nil
}

func removeDuplicateBindings(bindings []key.Binding) []key.Binding {
	seen := make(map[string]struct{})
	result := make([]key.Binding, 0, len(bindings))

	// Process bindings in reverse order
	for i := len(bindings) - 1; i >= 0; i-- {
		b := bindings[i]
		k := strings.Join(b.Keys(), " ")
		if _, ok := seen[k]; ok {
			// duplicate, skip
			continue
		}
		seen[k] = struct{}{}
		// Add to the beginning of result to maintain original order
		result = append([]key.Binding{b}, result...)
	}

	return result
}

func (h *helpCmp) normalizeSections() []HelpSection {
	if len(h.sections) > 0 {
		sections := make([]HelpSection, 0, len(h.sections))
		for _, section := range h.sections {
			bindings := removeDuplicateBindings(section.Bindings)
			if len(bindings) == 0 {
				continue
			}
			sections = append(sections, HelpSection{
				Title:    section.Title,
				Bindings: bindings,
			})
		}
		return sections
	}

	bindings := removeDuplicateBindings(h.keys)
	if len(bindings) == 0 {
		return nil
	}

	return []HelpSection{{
		Title:    "Shortcuts",
		Bindings: bindings,
	}}
}

func (h *helpCmp) renderSection(title string, bindings []key.Binding) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	helpKeyStyle := styles.Bold().
		Background(t.Background()).
		Foreground(t.Text()).
		Padding(0, 1, 0, 0)

	helpDescStyle := styles.Regular().
		Background(t.Background()).
		Foreground(t.TextMuted())

	titleStyle := styles.Bold().
		Background(t.Background()).
		Foreground(t.Primary())

	maxKeyWidth := 0
	lines := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		renderedKey := helpKeyStyle.Render(binding.Help().Key)
		if width := lipgloss.Width(renderedKey); width > maxKeyWidth {
			maxKeyWidth = width
		}
	}

	lineStyle := lipgloss.NewStyle().
		Background(t.Background()).
		Width(h.width - 6)

	for _, binding := range bindings {
		renderedKey := helpKeyStyle.Render(binding.Help().Key)
		keyPadding := max(0, maxKeyWidth-lipgloss.Width(renderedKey))
		line := lipgloss.JoinHorizontal(
			lipgloss.Top,
			renderedKey+baseStyle.Render(strings.Repeat(" ", keyPadding+2)),
			helpDescStyle.Render(binding.Help().Desc),
		)
		lines = append(lines, lineStyle.Render(line))
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render(title),
		lipgloss.JoinVertical(lipgloss.Left, lines...),
	)
}

func (h *helpCmp) render() string {
	t := theme.CurrentTheme()
	contentWidth := h.width - 6

	sections := h.normalizeSections()
	rendered := make([]string, 0, len(sections))
	for _, section := range sections {
		rendered = append(rendered, h.renderSection(section.Title, section.Bindings))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, rendered...)
	return lipgloss.NewStyle().
		Background(t.Background()).
		Width(contentWidth).
		Render(content)
}

func (h *helpCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	content := h.render()

	if h.ready {
		h.viewport.SetContent(content)
	}

	header := baseStyle.
		Bold(true).
		Width(h.width - 4).
		Foreground(t.Primary()).
		Render("Keyboard Shortcuts")

	scrollInfo := ""
	if h.ready && h.viewport.TotalLineCount() > h.viewport.Height {
		percent := int(h.viewport.ScrollPercent() * 100)
		scrollInfoStyle := lipgloss.NewStyle().
			Background(t.Background()).
			Foreground(t.TextMuted())
		scrollInfo = scrollInfoStyle.Render(fmt.Sprintf(" %d%% ↑↓", percent))
	}

	var viewportContent string
	if h.ready {
		viewportContent = h.viewport.View()
	} else {
		viewportContent = content
	}

	return baseStyle.Padding(1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.TextMuted()).
		Width(h.width).
		BorderBackground(t.Background()).
		Render(
			lipgloss.JoinVertical(lipgloss.Center,
				header,
				baseStyle.Render(strings.Repeat(" ", lipgloss.Width(header))),
				viewportContent,
				scrollInfo,
			),
		)
}

type HelpCmp interface {
	tea.Model
	SetBindings([]key.Binding)
	SetSections([]HelpSection)
}

func NewHelpCmp() HelpCmp {
	return &helpCmp{}
}
