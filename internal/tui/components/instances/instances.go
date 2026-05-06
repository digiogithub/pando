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

// liveEventMsg carries a single formatted event line for the live view.
type liveEventMsg struct {
	line string
}

// liveSubCancelMsg is sent when a live subscription goroutine exits.
type liveSubCancelMsg struct{}

// Model is the main Bubble Tea model for the instances browser.
type Model struct {
	width, height int

	// Data
	instances       []*instanceregistry.Entry
	selectedInst    int
	sessions        []protocol.SessionPayload
	selectedSession int
	liveEvents      []string
	activePane      paneID

	// Live subscription cancellation
	liveCancel context.CancelFunc

	// Registry
	registry *instanceregistry.Registry
}

// New returns a new instances browser model ready to be used.
func New() Model {
	return Model{
		registry:   instanceregistry.New(),
		activePane: paneInstances,
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

// subscribeLiveCmd subscribes to the PUB socket of the given instance and
// returns liveEventMsg for each relevant event. The supplied context cancel
// function is stored in the model so the subscription can be cancelled.
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

		// We only return the first event here; subsequent events are pumped
		// via a goroutine that sends liveEventMsg back to the program.
		// Bubble Tea does not support persistent goroutine subscriptions
		// directly, so we use a channel-based approach via a looping Cmd.
		select {
		case <-ctx.Done():
			return liveSubCancelMsg{}
		case env, ok := <-ch:
			if !ok {
				return liveSubCancelMsg{}
			}
			line := formatEnvelope(env, sessionID)
			if line == "" {
				// Skip irrelevant events by re-scheduling without emitting
				return nextLiveEventCmd(ctx, ch, sessionID)()
			}
			return liveEventMsg{line: line}
		}
	}
}

// nextLiveEventCmd returns a Cmd that reads the next event from an already
// open subscription channel. This is used to keep reading after the first event.
func nextLiveEventCmd(ctx context.Context, ch <-chan ipc.Envelope, sessionID string) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return liveSubCancelMsg{}
		case env, ok := <-ch:
			if !ok {
				return liveSubCancelMsg{}
			}
			line := formatEnvelope(env, sessionID)
			if line == "" {
				return nextLiveEventCmd(ctx, ch, sessionID)()
			}
			return liveEventMsg{line: line}
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
