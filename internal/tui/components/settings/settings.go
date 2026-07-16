package settings

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digiogithub/pando/internal/tui/styles"
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
			m.syncActiveFieldToScroll()
			return m, nil
		case "pgup", "ctrl+b":
			m.viewport.ViewUp()
			m.syncActiveFieldToScroll()
			return m, nil
		}
	}

	if mouseMsg, ok := msg.(tea.MouseMsg); ok {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(mouseMsg)
		m.syncActiveFieldToScroll()

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
					// Field heights vary (fields with a Hint are taller), so map
					// the clicked line through the section's real geometry.
					if activeSection := m.activeSection(); activeSection != nil {
						fieldIdx := activeSection.FieldAtLine(max(1, m.viewport.Width), contentY)
						if fieldIdx >= 0 {
							activeSection.SetActiveFieldIdx(fieldIdx)
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

	containerStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Foreground(t.Text())
	if t.HasBackground() {
		containerStyle = containerStyle.Background(t.Background())
	}
	return containerStyle.Render(lipgloss.JoinHorizontal(lipgloss.Top, sidebar, content))
}

// SetSections replaces the sections, preserving the active section, the active
// field of every section and the scroll position across the rebuild.
//
// Callers pass freshly built sections whose activeFieldIdx is always 0, so
// without this remapping any rebuild not followed by an explicit
// SetActiveField would snap focus and the viewport back to the top. That is
// what happened after saving a field: persisting writes the config file, the
// config watcher picks the write up and publishes a change event, and the
// resulting rebuild reset focus to the first field of the section.
func (m *SettingsCmp) SetSections(sections []Section) {
	activeTitle := ""
	if active := m.activeSection(); active != nil {
		activeTitle = active.Title
	}

	activeKeys := make(map[string]string, len(m.sections))
	for i := range m.sections {
		if field := m.sections[i].ActiveField(); field != nil {
			activeKeys[m.sections[i].Title] = field.Key
		}
	}

	m.sections = cloneSections(sections)
	m.activeSectionIdx = min(max(m.activeSectionIdx, 0), max(len(m.sections)-1, 0))
	for i := range m.sections {
		if activeTitle != "" && m.sections[i].Title == activeTitle {
			m.activeSectionIdx = i
		}
		key, ok := activeKeys[m.sections[i].Title]
		if !ok {
			continue
		}
		for fieldIdx := range m.sections[i].Fields {
			if m.sections[i].Fields[fieldIdx].Key == key {
				m.sections[i].activeFieldIdx = fieldIdx
				break
			}
		}
	}

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
	base := styles.BaseStyle()
	width := min(sidebarWidth, max(1, m.width))
	items := make([]string, 0, len(m.sections)+1)

	title := base.
		Foreground(t.Primary()).
		Bold(true).
		Padding(0, 1).
		Render("Settings")
	items = append(items, title)

	if len(m.sections) == 0 {
		items = append(items, base.
			Width(width-2).
			Padding(1, 1, 0, 1).
			Foreground(t.TextMuted()).
			Render("No sections"))
	} else {
		lastGroup := ""
		for i, section := range m.sections {
			if section.Group != "" && section.Group != lastGroup {
				groupHeader := base.
					Width(width-2).
					Padding(0, 1).
					Foreground(t.TextMuted()).
					Render("─ " + section.Group)
				items = append(items, groupHeader)
				lastGroup = section.Group
			}

			style := base.
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

	return base.
		Width(width).
		Height(m.height).
		Padding(1, 0).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(t.BorderNormal()).
		Render(lipgloss.JoinVertical(lipgloss.Left, items...))
}

func (m SettingsCmp) renderContent() string {
	t := theme.CurrentTheme()
	base := styles.BaseStyle()
	contentWidth := max(1, m.width-min(sidebarWidth, max(1, m.width))-1)
	activeSection := m.activeSection()

	title := "Select a section"
	body := base.
		Foreground(t.TextMuted()).
		Render("No settings available.")
	if activeSection != nil {
		title = activeSection.Title
		body = m.viewport.View()
	}

	header := base.
		Foreground(t.Primary()).
		Bold(true).
		Render(title)

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		"",
		body,
	)

	return base.
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

	if len(activeSection.Fields) == 0 {
		m.viewport.SetYOffset(0)
		return
	}

	width := max(1, m.viewport.Width)
	heights := activeSection.FieldHeights(width)
	idx := activeSection.ActiveFieldIdx()
	if idx < 0 || idx >= len(heights) {
		m.viewport.SetYOffset(0)
		return
	}

	targetLine := 0
	for i := 0; i < idx; i++ {
		targetLine += heights[i]
	}
	fieldHeight := heights[idx]
	totalLines := 0
	for _, h := range heights {
		totalLines += h
	}

	// Only move the viewport when the active field is not fully visible, so a
	// selection change scrolls the minimum needed instead of jumping to the top.
	yOffset := m.viewport.YOffset
	if targetLine < yOffset {
		yOffset = targetLine
	} else if targetLine+fieldHeight > yOffset+m.viewport.Height {
		yOffset = targetLine + fieldHeight - m.viewport.Height
	}
	maxOffset := max(0, totalLines-m.viewport.Height)
	m.viewport.SetYOffset(min(max(yOffset, 0), maxOffset))
}

// syncActiveFieldToScroll keeps the active field in sync with manual scrolling
// (mouse wheel, pgup/pgdown) that moves the viewport without going through
// up/down field navigation. Without this, the active field stays wherever it
// was left, and the next edit triggers autoScrollToActiveField, which snaps
// the view back to that stale (often off-screen) position instead of staying
// where the user scrolled to.
func (m *SettingsCmp) syncActiveFieldToScroll() {
	activeSection := m.activeSection()
	if activeSection == nil || len(activeSection.Fields) == 0 || m.viewport.Height <= 0 {
		return
	}

	width := max(1, m.viewport.Width)
	heights := activeSection.FieldHeights(width)
	idx := activeSection.ActiveFieldIdx()
	if idx < 0 || idx >= len(heights) {
		return
	}

	start := 0
	for i := 0; i < idx; i++ {
		start += heights[i]
	}
	end := start + heights[idx]
	top := m.viewport.YOffset
	bottom := top + m.viewport.Height
	if end > top && start < bottom {
		// Active field is still (at least partially) visible; leave it alone.
		return
	}

	newIdx := activeSection.FieldAtLine(width, top)
	if newIdx < 0 {
		newIdx = len(activeSection.Fields) - 1
	}
	activeSection.SetActiveFieldIdx(newIdx)
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
