// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package instances

import (
	"context"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// instanceBrowserKeyMap holds the keybindings for the instances browser.
var instanceBrowserKeyMap = struct {
	Tab       key.Binding
	Up        key.Binding
	Down      key.Binding
	Enter     key.Binding
	Escape    key.Binding
	Interrupt key.Binding
	SendMsg   key.Binding
	Switch    key.Binding
}{
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch panel"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "move down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Interrupt: key.NewBinding(
		key.WithKeys("i"),
		key.WithHelp("i", "interrupt session"),
	),
	SendMsg: key.NewBinding(
		key.WithKeys("m"),
		key.WithHelp("m", "send message"),
	),
	Switch: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "switch active session"),
	),
}

// Update handles all incoming messages for the instances browser.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		// Re-schedule the tick and refresh the instance list.
		return m, tea.Batch(
			tickCmd(),
			fetchInstancesCmd(m.registry),
		)

	case instancesUpdatedMsg:
		oldLen := len(m.instances)
		m.instances = msg.entries
		// Clamp selection index.
		if m.selectedInst >= len(m.instances) {
			m.selectedInst = max(0, len(m.instances)-1)
		}
		// If the list changed and we have an instance selected, refresh sessions.
		if len(m.instances) > 0 && (oldLen == 0 || m.activePane != paneInstances) {
			entry := m.selectedInstanceEntry()
			if entry != nil {
				return m, fetchSessionsCmd(entry)
			}
		}
		return m, nil

	case sessionsUpdatedMsg:
		m.sessions = msg.sessions
		if m.selectedSession >= len(m.sessions) {
			m.selectedSession = max(0, len(m.sessions)-1)
		}
		return m, nil

	case liveEventMsg:
		m.appendLiveEvent(msg.line)
		return m, nil

	case liveEventWithChannelMsg:
		// Received a relevant event; append it and schedule reading the next one.
		m.appendLiveEvent(msg.line)
		return m, nextLiveEventCmd(msg.ctx, msg.ch, msg.sid)

	case liveSubCancelMsg:
		// Subscription ended; clear the cancel func.
		m.liveCancel = nil
		return m, nil

	case sendMessageResultMsg:
		if msg.err != nil {
			m.setStatus("Error: " + msg.err.Error())
		} else {
			m.setStatus("Message sent")
		}
		return m, nil

	case switchSessionResultMsg:
		if msg.err != nil {
			m.setStatus("Error: " + msg.err.Error())
		} else {
			m.setStatus("Session switched")
		}
		return m, nil

	case tea.KeyMsg:
		// When the message input overlay is active, route all keys to it.
		if m.showMsgInput {
			return m.handleMsgInput(msg)
		}
		return m.handleKey(msg)
	}

	return m, nil
}

// handleMsgInput routes keyboard events to the textinput overlay.
// Enter submits the message, Esc cancels.
func (m Model) handleMsgInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		content := m.msgInput.Value()
		m.showMsgInput = false
		m.msgInput.SetValue("")
		m.msgInput.Blur()
		if content == "" {
			return m, nil
		}
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			m.setStatus("No instance/session selected")
			return m, nil
		}
		return m, sendMessageCmd(entry, sess.ID, content)

	case "esc":
		m.showMsgInput = false
		m.msgInput.SetValue("")
		m.msgInput.Blur()
		return m, nil

	default:
		var cmd tea.Cmd
		m.msgInput, cmd = m.msgInput.Update(msg)
		return m, cmd
	}
}

// handleKey processes keyboard input according to the active pane.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global bindings.
	switch {
	case key.Matches(msg, instanceBrowserKeyMap.Tab):
		m.activePane = (m.activePane + 1) % 3
		return m, nil

	case key.Matches(msg, instanceBrowserKeyMap.Escape):
		// Bubble Esc up to the app model so it can navigate back.
		return m, nil
	}

	// Pane-specific bindings.
	switch m.activePane {
	case paneInstances:
		return m.handleInstancesPane(msg)
	case paneSessions:
		return m.handleSessionsPane(msg)
	case paneLiveView:
		return m.handleLiveViewPane(msg)
	}

	return m, nil
}

// handleInstancesPane handles keys when the instances list pane is focused.
func (m Model) handleInstancesPane(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, instanceBrowserKeyMap.Up):
		if m.selectedInst > 0 {
			m.selectedInst--
			m.sessions = nil
			m.selectedSession = 0
			m.liveEvents = nil
			m.cancelLiveSub()
			entry := m.selectedInstanceEntry()
			if entry != nil {
				return m, fetchSessionsCmd(entry)
			}
		}
	case key.Matches(msg, instanceBrowserKeyMap.Down):
		if m.selectedInst < len(m.instances)-1 {
			m.selectedInst++
			m.sessions = nil
			m.selectedSession = 0
			m.liveEvents = nil
			m.cancelLiveSub()
			entry := m.selectedInstanceEntry()
			if entry != nil {
				return m, fetchSessionsCmd(entry)
			}
		}
	case key.Matches(msg, instanceBrowserKeyMap.Enter):
		// Move focus to the sessions pane.
		if len(m.instances) > 0 {
			m.activePane = paneSessions
		}
	}
	return m, nil
}

// handleSessionsPane handles keys when the sessions list pane is focused.
func (m Model) handleSessionsPane(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, instanceBrowserKeyMap.Up):
		if m.selectedSession > 0 {
			m.selectedSession--
			m.liveEvents = nil
			m.cancelLiveSub()
		}
	case key.Matches(msg, instanceBrowserKeyMap.Down):
		if m.selectedSession < len(m.sessions)-1 {
			m.selectedSession++
			m.liveEvents = nil
			m.cancelLiveSub()
		}
	case key.Matches(msg, instanceBrowserKeyMap.Enter):
		// Start live subscription for the selected session.
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			return m, nil
		}
		m.cancelLiveSub()
		m.liveEvents = nil
		ctx, cancel := context.Background(), context.CancelFunc(func() {})
		ctx, cancel = context.WithCancel(ctx)
		m.liveCancel = cancel
		m.activePane = paneLiveView
		return m, subscribeLiveCmd(ctx, entry, sess.ID)

	case key.Matches(msg, instanceBrowserKeyMap.Interrupt):
		// Send interrupt to the selected session.
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			return m, nil
		}
		return m, interruptSessionCmd(entry, sess.ID)

	case key.Matches(msg, instanceBrowserKeyMap.SendMsg):
		// Open the inline message send dialog.
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			return m, nil
		}
		m.showMsgInput = true
		m.msgInput.Focus()
		return m, nil

	case key.Matches(msg, instanceBrowserKeyMap.Switch):
		// Switch the active session on the remote instance.
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			return m, nil
		}
		return m, switchSessionCmd(entry, sess.ID)
	}
	return m, nil
}

// handleLiveViewPane handles keys when the live view pane is focused.
func (m Model) handleLiveViewPane(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, instanceBrowserKeyMap.SendMsg):
		// Open the inline message send dialog from the live view too.
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			return m, nil
		}
		m.showMsgInput = true
		m.msgInput.Focus()
		return m, nil

	case key.Matches(msg, instanceBrowserKeyMap.Interrupt):
		// Interrupt from live view as well.
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			return m, nil
		}
		return m, interruptSessionCmd(entry, sess.ID)
	}
	return m, nil
}

// max returns the larger of two ints (Go 1.21+ has a built-in, but this keeps
// compatibility with older toolchains that may be in use).
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
