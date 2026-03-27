package agent

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/digiogithub/pando/internal/session"
	"github.com/digiogithub/pando/internal/skills"
)

// Common errors
var (
	ErrRequestCancelled = errors.New("request cancelled by user")
	ErrSessionBusy      = errors.New("session is currently processing another request")
)

type AgentEventType string

const (
	AgentEventTypeError          AgentEventType = "error"
	AgentEventTypeResponse       AgentEventType = "response"
	AgentEventTypeSummarize      AgentEventType = "summarize"
	AgentEventTypeContentDelta   AgentEventType = "content_delta"
	AgentEventTypeThinkingDelta  AgentEventType = "thinking_delta"
	AgentEventTypeToolCall       AgentEventType = "tool_call"
	AgentEventTypeToolResult     AgentEventType = "tool_result"
)

type AgentEvent struct {
	Type    AgentEventType
	Message message.Message
	Error   error
	Delta   string
	ToolCall   *message.ToolCall
	ToolResult *message.ToolResult

	// When summarizing
	SessionID string
	Progress  string
	Done      bool
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
}

type agent struct {
	*pubsub.Broker[AgentEvent]
	sessions session.Service
	messages message.Service

	tools    []tools.BaseTool
	provider provider.Provider

	titleProvider     provider.Provider
	summarizeProvider provider.Provider
	agentName         config.AgentName
	skillManager      *skills.SkillManager
	contextManager    *skills.ContextManager
	luaMgr            *luaengine.FilterManager

	activeRequests sync.Map
}

func NewAgent(
	agentName config.AgentName,
	sessions session.Service,
	messages message.Service,
	agentTools []tools.BaseTool,
	skillManager *skills.SkillManager,
) (Service, error) {
	agentProvider, err := createAgentProvider(agentName, skillManager, nil)
	if err != nil {
		// If the model is not yet available (e.g. dynamic models not fetched yet),
		// create the agent without a provider. The TUI will prompt the user to select a model.
		logging.Warn("Agent provider not available, model selection required", "agent", agentName, "error", err)
		agentProvider = nil
	}

	var titleProvider provider.Provider
	// Only generate titles for the coder agent
	if agentName == config.AgentCoder {
		titleProvider, err = createAgentProvider(config.AgentTitle, nil, nil)
		if err != nil {
			logging.Debug("Title agent provider not available", "error", err)
			titleProvider = nil
		}
	}
	var summarizeProvider provider.Provider
	if agentName == config.AgentCoder {
		summarizeProvider, err = createAgentProvider(config.AgentSummarizer, nil, nil)
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
		Broker:            pubsub.NewBroker[AgentEvent](),
		provider:          agentProvider,
		messages:          messages,
		sessions:          sessions,
		tools:             agentTools,
		titleProvider:     titleProvider,
		summarizeProvider: summarizeProvider,
		agentName:         agentName,
		skillManager:      skillManager,
		contextManager:    contextManager,
		activeRequests:    sync.Map{},
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

	// Auto persona selection: a lite model picks the best persona and prepends it.
	if globalPersonaSelector != nil {
		content = globalPersonaSelector.SelectAndApply(ctx, content)
	}

	userMsg, err := a.createUserMessage(ctx, sessionID, content, attachmentParts)
	if err != nil {
		return a.err(fmt.Errorf("failed to create user message: %w", err))
	}
	// Append the new user message to the conversation history.
	msgHistory := append(msgs, userMsg)

	requestProvider, err := a.prepareProvider(content)
	if err != nil {
		return a.err(fmt.Errorf("failed to prepare agent provider: %w", err))
	}

	sawToolRound := false

	for {
		// Check for cancellation before each iteration
		select {
		case <-ctx.Done():
			return a.err(ctx.Err())
		default:
			// Continue processing
		}
		logging.Debug("processGeneration iteration", "sessionID", sessionID, "historyLength", len(msgHistory))
		agentMessage, toolResults, err := a.streamAndHandleEvents(ctx, sessionID, msgHistory, requestProvider, sawToolRound)
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
			sawToolRound = true
			msgHistory = append(msgHistory, agentMessage, *toolResults)

			// Check if we should auto-compact context before continuing the loop
			if sess, sessErr := a.sessions.Get(ctx, sessionID); sessErr == nil && a.shouldCompact(sess.PromptTokens) {
				a.publishEvent(AgentEvent{
					Type:  AgentEventTypeResponse,
					Delta: "\n\n⚡ Auto-compacting context to free space...\n",
				})
				if compactErr := a.compactContext(ctx, sessionID); compactErr != nil {
					logging.Warn("Context compaction failed", "error", compactErr)
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
					a.publishEvent(AgentEvent{
						Type:  AgentEventTypeResponse,
						Delta: "✓ Context compacted. Continuing...\n\n",
					})
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

func (a *agent) streamAndHandleEvents(ctx context.Context, sessionID string, msgHistory []message.Message, requestProvider provider.Provider, emitContentDeltas bool) (message.Message, *message.Message, error) {
	logging.Debug("streamAndHandleEvents started", "sessionID", sessionID, "historyLength", len(msgHistory), "model", requestProvider.Model().ID)
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	if cache, ok := tools.GetSessionCacheByID(sessionID); ok {
		ctx = context.WithValue(ctx, tools.SessionCacheContextKey, cache)
	}
	eventChan := requestProvider.StreamResponse(ctx, msgHistory, a.tools)

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
	for event := range eventChan {
		if processErr := a.processEvent(ctx, sessionID, &assistantMsg, event, requestProvider, emitContentDeltas); processErr != nil {
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
	for i, toolCall := range toolCalls {
		select {
		case <-ctx.Done():
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
				continue
			}
			toolResult, toolErr := tool.Run(ctx, tools.ToolCall{
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
					for j := i + 1; j < len(toolCalls); j++ {
						toolResults[j] = message.ToolResult{
							ToolCallID: toolCalls[j].ID,
							Name:       toolCalls[j].Name,
							Content:    "Tool execution canceled by user",
							IsError:    true,
						}
						a.publishEvent(AgentEvent{Type: AgentEventTypeToolResult, SessionID: sessionID, ToolResult: &toolResults[j]})
					}
					a.finishMessage(ctx, &assistantMsg, message.FinishReasonPermissionDenied)
					break
				}
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
	emitContentDeltas bool,
) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing.
	}

	switch event.Type {
	case provider.EventThinkingDelta:
		logging.Debug("Event: ThinkingDelta", "sessionID", sessionID, "contentLength", len(event.Content))
		assistantMsg.AppendReasoningContent(event.Content)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventContentDelta:
		logging.Debug("Event: ContentDelta", "sessionID", sessionID, "contentLength", len(event.Content))
		assistantMsg.AppendContent(event.Content)
		if emitContentDeltas {
			a.publishEvent(AgentEvent{Type: AgentEventTypeContentDelta, SessionID: sessionID, Delta: event.Content})
		}
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventToolUseStart:
		logging.Debug("Event: ToolUseStart", "sessionID", sessionID, "toolName", event.ToolCall.Name, "toolID", event.ToolCall.ID)
		assistantMsg.AddToolCall(*event.ToolCall)
		a.publishEvent(AgentEvent{Type: AgentEventTypeToolCall, SessionID: sessionID, ToolCall: event.ToolCall})
		return a.messages.Update(ctx, *assistantMsg)
	// TODO: see how to handle this
	// case provider.EventToolUseDelta:
	// 	tm := time.Unix(assistantMsg.UpdatedAt, 0)
	// 	assistantMsg.AppendToolCallInput(event.ToolCall.ID, event.ToolCall.Input)
	// 	if time.Since(tm) > 1000*time.Millisecond {
	// 		err := a.messages.Update(ctx, *assistantMsg)
	// 		assistantMsg.UpdatedAt = time.Now().Unix()
	// 		return err
	// 	}
	case provider.EventToolUseStop:
		logging.Debug("Event: ToolUseStop", "sessionID", sessionID, "toolID", event.ToolCall.ID)
		assistantMsg.FinishToolCall(event.ToolCall.ID)
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
		assistantMsg.SetToolCalls(event.Response.ToolCalls)
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

	provider, err := createAgentProvider(agentName, a.skillManager, nil)
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
	if a.summarizeProvider == nil {
		return fmt.Errorf("summarize provider not available")
	}

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
		// Get all messages from the session
		msgs, err := a.messages.List(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to list messages: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		summarizeCtx = context.WithValue(summarizeCtx, tools.SessionIDContextKey, sessionID)

		if len(msgs) == 0 {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("no messages to summarize"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Analyzing conversation...",
		}
		a.Publish(pubsub.CreatedEvent, event)

		// Add a system message to guide the summarization
		summarizePrompt := "Provide a detailed but concise summary of our conversation above. Focus on information that would be helpful for continuing the conversation, including what we did, what we're doing, which files we're working on, and what we're going to do next."

		// Create a new message with the summarize prompt
		promptMsg := message.Message{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: summarizePrompt}},
		}

		// Append the prompt to the messages
		msgsWithPrompt := append(msgs, promptMsg)

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Generating summary...",
		}

		a.Publish(pubsub.CreatedEvent, event)

		// Send the messages to the summarize provider
		response, err := a.summarizeProvider.SendMessages(
			summarizeCtx,
			msgsWithPrompt,
			make([]tools.BaseTool, 0),
		)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to summarize: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		summary := strings.TrimSpace(response.Content)
		if summary == "" {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("empty summary returned"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Creating new session...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		oldSession, err := a.sessions.Get(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to get session: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		// Create a message in the new session with the summary
		msg, err := a.messages.Create(summarizeCtx, oldSession.ID, message.CreateMessageParams{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: summary},
				message.Finish{
					Reason: message.FinishReasonEndTurn,
					Time:   time.Now().Unix(),
				},
			},
			Model: a.summarizeProvider.Model().ID,
		})
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to create summary message: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		oldSession.SummaryMessageID = msg.ID
		oldSession.CompletionTokens = response.Usage.OutputTokens
		oldSession.PromptTokens = 0
		model := a.summarizeProvider.Model()
		usage := response.Usage
		cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
			model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
			model.CostPer1MIn/1e6*float64(usage.InputTokens) +
			model.CostPer1MOut/1e6*float64(usage.OutputTokens)
		oldSession.Cost += cost
		_, err = a.sessions.Save(summarizeCtx, oldSession)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to save session: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
		}

		event = AgentEvent{
			Type:      AgentEventTypeSummarize,
			SessionID: oldSession.ID,
			Progress:  "Summary complete",
			Done:      true,
		}
		a.Publish(pubsub.CreatedEvent, event)
		// Send final success event with the new session ID
	}()

	return nil
}

// shouldCompact returns true if the context usage exceeds the auto-compact threshold.
func (a *agent) shouldCompact(usedTokens int64) bool {
	cfg := config.Get()
	agentCfg, ok := cfg.Agents[a.agentName]
	if !ok || !agentCfg.AutoCompact {
		return false
	}
	threshold := agentCfg.AutoCompactThreshold
	if threshold <= 0 {
		threshold = 0.85
	}
	contextWindow := a.provider.Model().ContextWindow
	if contextWindow <= 0 {
		return false
	}
	return float64(usedTokens) >= float64(contextWindow)*threshold
}

// compactContext summarizes the conversation history to reduce context size.
// It creates a summary message and sets it as the session's SummaryMessageID so
// subsequent calls to processGeneration will start from the summary.
func (a *agent) compactContext(ctx context.Context, sessionID string) error {
	if a.summarizeProvider == nil {
		return fmt.Errorf("no summarizer provider available for compaction")
	}

	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to list messages for compaction: %w", err)
	}

	if len(msgs) < 4 {
		return nil // not enough messages to compact
	}

	// Build conversation text for summarization (text content only)
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

	compactionPrompt := `Create a structured summary of the conversation to replace the conversation history. Include:
1. Task Overview: user's core request, success criteria, any constraints
2. Current State: completed work, files modified (with paths), key outputs produced
3. Important Discoveries: technical constraints, decisions made, errors resolved
4. Next Steps: remaining work, pending decisions or blockers

Be concise but complete. This summary will replace the conversation history.`

	sendCtx := context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	response, err := a.summarizeProvider.SendMessages(
		sendCtx,
		[]message.Message{
			{
				Role:  message.User,
				Parts: []message.ContentPart{message.TextContent{Text: compactionPrompt + "\n\nConversation to summarize:\n" + convText.String()}},
			},
		},
		[]tools.BaseTool{},
	)
	if err != nil {
		return fmt.Errorf("failed to generate compaction summary: %w", err)
	}

	summary := strings.TrimSpace(response.Content)
	if summary == "" {
		return fmt.Errorf("empty compaction summary returned")
	}

	summaryText := fmt.Sprintf("The following is a summary of the earlier conversation:\n\n%s\n\n---\nConversation continues below:", summary)

	summaryMsg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: summaryText},
			message.Finish{
				Reason: message.FinishReasonEndTurn,
				Time:   time.Now().Unix(),
			},
		},
		Model: a.summarizeProvider.Model().ID,
	})
	if err != nil {
		return fmt.Errorf("failed to create compaction summary message: %w", err)
	}

	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session for compaction: %w", err)
	}
	sess.SummaryMessageID = summaryMsg.ID
	if _, err = a.sessions.Save(ctx, sess); err != nil {
		return fmt.Errorf("failed to save session after compaction: %w", err)
	}

	logging.InfoPersist(fmt.Sprintf("Context compacted: %d messages summarized, SummaryMessageID: %s", len(msgs), summaryMsg.ID))
	return nil
}

func (a *agent) prepareProvider(userPrompt string) (provider.Provider, error) {
	logging.Debug("prepareProvider", "agentName", string(a.agentName), "hasSkillManager", a.skillManager != nil)
	if a.skillManager == nil {
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

	return createAgentProvider(a.agentName, a.skillManager, activeSkillInstructions)
}

func createAgentProvider(agentName config.AgentName, skillManager *skills.SkillManager, activeSkillInstructions []string) (provider.Provider, error) {
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

	providerCfg, ok := cfg.Providers[model.Provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not supported", model.Provider)
	}
	if providerCfg.Disabled {
		return nil, fmt.Errorf("provider %s is not enabled", model.Provider)
	}
	maxTokens := model.DefaultMaxTokens
	if agentConfig.MaxTokens > 0 {
		maxTokens = agentConfig.MaxTokens
	}
	opts := []provider.ProviderClientOption{
		provider.WithAPIKey(providerCfg.APIKey),
		provider.WithModel(model),
		provider.WithSystemMessage(buildSystemMessage(agentName, model.Provider, skillManager, activeSkillInstructions)),
		provider.WithMaxTokens(maxTokens),
	}
	if model.Provider == models.ProviderOpenAI || model.Provider == models.ProviderLocal && model.CanReason {
		opts = append(
			opts,
			provider.WithOpenAIOptions(
				provider.WithReasoningEffort(agentConfig.ReasoningEffort),
			),
		)
	}
	if model.Provider == models.ProviderOllama {
		opts = append(
			opts,
			provider.WithOpenAIOptions(
				provider.WithOpenAIBaseURL(models.ResolveOllamaBaseURL(providerCfg.BaseURL)),
			),
		)
	} else if model.Provider == models.ProviderAnthropic && model.CanReason && agentName == config.AgentCoder {
		opts = append(
			opts,
			provider.WithAnthropicOptions(
				provider.WithAnthropicShouldThinkFn(provider.DefaultShouldThinkFn),
			),
		)
	}
	agentProvider, err := provider.NewProvider(
		model.Provider,
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create provider: %v", err)
	}

	return agentProvider, nil
}

func buildSystemMessage(
	agentName config.AgentName,
	modelProvider models.ModelProvider,
	skillManager *skills.SkillManager,
	activeSkillInstructions []string,
) string {
	systemMessage := prompt.GetAgentPrompt(agentName, modelProvider, globalLuaManager)
	if skillManager == nil || (agentName != config.AgentCoder && agentName != config.AgentTask) {
		return systemMessage
	}

	sections := make([]string, 0, 2+len(activeSkillInstructions))
	if skillsMetadata := prompt.InjectSkillsMetadata(skillManager.GetAllMetadata()); skillsMetadata != "" {
		sections = append(sections, skillsMetadata)
	}
	sections = append(sections, activeSkillInstructions...)
	if len(sections) == 0 {
		return systemMessage
	}

	return systemMessage + "\n\n" + strings.Join(sections, "\n\n")
}

func effectiveMaxTokens(agentName config.AgentName, model models.Model) int {
	if cfg := config.Get(); cfg != nil {
		if agentConfig, ok := cfg.Agents[agentName]; ok && agentConfig.MaxTokens > 0 {
			return int(agentConfig.MaxTokens)
		}
	}
	return int(model.DefaultMaxTokens)
}
