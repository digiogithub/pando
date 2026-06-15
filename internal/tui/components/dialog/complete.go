package dialog

import (
	"slices"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/logging"
	utilComponents "github.com/digiogithub/pando/internal/tui/components/util"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
)

type CompletionItem struct {
	title string
	Title string
	Value string
}

type CompletionItemI interface {
	utilComponents.SimpleListItem
	GetValue() string
	DisplayValue() string
}

func renderMatchedText(text string, matchedIndexes []int, baseStyle, matchStyle lipgloss.Style) string {
	if len(matchedIndexes) == 0 {
		return baseStyle.Render(text)
	}

	runes := []rune(text)
	segments := make([]string, 0, len(runes))

	for idx, r := range runes {
		style := baseStyle
		if slices.Contains(matchedIndexes, idx) {
			style = matchStyle
		}
		segments = append(segments, style.Render(string(r)))
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, segments...)
}

func (ci *CompletionItem) Render(selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	itemStyle := baseStyle.
		Width(width).
		Padding(0, 1)

	if selected {
		itemStyle = itemStyle.
			Background(t.Background()).
			Foreground(t.Primary()).
			Bold(true)
	}

	title := itemStyle.Render(
		ci.GetValue(),
	)

	return title
}

func (ci *CompletionItem) DisplayValue() string {
	return ci.Title
}

func (ci *CompletionItem) GetValue() string {
	return ci.Value
}

func NewCompletionItem(completionItem CompletionItem) CompletionItemI {
	return &completionItem
}

type CompletionProvider interface {
	GetId() string
	GetEntry() CompletionItemI
	GetChildEntries(query string) ([]CompletionItemI, error)
}

// AsyncCompletionProvider is an optional interface a CompletionProvider may
// implement when its results are populated progressively (e.g. a fast indexed
// preview served immediately while a full filesystem scan runs in the
// background). RefreshCmd returns a command that resolves once newer results are
// ready, emitting a CompletionRefreshMsg so the dialog can re-query and update
// the visible list. It returns nil when there is nothing left to wait for.
type AsyncCompletionProvider interface {
	CompletionProvider
	RefreshCmd() tea.Cmd
}

// CompletionRefreshMsg asks the completion dialog to re-run the current query
// against its provider because background results have become available.
type CompletionRefreshMsg struct{}

// ResettableCompletionProvider is an optional interface allowing a provider to
// drop cached background results when the dialog is dismissed, so the next time
// it opens it recomputes them (e.g. re-scanning the filesystem for new files).
type ResettableCompletionProvider interface {
	Reset()
}

type CompletionSelectedMsg struct {
	SearchString    string
	CompletionValue string
}

type CompletionDialogCompleteItemMsg struct {
	Value string
}

type CompletionDialogCloseMsg struct{}

type CompletionDialog interface {
	tea.Model
	layout.Bindings
	SetWidth(width int)
}

type completionDialogCmp struct {
	query                string
	completionProvider   CompletionProvider
	width                int
	height               int
	pseudoSearchTextArea textarea.Model
	listView             utilComponents.SimpleList[CompletionItemI]
}

type completionDialogKeyMap struct {
	Complete key.Binding
	Cancel   key.Binding
}

var completionDialogKeys = completionDialogKeyMap{
	Complete: key.NewBinding(
		key.WithKeys("tab", "enter"),
	),
	Cancel: key.NewBinding(
		key.WithKeys(" ", "esc", "backspace"),
	),
}

func (c *completionDialogCmp) Init() tea.Cmd {
	return nil
}

func (c *completionDialogCmp) complete(item CompletionItemI) tea.Cmd {
	value := c.pseudoSearchTextArea.Value()

	if value == "" {
		return nil
	}

	return tea.Batch(
		util.CmdHandler(CompletionSelectedMsg{
			SearchString:    value,
			CompletionValue: item.GetValue(),
		}),
		c.close(),
	)
}

// refreshCmd returns the provider's background-refresh command when it supports
// asynchronous results, or nil otherwise.
func (c *completionDialogCmp) refreshCmd() tea.Cmd {
	if async, ok := c.completionProvider.(AsyncCompletionProvider); ok {
		return async.RefreshCmd()
	}
	return nil
}

func (c *completionDialogCmp) close() tea.Cmd {
	c.listView.SetItems([]CompletionItemI{})
	c.pseudoSearchTextArea.Reset()
	c.pseudoSearchTextArea.Blur()
	c.query = ""
	if resettable, ok := c.completionProvider.(ResettableCompletionProvider); ok {
		resettable.Reset()
	}

	return util.CmdHandler(CompletionDialogCloseMsg{})
}

func (c *completionDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if c.pseudoSearchTextArea.Focused() {

			if !key.Matches(msg, completionDialogKeys.Complete) {

				var cmd tea.Cmd
				c.pseudoSearchTextArea, cmd = c.pseudoSearchTextArea.Update(msg)
				cmds = append(cmds, cmd)

				var query string
				query = c.pseudoSearchTextArea.Value()
				if query != "" {
					query = query[1:]
				}

				if query != c.query {
					logging.Info("Query", query)
					items, err := c.completionProvider.GetChildEntries(query)
					if err != nil {
						logging.Error("Failed to get child entries", err)
					}

					c.listView.SetItems(items)
					c.query = query
				}

				u, cmd := c.listView.Update(msg)
				c.listView = u.(utilComponents.SimpleList[CompletionItemI])

				cmds = append(cmds, cmd)
			}

			switch {
			case key.Matches(msg, completionDialogKeys.Complete):
				item, i := c.listView.GetSelectedItem()
				if i == -1 {
					return c, nil
				}

				cmd := c.complete(item)

				return c, cmd
			case key.Matches(msg, completionDialogKeys.Cancel):
				// Only close on backspace when there are no characters left
				if msg.String() != "backspace" || len(c.pseudoSearchTextArea.Value()) <= 0 {
					return c, c.close()
				}
			}

			return c, tea.Batch(cmds...)
		} else {
			items, err := c.completionProvider.GetChildEntries("")
			if err != nil {
				logging.Error("Failed to get child entries", err)
			}

			c.listView.SetItems(items)
			c.pseudoSearchTextArea.SetValue(msg.String())
			return c, tea.Batch(c.pseudoSearchTextArea.Focus(), c.refreshCmd())
		}
	case CompletionRefreshMsg:
		// Background results (e.g. a finished filesystem scan) are ready. Only
		// refresh while the dialog is still active; otherwise it has been closed
		// and the update would be discarded anyway.
		if c.pseudoSearchTextArea.Focused() {
			items, err := c.completionProvider.GetChildEntries(c.query)
			if err != nil {
				logging.Error("Failed to get child entries", err)
			}
			c.listView.SetItems(items)
		}
		return c, nil
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
	}

	return c, tea.Batch(cmds...)
}

func (c *completionDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	maxWidth := 40

	completions := c.listView.GetItems()

	for _, cmd := range completions {
		title := cmd.DisplayValue()
		if len(title) > maxWidth-4 {
			maxWidth = len(title) + 4
		}
	}

	c.listView.SetMaxWidth(maxWidth)

	return baseStyle.Padding(0, 0).
		Border(lipgloss.NormalBorder()).
		BorderBottom(false).
		BorderRight(false).
		BorderLeft(false).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(c.width).
		Render(c.listView.View())
}

func (c *completionDialogCmp) SetWidth(width int) {
	c.width = width
}

func (c *completionDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(completionDialogKeys)
}

func NewCompletionDialogCmp(completionProvider CompletionProvider) CompletionDialog {
	ti := textarea.New()

	// Do NOT pre-load entries here: for large directories (e.g. the user's
	// home directory) this would block the TUI startup while scanning the
	// entire filesystem.  Entries are loaded on-demand when the user opens
	// the dialog (handled in Update when the textarea is not yet focused).
	li := utilComponents.NewSimpleList(
		[]CompletionItemI{},
		5,
		"No file matches found",
		false,
	)

	return &completionDialogCmp{
		query:                "",
		completionProvider:   completionProvider,
		pseudoSearchTextArea: ti,
		listView:             li,
	}
}
