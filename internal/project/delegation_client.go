package project

import (
	"context"
	"strings"
	"sync"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// captureSink accumulates the agent's streamed text output for a single
// delegated session. It is safe for concurrent use: the ACP SDK dispatches
// inbound notifications on their own goroutine, so SessionUpdate may run
// concurrently with the Delegate caller reading the accumulated text.
type captureSink struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *captureSink) write(text string) {
	if text == "" {
		return
	}
	s.mu.Lock()
	s.buf.WriteString(text)
	s.mu.Unlock()
}

// text returns the text captured so far.
func (s *captureSink) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// delegationClient is the ACP client used for manager-owned child connections.
// It reuses projectACPClient's no-op lifecycle/file/terminal behavior and
// auto-approving permission handling, and additionally CAPTURES the agent's
// streamed message text per delegated session so the orchestrator can recover
// a delegated task's <pando:conclusion> over the wire instead of scanning
// stdout.
//
// A single client instance backs one child connection but serves many
// concurrent delegated sessions; SessionUpdate is demultiplexed to the right
// capture sink by the notification's session id. Sessions that have not been
// registered (e.g. user/editor sessions, or updates after completion) are
// ignored, preserving the previous discard-everything behavior for them.
type delegationClient struct {
	*projectACPClient

	mu    sync.RWMutex
	sinks map[acpsdk.SessionId]*captureSink
}

// newDelegationClient creates a capturing ACP client for a project instance.
func newDelegationClient(proj Project) *delegationClient {
	return &delegationClient{
		projectACPClient: newProjectACPClient(proj),
		sinks:            make(map[acpsdk.SessionId]*captureSink),
	}
}

// register starts capturing output for the given session and returns its sink.
func (c *delegationClient) register(id acpsdk.SessionId) *captureSink {
	sink := &captureSink{}
	c.mu.Lock()
	c.sinks[id] = sink
	c.mu.Unlock()
	return sink
}

// unregister stops capturing output for the given session.
func (c *delegationClient) unregister(id acpsdk.SessionId) {
	c.mu.Lock()
	delete(c.sinks, id)
	c.mu.Unlock()
}

func (c *delegationClient) sinkFor(id acpsdk.SessionId) *captureSink {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sinks[id]
}

// SessionUpdate captures agent message text for registered delegated sessions
// and discards everything else (matching the base no-op client).
func (c *delegationClient) SessionUpdate(_ context.Context, n acpsdk.SessionNotification) error {
	sink := c.sinkFor(n.SessionId)
	if sink == nil {
		return nil
	}
	if chunk := n.Update.AgentMessageChunk; chunk != nil && chunk.Content.Text != nil {
		sink.write(chunk.Content.Text.Text)
	}
	return nil
}
