// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package instances provides a Bubble Tea component for browsing and
// interacting with remote Pando instances in a two-panel layout:
//
//   - Left panel: instances list (top) + sessions list for the selected instance (bottom)
//   - Right panel: live chat view with message history and an input box
package instances

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/digiogithub/pando/internal/instanceregistry"
	ipc "github.com/digiogithub/pando/internal/ipc"
	"github.com/digiogithub/pando/internal/ipc/protocol"
	"github.com/digiogithub/pando/internal/remoteview"
)

// paneID identifies which panel is focused.
type paneID int

const (
	paneInstances paneID = iota
	paneSessions
	paneChat
)

// maxLiveEvents is the maximum number of streaming lines kept in memory.
const maxStreamingLines = 500

// pollInterval is how often the instances list is refreshed.
const pollInterval = 2 * time.Second

// tickMsg is sent by the polling ticker.
type tickMsg time.Time

// instancesUpdatedMsg carries a fresh list of instances.
type instancesUpdatedMsg struct {
	entries []*instanceregistry.Entry
}

// sessionsUpdatedMsg carries the session list for the selected instance.
type sessionsUpdatedMsg struct {
	sessions []protocol.SessionPayload
}

// messagesLoadedMsg carries historical messages for the selected session.
type messagesLoadedMsg struct {
	messages []protocol.MessagePayload
	err      error
}

// liveEventWithChannelMsg carries a formatted event plus the open channel for re-reads.
type liveEventWithChannelMsg struct {
	env ipc.Envelope
	ch  <-chan ipc.Envelope
	ctx context.Context
	sid string
}

// liveSubCancelMsg is sent when a live subscription goroutine exits.
type liveSubCancelMsg struct{}

// sendMessageResultMsg is sent after a remote message.send RPC completes.
type sendMessageResultMsg struct {
	err error
}

// switchSessionResultMsg is sent after a remote session.activate RPC completes.
type switchSessionResultMsg struct {
	err error
}

// chatLine is a single rendered line in the chat panel.
type chatLine struct {
	role    string // "user", "assistant", "tool", "info", "stream"
	content string
}

// Model is the main Bubble Tea model for the instances browser.
type Model struct {
	width, height int

	// Data
	instances       []*instanceregistry.Entry
	selectedInst    int
	sessions        []protocol.SessionPayload
	selectedSession int
	historyMessages []protocol.MessagePayload
	chatLines       []chatLine // rendered chat lines (history + live)
	streamBuf       strings.Builder
	isStreaming     bool

	// Viewport for the chat panel (right side)
	chatViewport viewport.Model

	activePane paneID

	// Scroll offsets for the left panels
	scrollInstances int
	scrollSessions  int

	// Panel heights (set during View, used for mouse hit-testing)
	instBoxH int
	sessBoxH int

	// Live subscription cancellation
	liveCancel context.CancelFunc

	// Registry
	registry *instanceregistry.Registry

	// Message input
	msgInput textinput.Model

	// statusLine holds a transient status message.
	statusLine   string
	statusExpiry time.Time
}

// New returns a new instances browser model.
func New() Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message and press Enter…"
	ti.CharLimit = 4096
	// Start blurred — focus is granted when the user navigates to paneChat.
	ti.Blur()

	vp := viewport.New(0, 0)

	return Model{
		registry:     instanceregistry.New(),
		activePane:   paneInstances,
		msgInput:     ti,
		chatViewport: vp,
	}
}

// Init starts the polling ticker and performs the first instances fetch.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		fetchInstancesCmd(m.registry),
	)
}

// tickCmd returns a command that fires a tickMsg after pollInterval.
func tickCmd() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// fetchInstancesCmd fetches the live instance list from the registry.
func fetchInstancesCmd(reg *instanceregistry.Registry) tea.Cmd {
	return func() tea.Msg {
		entries, err := reg.List()
		if err != nil {
			return instancesUpdatedMsg{}
		}
		return instancesUpdatedMsg{entries: entries}
	}
}

// fetchSessionsCmd loads the session list from the selected instance via RPC.
func fetchSessionsCmd(entry *instanceregistry.Entry) tea.Cmd {
	return func() tea.Msg {
		endpoint := fmt.Sprintf("tcp://127.0.0.1:%d", entry.RPCPort)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		rc, err := remoteview.NewRemoteControl(ctx, endpoint)
		if err != nil {
			return sessionsUpdatedMsg{}
		}
		sessions, err := rc.ListSessions(ctx)
		if err != nil {
			return sessionsUpdatedMsg{}
		}
		return sessionsUpdatedMsg{sessions: sessions}
	}
}

// fetchMessagesCmd loads the message history for a session via RPC.
func fetchMessagesCmd(entry *instanceregistry.Entry, sessionID string) tea.Cmd {
	return func() tea.Msg {
		endpoint := fmt.Sprintf("tcp://127.0.0.1:%d", entry.RPCPort)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		rc, err := remoteview.NewRemoteControl(ctx, endpoint)
		if err != nil {
			return messagesLoadedMsg{err: err}
		}
		msgs, err := rc.ListMessages(ctx, sessionID)
		return messagesLoadedMsg{messages: msgs, err: err}
	}
}

// subscribeLiveCmd subscribes to the PUB socket of the given instance.
func subscribeLiveCmd(ctx context.Context, entry *instanceregistry.Entry, sessionID string) tea.Cmd {
	return func() tea.Msg {
		endpoint := fmt.Sprintf("tcp://127.0.0.1:%d", entry.PubPort)
		client, err := ipc.NewClient(ctx)
		if err != nil {
			return liveSubCancelMsg{}
		}
		ch, err := client.SubscribeTo(endpoint)
		if err != nil {
			return liveSubCancelMsg{}
		}
		return readNextEnvelope(ctx, ch, sessionID)
	}
}

// nextEnvelopeCmd returns a Cmd that reads the next event from an open channel.
func nextEnvelopeCmd(ctx context.Context, ch <-chan ipc.Envelope, sessionID string) tea.Cmd {
	return func() tea.Msg {
		return readNextEnvelope(ctx, ch, sessionID)
	}
}

// readNextEnvelope blocks until the next relevant envelope arrives.
func readNextEnvelope(ctx context.Context, ch <-chan ipc.Envelope, sessionID string) tea.Msg {
	for {
		select {
		case <-ctx.Done():
			return liveSubCancelMsg{}
		case env, ok := <-ch:
			if !ok {
				return liveSubCancelMsg{}
			}
			// Filter by session if one is specified.
			if sessionID != "" && !envelopeMatchesSession(env, sessionID) {
				continue
			}
			return liveEventWithChannelMsg{env: env, ch: ch, ctx: ctx, sid: sessionID}
		}
	}
}

// envelopeMatchesSession checks if an envelope belongs to the given session.
func envelopeMatchesSession(env ipc.Envelope, sessionID string) bool {
	switch env.Topic {
	case protocol.TopicLLMToken:
		var p protocol.LLMTokenPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return false
		}
		return p.SessionID == sessionID
	case protocol.TopicLLMStart:
		var p protocol.LLMStartPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return false
		}
		return p.SessionID == sessionID
	case protocol.TopicLLMEnd:
		var p protocol.LLMEndPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return false
		}
		return p.SessionID == sessionID
	case protocol.TopicToolStart:
		var p protocol.ToolStartPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return false
		}
		return p.SessionID == sessionID
	case protocol.TopicToolEnd:
		var p protocol.ToolEndPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return false
		}
		return p.SessionID == sessionID
	case protocol.TopicMessageAppend:
		var p protocol.MessageAppendPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return false
		}
		return p.SessionID == sessionID
	}
	// Allow heartbeat and session events to pass through regardless.
	return true
}

// interruptSessionCmd sends a session.interrupt RPC to the selected instance.
func interruptSessionCmd(entry *instanceregistry.Entry, sessionID string) tea.Cmd {
	return func() tea.Msg {
		endpoint := fmt.Sprintf("tcp://127.0.0.1:%d", entry.RPCPort)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		rc, err := remoteview.NewRemoteControl(ctx, endpoint)
		if err != nil {
			return sendMessageResultMsg{err: err}
		}
		err = rc.Interrupt(ctx, sessionID)
		return sendMessageResultMsg{err: err}
	}
}

// sendMessageCmd sends a user message to the selected session via RPC.
func sendMessageCmd(entry *instanceregistry.Entry, sessionID, content string) tea.Cmd {
	return func() tea.Msg {
		endpoint := fmt.Sprintf("tcp://127.0.0.1:%d", entry.RPCPort)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		rc, err := remoteview.NewRemoteControl(ctx, endpoint)
		if err != nil {
			return sendMessageResultMsg{err: err}
		}
		err = rc.SendMessage(ctx, sessionID, content)
		return sendMessageResultMsg{err: err}
	}
}

// switchSessionCmd sends a session.activate RPC to the selected instance.
func switchSessionCmd(entry *instanceregistry.Entry, sessionID string) tea.Cmd {
	return func() tea.Msg {
		endpoint := fmt.Sprintf("tcp://127.0.0.1:%d", entry.RPCPort)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		rc, err := remoteview.NewRemoteControl(ctx, endpoint)
		if err != nil {
			return switchSessionResultMsg{err: err}
		}
		err = rc.SwitchSession(ctx, sessionID)
		return switchSessionResultMsg{err: err}
	}
}

// rebuildChatLines rebuilds the chatLines slice from historyMessages + streaming.
func (m *Model) rebuildChatLines() {
	m.chatLines = nil
	for _, msg := range m.historyMessages {
		role := msg.Role
		if role == "" {
			role = "info"
		}
		m.chatLines = append(m.chatLines, chatLine{role: role, content: msg.Content})
	}
	if m.isStreaming && m.streamBuf.Len() > 0 {
		m.chatLines = append(m.chatLines, chatLine{role: "stream", content: m.streamBuf.String()})
	}
}

// scrollChatToBottom moves the viewport to the bottom.
func (m *Model) scrollChatToBottom() {
	m.chatViewport.GotoBottom()
}

// Width returns the current width of the model.
func (m Model) Width() int { return m.width }

// Height returns the current height of the model.
func (m Model) Height() int { return m.height }

// SetSize sets the width and height of the model.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.updateViewportSize()
}

// updateViewportSize recalculates the viewport dimensions to fit inside the chat panel.
//
// Chat panel outer height = leftH = m.height - 1
// Inside border (-2): leftH - 2
// title(-1) + viewport + hints(-1) + input(-1) = leftH - 2
// → viewport height = leftH - 2 - 3 = m.height - 6
//
// Viewport width = rightWidth - 2 (inside left+right border chars).
func (m *Model) updateViewportSize() {
	leftWidth := m.width * 30 / 100
	rightWidth := m.width - leftWidth - 1

	// leftH = m.height - 1 (header)
	// chat outer = leftH; inner = leftH - 2 (borders)
	// title(1) + hints(1) + input(1) = 3 rows consumed
	vpH := m.height - 1 - 2 - 3
	if vpH < 1 {
		vpH = 1
	}

	// -2 for left+right border characters inside the panel
	vpW := rightWidth - 2
	if vpW < 10 {
		vpW = 10
	}

	m.chatViewport.Width = vpW
	m.chatViewport.Height = vpH
}

// setStatus sets a transient status message.
func (m *Model) setStatus(msg string) {
	m.statusLine = msg
	m.statusExpiry = time.Now().Add(3 * time.Second)
}

// setActivePane changes the active pane and manages textinput focus accordingly.
func (m *Model) setActivePane(p paneID) {
	m.activePane = p
	if p == paneChat {
		m.msgInput.Focus()
	} else {
		m.msgInput.Blur()
	}
}

// cancelLiveSub cancels the current live subscription if one exists.
func (m *Model) cancelLiveSub() {
	if m.liveCancel != nil {
		m.liveCancel()
		m.liveCancel = nil
	}
}

// selectedInstanceEntry returns the currently selected instance entry, or nil.
func (m *Model) selectedInstanceEntry() *instanceregistry.Entry {
	if len(m.instances) == 0 || m.selectedInst < 0 || m.selectedInst >= len(m.instances) {
		return nil
	}
	return m.instances[m.selectedInst]
}

// selectedSessionEntry returns the currently selected session, or nil.
func (m *Model) selectedSessionEntry() *protocol.SessionPayload {
	if len(m.sessions) == 0 || m.selectedSession < 0 || m.selectedSession >= len(m.sessions) {
		return nil
	}
	return &m.sessions[m.selectedSession]
}

// clampScroll ensures scroll offsets are within valid bounds.
func clampScroll(offset, listLen, visibleH int) int {
	if offset < 0 {
		return 0
	}
	max := listLen - visibleH
	if max < 0 {
		max = 0
	}
	if offset > max {
		return max
	}
	return offset
}
