package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/session"
)

func TestNormalizeEnrichedBlock(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		maxChars int
		want     string
	}{
		{
			name: "extracts tagged block",
			raw:  "here you go\n<enriched_context>\n## Code\n- a.go:1 Foo — does x\n</enriched_context>\ntrailing",
			want: "<enriched_context>\n## Code\n- a.go:1 Foo — does x\n</enriched_context>",
		},
		{
			name: "wraps untagged output",
			raw:  "## Code\n- a.go:1 Foo",
			want: "<enriched_context>\n## Code\n- a.go:1 Foo\n</enriched_context>",
		},
		{name: "no relevant context", raw: "NO_RELEVANT_CONTEXT", want: ""},
		{name: "empty", raw: "   ", want: ""},
		{
			name:     "truncates to max chars",
			raw:      "<enriched_context>\nabcdefghij\n</enriched_context>",
			maxChars: 4,
			want:     "<enriched_context>\nabcd\n… (truncated)\n</enriched_context>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeEnrichedBlock(tt.raw, tt.maxChars)
			if got != tt.want {
				t.Errorf("normalizeEnrichedBlock() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

// --- fakes shared by the runLoop/EnrichContextForSession tests below ---

// enricherFakeSessions is a minimal in-memory session.Service good enough to
// exercise agentLoopEnricher's create/get/save/delete calls without a database.
type enricherFakeSessions struct {
	mu       sync.Mutex
	sessions map[string]session.Session
	deleted  []string
	nextID   int
}

func newEnricherFakeSessions() *enricherFakeSessions {
	return &enricherFakeSessions{sessions: map[string]session.Session{}}
}

func (s *enricherFakeSessions) Subscribe(ctx context.Context) <-chan pubsub.Event[session.Session] {
	return nil
}

func (s *enricherFakeSessions) Create(ctx context.Context, title string) (session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	sess := session.Session{ID: fmt.Sprintf("standalone-%d", s.nextID), Title: title}
	s.sessions[sess.ID] = sess
	return sess, nil
}

func (s *enricherFakeSessions) CreateTitleSession(ctx context.Context, parentSessionID string) (session.Session, error) {
	return session.Session{}, errors.New("not implemented")
}

func (s *enricherFakeSessions) CreateTaskSession(ctx context.Context, toolCallID, parentSessionID, title string) (session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := session.Session{ID: toolCallID, ParentSessionID: parentSessionID, Title: title}
	s.sessions[sess.ID] = sess
	return sess, nil
}

func (s *enricherFakeSessions) GetACPSessionState(ctx context.Context, sessionID string) (string, error) {
	return "", nil
}

func (s *enricherFakeSessions) Get(ctx context.Context, id string) (session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return session.Session{}, fmt.Errorf("session %s not found", id)
	}
	return sess, nil
}

func (s *enricherFakeSessions) List(ctx context.Context) ([]session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]session.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out, nil
}

func (s *enricherFakeSessions) SaveACPSessionState(ctx context.Context, sessionID string, state string) error {
	return nil
}

func (s *enricherFakeSessions) Save(ctx context.Context, sess session.Session) (session.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
	return sess, nil
}

func (s *enricherFakeSessions) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return fmt.Errorf("session %s not found", id)
	}
	delete(s.sessions, id)
	s.deleted = append(s.deleted, id)
	return nil
}

func (s *enricherFakeSessions) EndSession(ctx context.Context, id string) error { return nil }

func (s *enricherFakeSessions) deletedIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deleted...)
}

// childSessionID returns the id of the (at most one, in these tests) session
// created as a child of a parent session, or "" once it has been deleted.
func (s *enricherFakeSessions) childSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.sessions {
		if sess.ParentSessionID != "" {
			return id
		}
	}
	return ""
}

// enricherFakeAgent is a minimal agent.Service used to drive agentLoopEnricher's
// runLoop, following the steerMockAgent pattern (internal/api/handlers_steer_test.go).
type enricherFakeAgent struct {
	runFn    func(sessionID, content string) (<-chan agent.AgentEvent, error)
	cancelFn func(sessionID string)
}

func (f *enricherFakeAgent) Subscribe(ctx context.Context) <-chan pubsub.Event[agent.AgentEvent] {
	return nil
}
func (f *enricherFakeAgent) Model() models.Model { return models.Model{} }
func (f *enricherFakeAgent) Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan agent.AgentEvent, error) {
	return f.runFn(sessionID, content)
}
func (f *enricherFakeAgent) LastRunSystemMessages(sessionID string) []string { return nil }
func (f *enricherFakeAgent) Cancel(sessionID string) {
	if f.cancelFn != nil {
		f.cancelFn(sessionID)
	}
}
func (f *enricherFakeAgent) Steer(sessionID string, content string, attachments ...message.Attachment) error {
	return agent.ErrSessionNotBusy
}
func (f *enricherFakeAgent) PendingSteering(sessionID string) int { return 0 }
func (f *enricherFakeAgent) InjectConclusion(sessionID string, content string) error {
	return agent.ErrSessionNotBusy
}
func (f *enricherFakeAgent) Resume(ctx context.Context, sessionID string, content string) error {
	return nil
}
func (f *enricherFakeAgent) ResurrectionCount(sessionID string) int { return 0 }
func (f *enricherFakeAgent) IsSessionBusy(sessionID string) bool    { return false }
func (f *enricherFakeAgent) IsBusy() bool                           { return false }
func (f *enricherFakeAgent) Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error) {
	return models.Model{}, nil
}
func (f *enricherFakeAgent) Summarize(ctx context.Context, sessionID string) error { return nil }
func (f *enricherFakeAgent) SummarizeStream(ctx context.Context, sessionID string) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent)
	close(ch)
	return ch, nil
}
func (f *enricherFakeAgent) SetLuaManager(fm *luaengine.FilterManager) {}
func (f *enricherFakeAgent) GetTools() []tools.BaseTool                { return nil }

// fakeSearchFallback is a fake for the narrow searchFallbackEnricher interface
// agentLoopEnricher.fallback uses, so tests do not need a real *rag.ContextEnricher.
type fakeSearchFallback struct {
	block string
	calls int
}

func (f *fakeSearchFallback) EnrichContext(ctx context.Context, query string) string {
	f.calls++
	return f.block
}

// newTestEnricher builds an agentLoopEnricher wired to a fake session service and
// a fake enrichment agent whose Run/Cancel behavior is supplied by the caller.
func newTestEnricher(runFn func(sessionID, content string) (<-chan agent.AgentEvent, error), cancelFn func(string)) (*agentLoopEnricher, *enricherFakeSessions) {
	sessions := newEnricherFakeSessions()
	fake := &enricherFakeAgent{runFn: runFn, cancelFn: cancelFn}
	e := &agentLoopEnricher{
		sessions: sessions,
		timeout:  time.Second,
		maxChars: defaultEnrichmentLoopMaxChars,
		newAgent: func() (agent.Service, error) { return fake, nil },
	}
	return e, sessions
}

// withNonDebugConfig ensures cfg.Debug is false for the duration of the test, so
// deleteSessionCleanup's Debug guard does not accidentally suppress deletion (or
// the reverse) depending on what an earlier test in this package left behind.
func withNonDebugConfig(t *testing.T) {
	t.Helper()
	prev := config.Get()
	config.SetForTests(&config.Config{})
	t.Cleanup(func() { config.SetForTests(prev) })
}

func assistantResponseEvent(text string) agent.AgentEvent {
	return agent.AgentEvent{
		Type: agent.AgentEventTypeResponse,
		Done: true,
		Message: message.Message{
			Role:  message.Assistant,
			Parts: []message.ContentPart{message.TextContent{Text: text}},
		},
	}
}

// TestEnrichContextForSessionDrainsUntilFinalResponse pins the fix for the
// "single receive" bug: the run channel streams several non-terminal events —
// a system message, deltas, a tool call/result, token usage — before the real
// terminal Response, and the enricher must use that last event as the result
// (not whichever one happens to arrive first).
func TestEnrichContextForSessionDrainsUntilFinalResponse(t *testing.T) {
	withNonDebugConfig(t)

	e, sessions := newTestEnricher(func(sessionID, content string) (<-chan agent.AgentEvent, error) {
		ch := make(chan agent.AgentEvent, 8)
		go func() {
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeSystemMessage, SystemMessage: "starting"}
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeThinkingDelta, Delta: "thinking..."}
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeContentDelta, Delta: "chunk"}
			tc := message.ToolCall{ID: "call-1", Name: "kb_search", Input: "{}"}
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeToolCall, ToolCall: &tc}
			tr := message.ToolResult{ToolCallID: "call-1", Content: "some result"}
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeToolResult, ToolResult: &tr}
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeTokenUsage, TokenUsage: &agent.TokenUsageInfo{PromptTokens: 42}}
			ch <- assistantResponseEvent("<enriched_context>\nfound it\n</enriched_context>")
			close(ch)
		}()
		return ch, nil
	}, nil)

	outcome := e.EnrichContextForSession(context.Background(), "parent-1", "where is X defined?")

	if outcome.Block != "<enriched_context>\nfound it\n</enriched_context>" {
		t.Fatalf("Block = %q, want the tagged block from the final Response", outcome.Block)
	}
	if outcome.Source != agent.EnrichmentSourceAgentLoop {
		t.Fatalf("Source = %q, want %q", outcome.Source, agent.EnrichmentSourceAgentLoop)
	}
	if outcome.TimedOut {
		t.Fatal("TimedOut = true, want false")
	}

	// The child session must be gone once the run (and its cleanup) finished.
	if childID := sessions.childSessionID(); childID != "" {
		t.Fatalf("child session %q was not cleaned up after the run", childID)
	}
	if got := sessions.deletedIDs(); len(got) != 1 {
		t.Fatalf("deleted sessions = %v, want exactly one", got)
	}
}

// TestEnrichContextForSessionIgnoresNonTerminalError covers an intermediate
// Error event (the shape emitCompactionError sends mid-run) followed by the
// real terminal Response: the intermediate error must not be mistaken for the
// result.
func TestEnrichContextForSessionIgnoresNonTerminalError(t *testing.T) {
	e, _ := newTestEnricher(func(sessionID, content string) (<-chan agent.AgentEvent, error) {
		ch := make(chan agent.AgentEvent, 4)
		go func() {
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeError, Error: errors.New("context compaction failed, continuing")}
			ch <- assistantResponseEvent("<enriched_context>\nstill found it\n</enriched_context>")
			close(ch)
		}()
		return ch, nil
	}, nil)

	outcome := e.EnrichContextForSession(context.Background(), "parent-2", "query")

	if outcome.Block == "" {
		t.Fatal("Block is empty, want the block from the final Response despite the earlier non-terminal Error")
	}
	if outcome.Source != agent.EnrichmentSourceAgentLoop {
		t.Fatalf("Source = %q, want %q", outcome.Source, agent.EnrichmentSourceAgentLoop)
	}
}

// TestEnrichContextForSessionFinalErrorFallsBack covers the terminal Error
// case: the loop failed outright, so the block must come from the search
// fallback instead.
func TestEnrichContextForSessionFinalErrorFallsBack(t *testing.T) {
	e, _ := newTestEnricher(func(sessionID, content string) (<-chan agent.AgentEvent, error) {
		ch := make(chan agent.AgentEvent, 2)
		go func() {
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeError, Error: errors.New("provider exploded")}
			close(ch)
		}()
		return ch, nil
	}, nil)
	fallback := &fakeSearchFallback{block: "fallback context"}
	e.fallback = fallback

	outcome := e.EnrichContextForSession(context.Background(), "parent-3", "query")

	if outcome.Block != "fallback context" {
		t.Fatalf("Block = %q, want the fallback's block", outcome.Block)
	}
	if outcome.Source != agent.EnrichmentSourceSearchFallback {
		t.Fatalf("Source = %q, want %q", outcome.Source, agent.EnrichmentSourceSearchFallback)
	}
	if outcome.TimedOut {
		t.Fatal("TimedOut = true, want false (this was a failure, not a timeout)")
	}
	if fallback.calls != 1 {
		t.Fatalf("fallback called %d times, want 1", fallback.calls)
	}
}

// TestEnrichContextForSessionTimeoutCancelsAndWaitsForCleanup covers the
// timeout path: the fake agent never sends anything until Cancel is invoked,
// simulating the agent taking a while to unwind through its own cancellation
// path. The enricher must call Cancel, wait for the channel to actually close
// before running cleanup, and report the run as timed out.
func TestEnrichContextForSessionTimeoutCancelsAndWaitsForCleanup(t *testing.T) {
	withNonDebugConfig(t)

	var cancelCalls int32
	ch := make(chan agent.AgentEvent)
	cancelFn := func(sessionID string) {
		atomic.AddInt32(&cancelCalls, 1)
		// Simulate the agent's own cancellation path taking a moment to finish
		// its in-flight write before it sends the terminal event and closes.
		go func() {
			time.Sleep(20 * time.Millisecond)
			ch <- agent.AgentEvent{Type: agent.AgentEventTypeError, Error: agent.ErrRequestCancelled}
			close(ch)
		}()
	}

	e, sessions := newTestEnricher(func(sessionID, content string) (<-chan agent.AgentEvent, error) {
		return ch, nil
	}, cancelFn)
	e.timeout = 10 * time.Millisecond
	e.fallbackOff = true

	outcome := e.EnrichContextForSession(context.Background(), "parent-4", "query")

	if !outcome.TimedOut {
		t.Fatal("TimedOut = false, want true")
	}
	if outcome.Block != "" {
		t.Fatalf("Block = %q, want empty (fallback disabled)", outcome.Block)
	}
	if got := atomic.LoadInt32(&cancelCalls); got != 1 {
		t.Fatalf("Cancel called %d times, want 1", got)
	}
	// cleanup() only runs once CollectRunResult's drain returns, i.e. after the
	// channel actually closed — if it ran earlier this would still be present.
	if childID := sessions.childSessionID(); childID != "" {
		t.Fatalf("child session %q was not cleaned up after the drain", childID)
	}
}

// TestEnrichContextForSessionClosedWithoutEvents covers the degenerate case of
// a run channel that closes with no events at all: no panic, a clean error
// that leads to an empty (fallback-disabled) outcome.
func TestEnrichContextForSessionClosedWithoutEvents(t *testing.T) {
	e, _ := newTestEnricher(func(sessionID, content string) (<-chan agent.AgentEvent, error) {
		ch := make(chan agent.AgentEvent)
		close(ch)
		return ch, nil
	}, nil)
	e.fallbackOff = true

	outcome := e.EnrichContextForSession(context.Background(), "parent-5", "query")

	if outcome.Block != "" || outcome.TimedOut {
		t.Fatalf("outcome = %+v, want an empty, non-timed-out result", outcome)
	}
}
