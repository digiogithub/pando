package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

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

// SetAndPersistActivePersona activates the persona like SetActivePersona and
// then saves the choice to the config (project file when present, else global)
// so it survives a restart. Persistence failures are returned after the
// in-memory switch has already taken effect.
func SetAndPersistActivePersona(name string) error {
	if err := SetActivePersona(name); err != nil {
		return err
	}
	if err := config.UpdateActivePersona(name); err != nil {
		return fmt.Errorf("persona activated but not saved: %w", err)
	}
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
//
// It treats the call as one eligible user prompt without history; the agent
// loop uses resolvePersonaContent directly so it can pass the turn's history,
// the combined-request hook and announce the outcome.
func getPersonaContent(ctx context.Context, userPrompt string) string {
	return resolvePersonaContent(ctx, personaRequest{Prompt: userPrompt, Eligible: true}).Content
}

// resolvePersonaContent applies the persona priority for one run. Only the
// auto-selection step ever makes a decision or an LLM call; an explicit or
// manual persona triggers neither.
func resolvePersonaContent(ctx context.Context, req personaRequest) personaOutcome {
	// Per-session persona override takes top priority so that concurrent ACP /
	// delegated sessions can each use a different persona without clobbering the
	// package-global active persona. When a session is PersonaScoped, it manages
	// persona authoritatively: an explicit name wins, otherwise auto-selection is
	// used (the global active persona is intentionally ignored for that session).
	if ov := sessionLLMOverridesForContext(ctx); ov.PersonaScoped {
		// ov.Prompt (PANDO-US-0014: an AG-UI profile's extra system-prompt
		// text) is appended to whatever persona content this branch resolves,
		// below every return in it, so a profile's Prompt reaches the system
		// prompt regardless of whether the profile also named a persona.
		if ov.Persona != "" {
			if mgr := personaManager(); mgr != nil && mgr.HasPersona(ov.Persona) {
				logging.Debug("Persona: using per-session persona", "persona", ov.Persona)
				return personaOutcome{Content: appendSessionPrompt(mgr.GetPersona(ov.Persona), ov.Prompt)}
			}
		}
		out := autoSelectPersona(ctx, req)
		out.Content = appendSessionPrompt(out.Content, ov.Prompt)
		return out
	}

	// Manual persona takes priority over auto-selection.
	if active := GetActivePersona(); active != "" {
		if mgr := personaManager(); mgr != nil {
			logging.Debug("Persona: using manually set persona", "persona", active)
			return personaOutcome{Content: mgr.GetPersona(active)}
		}
	}

	// Fall back to automatic persona selection.
	return autoSelectPersona(ctx, req)
}

// appendSessionPrompt joins persona content with a session's extra Prompt
// override (SessionLLMOverrides.Prompt), separated by a blank line like the
// other sections buildSystemMessage assembles. Either half may be empty.
func appendSessionPrompt(personaContent, prompt string) string {
	switch {
	case personaContent == "":
		return prompt
	case prompt == "":
		return personaContent
	default:
		return personaContent + "\n\n" + prompt
	}
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

// PersonaSelector is the LLM based persona classifier. It uses a lite LLM
// provider (configured via agents["persona-selector"]) to pick the best
// matching persona from the personas directory. It is the selection path when
// the decision model option is off, and the fallback when the decision model
// cannot answer.
type PersonaSelector struct {
	// manager is the persona set to choose from. When nil the global persona
	// manager is used, so a persona path change takes effect without rebuilding
	// the selector.
	manager *persona.Manager

	// selectorProvider is a fixed provider (NewPersonaSelector, tests). A lazy
	// selector leaves it nil and builds the provider from the persona-selector
	// agent configuration on first use, rebuilding it when that changes.
	selectorProvider provider.Provider

	// lazy selectors follow personaAutoSelect.enabled live; an explicit
	// selector (SetPersonaSelector with NewPersonaSelector) is always active.
	lazy bool

	mu          sync.Mutex
	cached      provider.Provider
	cachedKey   string
	cachedError error     // last build error for cachedKey (retried after personaProviderRetry)
	failedAt    time.Time // when cachedError was recorded (autoClock)
}

// personaProviderRetry is the minimum time between provider build attempts for
// the same configuration after a failure, so a transient error is not cached
// forever but a broken setup is not rebuilt on every prompt.
const personaProviderRetry = 30 * time.Second

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

// NewLazyPersonaSelector creates a selector that can always be installed: it is
// only active while personaAutoSelect.enabled is on, resolves personas from the
// global persona manager and builds its LLM provider on first use from the
// current persona-selector agent configuration (rebuilt when that changes).
func NewLazyPersonaSelector() *PersonaSelector {
	return &PersonaSelector{lazy: true}
}

// personaMgr returns the persona set this selector chooses from.
func (ps *PersonaSelector) personaMgr() *persona.Manager {
	if ps.manager != nil {
		return ps.manager
	}
	return personaManager()
}

// llmProvider returns the provider used for the LLM selection.
func (ps *PersonaSelector) llmProvider(ctx context.Context) (provider.Provider, error) {
	if ps.selectorProvider != nil {
		return ps.selectorProvider, nil
	}
	if !ps.lazy {
		return nil, fmt.Errorf("persona-selector provider not available")
	}
	return ps.cachedProvider(personaProviderKey(), func() (provider.Provider, error) {
		return createAgentProvider(context.WithoutCancel(ctx), config.AgentPersonaSelector, nil, nil, nil)
	})
}

// cachedProvider returns the provider for key, building it with build when
// there is none. Only successes are cached for good; a failure is remembered
// for personaProviderRetry. build runs outside ps.mu.
func (ps *PersonaSelector) cachedProvider(key string, build func() (provider.Provider, error)) (provider.Provider, error) {
	ps.mu.Lock()
	if ps.cachedKey == key {
		if ps.cached != nil {
			p := ps.cached
			ps.mu.Unlock()
			return p, nil
		}
		if ps.cachedError != nil && autoClock().Sub(ps.failedAt) < personaProviderRetry {
			err := ps.cachedError
			ps.mu.Unlock()
			return nil, err
		}
	}
	ps.mu.Unlock()

	// Build outside the lock; a concurrent duplicate build is tolerated.
	p, err := build()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if err != nil {
		err = fmt.Errorf("persona-selector agent not available: %w", err)
		ps.cached, ps.cachedKey, ps.cachedError, ps.failedAt = nil, key, err, autoClock()
		return nil, err
	}
	ps.cached, ps.cachedKey, ps.cachedError = p, key, nil
	return p, nil
}

// personaProviderKey identifies the configuration the persona-selector
// provider is built from: the agent entry and the provider accounts.
func personaProviderKey() string {
	cfg := config.Get()
	if cfg == nil {
		return ""
	}
	h := sha256.New()
	agentCfg, _ := json.Marshal(cfg.Agents[config.AgentPersonaSelector])
	accounts, _ := json.Marshal(cfg.ProviderAccounts)
	h.Write(agentCfg)
	h.Write([]byte{0})
	h.Write(accounts)
	return hex.EncodeToString(h.Sum(nil))
}

// SelectPersonaName asks the persona-selector LLM which persona fits userPrompt.
// It returns "" with a nil error when the model answered "none" or an unknown
// name, and a non-nil error when the model could not be consulted (no usable
// model, provider creation failed, the call failed).
func (ps *PersonaSelector) SelectPersonaName(ctx context.Context, userPrompt string) (string, error) {
	mgr := ps.personaMgr()
	if mgr == nil {
		return "", nil
	}
	personas := mgr.ListPersonas()
	if len(personas) == 0 {
		return "", nil
	}

	// Build a compact persona listing: "- name: description"
	var personaList strings.Builder
	for _, name := range personas {
		if desc := mgr.Description(name); desc != "" {
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

	selectorProvider, err := ps.llmProvider(ctx)
	if err != nil {
		return "", err
	}
	response, err := selectorProvider.SendMessages(
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
		return "", err
	}

	selected := strings.TrimSpace(strings.ToLower(response.Content))
	// Strip any surrounding quotes or punctuation the model may add
	selected = strings.Trim(selected, `"'`+"`.,;!?")
	if selected == "" || selected == "none" {
		return "", nil
	}

	// Match case-insensitively against the available names
	for _, name := range personas {
		if strings.ToLower(name) == selected {
			return name, nil
		}
	}

	logging.Debug("PersonaSelector: model returned unknown persona", "returned", selected)
	return "", nil
}

// SelectPersonaContent selects the best persona for userPrompt and returns its raw
// content (the persona instructions). Returns an empty string if no persona matches,
// the selector is disabled, or an error occurs. The content is intended to be injected
// into the system prompt rather than prepended to the user message.
func (ps *PersonaSelector) SelectPersonaContent(ctx context.Context, userPrompt string) string {
	name, err := ps.SelectPersonaName(ctx, userPrompt)
	if err != nil {
		logging.Debug("PersonaSelector: selection call failed", "error", err)
		return ""
	}
	if name == "" {
		return ""
	}
	logging.Debug("PersonaSelector: applying persona", "persona", name)
	return ps.personaMgr().GetPersona(name)
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
