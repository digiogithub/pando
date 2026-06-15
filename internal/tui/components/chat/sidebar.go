package chat

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/diff"
	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
)

// ChatInfoSidebar is the embeddable session info column rendered to the right of
// the chat in the Chat workspace tab. It exposes the session title, configured
// LSPs, the current Plan (TodoWrite entries) and the files modified during the
// session. It satisfies layout.Container (tea.Model + Sizeable + Bindings) so it
// can be dropped into a SplitPaneLayout panel.
type ChatInfoSidebar interface {
	tea.Model
	SetSize(width, height int) tea.Cmd
	GetSize() (int, int)
	BindingKeys() []key.Binding
	// SetSession switches the sidebar to a different session, reloading its plan
	// and modified-files state.
	SetSession(s session.Session)
}

type sidebarCmp struct {
	width, height int
	session       session.Session
	history       history.Service
	goal          *GoalState
	modFiles      map[string]struct {
		additions int
		removals  int
	}
	// todos holds the latest plan entries from the TodoWrite tool.
	todos []tools.TodoItem
	// filesCh is the single history subscription used to keep the modified-files
	// list live. It is created once in Init and re-read after each event.
	filesCh <-chan pubsub.Event[history.File]
}

func (m *sidebarCmp) Init() tea.Cmd {
	if m.history == nil {
		return nil
	}
	ctx := context.Background()

	// Initialize the modified files map
	if m.modFiles == nil {
		m.modFiles = make(map[string]struct {
			additions int
			removals  int
		})
	}

	// Load initial files and calculate diffs
	m.loadModifiedFiles(ctx)

	// Subscribe exactly once; subsequent events re-read from this same channel
	// (see waitForFileEvent) instead of re-subscribing, which would leak
	// subscribers because the background context is never cancelled.
	if m.filesCh == nil {
		m.filesCh = m.history.Subscribe(ctx)
	}
	return m.waitForFileEvent()
}

// waitForFileEvent returns a command that blocks on the next history file event
// from the persistent subscription channel and delivers it to Update.
func (m *sidebarCmp) waitForFileEvent() tea.Cmd {
	ch := m.filesCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return ev
	}
}

func (m *sidebarCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SessionSelectedMsg:
		if msg.ID != m.session.ID {
			m.session = msg
			m.todos = tools.GetSessionTodos(msg.ID)
			ctx := context.Background()
			m.loadModifiedFiles(ctx)
		}
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent {
			if m.session.ID == msg.Payload.ID {
				m.session = msg.Payload
			}
		}
	case pubsub.Event[history.File]:
		if msg.Payload.SessionID == m.session.ID {
			m.processFileChanges(context.Background(), msg.Payload)
		}
		// Always keep listening on the same subscription, regardless of whether
		// this event was for the current session, so the loop never stops.
		return m, m.waitForFileEvent()
	case TodosUpdatedMsg:
		if msg.SessionID == m.session.ID {
			m.todos = msg.Todos
		}
	case GoalUpdatedMsg:
		if msg.SessionID == m.session.ID {
			m.goal = msg.Goal
		}
	}
	return m, nil
}

func (m *sidebarCmp) View() string {
	baseStyle := styles.BaseStyle()

	sections := []string{
		header(m.width),
		" ",
		m.sessionSection(),
		" ",
		lspsConfigured(m.width),
	}

	if todosView := m.todosSection(); todosView != "" {
		sections = append(sections, " ", todosView)
	}

	sections = append(sections, " ", m.modifiedFiles())

	return baseStyle.
		Width(m.width).
		PaddingLeft(1).
		PaddingRight(1).
		Height(m.height - 1).
		Render(lipgloss.JoinVertical(lipgloss.Top, sections...))
}

func (m *sidebarCmp) todosSection() string {
	if len(m.todos) == 0 {
		return ""
	}

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()
	innerWidth := m.width - 6 // account for sidebar padding

	title := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Render("Plan")

	var rows []string
	rows = append(rows, title)

	for _, item := range m.todos {
		icon, fg := todoItemStyle(item.Status, t)
		label := ansi.Truncate(item.Content, innerWidth-3, "…")
		row := baseStyle.
			Foreground(fg).
			Render(fmt.Sprintf("%s %s", icon, label))
		rows = append(rows, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func todoItemStyle(status string, t theme.Theme) (icon string, fg lipgloss.TerminalColor) {
	switch strings.ToLower(status) {
	case "completed":
		return "✓", t.Success()
	case "in_progress":
		return "→", t.Warning()
	default:
		return "○", t.TextMuted()
	}
}

func (m *sidebarCmp) sessionSection() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	sessionKey := baseStyle.
		Foreground(t.Primary()).
		Bold(true).
		Render("Session")

	sessionValue := baseStyle.
		Foreground(t.Text()).
		Width(m.width - lipgloss.Width(sessionKey)).
		Render(fmt.Sprintf(": %s", m.session.Title))

	return lipgloss.JoinHorizontal(
		lipgloss.Left,
		sessionKey,
		sessionValue,
	)
}

func (m *sidebarCmp) modifiedFile(filePath string, additions, removals int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	stats := ""
	if additions > 0 && removals > 0 {
		additionsStr := baseStyle.
			Foreground(t.Success()).
			PaddingLeft(1).
			Render(fmt.Sprintf("+%d", additions))

		removalsStr := baseStyle.
			Foreground(t.Error()).
			PaddingLeft(1).
			Render(fmt.Sprintf("-%d", removals))

		content := lipgloss.JoinHorizontal(lipgloss.Left, additionsStr, removalsStr)
		stats = baseStyle.Width(lipgloss.Width(content)).Render(content)
	} else if additions > 0 {
		additionsStr := fmt.Sprintf(" %s", baseStyle.
			PaddingLeft(1).
			Foreground(t.Success()).
			Render(fmt.Sprintf("+%d", additions)))
		stats = baseStyle.Width(lipgloss.Width(additionsStr)).Render(additionsStr)
	} else if removals > 0 {
		removalsStr := fmt.Sprintf(" %s", baseStyle.
			PaddingLeft(1).
			Foreground(t.Error()).
			Render(fmt.Sprintf("-%d", removals)))
		stats = baseStyle.Width(lipgloss.Width(removalsStr)).Render(removalsStr)
	}

	filePathStr := baseStyle.Render(filePath)

	return baseStyle.
		Width(m.width).
		Render(
			lipgloss.JoinHorizontal(
				lipgloss.Left,
				filePathStr,
				stats,
			),
		)
}

func (m *sidebarCmp) modifiedFiles() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	modifiedFiles := baseStyle.
		Width(m.width).
		Foreground(t.Primary()).
		Bold(true).
		Render("Modified Files:")

	// If no modified files, show a placeholder message
	if m.modFiles == nil || len(m.modFiles) == 0 {
		message := "No modified files"
		remainingWidth := m.width - lipgloss.Width(message)
		if remainingWidth > 0 {
			message += strings.Repeat(" ", remainingWidth)
		}
		return baseStyle.
			Width(m.width).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Top,
					modifiedFiles,
					baseStyle.Foreground(t.TextMuted()).Render(message),
				),
			)
	}

	// Sort file paths alphabetically for consistent ordering
	var paths []string
	for path := range m.modFiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// Create views for each file in sorted order
	var fileViews []string
	for _, path := range paths {
		stats := m.modFiles[path]
		fileViews = append(fileViews, m.modifiedFile(path, stats.additions, stats.removals))
	}

	return baseStyle.
		Width(m.width).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Top,
				modifiedFiles,
				lipgloss.JoinVertical(
					lipgloss.Left,
					fileViews...,
				),
			),
		)
}

func (m *sidebarCmp) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	return nil
}

func (m *sidebarCmp) GetSize() (int, int) {
	return m.width, m.height
}

func NewSidebarCmp(session session.Session, history history.Service) tea.Model {
	return &sidebarCmp{
		session: session,
		history: history,
	}
}

// NewChatInfoSidebar builds the embeddable chat info sidebar bound to the given
// session and history service.
func NewChatInfoSidebar(s session.Session, history history.Service) ChatInfoSidebar {
	return &sidebarCmp{
		session: s,
		history: history,
	}
}

// SetSession switches the sidebar to a different session, reloading its plan
// (TodoWrite entries) and modified-files state.
func (m *sidebarCmp) SetSession(s session.Session) {
	if m.session.ID == s.ID {
		m.session = s
		return
	}
	m.session = s
	m.todos = tools.GetSessionTodos(s.ID)
	m.loadModifiedFiles(context.Background())
}

// BindingKeys implements layout.Bindings. The sidebar is purely informational
// and exposes no key bindings of its own.
func (m *sidebarCmp) BindingKeys() []key.Binding {
	return nil
}

func (m *sidebarCmp) loadModifiedFiles(ctx context.Context) {
	if m.history == nil || m.session.ID == "" {
		return
	}

	// Get all latest files for this session
	latestFiles, err := m.history.ListLatestSessionFiles(ctx, m.session.ID)
	if err != nil {
		return
	}

	// Get all files for this session (to find initial versions)
	allFiles, err := m.history.ListBySession(ctx, m.session.ID)
	if err != nil {
		return
	}

	// Clear the existing map to rebuild it
	m.modFiles = make(map[string]struct {
		additions int
		removals  int
	})

	// Process each latest file
	for _, file := range latestFiles {
		// Skip if this is the initial version (no changes to show)
		if file.Version == history.InitialVersion {
			continue
		}

		// Find the initial version for this specific file
		var initialVersion history.File
		for _, v := range allFiles {
			if v.Path == file.Path && v.Version == history.InitialVersion {
				initialVersion = v
				break
			}
		}

		// Skip if we can't find the initial version
		if initialVersion.ID == "" {
			continue
		}
		if initialVersion.Content == file.Content {
			continue
		}

		// Calculate diff between initial and latest version
		_, additions, removals := diff.GenerateDiff(initialVersion.Content, file.Content, file.Path)

		// Only add to modified files if there are changes
		if additions > 0 || removals > 0 {
			// Remove working directory prefix from file path
			displayPath := file.Path
			workingDir := config.WorkingDirectory()
			displayPath = strings.TrimPrefix(displayPath, workingDir)
			displayPath = strings.TrimPrefix(displayPath, "/")

			m.modFiles[displayPath] = struct {
				additions int
				removals  int
			}{
				additions: additions,
				removals:  removals,
			}
		}
	}
}

func (m *sidebarCmp) processFileChanges(ctx context.Context, file history.File) {
	// Skip if this is the initial version (no changes to show)
	if file.Version == history.InitialVersion {
		return
	}

	// Find the initial version for this file
	initialVersion, err := m.findInitialVersion(ctx, file.Path)
	if err != nil || initialVersion.ID == "" {
		return
	}

	// Skip if content hasn't changed
	if initialVersion.Content == file.Content {
		// If this file was previously modified but now matches the initial version,
		// remove it from the modified files list
		displayPath := getDisplayPath(file.Path)
		delete(m.modFiles, displayPath)
		return
	}

	// Calculate diff between initial and latest version
	_, additions, removals := diff.GenerateDiff(initialVersion.Content, file.Content, file.Path)

	// Only add to modified files if there are changes
	if additions > 0 || removals > 0 {
		displayPath := getDisplayPath(file.Path)
		m.modFiles[displayPath] = struct {
			additions int
			removals  int
		}{
			additions: additions,
			removals:  removals,
		}
	} else {
		// If no changes, remove from modified files
		displayPath := getDisplayPath(file.Path)
		delete(m.modFiles, displayPath)
	}
}

// Helper function to find the initial version of a file
func (m *sidebarCmp) findInitialVersion(ctx context.Context, path string) (history.File, error) {
	// Get all versions of this file for the session
	fileVersions, err := m.history.ListBySession(ctx, m.session.ID)
	if err != nil {
		return history.File{}, err
	}

	// Find the initial version
	for _, v := range fileVersions {
		if v.Path == path && v.Version == history.InitialVersion {
			return v, nil
		}
	}

	return history.File{}, fmt.Errorf("initial version not found")
}

// Helper function to get the display path for a file
func getDisplayPath(path string) string {
	workingDir := config.WorkingDirectory()
	displayPath := strings.TrimPrefix(path, workingDir)
	return strings.TrimPrefix(displayPath, "/")
}
