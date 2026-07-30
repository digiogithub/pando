package acp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/imageopt"
	"github.com/digiogithub/pando/internal/message"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

const (
	acpGroupedThinkingFlushInterval = 300 * time.Millisecond
	acpGroupedThinkingFlushChars    = 360
)

type cleanModeContextKey struct{}

// ACPModeContextKey marks a context as running under an ACP client session.
// Tools that import this package can read it to fall back to text-only behavior
// when a blocking interactive UI is unavailable (e.g. AskUserQuestion). The
// `tools` package already depends on `acp`, so this key is safe to read there
// without creating an import cycle.
type ACPModeContextKey struct{}

type groupedThinkingState struct {
	pending   strings.Builder
	lastFlush time.Time
}

func (s *groupedThinkingState) append(delta string, now time.Time) {
	if delta == "" {
		return
	}
	if s.lastFlush.IsZero() {
		s.lastFlush = now
	}
	s.pending.WriteString(delta)
}

func (s *groupedThinkingState) hasPending() bool {
	return s.pending.Len() > 0
}

func (s *groupedThinkingState) shouldFlush(now time.Time) bool {
	if !s.hasPending() {
		return false
	}
	if s.pending.Len() >= acpGroupedThinkingFlushChars {
		return true
	}
	return now.Sub(s.lastFlush) >= acpGroupedThinkingFlushInterval
}

func (s *groupedThinkingState) flushReason(now time.Time, force bool) string {
	if !s.hasPending() {
		return ""
	}
	if force {
		return "forced"
	}
	if s.pending.Len() >= acpGroupedThinkingFlushChars {
		return "size"
	}
	if now.Sub(s.lastFlush) >= acpGroupedThinkingFlushInterval {
		return "time"
	}
	return ""
}

func (s *groupedThinkingState) readyText(now time.Time, force bool) (string, string) {
	if !s.hasPending() {
		return "", ""
	}
	reason := s.flushReason(now, force)
	if reason == "" {
		return "", ""
	}
	return s.pending.String(), reason
}

func (s *groupedThinkingState) markFlushed(now time.Time) {
	s.pending.Reset()
	s.lastFlush = now
}

// imageOnlyPromptText is used when a client sends a prompt made only of
// attachments (e.g. a pasted screenshot with no caption). The agent needs a
// non-empty user turn, and an empty string would otherwise be rejected.
const imageOnlyPromptText = "(no text provided — see the attached image)"

// extractPromptContent extracts text and attachments from a Prompt (slice of ContentBlocks).
// Per the ACP content spec it handles text, image (capability `image`),
// resource_link and embedded resource blocks. Audio is ignored because the
// agent does not declare the `audio` capability.
func (a *PandoACPAgent) extractPromptContent(prompt []acpsdk.ContentBlock) (string, []message.Attachment, error) {
	if len(prompt) == 0 {
		return "", nil, fmt.Errorf("empty prompt content")
	}

	var textParts []string
	var attachments []message.Attachment

	for _, block := range prompt {
		switch {
		case block.Text != nil:
			textParts = append(textParts, block.Text.Text)

		case block.Image != nil:
			img := block.Image
			var data []byte
			if img.Data != "" {
				// base64-encoded inline image
				decoded, err := base64.StdEncoding.DecodeString(img.Data)
				if err != nil {
					a.logger.Printf("[ACP AGENT] Warning: failed to decode image data: %v", err)
					continue
				}
				data = decoded
			}
			mimeType := img.MimeType
			if mimeType == "" {
				mimeType = "image/png"
			}
			att := message.Attachment{MimeType: mimeType, Content: data}
			if img.Uri != nil {
				att.FilePath = uriToPath(*img.Uri)
				att.FileName = filepath.Base(att.FilePath)
			}
			attachments = append(attachments, att)

		case block.ResourceLink != nil:
			// Clients attach files by reference (Zed sends resource_link for
			// @-mentioned or dragged files). Inline the bytes when it is a
			// readable local image; otherwise mention the path as text so the
			// agent can open it with its own tools.
			link := block.ResourceLink
			path := uriToPath(link.Uri)
			mime := ""
			if link.MimeType != nil {
				mime = *link.MimeType
			}
			if att, ok := a.attachmentFromFile(path, mime); ok {
				attachments = append(attachments, att)
				continue
			}
			label := link.Name
			if label == "" {
				label = link.Uri
			}
			textParts = append(textParts, fmt.Sprintf("[attached file: %s (%s)]", label, link.Uri))

		case block.Resource != nil:
			res := block.Resource.Resource
			if res.TextResourceContents != nil {
				txt := res.TextResourceContents
				textParts = append(textParts, fmt.Sprintf("[attached %s]\n%s", txt.Uri, txt.Text))
				continue
			}
			if res.BlobResourceContents == nil {
				continue
			}
			blob := res.BlobResourceContents
			mime := ""
			if blob.MimeType != nil {
				mime = *blob.MimeType
			}
			decoded, err := base64.StdEncoding.DecodeString(blob.Blob)
			if err != nil {
				a.logger.Printf("[ACP AGENT] Warning: failed to decode embedded resource %s: %v", blob.Uri, err)
				continue
			}
			if mime == "" {
				mime = imageopt.DetectMIME(decoded)
			}
			if !strings.HasPrefix(mime, "image/") {
				// Binary non-image payloads cannot be sent to the model; point
				// the agent at the file instead of dropping it silently.
				textParts = append(textParts, fmt.Sprintf("[attached binary resource: %s (%s)]", blob.Uri, mime))
				continue
			}
			path := uriToPath(blob.Uri)
			attachments = append(attachments, message.Attachment{
				FilePath: path,
				FileName: filepath.Base(path),
				MimeType: mime,
				Content:  decoded,
			})
		}
	}

	if len(textParts) == 0 {
		// An attachment-only prompt (pasted screenshot, dragged image) is
		// valid ACP; rejecting it left clients showing an error for every
		// caption-less image.
		if len(attachments) > 0 {
			return imageOnlyPromptText, attachments, nil
		}
		return "", nil, fmt.Errorf("no text content in prompt")
	}

	return joinTextParts(textParts), attachments, nil
}

// uriToPath converts a file:// URI to a local path, leaving plain paths as-is.
func uriToPath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return uri
	}
	if parsed, err := url.Parse(uri); err == nil && parsed.Path != "" {
		return parsed.Path
	}
	return strings.TrimPrefix(uri, "file://")
}

// attachmentFromFile reads a local image file referenced by a resource_link.
// It returns ok=false for missing files, non-images or files too large to
// inline, so the caller can fall back to a textual mention.
func (a *PandoACPAgent) attachmentFromFile(path, mime string) (message.Attachment, bool) {
	if path == "" || strings.Contains(path, "://") {
		return message.Attachment{}, false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxInlineAttachmentBytes {
		return message.Attachment{}, false
	}
	if mime != "" && !strings.HasPrefix(mime, "image/") {
		return message.Attachment{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.logger.Printf("[ACP AGENT] Warning: failed to read attached file %s: %v", path, err)
		return message.Attachment{}, false
	}
	detected := imageopt.DetectMIME(data)
	if detected == "" {
		return message.Attachment{}, false
	}
	return message.Attachment{
		FilePath: path,
		FileName: filepath.Base(path),
		MimeType: detected,
		Content:  data,
	}, true
}

// maxInlineAttachmentBytes caps how much of a referenced file is inlined into
// the prompt. Larger files stay on disk and are mentioned by path.
const maxInlineAttachmentBytes = 32 * 1024 * 1024

// joinTextParts joins multiple text parts with newlines.
func joinTextParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "\n"
		}
		result += part
	}
	return result
}

func (a *PandoACPAgent) terminalOutputEnabled() bool {
	return a.clientSupportsTerminalOutput
}

func (a *PandoACPAgent) toolMeta(toolName, toolCallID string, includeTerminalInfo bool) map[string]any {
	meta := map[string]any{
		"pando": map[string]any{"toolName": toolName},
	}
	if includeTerminalInfo && a.terminalOutputEnabled() && isBashTool(toolName) {
		meta["terminal_info"] = map[string]any{"terminal_id": toolCallID}
	}
	return meta
}

func (a *PandoACPAgent) toolStartContent(toolName, toolCallID string, fallback []acpsdk.ToolCallContent) []acpsdk.ToolCallContent {
	if a.terminalOutputEnabled() && isBashTool(toolName) {
		return []acpsdk.ToolCallContent{acpsdk.ToolTerminalRef(toolCallID)}
	}
	return fallback
}

func updateUserMessageTextWithID(text, messageID string) acpsdk.SessionUpdate {
	update := acpsdk.UpdateUserMessageText(text)
	if update.UserMessageChunk != nil && messageID != "" {
		update.UserMessageChunk.MessageId = &messageID
	}
	return update
}

func updateAgentMessageTextWithID(text, messageID string) acpsdk.SessionUpdate {
	update := acpsdk.UpdateAgentMessageText(text)
	if update.AgentMessageChunk != nil && messageID != "" {
		update.AgentMessageChunk.MessageId = &messageID
	}
	return update
}

func updateAgentThoughtTextWithID(text, messageID string) acpsdk.SessionUpdate {
	update := acpsdk.UpdateAgentThoughtText(text)
	if update.AgentThoughtChunk != nil && messageID != "" {
		update.AgentThoughtChunk.MessageId = &messageID
	}
	return update
}

// processPromptWithAgent processes a prompt using the Pando LLM agent.
// attachments carries any image/binary content extracted from the prompt (6d).
func (a *PandoACPAgent) processPromptWithAgent(
	ctx context.Context,
	acpSession *ACPServerSession,
	promptText string,
	attachments ...message.Attachment,
) (acpsdk.StopReason, error) {
	// Mark the context as ACP so tools without a usable blocking UI (e.g.
	// AskUserQuestion) switch to their text-mode behavior. This value flows
	// through agent.Run -> genCtx into each tool's Run call.
	ctx = context.WithValue(ctx, ACPModeContextKey{}, true)
	if acpSession.CleanMode() {
		ctx = context.WithValue(ctx, cleanModeContextKey{}, true)
	}
	reconcileACPThinkingSession(a.agentService, acpSession)
	if promptText != "" && acpSession.HasAgentConnection() {
		userMessageID := acpSession.PandoSessionID() + "-user"
		if err := acpSession.SendUpdate(updateUserMessageTextWithID(promptText, userMessageID)); err != nil {
			a.logger.Printf("[ACP AGENT] Failed to send user message chunk: %v", err)
		}
	}
	// Adopt a switch made by pando_setup in an earlier turn before reading the
	// session's overrides: otherwise this call would hand the pre-switch model
	// back to the agent and revert it.
	if reconcileACPSessionModel(a.agentService, acpSession) {
		a.sendSessionConfigOptionsUpdate(ctx, acpSession.ID)
	}
	overrides := sessionLLMOverridesFor(acpSession)
	a.logger.Printf(
		"[ACP AGENT] Applying ACP session overrides for session %s: model=%q stream_mode=%q reasoning_effort=%q thinking_mode=%q persona=%q clean=%t",
		acpSession.ID,
		overrides.Model,
		acpSession.ThinkingStreamMode(),
		overrides.ReasoningEffort,
		overrides.ThinkingMode,
		overrides.Persona,
		acpSession.CleanMode(),
	)
	a.agentService.SetSessionLLMOverrides(acpSession.PandoSessionID(), overrides)
	eventChan, err := a.agentService.Run(ctx, acpSession.PandoSessionID(), promptText, attachments...)
	if err != nil {
		return "", fmt.Errorf("failed to start agent: %w", err)
	}
	stopReason, streamErr := a.processAgentEventStream(ctx, acpSession, eventChan)
	// The run may have switched model mid-loop; publish it so the picker shows
	// which model actually finished the turn.
	if reconcileACPSessionModel(a.agentService, acpSession) {
		a.sendSessionConfigOptionsUpdate(ctx, acpSession.ID)
	}
	return stopReason, streamErr
}

func (a *PandoACPAgent) processAgentEventStream(
	ctx context.Context,
	acpSession *ACPServerSession,
	eventChan <-chan AgentEvent,
) (acpsdk.StopReason, error) {
	var finalStopReason acpsdk.StopReason
	var currentMessageID string
	thinkingStreamMode := acpSession.ThinkingStreamMode()
	a.logger.Printf("[ACP AGENT] Applied thinking stream mode %q for session %s", thinkingStreamMode, acpSession.ID)
	// Track whether streaming deltas were sent so processAgentResponse can skip
	// re-sending the full content (which would cause duplicate text in the client).
	var sentContentDeltas, sentThinkingDeltas bool
	var thinkingState groupedThinkingState
	sendThinking := func(text string) {
		if text == "" {
			return
		}
		if err := acpSession.SendUpdate(updateAgentThoughtTextWithID(text, currentMessageID)); err != nil {
			a.logger.Printf("[ACP AGENT] Failed to send thinking update: %v", err)
			return
		}
		sentThinkingDeltas = true
	}
	flushThinking := func(force bool) {
		if thinkingStreamMode != thinkingStreamModeGrouped {
			return
		}
		now := time.Now()
		text, reason := thinkingState.readyText(now, force)
		if text == "" {
			return
		}
		a.logger.Printf(
			"[ACP AGENT] Flushing grouped thinking for session %s: reason=%s chars=%d",
			acpSession.ID,
			reason,
			len(text),
		)
		beforeSent := sentThinkingDeltas
		sendThinking(text)
		if sentThinkingDeltas == beforeSent {
			return
		}
		thinkingState.markFlushed(now)
	}
	for event := range eventChan {
		switch event.Type {
		case AgentEventTypeError:
			flushThinking(true)
			if event.Error != nil {
				a.logger.Printf("[ACP AGENT] Agent error: %v", event.Error)
				return acpsdk.StopReasonRefusal, event.Error
			}

		case AgentEventTypeThinkingDelta:
			if event.Delta != "" {
				switch thinkingStreamMode {
				case thinkingStreamModeOff:
				case thinkingStreamModeFull:
					sendThinking(event.Delta)
				default:
					thinkingState.append(event.Delta, time.Now())
					flushThinking(false)
				}
			}

		case AgentEventTypeResponse:
			currentMessageID = event.Message.ID
			flushThinking(true)
			if err := a.processAgentResponse(acpSession, event.Message, sentContentDeltas, sentThinkingDeltas); err != nil {
				a.logger.Printf("[ACP AGENT] Failed to process response: %v", err)
				return acpsdk.StopReasonRefusal, err
			}
			finalStopReason = a.mapFinishReasonToStopReason(event.Message.FinishReason())

		case AgentEventTypeContentDelta:
			if event.Delta != "" {
				if err := acpSession.SendUpdate(updateAgentMessageTextWithID(event.Delta, currentMessageID)); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send content delta: %v", err)
				} else {
					sentContentDeltas = true
				}
			}

		case AgentEventTypeSystemMessage:
			flushThinking(true)
			msg := strings.TrimSpace(event.SystemMessage)
			if msg == "" {
				continue
			}
			if usageUpdate, normalized, suppress := a.normalizeSystemMessage(ctx, acpSession, msg); !suppress {
				if usageUpdate != nil {
					if err := acpSession.SendUpdate(*usageUpdate); err != nil {
						a.logger.Printf("[ACP AGENT] Failed to send system usage update: %v", err)
					}
				}
				if err := acpSession.SendUpdate(updateAgentMessageTextWithID(normalized, currentMessageID)); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send system message update: %v", err)
				} else {
					sentContentDeltas = true
				}
			}

		case AgentEventTypeToolCall:
			flushThinking(true)
			if event.ToolCall != nil {
				tc := event.ToolCall

				// TodoWrite → plan: emit a plan update on every streaming tool-call
				// event so ACP clients can display the evolving plan live, matching
				// the history replay path that already reconstructs the full plan.
				if strings.EqualFold(tc.Name, "TodoWrite") {
					a.pendingToolCallsMu.Lock()
					a.pendingToolCalls[tc.ID] = tc.Input
					// Mark as registered so processAgentResponse skips it.
					a.startedToolCalls[tc.ID] = true
					a.pendingToolCallsMu.Unlock()
					if entries := parseTodoWritePlan(tc.Input); len(entries) > 0 {
						if err := a.sendPlanModeUpdate(acpSession, entries); err != nil {
							a.logger.Printf("[ACP AGENT] Failed to send plan update (streaming): %v", err)
						}
					}
					continue
				}

				kind := mapToolKind(tc.Name)
				rawInput := parseRawInput(tc.Input)
				title := toolDisplayTitle(tc.Name, rawInput, acpSession.WorkDir)
				content := toolCallContent(tc.Name, rawInput)
				locations := toLocations(tc.Name, tc.Input)

				content = a.toolStartContent(tc.Name, tc.ID, content)
				toolMeta := a.toolMeta(tc.Name, tc.ID, true)

				// Prefer sending whatever structured/raw input is already available in the
				// initial tool_call payload so ACP clients like Zed can render arguments
				// from the first card instead of waiting for tool_call_update.
				// sendStart emits the initial tool_call notification.
				// ACP clients expect status=pending for the initial event while
				// input is still streaming; in_progress is reserved for when the
				// tool actually begins executing. We always force pending here.
				sendStart := func() error {
					startOpts := []acpsdk.ToolCallStartOpt{
						acpsdk.WithStartKind(kind),
						acpsdk.WithStartStatus(acpsdk.ToolCallStatusPending),
						acpsdk.WithStartRawInput(rawInput),
						acpsdk.WithStartMeta(toolMeta),
					}
					if len(locations) > 0 {
						startOpts = append(startOpts, acpsdk.WithStartLocations(locations))
					}
					if len(content) > 0 {
						startOpts = append(startOpts, acpsdk.WithStartContent(content))
					}
					return acpSession.SendUpdate(acpsdk.StartToolCall(acpsdk.ToolCallId(tc.ID), title, startOpts...))
				}

				// Always update the stored input; for edit tools sendWriteTextFile also reads it.
				a.pendingToolCallsMu.Lock()
				prevInput := a.pendingToolCalls[tc.ID]
				a.pendingToolCalls[tc.ID] = tc.Input
				started := a.startedToolCalls[tc.ID]
				a.pendingToolCallsMu.Unlock()

				hasUsefulInput := hasUsefulRawInput(rawInput)
				becameEnriched := started && !hasUsefulRawInput(parseRawInput(prevInput)) && hasUsefulInput

				if !tc.Finished {
					if !started {
						if err := sendStart(); err != nil {
							a.logger.Printf("[ACP AGENT] Failed to send tool call pending: %v", err)
						} else {
							a.pendingToolCallsMu.Lock()
							a.startedToolCalls[tc.ID] = true
							a.pendingToolCallsMu.Unlock()
						}
					} else {
						// Throttled delta update: the agent emits a new
						// AgentEventTypeToolCall during EventToolUseDelta with
						// the accumulated input so far. Forward it as an
						// UpdateToolCall so the ACP client sees enriched
						// rawInput / title / locations while the provider is
						// still streaming the tool-call JSON.
						deltaStatus := acpsdk.ToolCallStatusInProgress
						if becameEnriched {
							// When the initial tool_call had empty input, resend the first
							// enriched payload as pending so clients like Zed can treat it
							// as the canonical initial card state instead of only a follow-up.
							deltaStatus = acpsdk.ToolCallStatusPending
						}
						deltaOpts := []acpsdk.ToolCallUpdateOpt{
							acpsdk.WithUpdateStatus(deltaStatus),
							acpsdk.WithUpdateKind(kind),
							acpsdk.WithUpdateTitle(title),
							acpsdk.WithUpdateRawInput(rawInput),
						}
						if len(content) > 0 {
							deltaOpts = append(deltaOpts, acpsdk.WithUpdateContent(content))
						}
						if len(locations) > 0 {
							deltaOpts = append(deltaOpts, acpsdk.WithUpdateLocations(locations))
						}
						deltaUpdate := acpsdk.UpdateToolCall(acpsdk.ToolCallId(tc.ID), deltaOpts...)
						if deltaUpdate.ToolCallUpdate != nil {
							deltaUpdate.ToolCallUpdate.Meta = toolMeta
						}
						if err := acpSession.SendUpdate(deltaUpdate); err != nil {
							a.logger.Printf("[ACP AGENT] Failed to send tool call delta update: %v", err)
						}
					}
				} else {
					// tc.Finished == true (ToolUseStop event).
					// sentStart tracks whether StartToolCall is sent in this block so
					// we can decide whether to follow up with an in_progress update.
					sentStart := started
					if !started {
						// Only send StartToolCall when input is non-empty and parseable.
						// If input is still empty at ToolUseStop, defer entirely to
						// processAgentResponse which uses the complete assembled message.
						// Sending a StartToolCall with {} here would lock Zed onto an
						// empty card it will never update.
						if hasUsefulInput {
							if err := sendStart(); err != nil {
								a.logger.Printf("[ACP AGENT] Failed to send tool call start at ToolUseStop: %v", err)
							} else {
								a.pendingToolCallsMu.Lock()
								a.startedToolCalls[tc.ID] = true
								a.pendingToolCallsMu.Unlock()
								sentStart = true
							}
						}
						// else: processAgentResponse will send StartToolCall with full input.
					}

					// Only emit in_progress when a StartToolCall was (or had been) sent.
					// Without a prior StartToolCall the update is for an unknown toolCallId
					// and ACP clients silently discard it.
					if sentStart {
						inProgressOpts := []acpsdk.ToolCallUpdateOpt{
							acpsdk.WithUpdateStatus(acpsdk.ToolCallStatusInProgress),
							acpsdk.WithUpdateKind(kind),
							acpsdk.WithUpdateTitle(title),
							acpsdk.WithUpdateRawInput(rawInput),
							acpsdk.WithUpdateContent(content),
						}
						if len(locations) > 0 {
							inProgressOpts = append(inProgressOpts, acpsdk.WithUpdateLocations(locations))
						}
						inProgressUpdate := acpsdk.UpdateToolCall(acpsdk.ToolCallId(tc.ID), inProgressOpts...)
						if inProgressUpdate.ToolCallUpdate != nil {
							inProgressUpdate.ToolCallUpdate.Meta = toolMeta
						}
						if err := acpSession.SendUpdate(inProgressUpdate); err != nil {
							a.logger.Printf("[ACP AGENT] Failed to send tool call in_progress: %v", err)
						}
					}
				}
			}

		case AgentEventTypeToolResult:
			flushThinking(true)
			if event.ToolResult != nil {
				tr := event.ToolResult

				// For TodoWrite tools, send plan update from the tool call INPUT
				// (not the text output) and skip regular tool result processing.
				if strings.EqualFold(tr.Name, "TodoWrite") {
					a.handleTodoWritePlan(acpSession, tr)
					a.pendingToolCallsMu.Lock()
					delete(a.pendingToolCalls, tr.ToolCallID)
					delete(a.startedToolCalls, tr.ToolCallID)
					a.pendingToolCallsMu.Unlock()
					continue
				}

				status := acpsdk.ToolCallStatusCompleted
				if tr.IsError {
					status = acpsdk.ToolCallStatusFailed
				}

				// Retrieve the stored input for this tool call.
				// For edit tools we do NOT delete here — sendWriteTextFile needs it.
				// For all other tools we clean up immediately to avoid leaking memory.
				a.pendingToolCallsMu.Lock()
				storedInput := a.pendingToolCalls[tr.ToolCallID]
				// Capture wasStarted BEFORE deleting — if false it means StartToolCall
				// was never sent (e.g. AgentEventTypeToolCall events were dropped because
				// the 256-slot eventCh buffer overflowed with ThinkingDelta events, or
				// because the provider does not stream tool-use start/stop events).
				wasStarted := a.startedToolCalls[tr.ToolCallID]
				delete(a.startedToolCalls, tr.ToolCallID)
				if !isEditTool(tr.Name) {
					delete(a.pendingToolCalls, tr.ToolCallID)
				}
				a.pendingToolCallsMu.Unlock()

				// Fallback: if the streaming AgentEventTypeToolCall events were
				// dropped (buffer overflow), storedInput is empty but tr.Input
				// carries the full original tool call JSON populated by agent.go.
				if storedInput == "" && tr.Input != "" {
					storedInput = tr.Input
					// Also pre-populate pendingToolCalls for edit tools so that
					// sendWriteTextFile can find the file path later.
					if isEditTool(tr.Name) {
						a.pendingToolCallsMu.Lock()
						a.pendingToolCalls[tr.ToolCallID] = tr.Input
						a.pendingToolCallsMu.Unlock()
					}
				}

				// Guarantee that a tool_call (start) always precedes the first
				// tool_call_update for the same toolCallId.  Without it Zed (and other
				// ACP clients) have no entry for the tool call and silently ignore the
				// update, so the tool never appears in the conversation panel.
				if !wasStarted {
					synthKind := mapToolKind(tr.Name)
					synthRawInput := parseRawInput(storedInput)
					synthTitle := toolDisplayTitle(tr.Name, synthRawInput, acpSession.WorkDir)
					synthContent := toolCallContent(tr.Name, synthRawInput)
					synthLocations := toLocations(tr.Name, storedInput)
					synthMeta := a.toolMeta(tr.Name, tr.ToolCallID, true)
					synthContent = a.toolStartContent(tr.Name, tr.ToolCallID, synthContent)
					synthStartOpts := []acpsdk.ToolCallStartOpt{
						acpsdk.WithStartKind(synthKind),
						acpsdk.WithStartStatus(acpsdk.ToolCallStatusInProgress),
						acpsdk.WithStartRawInput(synthRawInput),
					}
					if len(synthLocations) > 0 {
						synthStartOpts = append(synthStartOpts, acpsdk.WithStartLocations(synthLocations))
					}
					if len(synthContent) > 0 {
						synthStartOpts = append(synthStartOpts, acpsdk.WithStartContent(synthContent))
					}
					synthStartUpdate := acpsdk.StartToolCall(acpsdk.ToolCallId(tr.ToolCallID), synthTitle, synthStartOpts...)
					if synthStartUpdate.ToolCall != nil {
						synthStartUpdate.ToolCall.Meta = synthMeta
					}
					if err := acpSession.SendUpdate(synthStartUpdate); err != nil {
						a.logger.Printf("[ACP AGENT] Failed to send synthetic tool call start (wasStarted=false): %v", err)
					} else {
						a.logger.Printf("[ACP AGENT] Sent synthetic tool call start for %s (id=%s): start events were dropped or not streamed", tr.Name, tr.ToolCallID)
					}
				}

				// Rebuild rawInput so clients can display tool arguments alongside the result.
				rawInput := parseRawInput(storedInput)

				// Build rawOutput matching the opencode format: { output/error, metadata }.
				rawOutput := buildRawOutput(tr.Content, tr.Metadata, tr.IsError)

				// For edit tools, add filediff metadata to match opencode standard
				if isEditTool(tr.Name) && !tr.IsError && storedInput != "" {
					var ep editToolInput
					if jerr := json.Unmarshal([]byte(storedInput), &ep); jerr == nil && ep.FilePath != "" {
						if tr.Name == "edit" {
							// Add filediff metadata for edit tools
							if rawOutput["metadata"] == nil {
								rawOutput["metadata"] = map[string]interface{}{}
							}
							meta := rawOutput["metadata"].(map[string]interface{})
							meta["filediff"] = map[string]interface{}{
								"file":      ep.FilePath,
								"before":    ep.OldString,
								"after":     ep.NewString,
								"additions": countLines(ep.NewString),
								"deletions": countLines(ep.OldString),
							}
						} else if tr.Name == "write" {
							// For write tools, include additions count
							if rawOutput["metadata"] == nil {
								rawOutput["metadata"] = map[string]interface{}{}
							}
							meta := rawOutput["metadata"].(map[string]interface{})
							meta["filediff"] = map[string]interface{}{
								"file":      ep.FilePath,
								"additions": countLines(ep.Content),
							}
						}
					}
				}

				// Use ToolCallContent so editors display the output correctly.
				outputContent := toolResultContent(tr.Name, tr.Content, tr.IsError)

				// For edit tools, attach a diff content block for visual diffs.
				if isEditTool(tr.Name) && !tr.IsError && storedInput != "" {
					var ep editToolInput
					if jerr := json.Unmarshal([]byte(storedInput), &ep); jerr == nil && ep.FilePath != "" {
						if tr.Name == "write" {
							outputContent = append(outputContent, acpsdk.ToolDiffContent(ep.FilePath, ep.Content))
						} else {
							outputContent = append(outputContent, acpsdk.ToolDiffContent(ep.FilePath, ep.NewString, ep.OldString))
						}
					}
				}

				kind := mapToolKind(tr.Name)
				title := toolDisplayTitle(tr.Name, rawInput, acpSession.WorkDir)

				// Build _meta for the result update.
				resultMeta := a.toolMeta(tr.Name, tr.ToolCallID, false)

				// For Bash tools, send terminal_output + terminal_exit via _meta
				// so editors can display the output in their terminal widgets.
				// This matches claude-agent-acp's streaming lifecycle:
				//   1. tool_call       → _meta.terminal_info  (sent above in ToolCall)
				//   2. tool_call_update → _meta.terminal_output (sent here)
				//   3. tool_call_update → _meta.terminal_exit   (sent here)
				if a.terminalOutputEnabled() && isBashTool(tr.Name) {
					// Step 2: send terminal_output as a separate notification.
					// Only _meta carries the output data — no content field, matching
					// claude-agent-acp behavior so clients don't render it twice.
					termOutput := strings.TrimSpace(tr.Content)
					if termOutput != "" {
						termOutputUpdate := acpsdk.UpdateToolCall(acpsdk.ToolCallId(tr.ToolCallID))
						if termOutputUpdate.ToolCallUpdate != nil {
							termOutputUpdate.ToolCallUpdate.Meta = map[string]any{
								"terminal_output": map[string]any{
									"terminal_id": tr.ToolCallID,
									"data":        termOutput,
								},
							}
						}
						if err := acpSession.SendUpdate(termOutputUpdate); err != nil {
							a.logger.Printf("[ACP AGENT] Failed to send terminal output: %v", err)
						}
					}

					exitCode := 0
					if tr.IsError {
						exitCode = 1
					}
					resultMeta["terminal_exit"] = map[string]any{
						"terminal_id": tr.ToolCallID,
						"exit_code":   exitCode,
						"signal":      nil,
					}
					// Keep only the terminal reference in content (output is carried via
					// _meta above). Text block fallback is omitted because it causes
					// duplicate display in clients that support terminal widgets —
					// matching claude-agent-acp's approach.
					outputContent = []acpsdk.ToolCallContent{
						acpsdk.ToolTerminalRef(tr.ToolCallID),
					}
				}

				resultOpts := []acpsdk.ToolCallUpdateOpt{
					acpsdk.WithUpdateStatus(status),
					acpsdk.WithUpdateKind(kind),
					acpsdk.WithUpdateTitle(title),
					acpsdk.WithUpdateContent(outputContent),
					acpsdk.WithUpdateRawInput(rawInput),
					acpsdk.WithUpdateRawOutput(rawOutput),
				}
				if locs := toLocations(tr.Name, storedInput); len(locs) > 0 {
					resultOpts = append(resultOpts, acpsdk.WithUpdateLocations(locs))
				}
				resultUpdate := acpsdk.UpdateToolCall(acpsdk.ToolCallId(tr.ToolCallID), resultOpts...)
				// Inject _meta into the result update.
				if resultUpdate.ToolCallUpdate != nil {
					resultUpdate.ToolCallUpdate.Meta = resultMeta
				}
				if err := acpSession.SendUpdate(resultUpdate); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send tool result: %v", err)
				}

				// 6a: if client supports writeTextFile and this was a successful edit/write,
				// push the new file content so the editor can refresh its buffers.
				if !tr.IsError && a.clientSupportsWriteFile && a.conn != nil && isEditTool(tr.Name) {
					a.sendWriteTextFile(ctx, acpSession.ID, tr.ToolCallID)
				}
			}

		case AgentEventTypeSummarize:
			flushThinking(true)
			if event.Progress != "" {
				if err := acpSession.SendUpdate(updateAgentMessageTextWithID(event.Progress+"\n", currentMessageID)); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send summarize update: %v", err)
				} else {
					sentContentDeltas = true
				}
			}
		}
	}

	flushThinking(true)

	if finalStopReason == "" {
		finalStopReason = acpsdk.StopReasonEndTurn
	}

	return finalStopReason, nil
}

// isEditTool returns true for tool names that modify files on disk.
func isEditTool(name string) bool {
	switch name {
	case "edit", "write", "patch", "multiedit":
		return true
	}
	return false
}

// isBashTool returns true for tool names that execute shell commands.
func isBashTool(name string) bool {
	switch strings.ToLower(name) {
	case "bash", "execute_command":
		return true
	}
	return false
}

// sendWriteTextFile reads the edited file and sends its content to the ACP client.
// This allows the editor to update open buffers without reloading from disk (6a).
func (a *PandoACPAgent) sendWriteTextFile(ctx context.Context, sessionID acpsdk.SessionId, toolCallID string) {
	a.pendingToolCallsMu.Lock()
	input, ok := a.pendingToolCalls[toolCallID]
	if ok {
		delete(a.pendingToolCalls, toolCallID)
	}
	a.pendingToolCallsMu.Unlock()

	if !ok || input == "" {
		return
	}

	var params editToolInput
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		a.logger.Printf("[ACP AGENT] WriteTextFile: failed to parse tool input: %v", err)
		return
	}

	filePath := params.FilePath
	if filePath == "" {
		return
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		a.logger.Printf("[ACP AGENT] WriteTextFile: failed to read %s: %v", filePath, err)
		return
	}

	_, err = a.conn.WriteTextFile(ctx, acpsdk.WriteTextFileRequest{
		SessionId: sessionID,
		Path:      filePath,
		Content:   string(content),
	})
	if err != nil {
		a.logger.Printf("[ACP AGENT] WriteTextFile: failed to send %s: %v", filePath, err)
		return
	}

	a.logger.Printf("[ACP AGENT] WriteTextFile: sent updated content for %s (%d bytes)", filePath, len(content))
}

// processAgentResponse processes an agent response message and sends updates to the client.
// sentContentDeltas and sentThinkingDeltas indicate whether streaming deltas were already sent
// for this turn; when true the full content/reasoning blobs are skipped to avoid duplicates.
func (a *PandoACPAgent) processAgentResponse(
	acpSession *ACPServerSession,
	msg message.Message,
	sentContentDeltas bool,
	sentThinkingDeltas bool,
) error {
	if !sentContentDeltas {
		if content := msg.Content(); content.String() != "" {
			update := updateAgentMessageTextWithID(content.String(), msg.ID)
			if err := acpSession.SendUpdate(update); err != nil {
				a.logger.Printf("[ACP AGENT] Failed to send message update: %v", err)
			}
		}
	}

	if !sentThinkingDeltas {
		if reasoning := msg.ReasoningContent(); reasoning.String() != "" {
			update := updateAgentThoughtTextWithID(reasoning.String(), msg.ID)
			if err := acpSession.SendUpdate(update); err != nil {
				a.logger.Printf("[ACP AGENT] Failed to send thought update: %v", err)
			}
		}
	}

	// Ensure pendingToolCalls is populated and StartToolCall is sent for every tool
	// call in the assembled message.
	//
	// Some providers (Copilot/OpenAI/Gemini) do not emit streaming ToolUseStart/Stop
	// events, so AgentEventTypeToolCall is never received and StartToolCall is never
	// sent to the client.  Without a prior StartToolCall the client has no record of
	// the tool and cannot show its name — it receives an UpdateToolCall for an unknown
	// tool call ID and silently discards or renders it blank.
	//
	// We detect this by checking for key existence (not value equality) in the map:
	// the streaming path always inserts the key (even with an empty-string value) when
	// it sends StartToolCall, so a missing key means StartToolCall was never sent.
	for _, toolCall := range msg.ToolCalls() {
		// TodoWrite → plan: convert todo list into an ACP plan notification so
		// clients render it as a structured plan instead of a plain tool call.
		if strings.EqualFold(toolCall.Name, "TodoWrite") {
			if entries := parseTodoWritePlan(toolCall.Input); len(entries) > 0 {
				if err := a.sendPlanModeUpdate(acpSession, entries); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send plan update: %v", err)
				}
			}
			// Mark as registered so the streaming path does not also emit StartToolCall.
			a.pendingToolCallsMu.Lock()
			a.pendingToolCalls[toolCall.ID] = toolCall.Input
			a.pendingToolCallsMu.Unlock()
			continue
		}

		a.pendingToolCallsMu.Lock()
		prevStoredInput, alreadyRegistered := a.pendingToolCalls[toolCall.ID]
		wasStarted := a.startedToolCalls[toolCall.ID]
		hadEmptyInput := alreadyRegistered && prevStoredInput == ""
		if !alreadyRegistered {
			a.pendingToolCalls[toolCall.ID] = toolCall.Input
		} else if prevStoredInput == "" {
			// Registered by ToolUseStart with empty input; update to full input now.
			a.pendingToolCalls[toolCall.ID] = toolCall.Input
		}
		a.pendingToolCallsMu.Unlock()

		if alreadyRegistered && wasStarted {
			// StartToolCall was already sent by the streaming path.
			// If the stored input was empty (EventToolUseStop dropped because the
			// 256-slot event buffer overflowed), send a corrective UpdateToolCall
			// so the client displays the correct command title and rawInput.
			if hadEmptyInput && toolCall.Input != "" {
				kind := mapToolKind(toolCall.Name)
				rawInput := parseRawInput(toolCall.Input)
				title := toolDisplayTitle(toolCall.Name, rawInput, acpSession.WorkDir)
				content := toolCallContent(toolCall.Name, rawInput)
				locations := toLocations(toolCall.Name, toolCall.Input)
				content = a.toolStartContent(toolCall.Name, toolCall.ID, content)
				toolMeta := a.toolMeta(toolCall.Name, toolCall.ID, true)
				correctiveOpts := []acpsdk.ToolCallUpdateOpt{
					acpsdk.WithUpdateKind(kind),
					acpsdk.WithUpdateTitle(title),
					acpsdk.WithUpdateRawInput(rawInput),
				}
				if len(content) > 0 {
					correctiveOpts = append(correctiveOpts, acpsdk.WithUpdateContent(content))
				}
				if len(locations) > 0 {
					correctiveOpts = append(correctiveOpts, acpsdk.WithUpdateLocations(locations))
				}
				correctiveUpdate := acpsdk.UpdateToolCall(acpsdk.ToolCallId(toolCall.ID), correctiveOpts...)
				if correctiveUpdate.ToolCallUpdate != nil {
					correctiveUpdate.ToolCallUpdate.Meta = toolMeta
				}
				if err := acpSession.SendUpdate(correctiveUpdate); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send corrective tool call update for %s (id=%s): %v", toolCall.Name, toolCall.ID, err)
				} else {
					a.logger.Printf("[ACP AGENT] Sent corrective tool call update for %s (id=%s): EventToolUseStop was dropped from event buffer", toolCall.Name, toolCall.ID)
				}
			}
			continue
		}

		if alreadyRegistered && !wasStarted {
			// Streaming path registered the tool call in pendingToolCalls but
			// deferred StartToolCall because input was empty. Now that the
			// complete message is available we fall through to send
			// StartToolCall with full input below.
			a.pendingToolCallsMu.Lock()
			a.pendingToolCalls[toolCall.ID] = toolCall.Input
			a.pendingToolCallsMu.Unlock()
		}

		// Non-streaming provider: send StartToolCall now so the client knows the tool name.
		kind := mapToolKind(toolCall.Name)
		rawInput := parseRawInput(toolCall.Input)
		title := toolDisplayTitle(toolCall.Name, rawInput, acpSession.WorkDir)
		content := toolCallContent(toolCall.Name, rawInput)
		locations := toLocations(toolCall.Name, toolCall.Input)

		content = a.toolStartContent(toolCall.Name, toolCall.ID, content)
		toolMeta := a.toolMeta(toolCall.Name, toolCall.ID, true)

		startOpts := []acpsdk.ToolCallStartOpt{
			acpsdk.WithStartKind(kind),
			acpsdk.WithStartStatus(acpsdk.ToolCallStatusPending),
			acpsdk.WithStartRawInput(rawInput),
		}
		if len(locations) > 0 {
			startOpts = append(startOpts, acpsdk.WithStartLocations(locations))
		}
		if len(content) > 0 {
			startOpts = append(startOpts, acpsdk.WithStartContent(content))
		}
		startUpdate := acpsdk.StartToolCall(acpsdk.ToolCallId(toolCall.ID), title, startOpts...)
		if startUpdate.ToolCall != nil {
			startUpdate.ToolCall.Meta = toolMeta
		}
		if err := acpSession.SendUpdate(startUpdate); err != nil {
			a.logger.Printf("[ACP AGENT] Failed to send tool call start (non-streaming): %v", err)
		}
	}

	for _, toolResult := range msg.ToolResults() {
		// TodoWrite results are suppressed — the plan notification already
		// carries all the relevant information; an UpdateToolCall would reference
		// a toolCallId that was never sent as a StartToolCall.
		if strings.EqualFold(toolResult.Name, "TodoWrite") {
			a.pendingToolCallsMu.Lock()
			delete(a.pendingToolCalls, toolResult.ToolCallID)
			a.pendingToolCallsMu.Unlock()
			continue
		}

		status := acpsdk.ToolCallStatusCompleted
		if toolResult.IsError {
			status = acpsdk.ToolCallStatusFailed
		}

		a.pendingToolCallsMu.Lock()
		storedInput := a.pendingToolCalls[toolResult.ToolCallID]
		if !isEditTool(toolResult.Name) {
			delete(a.pendingToolCalls, toolResult.ToolCallID)
		}
		a.pendingToolCallsMu.Unlock()

		rawInput := parseRawInput(storedInput)
		rawOutput := buildRawOutput(toolResult.Content, toolResult.Metadata, toolResult.IsError)

		content := toolResultContent(toolResult.Name, toolResult.Content, toolResult.IsError)
		if isEditTool(toolResult.Name) && !toolResult.IsError && storedInput != "" {
			var ep editToolInput
			if err := json.Unmarshal([]byte(storedInput), &ep); err == nil && ep.FilePath != "" {
				if toolResult.Name == "write" {
					content = append(content, acpsdk.ToolDiffContent(ep.FilePath, ep.Content))
				} else {
					content = append(content, acpsdk.ToolDiffContent(ep.FilePath, ep.NewString, ep.OldString))
				}
			}
		}

		kind := mapToolKind(toolResult.Name)
		title := toolDisplayTitle(toolResult.Name, rawInput, acpSession.WorkDir)

		resultMeta := a.toolMeta(toolResult.Name, toolResult.ToolCallID, false)

		// Terminal streaming for Bash results (same as streaming path).
		if a.terminalOutputEnabled() && isBashTool(toolResult.Name) {
			termOutput := strings.TrimSpace(toolResult.Content)
			if termOutput != "" {
				termOutputUpdate := acpsdk.UpdateToolCall(acpsdk.ToolCallId(toolResult.ToolCallID))
				if termOutputUpdate.ToolCallUpdate != nil {
					termOutputUpdate.ToolCallUpdate.Meta = map[string]any{
						"terminal_output": map[string]any{
							"terminal_id": toolResult.ToolCallID,
							"data":        termOutput,
						},
					}
					// No content field — output is carried in _meta only,
					// matching claude-agent-acp to avoid double rendering.
				}
				if err := acpSession.SendUpdate(termOutputUpdate); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send terminal output: %v", err)
				}
			}

			exitCode := 0
			if toolResult.IsError {
				exitCode = 1
			}
			resultMeta["terminal_exit"] = map[string]any{
				"terminal_id": toolResult.ToolCallID,
				"exit_code":   exitCode,
				"signal":      nil,
			}
			// Only terminal ref in content — no text block fallback, matching
			// claude-agent-acp behavior for terminal-capable clients.
			content = []acpsdk.ToolCallContent{acpsdk.ToolTerminalRef(toolResult.ToolCallID)}
		}

		resultOpts := []acpsdk.ToolCallUpdateOpt{
			acpsdk.WithUpdateStatus(status),
			acpsdk.WithUpdateKind(kind),
			acpsdk.WithUpdateTitle(title),
			acpsdk.WithUpdateContent(content),
			acpsdk.WithUpdateRawInput(rawInput),
			acpsdk.WithUpdateRawOutput(rawOutput),
		}
		if locs := toLocations(toolResult.Name, storedInput); len(locs) > 0 {
			resultOpts = append(resultOpts, acpsdk.WithUpdateLocations(locs))
		}

		resultUpdate := acpsdk.UpdateToolCall(acpsdk.ToolCallId(toolResult.ToolCallID), resultOpts...)
		if resultUpdate.ToolCallUpdate != nil {
			resultUpdate.ToolCallUpdate.Meta = resultMeta
		}
		if err := acpSession.SendUpdate(resultUpdate); err != nil {
			a.logger.Printf("[ACP AGENT] Failed to send tool result update: %v", err)
		}
	}

	return nil
}

// handleTodoWritePlan processes a TodoWrite tool result and sends a plan update
// to the ACP client. It uses the tool call INPUT (JSON with the todos array)
// rather than the tool result content (human-readable text) because the plan
// schema lives in the input, not the output.
// This is the primary plan-delivery path for non-streaming providers
// (OpenAI/Copilot/Gemini) that do not emit EventToolUseStart/Delta/Stop and
// therefore never trigger the streaming plan path in processAgentEventStream.
func (a *PandoACPAgent) handleTodoWritePlan(acpSession *ACPServerSession, toolResult *message.ToolResult) {
	// The tool call input ({"todos": [...]}) is the canonical plan source.
	// toolResult.Content is the human-readable confirmation text produced by
	// the TodoWrite tool's Run method and cannot be parsed as plan JSON.
	input := toolResult.Input
	if input == "" {
		a.logger.Printf("[ACP AGENT] TodoWrite result has no input for session %s, skipping plan update", acpSession.ID)
		return
	}

	entries := parseTodoWritePlan(input)
	if len(entries) == 0 {
		a.logger.Printf("[ACP AGENT] Failed to parse TodoWrite input as plan for session %s", acpSession.ID)
		return
	}

	if err := a.sendPlanModeUpdate(acpSession, entries); err != nil {
		a.logger.Printf("[ACP AGENT] Failed to send plan update: %v", err)
	} else {
		a.logger.Printf("[ACP AGENT] Successfully sent plan update with %d entries (from tool result)", len(entries))
	}
}

func (a *PandoACPAgent) sendPlanModeUpdate(acpSession *ACPServerSession, entries []acpsdk.PlanEntry) error {
	if acpSession == nil || len(entries) == 0 {
		return nil
	}

	if err := acpSession.SendUpdate(acpsdk.UpdateCurrentMode(acpsdk.SessionModeId("plan"))); err != nil {
		return err
	}
	return acpSession.SendUpdate(acpsdk.UpdatePlan(entries...))
}

// mapFinishReasonToStopReason maps Pando finish reasons to ACP stop reasons.
func (a *PandoACPAgent) mapFinishReasonToStopReason(finishReason message.FinishReason) acpsdk.StopReason {
	switch finishReason {
	case message.FinishReasonEndTurn:
		return acpsdk.StopReasonEndTurn
	case message.FinishReasonMaxTokens:
		return acpsdk.StopReasonMaxTokens
	case message.FinishReasonCanceled:
		return acpsdk.StopReasonCancelled
	case message.FinishReasonPermissionDenied:
		return acpsdk.StopReasonRefusal
	default:
		return acpsdk.StopReasonEndTurn
	}
}
