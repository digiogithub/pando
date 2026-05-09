// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package instances

import (
	"context"
	"encoding/json"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	ipc "github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/tui/theme"
)

// instanceBrowserKeyMap holds the keybindings for the instances browser.
var instanceBrowserKeyMap = struct {
	Tab       key.Binding
	Up        key.Binding
	Down      key.Binding
	Enter     key.Binding
	Escape    key.Binding
	Interrupt key.Binding
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
		key.WithHelp("enter", "select / send"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Interrupt: key.NewBinding(
		key.WithKeys("i"),
		key.WithHelp("i", "interrupt session"),
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
		m.updateViewportSize()
		m.refreshViewportContent()
		return m, nil

	case tickMsg:
		// Re-schedule the tick and refresh the instance list.
		// Also clear expired status lines.
		return m, tea.Batch(tickCmd(), fetchInstancesCmd(m.registry))

	case instancesUpdatedMsg:
		oldLen := len(m.instances)
		m.instances = msg.entries
		if m.selectedInst >= len(m.instances) {
			m.selectedInst = max(0, len(m.instances)-1)
		}
		if len(m.instances) > 0 && (oldLen == 0 || m.activePane != paneInstances) {
			if entry := m.selectedInstanceEntry(); entry != nil {
				return m, fetchSessionsCmd(entry)
			}
		}
		return m, nil

	case sessionsUpdatedMsg:
		m.sessions = msg.sessions
		if m.selectedSession >= len(m.sessions) {
			m.selectedSession = max(0, len(m.sessions)-1)
		}
		if sess := m.selectedSessionEntry(); sess != nil {
			if entry := m.selectedInstanceEntry(); entry != nil {
				return m, fetchMessagesCmd(entry, sess.ID)
			}
		}
		return m, nil

	case messagesLoadedMsg:
		if msg.err == nil {
			m.historyMessages = msg.messages
		}
		m.rebuildChatLines()
		m.refreshViewportContent()
		m.scrollChatToBottom()
		return m, nil

	case liveEventWithChannelMsg:
		// Process the envelope and schedule the next read.
		m.handleEnvelope(msg.env)
		return m, nextEnvelopeCmd(msg.ctx, msg.ch, msg.sid)

	case liveSubCancelMsg:
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

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		if m.activePane == paneChat {
			return m.handleChatKey(msg)
		}
		return m.handleKey(msg)
	}

	return m, nil
}

// handleEnvelope processes a ZMQ envelope and updates the chat state.
func (m *Model) handleEnvelope(env ipc.Envelope) {
	t := theme.CurrentTheme()
	_ = t

	switch env.Topic {
	case protocol.TopicLLMStart:
		m.isStreaming = true
		m.streamBuf.Reset()

	case protocol.TopicLLMToken:
		var p protocol.LLMTokenPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		m.streamBuf.WriteString(p.Token)
		m.rebuildChatLines()
		m.refreshViewportContent()
		m.scrollChatToBottom()

	case protocol.TopicLLMEnd:
		if m.isStreaming && m.streamBuf.Len() > 0 {
			// Commit the streaming message as a history entry.
			m.historyMessages = append(m.historyMessages, protocol.MessagePayload{
				Role:    "assistant",
				Content: m.streamBuf.String(),
			})
		}
		m.isStreaming = false
		m.streamBuf.Reset()
		m.rebuildChatLines()
		m.refreshViewportContent()
		m.scrollChatToBottom()

	case protocol.TopicToolStart:
		var p protocol.ToolStartPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		m.historyMessages = append(m.historyMessages, protocol.MessagePayload{
			Role:    "tool",
			Content: "⚙ " + p.ToolName + "…",
		})
		m.rebuildChatLines()
		m.refreshViewportContent()

	case protocol.TopicToolEnd:
		var p protocol.ToolEndPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		suffix := " ✓"
		if p.IsError {
			suffix = " ✗"
		}
		m.historyMessages = append(m.historyMessages, protocol.MessagePayload{
			Role:    "tool",
			Content: "⚙ " + p.ToolName + suffix,
		})
		m.rebuildChatLines()
		m.refreshViewportContent()

	case protocol.TopicMessageAppend:
		var p protocol.MessageAppendPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return
		}
		// Only add user messages here; assistant messages come via llm.token stream.
		if p.Role == "user" {
			m.historyMessages = append(m.historyMessages, protocol.MessagePayload{
				Role:    p.Role,
				Content: p.Content,
			})
			m.rebuildChatLines()
			m.refreshViewportContent()
			m.scrollChatToBottom()
		}
	}
}

// refreshViewportContent re-renders chat lines into the viewport.
func (m *Model) refreshViewportContent() {
	t := theme.CurrentTheme()
	content := renderChatLines(t, m.chatLines, m.chatViewport.Width+4)
	m.chatViewport.SetContent(content)
}

// handleKey processes keyboard input when not in the chat pane.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, instanceBrowserKeyMap.Tab):
		m.setActivePane((m.activePane + 1) % 3)
		return m, nil

	case key.Matches(msg, instanceBrowserKeyMap.Escape):
		return m, nil
	}

	switch m.activePane {
	case paneInstances:
		return m.handleInstancesPane(msg)
	case paneSessions:
		return m.handleSessionsPane(msg)
	}

	return m, nil
}

// handleChatKey processes keyboard input when the chat pane is focused.
func (m Model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		content := m.msgInput.Value()
		m.msgInput.SetValue("")
		if content == "" {
			return m, nil
		}
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			m.setStatus("No instance/session selected")
			return m, nil
		}
		// Add locally to chat immediately.
		m.historyMessages = append(m.historyMessages, protocol.MessagePayload{
			Role:    "user",
			Content: content,
		})
		m.rebuildChatLines()
		m.refreshViewportContent()
		m.scrollChatToBottom()
		return m, sendMessageCmd(entry, sess.ID, content)

	case "tab":
		m.setActivePane((m.activePane + 1) % 3)
		return m, nil

	case "esc":
		m.setActivePane(paneSessions)
		return m, nil

	case "i":
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			return m, nil
		}
		return m, interruptSessionCmd(entry, sess.ID)

	case "s":
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			return m, nil
		}
		return m, switchSessionCmd(entry, sess.ID)

	default:
		// Forward remaining keys to the text input and to the viewport for scrolling.
		var cmd tea.Cmd
		m.msgInput, cmd = m.msgInput.Update(msg)
		var vpCmd tea.Cmd
		m.chatViewport, vpCmd = m.chatViewport.Update(msg)
		return m, tea.Batch(cmd, vpCmd)
	}
}

// handleInstancesPane handles keys when the instances list pane is focused.
func (m Model) handleInstancesPane(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	leftWidth := m.width * 30 / 100
	leftH := m.height - 1
	instH := leftH * 40 / 100
	if instH < 3 {
		instH = 3
	}
	visibleInstH := instH - 3

	switch {
	case key.Matches(msg, instanceBrowserKeyMap.Up):
		if m.selectedInst > 0 {
			m.selectedInst--
			m.scrollInstances = clampScroll(m.scrollInstances, len(m.instances), visibleInstH)
			if m.selectedInst < m.scrollInstances {
				m.scrollInstances = m.selectedInst
			}
			m.resetSession()
			if entry := m.selectedInstanceEntry(); entry != nil {
				return m, fetchSessionsCmd(entry)
			}
		}
	case key.Matches(msg, instanceBrowserKeyMap.Down):
		if m.selectedInst < len(m.instances)-1 {
			m.selectedInst++
			if m.selectedInst >= m.scrollInstances+visibleInstH {
				m.scrollInstances = m.selectedInst - visibleInstH + 1
			}
			m.resetSession()
			if entry := m.selectedInstanceEntry(); entry != nil {
				return m, fetchSessionsCmd(entry)
			}
		}
	case key.Matches(msg, instanceBrowserKeyMap.Enter):
		if len(m.instances) > 0 {
			m.setActivePane(paneSessions)
		}
	}
	_ = leftWidth
	return m, nil
}

// handleSessionsPane handles keys when the sessions list pane is focused.
func (m Model) handleSessionsPane(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	leftH := m.height - 1
	instH := leftH * 40 / 100
	if instH < 3 {
		instH = 3
	}
	sessH := leftH - instH
	visibleSessH := sessH - 3

	switch {
	case key.Matches(msg, instanceBrowserKeyMap.Up):
		if m.selectedSession > 0 {
			m.selectedSession--
			if m.selectedSession < m.scrollSessions {
				m.scrollSessions = m.selectedSession
			}
			m.resetChat()
			entry := m.selectedInstanceEntry()
			sess := m.selectedSessionEntry()
			if entry != nil && sess != nil {
				return m, fetchMessagesCmd(entry, sess.ID)
			}
		}
	case key.Matches(msg, instanceBrowserKeyMap.Down):
		if m.selectedSession < len(m.sessions)-1 {
			m.selectedSession++
			if m.selectedSession >= m.scrollSessions+visibleSessH {
				m.scrollSessions = m.selectedSession - visibleSessH + 1
			}
			m.resetChat()
			entry := m.selectedInstanceEntry()
			sess := m.selectedSessionEntry()
			if entry != nil && sess != nil {
				return m, fetchMessagesCmd(entry, sess.ID)
			}
		}
	case key.Matches(msg, instanceBrowserKeyMap.Enter):
		// Move to chat and start live subscription.
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			return m, nil
		}
		m.cancelLiveSub()
		m.resetChat()
		ctx, cancel := context.WithCancel(context.Background())
		m.liveCancel = cancel
		m.setActivePane(paneChat)
		return m, tea.Batch(
			fetchMessagesCmd(entry, sess.ID),
			subscribeLiveCmd(ctx, entry, sess.ID),
		)

	case key.Matches(msg, instanceBrowserKeyMap.Interrupt):
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			return m, nil
		}
		return m, interruptSessionCmd(entry, sess.ID)

	case key.Matches(msg, instanceBrowserKeyMap.Switch):
		entry := m.selectedInstanceEntry()
		sess := m.selectedSessionEntry()
		if entry == nil || sess == nil {
			return m, nil
		}
		return m, switchSessionCmd(entry, sess.ID)
	}
	return m, nil
}

// handleMouse processes mouse events for panel selection and scrolling.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	leftWidth := m.width * 30 / 100
	leftH := m.height - 1
	instH := leftH * 40 / 100
	if instH < 3 {
		instH = 3
	}

	// Determine mouse position relative to layout.
	x := msg.X
	y := msg.Y - 1 // subtract header row

	if x < leftWidth {
		// Click is in the left column.
		if y >= 0 && y < instH {
			// Instances panel.
			switch msg.Button {
			case tea.MouseButtonLeft:
				if msg.Action == tea.MouseActionPress {
					m.setActivePane(paneInstances)
					// Row 0 is title, rows 1+ are items.
					itemRow := y - 1
					if itemRow >= 0 {
						idx := m.scrollInstances + itemRow
						if idx < len(m.instances) {
							m.selectedInst = idx
							m.resetSession()
							if entry := m.selectedInstanceEntry(); entry != nil {
								return m, fetchSessionsCmd(entry)
							}
						}
					}
				}
			case tea.MouseButtonWheelUp:
				if m.scrollInstances > 0 {
					m.scrollInstances--
				}
			case tea.MouseButtonWheelDown:
				m.scrollInstances++
			}
		} else if y >= instH {
			// Sessions panel.
			sessY := y - instH
			switch msg.Button {
			case tea.MouseButtonLeft:
				if msg.Action == tea.MouseActionPress {
					m.setActivePane(paneSessions)
					itemRow := sessY - 1
					if itemRow >= 0 {
						idx := m.scrollSessions + itemRow
						if idx < len(m.sessions) {
							m.selectedSession = idx
							m.resetChat()
							entry := m.selectedInstanceEntry()
							sess := m.selectedSessionEntry()
							if entry != nil && sess != nil {
								return m, fetchMessagesCmd(entry, sess.ID)
							}
						}
					}
				}
			case tea.MouseButtonWheelUp:
				if m.scrollSessions > 0 {
					m.scrollSessions--
				}
			case tea.MouseButtonWheelDown:
				m.scrollSessions++
			}
		}
	} else {
		// Click is in the right (chat) panel.
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			m.setActivePane(paneChat)
		}
		// Forward scroll wheel to viewport.
		var cmd tea.Cmd
		m.chatViewport, cmd = m.chatViewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

// resetSession clears session data when the selected instance changes.
func (m *Model) resetSession() {
	m.sessions = nil
	m.selectedSession = 0
	m.scrollSessions = 0
	m.resetChat()
}

// resetChat clears chat history and streaming state.
func (m *Model) resetChat() {
	m.historyMessages = nil
	m.chatLines = nil
	m.isStreaming = false
	m.streamBuf.Reset()
	m.cancelLiveSub()
	m.refreshViewportContent()
}

