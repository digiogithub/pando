package chat

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/tui/styles"
	"github.com/digiogithub/pando/internal/tui/theme"
	"github.com/digiogithub/pando/internal/version"
)

type SendMsg struct {
	Text        string
	Attachments []message.Attachment
}

// EditorHeightChangedMsg is emitted by the editor when its line count changes.
// Lines is clamped to [1, 10].
type EditorHeightChangedMsg struct {
	Lines int
}

type SessionSelectedMsg = session.Session

type SessionClearedMsg struct{}

// CompactSessionMsg requests a manual compaction of the current session.
type CompactSessionMsg struct{}

type EditorFocusMsg bool

// TodosUpdatedMsg is dispatched when the TodoWrite tool updates the plan.
type TodosUpdatedMsg struct {
	SessionID string
	Todos     []tools.TodoItem
}

// GoalState is the TUI-friendly projection of the current goal/autopilot state.
type GoalState struct {
	Objective      string
	Status         string
	Iteration      int64
	MaxIterations  int64
	StartedAt      int64
	CompletedAt    int64
	HasCompletedAt bool
	Progress       string
	NextStep       string
}

func (g *GoalState) IsRunning() bool {
	return g != nil && strings.EqualFold(g.Status, "running")
}

func (g *GoalState) IsTerminal() bool {
	if g == nil {
		return false
	}

	switch strings.ToLower(g.Status) {
	case "completed", "failed", "cancelled", "blocked", "timeout", "stalled":
		return true
	default:
		return false
	}
}

// GoalUpdatedMsg is dispatched when the current session goal state changes.
type GoalUpdatedMsg struct {
	SessionID string
	Goal      *GoalState
}

const goalPromptHeader = "# Goal Mode Instructions"

func formatUserInput(text string) string {
	if objective, ok := extractGoalObjective(text); ok {
		return "/goal " + objective
	}
	return text
}

func extractGoalObjective(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, goalPromptHeader) {
		return "", false
	}

	startMarker := "\n## Your Objective\n"
	endMarker := "\n## How Goal Mode Works"
	start := strings.Index(trimmed, startMarker)
	end := strings.Index(trimmed, endMarker)
	if start == -1 || end == -1 || end <= start {
		return "", false
	}

	objective := strings.TrimSpace(trimmed[start+len(startMarker) : end])
	if objective == "" {
		return "", false
	}
	return objective, true
}

func header(width int) string {
	return lipgloss.JoinVertical(
		lipgloss.Top,
		logo(width),
		repo(width),
		"",
		cwd(width),
	)
}

func lspsConfigured(width int) string {
	cfg := config.Get()
	title := "LSP Configuration"
	title = ansi.Truncate(title, width, "…")

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	lsps := baseStyle.
		Width(width).
		Foreground(t.Primary()).
		Bold(true).
		Render(title)

	// Get LSP names and sort them for consistent ordering
	var lspNames []string
	for name := range cfg.LSP {
		lspNames = append(lspNames, name)
	}
	sort.Strings(lspNames)

	var lspViews []string
	for _, name := range lspNames {
		lsp := cfg.LSP[name]
		lspName := baseStyle.
			Foreground(t.Text()).
			Render(fmt.Sprintf("• %s", name))

		cmd := lsp.Command
		cmd = ansi.Truncate(cmd, width-lipgloss.Width(lspName)-3, "…")

		lspPath := baseStyle.
			Foreground(t.TextMuted()).
			Render(fmt.Sprintf(" (%s)", cmd))

		lspViews = append(lspViews,
			baseStyle.
				Width(width).
				Render(
					lipgloss.JoinHorizontal(
						lipgloss.Left,
						lspName,
						lspPath,
					),
				),
		)
	}

	return baseStyle.
		Width(width).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Left,
				lsps,
				lipgloss.JoinVertical(
					lipgloss.Left,
					lspViews...,
				),
			),
		)
}

func logo(width int) string {
	logo := fmt.Sprintf("%s %s", styles.PandoIcon, "Pando")
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	versionText := baseStyle.
		Foreground(t.TextMuted()).
		Render(version.Version)

	return baseStyle.
		Bold(true).
		Width(width).
		Render(
			lipgloss.JoinHorizontal(
				lipgloss.Left,
				logo,
				" ",
				versionText,
			),
		)
}

func repo(width int) string {
	repo := "https://github.com/digiogithub/pando"
	t := theme.CurrentTheme()

	return styles.BaseStyle().
		Foreground(t.TextMuted()).
		Width(width).
		Render(repo)
}

func cwd(width int) string {
	cwd := fmt.Sprintf("cwd: %s", config.WorkingDirectory())
	t := theme.CurrentTheme()

	return styles.BaseStyle().
		Foreground(t.TextMuted()).
		Width(width).
		Render(cwd)
}
