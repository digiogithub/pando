package settings

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
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
	viewport         viewport.Model
}

func NewSettingsCmp() SettingsCmp {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	vp.MouseWheelDelta = 3

	return SettingsCmp{viewport: vp}
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
		m.syncViewport()
		m.autoScrollToActiveField()
		return m, nil
	case tea.KeyMsg:
		activeSection := m.activeSection()
		if activeSection != nil && activeSection.IsEditing() {
			cmd := activeSection.Update(msg)
			m.syncViewport()
			m.autoScrollToActiveField()
			return m, cmd
		}

		switch msg.String() {
		case "tab":
			if len(m.sections) > 0 {
				m.activeSectionIdx = (m.activeSectionIdx + 1) % len(m.sections)
				m.syncSectionWidths()
				m.syncViewport()
				m.autoScrollToActiveField()
			}
			return m, nil
		case "shift+tab":
			if len(m.sections) > 0 {
				m.activeSectionIdx = (m.activeSectionIdx - 1 + len(m.sections)) % len(m.sections)
				m.syncSectionWidths()
				m.syncViewport()
				m.autoScrollToActiveField()
			}
			return m, nil
		case "pgdown", "ctrl+f":
			m.viewport.ViewDown()
			return m, nil
		case "pgup", "ctrl+b":
			m.viewport.ViewUp()
			return m, nil
		}
	}

	if mouseMsg, ok := msg.(tea.MouseMsg); ok {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(mouseMsg)

		// Handle left-click for navigation
		if mouseMsg.Action == tea.MouseActionPress && mouseMsg.Button == tea.MouseButtonLeft {
			if mouseMsg.X < sidebarWidth {
				// Sidebar click: account for group header lines
				sectionIdx := m.sidebarYToSectionIdx(mouseMsg.Y)
				if sectionIdx >= 0 && sectionIdx < len(m.sections) {
					m.activeSectionIdx = sectionIdx
					m.syncSectionWidths()
					m.syncViewport()
					m.autoScrollToActiveField()
				}
			} else {
				// Content area click: Padding(1,2) + header(1) + empty(1) = viewport at y=3
				const viewportStartY = 3
				relY := mouseMsg.Y - viewportStartY
				if relY >= 0 && relY < m.viewport.Height {
					contentY := relY + m.viewport.YOffset
					// Each field is 3 lines: top border + content + bottom border
					fieldIdx := contentY / 3
					if activeSection := m.activeSection(); activeSection != nil {
						if fieldIdx >= 0 && fieldIdx < len(activeSection.Fields) {
							activeSection.activeFieldIdx = fieldIdx
							m.syncViewport()
							m.autoScrollToActiveField()
						}
					}
				}
			}
		}

		return m, cmd
	}

	if activeSection := m.activeSection(); activeSection != nil {
		cmd := activeSection.Update(msg)
		m.syncViewport()
		m.autoScrollToActiveField()
		return m, cmd
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
	m.syncViewport()
	m.autoScrollToActiveField()
}

func (m *SettingsCmp) Sections() []Section {
	return cloneSections(m.sections)
}

func (m *SettingsCmp) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.syncSectionWidths()
	m.syncViewport()
	m.autoScrollToActiveField()
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
				m.syncViewport()
				m.autoScrollToActiveField()
				return
			}
		}

		m.sections[sectionIdx].activeFieldIdx = 0
		m.sections[sectionIdx].editor = nil
		m.syncSectionWidths()
		m.syncViewport()
		m.autoScrollToActiveField()
		return
	}

	m.syncSectionWidths()
	m.syncViewport()
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
		lastGroup := ""
		for i, section := range m.sections {
			// Show group header when the group changes
			if section.Group != "" && section.Group != lastGroup {
				groupHeader := lipgloss.NewStyle().
					Width(width - 2).
					Padding(0, 1).
					Foreground(t.TextMuted()).
					Render("─ " + section.Group)
				items = append(items, groupHeader)
				lastGroup = section.Group
			}

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
		body = m.viewport.View()
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

func (m *SettingsCmp) syncViewport() {
	viewportWidth := max(1, m.width-min(sidebarWidth, max(1, m.width))-5)
	viewportHeight := max(1, m.height-4)
	m.viewport.Width = viewportWidth
	m.viewport.Height = viewportHeight

	activeSection := m.activeSection()
	if activeSection == nil {
		m.viewport.SetContent(lipgloss.NewStyle().Foreground(theme.CurrentTheme().TextMuted()).Render("No settings available."))
		m.viewport.SetYOffset(0)
		return
	}

	content := activeSection.View(viewportWidth, true)
	yOffset := m.viewport.YOffset
	maxOffset := max(0, lipgloss.Height(content)-viewportHeight)
	m.viewport.SetContent(content)
	m.viewport.SetYOffset(min(max(yOffset, 0), maxOffset))
}

func (m *SettingsCmp) autoScrollToActiveField() {
	activeSection := m.activeSection()
	if activeSection == nil || m.viewport.Height <= 0 {
		return
	}

	activeField := activeSection.ActiveField()
	if activeField == nil {
		m.viewport.SetYOffset(0)
		return
	}

	content := activeSection.View(max(1, m.viewport.Width), true)
	lines := strings.Split(content, "\n")
	targetLine := 0
	for idx, line := range lines {
		if strings.Contains(line, activeField.Label) {
			targetLine = idx
			break
		}
	}

	yOffset := m.viewport.YOffset
	if targetLine < yOffset {
		yOffset = targetLine
	} else if targetLine >= yOffset+m.viewport.Height {
		yOffset = targetLine - m.viewport.Height + 1
	}
	maxOffset := max(0, len(lines)-m.viewport.Height)
	m.viewport.SetYOffset(min(max(yOffset, 0), maxOffset))
}

func (m *SettingsCmp) activeSection() *Section {
	if len(m.sections) == 0 {
		return nil
	}

	m.activeSectionIdx = min(max(m.activeSectionIdx, 0), len(m.sections)-1)
	return &m.sections[m.activeSectionIdx]
}

// sidebarYToSectionIdx maps a sidebar Y coordinate to a section index,
// accounting for group header lines inserted between sections.
func (m SettingsCmp) sidebarYToSectionIdx(y int) int {
	// y=0: top padding, y=1: "Settings" title
	currentY := 2
	lastGroup := ""
	for i, section := range m.sections {
		if section.Group != "" && section.Group != lastGroup {
			// Group header line
			if y == currentY {
				return -1 // clicked on group header, not a section
			}
			currentY++
			lastGroup = section.Group
		}
		if y == currentY {
			return i
		}
		currentY++
	}
	return -1
}

func cloneSections(sections []Section) []Section {
	cloned := make([]Section, len(sections))
	for i, section := range sections {
		cloned[i] = Section{
			Title:          section.Title,
			Group:          section.Group,
			Fields:         append([]Field(nil), section.Fields...),
			activeFieldIdx: section.activeFieldIdx,
		}
	}

	return cloned
}

var _ tea.Model = SettingsCmp{}
