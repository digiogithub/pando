package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mesnada/persona"
	"github.com/digiogithub/pando/internal/message"
)

// personaMu guards the package-level persona state below. The setters are driven
// by the UI (TUI/Web/ACP) while the readers run inside live agent goroutines, so
// the state is genuinely accessed concurrently and must be synchronized.
var personaMu sync.RWMutex

// globalPersonaSelector is the package-level persona selector for the main session.
// Guarded by personaMu.
var globalPersonaSelector *PersonaSelector

// globalPersonaManager is the persona manager that holds all available personas
// (built-ins + user-defined). It is always initialised when personas are available,
// independently of whether auto-selection is configured. Guarded by personaMu.
var globalPersonaManager *persona.Manager

// activePersonaName stores the currently active persona name (empty = none).
// Guarded by personaMu.
var activePersonaName string

// SetPersonaSelector sets the global persona selector used in the main conversation agent.
func SetPersonaSelector(ps *PersonaSelector) {
	personaMu.Lock()
	defer personaMu.Unlock()
	globalPersonaSelector = ps
}

// SetPersonaManager sets the global persona manager used for persona listing and
// manual persona selection. This should be called during app initialisation.
func SetPersonaManager(mgr *persona.Manager) {
	personaMu.Lock()
	defer personaMu.Unlock()
	globalPersonaManager = mgr
}

// GetPersonaManager returns the global persona manager, or nil if not initialised.
func GetPersonaManager() *persona.Manager {
	personaMu.RLock()
	defer personaMu.RUnlock()
	return globalPersonaManager
}

// ListAvailablePersonas returns the names of all loaded personas.
// Uses the global persona manager when available; falls back to the selector's manager.
func ListAvailablePersonas() []string {
	if mgr := personaManager(); mgr != nil {
		return mgr.ListPersonas()
	}
	return []string{}
}

// GetActivePersona returns the currently active persona name.
// An empty string means no persona is active (auto-select or none).
func GetActivePersona() string {
	personaMu.RLock()
	defer personaMu.RUnlock()
	return activePersonaName
}

// SetActivePersona sets the active persona by name.
// Pass an empty string to clear the active persona (revert to auto-select or none).
// Returns an error if the named persona does not exist (and name is non-empty).
func SetActivePersona(name string) error {
	if name == "" {
		personaMu.Lock()
		activePersonaName = ""
		personaMu.Unlock()
		return nil
	}
	// Validate persona exists in manager or selector
	mgr := personaManager()
	if mgr == nil || !mgr.HasPersona(name) {
		return fmt.Errorf("persona %q not found", name)
	}
	personaMu.Lock()
	activePersonaName = name
	personaMu.Unlock()
	return nil
}

// setActivePersonaForTest sets the active persona without validating it, so tests
// can install a global persona through the same lock the agent goroutines use.
func setActivePersonaForTest(name string) {
	personaMu.Lock()
	defer personaMu.Unlock()
	activePersonaName = name
}

// personaManager returns the manager to resolve persona content from, preferring
// the global manager and falling back to the selector's manager.
func personaManager() *persona.Manager {
	personaMu.RLock()
	defer personaMu.RUnlock()
	if globalPersonaManager != nil {
		return globalPersonaManager
	}
	if globalPersonaSelector != nil {
		return globalPersonaSelector.manager
	}
	return nil
}

// personaSelector returns the global auto-selector, or nil when none is configured.
func personaSelector() *PersonaSelector {
	personaMu.RLock()
	defer personaMu.RUnlock()
	return globalPersonaSelector
}

// getPersonaContent returns the persona instructions for the given context.
// Priority: per-session persona override > manually set active persona >
// auto-selector > empty string. The returned content is intended to be injected
// into the system prompt, not prepended to the user message.
func getPersonaContent(ctx context.Context, userPrompt string) string {
	// Per-session persona override takes top priority so that concurrent ACP /
	// delegated sessions can each use a different persona without clobbering the
	// package-global active persona. When a session is PersonaScoped, it manages
	// persona authoritatively: an explicit name wins, otherwise auto-selection is
	// used (the global active persona is intentionally ignored for that session).
	if ov := sessionLLMOverridesForContext(ctx); ov.PersonaScoped {
		if ov.Persona != "" {
			if mgr := personaManager(); mgr != nil && mgr.HasPersona(ov.Persona) {
				logging.Debug("Persona: using per-session persona", "persona", ov.Persona)
				return mgr.GetPersona(ov.Persona)
			}
		}
		if selector := personaSelector(); selector != nil {
			return selector.SelectPersonaContent(ctx, userPrompt)
		}
		return ""
	}

	// Manual persona takes priority over auto-selection.
	if active := GetActivePersona(); active != "" {
		if mgr := personaManager(); mgr != nil {
			logging.Debug("Persona: using manually set persona", "persona", active)
			return mgr.GetPersona(active)
		}
	}

	// Fall back to automatic persona selection.
	if selector := personaSelector(); selector != nil {
		return selector.SelectPersonaContent(ctx, userPrompt)
	}

	return ""
}

// effectiveActivePersona returns the persona name in effect for the given
// context, honoring a per-session override before the package-global value.
// Used for status reporting only.
func effectiveActivePersona(ctx context.Context) string {
	if ov := sessionLLMOverridesForContext(ctx); ov.PersonaScoped {
		return ov.Persona
	}
	return GetActivePersona()
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

	selectorProvider, err := createAgentProvider(context.Background(), config.AgentPersonaSelector, nil, nil, nil)
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
