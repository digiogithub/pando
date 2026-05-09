// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

// Package instances provides a Bubble Tea component for browsing and
// observing remote Pando instances in a three-panel layout:
//
//   - Left panel: list of running instances discovered via instanceregistry
//   - Top-right panel: session list for the selected instance (loaded via RPC)
//   - Bottom-right panel: live event stream for the selected session (via ZMQ PUB)
package instances

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
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
	paneLiveView
)

// maxLiveEvents is the maximum number of live event lines kept in memory.
const maxLiveEvents = 200

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

// liveEventMsg carries a single formatted event line for the live view.
type liveEventMsg struct {
	line string
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

// Model is the main Bubble Tea model for the instances browser.
type Model struct {
	width, height int

	// Data
	instances        []*instanceregistry.Entry
	selectedInst     int
	sessions         []protocol.SessionPayload
	selectedSession  int
	historyMessages  []protocol.MessagePayload // historical messages for selected session
	liveEvents       []string
	activePane       paneID

	// Live subscription cancellation
	liveCancel context.CancelFunc

	// Registry
	registry *instanceregistry.Registry

	// Message send dialog: when showMsgInput is true an inline textinput is
	// overlaid at the bottom of the live-view pane.
	showMsgInput bool
	msgInput     textinput.Model

	// statusLine holds a transient status message shown in the header bar
	// (e.g. "Message sent", "Session switched", or an error).
	statusLine    string
	statusExpiry  time.Time
}

// New returns a new instances browser model ready to be used.
func New() Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message and press Enter…"
	ti.CharLimit = 4096

	return Model{
		registry:   instanceregistry.New(),
		activePane: paneInstances,
		msgInput:   ti,
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

// fetchMessagesCmd loads the message history for a session from the selected instance via RPC.
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

// liveEventWithChannelMsg carries a formatted event line plus the open channel,
// so the Update loop can schedule the next read without leaking goroutine stack depth.
type liveEventWithChannelMsg struct {
	line string
	ch   <-chan ipc.Envelope
	ctx  context.Context
	sid  string
}

// liveSubCancelWithChannelMsg signals that the subscription ended.
type liveSubCancelWithChannelMsg struct{}

// subscribeLiveCmd subscribes to the PUB socket of the given instance and
// returns liveEventWithChannelMsg for each relevant event. The supplied
// context cancel function is stored in the model so the subscription can be
// cancelled. Irrelevant events are skipped inside the blocking goroutine so
// the tea event loop only wakes for events that produce visible output.
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

		return readNextRelevantEvent(ctx, ch, sessionID)
	}
}

// nextLiveEventCmd returns a Cmd that reads the next relevant event from an
// already-open subscription channel. Returning a Cmd (rather than calling
// directly with `()`) ensures each iteration is scheduled by the tea runtime,
// preventing unbounded goroutine stack growth on high-volume event streams.
func nextLiveEventCmd(ctx context.Context, ch <-chan ipc.Envelope, sessionID string) tea.Cmd {
	return func() tea.Msg {
		return readNextRelevantEvent(ctx, ch, sessionID)
	}
}

// readNextRelevantEvent blocks on ch until a relevant event (non-empty line)
// is found or the context is done / channel closed. Called inside a tea.Cmd
// goroutine so blocking here is safe.
func readNextRelevantEvent(ctx context.Context, ch <-chan ipc.Envelope, sessionID string) tea.Msg {
	for {
		select {
		case <-ctx.Done():
			return liveSubCancelMsg{}
		case env, ok := <-ch:
			if !ok {
				return liveSubCancelMsg{}
			}
			line := formatEnvelope(env, sessionID)
			if line == "" {
				// Skip irrelevant events (e.g. heartbeats) and read next one.
				continue
			}
			return liveEventWithChannelMsg{line: line, ch: ch, ctx: ctx, sid: sessionID}
		}
	}
}

// formatEnvelope converts a ZMQ envelope to a human-readable event line.
// Returns an empty string for events that should be skipped.
func formatEnvelope(env ipc.Envelope, sessionID string) string {
	switch env.Topic {
	case "llm.token":
		var p protocol.LLMTokenPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return ""
		}
		if sessionID != "" && p.SessionID != sessionID {
			return ""
		}
		return "[LLM] " + p.Token
	case "llm.start":
		var p protocol.LLMStartPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return ""
		}
		if sessionID != "" && p.SessionID != sessionID {
			return ""
		}
		return "[LLM] thinking..."
	case "llm.end":
		var p protocol.LLMEndPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return ""
		}
		if sessionID != "" && p.SessionID != sessionID {
			return ""
		}
		return fmt.Sprintf("[LLM] done (in:%d out:%d)", p.TokensIn, p.TokensOut)
	case "tool.start":
		var p protocol.ToolStartPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return ""
		}
		if sessionID != "" && p.SessionID != sessionID {
			return ""
		}
		return "[Tool] " + p.ToolName + " ..."
	case "tool.end":
		var p protocol.ToolEndPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return ""
		}
		if sessionID != "" && p.SessionID != sessionID {
			return ""
		}
		if p.IsError {
			return "[Tool] " + p.ToolName + " ERROR"
		}
		return "[Tool] " + p.ToolName + " done"
	case "message.append":
		var p protocol.MessageAppendPayload
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return ""
		}
		if sessionID != "" && p.SessionID != sessionID {
			return ""
		}
		prefix := "[Msg]"
		if p.Role == "assistant" {
			prefix = "[Asst]"
		}
		content := p.Content
		if len(content) > 80 {
			content = content[:80] + "..."
		}
		return prefix + " " + content
	}
	return ""
}

// interruptSessionCmd sends a session.interrupt RPC to the selected instance.
func interruptSessionCmd(entry *instanceregistry.Entry, sessionID string) tea.Cmd {
	return func() tea.Msg {
		endpoint := fmt.Sprintf("tcp://127.0.0.1:%d", entry.RPCPort)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		rc, err := remoteview.NewRemoteControl(ctx, endpoint)
		if err != nil {
			return liveEventMsg{line: "[Err] interrupt: " + err.Error()}
		}
		if err := rc.Interrupt(ctx, sessionID); err != nil {
			return liveEventMsg{line: "[Err] interrupt: " + err.Error()}
		}
		return liveEventMsg{line: "[Info] session interrupted"}
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

// appendLiveEvent appends a line to the live events buffer, capping at maxLiveEvents.
func (m *Model) appendLiveEvent(line string) {
	m.liveEvents = append(m.liveEvents, line)
	if len(m.liveEvents) > maxLiveEvents {
		m.liveEvents = m.liveEvents[len(m.liveEvents)-maxLiveEvents:]
	}
}

// Width returns the current width of the model.
func (m Model) Width() int { return m.width }

// Height returns the current height of the model.
func (m Model) Height() int { return m.height }

// SetSize sets the width and height of the model.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

// setStatus sets a transient status message that is shown in the header bar
// for 3 seconds before being cleared by the next polling tick.
func (m *Model) setStatus(msg string) {
	m.statusLine = msg
	m.statusExpiry = time.Now().Add(3 * time.Second)
}

// cancelLiveSub cancels the current live subscription if one exists.
func (m *Model) cancelLiveSub() {
	if m.liveCancel != nil {
		m.liveCancel()
		m.liveCancel = nil
	}
}

// selectedInstance returns the currently selected instance entry, or nil.
func (m *Model) selectedInstanceEntry() *instanceregistry.Entry {
	if len(m.instances) == 0 || m.selectedInst < 0 || m.selectedInst >= len(m.instances) {
		return nil
	}
	return m.instances[m.selectedInst]
}

// selectedSessionEntry returns the currently selected session, or zero value.
func (m *Model) selectedSessionEntry() *protocol.SessionPayload {
	if len(m.sessions) == 0 || m.selectedSession < 0 || m.selectedSession >= len(m.sessions) {
		return nil
	}
	return &m.sessions[m.selectedSession]
}
