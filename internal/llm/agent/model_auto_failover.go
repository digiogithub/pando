package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/extevents"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/message"
)

// Provider failover inside an Auto turn (PANDO-SP-0003.R6). When the provider
// of the current candidate fails with a failover-eligible error the turn moves
// to the next candidate of the route and re-issues the same iteration. Tool
// results already in the history are kept and never executed again.

// autoCooldown is how long a candidate that failed with an auth or
// model-not-found error is skipped by later turns of any session.
const autoCooldown = 5 * time.Minute

// autoClock is the clock used by the cooldown map (injected by tests).
var autoClock = time.Now

var (
	cooldownMu sync.Mutex
	cooldowns  = map[models.ModelID]time.Time{}
)

// markCoolingDown skips model in Auto chains until the cooldown expires.
func markCoolingDown(model models.ModelID) {
	cooldownMu.Lock()
	defer cooldownMu.Unlock()
	cooldowns[model] = autoClock().Add(autoCooldown)
}

// coolingDown reports whether model is inside its cooldown window.
func coolingDown(model models.ModelID) bool {
	cooldownMu.Lock()
	defer cooldownMu.Unlock()
	until, ok := cooldowns[model]
	if !ok {
		return false
	}
	if !autoClock().Before(until) {
		delete(cooldowns, model)
		return false
	}
	return true
}

// dropCoolingDown removes models in cooldown, preserving order.
func dropCoolingDown(cands []models.ModelID) []models.ModelID {
	out := make([]models.ModelID, 0, len(cands))
	for _, c := range cands {
		if coolingDown(c) {
			logging.Debug("model_auto: skipping candidate in cooldown", "model", string(c))
			continue
		}
		out = append(out, c)
	}
	return out
}

// resetAutoCooldowns clears the cooldown map (tests).
func resetAutoCooldowns() {
	cooldownMu.Lock()
	defer cooldownMu.Unlock()
	cooldowns = map[models.ModelID]time.Time{}
}

// failoverAttempt is one failed candidate of the turn.
type failoverAttempt struct {
	Model models.ModelID
	Class provider.ErrorClass
	Err   error
}

// streamProviderError marks an error that came out of the provider stream, as
// opposed to a database or tool problem. Only these can trigger failover.
type streamProviderError struct{ err error }

func (e *streamProviderError) Error() string { return e.err.Error() }
func (e *streamProviderError) Unwrap() error { return e.err }

func isProviderStreamError(err error) bool {
	var pe *streamProviderError
	return errors.As(err, &pe)
}

// chainSummary lists the failed candidates with their error classes.
func (t *autoTurnState) chainSummary() string {
	parts := make([]string, 0, len(t.attempts))
	for _, at := range t.attempts {
		parts = append(parts, fmt.Sprintf("%s [%s]", at.Model, at.Class))
	}
	return strings.Join(parts, " → ")
}

// handleAutoStreamError decides what an Auto turn does with a failed iteration.
// handled=true means the caller must re-issue the iteration with the returned
// provider and history. handled=false with a nil error means "not an Auto
// concern: surface err as usual"; a non-nil returned error replaces err (the
// all-candidates-failed summary).
func (a *agent) handleAutoStreamError(
	ctx context.Context,
	sessionID string,
	turn *autoTurnState,
	err error,
	failed message.Message,
	current provider.Provider,
	userPrompt, personaContent string,
	history []message.Message,
	eventCh chan<- AgentEvent,
) (provider.Provider, []message.Message, bool, error) {
	if turn == nil || current == nil || !isProviderStreamError(err) || ctx.Err() != nil {
		return current, history, false, nil
	}
	class := provider.ClassifyError(err)

	if class == provider.ErrorClassContextLength {
		// Never a failover: a bigger prompt fails on the next model too. Take
		// the normal compaction path once and retry on the same model.
		turn.mu.Lock()
		already := turn.compacted
		turn.compacted = true
		turn.mu.Unlock()
		if already {
			return current, history, false, nil
		}
		a.discardFailedMessage(failed)
		if cerr := a.compactContext(ctx, sessionID); cerr != nil {
			logging.Debug("model_auto: compaction after context-length error failed", "error", cerr)
			return current, history, false, nil
		}
		reloaded, rerr := a.loadSessionMessagesFromSummary(ctx, sessionID)
		if rerr != nil {
			return current, history, false, nil
		}
		fitted, ferr := a.ensureHistoryFitsBeforeSend(ctx, sessionID, reloaded, current, eventCh)
		if ferr != nil {
			return current, history, false, nil
		}
		return current, fitted, true, nil
	}
	if !class.ShouldFailover() {
		return current, history, false, nil
	}

	failedModel := current.Model()
	turn.mu.Lock()
	turn.attempts = append(turn.attempts, failoverAttempt{Model: failedModel.ID, Class: class, Err: err})
	turn.mu.Unlock()
	if class.NeedsCooldown() {
		markCoolingDown(failedModel.ID)
	}
	// Counter record for telemetry/logs: provider, model and error class only.
	logging.Info("model_auto: failover",
		"session_id", sessionID,
		"provider", string(failedModel.Provider),
		"model", string(failedModel.ID),
		"error_class", class.String(),
	)
	a.discardFailedMessage(failed)

	from := failedModel.ID
	for {
		turn.mu.Lock()
		turn.idx++
		if turn.idx >= len(turn.cands) {
			turn.idx = len(turn.cands) - 1
			summary := ""
			if len(turn.attempts) > 1 {
				summary = turn.chainSummary()
			}
			turn.mu.Unlock()
			if summary != "" {
				return current, history, false, fmt.Errorf("%w (Auto failover chain: %s)", err, summary)
			}
			return current, history, false, nil
		}
		next := turn.cands[turn.idx]
		last := turn.idx == len(turn.cands)-1
		turn.mu.Unlock()

		if !last && coolingDown(next) {
			logging.Debug("model_auto: skipping failover candidate in cooldown", "model", string(next))
			continue
		}

		turn.mu.Lock()
		turn.info.Model = next
		turn.info.FallbackUsed = true
		turn.info.Kind = RoutingKindFailover
		turn.info.Reason = "failover"
		turn.info.Notice = fmt.Sprintf("Auto: %s failed (%s), retrying on %s", from, class, next)
		turn.quietSwitch = true
		info := turn.info
		turn.mu.Unlock()

		setAutoTurnModelOverride(sessionID, next)
		a.emitRoutingNotice(sessionID, eventCh, info)
		np, nh := a.applyPendingModelSwitch(ctx, sessionID, current, userPrompt, personaContent, history, eventCh)
		turn.mu.Lock()
		turn.quietSwitch = false
		turn.mu.Unlock()

		if np == nil || np.Model().ID != next {
			// The provider for this candidate could not be built (provider
			// disabled, missing key...): count it as failed and move on.
			turn.mu.Lock()
			turn.attempts = append(turn.attempts, failoverAttempt{
				Model: next, Class: provider.ErrorClassUnknown, Err: errors.New("provider could not be built"),
			})
			turn.mu.Unlock()
			from = next
			class = provider.ErrorClassUnknown
			continue
		}

		lastRoutings.Store(sessionID, info)
		extevents.ModelRouted(extevents.ModelRoutedInfo{
			SessionID:       sessionID,
			RouteID:         info.RouteID,
			Model:           string(next),
			Fallback:        true,
			Reason:          info.Reason,
			Probability:     info.Probability,
			Confidence:      info.Confidence,
			RouterProvider:  info.RouterProvider,
			RouterModel:     info.RouterModel,
			RouterLatencyMs: info.RouterLatencyMs,
			RouterCostUSD:   info.RouterCostUSD,
		})
		return np, nh, true, nil
	}
}

// discardFailedMessage removes the partial assistant message of a failed
// attempt so neither the next model nor the next turn sees it.
func (a *agent) discardFailedMessage(failed message.Message) {
	if failed.ID == "" || failed.Role != message.Assistant {
		return
	}
	if err := a.messages.Delete(context.Background(), failed.ID); err != nil {
		logging.Debug("model_auto: could not discard failed assistant message", "id", failed.ID, "error", err)
	}
}
