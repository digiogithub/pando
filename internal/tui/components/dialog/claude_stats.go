package dialog

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/stats"
)

// ClaudeStatsMsg is sent when stats should be shown.
type ClaudeStatsMsg struct {
	Content string // pre-formatted stats text
}

// ClaudeAuthStatusMsg is sent to show auth status.
type ClaudeAuthStatusMsg struct {
	Status *auth.ClaudeAuthStatus
	Err    error
}

// ClaudeLoginStartMsg triggers the OAuth login flow.
type ClaudeLoginStartMsg struct{}

// ClaudeLoginDoneMsg is sent after login completes.
type ClaudeLoginDoneMsg struct {
	DisplayName string
	Err         error
}

// ClaudeLogoutDoneMsg is sent after logout completes.
type ClaudeLogoutDoneMsg struct {
	Err error
}

// LoadClaudeStatsCmd loads stats and sends ClaudeStatsMsg.
func LoadClaudeStatsCmd() tea.Cmd {
	return func() tea.Msg {
		cache, err := stats.LoadBestAvailableStats()
		if err != nil || cache == nil {
			return ClaudeStatsMsg{Content: "No usage statistics available.\n\nUse Claude Code or Pando with a Claude account to track usage."}
		}
		return ClaudeStatsMsg{Content: stats.FormatStats(cache)}
	}
}

// ClaudeLoginCmd performs OAuth login in background.
func ClaudeLoginCmd() tea.Cmd {
	return func() tea.Msg {
		creds, displayName, err := auth.ClaudeLogin()
		if err != nil {
			return ClaudeLoginDoneMsg{Err: err}
		}
		if err := auth.SaveClaudeCredentials(creds); err != nil {
			return ClaudeLoginDoneMsg{Err: err}
		}
		return ClaudeLoginDoneMsg{DisplayName: displayName}
	}
}

// ClaudeLogoutCmd performs logout.
func ClaudeLogoutCmd() tea.Cmd {
	return func() tea.Msg {
		err := auth.ClaudeLogout()
		return ClaudeLogoutDoneMsg{Err: err}
	}
}

// GetClaudeAuthStatusCmd fetches current auth status.
func GetClaudeAuthStatusCmd() tea.Cmd {
	return func() tea.Msg {
		status, err := auth.GetClaudeAuthStatus()
		return ClaudeAuthStatusMsg{Status: status, Err: err}
	}
}
