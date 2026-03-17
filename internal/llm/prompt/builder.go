package prompt

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/luaengine"
)

// PromptBuilder composes a final system prompt from template sections.
type PromptBuilder struct {
	agentName string
	provider  string
	data      *PromptData
	luaMgr    *luaengine.FilterManager
	registry  *TemplateRegistry
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

	// 2. Provider-specific section (with optional Lua override)
	providerTemplate := b.selectProvider(ctx)
	if providerTemplate != "" && b.registry.Exists(providerTemplate) {
		if s := b.renderSection(ctx, providerTemplate); s.Content != "" {
			sections = append(sections, s)
		}
	}

	// 3. Agent-specific section
	agentName := strings.ToLower(b.agentName)
	if agentName != "" && b.registry.Exists("agents/"+agentName) {
		if s := b.renderSection(ctx, "agents/"+agentName); s.Content != "" {
			sections = append(sections, s)
		}
	}

	// 4. Environment
	if s := b.renderSection(ctx, "base/environment"); s.Content != "" {
		sections = append(sections, s)
	}

	// 5. Capabilities
	capabilityMap := map[string]bool{
		"remembrances":  b.data.HasRemembrances,
		"orchestration": b.data.HasOrchestration,
		"web_search":    b.data.HasWebSearch,
		"code_indexing":  b.data.HasCodeIndexing,
		"lsp":           b.data.HasLSP,
	}
	for name, available := range capabilityMap {
		if b.checkCapability(ctx, name, available) && b.registry.Exists("capabilities/"+name) {
			if s := b.renderSection(ctx, "capabilities/"+name); s.Content != "" {
				sections = append(sections, s)
			}
		}
	}

	// 6. Git context
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
func (b *PromptBuilder) selectProvider(ctx context.Context) string {
	providerName := strings.ToLower(b.provider)
	if providerName == "" {
		return ""
	}
	defaultTemplate := "providers/" + providerName

	if b.luaMgr != nil && b.luaMgr.IsEnabled() {
		hookData := map[string]interface{}{
			"provider":   b.provider,
			"model":      b.data.Model,
			"agent_name": b.agentName,
		}
		result, err := b.luaMgr.ExecuteHook(ctx, luaengine.HookProviderSelect, hookData)
		if err == nil && result != nil && result.Modified {
			if override, ok := result.Data["provider_template"].(string); ok && override != "" {
				return override
			}
		}
	}

	return defaultTemplate
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
