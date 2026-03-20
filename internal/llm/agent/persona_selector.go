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

// SetPersonaSelector sets the global persona selector used in the main conversation agent.
func SetPersonaSelector(ps *PersonaSelector) {
	globalPersonaSelector = ps
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

// SelectAndApply selects the best persona for userPrompt and returns the prompt with the
// persona content prepended. Returns the original prompt unchanged if no persona matches,
// the selector is disabled, or an error occurs.
func (ps *PersonaSelector) SelectAndApply(ctx context.Context, userPrompt string) string {
	personas := ps.manager.ListPersonas()
	if len(personas) == 0 {
		return userPrompt
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
		return userPrompt
	}

	selected := strings.TrimSpace(strings.ToLower(response.Content))
	// Strip any surrounding quotes or punctuation the model may add
	selected = strings.Trim(selected, `"'`+"`.,;!?")
	if selected == "" || selected == "none" {
		return userPrompt
	}

	// Match case-insensitively against the available names
	for _, name := range personas {
		if strings.ToLower(name) == selected {
			logging.Debug("PersonaSelector: applying persona", "persona", name)
			return ps.manager.ApplyPersona(name, userPrompt)
		}
	}

	logging.Debug("PersonaSelector: model returned unknown persona", "returned", selected)
	return userPrompt
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
