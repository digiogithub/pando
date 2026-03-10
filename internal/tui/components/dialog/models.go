package dialog

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/tui/layout"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/tui/util"
	tuizone "github.com/digiogithub/pando/internal/tui/zone"
	"github.com/sahilm/fuzzy"
)

const (
	numVisibleModels = 10
	maxDialogWidth   = 44
)

// ModelSelectedMsg is sent when a model is selected
type ModelSelectedMsg struct {
	Model models.Model
}

// CloseModelDialogMsg is sent when a model is selected
type CloseModelDialogMsg struct{}

// ModelDialog interface for the model selection dialog
type ModelDialog interface {
	tea.Model
	layout.Bindings
}

type modelDialogCmp struct {
	models             []models.Model
	filteredModels     []models.Model
	provider           models.ModelProvider
	availableProviders []models.ModelProvider

	selectedIdx     int
	width           int
	height          int
	scrollOffset    int
	hScrollOffset   int
	hScrollPossible bool
	queryInput      textinput.Model
}

type modelKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Enter  key.Binding
	Escape key.Binding
	J      key.Binding
	K      key.Binding
	H      key.Binding
	L      key.Binding
}

var modelKeys = modelKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up"),
		key.WithHelp("↑", "previous model"),
	),
	Down: key.NewBinding(
		key.WithKeys("down"),
		key.WithHelp("↓", "next model"),
	),
	Left: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←", "scroll left"),
	),
	Right: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("→", "scroll right"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select model"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	),
	J: key.NewBinding(
		key.WithKeys("j"),
		key.WithHelp("j", "next model"),
	),
	K: key.NewBinding(
		key.WithKeys("k"),
		key.WithHelp("k", "previous model"),
	),
	H: key.NewBinding(
		key.WithKeys("h"),
		key.WithHelp("h", "scroll left"),
	),
	L: key.NewBinding(
		key.WithKeys("l"),
		key.WithHelp("l", "scroll right"),
	),
}

func (m *modelDialogCmp) Init() tea.Cmd {
	m.setupModels()
	m.queryInput.Focus()
	return textinput.Blink
}

func (m *modelDialogCmp) filterModels() {
	query := strings.TrimSpace(m.queryInput.Value())
	if query == "" {
		m.filteredModels = m.models
		return
	}

	names := make([]string, len(m.models))
	for i, model := range m.models {
		names[i] = model.Name
	}

	matches := fuzzy.Find(strings.ToLower(query), lowerStrings(names))
	filtered := make([]models.Model, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, m.models[match.Index])
	}

	m.filteredModels = filtered
	if m.selectedIdx >= len(m.filteredModels) {
		m.selectedIdx = 0
		m.scrollOffset = 0
	}
}

func (m *modelDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, modelKeys.Up) || key.Matches(msg, modelKeys.K):
			m.moveSelectionUp()
		case key.Matches(msg, modelKeys.Down) || key.Matches(msg, modelKeys.J):
			m.moveSelectionDown()
		case key.Matches(msg, modelKeys.Left):
			if m.hScrollPossible {
				m.switchProvider(-1)
			}
		case key.Matches(msg, modelKeys.Right):
			if m.hScrollPossible {
				m.switchProvider(1)
			}
		case key.Matches(msg, modelKeys.Enter):
			if len(m.filteredModels) == 0 {
				return m, nil
			}
			util.ReportInfo(fmt.Sprintf("selected model: %s", m.filteredModels[m.selectedIdx].Name))
			return m, util.CmdHandler(ModelSelectedMsg{Model: m.filteredModels[m.selectedIdx]})
		case key.Matches(msg, modelKeys.Escape):
			return m, util.CmdHandler(CloseModelDialogMsg{})
		default:
			var cmd tea.Cmd
			m.queryInput, cmd = m.queryInput.Update(msg)
			m.filterModels()
			return m, cmd
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress {
			break
		}
		switch msg.Button {
		case tea.MouseButtonLeft:
			for i, model := range m.filteredModels {
				if !tuizone.InBounds(tuizone.ModelItemID(string(model.ID)), msg) {
					continue
				}
				m.selectedIdx = i
				if m.selectedIdx < m.scrollOffset {
					m.scrollOffset = m.selectedIdx
				}
				if m.selectedIdx >= m.scrollOffset+numVisibleModels {
					m.scrollOffset = m.selectedIdx - (numVisibleModels - 1)
				}
				return m, util.CmdHandler(ModelSelectedMsg{Model: m.filteredModels[m.selectedIdx]})
			}
		case tea.MouseButtonWheelUp:
			m.moveSelectionUp()
			return m, nil
		case tea.MouseButtonWheelDown:
			m.moveSelectionDown()
			return m, nil
		}
	}

	return m, nil
}

// moveSelectionUp moves the selection up or wraps to bottom
func (m *modelDialogCmp) moveSelectionUp() {
	if len(m.filteredModels) == 0 {
		return
	}
	if m.selectedIdx > 0 {
		m.selectedIdx--
	} else {
		m.selectedIdx = len(m.filteredModels) - 1
		m.scrollOffset = max(0, len(m.filteredModels)-numVisibleModels)
	}

	// Keep selection visible
	if m.selectedIdx < m.scrollOffset {
		m.scrollOffset = m.selectedIdx
	}
}

// moveSelectionDown moves the selection down or wraps to top
func (m *modelDialogCmp) moveSelectionDown() {
	if len(m.filteredModels) == 0 {
		return
	}
	if m.selectedIdx < len(m.filteredModels)-1 {
		m.selectedIdx++
	} else {
		m.selectedIdx = 0
		m.scrollOffset = 0
	}

	// Keep selection visible
	if m.selectedIdx >= m.scrollOffset+numVisibleModels {
		m.scrollOffset = m.selectedIdx - (numVisibleModels - 1)
	}
}

func (m *modelDialogCmp) switchProvider(offset int) {
	if len(m.availableProviders) == 0 {
		return
	}

	newOffset := m.hScrollOffset + offset

	// Ensure we stay within bounds
	if newOffset < 0 {
		newOffset = len(m.availableProviders) - 1
	}
	if newOffset >= len(m.availableProviders) {
		newOffset = 0
	}

	m.hScrollOffset = newOffset
	m.provider = m.availableProviders[m.hScrollOffset]
	m.setupModelsForProvider(m.provider)
}

func (m *modelDialogCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	// Handle case where no providers are available
	if len(m.availableProviders) == 0 || m.provider == "" {
		noProviderMsg := baseStyle.
			Foreground(t.Primary()).
			Bold(true).
			Width(maxDialogWidth).
			Padding(1, 2).
			Render("No Model Providers Available")

		helpLines := []string{
			baseStyle.Foreground(t.TextMuted()).Width(maxDialogWidth).Padding(0, 2).Render("Configure a provider to get started:"),
			"",
			baseStyle.Foreground(t.Text()).Width(maxDialogWidth).Padding(0, 2).Render("• GitHub Copilot: /login"),
			baseStyle.Foreground(t.Text()).Width(maxDialogWidth).Padding(0, 2).Render("• Ollama: install & run ollama"),
			baseStyle.Foreground(t.Text()).Width(maxDialogWidth).Padding(0, 2).Render("• OpenAI: set OPENAI_API_KEY"),
			baseStyle.Foreground(t.Text()).Width(maxDialogWidth).Padding(0, 2).Render("• Anthropic: set ANTHROPIC_API_KEY"),
			"",
			baseStyle.Foreground(t.TextMuted()).Width(maxDialogWidth).Padding(0, 2).Render("Press Esc to close"),
		}

		content := lipgloss.JoinVertical(lipgloss.Left,
			noProviderMsg,
			lipgloss.JoinVertical(lipgloss.Left, helpLines...),
		)

		return baseStyle.Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderBackground(t.Background()).
			BorderForeground(t.TextMuted()).
			Width(lipgloss.Width(content) + 4).
			Render(content)
	}

	// Capitalize first letter of provider name
	providerName := strings.ToUpper(string(m.provider)[:1]) + string(m.provider[1:])
	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(maxDialogWidth).
		Padding(0, 0, 1).
		Render(fmt.Sprintf("Select %s Model", providerName))

	// Search input
	queryStyle := baseStyle.
		Width(maxDialogWidth).
		Padding(0, 1).
		Border(lipgloss.NormalBorder()).
		BorderForeground(t.TextMuted())
	m.queryInput.Width = max(0, maxDialogWidth-4)
	searchInput := queryStyle.Render(m.queryInput.View())

	// Render visible models
	endIdx := min(m.scrollOffset+numVisibleModels, len(m.filteredModels))
	modelItems := make([]string, 0, endIdx-m.scrollOffset)

	if len(m.filteredModels) == 0 {
		modelItems = append(modelItems, baseStyle.Width(maxDialogWidth).Padding(0, 1).Foreground(t.TextMuted()).Render("No models found"))
	} else {
		for i := m.scrollOffset; i < endIdx; i++ {
			itemStyle := baseStyle.Width(maxDialogWidth)
			if i == m.selectedIdx {
				itemStyle = itemStyle.Background(t.Primary()).
					Foreground(t.Background()).Bold(true)
			}
			modelItems = append(modelItems, tuizone.MarkModelItem(string(m.filteredModels[i].ID), itemStyle.Render(m.filteredModels[i].Name)))
		}
	}

	scrollIndicator := m.getScrollIndicators(maxDialogWidth)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		searchInput,
		baseStyle.Width(maxDialogWidth).Render(""),
		baseStyle.Width(maxDialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, modelItems...)),
		scrollIndicator,
	)

	return baseStyle.Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderBackground(t.Background()).
		BorderForeground(t.TextMuted()).
		Width(lipgloss.Width(content) + 4).
		Render(content)
}

func (m *modelDialogCmp) getScrollIndicators(maxWidth int) string {
	var indicator string

	if len(m.filteredModels) > numVisibleModels {
		if m.scrollOffset > 0 {
			indicator += "↑ "
		}
		if m.scrollOffset+numVisibleModels < len(m.filteredModels) {
			indicator += "↓ "
		}
	}

	if m.hScrollPossible {
		if m.hScrollOffset > 0 {
			indicator = "← " + indicator
		}
		if m.hScrollOffset < len(m.availableProviders)-1 {
			indicator += "→"
		}
	}

	if indicator == "" {
		return ""
	}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	return baseStyle.
		Foreground(t.Primary()).
		Width(maxWidth).
		Align(lipgloss.Right).
		Bold(true).
		Render(indicator)
}

func (m *modelDialogCmp) BindingKeys() []key.Binding {
	return layout.KeyMapToSlice(modelKeys)
}

func (m *modelDialogCmp) setupModels() {
	cfg := config.Get()
	modelInfo := GetSelectedModel(cfg)
	m.availableProviders = getEnabledProviders(cfg)
	m.hScrollPossible = len(m.availableProviders) > 1

	// If no providers are available, leave provider empty and models empty
	if len(m.availableProviders) == 0 {
		m.provider = ""
		m.models = nil
		m.filteredModels = nil
		return
	}

	m.provider = modelInfo.Provider

	// If the selected model's provider is empty or not in the available list,
	// fall back to the first available provider
	if m.provider == "" || findProviderIndex(m.availableProviders, m.provider) == -1 {
		m.provider = m.availableProviders[0]
	}

	m.hScrollOffset = findProviderIndex(m.availableProviders, m.provider)

	m.setupModelsForProvider(m.provider)
}

func GetSelectedModel(cfg *config.Config) models.Model {

	agentCfg := cfg.Agents[config.AgentCoder]
	selectedModelId := agentCfg.Model
	return models.SupportedModels[selectedModelId]
}

func getEnabledProviders(cfg *config.Config) []models.ModelProvider {
	var providers []models.ModelProvider
	for providerId, provider := range cfg.Providers {
		if !provider.Disabled {
			providers = append(providers, providerId)
		}
	}

	// Sort by provider popularity
	slices.SortFunc(providers, func(a, b models.ModelProvider) int {
		rA := models.ProviderPopularity[a]
		rB := models.ProviderPopularity[b]

		// models not included in popularity ranking default to last
		if rA == 0 {
			rA = 999
		}
		if rB == 0 {
			rB = 999
		}
		return rA - rB
	})
	return providers
}

// findProviderIndex returns the index of the provider in the list, or -1 if not found
func findProviderIndex(providers []models.ModelProvider, provider models.ModelProvider) int {
	for i, p := range providers {
		if p == provider {
			return i
		}
	}
	return -1
}

func (m *modelDialogCmp) setupModelsForProvider(provider models.ModelProvider) {
	cfg := config.Get()
	agentCfg := cfg.Agents[config.AgentCoder]
	selectedModelId := agentCfg.Model

	m.provider = provider
	m.models = getModelsForProvider(provider)
	m.selectedIdx = 0
	m.scrollOffset = 0
	m.queryInput.SetValue("")
	m.filterModels()

	// Try to select the current model if it belongs to this provider
	if provider == models.SupportedModels[selectedModelId].Provider {
		for i, model := range m.filteredModels {
			if model.ID == selectedModelId {
				m.selectedIdx = i
				// Adjust scroll position to keep selected model visible
				if m.selectedIdx >= numVisibleModels {
					m.scrollOffset = m.selectedIdx - (numVisibleModels - 1)
				}
				break
			}
		}
	}
}

func getModelsForProvider(provider models.ModelProvider) []models.Model {
	var providerModels []models.Model
	for _, model := range models.SupportedModels {
		if model.Provider == provider {
			providerModels = append(providerModels, model)
		}
	}

	// reverse alphabetical order (if llm naming was consistent latest would appear first)
	slices.SortFunc(providerModels, func(a, b models.Model) int {
		if a.Name > b.Name {
			return -1
		} else if a.Name < b.Name {
			return 1
		}
		return 0
	})

	return providerModels
}

func NewModelDialogCmp() ModelDialog {
	input := textinput.New()
	input.Placeholder = "Search models..."
	input.Prompt = "> "
	input.CharLimit = 128

	return &modelDialogCmp{
		queryInput: input,
	}
}
