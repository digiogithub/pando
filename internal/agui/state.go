package agui

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/agent"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/tools"
)

// The shared-state document (P2).
//
// AG-UI carries an arbitrary JSON document alongside the message stream:
// STATE_SNAPSHOT publishes it in full, STATE_DELTA patches it with RFC-6902
// operations. CopilotKit exposes it to the page through useCoAgent /
// useCoAgentStateRender, which is how a Generative-UI frontend renders a todo
// list or a token gauge without parsing chat text.
//
// The document is adapter-owned and derived only from events Pando already
// broadcasts, so nothing in the agent had to change to produce it. It is scoped
// to one thread and rebuilt from scratch when the adapter restarts: it is a
// projection for rendering, never a source of truth.

// ModelState describes the model backing the thread.
type ModelState struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Provider      string `json:"provider,omitempty"`
	ContextWindow int64  `json:"contextWindow,omitempty"`
}

// TokenUsageState is the live context budget, mirroring agent.TokenUsageInfo in
// the protocol's camelCase convention.
type TokenUsageState struct {
	PromptTokens     int64   `json:"promptTokens"`
	CompletionTokens int64   `json:"completionTokens"`
	ContextWindow    int64   `json:"contextWindow"`
	Estimated        bool    `json:"estimated"`
	CacheReadTokens  int64   `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64   `json:"cacheWriteTokens,omitempty"`
	ReasoningTokens  int64   `json:"reasoningTokens,omitempty"`
	Cost             float64 `json:"cost,omitempty"`
}

// FileState is one workspace file the run touched.
type FileState struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Action string `json:"action"`
}

// File actions reported in StateDoc.Files.
const (
	FileActionRead    = "read"
	FileActionWrite   = "write"
	FileActionEdit    = "edit"
	FileActionPatched = "patch"
)

// fileToolActions maps the Pando tools whose target file is worth surfacing to
// the page. Anything not listed here is ignored, so the file list stays a short,
// meaningful summary instead of a log.
var fileToolActions = map[string]string{
	"view":       FileActionRead,
	"write":      FileActionWrite,
	"edit":       FileActionEdit,
	"multiedit":  FileActionEdit,
	"patch":      FileActionPatched,
	"image_crop": FileActionRead,
}

// StateDoc is the document published to the client. Field order is irrelevant to
// the protocol; the JSON pointer paths used by the deltas are what matter, so
// the tags below are part of the contract.
type StateDoc struct {
	Thread     string           `json:"thread"`
	Session    string           `json:"session"`
	Agent      string           `json:"agent"`
	Model      ModelState       `json:"model"`
	Todos      []tools.TodoItem `json:"todos"`
	TokenUsage *TokenUsageState `json:"tokenUsage"`
	Files      []FileState      `json:"files"`
	// SubAgents lists the mesnada tasks this thread delegated (P7). See
	// subagents.go: it is derived from the mesnada_* tool traffic, never read
	// from the orchestrator.
	SubAgents []SubAgentState `json:"subAgents"`
	// Client echoes RunAgentInput.state. It is read-only context the page pushed
	// into the run; the adapter never writes it back into Pando's config.
	Client any `json:"client,omitempty"`
}

// stateTracker owns one thread's state document and turns Pando events into the
// deltas that keep the client's copy in sync.
type stateTracker struct {
	mu  sync.Mutex
	doc StateDoc

	// toolInputs remembers a call's arguments so a tool result can be attributed
	// to a file even when the result carries no input snapshot.
	toolInputs map[string]toolInvocation
	// filesSeen deduplicates the file list, keeping the last action per path.
	filesSeen map[string]int
	// subAgentIndex maps a mesnada task id to its position in doc.SubAgents.
	subAgentIndex map[string]int
}

type toolInvocation struct {
	name  string
	input string
}

func newStateTracker(threadID, sessionID string, agentName config.AgentName, model models.Model, clientState any) *stateTracker {
	return &stateTracker{
		doc: StateDoc{
			Thread:  threadID,
			Session: sessionID,
			Agent:   string(agentName),
			Model: ModelState{
				ID:            string(model.ID),
				Name:          model.Name,
				Provider:      string(model.Provider),
				ContextWindow: model.ContextWindow,
			},
			Todos:     []tools.TodoItem{},
			Files:     []FileState{},
			SubAgents: []SubAgentState{},
			Client:    clientState,
		},
		toolInputs:    make(map[string]toolInvocation),
		filesSeen:     make(map[string]int),
		subAgentIndex: make(map[string]int),
	}
}

// rebind points an existing document at a new run. The document is per thread,
// but the model may have changed between turns and the client may have sent a
// new client-owned state blob with the request.
func (s *stateTracker) rebind(model models.Model, clientState any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doc.Model = ModelState{
		ID:            string(model.ID),
		Name:          model.Name,
		Provider:      string(model.Provider),
		ContextWindow: model.ContextWindow,
	}
	if clientState != nil {
		s.doc.Client = clientState
	}
}

// session reports which Pando session the document was built for.
func (s *stateTracker) session() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doc.Session
}

// stateStore keeps one state document per thread.
//
// The document must outlive the run that produced it: a conversation's todo
// list, touched files and delegated sub-agents are properties of the thread, and
// a client that only ever saw the current turn's snapshot would watch them
// vanish on every new message. Documents are memory-only (the thread->session
// binding in threads.go is the durable part), so a restart empties the board.
type stateStore struct {
	mu       sync.Mutex
	byThread map[string]*stateTracker
	order    []string
}

// maxStateThreads bounds the store. Threads are long-lived and documents are
// small, but nothing else evicts them, so the least recently used ones go.
const maxStateThreads = 256

func newStateStore() *stateStore {
	return &stateStore{byThread: make(map[string]*stateTracker)}
}

// get returns the thread's document, creating it on first contact. A document
// built for a different session is discarded: the thread was rebound and its
// files and sub-agents belong to a conversation that no longer exists.
func (s *stateStore) get(threadID, sessionID string, agentName config.AgentName, model models.Model, clientState any) *stateTracker {
	s.mu.Lock()
	defer s.mu.Unlock()

	if tracker, ok := s.byThread[threadID]; ok {
		if tracker.session() == sessionID {
			tracker.rebind(model, clientState)
			s.touchLocked(threadID)
			return tracker
		}
		delete(s.byThread, threadID)
	}

	tracker := newStateTracker(threadID, sessionID, agentName, model, clientState)
	s.byThread[threadID] = tracker
	s.touchLocked(threadID)
	s.evictLocked()
	return tracker
}

func (s *stateStore) touchLocked(threadID string) {
	for i, id := range s.order {
		if id == threadID {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	s.order = append(s.order, threadID)
}

func (s *stateStore) evictLocked() {
	for len(s.order) > maxStateThreads {
		delete(s.byThread, s.order[0])
		s.order = s.order[1:]
	}
}

// Snapshot returns the STATE_SNAPSHOT event for the current document. It is sent
// once per run, right after RUN_STARTED, so a client that joined late (or
// resumed an interrupted run) starts from a known state.
func (s *stateTracker) Snapshot() Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return NewStateSnapshot(s.copyLocked())
}

// Observe folds one Pando event into the document and returns the STATE_DELTA
// events describing what changed. It returns nil when nothing changed, so a
// stream of identical token-usage estimates does not become a stream of empty
// patches.
func (s *stateTracker) Observe(ev agent.AgentEvent) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch ev.Type {
	case agent.AgentEventTypeTodosUpdated:
		todos := ev.Todos
		if todos == nil {
			todos = []tools.TodoItem{}
		}
		if sameJSON(s.doc.Todos, todos) {
			return nil
		}
		s.doc.Todos = todos
		return []Event{NewStateDelta([]JSONPatchOperation{
			{Op: "replace", Path: "/todos", Value: todos},
		})}

	case agent.AgentEventTypeTokenUsage:
		if ev.TokenUsage == nil {
			return nil
		}
		usage := &TokenUsageState{
			PromptTokens:     ev.TokenUsage.PromptTokens,
			CompletionTokens: ev.TokenUsage.CompletionTokens,
			ContextWindow:    ev.TokenUsage.ContextWindow,
			Estimated:        ev.TokenUsage.Estimated,
			CacheReadTokens:  ev.TokenUsage.CacheReadTokens,
			CacheWriteTokens: ev.TokenUsage.CacheCreationTokens,
			ReasoningTokens:  ev.TokenUsage.ReasoningTokens,
			Cost:             ev.TokenUsage.Cost,
		}
		if sameJSON(s.doc.TokenUsage, usage) {
			return nil
		}
		s.doc.TokenUsage = usage
		return []Event{NewStateDelta([]JSONPatchOperation{
			{Op: "replace", Path: "/tokenUsage", Value: usage},
		})}

	case agent.AgentEventTypeToolCall:
		if ev.ToolCall != nil && ev.ToolCall.Finished {
			s.toolInputs[ev.ToolCall.ID] = toolInvocation{name: ev.ToolCall.Name, input: ev.ToolCall.Input}
		}
		return nil

	case agent.AgentEventTypeToolResult:
		return s.observeToolResultLocked(ev)
	}
	return nil
}

// observeToolResultLocked appends or updates the file entry a successful
// file-touching tool produced.
func (s *stateTracker) observeToolResultLocked(ev agent.AgentEvent) []Event {
	res := ev.ToolResult
	if res == nil || res.IsError {
		return nil
	}
	name, input := res.Name, res.Input
	if prior, ok := s.toolInputs[res.ToolCallID]; ok {
		if name == "" {
			name = prior.name
		}
		if input == "" {
			input = prior.input
		}
	}
	delete(s.toolInputs, res.ToolCallID)

	// Delegation tools describe sub-agents, never files.
	if events, handled := s.observeMesnadaLocked(name, input, res.Content); handled {
		return events
	}

	action, ok := fileToolActions[strings.ToLower(name)]
	if !ok {
		return nil
	}
	path := filePathFromToolInput(input)
	if path == "" {
		return nil
	}

	entry := FileState{Path: path, Name: filepath.Base(path), Action: action}
	if idx, seen := s.filesSeen[path]; seen {
		if s.doc.Files[idx].Action == action {
			return nil
		}
		s.doc.Files[idx] = entry
		return []Event{NewStateDelta([]JSONPatchOperation{
			{Op: "replace", Path: jsonPointerIndex("/files", idx), Value: entry},
		})}
	}
	s.filesSeen[path] = len(s.doc.Files)
	s.doc.Files = append(s.doc.Files, entry)
	return []Event{NewStateDelta([]JSONPatchOperation{
		{Op: "add", Path: "/files/-", Value: entry},
	})}
}

// copyLocked returns a shallow copy safe to hand to the JSON encoder while the
// tracker keeps mutating. The slices are copied into non-nil destinations so an
// empty document serializes as [] and stays a valid target for an `add` patch —
// a null would make the client's patch library fail on the first delta.
func (s *stateTracker) copyLocked() StateDoc {
	out := s.doc
	out.Todos = make([]tools.TodoItem, len(s.doc.Todos))
	copy(out.Todos, s.doc.Todos)
	out.Files = make([]FileState, len(s.doc.Files))
	copy(out.Files, s.doc.Files)
	out.SubAgents = make([]SubAgentState, len(s.doc.SubAgents))
	copy(out.SubAgents, s.doc.SubAgents)
	return out
}

// filePathFromToolInput pulls the file argument out of a tool call's JSON input.
// Pando's file tools all name it file_path; a couple of MCP-provided ones use
// path, so both are accepted.
func filePathFromToolInput(input string) string {
	if input == "" {
		return ""
	}
	var params struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return ""
	}
	if params.FilePath != "" {
		return params.FilePath
	}
	return params.Path
}

// jsonPointerIndex builds an RFC-6901 pointer to an array element.
func jsonPointerIndex(base string, idx int) string {
	return base + "/" + strconv.Itoa(idx)
}

// sameJSON reports whether two values serialize identically. Used to suppress
// no-op deltas without hand-writing a comparison per field.
func sameJSON(a, b any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}
