package acp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/digiogithub/pando/internal/message"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// availableModes returns the fixed set of session modes supported by Pando,
// plus persona-specific combinations exposed as ACP modes.
func availableModes(svc AgentService) []acpsdk.SessionMode {
	descPtr := func(s string) *string { return &s }
	modes := []acpsdk.SessionMode{
		{
			Id:          "agent",
			Name:        "Agent",
			Description: descPtr("Full agent — tools auto-approved without prompting"),
		},
		{
			Id:          "ask",
			Name:        "Ask",
			Description: descPtr("Ask for permission before each tool use"),
		},
	}

	if svc == nil {
		return modes
	}

	for _, persona := range svc.ListPersonas() {
		persona = strings.TrimSpace(persona)
		if persona == "" {
			continue
		}

		modes = append(modes,
			acpsdk.SessionMode{
				Id:          acpsdk.SessionModeId(persona + ":agent"),
				Name:        persona + ": yolo",
				Description: descPtr("Agent mode with persona " + persona),
			},
			acpsdk.SessionMode{
				Id:          acpsdk.SessionModeId(persona + ":ask"),
				Name:        persona + ": ask",
				Description: descPtr("Ask mode with persona " + persona),
			},
		)
	}

	return modes
}

// buildSessionModeState constructs the SessionModeState for ACP responses.
func buildSessionModeState(svc AgentService, currentModeID string, currentPersona string) *acpsdk.SessionModeState {
	if currentModeID == "" {
		currentModeID = "agent"
	}
	currentModeKey := currentModeID
	if currentPersona != "" {
		currentModeKey = currentPersona + ":" + currentModeID
	}
	return &acpsdk.SessionModeState{
		AvailableModes: availableModes(svc),
		CurrentModeId:  acpsdk.SessionModeId(currentModeKey),
	}
}

// buildSessionModelState constructs the SessionModelState from the AgentService.
func buildSessionModelState(svc AgentService) *acpsdk.SessionModelState {
	currentID := svc.CurrentModelID()
	available := svc.AvailableModels()

	infos := make([]acpsdk.ModelInfo, 0, len(available))
	for _, m := range available {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		infos = append(infos, acpsdk.ModelInfo{
			ModelId: acpsdk.ModelId(m.ID),
			Name:    name,
		})
	}

	return &acpsdk.SessionModelState{
		AvailableModels: infos,
		CurrentModelId:  acpsdk.ModelId(currentID),
	}
}

// personaStateToMeta serialises SessionPersonaState into a map[string]any
// suitable for placement in ACP _meta fields (which changed from any to
// map[string]any in SDK v0.12).
func personaStateToMeta(s *SessionPersonaState) map[string]any {
	b, err := json.Marshal(s)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}

// buildSessionPersonaState constructs the SessionPersonaState from the AgentService.
func buildSessionPersonaState(svc AgentService) *SessionPersonaState {
	available := svc.ListPersonas()
	active := svc.GetActivePersona()

	infos := make([]PersonaInfo, 0, len(available))
	for _, name := range available {
		infos = append(infos, PersonaInfo{
			ID:   name,
			Name: name,
		})
	}

	return &SessionPersonaState{
		AvailablePersonas: infos,
		CurrentPersonaId:  active,
	}
}

// sendAvailableCommandsUpdate sends an available_commands_update SessionUpdate to the
// connected ACP client so that clients (Zed, multicoder, etc.) can display the tool
// names and descriptions. This mirrors how opencode sends the update after session
// creation/loading.
func (a *PandoACPAgent) sendAvailableCommandsUpdate(ctx context.Context, sessionID acpsdk.SessionId) {
	if a.conn == nil {
		return
	}

	tools := a.agentService.ListAvailableTools()
	commands := make([]acpsdk.AvailableCommand, 0, len(tools))
	for _, t := range tools {
		commands = append(commands, acpsdk.AvailableCommand{
			Name:        t.Name,
			Description: t.Description,
		})
	}

	update := acpsdk.SessionNotification{
		SessionId: sessionID,
		Update: acpsdk.SessionUpdate{
			AvailableCommandsUpdate: &acpsdk.SessionAvailableCommandsUpdate{
				AvailableCommands: commands,
				SessionUpdate:     "available_commands_update",
			},
		},
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				a.logger.Printf("[ACP AGENT] sendAvailableCommandsUpdate: recovered from panic for session %s: %v", sessionID, r)
			}
		}()
		if err := a.conn.SessionUpdate(ctx, update); err != nil {
			a.logger.Printf("[ACP AGENT] sendAvailableCommandsUpdate: failed for session %s: %v", sessionID, err)
		}
	}()
}

// streamSessionHistory replays a session's conversation history to the connected ACP client.
// It sends each message as a sequence of SessionUpdate notifications so that the client
// (e.g. Zed) can reconstruct the conversation view when loading an existing session.
// This is called asynchronously after LoadSession returns its response.
func (a *PandoACPAgent) streamSessionHistory(ctx context.Context, sessionID acpsdk.SessionId, pandoSessionID string) {
	if a.conn == nil {
		a.logger.Printf("[ACP AGENT] streamSessionHistory: no connection available for session %s", sessionID)
		return
	}

	msgs, err := a.sessionService.GetMessages(ctx, pandoSessionID)
	if err != nil {
		a.logger.Printf("[ACP AGENT] streamSessionHistory: failed to get messages for session %s: %v", sessionID, err)
		return
	}

	if len(msgs) == 0 {
		a.logger.Printf("[ACP AGENT] streamSessionHistory: no messages to replay for session %s", sessionID)
		return
	}

	a.logger.Printf("[ACP AGENT] streamSessionHistory: replaying %d messages for session %s", len(msgs), sessionID)

	sendUpdate := func(update acpsdk.SessionUpdate) {
		notification := acpsdk.SessionNotification{
			SessionId: sessionID,
			Update:    update,
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					a.logger.Printf("[ACP AGENT] streamSessionHistory: recovered from panic for session %s: %v", sessionID, r)
				}
			}()
			if err := a.conn.SessionUpdate(ctx, notification); err != nil {
				a.logger.Printf("[ACP AGENT] streamSessionHistory: failed to send update for session %s: %v", sessionID, err)
			}
		}()
	}

	// Retrieve the session's working directory for display-path formatting.
	a.sessionsMu.RLock()
	acpSession := a.sessions[sessionID]
	a.sessionsMu.RUnlock()
	workDir := a.workDir
	if acpSession != nil {
		workDir = acpSession.WorkDir
	}

	for _, msg := range msgs {
		switch msg.Role {
		case message.User:
			// Send user message text parts
			for _, part := range msg.Parts {
				switch p := part.(type) {
				case message.TextContent:
					if p.Text != "" {
						sendUpdate(acpsdk.UpdateUserMessageText(p.Text))
					}
				}
			}

		case message.Assistant:
			// Send assistant message: text parts, thinking parts, and tool calls
			for _, part := range msg.Parts {
				switch p := part.(type) {
				case message.TextContent:
					if p.Text != "" {
						sendUpdate(acpsdk.UpdateAgentMessageText(p.Text))
					}
				case message.ReasoningContent:
					if p.Thinking != "" {
						sendUpdate(acpsdk.UpdateAgentThoughtText(p.Thinking))
					}
				case message.ToolCall:
					// TodoWrite → plan: emit a plan notification instead of a
					// regular tool_call so history playback matches live behaviour.
					if strings.EqualFold(p.Name, "TodoWrite") {
						if entries := parseTodoWritePlan(p.Input); len(entries) > 0 {
							sendUpdate(acpsdk.UpdatePlan(entries...))
						}
						continue
					}

					// Replay the tool call with full metadata so clients display
					// proper title, kind, content, and locations.
					rawInput := parseJSONInput(p.Input)
					toolCallID := acpsdk.ToolCallId(p.ID)
					kind := mapToolKind(p.Name)
					title := toolDisplayTitle(p.Name, rawInput, workDir)
					content := toolCallContent(p.Name, rawInput)
					locations := toLocations(p.Name, p.Input)

					startOpts := []acpsdk.ToolCallStartOpt{
						acpsdk.WithStartKind(kind),
						acpsdk.WithStartStatus(acpsdk.ToolCallStatusPending),
						acpsdk.WithStartRawInput(rawInput),
					}
					if len(content) > 0 {
						startOpts = append(startOpts, acpsdk.WithStartContent(content))
					}
					if len(locations) > 0 {
						startOpts = append(startOpts, acpsdk.WithStartLocations(locations))
					}
					startUpdate := acpsdk.StartToolCall(toolCallID, title, startOpts...)

					// Add _meta with toolName and terminal_info for Bash tools.
					toolMeta := map[string]any{
						"pando": map[string]any{"toolName": p.Name},
					}
					if isBashTool(p.Name) {
						toolMeta["terminal_info"] = map[string]any{"terminal_id": p.ID}
						if startUpdate.ToolCall != nil {
							startUpdate.ToolCall.Content = []acpsdk.ToolCallContent{acpsdk.ToolTerminalRef(p.ID)}
						}
					}
					if startUpdate.ToolCall != nil {
						startUpdate.ToolCall.Meta = toolMeta
					}
					sendUpdate(startUpdate)
				}
			}

		case message.Tool:
			// Tool results: update existing tool calls with full output details.
			for _, part := range msg.Parts {
				if tr, ok := part.(message.ToolResult); ok {
					// TodoWrite results are suppressed in history — the plan notification
					// emitted above already carries all relevant information.
					if strings.EqualFold(tr.Name, "TodoWrite") {
						continue
					}

					status := acpsdk.ToolCallStatusCompleted
					if tr.IsError {
						status = acpsdk.ToolCallStatusFailed
					}

					// Build structured rawOutput matching the streaming path.
					rawOutput := map[string]interface{}{"output": tr.Content}
					if tr.Metadata != "" {
						var meta interface{}
						if jerr := json.Unmarshal([]byte(tr.Metadata), &meta); jerr == nil {
							rawOutput["metadata"] = meta
						} else {
							rawOutput["metadata"] = tr.Metadata
						}
					}

					// Use the tool name to reconstruct rich content.
					outputContent := toolResultContent(tr.Name, tr.Content, tr.IsError)

					kind := mapToolKind(tr.Name)
					// Retrieve stored input from the matching tool call, if available.
					storedInput := ""
					for _, assistMsg := range msgs {
						if assistMsg.Role != message.Assistant {
							continue
						}
						for _, ap := range assistMsg.Parts {
							if tc, ok := ap.(message.ToolCall); ok && tc.ID == tr.ToolCallID {
								storedInput = tc.Input
								break
							}
						}
						if storedInput != "" {
							break
						}
					}

					rawInput := parseJSONInput(storedInput)
					title := toolDisplayTitle(tr.Name, rawInput, workDir)

					// For edit tools, attach a diff content block.
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

					// Build _meta for the result.
					resultMeta := map[string]any{
						"pando": map[string]any{"toolName": tr.Name},
					}

					// For Bash tools, send terminal_output + terminal_exit via _meta
					// so clients display the output in their terminal widgets.
					if isBashTool(tr.Name) {
						termOutput := strings.TrimSpace(tr.Content)
						if termOutput != "" {
							// Step 2: terminal_output notification — only _meta, no content
							// field, matching claude-agent-acp to avoid double rendering.
							termOutputUpdate := acpsdk.UpdateToolCall(acpsdk.ToolCallId(tr.ToolCallID))
							if termOutputUpdate.ToolCallUpdate != nil {
								termOutputUpdate.ToolCallUpdate.Meta = map[string]any{
									"terminal_output": map[string]any{
										"terminal_id": tr.ToolCallID,
										"data":        termOutput,
									},
								}
							}
							sendUpdate(termOutputUpdate)
						}

						// Step 3: attach terminal_exit to the final result.
						exitCode := 0
						if tr.IsError {
							exitCode = 1
						}
						resultMeta["terminal_exit"] = map[string]any{
							"terminal_id": tr.ToolCallID,
							"exit_code":   exitCode,
							"signal":      nil,
						}

						// Only terminal ref in content — no text block fallback,
						// matching claude-agent-acp behavior.
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
					if resultUpdate.ToolCallUpdate != nil {
						resultUpdate.ToolCallUpdate.Meta = resultMeta
					}
					sendUpdate(resultUpdate)
				}
			}
		}
	}

	a.logger.Printf("[ACP AGENT] streamSessionHistory: completed replaying history for session %s", sessionID)
}
