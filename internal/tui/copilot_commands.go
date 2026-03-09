package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/tui/util"
	"go.dalton.dog/bubbleup"
)

// copilotLoginDoneMsg is sent when the Copilot login flow completes.
type copilotLoginDoneMsg struct {
	err error
}

func copilotLoginCommand() tea.Cmd {
	return tea.Batch(
		// Show persistent info alert while login is in progress
		util.AlertPersist(bubbleup.InfoKey, "Starting GitHub Copilot login..."),
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()

			deviceCode, err := auth.StartCopilotDeviceFlow(ctx, "")
			if err != nil {
				return util.AlertMsg{
					Type: bubbleup.ErrorKey,
					Msg:  fmt.Sprintf("Copilot login failed: %v", err),
				}
			}

			// Show the device code as a persistent alert (stays until Esc or login completes)
			instructions := fmt.Sprintf(
				"Open: %s\nCode: %s\n\nWaiting for authorization... (Esc to cancel)",
				deviceCode.VerificationURI,
				deviceCode.UserCode,
			)

			// Try to open browser automatically
			if err := auth.OpenBrowser(deviceCode.VerificationURI); err != nil {
				instructions = fmt.Sprintf(
					"Could not open browser automatically.\nOpen: %s\nCode: %s\n\nWaiting for authorization... (Esc to cancel)",
					deviceCode.VerificationURI,
					deviceCode.UserCode,
				)
			}

			// This is a blocking call - we send the persistent alert first via a goroutine trick
			// but since we're in a tea.Cmd, we show the alert via returning a message
			// and then poll in a follow-up command
			return copilotDeviceCodeMsg{
				deviceCode:   deviceCode,
				instructions: instructions,
			}
		},
	)
}

// copilotDeviceCodeMsg carries the device code info for display.
type copilotDeviceCodeMsg struct {
	deviceCode   *auth.CopilotDeviceCode
	instructions string
}

// copilotPollCommand starts polling for login completion.
func copilotPollCommand(deviceCode *auth.CopilotDeviceCode) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		if _, err := auth.CompleteCopilotDeviceFlow(ctx, "", deviceCode); err != nil {
			return copilotLoginDoneMsg{err: err}
		}
		return copilotLoginDoneMsg{err: nil}
	}
}

func copilotLogoutCommand() tea.Cmd {
	return func() tea.Msg {
		if err := auth.DeleteCopilotSession(); err != nil {
			return util.AlertMsg{
				Type: bubbleup.ErrorKey,
				Msg:  fmt.Sprintf("Copilot logout failed: %v", err),
			}
		}

		return util.AlertMsg{
			Type: bubbleup.InfoKey,
			Msg:  "GitHub Copilot session removed.",
		}
	}
}

func copilotStatusCommand() tea.Cmd {
	status := auth.GetCopilotAuthStatus()
	alertType := bubbleup.InfoKey
	if !status.Authenticated {
		alertType = bubbleup.WarnKey
	}
	return util.AlertPersist(alertType, status.Message)
}
