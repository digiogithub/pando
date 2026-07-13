package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/prompt"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/ponytail"
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/runtime"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/skills"
	"github.com/digiogithub/pando/internal/superpowers"
)

type cleanModeContextKey struct{}

type cleanModeCatalogTool struct{}

func isCleanModeContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(cleanModeContextKey{}).(bool)
	return enabled
}

func (cleanModeCatalogTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name:        "list_mcp_catalog",
		Description: "Return the MCP catalog listing as normally exposed by the prompt builder. This tool takes no arguments.",
		Parameters:  map[string]any{},
		Required:    []string{},
	}
}

func (cleanModeCatalogTool) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	_ = params
	return tools.NewTextResponse(promptMcpCatalogListing(ctx)), nil
}

// Common errors
var (
	ErrRequestCancelled = errors.New("request cancelled by user")
	ErrSessionBusy      = errors.New("session is currently processing another request")
	// ErrSessionNotBusy is returned by Steer when the session has no active run to
	// steer. Callers should fall back to a normal Run in that case.
	ErrSessionNotBusy = errors.New("session is not currently processing a request")
)

// ContextEnricher is the interface used by the agent to enrich the user's prompt with
// context retrieved from the KB and code index before sending it to the LLM.
// A local interface is used to avoid import cycles between agent and rag packages.
type ContextEnricher interface {
	EnrichContext(ctx context.Context, query string) string
}

// globalContextEnricher is the package-level enricher injected from app.go.
var globalContextEnricher ContextEnricher

// SetContextEnricher sets the context enricher used to prepend KB/code context to user messages.
// Pass nil to disable context enrichment.
func SetContextEnricher(e ContextEnricher) {
	globalContextEnricher = e
}

// trimmedToolsKey is the context key used to store the pre-computed list of relevant tool names
// produced by the ContextTrimmer at session start.
type trimmedToolsKey struct{}

// globalContextTrimmer is the optional pre-session context trimmer injected from app.go.
var globalContextTrimmer ContextTrimmer

// SetContextTrimmer sets the context trimmer used to filter the tool list for new sessions.
// Pass nil to disable context trimming (all tools will always be included).
func SetContextTrimmer(ct ContextTrimmer) {
	globalContextTrimmer = ct
}

// MemoryInjector injects a <memories> block into the system prompt before each turn.
// It is a separate interface from ContextEnricher so memory enrichment can be enabled
// independently of the main context-enrichment pipeline.
type MemoryInjector interface {
	BuildMemoryBlock(ctx context.Context, query string) string
}

// globalMemoryInjector is injected from app.go when MemoryContextEnrichmentEnabled is true.
var globalMemoryInjector MemoryInjector

// SetMemoryInjector wires the memory injector used to prepend a <memories> block to the
// system prompt. Pass nil to disable memory injection.
func SetMemoryInjector(m MemoryInjector) {
	globalMemoryInjector = m
}

// globalNonInteractive indicates that Pando is running in non-interactive CLI mode (-p flag).
// When true, the system prompt instructs agents to act autonomously without requesting user input.
var globalNonInteractive bool

// SetNonInteractiveMode configures all agents to run autonomously without waiting for user input.
// Call this before running a session when a prompt is provided via the -p flag or stdin pipe.
func SetNonInteractiveMode(enabled bool) {
	globalNonInteractive = enabled
}

// nonInteractiveInstructions are appended to the system prompt when running in non-interactive mode.
const nonInteractiveInstructions = `
# Non-Interactive Mode
You are running in non-interactive mode (prompt supplied via -p flag or stdin). There is NO user present to answer questions or provide feedback during execution.

Rules you MUST follow:
- Complete the requested task autonomously without asking for clarification or confirmation.
- Make reasonable assumptions when information is ambiguous; document your assumptions in the output.
- NEVER pause, prompt, or wait for user input at any point.
- Exception — stop and report (do NOT proceed) only if the task explicitly or implicitly requires a DESTRUCTIVE action (permanent deletion of files/data, formatting/wiping storage, dropping databases) that is NOT clearly described or implied in the original prompt. In that case, explain what you cannot do safely and exit.
- Once the task is complete, produce a concise summary of what was done and terminate.`

type AgentEventType string

const (
	AgentEventTypeError         AgentEventType = "error"
	AgentEventTypeResponse      AgentEventType = "response"
	AgentEventTypeSummarize     AgentEventType = "summarize"
	AgentEventTypeContentDelta  AgentEventType = "content_delta"
	AgentEventTypeThinkingDelta AgentEventType = "thinking_delta"
	AgentEventTypeToolCall      AgentEventType = "tool_call"
	AgentEventTypeToolResult    AgentEventType = "tool_result"
	// AgentEventTypeTodosUpdated is emitted when the TodoWrite tool runs successfully.
	// It carries the current todo list for non-ACP consumers (TUI, WebUI).
	AgentEventTypeTodosUpdated AgentEventType = "todos_updated"
	// AgentEventTypeSystemMessage carries internal status messages (context compaction,
	// retries, etc.) that should be displayed to the user but are not part of the
	// LLM response. Unlike ContentDelta these are sent with a blocking channel write
	// so they are never silently dropped.
	AgentEventTypeSystemMessage AgentEventType = "system_message"
	// AgentEventTypeTokenUsage carries a live context-window token update so the
	// TUI/WebUI can show consumption as it grows during the agent loop (e.g. while
	// tools execute or a file is produced), not only when the LLM response finishes.
	// When TokenUsage.Estimated is true the value is a heuristic estimate that the
	// next confirmed usage (EventComplete) will reconcile.
	AgentEventTypeTokenUsage AgentEventType = "token_usage"
	// AgentEventTypeSteeringQueued is emitted when the user submits a steering
	// message (mid-run feedback) that is queued for injection at the next safe
	// boundary of the agent loop. SystemMessage carries a human-readable note.
	AgentEventTypeSteeringQueued AgentEventType = "steering_queued"
	// AgentEventTypeSteeringInjected is emitted when one or more queued steering
	// messages have been injected into the conversation and the loop will continue
	// taking them into account. SystemMessage carries a human-readable note.
	AgentEventTypeSteeringInjected AgentEventType = "steering_injected"
	// AgentEventTypeConclusionQueued is emitted when a delegated-task conclusion
	// is queued (via InjectConclusion) for injection into a still-running parent
	// loop at the next safe boundary. SystemMessage carries a human-readable note.
	AgentEventTypeConclusionQueued AgentEventType = "conclusion_queued"
	// AgentEventTypeConclusionInjected is emitted when one or more queued delegated
	// conclusions have been injected into the conversation so the parent loop can
	// react to the subagent's result. SystemMessage carries a human-readable note.
	AgentEventTypeConclusionInjected AgentEventType = "conclusion_injected"
	// AgentEventTypeResurrected is emitted at the start of a system-initiated run
	// that resumes an idle parent session because one or more delegated tasks
	// finished (Case B of the delegation protocol). It lets the UI frame the turn
	// as "resuming because a delegated task reported its result" rather than as a
	// user message. SystemMessage carries a human-readable note.
	AgentEventTypeResurrected AgentEventType = "resurrected"
)

// TokenUsageInfo carries a live token-usage snapshot for AgentEventTypeTokenUsage.
// PromptTokens/CompletionTokens are cumulative for the session (same semantics as
// session.Session), ContextWindow is the effective window for the active model, and
// Estimated marks the value as provisional (not yet confirmed by the provider).
type TokenUsageInfo struct {
	PromptTokens     int64
	CompletionTokens int64
	ContextWindow    int64
	Estimated        bool
}

type AgentEvent struct {
	Type       AgentEventType
	Message    message.Message
	Error      error
	Delta      string
	ToolCall   *message.ToolCall
	ToolResult *message.ToolResult

	// When summarizing
	SessionID string
	Progress  string
	Done      bool

	// Todos is populated when Type == AgentEventTypeTodosUpdated.
	Todos []tools.TodoItem

	// SystemMessage is populated when Type == AgentEventTypeSystemMessage.
	// It carries a human-readable status message (context compaction, retries, etc.)
	// that should be shown to the user regardless of the transport (TUI, ACP, web).
	SystemMessage string

	// TokenUsage is populated when Type == AgentEventTypeTokenUsage.
	TokenUsage *TokenUsageInfo
}

const (
	summaryOutputReservationTokens = int64(2048)
	summaryToolOverheadTokens      = int64(512)
	summaryMinInputBudgetTokens    = int64(1024)
	continuationMarkerTemplate     = "The previous conversation was interrupted while compacting context. Resume the unfinished work using this summary before continuing:\n\n%s"
)

type summaryMode int

const (
	summaryModeManual summaryMode = iota
	summaryModeCompaction
)

type summaryResult struct {
	message      message.Message
	text         string
	model        models.Model
	usage        provider.TokenUsage
	usedFallback bool
}

type Service interface {
	pubsub.Suscriber[AgentEvent]
	Model() models.Model
	Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error)
	LastRunSystemMessages(sessionID string) []string
	Cancel(sessionID string)
	// Steer queues a mid-run feedback message for the given session. It is injected
	// into the conversation at the next safe boundary of the agent loop (after the
	// current iteration's tool results, or at the end of the current turn) without
	// cancelling the run. Returns ErrSessionNotBusy if there is no active run.
	Steer(sessionID string, content string, attachments ...message.Attachment) error
	// PendingSteering reports how many steering messages are queued for the session.
	PendingSteering(sessionID string) int
	// InjectConclusion queues a delegated-task conclusion for injection into a
	// still-running parent loop at the next safe boundary (Case A of the delegation
	// protocol). The content must already be the fully formatted text the parent
	// will see — the agent does not re-frame it. Behaviour mirrors Steer but the
	// message is typed as a conclusion (distinct UI framing) and it carries no
	// attachments. Returns ErrSessionNotBusy when the session is idle; the
	// supervisor uses that signal to fall back to resurrection (a later phase).
	InjectConclusion(sessionID string, content string) error
	// Resume starts a NEW system-initiated run for an IDLE session (Case B of the
	// delegation protocol). The content is the pre-framed resurrection text the
	// supervisor built. It is distinct from Run (user-initiated) and from
	// InjectConclusion (live-loop steering): events still reach UIs via the pubsub
	// broker, but the returned run channel is drained internally so the caller does
	// not have to. Returns ErrSessionBusy if a run is already active. Each Resume
	// increments the session's resurrection counter (see ResurrectionCount); a
	// user-initiated Run resets it.
	Resume(ctx context.Context, sessionID string, content string) error
	// ResurrectionCount reports how many times the session has been resurrected via
	// Resume since the last user-initiated Run. The supervisor reads it to enforce
	// the MaxResurrections cap; the count auto-resets whenever the user sends a new
	// manual message (Run) and is cleared on Cancel.
	ResurrectionCount(sessionID string) int
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error)
	Summarize(ctx context.Context, sessionID string) error
	SetLuaManager(fm *luaengine.FilterManager)
	// GetTools returns the tools available to this agent instance.
	GetTools() []tools.BaseTool
}

type agent struct {
	*pubsub.Broker[AgentEvent]
	sessions session.Service
	messages message.Service

	tools    []tools.BaseTool
	provider provider.Provider

	titleProvider             provider.Provider
	summarizeProvider         provider.Provider
	summarizeFallbackProvider provider.Provider
	agentName                 config.AgentName
	skillManager              *skills.SkillManager
	contextManager            *skills.ContextManager
	luaMgr                    *luaengine.FilterManager

	activeRequests sync.Map
	// toolCallThrottle debounces AgentEventTypeToolCall emissions during
	// EventToolUseDelta. It maps toolCallID → time.Time of the last emitted
	// event, preventing the ACP event channel from being flooded while still
	// giving the frontend periodic enriched updates (rawInput, title, etc.)
	// as the provider streams the tool-call input JSON.
	toolCallThrottle  sync.Map
	runStatusMu       sync.Mutex
	runStatusMessages map[string][]string

	// steeringMu guards steeringQueue. steeringQueue holds mid-run feedback
	// messages, keyed by sessionID, that are injected into the conversation at the
	// next safe boundary of the agent loop (see drainSteeringInto).
	steeringMu    sync.Mutex
	steeringQueue map[string][]steeringMessage

	// resurrectMu guards resurrectCount. resurrectCount tracks how many times each
	// session has been resurrected (Case B) since the last user-initiated Run. The
	// supervisor reads it via ResurrectionCount to enforce MaxResurrections; Resume
	// increments it, Run resets it to 0, and Cancel clears the entry.
	resurrectMu    sync.Mutex
	resurrectCount map[string]int
}

// steeringKind classifies a queued out-of-band message. The same inbox carries
// both user feedback and delegated-task conclusions so they can be drained at the
// same safe boundaries; the kind drives the UI framing on injection.
type steeringKind int

const (
	// steeringFeedback is a user-submitted mid-run feedback message (the original,
	// always-on steering path). It is the zero value so existing code is unchanged.
	steeringFeedback steeringKind = iota
	// steeringConclusion is a delegated-task conclusion injected into a live parent
	// loop (Case A of the delegation protocol).
	steeringConclusion
)

// steeringMessage is a queued mid-run message awaiting injection. It is either
// user feedback (kind=steeringFeedback) or a delegated-task conclusion
// (kind=steeringConclusion).
type steeringMessage struct {
	kind        steeringKind
	content     string
	attachments []message.Attachment
}

func NewAgent(
	agentName config.AgentName,
	sessions session.Service,
	messages message.Service,
	agentTools []tools.BaseTool,
	skillManager *skills.SkillManager,
) (Service, error) {
	agentProvider, err := createAgentProvider(context.Background(), agentName, agentTools, skillManager, nil)
	if err != nil {
		// If the model is not yet available (e.g. dynamic models not fetched yet),
		// create the agent without a provider. The TUI will prompt the user to select a model.
		logging.Warn("Agent provider not available, model selection required", "agent", agentName, "error", err)
		agentProvider = nil
	}

	var titleProvider provider.Provider
	// Only generate titles for the coder agent
	if agentName == config.AgentCoder {
		titleProvider, err = createAgentProvider(context.Background(), config.AgentTitle, nil, nil, nil)
		if err != nil {
			logging.Debug("Title agent provider not available", "error", err)
			titleProvider = nil
		}
	}
	var summarizeProvider provider.Provider
	if agentName == config.AgentCoder {
		summarizeProvider, err = createAgentProvider(context.Background(), config.AgentSummarizer, nil, nil, nil)
		if err != nil {
			logging.Debug("Summarizer agent provider not available", "error", err)
			summarizeProvider = nil
		}
	}

	var contextManager *skills.ContextManager
	if skillManager != nil && agentProvider != nil && (agentName == config.AgentCoder || agentName == config.AgentTask) {
		contextManager = skills.NewContextManager(skillManager, effectiveMaxTokens(agentName, agentProvider.Model()))
	}

	modelID := models.ModelID("")
	if agentProvider != nil {
		modelID = agentProvider.Model().ID
	}

	agent := &agent{
		Broker:                    pubsub.NewBroker[AgentEvent](),
		provider:                  agentProvider,
		messages:                  messages,
		sessions:                  sessions,
		tools:                     agentTools,
		titleProvider:             titleProvider,
		summarizeProvider:         summarizeProvider,
		summarizeFallbackProvider: agentProvider,
		agentName:                 agentName,
		skillManager:              skillManager,
		contextManager:            contextManager,
		activeRequests:            sync.Map{},
		runStatusMessages:         make(map[string][]string),
		steeringQueue:             make(map[string][]steeringMessage),
		resurrectCount:            make(map[string]int),
	}

	logging.Debug("Agent created", "name", string(agentName), "model", modelID, "toolCount", len(agentTools))
	return agent, nil
}

func (a *agent) Model() models.Model {
	if a.provider == nil {
		return models.Model{}
	}
	return a.provider.Model()
}

func (a *agent) GetTools() []tools.BaseTool {
	return a.tools
}

func (a *agent) SetLuaManager(fm *luaengine.FilterManager) {
	a.luaMgr = fm
	globalLuaManagerForTools = fm
}

func (a *agent) Cancel(sessionID string) {
	// Drop any queued steering messages for this session; the run is being aborted.
	a.clearSteering(sessionID)
	// Clear the resurrection budget for this session; a cancelled chain starts fresh.
	a.clearResurrectionCount(sessionID)

	// Cancel regular requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist(fmt.Sprintf("Request cancellation initiated for session: %s", sessionID))
			cancel()
		}
	}

	// Also check for summarize requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID + "-summarize"); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist(fmt.Sprintf("Summarize cancellation initiated for session: %s", sessionID))
			cancel()
		}
	}
}

// Steer queues a mid-run feedback message for injection at the next safe boundary
// of the agent loop. It only succeeds while a run is active for the session;
// otherwise it returns ErrSessionNotBusy so the caller can fall back to Run.
func (a *agent) Steer(sessionID string, content string, attachments ...message.Attachment) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("cannot steer with empty content")
	}
	if !a.IsSessionBusy(sessionID) {
		return ErrSessionNotBusy
	}

	a.steeringMu.Lock()
	a.steeringQueue[sessionID] = append(a.steeringQueue[sessionID], steeringMessage{
		content:     content,
		attachments: attachments,
	})
	queued := len(a.steeringQueue[sessionID])
	a.steeringMu.Unlock()

	logging.Debug("Steering message queued", "sessionID", sessionID, "queued", queued)
	ev := AgentEvent{
		Type:          AgentEventTypeSteeringQueued,
		SessionID:     sessionID,
		SystemMessage: "💬 Feedback queued — it will be injected at the next step.",
	}
	a.publishEvent(ev)
	return nil
}

// InjectConclusion queues a delegated-task conclusion for injection into a
// still-running parent loop at the next safe boundary. It mirrors Steer but the
// queued message is typed as a conclusion and carries no attachments; the content
// is used verbatim (the supervisor has already formatted it). Returns
// ErrSessionNotBusy when the session is idle so the caller can decide whether to
// resurrect it (a later phase).
func (a *agent) InjectConclusion(sessionID string, content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("cannot inject empty conclusion")
	}
	if !a.IsSessionBusy(sessionID) {
		return ErrSessionNotBusy
	}

	a.steeringMu.Lock()
	a.steeringQueue[sessionID] = append(a.steeringQueue[sessionID], steeringMessage{
		kind:    steeringConclusion,
		content: content,
	})
	queued := len(a.steeringQueue[sessionID])
	a.steeringMu.Unlock()

	logging.Debug("Delegated conclusion queued", "sessionID", sessionID, "queued", queued)
	ev := AgentEvent{
		Type:          AgentEventTypeConclusionQueued,
		SessionID:     sessionID,
		SystemMessage: "📥 Delegated result queued — it will be injected at the next step.",
	}
	a.publishEvent(ev)
	return nil
}

// Resume starts a new system-initiated run for an idle session, used by the
// delegation supervisor to "resurrect" a parent loop when one or more delegated
// tasks finished while the parent was idle (Case B). It reuses the Run machinery
// so the run's behaviour for user calls stays unchanged: Resume publishes a
// distinct AgentEventTypeResurrected system message (so UIs can frame the turn),
// increments the session's resurrection counter, then calls Run and drains the
// returned channel in a goroutine. Events still reach UIs via the pubsub broker;
// draining only prevents the buffered run channel from blocking the run goroutine.
func (a *agent) Resume(ctx context.Context, sessionID string, content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("cannot resume with empty content")
	}
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}

	// Frame the turn for the UI before starting the run. Published on the broker so
	// every subscribed UI sees why the session woke.
	a.publishEvent(AgentEvent{
		Type:          AgentEventTypeResurrected,
		SessionID:     sessionID,
		SystemMessage: "🔁 Resuming — a delegated task reported its result.",
	})

	events, err := a.runInternal(ctx, sessionID, content)
	if err != nil {
		// The run failed to start (e.g. ErrSessionBusy from a race, or ErrNoModel);
		// do not count a resurrection that never started.
		return err
	}

	// Count the resurrection now that the run actually started. runInternal does
	// NOT reset the counter (unlike Run), so it accumulates across resurrections.
	a.incrementResurrectionCount(sessionID)

	// Nobody reads the returned channel; drain it so the run goroutine never blocks
	// on the buffered(512) channel filling up.
	go func() {
		for range events {
		}
	}()
	return nil
}

// ResurrectionCount reports how many times the session has been resurrected via
// Resume since the last user-initiated Run.
func (a *agent) ResurrectionCount(sessionID string) int {
	a.resurrectMu.Lock()
	defer a.resurrectMu.Unlock()
	return a.resurrectCount[sessionID]
}

func (a *agent) incrementResurrectionCount(sessionID string) {
	a.resurrectMu.Lock()
	a.resurrectCount[sessionID]++
	a.resurrectMu.Unlock()
}

func (a *agent) clearResurrectionCount(sessionID string) {
	a.resurrectMu.Lock()
	delete(a.resurrectCount, sessionID)
	a.resurrectMu.Unlock()
}

// PendingSteering reports how many steering messages are queued for the session.
func (a *agent) PendingSteering(sessionID string) int {
	a.steeringMu.Lock()
	defer a.steeringMu.Unlock()
	return len(a.steeringQueue[sessionID])
}

// dequeueSteering atomically removes and returns all queued steering messages for
// the session.
func (a *agent) dequeueSteering(sessionID string) []steeringMessage {
	a.steeringMu.Lock()
	defer a.steeringMu.Unlock()
	msgs := a.steeringQueue[sessionID]
	if len(msgs) == 0 {
		return nil
	}
	delete(a.steeringQueue, sessionID)
	return msgs
}

// clearSteering drops any queued steering messages for the session.
func (a *agent) clearSteering(sessionID string) {
	a.steeringMu.Lock()
	defer a.steeringMu.Unlock()
	delete(a.steeringQueue, sessionID)
}

// drainSteeringInto materializes any queued steering messages as persisted user
// messages and appends them to msgHistory. It returns the (possibly extended)
// history and whether any messages were injected. Injection happens only at safe
// loop boundaries (never between a tool_call and its tool_result).
func (a *agent) drainSteeringInto(ctx context.Context, sessionID string, msgHistory []message.Message, eventCh chan<- AgentEvent) ([]message.Message, bool) {
	queued := a.dequeueSteering(sessionID)
	if len(queued) == 0 {
		return msgHistory, false
	}

	injected := 0
	feedbackInjected := 0
	conclusionInjected := 0
	for _, sm := range queued {
		var attachmentParts []message.ContentPart
		for _, attachment := range sm.attachments {
			attachmentParts = append(attachmentParts, message.BinaryContent{
				Path:     attachment.FilePath,
				MIMEType: attachment.MimeType,
				Data:     attachment.Content,
			})
		}
		userMsg, err := a.createUserMessage(ctx, sessionID, sm.content, attachmentParts)
		if err != nil {
			logging.ErrorPersist(fmt.Sprintf("failed to inject steering message: %v", err))
			continue
		}
		msgHistory = append(msgHistory, userMsg)
		injected++
		switch sm.kind {
		case steeringConclusion:
			conclusionInjected++
		default:
			feedbackInjected++
		}
	}
	if injected == 0 {
		return msgHistory, false
	}

	logging.Debug("Steering messages injected", "sessionID", sessionID, "count", injected,
		"feedback", feedbackInjected, "conclusions", conclusionInjected)
	a.addRunStatusMessage(sessionID, fmt.Sprintf("Injected %d steering message(s)", injected))

	// Emit one event per kind so the UI can frame user feedback and delegated
	// conclusions differently. Both are delivered through the run event channel.
	if feedbackInjected > 0 {
		ev := AgentEvent{
			Type:          AgentEventTypeSteeringInjected,
			SessionID:     sessionID,
			SystemMessage: "💬 Feedback injected — continuing with your input.",
		}
		a.publishEvent(ev)
		select {
		case eventCh <- ev:
		default:
		}
	}
	if conclusionInjected > 0 {
		ev := AgentEvent{
			Type:          AgentEventTypeConclusionInjected,
			SessionID:     sessionID,
			SystemMessage: "📥 Delegated result injected — continuing with the subagent's findings.",
		}
		a.publishEvent(ev)
		select {
		case eventCh <- ev:
		default:
		}
	}
	return msgHistory, true
}

func (a *agent) IsBusy() bool {
	busy := false
	a.activeRequests.Range(func(key, value interface{}) bool {
		if cancelFunc, ok := value.(context.CancelFunc); ok {
			if cancelFunc != nil {
				busy = true
				return false // Stop iterating
			}
		}
		return true // Continue iterating
	})
	return busy
}

func (a *agent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Load(sessionID)
	return busy
}

func (a *agent) generateTitle(ctx context.Context, sessionID string, content string) error {
	logging.Debug("Generating title", "sessionID", sessionID, "contentLength", len(content))
	if content == "" {
		return nil
	}
	if a.titleProvider == nil {
		return nil
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	parts := []message.ContentPart{message.TextContent{Text: content}}
	response, err := a.titleProvider.SendMessages(
		ctx,
		[]message.Message{
			{
				Role:  message.User,
				Parts: parts,
			},
		},
		make([]tools.BaseTool, 0),
	)
	if err != nil {
		return err
	}

	title := strings.TrimSpace(strings.ReplaceAll(response.Content, "\n", " "))
	if title == "" {
		return nil
	}

	session.Title = title
	_, err = a.sessions.Save(ctx, session)
	return err
}

func (a *agent) err(err error) AgentEvent {
	return AgentEvent{
		Type:  AgentEventTypeError,
		Error: err,
	}
}

func (a *agent) publishEvent(event AgentEvent) {
	a.Publish(pubsub.CreatedEvent, event)
}

func (a *agent) addRunStatusMessage(sessionID string, msg string) {
	msg = strings.TrimSpace(msg)
	if sessionID == "" || msg == "" {
		return
	}
	a.runStatusMu.Lock()
	defer a.runStatusMu.Unlock()
	a.runStatusMessages[sessionID] = append(a.runStatusMessages[sessionID], msg)
}

func (a *agent) clearRunStatusMessages(sessionID string) {
	if sessionID == "" {
		return
	}
	a.runStatusMu.Lock()
	defer a.runStatusMu.Unlock()
	delete(a.runStatusMessages, sessionID)
}

func (a *agent) LastRunSystemMessages(sessionID string) []string {
	a.runStatusMu.Lock()
	defer a.runStatusMu.Unlock()
	msgs := append([]string(nil), a.runStatusMessages[sessionID]...)
	delete(a.runStatusMessages, sessionID)
	return msgs
}

func (a *agent) emitCompactionError(sessionID string, err error, eventCh chan<- AgentEvent) {
	if err == nil {
		return
	}
	logging.WarnPersist("Context compaction failed", "sessionID", sessionID, "error", err)
	event := AgentEvent{Type: AgentEventTypeError, SessionID: sessionID, Error: err}
	a.publishEvent(event)
	select {
	case eventCh <- event:
	default:
	}
}

// ErrNoModel is returned when the agent has no model configured.
var ErrNoModel = fmt.Errorf("no model configured, please select a model")

func (a *agent) Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error) {
	// A user-initiated run resets the resurrection budget for the session so the
	// next idle-completion chain starts from zero (and MaxResurrections is enforced
	// per user turn-chain, not for the whole session lifetime).
	a.clearResurrectionCount(sessionID)
	return a.runInternal(ctx, sessionID, content, attachments...)
}

// runInternal is the shared run machinery used by both the user-initiated Run and
// the system-initiated Resume. It deliberately does NOT touch the resurrection
// counter (callers manage that) so Run's behaviour for user calls is preserved
// exactly while Resume can accumulate the count across resurrections.
func (a *agent) runInternal(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error) {
	a.clearRunStatusMessages(sessionID)
	logging.Debug("Agent.Run called", "sessionID", sessionID, "contentLength", len(content), "attachmentCount", len(attachments))
	if a.provider == nil {
		return nil, ErrNoModel
	}
	if !a.provider.Model().SupportsAttachments && attachments != nil {
		attachments = nil
	}
	events := make(chan AgentEvent, 512)
	if a.IsSessionBusy(sessionID) {
		return nil, ErrSessionBusy
	}

	genCtx, cancel := context.WithCancel(ctx)

	a.activeRequests.Store(sessionID, cancel)
	go func() {
		logging.Debug("Request started", "sessionID", sessionID)
		defer logging.RecoverPanic("agent.Run", func() {
			events <- a.err(fmt.Errorf("panic while running the agent"))
		})
		var attachmentParts []message.ContentPart
		for _, attachment := range attachments {
			attachmentParts = append(attachmentParts, message.BinaryContent{Path: attachment.FilePath, MIMEType: attachment.MimeType, Data: attachment.Content})
		}
		result := a.processGeneration(genCtx, sessionID, content, attachmentParts, events)
		if result.Error != nil && !errors.Is(result.Error, ErrRequestCancelled) && !errors.Is(result.Error, context.Canceled) {
			logging.ErrorPersist(result.Error.Error())
		}
		logging.Debug("Request completed", "sessionID", sessionID)
		a.activeRequests.Delete(sessionID)
		// Drop any steering messages that were not consumed by the loop so they do
		// not leak into a subsequent run for the same session.
		a.clearSteering(sessionID)
		cancel()
		a.publishEvent(result)
		events <- result
		close(events)
	}()
	return events, nil
}

// sanitizeToolCallHistory normalizes assistant/tool exchanges into the strict
// shape expected by providers like Anthropic:
//   - every assistant message with tool_calls is followed by exactly one tool
//     message covering only those tool_call_ids
//   - missing tool results are synthesized as interrupted errors
//   - orphan tool messages are dropped
//
// This avoids invalid histories after interrupted tool execution, partial tool
// persistence, or front-trimming that removes the originating assistant turn.
func sanitizeToolCallHistory(msgs []message.Message) []message.Message {
	result := make([]message.Message, 0, len(msgs))
	for i := 0; i < len(msgs); i++ {
		msg := msgs[i]
		if msg.Role == message.Tool {
			logging.Debug("sanitizeToolCallHistory: dropping orphan tool message", "messageID", msg.ID)
			continue
		}
		result = append(result, msg)
		if msg.Role != message.Assistant {
			continue
		}
		toolCalls := msg.ToolCalls()
		if len(toolCalls) == 0 {
			continue
		}
		expected := make(map[string]message.ToolCall, len(toolCalls))
		orderedResults := make([]message.ContentPart, 0, len(toolCalls))
		collected := make(map[string]message.ToolResult, len(toolCalls))
		for _, tc := range toolCalls {
			expected[tc.ID] = tc
		}
		nextIndex := i + 1
		for nextIndex < len(msgs) && msgs[nextIndex].Role == message.Tool {
			for _, tr := range msgs[nextIndex].ToolResults() {
				tc, ok := expected[tr.ToolCallID]
				if !ok {
					continue
				}
				if _, alreadyCollected := collected[tr.ToolCallID]; alreadyCollected {
					continue
				}
				if tr.Name == "" {
					tr.Name = tc.Name
				}
				collected[tr.ToolCallID] = tr
			}
			nextIndex++
		}
		missingCount := 0
		for _, tc := range toolCalls {
			tr, ok := collected[tc.ID]
			if !ok {
				tr = message.ToolResult{
					ToolCallID: tc.ID,
					Name:       tc.Name,
					Content:    "Tool execution was interrupted",
					IsError:    true,
				}
				missingCount++
			}
			orderedResults = append(orderedResults, tr)
		}
		if len(orderedResults) > 0 {
			if missingCount > 0 || nextIndex > i+2 {
				logging.Debug("sanitizeToolCallHistory: normalizing tool results",
					"assistantMsgID", msg.ID,
					"collected", len(collected),
					"missing", missingCount,
					"consumedToolMessages", nextIndex-(i+1),
				)
			}
			result = append(result, message.Message{
				Role:  message.Tool,
				Parts: orderedResults,
			})
		}
		i = nextIndex - 1
	}
	return result
}

func (a *agent) processGeneration(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart, eventCh chan<- AgentEvent) AgentEvent {
	cfg := config.Get()
	// List existing messages; if none, start title generation asynchronously.
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to list messages: %w", err))
	}
	if len(msgs) == 0 {
		go func() {
			defer logging.RecoverPanic("agent.Run", func() {
				logging.ErrorPersist("panic while generating title")
			})
			titleErr := a.generateTitle(context.Background(), sessionID, content)
			if titleErr != nil {
				logging.ErrorPersist(fmt.Sprintf("failed to generate title: %v", titleErr))
			}
		}()
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to get session: %w", err))
	}
	logging.Debug("processGeneration", "sessionID", sessionID, "existingMessages", len(msgs), "hasSummary", session.SummaryMessageID != "")
	msgs = applySummaryBoundary(msgs, session.SummaryMessageID)

	// Trim message history to fit within 40% of the current model's context window.
	// This prevents overflowing when the model was switched to one with a smaller
	// context window mid-conversation. The 40% budget leaves ample room for the new
	// user message, tool calls, and the model's response.
	if a.provider != nil {
		contextWindow := effectiveContextWindow(a.agentName, a.provider.Model())
		msgs = trimMessagesToContextBudget(msgs, contextWindow, 0.40)
	}

	// Sanitize history: if an assistant message has tool_calls but no matching
	// tool results follow (e.g. the session was interrupted mid-tool-execution),
	// insert synthetic "interrupted" results so the API does not reject the request.
	msgs = sanitizeToolCallHistory(msgs)

	// Hook 4: hook_conversation_start — may inject context into user content
	if a.luaMgr != nil && a.luaMgr.IsEnabled() {
		hookData := map[string]interface{}{
			"session_id":     sessionID,
			"is_new_session": len(msgs) == 0,
			"message_count":  len(msgs),
		}
		result, _ := a.luaMgr.ExecuteHook(ctx, luaengine.HookConversationStart, hookData)
		if result != nil && result.Modified {
			if injected, ok := result.Data["injected_context"].(string); ok && injected != "" {
				content = injected + "\n\n" + content
			}
		}
	}

	// Build the request-scoped context carrying the session ID so per-session
	// overrides (model, persona, inference settings) resolve correctly even for
	// concurrent sessions sharing this process.
	promptCtx := context.WithValue(ctx, prompt.SessionIDKey, sessionID)

	// Resolve persona content to inject into the system prompt.
	// This is done before creating the user message so the content (user query)
	// can be used for auto-selection without modifying the user message itself.
	personaContent := getPersonaContent(promptCtx, content)
	if isCleanModeContext(ctx) {
		personaContent = ""
	}
	if activePersona := strings.TrimSpace(effectiveActivePersona(promptCtx)); activePersona != "" {
		a.addRunStatusMessage(sessionID, fmt.Sprintf("Selected persona: %s", activePersona))
	}

	// Context enrichment: if the KB/code enricher is active, append retrieved context
	// after the user message so the user intent is clear and context follows naturally.
	if globalContextEnricher != nil {
		enriched := globalContextEnricher.EnrichContext(ctx, content)
		if enriched != "" {
			content = content + "\n\n" + enriched
		}
	}

	userMsg, err := a.createUserMessage(ctx, sessionID, content, attachmentParts)
	if err != nil {
		return a.err(fmt.Errorf("failed to create user message: %w", err))
	}
	// Append the new user message to the conversation history.
	msgHistory := append(msgs, userMsg)

	// Build provider with persona injected into the system prompt.
	requestProvider, err := a.prepareProvider(promptCtx, content, personaContent)
	if err != nil {
		return a.err(fmt.Errorf("failed to prepare agent provider: %w", err))
	}

	msgHistory, err = a.ensureHistoryFitsBeforeSend(ctx, sessionID, msgHistory, requestProvider, eventCh)
	if err != nil {
		return a.err(err)
	}

	// Pre-session context trimming: for the first message of a session, analyze the task
	// and store the recommended tool subset in the context so streamAndHandleEvents can
	// filter the advertised tool list. This reduces context window usage by removing
	// irrelevant tools. Falls back silently to all tools on any error.
	runCtx := ctx
	if globalContextTrimmer != nil && len(msgs) == 0 {
		toolDescs := toolDescriptionsFrom(a.tools)
		if filteredNames, trimErr := globalContextTrimmer.ProfileTask(ctx, content, toolDescs); trimErr == nil && len(filteredNames) > 0 {
			runCtx = context.WithValue(ctx, trimmedToolsKey{}, filteredNames)
			logging.Debug("Context trimmer applied", "original_tools", len(a.tools), "filtered_tools", len(filteredNames))
		} else if trimErr != nil {
			logging.Debug("Context trimmer failed, using all tools", "error", trimErr)
		}
	}

	for {
		// Check for cancellation before each iteration
		select {
		case <-runCtx.Done():
			return a.err(runCtx.Err())
		default:
			// Continue processing
		}
		logging.Debug("processGeneration iteration", "sessionID", sessionID, "historyLength", len(msgHistory))
		agentMessage, toolResults, err := a.streamAndHandleEvents(runCtx, sessionID, msgHistory, requestProvider, eventCh)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				agentMessage.AddFinish(message.FinishReasonCanceled)
				a.messages.Update(context.Background(), agentMessage)
				return a.err(ErrRequestCancelled)
			}
			return a.err(fmt.Errorf("failed to process events: %w", err))
		}
		if cfg.Debug {
			seqId := (len(msgHistory) + 1) / 2
			toolResultFilepath := logging.WriteToolResultsJson(sessionID, seqId, toolResults)
			logging.Info("Result", "message", agentMessage.FinishReason(), "toolResults", "{}", "filepath", toolResultFilepath)
		} else {
			logging.Info("Result", "message", agentMessage.FinishReason(), "toolResults", toolResults)
		}
		if (agentMessage.FinishReason() == message.FinishReasonToolUse) && toolResults != nil {
			// We are not done, we need to respond with the tool response
			msgHistory = append(msgHistory, agentMessage, *toolResults)

			// Safe boundary: inject any queued steering feedback now that the
			// tool results for this iteration are persisted and the history is in a
			// valid tool_call/tool_result shape.
			msgHistory, _ = a.drainSteeringInto(ctx, sessionID, msgHistory, eventCh)

			// Check if we should auto-compact context before continuing the loop
			if sess, sessErr := a.sessions.Get(ctx, sessionID); sessErr == nil && a.shouldCompact(sess) {
				compactMsg := "\n\n⚡ Auto-compacting context to free space...\n"
				sysEv := AgentEvent{Type: AgentEventTypeSystemMessage, SessionID: sessionID, SystemMessage: compactMsg}
				a.publishEvent(sysEv)
				eventCh <- sysEv
				if compactErr := a.compactContext(ctx, sessionID); compactErr != nil {
					a.emitCompactionError(sessionID, compactErr, eventCh)
				} else {
					if newMsgs, reloadErr := a.loadSessionMessagesFromSummary(ctx, sessionID); reloadErr == nil {
						msgHistory = newMsgs
					}
					doneMsg := "✓ Context compacted. Continuing...\n\n"
					doneEv := AgentEvent{Type: AgentEventTypeSystemMessage, SessionID: sessionID, SystemMessage: doneMsg}
					a.publishEvent(doneEv)
					eventCh <- doneEv
				}
			}

			continue
		}

		// End of turn: before finishing, check for queued steering feedback. If the
		// user provided input while the model was responding, inject it as a new
		// user turn and continue the loop instead of returning, so the run keeps
		// going without the user having to resend.
		if newHistory, injected := a.drainSteeringInto(ctx, sessionID, append(msgHistory, agentMessage), eventCh); injected {
			fitted, fitErr := a.ensureHistoryFitsBeforeSend(ctx, sessionID, newHistory, requestProvider, eventCh)
			if fitErr != nil {
				return a.err(fitErr)
			}
			msgHistory = fitted
			continue
		}

		return AgentEvent{
			Type:      AgentEventTypeResponse,
			Message:   agentMessage,
			SessionID: sessionID,
			Done:      true,
		}
	}
}

func (a *agent) ensureHistoryFitsBeforeSend(ctx context.Context, sessionID string, msgHistory []message.Message, requestProvider provider.Provider, eventCh chan<- AgentEvent) ([]message.Message, error) {
	fitted := fitMessagesToProviderBudget(msgHistory, a.agentName, requestProvider.Model())
	if estimateMessagesTokens(fitted) <= providerInputBudget(a.agentName, requestProvider.Model()) {
		return fitted, nil
	}

	compactMsg := "\n\n⚡ Auto-compacting context before sending request...\n"
	a.addRunStatusMessage(sessionID, "Auto-compacting context before sending request")
	compactEv := AgentEvent{Type: AgentEventTypeSystemMessage, SessionID: sessionID, SystemMessage: compactMsg}
	a.publishEvent(compactEv)
	eventCh <- compactEv

	if err := a.compactContext(ctx, sessionID); err != nil {
		trimmed := fitMessagesToProviderBudget(msgHistory, a.agentName, requestProvider.Model())
		if estimateMessagesTokens(trimmed) <= providerInputBudget(a.agentName, requestProvider.Model()) {
			warnMsg := "⚠️ Context compaction failed; continuing with aggressively trimmed history.\n\n"
			a.addRunStatusMessage(sessionID, "Context compaction failed; continuing with trimmed history")
			warnEv := AgentEvent{Type: AgentEventTypeSystemMessage, SessionID: sessionID, SystemMessage: warnMsg}
			a.publishEvent(warnEv)
			eventCh <- warnEv
			return trimmed, nil
		}
		return nil, fmt.Errorf("session exceeds %s context budget and compaction failed: %w", requestProvider.Model().ID, err)
	}

	reloaded, err := a.loadSessionMessagesFromSummary(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	reloaded = fitMessagesToProviderBudget(reloaded, a.agentName, requestProvider.Model())
	if estimateMessagesTokens(reloaded) > providerInputBudget(a.agentName, requestProvider.Model()) {
		return nil, fmt.Errorf("session remains too large for %s after compaction", requestProvider.Model().ID)
	}

	doneMsg := "✓ Context compacted before sending request.\n\n"
	a.addRunStatusMessage(sessionID, "Context compacted before sending request")
	doneEv := AgentEvent{Type: AgentEventTypeSystemMessage, SessionID: sessionID, SystemMessage: doneMsg}
	a.publishEvent(doneEv)
	eventCh <- doneEv
	return reloaded, nil
}

func (a *agent) loadSessionMessagesFromSummary(ctx context.Context, sessionID string) ([]message.Message, error) {
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to reload session messages: %w", err)
	}
	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to reload session: %w", err)
	}
	return applySummaryBoundary(msgs, sess.SummaryMessageID), nil
}

func applySummaryBoundary(msgs []message.Message, summaryMessageID string) []message.Message {
	if summaryMessageID == "" {
		return sanitizeMessagesForPrompt(msgs)
	}
	summaryMsgIndex := -1
	for i, msg := range msgs {
		if msg.ID == summaryMessageID {
			summaryMsgIndex = i
			break
		}
	}
	if summaryMsgIndex == -1 {
		return sanitizeMessagesForPrompt(msgs)
	}
	msgs = msgs[summaryMsgIndex:]
	if len(msgs) > 0 {
		msgs[0].Role = message.User
	}
	return sanitizeMessagesForPrompt(msgs)
}

func sanitizeMessagesForPrompt(msgs []message.Message) []message.Message {
	sanitized := make([]message.Message, 0, len(msgs))
	for _, msg := range msgs {
		msgCopy := msg
		if msg.Role == message.Tool {
			parts := make([]message.ContentPart, 0, len(msg.Parts))
			for _, part := range msg.Parts {
				tr, ok := part.(message.ToolResult)
				if !ok {
					parts = append(parts, part)
					continue
				}
				parts = append(parts, tr.SanitizedForPrompt())
			}
			msgCopy.Parts = parts
		}
		sanitized = append(sanitized, msgCopy)
	}
	return sanitized
}

func effectiveContextWindow(agentName config.AgentName, model models.Model) int64 {
	cfg := config.Get()
	if cfg != nil {
		if agentCfg, ok := cfg.Agents[agentName]; ok && agentCfg.ContextWindowOverride > 0 {
			return agentCfg.ContextWindowOverride
		}
	}
	return model.ContextWindow
}

func providerInputBudget(agentName config.AgentName, model models.Model) int64 {
	contextWindow := effectiveContextWindow(agentName, model)
	if contextWindow <= 0 {
		return 0
	}
	reserved := int64(effectiveMaxTokens(agentName, model)) + summaryToolOverheadTokens
	budget := contextWindow - reserved
	if budget < summaryMinInputBudgetTokens {
		if contextWindow <= summaryMinInputBudgetTokens {
			return contextWindow
		}
		return summaryMinInputBudgetTokens
	}
	return budget
}

func fitMessagesToProviderBudget(msgs []message.Message, agentName config.AgentName, model models.Model) []message.Message {
	budget := providerInputBudget(agentName, model)
	if budget <= 0 {
		return sanitizeToolCallHistory(msgs)
	}
	trimmed := append([]message.Message(nil), msgs...)
	for len(trimmed) > 1 && estimateMessagesTokens(trimmed) > budget {
		trimmed = trimmed[1:]
	}
	return sanitizeToolCallHistory(trimmed)
}

func (a *agent) createUserMessage(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart) (message.Message, error) {
	// Hook 5: hook_user_prompt — may modify user content before creating message
	if a.luaMgr != nil && a.luaMgr.IsEnabled() {
		hookData := map[string]interface{}{
			"session_id":   sessionID,
			"user_content": content,
		}
		result, _ := a.luaMgr.ExecuteHook(ctx, luaengine.HookUserPrompt, hookData)
		if result != nil && result.Modified {
			if modified, ok := result.Data["modified_content"].(string); ok && modified != "" {
				content = modified
			}
		}
	}

	parts := []message.ContentPart{message.TextContent{Text: content}}
	parts = append(parts, attachmentParts...)
	return a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: parts,
	})
}

func (a *agent) streamAndHandleEvents(ctx context.Context, sessionID string, msgHistory []message.Message, requestProvider provider.Provider, eventCh chan<- AgentEvent) (message.Message, *message.Message, error) {
	logging.Debug("streamAndHandleEvents started", "sessionID", sessionID, "historyLength", len(msgHistory), "model", requestProvider.Model().ID)
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	if cache, ok := tools.GetSessionCacheByID(sessionID); ok {
		ctx = context.WithValue(ctx, tools.SessionCacheContextKey, cache)
	}

	// Apply context-trimmed tool list if a pre-session profile was computed.
	// Only the advertised tool list is filtered; tool execution still looks up from a.tools
	// so any tool can always be invoked (e.g. if the LLM calls one based on prior context).
	activeTools := a.tools
	if filteredNames, ok := ctx.Value(trimmedToolsKey{}).([]string); ok && len(filteredNames) > 0 {
		activeTools = filterToolsByNames(a.tools, filteredNames)
		logging.Debug("streamAndHandleEvents: using trimmed tool list", "active", len(activeTools), "total", len(a.tools))
	}

	providerEventChan := requestProvider.StreamResponse(ctx, msgHistory, activeTools)

	assistantMsg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{},
		Model: requestProvider.Model().ID,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create assistant message: %w", err)
	}

	// Add the session and message ID into the context if needed by tools.
	ctx = context.WithValue(ctx, tools.MessageIDContextKey, assistantMsg.ID)

	// Process each event in the stream.
	for event := range providerEventChan {
		if processErr := a.processEvent(ctx, sessionID, &assistantMsg, event, requestProvider, eventCh); processErr != nil {
			a.finishMessage(ctx, &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, processErr
		}
		if ctx.Err() != nil {
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			return assistantMsg, nil, ctx.Err()
		}
	}

	toolResults := make([]message.ToolResult, len(assistantMsg.ToolCalls()))
	toolCalls := assistantMsg.ToolCalls()
	toolCtx := ctx
	if len(toolCalls) > 0 {
		var toolCtxErr error
		toolCtx, toolCtxErr = withToolWorkspaceContext(ctx)
		if toolCtxErr != nil {
			return assistantMsg, nil, toolCtxErr
		}
	}

	// Capture the last confirmed token totals so we can emit live, provisional
	// context-window estimates as each tool result is produced. The next confirmed
	// usage (EventComplete on the following request) reconciles these estimates.
	tokenBasePrompt, tokenBaseCompletion, tokenContextWindow := a.tokenEstimateBase(ctx, sessionID)
	var estimatedToolTokens int64
	for i, toolCall := range toolCalls {
		select {
		case <-toolCtx.Done():
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			// Make all future tool calls cancelled
			for j := i; j < len(toolCalls); j++ {
				toolResults[j] = message.ToolResult{
					ToolCallID: toolCalls[j].ID,
					Content:    "Tool execution canceled by user",
					IsError:    true,
				}
			}
			goto out
		default:
			// Continue processing
			var tool tools.BaseTool
			// Resolve cross-model alias first (e.g. "read" → "view" for non-Anthropic models).
			resolvedName := tools.ResolveToolAlias(toolCall.Name)
			for _, availableTool := range a.tools {
				name := availableTool.Info().Name
				if name == toolCall.Name || name == resolvedName {
					tool = availableTool
					break
				}
			}

			// Tool not found
			if tool == nil {
				toolResults[i] = message.ToolResult{
					ToolCallID: toolCall.ID,
					Name:       toolCall.Name,
					Content:    fmt.Sprintf("Tool not found: %s", toolCall.Name),
					IsError:    true,
					Input:      toolCall.Input,
				}
				a.publishEvent(AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[i]})
				select {
				case eventCh <- AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[i]}:
				default:
				}
				continue
			}
			toolResult, toolErr := tool.Run(toolCtx, tools.ToolCall{
				ID:    toolCall.ID,
				Name:  toolCall.Name,
				Input: toolCall.Input,
			})
			if toolErr != nil {
				if errors.Is(toolErr, permission.ErrorPermissionDenied) {
					toolResults[i] = message.ToolResult{
						ToolCallID: toolCall.ID,
						Name:       toolCall.Name,
						Content:    "Permission denied",
						IsError:    true,
						Input:      toolCall.Input,
					}
					a.publishEvent(AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[i]})
					select {
					case eventCh <- AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[i]}:
					default:
					}
					for j := i + 1; j < len(toolCalls); j++ {
						toolResults[j] = message.ToolResult{
							ToolCallID: toolCalls[j].ID,
							Name:       toolCalls[j].Name,
							Content:    "Tool execution canceled by user",
							IsError:    true,
							Input:      toolCalls[j].Input,
						}
						a.publishEvent(AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[j]})
						select {
						case eventCh <- AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[j]}:
						default:
						}
					}
					a.finishMessage(ctx, &assistantMsg, message.FinishReasonPermissionDenied)
					break
				}
				toolResults[i] = message.ToolResult{
					ToolCallID: toolCall.ID,
					Name:       toolCall.Name,
					Content:    toolErr.Error(),
					IsError:    true,
					Input:      toolCall.Input,
				}
				a.publishEvent(AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[i]})
				select {
				case eventCh <- AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[i]}:
				default:
				}
				continue
			}
			// Auto-cache large responses to reduce context token usage
			if toolErr == nil {
				if cache := tools.GetSessionCache(ctx); cache != nil {
					toolResult = tools.InterceptToolResponse(cache, toolCall.ID, toolCall.Name, toolResult)
				}
			}
			toolResults[i] = message.ToolResult{
				ToolCallID: toolCall.ID,
				Name:       toolCall.Name,
				Content:    toolResult.Content,
				Metadata:   toolResult.Metadata,
				IsError:    toolResult.IsError,
				Input:      toolCall.Input,
			}
			a.publishEvent(AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[i]})
			select {
			case eventCh <- AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[i]}:
			default:
			}

			// Emit a live (estimated) context-window update so the TUI/WebUI counter
			// grows as this tool result enters the conversation, before the next LLM
			// request confirms the real usage.
			estimatedToolTokens += estimateToolResultTokens(toolResults[i])
			a.publishTokenUsage(sessionID, eventCh, TokenUsageInfo{
				PromptTokens:     tokenBasePrompt + estimatedToolTokens,
				CompletionTokens: tokenBaseCompletion,
				ContextWindow:    tokenContextWindow,
				Estimated:        true,
			})

			// Emit todos_updated event so TUI and WebUI can display the current plan.
			if toolCall.Name == tools.TodoWriteToolName && !toolResult.IsError {
				if currentTodos := tools.GetSessionTodos(sessionID); currentTodos != nil {
					todosEvent := AgentEvent{
						Type:      AgentEventTypeTodosUpdated,
						SessionID: sessionID,
						Todos:     currentTodos,
					}
					a.publishEvent(todosEvent)
					select {
					case eventCh <- todosEvent:
					default:
					}
				}
			}
		}
	}
out:
	logging.Debug("Tool calls processed", "sessionID", sessionID, "toolCallCount", len(toolCalls), "toolResultCount", len(toolResults))
	if len(toolResults) == 0 {
		return assistantMsg, nil, nil
	}
	parts := make([]message.ContentPart, 0)
	for _, tr := range toolResults {
		parts = append(parts, tr)
	}
	msg, err := a.messages.Create(context.Background(), assistantMsg.SessionID, message.CreateMessageParams{
		Role:  message.Tool,
		Parts: parts,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create cancelled tool message: %w", err)
	}

	return assistantMsg, &msg, err
}

func withToolWorkspaceContext(ctx context.Context) (context.Context, error) {
	resolver, ok := ctx.Value(tools.RuntimeResolverContextKey).(runtime.RuntimeResolver)
	if !ok || resolver == nil {
		resolver = runtime.NewResolver()
		ctx = context.WithValue(ctx, tools.RuntimeResolverContextKey, resolver)
	}

	cfg := config.ContainerConfig{}
	if loaded := config.Get(); loaded != nil {
		cfg = loaded.Container
	}

	_, workspaceFS, err := resolver.Resolve(cfg)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace filesystem: %w", err)
	}
	return context.WithValue(ctx, tools.WorkspaceFSContextKey, workspaceFS), nil
}

func (a *agent) finishMessage(ctx context.Context, msg *message.Message, finishReson message.FinishReason) {
	msg.AddFinish(finishReson)
	_ = a.messages.Update(ctx, *msg)
}

func (a *agent) processEvent(
	ctx context.Context,
	sessionID string,
	assistantMsg *message.Message,
	event provider.ProviderEvent,
	requestProvider provider.Provider,
	eventCh chan<- AgentEvent,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing.
	}

	switch event.Type {
	case provider.EventThinkingDelta:
		logging.Debug("Event: ThinkingDelta", "sessionID", sessionID, "contentLength", len(event.Thinking))
		assistantMsg.AppendReasoningContent(event.Thinking)
		a.publishEvent(AgentEvent{Type: AgentEventTypeThinkingDelta, SessionID: sessionID, Delta: event.Thinking})
		select {
		case eventCh <- AgentEvent{Type: AgentEventTypeThinkingDelta, SessionID: sessionID, Delta: event.Thinking}:
		default:
		}
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventContentDelta:
		logging.Debug("Event: ContentDelta", "sessionID", sessionID, "contentLength", len(event.Content))
		assistantMsg.AppendContent(event.Content)
		a.publishEvent(AgentEvent{Type: AgentEventTypeContentDelta, SessionID: sessionID, Delta: event.Content})
		select {
		case eventCh <- AgentEvent{Type: AgentEventTypeContentDelta, SessionID: sessionID, Delta: event.Content}:
		default:
		}
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventToolUseStart:
		logging.Debug("Event: ToolUseStart", "sessionID", sessionID, "toolName", event.ToolCall.Name, "toolID", event.ToolCall.ID)
		assistantMsg.AddToolCall(*event.ToolCall)
		a.publishEvent(AgentEvent{Type: AgentEventTypeToolCall, SessionID: sessionID, ToolCall: event.ToolCall})
		select {
		case eventCh <- AgentEvent{Type: AgentEventTypeToolCall, SessionID: sessionID, ToolCall: event.ToolCall}:
		default:
		}
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventToolUseDelta:
		// Accumulate tool call input in the assistant message so persistence stays consistent.
		assistantMsg.AppendToolCallInput(event.ToolCall.ID, event.ToolCall.Input)
		// Emit an AgentEventTypeToolCall event at most once every 100ms per
		// tool call. This gives the ACP prompt handler periodic enriched
		// updates (rawInput, title, locations) without saturating the
		// 256-slot event channel.
		const throttleInterval = 100 * time.Millisecond
		now := time.Now()
		throttleKey := event.ToolCall.ID
		lastEmit, loaded := a.toolCallThrottle.Load(throttleKey)
		if !loaded || now.Sub(lastEmit.(time.Time)) >= throttleInterval {
			a.toolCallThrottle.Store(throttleKey, now)
			// Retrieve the updated tool call from the assistant message so the
			// event carries the fully accumulated input so far.
			for _, tc := range assistantMsg.ToolCalls() {
				if tc.ID == event.ToolCall.ID {
					tcCopy := tc
					a.publishEvent(AgentEvent{Type: AgentEventTypeToolCall, SessionID: sessionID, ToolCall: &tcCopy})
					select {
					case eventCh <- AgentEvent{Type: AgentEventTypeToolCall, SessionID: sessionID, ToolCall: &tcCopy}:
					default:
					}
					break
				}
			}
		}
	case provider.EventToolUseStop:
		logging.Debug("Event: ToolUseStop", "sessionID", sessionID, "toolID", event.ToolCall.ID)
		assistantMsg.FinishToolCall(event.ToolCall.ID)
		// Clean up the throttle entry for this tool call to avoid leaking memory
		// across the session lifetime.
		a.toolCallThrottle.Delete(event.ToolCall.ID)
		// Send updated tool_call event with complete input to frontend
		for _, tc := range assistantMsg.ToolCalls() {
			if tc.ID == event.ToolCall.ID {
				a.publishEvent(AgentEvent{Type: AgentEventTypeToolCall, SessionID: sessionID, ToolCall: &tc})
				select {
				case eventCh <- AgentEvent{Type: AgentEventTypeToolCall, SessionID: sessionID, ToolCall: &tc}:
				default:
				}
				break
			}
		}
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventError:
		if errors.Is(event.Error, context.Canceled) {
			logging.InfoPersist(fmt.Sprintf("Event processing canceled for session: %s", sessionID))
			return context.Canceled
		}
		logging.ErrorPersist(event.Error.Error())
		return event.Error
	case provider.EventComplete:
		logging.Debug("Event: Complete", "sessionID", sessionID, "finishReason", event.Response.FinishReason, "toolCallCount", len(event.Response.ToolCalls), "inputTokens", event.Response.Usage.InputTokens, "outputTokens", event.Response.Usage.OutputTokens)
		resolvedToolCalls := resolveToolCallsOnComplete(
			assistantMsg.ToolCalls(),
			event.Response.ToolCalls,
			event.Response.FinishReason,
		)
		assistantMsg.SetToolCalls(resolvedToolCalls)
		assistantMsg.AddFinish(event.Response.FinishReason)
		if err := a.messages.Update(ctx, *assistantMsg); err != nil {
			return fmt.Errorf("failed to update message: %w", err)
		}
		// Hook 6: hook_agent_response_finish — informational, result ignored
		if a.luaMgr != nil && a.luaMgr.IsEnabled() {
			hookData := map[string]interface{}{
				"session_id":    sessionID,
				"finish_reason": string(event.Response.FinishReason),
				"input_tokens":  event.Response.Usage.InputTokens,
				"output_tokens": event.Response.Usage.OutputTokens,
			}
			a.luaMgr.ExecuteHook(ctx, luaengine.HookAgentResponseFinish, hookData) //nolint:errcheck
		}
		if err := a.TrackUsage(ctx, sessionID, requestProvider.Model(), event.Response.Usage); err != nil {
			return err
		}
		// Emit a confirmed token-usage update so live consumers (WebUI SSE) can
		// reconcile any provisional estimates shown during tool execution.
		if sess, err := a.sessions.Get(ctx, sessionID); err == nil {
			a.publishTokenUsage(sessionID, eventCh, TokenUsageInfo{
				PromptTokens:     sess.PromptTokens,
				CompletionTokens: sess.CompletionTokens,
				ContextWindow:    effectiveContextWindow(a.agentName, requestProvider.Model()),
				Estimated:        false,
			})
		}
		return nil
	}

	return nil
}

func resolveToolCallsOnComplete(existingToolCalls, responseToolCalls []message.ToolCall, finishReason message.FinishReason) []message.ToolCall {
	if len(responseToolCalls) > 0 {
		return responseToolCalls
	}

	if finishReason == message.FinishReasonToolUse && len(existingToolCalls) > 0 {
		return existingToolCalls
	}

	return responseToolCalls
}

// tokenEstimateBase returns the last confirmed prompt/completion token totals for
// the session plus the effective context window for the active model. These are
// used as the base for live, provisional context-window estimates emitted while
// tools execute. Failures are non-fatal: zeroed values simply mean the estimate
// starts from scratch and is reconciled on the next confirmed usage.
func (a *agent) tokenEstimateBase(ctx context.Context, sessionID string) (promptTokens, completionTokens, contextWindow int64) {
	contextWindow = effectiveContextWindow(a.agentName, a.provider.Model())
	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return 0, 0, contextWindow
	}
	return sess.PromptTokens, sess.CompletionTokens, contextWindow
}

// estimateToolResultTokens returns a rough token estimate for a tool result that
// will be appended to the conversation, using the heuristic of ~4 bytes per token.
func estimateToolResultTokens(tr message.ToolResult) int64 {
	chars := int64(len(tr.Content) + len(tr.Metadata))
	return (chars + 3) / 4
}

// publishTokenUsage emits a live AgentEventTypeTokenUsage event on both the pubsub
// broker and the per-run event channel (non-blocking) so the TUI/WebUI can update
// the context-window counter in real time during the agent loop.
func (a *agent) publishTokenUsage(sessionID string, eventCh chan<- AgentEvent, info TokenUsageInfo) {
	infoCopy := info
	ev := AgentEvent{Type: AgentEventTypeTokenUsage, SessionID: sessionID, TokenUsage: &infoCopy}
	a.publishEvent(ev)
	if eventCh != nil {
		select {
		case eventCh <- ev:
		default:
		}
	}
}

func (a *agent) TrackUsage(ctx context.Context, sessionID string, model models.Model, usage provider.TokenUsage) error {
	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		model.CostPer1MIn/1e6*float64(usage.InputTokens) +
		model.CostPer1MOut/1e6*float64(usage.OutputTokens)

	logging.Debug("TrackUsage", "sessionID", sessionID, "model", model.ID, "cost", cost, "inputTokens", usage.InputTokens, "outputTokens", usage.OutputTokens)

	sess.Cost += cost
	sess.CompletionTokens = usage.OutputTokens + usage.CacheReadTokens
	sess.PromptTokens = usage.InputTokens + usage.CacheCreationTokens

	_, err = a.sessions.Save(ctx, sess)
	if err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	return nil
}

func (a *agent) Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error) {
	logging.Debug("Agent model update", "agentName", string(agentName), "newModel", string(modelID))
	if a.IsBusy() {
		return models.Model{}, fmt.Errorf("cannot change model while processing requests")
	}

	if err := config.UpdateAgentModel(agentName, modelID); err != nil {
		return models.Model{}, fmt.Errorf("failed to update config: %w", err)
	}

	provider, err := createAgentProvider(context.Background(), agentName, a.tools, a.skillManager, nil)
	if err != nil {
		return models.Model{}, fmt.Errorf("failed to create provider for model %s: %w", modelID, err)
	}

	a.provider = provider
	if a.skillManager != nil && (agentName == config.AgentCoder || agentName == config.AgentTask) {
		a.contextManager = skills.NewContextManager(a.skillManager, effectiveMaxTokens(agentName, provider.Model()))
	}

	return a.provider.Model(), nil
}

func (a *agent) Summarize(ctx context.Context, sessionID string) error {
	logging.Debug("Summarize started", "sessionID", sessionID)
	// Check if session is busy
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}

	// Create a new context with cancellation
	summarizeCtx, cancel := context.WithCancel(ctx)

	// Store the cancel function in activeRequests to allow cancellation
	a.activeRequests.Store(sessionID+"-summarize", cancel)

	go func() {
		defer a.activeRequests.Delete(sessionID + "-summarize")
		defer cancel()
		event := AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Starting summarization...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Analyzing conversation...",
		}
		a.Publish(pubsub.CreatedEvent, event)

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Generating summary...",
		}

		a.Publish(pubsub.CreatedEvent, event)

		result, err := a.generateAndPersistSummary(summarizeCtx, sessionID, summaryModeManual)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: err,
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Persisting summary...",
		}

		a.Publish(pubsub.CreatedEvent, event)

		persistedMsg := "Summary persisted. Continue explicitly from the summary if more work is needed."
		if result.usedFallback {
			persistedMsg = "Summary persisted using coder fallback. Continue explicitly from the summary if more work is needed."
		}
		event = AgentEvent{
			Type:      AgentEventTypeSummarize,
			SessionID: result.message.SessionID,
			Progress:  persistedMsg,
		}
		a.Publish(pubsub.CreatedEvent, event)

		completeMsg := "Summary complete"
		if result.usedFallback {
			completeMsg = "Summary complete using coder fallback"
		}
		event = AgentEvent{
			Type:      AgentEventTypeSummarize,
			SessionID: result.message.SessionID,
			Progress:  completeMsg,
			Done:      true,
		}
		a.Publish(pubsub.CreatedEvent, event)
	}()

	return nil
}

// shouldCompact returns true if the active session is close to exhausting the
// model context after reserving output and tool overhead budget.
func (a *agent) shouldCompact(sess session.Session) bool {
	if a.provider == nil {
		return false
	}
	cfg := config.Get()
	agentCfg, ok := cfg.Agents[a.agentName]
	if !ok || !agentCfg.AutoCompact {
		return false
	}
	threshold := agentCfg.AutoCompactThreshold
	if threshold <= 0 {
		threshold = 0.85
	}
	// Use config override if set, otherwise fall back to model's reported context window.
	contextWindow := agentCfg.ContextWindowOverride
	if contextWindow <= 0 {
		contextWindow = a.provider.Model().ContextWindow
	}
	if contextWindow <= 0 {
		return false
	}
	reservedTokens := summaryOutputReservationTokens + summaryToolOverheadTokens
	availableContext := contextWindow - reservedTokens
	if availableContext <= 0 {
		availableContext = contextWindow
	}
	usedTokens := sess.PromptTokens + sess.CompletionTokens
	compactAt := int64(float64(availableContext) * threshold)
	should := usedTokens >= compactAt
	if should {
		logging.InfoPersist(fmt.Sprintf(
			"Auto-compact triggered: used=%d tokens, available=%d tokens (window=%d, reserved=%d), threshold=%.0f%% (%d tokens)",
			usedTokens, availableContext, contextWindow, reservedTokens, threshold*100, compactAt,
		))
	}
	return should
}

func (a *agent) generateAndPersistSummary(ctx context.Context, sessionID string, mode summaryMode) (*summaryResult, error) {
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to list messages for summary: %w", err)
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("no messages to summarize")
	}

	sendCtx := context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	response, usedProvider, usedFallback, err := a.sendSummaryRequest(sendCtx, sessionID, msgs, mode)
	if err != nil {
		return nil, err
	}

	summary := strings.TrimSpace(response.Content)
	if summary == "" {
		if mode == summaryModeCompaction {
			return nil, fmt.Errorf("empty compaction summary returned")
		}
		return nil, fmt.Errorf("empty summary returned")
	}

	summaryText := summary
	if mode == summaryModeCompaction {
		summaryText = a.buildCompactionContinuationSummary(msgs, summary)
	}

	summaryMsg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: summaryText},
			message.Finish{
				Reason: message.FinishReasonEndTurn,
				Time:   time.Now().Unix(),
			},
		},
		Model: usedProvider.Model().ID,
	})
	if err != nil {
		if mode == summaryModeCompaction {
			return nil, fmt.Errorf("failed to create compaction summary message: %w", err)
		}
		return nil, fmt.Errorf("failed to create summary message: %w", err)
	}

	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		if mode == summaryModeCompaction {
			return nil, fmt.Errorf("failed to get session for compaction: %w", err)
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	sess.SummaryMessageID = summaryMsg.ID
	sess.CompletionTokens = response.Usage.OutputTokens
	sess.PromptTokens = 0
	model := usedProvider.Model()
	usage := response.Usage
	cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		model.CostPer1MIn/1e6*float64(usage.InputTokens) +
		model.CostPer1MOut/1e6*float64(usage.OutputTokens)
	sess.Cost += cost
	if _, err = a.sessions.Save(ctx, sess); err != nil {
		if mode == summaryModeCompaction {
			return nil, fmt.Errorf("failed to save session after compaction: %w", err)
		}
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	return &summaryResult{message: summaryMsg, text: summary, model: model, usage: usage, usedFallback: usedFallback}, nil
}

func (a *agent) sendSummaryRequest(ctx context.Context, sessionID string, msgs []message.Message, mode summaryMode) (*provider.ProviderResponse, provider.Provider, bool, error) {
	providerToUse := a.summarizeProvider
	if providerToUse == nil {
		providerToUse = a.summarizeFallbackProvider
	}
	if providerToUse == nil {
		return nil, nil, false, fmt.Errorf("no summarizer provider available")
	}

	buildMessages := func(p provider.Provider) []message.Message {
		if mode == summaryModeManual {
			return buildManualSummaryMessages(msgs, p)
		}
		return buildCompactionSummaryMessages(sessionID, msgs, p)
	}

	messages := buildMessages(providerToUse)
	response, err := providerToUse.SendMessages(ctx, messages, []tools.BaseTool{})
	if err == nil {
		return response, providerToUse, false, nil
	}

	if a.summarizeProvider != nil && a.summarizeFallbackProvider != nil && providerToUse == a.summarizeProvider {
		fallback := a.summarizeFallbackProvider
		logging.WarnPersist("Configured summary model failed, retrying with coder (fallback) model",
			"error", err, "fallbackModel", fallback.Model().ID)
		a.addRunStatusMessage(sessionID, fmt.Sprintf("Compaction summary model failed; retrying with fallback model %s", fallback.Model().ID))
		response, fallbackErr := fallback.SendMessages(ctx, buildMessages(fallback), []tools.BaseTool{})
		if fallbackErr == nil {
			return response, fallback, true, nil
		}
		return nil, nil, false, fmt.Errorf("configured summary model failed: %w; coder fallback also failed: %w", err, fallbackErr)
	}

	return nil, nil, false, fmt.Errorf("failed to summarize: %w", err)
}

func buildConversationText(msgs []message.Message) string {
	var convText strings.Builder
	for _, msg := range msgs {
		role := "User"
		if msg.Role == message.Assistant {
			role = "Assistant"
		}
		text := msg.Content().Text
		if text != "" {
			convText.WriteString(fmt.Sprintf("\n\n%s: %s", role, text))
		}
	}
	return convText.String()
}

func buildManualSummaryMessages(msgs []message.Message, p provider.Provider) []message.Message {
	promptText := prompt.SummarizerPrompt(p.Model().Provider)
	promptMsg := message.Message{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: promptText}},
	}
	trimmed := trimMessagesToSummaryBudget(msgs, p)
	return append(trimmed, promptMsg)
}

func buildCompactionSummaryMessages(sessionID string, msgs []message.Message, p provider.Provider) []message.Message {
	promptText := prompt.SummarizerPrompt(p.Model().Provider)
	structured := buildStructuredConversationSummary(sessionID, msgs)
	budgeted := trimTextToSummaryBudget(structured, p)
	return []message.Message{{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: promptText + "\n\nConversation state to summarize:\n" + budgeted}},
	}}
}

func trimMessagesToSummaryBudget(msgs []message.Message, p provider.Provider) []message.Message {
	budget := summaryInputBudget(p)
	if budget <= 0 {
		return msgs
	}

	trimmed := append([]message.Message(nil), msgs...)
	for len(trimmed) > 2 && estimateMessagesTokens(trimmed) > budget {
		trimmed = trimmed[1:]
	}
	return trimmed
}

func trimTextToSummaryBudget(text string, p provider.Provider) string {
	budget := summaryInputBudget(p)
	if budget <= 0 {
		return text
	}
	return trimTextToContextWindow(text, budget, 0)
}

func summaryInputBudget(p provider.Provider) int64 {
	contextWindow := p.Model().ContextWindow
	if contextWindow <= 0 {
		return 0
	}
	budget := contextWindow - summaryOutputReservationTokens - summaryToolOverheadTokens
	if budget < summaryMinInputBudgetTokens {
		if contextWindow <= summaryMinInputBudgetTokens {
			return contextWindow
		}
		return summaryMinInputBudgetTokens
	}
	return budget
}

func estimateMessagesTokens(msgs []message.Message) int64 {
	var total int64
	for _, msg := range msgs {
		total += estimateMessageTokens(msg)
	}
	return total
}

func buildStructuredConversationSummary(sessionID string, msgs []message.Message) string {
	var b strings.Builder
	b.WriteString("Session ID: ")
	b.WriteString(sessionID)
	b.WriteString("\n")
	b.WriteString("Message count: ")
	b.WriteString(strconv.Itoa(len(msgs)))
	b.WriteString("\n\n")

	for i, msg := range msgs {
		b.WriteString("Message ")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(":\n")
		b.WriteString("- Role: ")
		b.WriteString(string(msg.Role))
		b.WriteString("\n")
		if msg.Model != "" {
			b.WriteString("- Model: ")
			b.WriteString(string(msg.Model))
			b.WriteString("\n")
		}
		if text := strings.TrimSpace(msg.Content().Text); text != "" {
			b.WriteString("- Text:\n")
			b.WriteString(indentSummaryBlock(text))
			b.WriteString("\n")
		}
		if thinking := strings.TrimSpace(msg.ReasoningContent().Thinking); thinking != "" {
			b.WriteString("- Reasoning:\n")
			b.WriteString(indentSummaryBlock(thinking))
			b.WriteString("\n")
		}
		if toolCalls := msg.ToolCalls(); len(toolCalls) > 0 {
			b.WriteString("- Tool calls:\n")
			for _, tc := range toolCalls {
				b.WriteString("  - ")
				b.WriteString(tc.Name)
				if tc.ID != "" {
					b.WriteString(" (#")
					b.WriteString(tc.ID)
					b.WriteString(")")
				}
				if input := strings.TrimSpace(tc.Input); input != "" {
					b.WriteString(" input:\n")
					b.WriteString(indentSummaryBlock(input))
				} else {
					b.WriteString("\n")
				}
			}
		}
		if toolResults := msg.ToolResults(); len(toolResults) > 0 {
			b.WriteString("- Tool results:\n")
			for _, tr := range toolResults {
				b.WriteString("  - ")
				b.WriteString(tr.Name)
				if tr.ToolCallID != "" {
					b.WriteString(" (#")
					b.WriteString(tr.ToolCallID)
					b.WriteString(")")
				}
				if shouldOmitToolResultContent(tr.Name) {
					b.WriteString(": <omitted from summary>\n")
					continue
				}
				if content := strings.TrimSpace(tr.Content); content != "" {
					b.WriteString(":\n")
					b.WriteString(indentSummaryBlock(content))
				} else {
					b.WriteString("\n")
				}
			}
		}
		if finish := msg.FinishPart(); finish != nil {
			b.WriteString("- Finish reason: ")
			b.WriteString(string(finish.Reason))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

func shouldOmitToolResultContent(toolName string) bool {
	return strings.EqualFold(toolName, tools.BrowserScreenshotToolName)
}

func indentSummaryBlock(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

func (a *agent) buildCompactionContinuationSummary(msgs []message.Message, summary string) string {
	summaryText := fmt.Sprintf("The following is a summary of the earlier conversation:\n\n%s\n\n---\nConversation continues below:", summary)
	if summaryNeedsContinuationPrompt(msgs) {
		return fmt.Sprintf(continuationMarkerTemplate, summaryText)
	}
	return summaryText
}

func summaryNeedsContinuationPrompt(msgs []message.Message) bool {
	if len(msgs) == 0 {
		return false
	}
	normalized := sanitizeToolCallHistory(msgs)
	if len(normalized) == 0 {
		return false
	}
	lastMsg := normalized[len(normalized)-1]
	if lastMsg.Role == message.Tool && len(lastMsg.ToolResults()) > 0 {
		return true
	}
	return lastMsg.Role == message.Assistant && len(lastMsg.ToolCalls()) > 0
}

// compactContext summarizes the conversation history to reduce context size.
// It creates a summary message and sets it as the session's SummaryMessageID so
// subsequent calls to processGeneration will start from the summary.

func (a *agent) compactContext(ctx context.Context, sessionID string) error {
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to list messages for compaction: %w", err)
	}

	if len(msgs) < 4 {
		return nil // not enough messages to compact
	}

	result, err := a.generateAndPersistSummary(ctx, sessionID, summaryModeCompaction)
	if err != nil {
		return fmt.Errorf("failed to generate compaction summary: %w", err)
	}

	logging.InfoPersist(fmt.Sprintf("Context compacted: %d messages summarized, SummaryMessageID: %s", len(msgs), result.message.ID))
	return nil
}

func (a *agent) prepareProvider(ctx context.Context, userPrompt string, personaContent string) (provider.Provider, error) {
	logging.Debug("prepareProvider", "agentName", string(a.agentName), "hasSkillManager", a.skillManager != nil, "hasPersona", personaContent != "")
	if isCleanModeContext(ctx) {
		return createAgentProvider(ctx, a.agentName, []tools.BaseTool{cleanModeCatalogTool{}}, nil, nil, "")
	}

	// When there is no skill manager, no persona and no active session policy, use
	// the pre-built provider as-is. Ponytail and Superpowers must still be honored
	// here because they inject per-turn even without a skill manager.
	if a.skillManager == nil && personaContent == "" && !sessionPolicyActive(ctx) {
		return a.provider, nil
	}

	var activeSkillInstructions []string
	if a.contextManager != nil {
		for _, skillName := range a.contextManager.ShouldActivate(userPrompt) {
			instructions, err := a.contextManager.ActivateSkill(skillName)
			if err != nil {
				return nil, fmt.Errorf("activate skill %q: %w", skillName, err)
			}
			if injected := prompt.InjectSkillInstructions(skillName, instructions); injected != "" {
				activeSkillInstructions = append(activeSkillInstructions, injected)
			}
		}
	}

	activeSkillInstructions = append(activeSkillInstructions, sessionPolicyInstructions(ctx)...)

	return createAgentProvider(ctx, a.agentName, a.tools, a.skillManager, activeSkillInstructions, personaContent)
}

// sessionPolicyActive reports whether any per-session prompt policy (Ponytail,
// Superpowers) is active for the request context. It gates the prepareProvider
// fast path that would otherwise reuse the pre-built provider.
func sessionPolicyActive(ctx context.Context) bool {
	return ponytailModeForContext(ctx).IsActive() || superpowersEnabledForContext(ctx)
}

// sessionPolicyInstructions returns the per-session policy rulesets to inject
// after any automatically activated skill, in a deliberate order: the
// Superpowers development lifecycle first (it governs how work is approached),
// then Ponytail (it governs how much gets built). Both remain subordinate to
// direct user instructions and AGENTS.md, which the rulesets state explicitly.
func sessionPolicyInstructions(ctx context.Context) []string {
	var injected []string

	// Superpowers mode (per-session, ctx-threaded): the opt-in disciplined
	// development workflow, injected before each turn like any other skill.
	if superpowersEnabledForContext(ctx) {
		if text := prompt.InjectSkillInstructions("superpowers", superpowers.Instructions()); text != "" {
			injected = append(injected, text)
		}
	}

	// Ponytail mode (per-session, ctx-threaded): when active, inject the
	// "lazy senior developer" ruleset before each turn just like any other skill.
	if mode := ponytailModeForContext(ctx); mode.IsActive() {
		if text := prompt.InjectSkillInstructions("ponytail ("+mode.String()+")", ponytail.Instructions(mode)); text != "" {
			injected = append(injected, text)
		}
	}

	return injected
}

func createAgentProvider(ctx context.Context, agentName config.AgentName, agentTools []tools.BaseTool, skillManager *skills.SkillManager, activeSkillInstructions []string, personaContent ...string) (provider.Provider, error) {
	logging.Debug("createAgentProvider", "agentName", string(agentName))
	cfg := config.Get()
	agentConfig, ok := cfg.Agents[agentName]
	if !ok {
		return nil, fmt.Errorf("agent %s not found", agentName)
	}
	// Per-session model override (request-scoped): when this session selected a
	// specific model, use it instead of the agent's globally configured model.
	// agentConfig is a copy of the map value, so this never mutates global config
	// and is safe for concurrent sessions running different models in parallel.
	sessionOverrides := sessionLLMOverridesForContext(ctx)
	if sessionOverrides.Model != "" {
		if _, supported := models.SupportedModels[sessionOverrides.Model]; supported {
			agentConfig.Model = sessionOverrides.Model
		} else {
			logging.Debug("createAgentProvider: ignoring unsupported session model override", "model", sessionOverrides.Model)
		}
	}
	model, ok := models.SupportedModels[agentConfig.Model]
	if !ok {
		return nil, fmt.Errorf("model %s not supported", agentConfig.Model)
	}
	logging.Debug("createAgentProvider", "agentName", string(agentName), "model", agentConfig.Model, "provider", model.Provider)

	// Resolve the provider account: by AccountID if set, otherwise by provider type.
	var acc *config.ProviderAccount
	var err error
	needsConcreteAccount := model.AccountID != "" || model.Provider != models.ProviderAntigravity
	if needsConcreteAccount {
		if model.AccountID != "" {
			acc, err = config.ResolveProviderAccountByID(model.AccountID)
		} else {
			acc, err = config.ResolveProviderAccountForType(model.Provider)
		}
		if err != nil {
			return nil, fmt.Errorf("could not resolve provider account: %w", err)
		}
		if acc.Disabled {
			return nil, fmt.Errorf("provider account %q is disabled", acc.ID)
		}
	} else {
		acc = &config.ProviderAccount{Type: models.ProviderAntigravity}
	}

	maxTokens := config.ResolveAgentMaxTokens(agentName, agentConfig, model)

	pc := ""
	if len(personaContent) > 0 {
		pc = personaContent[0]
	}
	systemMessage := buildSystemMessage(ctx, agentName, model.Provider, agentTools, skillManager, activeSkillInstructions, pc)

	// For models with special provider options (reasoning effort, thinking mode),
	// build opts explicitly using resolved account credentials. sessionOverrides
	// was resolved above (also carries the per-session model override).
	needsExtraOpts := (model.Provider == models.ProviderOpenAI && model.CanReason) ||
		(model.Provider == models.ProviderCopilot && model.CanReason) ||
		(model.Provider == models.ProviderLocal && model.CanReason) ||
		(model.Provider == models.ProviderAnthropic && model.CanReason)

	if needsExtraOpts {
		opts := []provider.ProviderClientOption{
			provider.WithAPIKey(acc.APIKey),
			provider.WithUseOAuth(acc.UseOAuth),
			provider.WithModel(model),
			provider.WithSystemMessage(systemMessage),
			provider.WithMaxTokens(maxTokens),
		}
		if (model.Provider == models.ProviderOpenAI || model.Provider == models.ProviderLocal) && model.CanReason {
			opts = append(opts, provider.WithOpenAIOptions(
				provider.WithReasoningEffort(effectiveReasoningEffort(agentConfig, sessionOverrides)),
			))
		}
		if model.Provider == models.ProviderCopilot && model.CanReason {
			opts = append(opts, provider.WithCopilotOptions(
				provider.WithCopilotReasoningEffort(effectiveReasoningEffort(agentConfig, sessionOverrides)),
			))
		}
		if model.Provider == models.ProviderAnthropic && model.CanReason {
			opts = append(opts, provider.WithAnthropicOptions(
				provider.WithAnthropicThinkingMode(effectiveAnthropicThinkingMode(model, agentConfig, sessionOverrides)),
				provider.WithAnthropicReasoningEffort(effectiveReasoningEffort(agentConfig, sessionOverrides)),
			))
		}
		if acc.BaseURL != "" && model.Provider == models.ProviderOllama {
			opts = append(opts, provider.WithOpenAIOptions(
				provider.WithOpenAIBaseURL(models.ResolveOllamaBaseURL(acc.BaseURL)),
			))
		}
		// Apply cache-disable options from global config
		anthCacheOpts, oaiCacheOpts, gemCacheOpts := provider.CacheDisabledOptions()
		if len(anthCacheOpts) > 0 {
			opts = append(opts, provider.WithAnthropicOptions(anthCacheOpts...))
		}
		if len(oaiCacheOpts) > 0 {
			opts = append(opts, provider.WithOpenAIOptions(oaiCacheOpts...))
		}
		if len(gemCacheOpts) > 0 {
			opts = append(opts, provider.WithGeminiOptions(gemCacheOpts...))
		}
		agentProvider, err := provider.NewProvider(model.Provider, opts...)
		if err != nil {
			return nil, fmt.Errorf("could not create provider: %v", err)
		}
		return agentProvider, nil
	}

	agentProvider, err := provider.NewProviderFromAccount(*acc, model, maxTokens, systemMessage)
	if err != nil {
		return nil, fmt.Errorf("could not create provider: %w", err)
	}
	return agentProvider, nil
}

func defaultAnthropicThinkingMode(model models.Model, configuredMode config.ThinkingMode) config.ThinkingMode {
	if model.Provider == models.ProviderAnthropic && model.CanReason && configuredMode == "" {
		return config.ThinkingMedium
	}
	return configuredMode
}

func buildSystemMessage(
	ctx context.Context,
	agentName config.AgentName,
	modelProvider models.ModelProvider,
	agentTools []tools.BaseTool,
	skillManager *skills.SkillManager,
	activeSkillInstructions []string,
	personaContent string,
) string {
	if isCleanModeContext(ctx) {
		return ""
	}
	cfg := config.Get()
	if cfg == nil {
		return prompt.GetAgentPrompt(agentName, modelProvider, globalLuaManager)
	}

	skillsMetadata := ""
	if skillManager != nil && (agentName == config.AgentCoder || agentName == config.AgentTask) {
		skillsMetadata = prompt.InjectSkillsMetadata(skillManager.GetAllMetadata())
	}

	buildCtx := ctx
	if buildCtx == nil {
		buildCtx = context.Background()
	}

	systemMessage, err := prompt.BuildPrompt(buildCtx, agentName, modelProvider, globalLuaManager,
		prompt.WithEnvironment(cfg.WorkingDir, isGitRepo(cfg.WorkingDir), goruntime.GOOS, time.Now().Format("2006-01-02 15:04:05 MST")),
		prompt.WithGitInfo(getGitBranch(cfg.WorkingDir), "", ""),
		prompt.WithMCPServers(promptMCPServerNames(cfg)),
		prompt.WithTools(promptToolNames(agentTools)),
		prompt.WithContextFiles(prompt.LoadContextFilesFromConfig()),
		prompt.WithSkills(skillsMetadata, activeSkillInstructions),
	)
	if err != nil {
		logging.Warn("Template prompt build failed; falling back to legacy system prompt", "agent", string(agentName), "error", err)
		systemMessage = prompt.GetAgentPrompt(agentName, modelProvider, globalLuaManager)
	} else {
		logging.Debug("Self-improvement prompt build completed",
			"agent", string(agentName),
			"provider", string(modelProvider),
			"prompt_length", len(systemMessage),
		)
	}

	sections := make([]string, 0, 2)

	// Persona instructions are appended to the system prompt so they take effect
	// without polluting the user message. They are added last so they can override
	// or extend the base identity/workflow instructions.
	if personaContent != "" {
		sections = append(sections, personaContent)
	}

	// Non-interactive mode: instruct agents to act autonomously without user feedback.
	if globalNonInteractive {
		sections = append(sections, nonInteractiveInstructions)
	}

	// Memories block is prepended before the main system prompt so the model treats
	// stored memories as pre-loaded knowledge rather than appended context.
	finalPrompt := systemMessage
	if globalMemoryInjector != nil {
		memBlock := globalMemoryInjector.BuildMemoryBlock(ctx, "")
		if memBlock != "" {
			finalPrompt = memBlock + "\n\n" + finalPrompt
		}
	}
	if len(sections) > 0 {
		finalPrompt += "\n\n" + strings.Join(sections, "\n\n")
	}
	logging.Debug("System prompt built",
		"agent", string(agentName),
		"length", len(finalPrompt),
		"sections", len(sections),
		"system_prompt", finalPrompt,
	)
	return finalPrompt
}

func promptMcpCatalogListing(ctx context.Context) string {
	toolNames := promptToolNames(GetMcpTools(ctx, permission.NewPermissionService()))
	return prompt.FormatMCPToolsForPrompt(toolNames)
}

func promptMCPServerNames(cfg *config.Config) []string {
	if cfg == nil || len(cfg.MCPServers) == 0 {
		return nil
	}

	names := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func promptToolNames(agentTools []tools.BaseTool) []string {
	if len(agentTools) == 0 {
		return nil
	}

	names := make([]string, 0, len(agentTools))
	for _, tool := range agentTools {
		if tool == nil {
			continue
		}
		name := strings.TrimSpace(tool.Info().Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func getGitBranch(dir string) string {
	branchFile := filepath.Join(dir, ".git", "HEAD")
	data, err := os.ReadFile(branchFile)
	if err != nil {
		return ""
	}

	line := strings.TrimSpace(string(data))
	const prefix = "ref: refs/heads/"
	if strings.HasPrefix(line, prefix) {
		return strings.TrimPrefix(line, prefix)
	}
	if len(line) >= 8 {
		return line[:8]
	}
	return line
}

func isGitRepo(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func effectiveMaxTokens(agentName config.AgentName, model models.Model) int {
	agentCfg := config.Agent{}
	if cfg := config.Get(); cfg != nil {
		if ac, ok := cfg.Agents[agentName]; ok {
			agentCfg = ac
		}
	}
	return int(config.ResolveAgentMaxTokens(agentName, agentCfg, model))
}

// estimateMessageTokens returns a rough token estimate for a single message.
// Uses the heuristic of 1 token ≈ 4 UTF-8 bytes.
func estimateMessageTokens(msg message.Message) int64 {
	chars := int64(len(msg.Content().Text))
	for _, tc := range msg.ToolCalls() {
		chars += int64(len(tc.Input))
	}
	return chars / 4
}

// trimMessagesToContextBudget trims the message slice from the oldest end so that
// the estimated token count fits within contextWindow * fraction.
// A minimum of 2 messages is always preserved.
// If contextWindow <= 0, msgs is returned unchanged.
func trimMessagesToContextBudget(msgs []message.Message, contextWindow int64, fraction float64) []message.Message {
	if contextWindow <= 0 || len(msgs) == 0 || fraction <= 0 {
		return msgs
	}
	maxTokens := int64(float64(contextWindow) * fraction)

	var estimatedTokens int64
	for _, msg := range msgs {
		estimatedTokens += estimateMessageTokens(msg)
	}

	if estimatedTokens <= maxTokens {
		return msgs
	}

	original := len(msgs)
	for len(msgs) > 2 && estimatedTokens > maxTokens {
		estimatedTokens -= estimateMessageTokens(msgs[0])
		msgs = msgs[1:]
	}

	if len(msgs) < original {
		logging.InfoPersist(fmt.Sprintf(
			"Message history trimmed for context budget: %d→%d messages (~%d tokens, budget=%.0f%% of %d)",
			original, len(msgs), estimatedTokens, fraction*100, contextWindow,
		))
	}
	return msgs
}

// trimTextToContextWindow trims text from the beginning so that its estimated
// token count fits within contextWindow - reservation tokens.
// Returns the original text if contextWindow <= 0.
func trimTextToContextWindow(text string, contextWindow int64, reservation int64) string {
	if contextWindow <= 0 {
		return text
	}
	available := contextWindow - reservation
	if available <= 0 {
		available = contextWindow / 2
	}
	maxChars := available * 4
	if int64(len(text)) <= maxChars {
		return text
	}
	// Keep the most recent portion (tail) of the text.
	trimmed := text[int64(len(text))-maxChars:]
	// Advance to the next newline to avoid cutting in mid-sentence.
	if idx := strings.IndexByte(trimmed, '\n'); idx > 0 && idx < 200 {
		trimmed = trimmed[idx+1:]
	}
	logging.InfoPersist(fmt.Sprintf(
		"Conversation text trimmed for compaction: %d→%d chars (context window=%d tokens)",
		len(text), len(trimmed), contextWindow,
	))
	return trimmed
}

// CreateAgentProvider creates a provider for the given agent name.
// It is exported for use in app-layer code that needs a provider outside of the agent itself
// (e.g. the context-enricher LLM planner adapter).
func CreateAgentProvider(ctx context.Context, agentName config.AgentName) (provider.Provider, error) {
	return createAgentProvider(ctx, agentName, nil, nil, nil)
}
