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
	numVisibleModels = 8
	maxDialogWidth   = 44
	modelListZoneID  = "model-dialog-list"
)

// ModelSelectedMsg is sent when a model is selected
type ModelSelectedMsg struct {
	Model models.Model
}

type ModelDialogOption struct {
	ID       models.ModelID
	Name     string
	Provider models.ModelProvider
}

// ModelDialogSelectionMsg is sent when a model is selected from a custom model dialog instance.
type ModelDialogSelectionMsg struct {
	Selected ModelDialogOption
}

// CloseModelDialogMsg is sent when a model is selected
type CloseModelDialogMsg struct{}

// ModelDialog interface for the model selection dialog
type ModelDialog interface {
	tea.Model
	layout.Bindings
}

type modelDialogCmp struct {
	models               []models.Model
	filteredModels       []models.Model
	provider             models.ModelProvider
	availableProviders   []models.ModelProvider
	selectedModelID      models.ModelID
	title                string
	onSelect             func(models.Model) tea.Msg
	customProviderModels map[models.ModelProvider][]models.Model

	selectedIdx     int
	width           int
	height          int
	scrollOffset    int
	hScrollOffset   int
	hScrollPossible bool
	queryInput      textinput.Model

	// accountDisplayNames maps a provider-account ID to its human-friendly Display
	// Name, used to label models when several accounts share a provider type.
	accountDisplayNames map[string]string
}

type modelKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Enter  key.Binding
	Escape key.Binding
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
}

func (m *modelDialogCmp) Init() tea.Cmd {
	m.setupModels()
	m.queryInput.Focus()
	return textinput.Blink
}

func (m *modelDialogCmp) emitSelection(model models.Model) tea.Msg {
	if m.onSelect != nil {
		return m.onSelect(model)
	}
	return ModelSelectedMsg{Model: model}
}

// sameTypeAccountCount returns how many distinct provider accounts contribute
// models to the currently displayed provider. When greater than one, model
// labels are prefixed with their account Display Name (via accountLabel) so
// models from a global and a project-level account of the same provider type can
// be told apart.
func (m *modelDialogCmp) sameTypeAccountCount() int {
	seen := make(map[string]struct{})
	for _, model := range m.models {
		if model.AccountID != "" {
			seen[model.AccountID] = struct{}{}
		}
	}
	return len(seen)
}

// accountLabel renders a model's label, prefixing it with the owning account's
// Display Name (falling back to its ID) when more than one account of the same
// provider type is present. This is the dialog-layer equivalent of
// Model.DisplayLabel, but resolves the account ID slug to its Display Name —
// which the low-level models package cannot do without importing config.
func (m *modelDialogCmp) accountLabel(model models.Model, sameTypeCount int) string {
	if model.AccountID == "" || sameTypeCount <= 1 {
		return model.Name
	}
	name := model.AccountID
	if dn := m.accountDisplayNames[model.AccountID]; strings.TrimSpace(dn) != "" {
		name = dn
	}
	return name + ": " + model.Name
}

// loadAccountDisplayNames refreshes the account ID → Display Name map from config.
func (m *modelDialogCmp) loadAccountDisplayNames() {
	names := make(map[string]string)
	for _, acc := range config.GetProviderAccounts() {
		names[acc.ID] = acc.DisplayName
	}
	m.accountDisplayNames = names
}

// dropAmbiguousStaticModels hides the account-less static models (AccountID == "")
// for a provider type once two or more accounts of that type contribute models.
// The static entries always resolve to the first configured account, so leaving
// them visible alongside the per-account ones is what made every selection appear
// to use the "global" account. Per-account copies inherit the static metadata
// (see registry.modelFromFetchedAccountModel), so nothing is lost by hiding them.
func dropAmbiguousStaticModels(in []models.Model) []models.Model {
	seen := make(map[string]struct{})
	for _, model := range in {
		if model.AccountID != "" {
			seen[model.AccountID] = struct{}{}
		}
	}
	if len(seen) < 2 {
		return in
	}
	out := in[:0:0]
	for _, model := range in {
		if model.AccountID == "" {
			continue
		}
		out = append(out, model)
	}
	return out
}

func (m *modelDialogCmp) filterModels() {
	query := strings.TrimSpace(m.queryInput.Value())
	if query == "" {
		m.filteredModels = m.models
		return
	}

	sameTypeCount := m.sameTypeAccountCount()
	names := make([]string, len(m.models))
	for i, model := range m.models {
		names[i] = m.accountLabel(model, sameTypeCount)
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
		// NOTE: the search input is always focused in this dialog, so list
		// navigation is bound to the arrow keys only. Letter keys (incl.
		// h/j/k/l) must reach the query input so model names can be typed.
		case key.Matches(msg, modelKeys.Up):
			m.moveSelectionUp()
		case key.Matches(msg, modelKeys.Down):
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
			return m, util.CmdHandler(m.emitSelection(m.filteredModels[m.selectedIdx]))
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
			if !tuizone.InBounds(tuizone.ModelListID(modelListZoneID), msg) {
				break
			}
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
				return m, util.CmdHandler(m.emitSelection(m.filteredModels[m.selectedIdx]))
			}
		case tea.MouseButtonWheelUp:
			if !tuizone.InBounds(tuizone.ModelListID(modelListZoneID), msg) {
				break
			}
			m.moveSelectionUp()
			return m, nil
		case tea.MouseButtonWheelDown:
			if !tuizone.InBounds(tuizone.ModelListID(modelListZoneID), msg) {
				break
			}
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

	titleText := m.title
	if strings.TrimSpace(titleText) == "" {
		providerName := strings.ToUpper(string(m.provider)[:1]) + string(m.provider[1:])
		titleText = fmt.Sprintf("Select %s Model", providerName)
	}
	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Width(maxDialogWidth).
		Padding(0, 0, 1).
		Render(titleText)

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
		sameTypeCount := m.sameTypeAccountCount()
		for i := m.scrollOffset; i < endIdx; i++ {
			mod := m.filteredModels[i]
			itemStyle := baseStyle.Width(maxDialogWidth)
			if i == m.selectedIdx {
				itemStyle = itemStyle.Background(t.Primary()).
					Foreground(t.BadgeText()).Bold(true)
			}
			label := m.accountLabel(mod, sameTypeCount)
			if mod.CanReason {
				label += " ⚡"
			}
			modelItems = append(modelItems, tuizone.MarkModelItem(string(mod.ID), itemStyle.Render(label)))
		}
	}

	scrollIndicator := m.getScrollIndicators(maxDialogWidth)

	modelList := baseStyle.Width(maxDialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, modelItems...))
	if len(m.filteredModels) > 0 {
		modelList = tuizone.MarkModelList(modelListZoneID, modelList)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		searchInput,
		baseStyle.Width(maxDialogWidth).Render(""),
		modelList,
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
	m.loadAccountDisplayNames()
	if len(m.customProviderModels) > 0 {
		m.availableProviders = make([]models.ModelProvider, 0, len(m.customProviderModels))
		for provider := range m.customProviderModels {
			m.availableProviders = append(m.availableProviders, provider)
		}
		slices.SortFunc(m.availableProviders, func(a, b models.ModelProvider) int {
			return strings.Compare(string(a), string(b))
		})
		m.hScrollPossible = len(m.availableProviders) > 1
		if len(m.availableProviders) == 0 {
			m.provider = ""
			m.models = nil
			m.filteredModels = nil
			return
		}
		m.provider = models.SupportedModels[m.selectedModelID].Provider
		if m.provider == "" || findProviderIndex(m.availableProviders, m.provider) == -1 {
			m.provider = m.availableProviders[0]
		}
		m.hScrollOffset = findProviderIndex(m.availableProviders, m.provider)
		m.setupModelsForProvider(m.provider)
		return
	}

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
	m.selectedModelID = modelInfo.ID

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
	m.provider = provider
	if len(m.customProviderModels) > 0 {
		m.models = append([]models.Model(nil), m.customProviderModels[provider]...)
	} else {
		m.models = dropAmbiguousStaticModels(getModelsForProvider(provider))
	}
	m.selectedIdx = 0
	m.scrollOffset = 0
	m.queryInput.SetValue("")
	m.filterModels()

	selectedModelID := m.selectedModelID
	if selectedModelID == "" {
		cfg := config.Get()
		agentCfg := cfg.Agents[config.AgentCoder]
		selectedModelID = agentCfg.Model
	}

	// Try to select the current model if it belongs to this provider
	if provider == models.SupportedModels[selectedModelID].Provider {
		for i, model := range m.filteredModels {
			if model.ID == selectedModelID {
				m.selectedIdx = i
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
	return NewCustomModelDialogCmp("", "", nil, nil)
}

func NewCustomModelDialogCmp(title string, selectedModelID models.ModelID, modelOptions []ModelDialogOption, onSelect func(models.Model) tea.Msg) ModelDialog {
	input := textinput.New()
	input.Placeholder = "Search models..."
	input.Prompt = "> "
	input.CharLimit = 128

	cmp := &modelDialogCmp{
		queryInput:           input,
		title:                title,
		selectedModelID:      selectedModelID,
		onSelect:             onSelect,
		customProviderModels: make(map[models.ModelProvider][]models.Model),
	}
	for _, option := range modelOptions {
		provider := option.Provider
		if provider == "" {
			provider = models.SupportedModels[option.ID].Provider
		}
		cmp.customProviderModels[provider] = append(cmp.customProviderModels[provider], models.Model{
			ID:       option.ID,
			Name:     option.Name,
			Provider: provider,
		})
	}
	for provider, providerModels := range cmp.customProviderModels {
		slices.SortFunc(providerModels, func(a, b models.Model) int {
			if a.Name > b.Name {
				return -1
			} else if a.Name < b.Name {
				return 1
			}
			return 0
		})
		cmp.customProviderModels[provider] = providerModels
	}
	return cmp
}
