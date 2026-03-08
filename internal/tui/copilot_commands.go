package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/auth"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/tui/util"
)

const copilotAuthStatusTTL = 20 * time.Second

func copilotLoginCommand() tea.Cmd {
	return tea.Batch(
		util.ReportInfo("Starting GitHub Copilot login…"),
		func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			defer cancel()

			deviceCode, err := auth.StartCopilotDeviceFlow(ctx, "")
			if err != nil {
				return util.InfoMsg{
					Type: util.InfoTypeError,
					Msg:  fmt.Sprintf("Copilot login failed: %v", err),
					TTL:  copilotAuthStatusTTL,
				}
			}

			instructions := auth.CopilotDeviceFlowInstructions(*deviceCode)
			if err := auth.OpenBrowser(deviceCode.VerificationURI); err != nil {
				logging.WarnPersist(
					"Could not open the browser automatically for GitHub Copilot login. "+instructions,
					logging.PersistTimeArg,
					45*time.Second,
				)
			} else {
				logging.InfoPersist(
					"GitHub Copilot device login started. "+instructions,
					logging.PersistTimeArg,
					45*time.Second,
				)
			}

			if _, err := auth.CompleteCopilotDeviceFlow(ctx, "", deviceCode); err != nil {
				return util.InfoMsg{
					Type: util.InfoTypeError,
					Msg:  fmt.Sprintf("Copilot login failed: %v", err),
					TTL:  copilotAuthStatusTTL,
				}
			}

			logging.InfoPersist(auth.GetCopilotAuthStatus().Message, logging.PersistTimeArg, 20*time.Second)
			return util.InfoMsg{
				Type: util.InfoTypeInfo,
				Msg:  "GitHub Copilot login saved.",
				TTL:  copilotAuthStatusTTL,
			}
		},
	)
}

func copilotLogoutCommand() tea.Cmd {
	return func() tea.Msg {
		if err := auth.DeleteCopilotSession(); err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  fmt.Sprintf("Copilot logout failed: %v", err),
				TTL:  copilotAuthStatusTTL,
			}
		}

		logging.InfoPersist("GitHub Copilot session removed.", logging.PersistTimeArg, 15*time.Second)
		return util.InfoMsg{
			Type: util.InfoTypeInfo,
			Msg:  "GitHub Copilot session removed.",
			TTL:  copilotAuthStatusTTL,
		}
	}
}

func copilotStatusCommand() tea.Cmd {
	return util.ReportInfo(auth.GetCopilotAuthStatus().Message)
}
