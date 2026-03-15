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

// ClaudeLoginStartMsg is sent when the OAuth session has been initialized.
// The TUI uses it to show the login dialog and open the browser.
type ClaudeLoginStartMsg struct {
	Session *auth.ClaudeLoginSession
}

// ClaudeLoginDoneMsg is sent after login completes (success or failure).
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

// ClaudeLoginCmd starts the OAuth flow (phase 1): initializes the session and
// returns ClaudeLoginStartMsg so the TUI can show the dialog and open the browser.
func ClaudeLoginCmd() tea.Cmd {
	return func() tea.Msg {
		session, err := auth.ClaudeLoginStart()
		if err != nil {
			return ClaudeLoginDoneMsg{Err: err}
		}
		return ClaudeLoginStartMsg{Session: session}
	}
}

// ClaudeExchangeCodeCmd performs the token exchange (phase 2).
// It is called after the TUI receives the authorization code — either from the
// automatic browser callback (ClaudeAutoCode) or from the manual input dialog.
func ClaudeExchangeCodeCmd(session *auth.ClaudeLoginSession, code, redirectURI string) tea.Cmd {
	return func() tea.Msg {
		creds, displayName, err := auth.ClaudeLoginFinish(session, code, redirectURI)
		if err != nil {
			return ClaudeLoginDoneMsg{Err: err}
		}
		if err := auth.SaveClaudeCredentials(creds); err != nil {
			return ClaudeLoginDoneMsg{Err: err}
		}
		return ClaudeLoginDoneMsg{DisplayName: displayName}
	}
}

// ClaudeWaitAutoCodeCmd waits for the automatic browser callback to deliver the
// authorization code. It blocks until the code arrives or the session times out.
func ClaudeWaitAutoCodeCmd(session *auth.ClaudeLoginSession) tea.Cmd {
	return func() tea.Msg {
		result := <-session.AutoCodeCh
		return result // auth.ClaudeAutoCode — handled in tui.go
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
