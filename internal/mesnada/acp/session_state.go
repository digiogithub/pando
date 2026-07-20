package acp

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	llmmodels "github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/message"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

const (
	defaultACPMode                    = "agent"
	askModeID                         = "ask"
	agentModeID                       = "agent"
	goalModeID                        = "goal"
	cleanModeID                       = "clean"
	sessionConfigModelID              = "model"
	sessionConfigModeID               = "mode"
	sessionConfigAskPermissionID      = "askPermission"
	sessionConfigThinkingStreamModeID = "thinking_stream_mode"
	sessionConfigReasoningEffortID    = "reasoning_effort"
	sessionConfigThinkingModeID       = "thinking_mode"
	sessionConfigAgentID              = "agent"
	askPermissionYesValue             = "yes"
	askPermissionNoValue              = "no"
	reasoningEffortLow                = "low"
	reasoningEffortMedium             = "medium"
	reasoningEffortHigh               = "high"
	thinkingStreamModeOff             = "off"
	thinkingStreamModeGrouped         = "grouped"
	thinkingStreamModeFull            = "full"
	thinkingModeDisabled              = "disabled"
	thinkingModeLow                   = "low"
	thinkingModeMedium                = "medium"
	thinkingModeHigh                  = "high"
	defaultACPReasoningEffort         = reasoningEffortMedium
	defaultACPThinkingMode            = thinkingModeMedium
	defaultACPThinkingStreamMode      = thinkingStreamModeGrouped
	goalCommandToken                  = "goal"
	autopilotCommandToken             = "autopilot"
	goalStatusCommandToken            = "goal-status"
	goalCancelCommandToken            = "goal-cancel"
	compactCommandToken               = "compact"
	summarizeCommandToken             = "summarize"
	dbCompactCommandToken             = "db-compact"
	ponytailCommandToken              = "ponytail"
	improveAgentsMdCommandToken       = "improve-agents-md"
	superpowersCommandToken           = "superpowers"
	superpowersFinishCommandToken     = "superpowers-finish"
	cavemanCommandToken               = "caveman"
	cavemanFinishCommandToken         = "caveman-finish"
	learningCommandToken              = "learning"
	learningFinishCommandToken        = "learning-finish"
	vulnhuntCommandToken              = "vulnhunt"
	vulnhunterFixCommandToken         = "vulnhunter-fix"
	vulnhuntFixVerifyCommandToken     = "vulnhunt-fix-verify"
)

const (
	goalCommandName       = "/" + goalCommandToken
	autopilotCommandName  = "/" + autopilotCommandToken
	goalStatusCommandName = "/" + goalStatusCommandToken
	goalCancelCommandName = "/" + goalCancelCommandToken
	compactCommandName    = "/" + compactCommandToken
	summarizeCommandName  = "/" + summarizeCommandToken
	dbCompactCommandName  = "/" + dbCompactCommandToken
	ponytailCommandName   = "/" + ponytailCommandToken
)

func availableModes(_ AgentService) []acpsdk.SessionMode {
	descPtr := func(s string) *string { return &s }
	return []acpsdk.SessionMode{
		{
			Id:          agentModeID,
			Name:        "Agent",
			Description: descPtr("Use the coding agent workflow with tools available"),
		},
		{
			Id:          askModeID,
			Name:        "Ask",
			Description: descPtr("Prefer direct answers and avoid tool use unless needed"),
		},
		{
			Id:          goalModeID,
			Name:        "Goal",
			Description: descPtr("Persist an objective and work autonomously toward completion"),
		},
		{
			Id:          cleanModeID,
			Name:        "Clean",
			Description: descPtr("No additional instructions only your prompt"),
		},
	}
}

// buildSessionModeState constructs the SessionModeState for ACP responses.
func buildSessionModeState(svc AgentService, currentModeID string) *acpsdk.SessionModeState {
	if currentModeID == "" {
		currentModeID = defaultACPMode
	}
	return &acpsdk.SessionModeState{
		AvailableModes: availableModes(svc),
		CurrentModeId:  acpsdk.SessionModeId(currentModeID),
	}
}

// buildSessionModelState constructs the SessionModelState from the AgentService.
func buildSessionModelState(svc AgentService, currentModelID string) *acpsdk.SessionModelState {
	currentID := resolvedModelValue(svc, currentModelID)
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

func buildSessionPersonaState(svc AgentService, currentPersona string) *SessionPersonaState {
	available := svc.ListPersonas()
	active := resolvedPersonaValue(svc, currentPersona)

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

func buildSessionConfigOptions(svc AgentService, session *ACPServerSession) []acpsdk.SessionConfigOption {
	var (
		currentModel   string
		currentMode    string
		currentPersona string
		askPermission  bool
	)
	if session != nil {
		currentModel = session.Model()
		currentMode = session.Mode()
		currentPersona = session.Persona()
		askPermission = session.AskPermission()
	}
	if currentMode == "" {
		currentMode = defaultACPMode
	}
	selectedModel, hasSelectedModel := selectedACPModel(svc, currentModel)
	thinkingSettings := normalizedACPThinkingSettingsForSession(svc, session)
	thinkingStreamDescription := "How reasoning is shown while the model is generating a response"
	if hasSelectedModel && !supportsACPThinkingStreaming(selectedModel) {
		thinkingStreamDescription = "How reasoning would be shown if the selected model emitted thinking output"
	}

	options := []acpsdk.SessionConfigOption{
		newSelectConfigOption(
			sessionConfigModelID,
			"Model",
			"The model used for this session",
			"model",
			resolvedModelValue(svc, currentModel),
			buildModelConfigValues(svc),
		),
		newSelectConfigOption(
			sessionConfigModeID,
			"Mode",
			"The interaction mode exposed to ACP clients",
			"mode",
			currentMode,
			[]configOptionValue{
				{Value: agentModeID, Name: "Agent", Description: "Use the coding agent workflow with tools available"},
				{Value: askModeID, Name: "Ask", Description: "Prefer direct answers and avoid tool use unless needed"},
				{Value: goalModeID, Name: "Goal", Description: "Persist an objective and work autonomously toward completion"},
				{Value: cleanModeID, Name: "Clean", Description: "No additional instructions only your prompt"},
			},
		),
		newSelectConfigOption(
			sessionConfigAskPermissionID,
			"Ask Permission",
			"Whether tool calls should wait for approval before running",
			"_permission",
			boolToAskPermissionValue(askPermission),
			[]configOptionValue{
				{Value: askPermissionYesValue, Name: "Yes", Description: "Request approval before each tool call"},
				{Value: askPermissionNoValue, Name: "No", Description: "Automatically approve tool calls"},
			},
		),
		newSelectConfigOption(
			sessionConfigThinkingStreamModeID,
			"Thinking Visibility",
			thinkingStreamDescription,
			"thinking",
			thinkingSettings.ThinkingStreamMode,
			[]configOptionValue{
				{Value: thinkingStreamModeOff, Name: "Off", Description: "Show only the final reasoning summary"},
				{Value: thinkingStreamModeGrouped, Name: "Grouped", Description: "Show reasoning in periodic grouped blocks"},
				{Value: thinkingStreamModeFull, Name: "Full", Description: "Show every reasoning chunk as it arrives; may be noisy"},
			},
		),
	}

	if hasSelectedModel && supportsACPReasoningEffort(selectedModel) {
		options = append(options, newSelectConfigOption(
			sessionConfigReasoningEffortID,
			"Reasoning Effort",
			"How much reasoning effort the selected model should use",
			"thinking",
			thinkingSettings.ReasoningEffort,
			[]configOptionValue{
				{Value: reasoningEffortLow, Name: "Low", Description: "Prefer faster responses with less reasoning effort"},
				{Value: reasoningEffortMedium, Name: "Medium", Description: "Use a balanced amount of reasoning effort"},
				{Value: reasoningEffortHigh, Name: "High", Description: "Prefer deeper reasoning when the model supports it"},
			},
		))
	}

	if hasSelectedModel && supportsACPThinkingMode(selectedModel) {
		options = append(options, newSelectConfigOption(
			sessionConfigThinkingModeID,
			"Thinking Mode",
			"How much extended thinking the selected Anthropic model should use",
			"thinking",
			thinkingSettings.ThinkingMode,
			[]configOptionValue{
				{Value: thinkingModeDisabled, Name: "Disabled", Description: "Disable extended thinking for this session"},
				{Value: thinkingModeLow, Name: "Low", Description: "Use a small amount of extended thinking"},
				{Value: thinkingModeMedium, Name: "Medium", Description: "Use a balanced amount of extended thinking"},
				{Value: thinkingModeHigh, Name: "High", Description: "Use the deepest available extended thinking"},
			},
		))
	}

	if personaOption := buildPersonaConfigOption(svc, currentPersona); personaOption != nil {
		options = append(options, *personaOption)
	}

	return options
}

func buildUnstableSessionConfigOptions(opts []acpsdk.SessionConfigOption) []acpsdk.UnstableSessionConfigOption {
	if len(opts) == 0 {
		return nil
	}
	raw, err := json.Marshal(opts)
	if err != nil {
		return nil
	}
	var unstable []acpsdk.UnstableSessionConfigOption
	if err := json.Unmarshal(raw, &unstable); err != nil {
		return nil
	}
	return unstable
}

type configOptionValue struct {
	Value       string
	Name        string
	Description string
}

func newSelectConfigOption(id, name, description, category, currentValue string, values []configOptionValue) acpsdk.SessionConfigOption {
	ungrouped := make(acpsdk.SessionConfigSelectOptionsUngrouped, 0, len(values))
	for _, value := range values {
		option := acpsdk.SessionConfigSelectOption{
			Name:  value.Name,
			Value: acpsdk.SessionConfigValueId(value.Value),
		}
		if value.Description != "" {
			desc := value.Description
			option.Description = &desc
		}
		ungrouped = append(ungrouped, option)
	}

	opt := acpsdk.NewSessionConfigOptionSelect(
		acpsdk.SessionConfigValueId(currentValue),
		acpsdk.SessionConfigSelectOptions{Ungrouped: &ungrouped},
	)
	opt.Select.Id = acpsdk.SessionConfigId(id)
	opt.Select.Name = name
	if description != "" {
		desc := description
		opt.Select.Description = &desc
	}
	if category != "" {
		opt.Select.Category = sessionConfigCategory(category)
	}
	return opt
}

func buildPersonaConfigOption(svc AgentService, currentPersona string) *acpsdk.SessionConfigOption {
	personas := svc.ListPersonas()
	if len(personas) == 0 {
		return nil
	}

	values := make([]configOptionValue, 0, len(personas))
	for _, persona := range personas {
		persona = strings.TrimSpace(persona)
		if persona == "" {
			continue
		}
		values = append(values, configOptionValue{
			Value:       persona,
			Name:        persona,
			Description: "Use persona " + persona + " for this session",
		})
	}
	if len(values) == 0 {
		return nil
	}

	current := resolvedPersonaValue(svc, currentPersona)
	if current == "" {
		current = values[0].Value
	}

	opt := newSelectConfigOption(
		sessionConfigAgentID,
		"Agent",
		"Select the persona used for this session",
		"_agent",
		current,
		values,
	)
	return &opt
}

func selectedACPModel(svc AgentService, currentModel string) (llmmodels.Model, bool) {
	modelID := strings.TrimSpace(resolvedModelValue(svc, currentModel))
	if modelID == "" {
		return llmmodels.Model{}, false
	}
	model, ok := llmmodels.SupportedModels[llmmodels.ModelID(modelID)]
	if ok {
		return model, true
	}
	resolvedID, resolved := llmmodels.ResolveModelID(llmmodels.ModelID(modelID))
	if !resolved {
		return llmmodels.Model{}, false
	}
	model, ok = llmmodels.SupportedModels[resolvedID]
	return model, ok
}

func sessionConfigCategory(name string) *acpsdk.SessionConfigOptionCategory {
	category := acpsdk.SessionConfigOptionCategoryOther(name)
	return &acpsdk.SessionConfigOptionCategory{Other: &category}
}

func resolvedPersonaValue(svc AgentService, currentPersona string) string {
	currentPersona = strings.TrimSpace(currentPersona)
	if currentPersona != "" {
		return currentPersona
	}

	active := strings.TrimSpace(svc.GetActivePersona())
	if active != "" {
		return active
	}

	for _, persona := range svc.ListPersonas() {
		persona = strings.TrimSpace(persona)
		if persona != "" {
			return persona
		}
	}

	return ""
}

func buildModelConfigValues(svc AgentService) []configOptionValue {
	available := svc.AvailableModels()
	values := make([]configOptionValue, 0, len(available))
	for _, model := range available {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = id
		}
		values = append(values, configOptionValue{
			Value:       id,
			Name:        name,
			Description: "Use model " + name,
		})
	}
	return values
}

func resolvedModelValue(svc AgentService, currentModel string) string {
	currentModel = strings.TrimSpace(currentModel)
	if currentModel != "" {
		return currentModel
	}
	return strings.TrimSpace(svc.CurrentModelID())
}

func boolToAskPermissionValue(enabled bool) string {
	if enabled {
		return askPermissionYesValue
	}
	return askPermissionNoValue
}

// availableCommandsPostResponseDelay defers the available_commands_update
// notification until after the enclosing request's JSON-RPC response has been
// written to the wire.
//
// The notification and the session/new (or session/load) response both compete
// for the SDK connection's single writeMu. If the notification wins the race it
// is delivered before the response, so the client (Zed) receives commands for a
// session it has not registered yet and silently drops them ("Available
// commands: none"). Modern Zed requires the session response first. The
// canonical claude-agent-acp wrapper handles this by scheduling the send on the
// next event-loop tick (setTimeout(0), "Needs to happen after we return the
// session"); this delay is the Go equivalent — the response is written
// synchronously right after the handler returns, so a short pause reliably lets
// it acquire writeMu first.
const availableCommandsPostResponseDelay = 50 * time.Millisecond

// scheduleAvailableCommandsUpdate sends the available_commands_update
// notification after the current request handler has returned and its response
// has been flushed. Callers running inside a request handler (NewSession,
// LoadSession) MUST use this instead of a bare `go sendAvailableCommandsUpdate`
// to avoid racing the response onto the wire (see delay constant above).
func (a *PandoACPAgent) scheduleAvailableCommandsUpdate(sessionID acpsdk.SessionId) {
	go func() {
		time.Sleep(availableCommandsPostResponseDelay)
		a.sendAvailableCommandsUpdate(context.Background(), sessionID)
	}()
}

// sendAvailableCommandsUpdate publishes the ACP slash commands supported by Pando.
func (a *PandoACPAgent) sendAvailableCommandsUpdate(ctx context.Context, sessionID acpsdk.SessionId) {
	// Read the connection under sessionsMu (the same lock SetConnection writes it
	// under) and pin it to a local: this method runs in a goroutine spawned by
	// NewSession/SetConnection, so an unsynchronized a.conn read would race a
	// concurrent SetConnection.
	conn := a.getConn()
	if conn == nil {
		return
	}

	update := acpsdk.SessionNotification{
		SessionId: sessionID,
		Update:    acpsdk.UpdateAvailableCommands(availableCommands()...),
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				a.logger.Printf("[ACP AGENT] sendAvailableCommandsUpdate: recovered from panic for session %s: %v", sessionID, r)
			}
		}()
		if err := conn.SessionUpdate(ctx, update); err != nil {
			if isEntityReleasedError(err) || strings.Contains(err.Error(), "unknown session") {
				a.logger.Printf("[ACP AGENT] sendAvailableCommandsUpdate: skipped session %s after reconnect: %v", sessionID, err)
				return
			}
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
						sendUpdate(updateUserMessageTextWithID(p.Text, msg.ID))
					}
				}
			}

		case message.Assistant:
			// Send assistant message: text parts, thinking parts, and tool calls
			for _, part := range msg.Parts {
				switch p := part.(type) {
				case message.TextContent:
					if p.Text != "" {
						sendUpdate(updateAgentMessageTextWithID(p.Text, msg.ID))
					}
				case message.ReasoningContent:
					if p.Thinking != "" {
						sendUpdate(updateAgentThoughtTextWithID(p.Thinking, msg.ID))
					}
				case message.ToolCall:
					// TodoWrite → plan: emit a plan notification instead of a
					// regular tool_call so history playback matches live behaviour.
					if strings.EqualFold(p.Name, "TodoWrite") {
						if entries := parseTodoWritePlan(p.Input); len(entries) > 0 {
							if err := a.sendPlanModeUpdate(acpSession, entries); err != nil {
								a.logger.Printf("[ACP AGENT] streamSessionHistory: failed to send plan replay for session %s: %v", sessionID, err)
							}
						}
						continue
					}

					// Replay the tool call with full metadata so clients display
					// proper title, kind, content, and locations.
					rawInput := parseRawInput(p.Input)
					toolCallID := acpsdk.ToolCallId(p.ID)
					kind := mapToolKind(p.Name)
					title := toolDisplayTitle(p.Name, rawInput, workDir)
					content := a.toolStartContent(p.Name, p.ID, toolCallContent(p.Name, rawInput))
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

					toolMeta := a.toolMeta(p.Name, p.ID, true)
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
					// Browser screenshots may contain very large base64 payloads. Omit them
					// from resumed-session history so they do not bloat the next prompt.
					if strings.EqualFold(tr.Name, "browser_screenshot") {
						continue
					}

					status := acpsdk.ToolCallStatusCompleted
					if tr.IsError {
						status = acpsdk.ToolCallStatusFailed
					}

					// Build structured rawOutput matching the streaming path.
					rawOutput := buildRawOutput(tr.Content, tr.Metadata, tr.IsError)

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

					rawInput := parseRawInput(storedInput)
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
					resultMeta := a.toolMeta(tr.Name, tr.ToolCallID, false)

					// For Bash tools, send terminal_output + terminal_exit via _meta
					// so clients display the output in their terminal widgets.
					if a.terminalOutputEnabled() && isBashTool(tr.Name) {
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
