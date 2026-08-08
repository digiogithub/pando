package prompt

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/luaengine"
)

// PromptEvaluatorTemplate mirrors evaluator.PromptTemplate to avoid import cycles.
type PromptEvaluatorTemplate struct {
	ID      string
	Content string
	Version int
}

// PromptEvaluatorSkill mirrors evaluator.Skill to avoid import cycles.
type PromptEvaluatorSkill struct {
	Content string
}

// PromptEvaluator is the interface used by PromptBuilder for UCB template
// selection and skill injection. It is exported so app.go can implement an adapter
// without creating an import cycle.
type PromptEvaluator interface {
	SelectTemplate(ctx context.Context, sectionName string) (*PromptEvaluatorTemplate, error)
	GetActiveSkills(ctx context.Context, taskType string) ([]PromptEvaluatorSkill, error)
	RecordTemplateSelection(ctx context.Context, sessionID, templateID string)
	// ClassifyTask returns a task type label from the user's first message.
	// Returns "general" if no pattern matches.
	ClassifyTask(text string) string
}

// promptEvaluator is the internal alias kept for backward-compatibility within this file.
type promptEvaluator = PromptEvaluator

// evaluatorTemplate is the internal alias for PromptEvaluatorTemplate.
type evaluatorTemplate = PromptEvaluatorTemplate

// evaluatorSkill is the internal alias for PromptEvaluatorSkill.
type evaluatorSkill = PromptEvaluatorSkill

// sessionIDKey is the context key used to retrieve the current session ID.
type sessionIDKey struct{}

// SessionIDKey is the exported context key for storing the session ID in context.
var SessionIDKey = sessionIDKey{}

// PromptBuilder composes a final system prompt from template sections.
type PromptBuilder struct {
	agentName string
	provider  string
	data      *PromptData
	luaMgr    *luaengine.FilterManager
	registry  *TemplateRegistry
	evaluator promptEvaluator
}

// SetEvaluator wires an evaluator into the builder for UCB template selection
// and skill injection. If e is nil the builder falls back to its default behaviour.
func (b *PromptBuilder) SetEvaluator(e promptEvaluator) {
	b.evaluator = e
}

// NewPromptBuilder creates a new PromptBuilder.
func NewPromptBuilder(agentName string, provider string, data *PromptData, luaMgr *luaengine.FilterManager) *PromptBuilder {
	// Build override directories from data
	var overrideDirs []string
	if data != nil && data.WorkingDir != "" {
		overrideDirs = append(overrideDirs, data.WorkingDir+"/.pando/templates")
	}
	if home, err := homeDir(); err == nil && home != "" {
		overrideDirs = append(overrideDirs, home+"/.config/pando/templates")
	}

	return &PromptBuilder{
		agentName: agentName,
		provider:  provider,
		data:      data,
		luaMgr:    luaMgr,
		registry:  NewTemplateRegistry(overrideDirs...),
	}
}

// Build composes the final system prompt by rendering all applicable template
// sections and joining them together.
func (b *PromptBuilder) Build(ctx context.Context) (string, error) {
	var sections []PromptSection

	// 1. Base identity
	if s := b.renderSection(ctx, "base/identity"); s.Content != "" {
		sections = append(sections, s)
	}

	// 2. Agent-specific section
	agentName := strings.ToLower(b.agentName)
	if agentName != "" && b.registry.Exists("agents/"+agentName) {
		if s := b.renderSection(ctx, "agents/"+agentName); s.Content != "" {
			sections = append(sections, s)
		}
	}

	// The context enricher is a read-only retrieval loop: the shared coding workflow
	// and code-style rules would only push it towards writing code it must not write.
	retrievalOnlyAgent := agentName == string(config.AgentContextEnricher)

	// 3. Workflow guidelines (shared behavioral rules)
	if !retrievalOnlyAgent && b.registry.Exists("base/workflow") {
		if s := b.renderSection(ctx, "base/workflow"); s.Content != "" {
			sections = append(sections, s)
		}
	}

	// 4. Coding conventions (language-agnostic code style rules)
	if !retrievalOnlyAgent && b.registry.Exists("base/conventions") {
		if s := b.renderSection(ctx, "base/conventions"); s.Content != "" {
			sections = append(sections, s)
		}
	}

	// 5. Environment
	if s := b.renderSection(ctx, "base/environment"); s.Content != "" {
		sections = append(sections, s)
	}

	// 6. Priority capabilities that should guide the initial analysis flow.
	priorityCapabilities := []struct {
		name      string
		available bool
	}{
		{name: "remembrances", available: b.data.HasRemembrances},
		{name: "orchestration", available: b.data.HasOrchestration},
	}
	for _, capability := range priorityCapabilities {
		if b.checkCapability(ctx, capability.name, capability.available) && b.registry.Exists("capabilities/"+capability.name) {
			if s := b.renderSection(ctx, "capabilities/"+capability.name); s.Content != "" {
				sections = append(sections, s)
			}
		}
	}

	// 7. Provider-specific section (with optional Lua override)
	providerTemplate := b.selectProvider(ctx)
	if providerTemplate != "" && b.registry.Exists(providerTemplate) {
		if s := b.renderSection(ctx, providerTemplate); s.Content != "" {
			sections = append(sections, s)
		}
	}

	// 8. Remaining capabilities
	remainingCapabilities := []struct {
		name      string
		available bool
	}{
		{name: "web_search", available: b.data.HasWebSearch},
		{name: "code_indexing", available: b.data.HasCodeIndexing},
		{name: "lsp", available: b.data.HasLSP},
	}
	for _, capability := range remainingCapabilities {
		if b.checkCapability(ctx, capability.name, capability.available) && b.registry.Exists("capabilities/"+capability.name) {
			if s := b.renderSection(ctx, "capabilities/"+capability.name); s.Content != "" {
				sections = append(sections, s)
			}
		}
	}

	// 9. Git context
	if s := b.renderSection(ctx, "context/git"); s.Content != "" {
		sections = append(sections, s)
	}

	// 7. Project context
	if s := b.renderSection(ctx, "context/project"); s.Content != "" {
		sections = append(sections, s)
	}

	// 8. Skills context (if applicable)
	if b.data.HasSkills {
		if s := b.renderSection(ctx, "context/skills"); s.Content != "" {
			sections = append(sections, s)
		}
	}

	// 8b. Learned optimization rules from the Skill Library (injected by the evaluator).
	if b.evaluator != nil {
		// Prefer the evaluator's pattern-based classifier (Phase 1); fall back to
		// the built-in keyword classifier (Phase 0) when the evaluator is unavailable.
		taskType := b.evaluator.ClassifyTask(b.data.UserRequest)
		if taskType == "" {
			taskType = classifyTaskType(b.data.UserRequest)
		}
		if learnedSkills, err := b.evaluator.GetActiveSkills(ctx, taskType); err == nil && len(learnedSkills) > 0 {
			var skillsText strings.Builder
			skillsText.WriteString("## Learned Optimization Rules\n")
			for _, sk := range learnedSkills {
				skillsText.WriteString("- ")
				skillsText.WriteString(sk.Content)
				skillsText.WriteString("\n")
			}
			sections = append(sections, PromptSection{
				Name:    "evaluator/learned_skills",
				Content: skillsText.String(),
			})
			logging.Debug("Self-improvement learned skills injected", "count", len(learnedSkills), "task_type", taskType, "agent", b.agentName)
		} else if err != nil {
			logging.Debug("Self-improvement learned skills unavailable", "agent", b.agentName, "error", err)
		}
	}

	// 9. MCP instructions (if any)
	if b.data.MCPInstructions != "" {
		if s := b.renderSection(ctx, "context/mcp_instructions"); s.Content != "" {
			sections = append(sections, s)
		}
	}

	// 10. Apply Lua hook_prompt_compose (reorder/add/remove sections)
	sections = b.applyComposeHook(ctx, sections)

	// 11. Join all non-empty sections
	var parts []string
	for _, s := range sections {
		trimmed := strings.TrimSpace(s.Content)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	finalPrompt := strings.Join(parts, "\n\n")

	// 12. Apply Lua hook_system_prompt
	finalPrompt = b.applyLuaSystemPromptHook(ctx, finalPrompt)

	return finalPrompt, nil
}

// renderSection renders a single template section and applies the Lua
// hook_template_section if available.
func (b *PromptBuilder) renderSection(ctx context.Context, name string) PromptSection {
	// Try UCB template selection via the evaluator before falling back to the registry.
	if b.evaluator != nil {
		if tmpl, err := b.evaluator.SelectTemplate(ctx, name); err == nil && tmpl != nil {
			// Record which template was chosen so the evaluator can score it later.
			if sessionID, ok := ctx.Value(SessionIDKey).(string); ok && sessionID != "" {
				b.evaluator.RecordTemplateSelection(ctx, sessionID, tmpl.ID)
				logging.Debug("Self-improvement template selected", "section", name, "template_id", tmpl.ID, "session_id", sessionID, "version", tmpl.Version)
			} else {
				logging.Debug("Self-improvement template selected without session context", "section", name, "template_id", tmpl.ID, "version", tmpl.Version)
			}
			return PromptSection{Name: name, Content: tmpl.Content}
		} else if err != nil {
			logging.Debug("Self-improvement template selection failed", "section", name, "error", err)
		}
	}

	content, err := b.registry.Render(name, b.data)
	if err != nil {
		logging.Debug("Template section render skipped", "name", name, "error", err)
		return PromptSection{Name: name}
	}

	// Apply Lua hook_template_section if available
	if b.luaMgr != nil && b.luaMgr.IsEnabled() {
		hookData := map[string]interface{}{
			"section_name":    name,
			"section_content": content,
			"agent_name":      b.agentName,
			"provider":        b.provider,
		}
		result, err := b.luaMgr.ExecuteHook(ctx, luaengine.HookTemplateSection, hookData)
		if err == nil && result != nil && result.Modified {
			if modified, ok := result.Data["section_content"].(string); ok {
				content = modified
			}
		}
	}

	return PromptSection{
		Name:    name,
		Content: content,
	}
}

// checkCapability checks if a capability should be included, potentially
// delegating to a Lua hook for custom override.
func (b *PromptBuilder) checkCapability(ctx context.Context, name string, available bool) bool {
	if b.luaMgr != nil && b.luaMgr.IsEnabled() {
		hookData := map[string]interface{}{
			"capability": name,
			"available":  available,
			"agent_name": b.agentName,
		}
		result, err := b.luaMgr.ExecuteHook(ctx, luaengine.HookType("capability_check"), hookData)
		if err == nil && result != nil && result.Modified {
			if override, ok := result.Data["available"].(bool); ok {
				return override
			}
		}
	}
	return available
}

// selectProvider determines which provider template to use, allowing Lua
// hook_provider_select to override the default selection.
//
// Resolution order:
//  1. Lua hook_provider_select override (highest priority)
//  2. Family-specific template: providers/{provider}/{family}  (e.g. providers/openai/o-series)
//  3. Provider-level fallback: providers/{provider}            (e.g. providers/openai)
func (b *PromptBuilder) selectProvider(ctx context.Context) string {
	providerName := strings.ToLower(b.provider)
	if providerName == "" {
		return ""
	}

	// 1. Allow Lua hook to override entirely
	if b.luaMgr != nil && b.luaMgr.IsEnabled() {
		hookData := map[string]interface{}{
			"provider":     b.provider,
			"model":        b.data.Model,
			"model_family": string(b.data.ModelFamily),
			"agent_name":   b.agentName,
		}
		result, err := b.luaMgr.ExecuteHook(ctx, luaengine.HookProviderSelect, hookData)
		if err == nil && result != nil && result.Modified {
			if override, ok := result.Data["provider_template"].(string); ok && override != "" {
				return override
			}
		}
	}

	// 2. Try family-specific template first
	if b.data.ModelFamily != FamilyDefault {
		familyTemplate := "providers/" + providerName + "/" + string(b.data.ModelFamily)
		if b.registry.Exists(familyTemplate) {
			logging.Debug("Using model-family template", "provider", providerName, "family", string(b.data.ModelFamily), "template", familyTemplate)
			return familyTemplate
		}
	}

	// 3. Fall back to provider-level template
	return "providers/" + providerName
}

// applyComposeHook applies Lua hook_prompt_compose to allow reordering,
// adding, or removing sections before final assembly.
func (b *PromptBuilder) applyComposeHook(ctx context.Context, sections []PromptSection) []PromptSection {
	if b.luaMgr == nil || !b.luaMgr.IsEnabled() {
		return sections
	}

	// Build sections data for Lua
	sectionsList := make([]interface{}, len(sections))
	for i, s := range sections {
		sectionsList[i] = map[string]interface{}{
			"name":    s.Name,
			"content": s.Content,
		}
	}

	hookData := map[string]interface{}{
		"sections":   sectionsList,
		"agent_name": b.agentName,
		"provider":   b.provider,
	}
	result, err := b.luaMgr.ExecuteHook(ctx, luaengine.HookPromptCompose, hookData)
	if err != nil || result == nil || !result.Modified {
		return sections
	}

	// Reconstruct sections from Lua result
	rawSections, ok := result.Data["sections"]
	if !ok {
		return sections
	}

	switch v := rawSections.(type) {
	case []interface{}:
		newSections := make([]PromptSection, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				name, _ := m["name"].(string)
				content, _ := m["content"].(string)
				newSections = append(newSections, PromptSection{Name: name, Content: content})
			}
		}
		return newSections
	case map[string]interface{}:
		// Lua tables with integer keys are returned as maps
		newSections := make([]PromptSection, 0, len(v))
		for i := 1; i <= len(v); i++ {
			key := fmt.Sprintf("%d", i)
			if item, ok := v[key]; ok {
				if m, ok := item.(map[string]interface{}); ok {
					name, _ := m["name"].(string)
					content, _ := m["content"].(string)
					newSections = append(newSections, PromptSection{Name: name, Content: content})
				}
			}
		}
		if len(newSections) > 0 {
			return newSections
		}
	}

	return sections
}

// applyLuaSystemPromptHook applies the existing Lua system_prompt hook.
func (b *PromptBuilder) applyLuaSystemPromptHook(ctx context.Context, prompt string) string {
	if b.luaMgr == nil || !b.luaMgr.IsEnabled() {
		return prompt
	}

	hookData := map[string]interface{}{
		"system_prompt": prompt,
		"agent_name":    b.agentName,
		"provider":      b.provider,
	}
	result, err := b.luaMgr.ExecuteHook(ctx, luaengine.HookSystemPrompt, hookData)
	if err == nil && result != nil && result.Modified {
		if modified, ok := result.Data["system_prompt"].(string); ok && modified != "" {
			return modified
		}
	}

	return prompt
}

// homeDir returns the user's home directory.
func homeDir() (string, error) {
	return os.UserHomeDir()
}

// classifyTaskType returns a task type label from a user message using simple keyword matching.
// This is the Phase 0 fallback used when no evaluator is available.
// Returns "general" if no pattern matches.
func classifyTaskType(text string) string {
	lower := strings.ToLower(text)
	switch {
	case containsAny(lower, "fix", "bug", "error", "crash", "broken", "failing", "exception", "panic", "traceback"):
		return "debug"
	case containsAny(lower, "refactor", "rename", "reorganize", "clean up", "extract", "move"):
		return "refactor"
	case containsAny(lower, "explain", "how does", "what is", "describe", "understand", "document"):
		return "explain"
	case containsAny(lower, "test", "spec", "coverage", "assert", "mock"):
		return "test"
	case containsAny(lower, "implement", "create", "add feature", "write", "build"):
		return "code"
	case containsAny(lower, "find", "search", "where is", "locate", "grep for"):
		return "search"
	default:
		return "general"
	}
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
