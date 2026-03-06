package settings

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/theme"
)

const sidebarWidth = 25

type SettingsCmp struct {
	sections         []Section
	activeSectionIdx int
	width            int
	height           int
}

func NewSettingsCmp() SettingsCmp {
	return SettingsCmp{}
}

func (m SettingsCmp) Init() tea.Cmd {
	return nil
}

func (m SettingsCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncSectionWidths()
		return m, nil
	case tea.KeyMsg:
		activeSection := m.activeSection()
		if activeSection != nil && activeSection.IsEditing() {
			return m, activeSection.Update(msg)
		}

		switch msg.String() {
		case "tab":
			if len(m.sections) > 0 {
				m.activeSectionIdx = (m.activeSectionIdx + 1) % len(m.sections)
				m.syncSectionWidths()
			}
			return m, nil
		case "shift+tab":
			if len(m.sections) > 0 {
				m.activeSectionIdx = (m.activeSectionIdx - 1 + len(m.sections)) % len(m.sections)
				m.syncSectionWidths()
			}
			return m, nil
		}
	}

	if activeSection := m.activeSection(); activeSection != nil {
		return m, activeSection.Update(msg)
	}

	return m, nil
}

func (m SettingsCmp) View() string {
	t := theme.CurrentTheme()

	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	sidebar := m.renderSidebar()
	content := m.renderContent()

	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Background(t.Background()).
		Foreground(t.Text()).
		Render(lipgloss.JoinHorizontal(lipgloss.Top, sidebar, content))
}

func (m *SettingsCmp) SetSections(sections []Section) {
	m.sections = cloneSections(sections)
	m.activeSectionIdx = min(max(m.activeSectionIdx, 0), max(len(m.sections)-1, 0))
	m.syncSectionWidths()
}

func (m *SettingsCmp) Sections() []Section {
	return cloneSections(m.sections)
}

func (m *SettingsCmp) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.syncSectionWidths()
}

func (m *SettingsCmp) ActiveSection() *Section {
	return m.activeSection()
}

func (m *SettingsCmp) SetActiveField(sectionTitle, fieldKey string) {
	for sectionIdx := range m.sections {
		if m.sections[sectionIdx].Title != sectionTitle {
			continue
		}

		m.activeSectionIdx = sectionIdx
		for fieldIdx := range m.sections[sectionIdx].Fields {
			if m.sections[sectionIdx].Fields[fieldIdx].Key == fieldKey {
				m.sections[sectionIdx].activeFieldIdx = fieldIdx
				m.sections[sectionIdx].editor = nil
				m.syncSectionWidths()
				return
			}
		}

		m.sections[sectionIdx].activeFieldIdx = 0
		m.sections[sectionIdx].editor = nil
		m.syncSectionWidths()
		return
	}

	m.syncSectionWidths()
}

func (m SettingsCmp) renderSidebar() string {
	t := theme.CurrentTheme()
	width := min(sidebarWidth, max(1, m.width))
	items := make([]string, 0, len(m.sections)+1)

	title := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Padding(0, 1).
		Render("Settings")
	items = append(items, title)

	if len(m.sections) == 0 {
		items = append(items, lipgloss.NewStyle().
			Padding(1, 1, 0, 1).
			Foreground(t.TextMuted()).
			Render("No sections"))
	} else {
		for i, section := range m.sections {
			style := lipgloss.NewStyle().
				Width(width-2).
				Padding(0, 1).
				Foreground(t.Text())

			prefix := "  "
			if i == m.activeSectionIdx {
				prefix = "> "
				style = style.Foreground(t.Primary()).Bold(true)
			}

			items = append(items, style.Render(prefix+section.Title))
		}
	}

	return lipgloss.NewStyle().
		Width(width).
		Height(m.height).
		Padding(1, 0).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(t.BorderNormal()).
		Render(lipgloss.JoinVertical(lipgloss.Left, items...))
}

func (m SettingsCmp) renderContent() string {
	t := theme.CurrentTheme()
	contentWidth := max(1, m.width-min(sidebarWidth, max(1, m.width))-1)
	activeSection := m.activeSection()

	title := "Select a section"
	body := lipgloss.NewStyle().
		Foreground(t.TextMuted()).
		Render("No settings available.")
	if activeSection != nil {
		title = activeSection.Title
		body = lipgloss.NewStyle().
			Width(max(1, contentWidth-4)).
			Render(activeSection.View(max(1, contentWidth-4), true))
	}

	header := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Bold(true).
		Render(title)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		body,
	)

	return lipgloss.NewStyle().
		Width(contentWidth).
		Height(m.height).
		Padding(1, 2).
		Render(content)
}

func (m *SettingsCmp) syncSectionWidths() {
	contentWidth := max(1, m.width-min(sidebarWidth, max(1, m.width))-5)
	for i := range m.sections {
		m.sections[i].SetWidth(contentWidth)
	}
}

func (m *SettingsCmp) activeSection() *Section {
	if len(m.sections) == 0 {
		return nil
	}

	m.activeSectionIdx = min(max(m.activeSectionIdx, 0), len(m.sections)-1)
	return &m.sections[m.activeSectionIdx]
}

func cloneSections(sections []Section) []Section {
	cloned := make([]Section, len(sections))
	for i, section := range sections {
		cloned[i] = Section{
			Title:          section.Title,
			Fields:         append([]Field(nil), section.Fields...),
			activeFieldIdx: section.activeFieldIdx,
		}
	}

	return cloned
}

var _ tea.Model = SettingsCmp{}
