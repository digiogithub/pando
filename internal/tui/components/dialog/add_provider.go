package dialog

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

const (
	addProviderDialogWidth      = 60
	addProviderDialogMaxVisible = 12
	// AddProviderDialogWidth is the exported width for callers that need to centre the overlay.
	AddProviderDialogWidth = addProviderDialogWidth
)

// ProviderAccountCreatedMsg is sent when a new provider account is fully configured.
type ProviderAccountCreatedMsg struct {
	Account config.ProviderAccount
}

// CloseAddProviderDialogMsg is sent when the add-provider dialog is dismissed.
type CloseAddProviderDialogMsg struct{}

type addProviderStep int

const (
	addProviderStepTypeSelect addProviderStep = iota
	addProviderStepForm
)

type providerTypeEntry struct {
	Type            models.ModelProvider
	DisplayName     string
	RequiresAPIKey  bool
	RequiresBaseURL bool
	SupportsOAuth   bool
}

var addProviderTypeList = []providerTypeEntry{
	{models.ProviderAnthropic, "Anthropic", true, false, false},
	{models.ProviderOpenAI, "OpenAI", true, false, false},
	{models.ProviderOpenAICompatible, "OpenAI Compatible (custom)", true, true, false},
	{models.ProviderOllama, "Ollama", false, true, false},
	{models.ProviderGemini, "Google Gemini", true, false, false},
	{models.ProviderGROQ, "Groq", true, false, false},
	{models.ProviderOpenRouter, "OpenRouter", true, false, false},
	{models.ProviderXAI, "xAI (Grok)", true, false, false},
	{models.ProviderAzure, "Azure OpenAI", true, true, false},
	{models.ProviderBedrock, "AWS Bedrock", true, false, false},
	{models.ProviderVertexAI, "Google Vertex AI", false, false, true},
	{models.ProviderCopilot, "GitHub Copilot", false, false, true},
}

// AddProviderDialog is the interface for the add-provider dialog.
type AddProviderDialog interface {
	tea.Model
}

type addProviderDialogCmp struct {
	step   addProviderStep
	width  int
	height int

	// Step 1: type selection
	typeIdx int

	// Step 2: form
	selectedType providerTypeEntry

	// form inputs: 0=displayName, 1=id, 2=apiKey, 3=baseUrl
	inputs       [4]textinput.Model
	activeInput  int
	idAutoSynced bool // true while ID auto-tracks displayName
}

var addProviderSlugRe = regexp.MustCompile(`[^a-z0-9-]`)

func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			b.WriteByte('-')
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func newAddProviderInputs() [4]textinput.Model {
	var inputs [4]textinput.Model
	for i := range inputs {
		inputs[i] = textinput.New()
		inputs[i].Prompt = ""
	}
	inputs[0].Placeholder = "My Anthropic Account"
	inputs[0].Focus()
	inputs[1].Placeholder = "my-anthropic (lowercase, letters, numbers, hyphens)"
	inputs[2].Placeholder = "sk-…"
	inputs[2].EchoMode = textinput.EchoPassword
	inputs[2].EchoCharacter = '•'
	inputs[3].Placeholder = "https://api.example.com/v1"
	return inputs
}

// NewAddProviderDialog creates a new add-provider two-step dialog.
func NewAddProviderDialog(width, height int) AddProviderDialog {
	return &addProviderDialogCmp{
		step:   addProviderStepTypeSelect,
		width:  width,
		height: height,
	}
}

func (d *addProviderDialogCmp) Init() tea.Cmd {
	return nil
}

// visibleInputCount returns how many inputs are relevant for the selected type.
func (d *addProviderDialogCmp) visibleInputCount() int {
	n := 2 // displayName + id always shown
	if d.selectedType.RequiresAPIKey {
		n++
	}
	if d.selectedType.RequiresBaseURL {
		n++
	}
	return n
}

// inputForPosition returns the logical input index given the visual position.
// Positions: 0=displayName, 1=id, 2=apiKey (if applicable), 2or3=baseUrl (if applicable).
func (d *addProviderDialogCmp) inputForPosition(pos int) int {
	switch pos {
	case 0:
		return 0 // displayName
	case 1:
		return 1 // id
	case 2:
		if d.selectedType.RequiresAPIKey {
			return 2 // apiKey
		}
		return 3 // baseUrl (no apiKey, jump straight to baseUrl)
	case 3:
		return 3 // baseUrl
	}
	return pos
}

func (d *addProviderDialogCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch d.step {
	case addProviderStepTypeSelect:
		return d.updateTypeSelect(msg)
	case addProviderStepForm:
		return d.updateForm(msg)
	}
	return d, nil
}

func (d *addProviderDialogCmp) updateTypeSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch keyMsg.String() {
	case "up", "k":
		if d.typeIdx > 0 {
			d.typeIdx--
		}
	case "down", "j":
		if d.typeIdx < len(addProviderTypeList)-1 {
			d.typeIdx++
		}
	case "enter", " ":
		d.selectedType = addProviderTypeList[d.typeIdx]
		d.inputs = newAddProviderInputs()
		d.activeInput = 0
		d.idAutoSynced = true
		d.step = addProviderStepForm
	case "esc":
		return d, func() tea.Msg { return CloseAddProviderDialogMsg{} }
	}
	return d, nil
}

func (d *addProviderDialogCmp) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		// Pass non-key messages to the active input
		var cmd tea.Cmd
		d.inputs[d.inputForPosition(d.activeInput)], cmd = d.inputs[d.inputForPosition(d.activeInput)].Update(msg)
		return d, cmd
	}

	inputIdx := d.inputForPosition(d.activeInput)
	visibleCount := d.visibleInputCount()

	switch keyMsg.String() {
	case "esc":
		// Go back to type selection
		d.step = addProviderStepTypeSelect
		return d, nil

	case "tab", "down":
		d.inputs[inputIdx].Blur()
		d.activeInput = (d.activeInput + 1) % visibleCount
		d.inputs[d.inputForPosition(d.activeInput)].Focus()
		return d, nil

	case "shift+tab", "up":
		d.inputs[inputIdx].Blur()
		d.activeInput = (d.activeInput - 1 + visibleCount) % visibleCount
		d.inputs[d.inputForPosition(d.activeInput)].Focus()
		return d, nil

	case "enter":
		if d.activeInput < visibleCount-1 {
			// Move to next field
			d.inputs[inputIdx].Blur()
			d.activeInput++
			d.inputs[d.inputForPosition(d.activeInput)].Focus()
			return d, nil
		}
		// Last field: save
		return d, d.saveAccount()

	case "ctrl+s":
		return d, d.saveAccount()
	}

	// Update the active input
	var cmd tea.Cmd
	d.inputs[inputIdx], cmd = d.inputs[inputIdx].Update(msg)

	// Auto-sync ID from displayName while the ID hasn't been manually edited
	if inputIdx == 0 && d.idAutoSynced {
		d.inputs[1].SetValue(slugify(d.inputs[0].Value()))
	}
	// Once the user focuses the ID field and types, disable auto-sync
	if inputIdx == 1 {
		d.idAutoSynced = false
	}

	return d, cmd
}

func (d *addProviderDialogCmp) saveAccount() tea.Cmd {
	displayName := strings.TrimSpace(d.inputs[0].Value())
	id := strings.TrimSpace(d.inputs[1].Value())
	apiKey := d.inputs[2].Value()
	baseURL := strings.TrimSpace(d.inputs[3].Value())

	// Validate
	if displayName == "" {
		displayName = d.selectedType.DisplayName
	}
	if id == "" {
		id = slugify(displayName)
	}
	if id == "" {
		id = string(d.selectedType.Type)
	}
	id = addProviderSlugRe.ReplaceAllString(id, "-")

	account := config.ProviderAccount{
		ID:          id,
		DisplayName: displayName,
		Type:        d.selectedType.Type,
		APIKey:      apiKey,
		BaseURL:     baseURL,
		Disabled:    false,
	}

	return func() tea.Msg {
		return ProviderAccountCreatedMsg{Account: account}
	}
}

func (d *addProviderDialogCmp) View() string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()

	switch d.step {
	case addProviderStepTypeSelect:
		return d.viewTypeSelect(t, base)
	case addProviderStepForm:
		return d.viewForm(t, base)
	}
	return ""
}

func (d *addProviderDialogCmp) viewTypeSelect(t theme.Theme, base lipgloss.Style) string {
	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		MarginBottom(1)

	sb.WriteString(titleStyle.Render("Add Provider"))
	sb.WriteByte('\n')
	sb.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted()).Render("Select provider type:"))
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	maxStart := 0
	if len(addProviderTypeList) > addProviderDialogMaxVisible && d.typeIdx >= addProviderDialogMaxVisible/2 {
		maxStart = d.typeIdx - addProviderDialogMaxVisible/2
		if maxStart > len(addProviderTypeList)-addProviderDialogMaxVisible {
			maxStart = len(addProviderTypeList) - addProviderDialogMaxVisible
		}
	}
	endIdx := maxStart + addProviderDialogMaxVisible
	if endIdx > len(addProviderTypeList) {
		endIdx = len(addProviderTypeList)
	}

	for i := maxStart; i < endIdx; i++ {
		entry := addProviderTypeList[i]
		prefix := "  "
		style := lipgloss.NewStyle().
			Width(addProviderDialogWidth - 6).
			Foreground(t.Text())
		if i == d.typeIdx {
			prefix = "> "
			style = style.Foreground(t.Primary()).Bold(true)
		}
		sb.WriteString(style.Render(prefix + entry.DisplayName))
		sb.WriteByte('\n')
	}

	sb.WriteByte('\n')
	helpStyle := lipgloss.NewStyle().Foreground(t.TextMuted())
	sb.WriteString(helpStyle.Render("↑↓/jk navigate  Enter select  Esc cancel"))

	return base.
		Width(addProviderDialogWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary()).
		Padding(1, 2).
		Render(sb.String())
}

func (d *addProviderDialogCmp) viewForm(t theme.Theme, base lipgloss.Style) string {
	var sb strings.Builder

	titleStyle := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		MarginBottom(1)

	sb.WriteString(titleStyle.Render("Configure: " + d.selectedType.DisplayName))
	sb.WriteByte('\n')
	sb.WriteByte('\n')

	labelStyle := lipgloss.NewStyle().
		Width(14).
		Foreground(t.TextMuted())

	inputWidth := addProviderDialogWidth - 22

	type inputDef struct {
		label string
		idx   int
	}
	var inputDefs []inputDef
	inputDefs = append(inputDefs, inputDef{"Display Name", 0})
	inputDefs = append(inputDefs, inputDef{"Account ID  ", 1})
	if d.selectedType.RequiresAPIKey {
		inputDefs = append(inputDefs, inputDef{"API Key     ", 2})
	}
	if d.selectedType.RequiresBaseURL {
		inputDefs = append(inputDefs, inputDef{"Base URL    ", 3})
	}

	for pos, def := range inputDefs {
		inputModel := d.inputs[def.idx]
		inputModel.Width = inputWidth

		selected := pos == d.activeInput
		rowStyle := lipgloss.NewStyle().Foreground(t.Text())
		if selected {
			rowStyle = rowStyle.Foreground(t.Primary())
		}

		row := lipgloss.JoinHorizontal(
			lipgloss.Center,
			labelStyle.Render(def.label+": "),
			inputModel.View(),
		)
		sb.WriteString(rowStyle.Render(row))
		sb.WriteByte('\n')
	}

	if d.selectedType.SupportsOAuth {
		sb.WriteByte('\n')
		sb.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted()).Render("  Note: this provider uses OAuth — no API key required."))
		sb.WriteByte('\n')
	}

	sb.WriteByte('\n')
	helpStyle := lipgloss.NewStyle().Foreground(t.TextMuted())
	sb.WriteString(helpStyle.Render("Tab/↓ next  Shift+Tab/↑ prev  Enter/Ctrl+S save  Esc back"))

	return base.
		Width(addProviderDialogWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary()).
		Padding(1, 2).
		Render(sb.String())
}

// compile-time check
var _ tea.Model = (*addProviderDialogCmp)(nil)

// addProviderDialogKey map (for help display)
type addProviderDialogKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Escape key.Binding
}

var addProviderKeys = addProviderDialogKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "previous"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "next"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select/confirm"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel/back"),
	),
}
