package acp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/digiogithub/pando/internal/message"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// extractPromptContent extracts text and image attachments from a Prompt (slice of ContentBlocks).
// Supports text blocks (ContentBlock::Text) and image blocks (ContentBlock::Image, requires 6d capability).
func (a *PandoACPAgent) extractPromptContent(prompt []acpsdk.ContentBlock) (string, []message.Attachment, error) {
	if len(prompt) == 0 {
		return "", nil, fmt.Errorf("empty prompt content")
	}

	var textParts []string
	var attachments []message.Attachment

	for _, block := range prompt {
		if block.Text != nil {
			textParts = append(textParts, block.Text.Text)
		}
		// 6d: handle image content blocks
		if block.Image != nil {
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
			attachments = append(attachments, message.Attachment{
				MimeType: mimeType,
				Content:  data,
			})
		}
	}

	if len(textParts) == 0 {
		return "", nil, fmt.Errorf("no text content in prompt")
	}

	return joinTextParts(textParts), attachments, nil
}

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

// processPromptWithAgent processes a prompt using the Pando LLM agent.
// attachments carries any image/binary content extracted from the prompt (6d).
func (a *PandoACPAgent) processPromptWithAgent(
	ctx context.Context,
	acpSession *ACPServerSession,
	promptText string,
	attachments ...message.Attachment,
) (acpsdk.StopReason, error) {
	pandoSessionID := acpSession.PandoSessionID()

	eventChan, err := a.agentService.Run(ctx, pandoSessionID, promptText, attachments...)
	if err != nil {
		return "", fmt.Errorf("failed to start agent: %w", err)
	}

	var finalStopReason acpsdk.StopReason
	// Track whether streaming deltas were sent so processAgentResponse can skip
	// re-sending the full content (which would cause duplicate text in the client).
	var sentContentDeltas, sentThinkingDeltas bool
	for event := range eventChan {
		switch event.Type {
		case AgentEventTypeError:
			if event.Error != nil {
				a.logger.Printf("[ACP AGENT] Agent error: %v", event.Error)
				return acpsdk.StopReasonRefusal, event.Error
			}

		case AgentEventTypeResponse:
			err := a.processAgentResponse(acpSession, event.Message, sentContentDeltas, sentThinkingDeltas)
			if err != nil {
				a.logger.Printf("[ACP AGENT] Failed to process response: %v", err)
				return acpsdk.StopReasonRefusal, err
			}
			finalStopReason = a.mapFinishReasonToStopReason(event.Message.FinishReason())

		case AgentEventTypeContentDelta:
			if event.Delta != "" {
				if err := acpSession.SendUpdate(acpsdk.UpdateAgentMessageText(event.Delta)); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send content delta: %v", err)
				} else {
					sentContentDeltas = true
				}
			}

		case AgentEventTypeThinkingDelta:
			if event.Delta != "" {
				if err := acpSession.SendUpdate(acpsdk.UpdateAgentThoughtText(event.Delta)); err != nil {
					a.logger.Printf("[ACP AGENT] Failed to send thinking delta: %v", err)
				} else {
					sentThinkingDeltas = true
				}
			}

		case AgentEventTypeToolCall:
			if event.ToolCall != nil {
				tc := event.ToolCall

				// TodoWrite → plan: suppress all tool_call notifications and emit
				// a plan only when the full input is assembled (Finished=true).
				if strings.EqualFold(tc.Name, "TodoWrite") {
					a.pendingToolCallsMu.Lock()
					a.pendingToolCalls[tc.ID] = tc.Input
					// Mark as registered so processAgentResponse skips it.
					a.startedToolCalls[tc.ID] = true
					a.pendingToolCallsMu.Unlock()
					if tc.Finished {
						if entries := parseTodoWritePlan(tc.Input); len(entries) > 0 {
							if err := acpSession.SendUpdate(acpsdk.UpdatePlan(entries...)); err != nil {
								a.logger.Printf("[ACP AGENT] Failed to send plan update (streaming): %v", err)
							}
						}
					}
					continue
				}

				kind := mapToolKind(tc.Name)
				rawInput := parseJSONInput(tc.Input)
				title := toolDisplayTitle(tc.Name, rawInput, acpSession.WorkDir)
				content := toolCallContent(tc.Name, rawInput)
				locations := toLocations(tc.Name, tc.Input)

				toolMeta := map[string]any{
					"pando": map[string]any{"toolName": tc.Name},
				}
				if isBashTool(tc.Name) {
					toolMeta["terminal_info"] = map[string]any{"terminal_id": tc.ID}
					content = []acpsdk.ToolCallContent{acpsdk.ToolTerminalRef(tc.ID)}
				}

				sendStart := func(status acpsdk.ToolCallStatus) error {
					startOpts := []acpsdk.ToolCallStartOpt{
						acpsdk.WithStartKind(kind),
						acpsdk.WithStartStatus(status),
						acpsdk.WithStartRawInput(rawInput),
					}
					if len(locations) > 0 {
						startOpts = append(startOpts, acpsdk.WithStartLocations(locations))
					}
					if len(content) > 0 {
						startOpts = append(startOpts, acpsdk.WithStartContent(content))
					}
					startUpdate := acpsdk.StartToolCall(acpsdk.ToolCallId(tc.ID), title, startOpts...)
					if startUpdate.ToolCall != nil {
						startUpdate.ToolCall.Meta = toolMeta
					}
					return acpSession.SendUpdate(startUpdate)
				}

				// Always update the stored input; for edit tools sendWriteTextFile also reads it.
				a.pendingToolCallsMu.Lock()
				a.pendingToolCalls[tc.ID] = tc.Input
				started := a.startedToolCalls[tc.ID]
				a.pendingToolCallsMu.Unlock()

				if !tc.Finished {
					if !started {
						if err := sendStart(acpsdk.ToolCallStatusPending); err != nil {
							a.logger.Printf("[ACP AGENT] Failed to send tool call pending: %v", err)
						} else {
							a.pendingToolCallsMu.Lock()
							a.startedToolCalls[tc.ID] = true
							a.pendingToolCallsMu.Unlock()
						}
					}
				} else {
					if !started {
						if err := sendStart(acpsdk.ToolCallStatusInProgress); err != nil {
							a.logger.Printf("[ACP AGENT] Failed to send synthetic tool call start: %v", err)
						} else {
							a.pendingToolCallsMu.Lock()
							a.startedToolCalls[tc.ID] = true
							a.pendingToolCallsMu.Unlock()
						}
					}

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

		case AgentEventTypeToolResult:
			if event.ToolResult != nil {
				tr := event.ToolResult

				// TodoWrite results are suppressed — the plan notification already
				// carries all relevant information. Clean up pending state and skip.
				if strings.EqualFold(tr.Name, "TodoWrite") {
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

				// Guarantee that a tool_call (start) always precedes the first
				// tool_call_update for the same toolCallId.  Without it Zed (and other
				// ACP clients) have no entry for the tool call and silently ignore the
				// update, so the tool never appears in the conversation panel.
				if !wasStarted {
					synthKind := mapToolKind(tr.Name)
					synthRawInput := parseJSONInput(storedInput)
					synthTitle := toolDisplayTitle(tr.Name, synthRawInput, acpSession.WorkDir)
					synthContent := toolCallContent(tr.Name, synthRawInput)
					synthLocations := toLocations(tr.Name, storedInput)
					synthMeta := map[string]any{"pando": map[string]any{"toolName": tr.Name}}
					if isBashTool(tr.Name) {
						synthMeta["terminal_info"] = map[string]any{"terminal_id": tr.ToolCallID}
						synthContent = []acpsdk.ToolCallContent{acpsdk.ToolTerminalRef(tr.ToolCallID)}
					}
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
				rawInput := parseJSONInput(storedInput)

				// Build rawOutput matching the opencode format: { output, metadata }.
				rawOutput := map[string]interface{}{
					"output": tr.Content,
				}
				if tr.Metadata != "" {
					var meta interface{}
					if jerr := json.Unmarshal([]byte(tr.Metadata), &meta); jerr == nil {
						rawOutput["metadata"] = meta
					} else {
						rawOutput["metadata"] = tr.Metadata
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
				resultMeta := map[string]any{
					"pando": map[string]any{
						"toolName": tr.Name,
					},
				}

				// For Bash tools, send terminal_output + terminal_exit via _meta
				// so editors can display the output in their terminal widgets.
				// This matches claude-agent-acp's streaming lifecycle:
				//   1. tool_call       → _meta.terminal_info  (sent above in ToolCall)
				//   2. tool_call_update → _meta.terminal_output (sent here)
				//   3. tool_call_update → _meta.terminal_exit   (sent here)
				if isBashTool(tr.Name) {
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

					// Step 3: attach terminal_exit to the final result update.
					exitCode := 0
					if tr.IsError {
						exitCode = 1
					}
					resultMeta["terminal_exit"] = map[string]any{
						"terminal_id": tr.ToolCallID,
						"exit_code":   exitCode,
						"signal":      nil,
					}

					// Bash tool result content: only terminal ref (output already sent
					// via terminal_output _meta above). Text block fallback is omitted
					// because it causes duplicate display in clients that support terminal
					// widgets — matching claude-agent-acp's approach.
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
			a.logger.Printf("[ACP AGENT] Summarize event")
		}
	}

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
			update := acpsdk.UpdateAgentMessageText(content.String())
			if err := acpSession.SendUpdate(update); err != nil {
				a.logger.Printf("[ACP AGENT] Failed to send message update: %v", err)
			}
		}
	}

	if !sentThinkingDeltas {
		if reasoning := msg.ReasoningContent(); reasoning.String() != "" {
			update := acpsdk.UpdateAgentThoughtText(reasoning.String())
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
				if err := acpSession.SendUpdate(acpsdk.UpdatePlan(entries...)); err != nil {
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
		_, alreadyRegistered := a.pendingToolCalls[toolCall.ID]
		// hadEmptyInput is true when EventToolUseStart stored "" (empty) and
		// EventToolUseStop was silently dropped because the 256-slot event buffer
		// was full (non-blocking send with default:). In that case the streaming
		// path sent StartToolCall with title="<toolname>" and rawInput={}, so we
		// must send a corrective UpdateToolCall here with the full command info.
		hadEmptyInput := alreadyRegistered && a.pendingToolCalls[toolCall.ID] == ""
		if !alreadyRegistered {
			a.pendingToolCalls[toolCall.ID] = toolCall.Input
		} else if a.pendingToolCalls[toolCall.ID] == "" {
			// Registered by ToolUseStart with empty input; update to full input now.
			a.pendingToolCalls[toolCall.ID] = toolCall.Input
		}
		a.pendingToolCallsMu.Unlock()

		if alreadyRegistered {
			// StartToolCall was already sent by the streaming path.
			// If EventToolUseStop was dropped (hadEmptyInput), send a corrective
			// UpdateToolCall so the client displays the correct command title and
			// rawInput instead of just the tool name (e.g. "bash").
			if hadEmptyInput && toolCall.Input != "" {
				kind := mapToolKind(toolCall.Name)
				rawInput := parseJSONInput(toolCall.Input)
				title := toolDisplayTitle(toolCall.Name, rawInput, acpSession.WorkDir)
				content := toolCallContent(toolCall.Name, rawInput)
				locations := toLocations(toolCall.Name, toolCall.Input)
				toolMeta := map[string]any{"pando": map[string]any{"toolName": toolCall.Name}}
				if isBashTool(toolCall.Name) {
					toolMeta["terminal_info"] = map[string]any{"terminal_id": toolCall.ID}
					content = []acpsdk.ToolCallContent{acpsdk.ToolTerminalRef(toolCall.ID)}
				}
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

		// Non-streaming provider: send StartToolCall now so the client knows the tool name.
		kind := mapToolKind(toolCall.Name)
		rawInput := parseJSONInput(toolCall.Input)
		title := toolDisplayTitle(toolCall.Name, rawInput, acpSession.WorkDir)
		content := toolCallContent(toolCall.Name, rawInput)
		locations := toLocations(toolCall.Name, toolCall.Input)

		toolMeta := map[string]any{
			"pando": map[string]any{"toolName": toolCall.Name},
		}
		if isBashTool(toolCall.Name) {
			toolMeta["terminal_info"] = map[string]any{"terminal_id": toolCall.ID}
			content = []acpsdk.ToolCallContent{acpsdk.ToolTerminalRef(toolCall.ID)}
		}

		startOpts := []acpsdk.ToolCallStartOpt{
			acpsdk.WithStartKind(kind),
			acpsdk.WithStartStatus(acpsdk.ToolCallStatusInProgress),
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

		rawInput := parseJSONInput(storedInput)
		rawOutput := map[string]interface{}{"output": toolResult.Content}
		if toolResult.Metadata != "" {
			var meta interface{}
			if err := json.Unmarshal([]byte(toolResult.Metadata), &meta); err == nil {
				rawOutput["metadata"] = meta
			} else {
				rawOutput["metadata"] = toolResult.Metadata
			}
		}

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

		resultMeta := map[string]any{
			"pando": map[string]any{"toolName": toolResult.Name},
		}

		// Terminal streaming for Bash results (same as streaming path).
		if isBashTool(toolResult.Name) {
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
