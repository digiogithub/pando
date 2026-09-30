package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/systemone/systemonetest"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
)

// Shared harness of the Auto model mode tests: an in-memory message store,
// scripted per-model providers, a counting tool and a fake System One router.

const (
	autoCoder models.ModelID = "auto-test-coder"
	autoImpl  models.ModelID = "auto-test-impl"
	autoPlan  models.ModelID = "auto-test-plan"
	autoFb1   models.ModelID = "auto-test-fb1"
	autoFb2   models.ModelID = "auto-test-fb2"
	autoTask  models.ModelID = "auto-test-task"
)

// autoMemMessages is an in-memory message.Service.
type autoMemMessages struct {
	*pubsub.Broker[message.Message]
	mu   sync.Mutex
	msgs []message.Message
	seq  int
}

func newAutoMemMessages() *autoMemMessages {
	return &autoMemMessages{Broker: pubsub.NewBroker[message.Message]()}
}

func (m *autoMemMessages) Create(_ context.Context, sessionID string, p message.CreateMessageParams) (message.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	msg := message.Message{
		ID: fmt.Sprintf("m%d", m.seq), SessionID: sessionID, Role: p.Role, Parts: p.Parts,
		Model: p.Model, CreatedAt: int64(m.seq), UpdatedAt: int64(m.seq),
	}
	m.msgs = append(m.msgs, msg)
	return msg, nil
}

func (m *autoMemMessages) Update(_ context.Context, msg message.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.msgs {
		if m.msgs[i].ID == msg.ID {
			m.msgs[i] = msg
			return nil
		}
	}
	return fmt.Errorf("message %s not found", msg.ID)
}

func (m *autoMemMessages) Get(_ context.Context, id string) (message.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.msgs {
		if msg.ID == id {
			return msg, nil
		}
	}
	return message.Message{}, fmt.Errorf("not found")
}

func (m *autoMemMessages) List(_ context.Context, sessionID string) ([]message.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []message.Message
	for _, msg := range m.msgs {
		if msg.SessionID == sessionID {
			out = append(out, msg)
		}
	}
	return out, nil
}

func (m *autoMemMessages) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.msgs {
		if m.msgs[i].ID == id {
			m.msgs = append(m.msgs[:i], m.msgs[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *autoMemMessages) DeleteSessionMessages(context.Context, string) error { return nil }

// assistantModels returns the model of every stored assistant message of a
// session, in order.
func (m *autoMemMessages) assistantModels(sessionID string) []models.ModelID {
	msgs, _ := m.List(context.Background(), sessionID)
	var out []models.ModelID
	for _, msg := range msgs {
		if msg.Role == message.Assistant {
			out = append(out, msg.Model)
		}
	}
	return out
}

// scriptedProvider is a provider.Provider whose answers come from a script.
type scriptedProvider struct {
	model models.Model

	mu        sync.Mutex
	calls     int
	histories [][]message.Message
	starts    []time.Time
	respond   func(call int, history []message.Message) []provider.ProviderEvent
	// gate, when set, blocks every response until closed.
	gate chan struct{}
	// started receives one value per call, when set.
	started chan struct{}
}

func (p *scriptedProvider) SendMessages(context.Context, []message.Message, []tools.BaseTool) (*provider.ProviderResponse, error) {
	return &provider.ProviderResponse{}, nil
}

func (p *scriptedProvider) Model() models.Model { return p.model }

func (p *scriptedProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *scriptedProvider) history(i int) []message.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.histories[i]
}

func (p *scriptedProvider) StreamResponse(_ context.Context, msgs []message.Message, _ []tools.BaseTool) <-chan provider.ProviderEvent {
	p.mu.Lock()
	call := p.calls
	p.calls++
	p.histories = append(p.histories, append([]message.Message(nil), msgs...))
	p.starts = append(p.starts, time.Now())
	respond, gate, started := p.respond, p.gate, p.started
	p.mu.Unlock()

	ch := make(chan provider.ProviderEvent, 16)
	go func() {
		defer close(ch)
		if started != nil {
			started <- struct{}{}
		}
		if gate != nil {
			<-gate
		}
		var events []provider.ProviderEvent
		if respond != nil {
			events = respond(call, msgs)
		} else {
			events = textReply("ok from " + string(p.model.ID))
		}
		for _, ev := range events {
			ch <- ev
		}
	}()
	return ch
}

func textReply(text string) []provider.ProviderEvent {
	return []provider.ProviderEvent{
		{Type: provider.EventContentDelta, Content: text},
		{Type: provider.EventComplete, Response: &provider.ProviderResponse{
			Content: text, FinishReason: message.FinishReasonEndTurn,
		}},
	}
}

func reasoningReply(text string) []provider.ProviderEvent {
	return append([]provider.ProviderEvent{{Type: provider.EventThinkingDelta, Thinking: "thinking hard"}}, textReply(text)...)
}

func toolReply(id, name string) []provider.ProviderEvent {
	call := message.ToolCall{ID: id, Name: name, Input: `{}`, Type: "function", Finished: true}
	return []provider.ProviderEvent{
		{Type: provider.EventComplete, Response: &provider.ProviderResponse{
			ToolCalls: []message.ToolCall{call}, FinishReason: message.FinishReasonToolUse,
		}},
	}
}

func errorReply(err error) []provider.ProviderEvent {
	return []provider.ProviderEvent{{Type: provider.EventError, Error: err}}
}

// countingTool counts executions.
type countingTool struct {
	mu    sync.Mutex
	count int
}

func (c *countingTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: "echo", Description: "test tool", Parameters: map[string]any{}}
}

func (c *countingTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
	return tools.NewTextResponse("echoed"), nil
}

func (c *countingTool) runs() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

// autoEnv bundles an agent wired to scripted providers and a fake router.
type autoEnv struct {
	t     *testing.T
	a     *agent
	msgs  *autoMemMessages
	srv   *systemonetest.Server
	tool  *countingTool
	provs map[models.ModelID]*scriptedProvider

	mu      sync.Mutex
	retries map[models.ModelID][]int
}

type autoEnvOption func(*config.Config)

func withAutoConfig(fn func(*config.ModelAutoModeConfig)) autoEnvOption {
	return func(c *config.Config) { fn(&c.ModelAutoMode) }
}

func newAutoEnv(t *testing.T, opts ...autoEnvOption) *autoEnv {
	t.Helper()
	srv := systemonetest.NewOllama035(t)

	catalogue := []models.ModelID{autoCoder, autoImpl, autoPlan, autoFb1, autoFb2, autoTask}
	env := &autoEnv{
		t: t, srv: srv, msgs: newAutoMemMessages(), tool: &countingTool{},
		provs: map[models.ModelID]*scriptedProvider{}, retries: map[models.ModelID][]int{},
	}
	for _, id := range catalogue {
		m := models.Model{
			ID: id, Name: string(id), Provider: models.ProviderMock,
			ContextWindow: 100_000, SupportsAttachments: true,
		}
		models.SetSupportedModel(m)
		env.provs[id] = &scriptedProvider{model: m}
	}

	cfg := &config.Config{
		WorkingDir: t.TempDir(),
		Agents: map[config.AgentName]config.Agent{
			config.AgentCoder: {Model: autoCoder},
			config.AgentTask:  {Model: autoTask},
		},
		ProviderAccounts: []config.ProviderAccount{{ID: "mock-1", Type: models.ProviderMock, APIKey: "key"}},
		Providers:        map[models.ModelProvider]config.Provider{},
		LSP:              map[string]config.LSPConfig{},
		ModelAutoMode: config.ModelAutoModeConfig{
			Enabled:     true,
			DefaultAuto: true,
			Router: config.DecisionRouterConfig{
				Provider: config.DecisionProviderOllama, BaseURL: srv.URL, Model: "tev1:0.8b",
			},
			Threshold: 0.6,
			TimeoutMs: 2000,
			Routes: []config.ModelAutoRoute{
				{ID: "implementation", Description: "write or change code", Model: autoImpl,
					Fallbacks: []models.ModelID{autoFb1, autoFb2}},
				{ID: "planning", Description: "plan or design", Model: autoPlan},
			},
		},
	}
	for _, o := range opts {
		o(cfg)
	}
	prev := config.Get()
	config.SetForTests(cfg)

	providerBuilderHook = func(model models.Model, retries int) (provider.Provider, bool) {
		env.mu.Lock()
		env.retries[model.ID] = append(env.retries[model.ID], retries)
		env.mu.Unlock()
		p, ok := env.provs[model.ID]
		return p, ok
	}
	resetAutoWarnings()
	resetAutoCooldowns()

	env.a = &agent{
		Broker:            pubsub.NewBroker[AgentEvent](),
		sessions:          newResumeStubSessions(),
		messages:          env.msgs,
		agentName:         config.AgentCoder,
		provider:          env.provs[autoCoder],
		tools:             []tools.BaseTool{env.tool},
		runStatusMessages: make(map[string][]string),
		steeringQueue:     make(map[string][]steeringMessage),
		resurrectCount:    make(map[string]int),
	}

	t.Cleanup(func() {
		waitForRunsOrFail(t, env.a)
		providerBuilderHook = nil
		autoClock = time.Now
		resetAutoWarnings()
		resetAutoCooldowns()
		config.SetForTests(prev)
		for _, id := range catalogue {
			models.DeleteSupportedModels(id)
		}
	})
	return env
}

// session registers cleanup for a session id and seeds one finished exchange
// answered by the given model, so the turn is not the first of the session (no
// title generation) and history hygiene has something to act on.
func (e *autoEnv) session(id string, seedModel models.ModelID) string {
	e.t.Helper()
	e.t.Cleanup(func() {
		sessionLLMOverrides.Delete(id)
		lastRoutings.Delete(id)
		autoTurns.Delete(id)
	})
	ctx := context.Background()
	_, _ = e.msgs.Create(ctx, id, message.CreateMessageParams{
		Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "earlier question"}},
	})
	_, _ = e.msgs.Create(ctx, id, message.CreateMessageParams{
		Role: message.Assistant, Model: seedModel,
		Parts: []message.ContentPart{
			message.ReasoningContent{Thinking: "old reasoning"},
			message.TextContent{Text: "earlier answer"},
		},
	})
	return id
}

// decide configures the fake router's answer.
func (e *autoEnv) decide(choice string, p float64) {
	probs := map[string]float64{choice: p, "none": 1 - p}
	if choice == "none" {
		probs = map[string]float64{"none": p, "implementation": 1 - p}
	}
	e.srv.SetDecision(choice, probs)
}

// run sends a prompt and returns every event of the run.
func (e *autoEnv) run(sessionID, prompt string) []AgentEvent {
	e.t.Helper()
	ch, err := e.a.Run(context.Background(), sessionID, prompt)
	if err != nil {
		e.t.Fatalf("Run: %v", err)
	}
	return drainAutoEvents(e.t, ch)
}

func drainAutoEvents(t *testing.T, ch <-chan AgentEvent) []AgentEvent {
	t.Helper()
	var events []AgentEvent
	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
		case <-timeout:
			t.Fatal("run did not finish")
		}
	}
}

func finalEvent(events []AgentEvent) AgentEvent {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == AgentEventTypeResponse || events[i].Type == AgentEventTypeError {
			return events[i]
		}
	}
	return AgentEvent{}
}

// notices returns the SystemMessage text of every system_message event.
func notices(events []AgentEvent) []string {
	var out []string
	for _, ev := range events {
		if ev.Type == AgentEventTypeSystemMessage && strings.TrimSpace(ev.SystemMessage) != "" {
			out = append(out, strings.TrimSpace(ev.SystemMessage))
		}
	}
	return out
}

func autoNotices(events []AgentEvent) []string {
	var out []string
	for _, n := range notices(events) {
		if strings.HasPrefix(n, "Auto:") {
			out = append(out, n)
		}
	}
	return out
}

func (e *autoEnv) routerCalls() int { return e.srv.Count("/v1/systemone") }

func (e *autoEnv) retriesFor(id models.ModelID) []int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]int(nil), e.retries[id]...)
}
