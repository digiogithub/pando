package dialog

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	"github.com/sahilm/fuzzy"
)

type CommandCategory string

const (
	CommandCategoryGeneral  CommandCategory = "General"
	CommandCategoryFiles    CommandCategory = "Files"
	CommandCategorySessions CommandCategory = "Sessions"
	CommandCategoryModels   CommandCategory = "Models"
	CommandCategoryView     CommandCategory = "View"
	CommandCategoryAccount  CommandCategory = "Account"

	commandDialogMinWidth        = 56
	commandDialogMaxVisibleItems = 8
)

var commandCategoryOrder = []CommandCategory{
	CommandCategoryGeneral,
	CommandCategoryFiles,
	CommandCategorySessions,
	CommandCategoryModels,
	CommandCategoryView,
	CommandCategoryAccount,
}

// Command represents a command that can be executed.
type Command struct {
	ID          string
	Title       string
	Description string
	Shortcut    string
	Category    CommandCategory
	Handler     func(cmd Command) tea.Cmd
}

type commandMatch struct {
	command        Command
	matchedIndexes []int
	score          int
}

func (c Command) normalizedCategory() CommandCategory {
	switch c.Category {
	case CommandCategoryFiles, CommandCategorySessions, CommandCategoryModels, CommandCategoryView, CommandCategoryAccount:
		return c.Category
	default:
		return CommandCategoryGeneral
	}
}

func commandCategoryRank(category CommandCategory) int {
	for idx, candidate := range commandCategoryOrder {
		if candidate == category {
			return idx
		}
	}
	return len(commandCategoryOrder)
}

func normalizeCommands(commands []Command) []Command {
	normalized := make([]Command, 0, len(commands))
	for _, cmd := range commands {
		cmd.Category = cmd.normalizedCategory()
		normalized = append(normalized, cmd)
	}
	return normalized
}

func sortCommands(commands []Command) {
	sort.SliceStable(commands, func(i, j int) bool {
		left := commands[i]
		right := commands[j]
		if left.normalizedCategory() != right.normalizedCategory() {
			return commandCategoryRank(left.normalizedCategory()) < commandCategoryRank(right.normalizedCategory())
		}
		return strings.ToLower(left.Title) < strings.ToLower(right.Title)
	})
}

func (ci Command) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	titleLineStyle := baseStyle.Width(width).Padding(0, 1)
	descStyle := baseStyle.Width(width).Padding(0, 1).Foreground(t.TextMuted())
	titleStyle := baseStyle.Foreground(t.Text())
	matchStyle := titleStyle.Foreground(t.Primary()).Bold(true)
	shortcutStyle := baseStyle.Foreground(t.TextMuted())

	if selected {
		titleLineStyle = titleLineStyle.
			Background(t.SelectionBackground()).
			Foreground(t.SelectionForeground()).
			Bold(true)
		descStyle = descStyle.
			Background(t.SelectionBackground()).
			Foreground(t.SelectionForeground())
		titleStyle = titleStyle.Foreground(t.SelectionForeground()).Bold(true)
		matchStyle = matchStyle.Foreground(t.SelectionForeground()).Bold(true).Underline(true)
		shortcutStyle = shortcutStyle.Foreground(t.SelectionForeground())
	}

	title := titleStyle.Render(ci.Title)
	if ci.Shortcut != "" {
		title = lipgloss.JoinHorizontal(
			lipgloss.Left,
			title,
			baseStyle.Render("  "),
			shortcutStyle.Render(ci.Shortcut),
		)
	}

	title = titleLineStyle.Render(title)
	if ci.Description != "" {
		description := descStyle.Render(ci.Description)
		return lipgloss.JoinVertical(lipgloss.Left, title, description)
	}
	return title
}

func (m commandMatch) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	titleLineStyle := baseStyle.Width(width).Padding(0, 1)
	descStyle := baseStyle.Width(width).Padding(0, 1).Foreground(t.TextMuted())
	titleStyle := baseStyle.Foreground(t.Text())
	matchStyle := titleStyle.Foreground(t.Primary()).Bold(true)
	shortcutStyle := baseStyle.Foreground(t.TextMuted())

	if selected {
		titleLineStyle = titleLineStyle.
			Background(t.SelectionBackground()).
			Foreground(t.SelectionForeground()).
			Bold(true)
		descStyle = descStyle.
			Background(t.SelectionBackground()).
			Foreground(t.SelectionForeground())
		titleStyle = titleStyle.Foreground(t.SelectionForeground()).Bold(true)
		matchStyle = matchStyle.Foreground(t.SelectionForeground()).Bold(true).Underline(true)
		shortcutStyle = shortcutStyle.Foreground(t.SelectionForeground())
	}

	title := renderMatchedText(m.command.Title, m.matchedIndexes, titleStyle, matchStyle)
	if m.command.Shortcut != "" {
		title = lipgloss.JoinHorizontal(
			lipgloss.Left,
			title,
			baseStyle.Render("  "),
			shortcutStyle.Render(m.command.Shortcut),
		)
	}

	title = titleLineStyle.Render(title)
	if m.command.Description != "" {
		description := descStyle.Render(m.command.Description)
		return lipgloss.JoinVertical(lipgloss.Left, title, description)
	}
	return title
}

// CommandSelectedMsg is sent when a command is selected.
type CommandSelectedMsg struct {
	Command Command
}

// CloseCommandDialogMsg is sent when the command dialog is closed.
type CloseCommandDialogMsg struct{}

type OpenSessionDialogMsg struct{}
type OpenModelDialogMsg struct{}
type OpenThemeDialogMsg struct{}
type OpenFilepickerMsg struct{}

// CommandDialog interface for the command selection dialog.
type CommandDialog interface {
	tea.Model
	layout.Bindings
	SetCommands(commands []Command)
}

type commandDialogCmp struct {
	commands    []Command
	filtered    []commandMatch
	selectedIdx int
	width       int
	height      int
	queryInput  textinput.Model
}

type commandKeyMap struct {
	Enter  key.Binding
	Escape key.Binding
	Up     key.Binding
	Down   key.Binding
	J      key.Binding
	K      key.Binding
}

var commandKeys = commandKeyMap{
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select command"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑", "previous command"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↓", "next command"),
	),
	J: key.NewBinding(
		key.WithKeys("j"),
		key.WithHelp("j", "next command"),
	),
	K: key.NewBinding(
		key.WithKeys("k"),
		key.WithHelp("k", "previous command"),
	),
}

func (c *commandDialogCmp) Init() tea.Cmd {
	c.queryInput.Focus()
	return textinput.Blink
}

func (c *commandDialogCmp) filterCommands() {
	query := strings.TrimSpace(c.queryInput.Value())

	if query == "" {
		commands := normalizeCommands(c.commands)
		sortCommands(commands)

		c.filtered = make([]commandMatch, 0, len(commands))
		for _, cmd := range commands {
			c.filtered = append(c.filtered, commandMatch{command: cmd})
		}
		c.selectedIdx = 0
		return
	}

	titles := make([]string, 0, len(c.commands))
	normalized := normalizeCommands(c.commands)
	for _, cmd := range normalized {
		titles = append(titles, cmd.Title)
	}

	matches := fuzzy.Find(strings.ToLower(query), lowerStrings(titles))
	filtered := make([]commandMatch, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, commandMatch{
			command:        normalized[match.Index],
			matchedIndexes: match.MatchedIndexes,
			score:          match.Score,
		})
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].score != filtered[j].score {
			return filtered[i].score > filtered[j].score
		}
		if filtered[i].command.normalizedCategory() != filtered[j].command.normalizedCategory() {
			return commandCategoryRank(filtered[i].command.normalizedCategory()) < commandCategoryRank(filtered[j].command.normalizedCategory())
		}
		return strings.ToLower(filtered[i].command.Title) < strings.ToLower(filtered[j].command.Title)
	})

	c.filtered = filtered
	if len(c.filtered) == 0 {
		c.selectedIdx = 0
		return
	}
	if c.selectedIdx >= len(c.filtered) {
		c.selectedIdx = len(c.filtered) - 1
	}
}

func lowerStrings(values []string) []string {
	lowered := make([]string, 0, len(values))
	for _, value := range values {
		lowered = append(lowered, strings.ToLower(value))
	}
	return lowered
}

func (c *commandDialogCmp) moveSelection(offset int) {
	if len(c.filtered) == 0 {
		c.selectedIdx = 0
		return
	}

	c.selectedIdx += offset
	if c.selectedIdx < 0 {
		c.selectedIdx = 0
	}
	if c.selectedIdx >= len(c.filtered) {
		c.selectedIdx = len(c.filtered) - 1
	}
}

func (c *commandDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, commandKeys.Enter):
			if len(c.filtered) == 0 {
				return c, nil
			}
			return c, util.CmdHandler(CommandSelectedMsg{
				Command: c.filtered[c.selectedIdx].command,
			})
		case key.Matches(msg, commandKeys.Escape):
			return c, util.CmdHandler(CloseCommandDialogMsg{})
		case key.Matches(msg, commandKeys.Up) || key.Matches(msg, commandKeys.K):
			c.moveSelection(-1)
			return c, nil
		case key.Matches(msg, commandKeys.Down) || key.Matches(msg, commandKeys.J):
			c.moveSelection(1)
			return c, nil
		}

		var cmd tea.Cmd
		c.queryInput, cmd = c.queryInput.Update(msg)
		c.filterCommands()
		return c, cmd
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
	}

	return c, nil
}

func (c *commandDialogCmp) visibleMatches() []commandMatch {
	if len(c.filtered) <= commandDialogMaxVisibleItems {
		return c.filtered
	}

	startIdx := 0
	halfVisible := commandDialogMaxVisibleItems / 2
	if c.selectedIdx >= halfVisible && c.selectedIdx < len(c.filtered)-halfVisible {
		startIdx = c.selectedIdx - halfVisible
	} else if c.selectedIdx >= len(c.filtered)-halfVisible {
		startIdx = len(c.filtered) - commandDialogMaxVisibleItems
	}

	endIdx := min(startIdx+commandDialogMaxVisibleItems, len(c.filtered))
	return c.filtered[startIdx:endIdx]
}

func (c *commandDialogCmp) maxContentWidth() int {
	maxWidth := commandDialogMinWidth
	for _, match := range c.filtered {
		lineWidth := lipgloss.Width(match.command.Title) + 4
		if match.command.Shortcut != "" {
			lineWidth += lipgloss.Width(match.command.Shortcut) + 2
		}
		if match.command.Description != "" {
			lineWidth = max(lineWidth, lipgloss.Width(match.command.Description)+4)
		}
		maxWidth = max(maxWidth, lineWidth)
	}

	if c.width > 0 {
		return max(commandDialogMinWidth, min(maxWidth, c.width-10))
	}
	return maxWidth
}

func (c *commandDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	dialogWidth := c.maxContentWidth()
	queryStyle := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.TextMuted())

	c.queryInput.Width = max(0, dialogWidth-4)
	query := queryStyle.Render(c.queryInput.View())

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(dialogWidth).
		Padding(0, 1).
		Render("Commands")

	var lines []string
	if len(c.filtered) == 0 {
		lines = append(lines, baseStyle.Width(dialogWidth).Padding(0, 1).Render("No commands found"))
	} else {
		visible := c.visibleMatches()
		var lastCategory CommandCategory
		for _, match := range visible {
			if match.command.normalizedCategory() != lastCategory {
				lastCategory = match.command.normalizedCategory()
				lines = append(lines, baseStyle.
					Width(dialogWidth).
					Padding(0, 1).
					Foreground(t.TextMuted()).
					Bold(true).
					Render(string(lastCategory)))
			}
			lines = append(lines, match.Render(c.filtered[c.selectedIdx].command.ID == match.command.ID, dialogWidth))
		}
	}

	footer := baseStyle.
		Width(dialogWidth).
		Padding(0, 1).
		Foreground(t.TextMuted()).
		Render("Type to filter, enter to run")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		baseStyle.Width(dialogWidth).Render(""),
		query,
		baseStyle.Width(dialogWidth).Render(""),
		lipgloss.JoinVertical(lipgloss.Left, lines...),
		baseStyle.Width(dialogWidth).Render(""),
		footer,
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (c *commandDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(commandKeys)
}

func (c *commandDialogCmp) SetCommands(commands []Command) {
	c.commands = normalizeCommands(commands)
	c.queryInput.SetValue("")
	c.queryInput.CursorEnd()
	c.queryInput.Focus()
	c.selectedIdx = 0
	c.filterCommands()
}

// NewCommandDialogCmp creates a new command selection dialog.
func NewCommandDialogCmp() CommandDialog {
	input := textinput.New()
	input.Placeholder = "Search commands"
	input.Prompt = "> "
	input.CharLimit = 128

	dialog := &commandDialogCmp{
		queryInput: input,
	}
	dialog.filterCommands()
	return dialog
}
