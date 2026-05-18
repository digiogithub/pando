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
	"github.com/digiogithub/pando/internal/pubsub"
	"github.com/digiogithub/pando/internal/runtime"
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/skills"
)

// Common errors
var (
	ErrRequestCancelled = errors.New("request cancelled by user")
	ErrSessionBusy      = errors.New("session is currently processing another request")
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
)

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
	Cancel(sessionID string)
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
}

func (a *agent) Cancel(sessionID string) {
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
	logging.Debug("Agent.Run called", "sessionID", sessionID, "contentLength", len(content), "attachmentCount", len(attachments))
	if a.provider == nil {
		return nil, ErrNoModel
	}
	if !a.provider.Model().SupportsAttachments && attachments != nil {
		attachments = nil
	}
	events := make(chan AgentEvent, 256)
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
		cancel()
		a.publishEvent(result)
		events <- result
		close(events)
	}()
	return events, nil
}

// sanitizeToolCallHistory ensures that every assistant message with tool_calls
// is followed by a tool message that covers all the corresponding tool_call_ids.
// When a session is interrupted mid-tool-execution the tool results message may
// never have been saved, leaving an invalid history that OpenAI-compatible APIs
// reject with a 400 error. This function inserts ephemeral synthetic results for
// any uncovered tool_call_ids so the resumed conversation is accepted.
func sanitizeToolCallHistory(msgs []message.Message) []message.Message {
	result := make([]message.Message, 0, len(msgs))
	for i, msg := range msgs {
		result = append(result, msg)
		if msg.Role != message.Assistant {
			continue
		}
		toolCalls := msg.ToolCalls()
		if len(toolCalls) == 0 {
			continue
		}
		// Collect tool_call_ids already covered by the immediately following tool message.
		covered := make(map[string]bool)
		if i+1 < len(msgs) && msgs[i+1].Role == message.Tool {
			for _, tr := range msgs[i+1].ToolResults() {
				covered[tr.ToolCallID] = true
			}
		}
		// Build synthetic results for any uncovered tool_call_ids.
		var syntheticParts []message.ContentPart
		for _, tc := range toolCalls {
			if !covered[tc.ID] {
				tr := message.ToolResult{
					ToolCallID: tc.ID,
					Name:       tc.Name,
					Content:    "Tool execution was interrupted",
					IsError:    true,
				}
				syntheticParts = append(syntheticParts, tr)
			}
		}
		if len(syntheticParts) > 0 {
			logging.Debug("sanitizeToolCallHistory: inserting synthetic tool results",
				"assistantMsgID", msg.ID,
				"count", len(syntheticParts),
			)
			result = append(result, message.Message{
				Role:  message.Tool,
				Parts: syntheticParts,
			})
		}
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
	if session.SummaryMessageID != "" {
		summaryMsgInex := -1
		for i, msg := range msgs {
			if msg.ID == session.SummaryMessageID {
				summaryMsgInex = i
				break
			}
		}
		if summaryMsgInex != -1 {
			msgs = msgs[summaryMsgInex:]
			msgs[0].Role = message.User
		}
	}

	// Trim message history to fit within 40% of the current model's context window.
	// This prevents overflowing when the model was switched to one with a smaller
	// context window mid-conversation. The 40% budget leaves ample room for the new
	// user message, tool calls, and the model's response.
	if a.provider != nil {
		contextWindow := a.provider.Model().ContextWindow
		cfg2 := config.Get()
		if agentCfg2, ok2 := cfg2.Agents[a.agentName]; ok2 && agentCfg2.ContextWindowOverride > 0 {
			contextWindow = agentCfg2.ContextWindowOverride
		}
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

	// Resolve persona content to inject into the system prompt.
	// This is done before creating the user message so the content (user query)
	// can be used for auto-selection without modifying the user message itself.
	personaContent := getPersonaContent(ctx, content)

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
	promptCtx := context.WithValue(ctx, prompt.SessionIDKey, sessionID)
	requestProvider, err := a.prepareProvider(promptCtx, content, personaContent)
	if err != nil {
		return a.err(fmt.Errorf("failed to prepare agent provider: %w", err))
	}

	for {
		// Check for cancellation before each iteration
		select {
		case <-ctx.Done():
			return a.err(ctx.Err())
		default:
			// Continue processing
		}
		logging.Debug("processGeneration iteration", "sessionID", sessionID, "historyLength", len(msgHistory))
		agentMessage, toolResults, err := a.streamAndHandleEvents(ctx, sessionID, msgHistory, requestProvider, eventCh)
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

			// Check if we should auto-compact context before continuing the loop
			if sess, sessErr := a.sessions.Get(ctx, sessionID); sessErr == nil && a.shouldCompact(sess) {
				compactMsg := "\n\n⚡ Auto-compacting context to free space...\n"
				a.publishEvent(AgentEvent{Type: AgentEventTypeContentDelta, SessionID: sessionID, Delta: compactMsg})
				select {
				case eventCh <- AgentEvent{Type: AgentEventTypeContentDelta, SessionID: sessionID, Delta: compactMsg}:
				default:
				}
				if compactErr := a.compactContext(ctx, sessionID); compactErr != nil {
					a.emitCompactionError(sessionID, compactErr, eventCh)
				} else {
					// Reload msgHistory from DB using the same SummaryMessageID logic
					if newMsgs, listErr := a.messages.List(ctx, sessionID); listErr == nil {
						if sess2, sessErr2 := a.sessions.Get(ctx, sessionID); sessErr2 == nil && sess2.SummaryMessageID != "" {
							for i, m := range newMsgs {
								if m.ID == sess2.SummaryMessageID {
									newMsgs = newMsgs[i:]
									newMsgs[0].Role = message.User
									break
								}
							}
						}
						msgHistory = newMsgs
					}
					doneMsg := "✓ Context compacted. Continuing...\n\n"
					a.publishEvent(AgentEvent{Type: AgentEventTypeContentDelta, SessionID: sessionID, Delta: doneMsg})
					select {
					case eventCh <- AgentEvent{Type: AgentEventTypeContentDelta, SessionID: sessionID, Delta: doneMsg}:
					default:
					}
				}
			}

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
	providerEventChan := requestProvider.StreamResponse(ctx, msgHistory, a.tools)

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
			for _, availableTool := range a.tools {
				if availableTool.Info().Name == toolCall.Name {
					tool = availableTool
					break
				}
				// Monkey patch for Copilot Sonnet-4 tool repetition obfuscation
				// if strings.HasPrefix(toolCall.Name, availableTool.Info().Name) &&
				// 	strings.HasPrefix(toolCall.Name, availableTool.Info().Name+availableTool.Info().Name) {
				// 	tool = availableTool
				// 	break
				// }
			}

			// Tool not found
			if tool == nil {
				toolResults[i] = message.ToolResult{
					ToolCallID: toolCall.ID,
					Name:       toolCall.Name,
					Content:    fmt.Sprintf("Tool not found: %s", toolCall.Name),
					IsError:    true,
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
			}
			a.publishEvent(AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[i]})
			select {
			case eventCh <- AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[i]}:
			default:
			}

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
		// Accumulate tool call input without sending to frontend (too frequent)
		assistantMsg.AppendToolCallInput(event.ToolCall.ID, event.ToolCall.Input)
	case provider.EventToolUseStop:
		logging.Debug("Event: ToolUseStop", "sessionID", sessionID, "toolID", event.ToolCall.ID)
		assistantMsg.FinishToolCall(event.ToolCall.ID)
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
		return a.TrackUsage(ctx, sessionID, requestProvider.Model(), event.Response.Usage)
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

func indentSummaryBlock(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

func (a *agent) buildCompactionContinuationSummary(msgs []message.Message, summary string) string {
	summaryText := fmt.Sprintf("The following is a summary of the earlier conversation:\n\n%s\n\n---\nConversation continues below:", summary)
	if wasInterruptedMidTool(msgs) {
		return fmt.Sprintf(continuationMarkerTemplate, summaryText)
	}
	return summaryText
}

func wasInterruptedMidTool(msgs []message.Message) bool {
	if len(msgs) == 0 {
		return false
	}
	lastMsg := msgs[len(msgs)-1]
	return len(lastMsg.ToolCalls()) > 0
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

	// When there is no skill manager and no persona, use the pre-built provider as-is.
	if a.skillManager == nil && personaContent == "" {
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

	return createAgentProvider(ctx, a.agentName, a.tools, a.skillManager, activeSkillInstructions, personaContent)
}

func createAgentProvider(ctx context.Context, agentName config.AgentName, agentTools []tools.BaseTool, skillManager *skills.SkillManager, activeSkillInstructions []string, personaContent ...string) (provider.Provider, error) {
	logging.Debug("createAgentProvider", "agentName", string(agentName))
	cfg := config.Get()
	agentConfig, ok := cfg.Agents[agentName]
	if !ok {
		return nil, fmt.Errorf("agent %s not found", agentName)
	}
	model, ok := models.SupportedModels[agentConfig.Model]
	if !ok {
		return nil, fmt.Errorf("model %s not supported", agentConfig.Model)
	}
	logging.Debug("createAgentProvider", "agentName", string(agentName), "model", agentConfig.Model, "provider", model.Provider)

	// Resolve the provider account: by AccountID if set, otherwise by provider type.
	var acc *config.ProviderAccount
	var err error
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

	maxTokens := config.ResolveAgentMaxTokens(agentName, agentConfig, model)

	pc := ""
	if len(personaContent) > 0 {
		pc = personaContent[0]
	}
	systemMessage := buildSystemMessage(ctx, agentName, model.Provider, agentTools, skillManager, activeSkillInstructions, pc)

	// For models with special provider options (reasoning effort, thinking mode),
	// build opts explicitly using resolved account credentials.
	needsExtraOpts := (model.Provider == models.ProviderOpenAI && model.CanReason) ||
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
				provider.WithReasoningEffort(agentConfig.ReasoningEffort),
			))
		}
		if model.Provider == models.ProviderAnthropic && model.CanReason {
			opts = append(opts, provider.WithAnthropicOptions(
				provider.WithAnthropicThinkingMode(defaultAnthropicThinkingMode(model, agentConfig.ThinkingMode)),
				provider.WithAnthropicReasoningEffort(agentConfig.ReasoningEffort),
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

	finalPrompt := systemMessage
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
