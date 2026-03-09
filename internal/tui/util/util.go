package util

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func CmdHandler(msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return msg
	}
}

func ReportError(err error) tea.Cmd {
	return CmdHandler(InfoMsg{
		Type: InfoTypeError,
		Msg:  err.Error(),
	})
}

type InfoType int

const (
	InfoTypeInfo InfoType = iota
	InfoTypeWarn
	InfoTypeError
)

func ReportInfo(info string) tea.Cmd {
	return CmdHandler(InfoMsg{
		Type: InfoTypeInfo,
		Msg:  info,
	})
}

func ReportWarn(warn string) tea.Cmd {
	return CmdHandler(InfoMsg{
		Type: InfoTypeWarn,
		Msg:  warn,
	})
}

type (
	InfoMsg struct {
		Type InfoType
		Msg  string
		TTL  time.Duration
	}
	ClearStatusMsg struct{}

	// AlertMsg triggers a bubbleup overlay alert instead of the status bar.
	AlertMsg struct {
		Type    string // bubbleup alert key: "Info", "Warn", "Error"
		Msg     string
		Persist bool // if true, alert stays until dismissed with Esc
	}
)

// AlertInfo creates a bubbleup info overlay alert.
func AlertInfo(msg string) tea.Cmd {
	return CmdHandler(AlertMsg{Type: "Info", Msg: msg})
}

// AlertWarn creates a bubbleup warning overlay alert.
func AlertWarn(msg string) tea.Cmd {
	return CmdHandler(AlertMsg{Type: "Warn", Msg: msg})
}

// AlertError creates a bubbleup error overlay alert.
func AlertError(msg string) tea.Cmd {
	return CmdHandler(AlertMsg{Type: "Error", Msg: msg})
}

// AlertPersist creates a persistent bubbleup alert that stays until Esc is pressed.
func AlertPersist(alertType, msg string) tea.Cmd {
	return CmdHandler(AlertMsg{Type: alertType, Msg: msg, Persist: true})
}

func Clamp(v, low, high int) int {
	if high < low {
		low, high = high, low
	}
	return min(high, max(low, v))
}
