package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/tools"
)

// setupModelProposalTTL bounds how long a quoted switch stays confirmable. A
// stale "--confirm" arriving many turns later would otherwise apply a price the
// user approved in a completely different context.
const setupModelProposalTTL = 10 * time.Minute

// setupModelProposal is a cost quote returned to the agent and awaiting the
// user's approval. Storing the model id is what makes the confirmation specific:
// approving a switch to a cheap model must not authorize a switch to any other.
type setupModelProposal struct {
	Model    models.ModelID
	QuotedAt time.Time
}

// setupModelProposals maps sessionID -> setupModelProposal.
var setupModelProposals sync.Map

// CurrentModel reports the model the session runs on, flagging whether it comes
// from a session override or from the agent's configured default.
func (b *setupBridge) CurrentModel(sessionID string) (tools.SetupModelState, error) {
	id, overridden := effectiveSessionModel(sessionID)
	if id == "" {
		return tools.SetupModelState{}, fmt.Errorf("no model is configured for the coder agent")
	}

	model, known := models.SupportedModels[id]
	if !known {
		// The configured model is not in the catalogue (removed provider account,
		// stale config). Report what we know instead of failing: the agent still
		// needs to see which id is in use to fix it.
		return tools.SetupModelState{ID: string(id), Overridden: overridden}, nil
	}
	return setupModelState(model, overridden), nil
}

// SetSessionModel switches the model of a single session, gated by configuration
// and by the cost-confirmation handshake.
func (b *setupBridge) SetSessionModel(sessionID, modelID string, confirmed bool) (tools.SetupModelSwitch, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return tools.SetupModelSwitch{}, fmt.Errorf("no session is attached to this call")
	}
	if !config.SetupModelSwitchEnabled() {
		return tools.SetupModelSwitch{}, fmt.Errorf(
			"changing the model at runtime is disabled. The user can enable it by setting " +
				"SetupModelSwitchEnabled = true under [InternalTools] in .pando.toml. " +
				"Reading the active model works regardless")
	}

	current, err := b.CurrentModel(sessionID)
	if err != nil {
		return tools.SetupModelSwitch{}, err
	}

	// Clearing the override restores a model the user already picked, so it needs
	// no cost confirmation — but it still rebuilds the provider, so it goes
	// through the same compatibility guards and the same per-run budget.
	clearing := strings.TrimSpace(modelID) == ""

	var target models.Model
	if clearing {
		configured, _ := configuredAgentModel()
		if configured == "" {
			return tools.SetupModelSwitch{}, fmt.Errorf("no model is configured for the coder agent")
		}
		known, ok := models.SupportedModels[configured]
		if !ok {
			return tools.SetupModelSwitch{}, fmt.Errorf(
				"the configured model %s is not in the catalogue, so the override cannot be cleared", configured)
		}
		target = known
	} else if target, err = resolveSetupModel(modelID); err != nil {
		return tools.SetupModelSwitch{}, err
	}

	targetState := setupModelState(target, !clearing)
	if targetState.ID == current.ID {
		setupModelProposals.Delete(sessionID)
		return tools.SetupModelSwitch{Applied: true, Cleared: clearing, Previous: current, Target: targetState}, nil
	}

	state := modelSwitchRunStateFor(sessionID)
	if err := checkSetupModelCompatibility(state, target); err != nil {
		return tools.SetupModelSwitch{}, err
	}

	// Refuse an exhausted budget before quoting anything: asking the user to
	// approve a switch that will be rejected afterwards wastes their turn.
	limit := modelSwitchCapForRun()
	if state != nil && !state.switchAvailable(limit) {
		return tools.SetupModelSwitch{}, fmt.Errorf(
			"the model has already been changed %d times in this run, which is the limit. "+
				"Finish the turn on %s; the user can switch again in the next one", limit, current.ID)
	}

	if !clearing {
		if reason, needed := setupModelConfirmationReason(current, targetState); needed {
			if !confirmed || !consumeSetupModelProposal(sessionID, target.ID) {
				setupModelProposals.Store(sessionID, setupModelProposal{Model: target.ID, QuotedAt: time.Now()})
				return tools.SetupModelSwitch{Previous: current, Target: targetState, ConfirmationReason: reason}, nil
			}
		}
	}
	setupModelProposals.Delete(sessionID)

	// Consume the budget only now: a switch that was merely quoted, or refused by
	// a guard, must not count against it.
	if state != nil && !state.reserveSwitch(limit) {
		return tools.SetupModelSwitch{}, fmt.Errorf(
			"the model has already been changed %d times in this run, which is the limit. "+
				"Finish the turn on %s; the user can switch again in the next one", limit, current.ID)
	}

	if clearing {
		SetSessionModelOverride(sessionID, "")
	} else {
		SetSessionModelOverride(sessionID, target.ID)
	}
	return tools.SetupModelSwitch{Applied: true, Cleared: clearing, Previous: current, Target: targetState}, nil
}

// checkSetupModelCompatibility refuses a switch the live conversation cannot
// survive. Catching it here turns what would be a mid-request provider rejection
// into a tool error the agent can act on, e.g. by picking another model.
func checkSetupModelCompatibility(state *modelSwitchRunState, target models.Model) error {
	if state == nil {
		return nil
	}
	if state.historyHasImages() && !target.SupportsAttachments {
		return fmt.Errorf(
			"%s cannot read images and this conversation already contains some, so the next "+
				"request would be rejected. Pick a model with attachment support", target.ID)
	}
	return nil
}

// effectiveSessionModel returns the model id in force for a session and whether
// it comes from a session override.
func effectiveSessionModel(sessionID string) (models.ModelID, bool) {
	if override := SessionLLMOverridesFor(sessionID).Model; override != "" {
		return override, true
	}
	configured, _ := configuredAgentModel()
	return configured, false
}

// SessionModelID returns the model a session actually runs on: its runtime
// override when one is set, otherwise the coder agent's configured model.
// Surfaces (TUI status bar, ACP model picker) must use this instead of reading
// the configured agent model, which does not know about per-session switches.
func SessionModelID(sessionID string) models.ModelID {
	id, _ := effectiveSessionModel(sessionID)
	return id
}

// SessionModelOverrideID returns only the runtime override of a session, empty
// when the session still runs on the configured model. Callers that keep their
// own notion of the selected model (the ACP session state) use this to tell
// "the agent switched model" apart from "the global default differs from what
// this session picked", which must not be overwritten.
func SessionModelOverrideID(sessionID string) models.ModelID {
	return SessionLLMOverridesFor(sessionID).Model
}

// configuredAgentModel returns the coder agent's configured model, which is what
// a session falls back to once its override is cleared.
func configuredAgentModel() (models.ModelID, bool) {
	cfg := config.Get()
	if cfg == nil {
		return "", false
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return "", false
	}
	return agentCfg.Model, true
}

// resolveSetupModel validates a model id the way createAgentProvider will: it
// must be in the catalogue and its provider account must resolve and be enabled,
// otherwise the switch would only fail later, mid-request.
func resolveSetupModel(modelID string) (models.Model, error) {
	id := models.ModelID(strings.TrimSpace(modelID))
	model, known := models.SupportedModels[id]
	if !known {
		return models.Model{}, fmt.Errorf(
			"unknown model %q. Run pando_setup with command=\"models\" to list the selectable ids", modelID)
	}

	// Mirrors createAgentProvider: Antigravity is the one provider that works
	// without a concrete account.
	if model.AccountID == "" && model.Provider == models.ProviderAntigravity {
		return model, nil
	}

	var (
		acc *config.ProviderAccount
		err error
	)
	if model.AccountID != "" {
		acc, err = config.ResolveProviderAccountByID(model.AccountID)
	} else {
		acc, err = config.ResolveProviderAccountForType(model.Provider)
	}
	if err != nil {
		return models.Model{}, fmt.Errorf("model %s is not usable: %w", id, err)
	}
	if acc.Disabled {
		return models.Model{}, fmt.Errorf("model %s is not usable: provider account %q is disabled", id, acc.ID)
	}
	return model, nil
}

func setupModelState(model models.Model, overridden bool) tools.SetupModelState {
	return tools.SetupModelState{
		ID:            string(model.ID),
		Name:          model.Name,
		Provider:      string(model.Provider),
		Account:       model.AccountID,
		ContextWindow: model.ContextWindow,
		CostIn:        model.CostPer1MIn,
		CostOut:       model.CostPer1MOut,
		CostKnown:     model.CostPer1MIn > 0 || model.CostPer1MOut > 0,
		Overridden:    overridden,
	}
}

// setupModelConfirmationReason decides whether a switch needs the user's
// approval, and explains why. A target is "more expensive" when either its input
// or its output price exceeds the current one: comparing a blended figure would
// let a large output-price jump hide behind a cheaper input price.
func setupModelConfirmationReason(current, target tools.SetupModelState) (string, bool) {
	if !current.CostKnown || !target.CostKnown {
		unknown := current.ID + " and " + target.ID
		switch {
		case current.CostKnown:
			unknown = target.ID
		case target.CostKnown:
			unknown = current.ID
		}
		return fmt.Sprintf(
			"No published price for %s, so the cost of this switch cannot be compared. "+
				"The user has to decide.", unknown), true
	}

	if target.CostIn > current.CostIn || target.CostOut > current.CostOut {
		return fmt.Sprintf(
			"This switch increases the cost of the session: input $%.2f -> $%.2f and "+
				"output $%.2f -> $%.2f per 1M tokens.",
			current.CostIn, target.CostIn, current.CostOut, target.CostOut), true
	}
	return "", false
}

// pruneExpiredSetupModelProposals drops quotes that can no longer be confirmed.
func pruneExpiredSetupModelProposals() {
	setupModelProposals.Range(func(key, value any) bool {
		proposal, ok := value.(setupModelProposal)
		if !ok || time.Since(proposal.QuotedAt) > setupModelProposalTTL {
			setupModelProposals.Delete(key)
		}
		return true
	})
}

// consumeSetupModelProposal reports whether the session has a live, matching
// quote, and removes it. A confirmation for a model that was never quoted (or
// quoted too long ago) is refused, so the user's approval cannot be recycled for
// a different or a stale switch.
func consumeSetupModelProposal(sessionID string, model models.ModelID) bool {
	value, ok := setupModelProposals.Load(sessionID)
	if !ok {
		return false
	}
	setupModelProposals.Delete(sessionID)

	proposal, ok := value.(setupModelProposal)
	if !ok || proposal.Model != model {
		return false
	}
	return time.Since(proposal.QuotedAt) <= setupModelProposalTTL
}
