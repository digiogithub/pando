package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mesnada/persona"
)

// globalPersonaSelector is the package-level persona selector for the main session.
var globalPersonaSelector *PersonaSelector

// globalPersonaManager is the persona manager that holds all available personas
// (built-ins + user-defined). It is always initialised when personas are available,
// independently of whether auto-selection is configured.
var globalPersonaManager *persona.Manager

// activePersonaName stores the currently active persona name (empty = none).
var activePersonaName string

// SetPersonaSelector sets the global persona selector used in the main conversation agent.
func SetPersonaSelector(ps *PersonaSelector) {
	globalPersonaSelector = ps
}

// SetPersonaManager sets the global persona manager used for persona listing and
// manual persona selection. This should be called during app initialisation.
func SetPersonaManager(mgr *persona.Manager) {
	globalPersonaManager = mgr
}

// GetPersonaManager returns the global persona manager, or nil if not initialised.
func GetPersonaManager() *persona.Manager {
	return globalPersonaManager
}

// ListAvailablePersonas returns the names of all loaded personas.
// Uses the global persona manager when available; falls back to the selector's manager.
func ListAvailablePersonas() []string {
	if globalPersonaManager != nil {
		return globalPersonaManager.ListPersonas()
	}
	if globalPersonaSelector != nil {
		return globalPersonaSelector.manager.ListPersonas()
	}
	return []string{}
}

// GetActivePersona returns the currently active persona name.
// An empty string means no persona is active (auto-select or none).
func GetActivePersona() string {
	return activePersonaName
}

// SetActivePersona sets the active persona by name.
// Pass an empty string to clear the active persona (revert to auto-select or none).
// Returns an error if the named persona does not exist (and name is non-empty).
func SetActivePersona(name string) error {
	if name == "" {
		activePersonaName = ""
		return nil
	}
	// Validate persona exists in manager or selector
	mgr := globalPersonaManager
	if mgr == nil && globalPersonaSelector != nil {
		mgr = globalPersonaSelector.manager
	}
	if mgr == nil || !mgr.HasPersona(name) {
		return fmt.Errorf("persona %q not found", name)
	}
	activePersonaName = name
	return nil
}

// getPersonaContent returns the persona instructions for the given context.
// Priority: manually set active persona > auto-selector > empty string.
// The returned content is intended to be injected into the system prompt,
// not prepended to the user message.
func getPersonaContent(ctx context.Context, userPrompt string) string {
	// Manual persona takes priority over auto-selection.
	if activePersonaName != "" {
		mgr := globalPersonaManager
		if mgr == nil && globalPersonaSelector != nil {
			mgr = globalPersonaSelector.manager
		}
		if mgr != nil {
			logging.Debug("Persona: using manually set persona", "persona", activePersonaName)
			return mgr.GetPersona(activePersonaName)
		}
	}

	// Fall back to automatic persona selection.
	if globalPersonaSelector != nil {
		return globalPersonaSelector.SelectPersonaContent(ctx, userPrompt)
	}

	return ""
}

// PersonaSelector automatically selects and applies a persona for each user prompt.
// It uses a lite LLM provider (configured via agents["persona-selector"]) to pick the
// best matching persona from the personas directory, then prepends its content to the
// user message before it reaches the main conversation model.
type PersonaSelector struct {
	manager          *persona.Manager
	selectorProvider provider.Provider
}

const personaSelectorMaxPromptLen = 600

const personaSelectorInstruction = `Select the most appropriate persona for the user task below.
Available personas:
%s
User task:
%s

Reply with ONLY the exact persona name from the list above, or "none" if no persona is clearly relevant.
Do not add any explanation, punctuation, or extra text.`

// NewPersonaSelector creates a PersonaSelector that loads personas from personaPath and
// uses the model configured under agents["persona-selector"] to perform selection.
// Returns an error if the persona-selector agent is not configured or the model is unavailable.
func NewPersonaSelector(personaPath string) (*PersonaSelector, error) {
	mgr, err := persona.NewManager(personaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load personas: %w", err)
	}

	selectorProvider, err := createAgentProvider(config.AgentPersonaSelector, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("persona-selector agent not available: %w", err)
	}

	return &PersonaSelector{
		manager:          mgr,
		selectorProvider: selectorProvider,
	}, nil
}

// SelectPersonaContent selects the best persona for userPrompt and returns its raw
// content (the persona instructions). Returns an empty string if no persona matches,
// the selector is disabled, or an error occurs. The content is intended to be injected
// into the system prompt rather than prepended to the user message.
func (ps *PersonaSelector) SelectPersonaContent(ctx context.Context, userPrompt string) string {
	personas := ps.manager.ListPersonas()
	if len(personas) == 0 {
		return ""
	}

	// Build a compact persona listing: "- name: first heading/line"
	var personaList strings.Builder
	for _, name := range personas {
		content := ps.manager.GetPersona(name)
		if desc := extractPersonaTitle(content); desc != "" {
			personaList.WriteString(fmt.Sprintf("- %s: %s\n", name, desc))
		} else {
			personaList.WriteString(fmt.Sprintf("- %s\n", name))
		}
	}

	truncatedPrompt := userPrompt
	if len(truncatedPrompt) > personaSelectorMaxPromptLen {
		truncatedPrompt = truncatedPrompt[:personaSelectorMaxPromptLen] + "..."
	}

	selectionRequest := fmt.Sprintf(personaSelectorInstruction, personaList.String(), truncatedPrompt)

	response, err := ps.selectorProvider.SendMessages(
		ctx,
		[]message.Message{
			{
				Role:  message.User,
				Parts: []message.ContentPart{message.TextContent{Text: selectionRequest}},
			},
		},
		make([]tools.BaseTool, 0),
	)
	if err != nil {
		logging.Debug("PersonaSelector: selection call failed", "error", err)
		return ""
	}

	selected := strings.TrimSpace(strings.ToLower(response.Content))
	// Strip any surrounding quotes or punctuation the model may add
	selected = strings.Trim(selected, `"'`+"`.,;!?")
	if selected == "" || selected == "none" {
		return ""
	}

	// Match case-insensitively against the available names
	for _, name := range personas {
		if strings.ToLower(name) == selected {
			logging.Debug("PersonaSelector: applying persona", "persona", name)
			return ps.manager.GetPersona(name)
		}
	}

	logging.Debug("PersonaSelector: model returned unknown persona", "returned", selected)
	return ""
}

// SelectAndApply selects the best persona for userPrompt and returns the prompt with the
// persona content prepended. Kept for backward compatibility; prefer SelectPersonaContent
// when the content will be injected into the system prompt.
func (ps *PersonaSelector) SelectAndApply(ctx context.Context, userPrompt string) string {
	content := ps.SelectPersonaContent(ctx, userPrompt)
	if content == "" {
		return userPrompt
	}
	return content + "\n\n" + userPrompt
}

// extractPersonaTitle returns the first meaningful line of a persona's markdown content,
// stripping leading heading markers (#) so it can serve as a short description.
func extractPersonaTitle(content string) string {
	content = strings.TrimSpace(content)
	if idx := strings.IndexByte(content, '\n'); idx >= 0 {
		content = content[:idx]
	}
	content = strings.TrimLeft(content, "# ")
	return strings.TrimSpace(content)
}
