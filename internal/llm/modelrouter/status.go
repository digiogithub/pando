package modelrouter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/systemone"
)

// statusCallTimeout bounds the health and discovery calls made for a text
// report (pando_setup, the ACP /decision-model command).
const statusCallTimeout = 10 * time.Second

// MaskedAPIKey returns a display-safe form of the router API key: empty when
// none is stored, "$VAR" references verbatim (masked), the masked tail
// otherwise. The unmasked key is never returned.
func MaskedAPIKey(r config.DecisionRouterConfig) string {
	raw := strings.TrimSpace(r.APIKey)
	if raw == "" {
		return ""
	}
	if eff := r.EffectiveAPIKey(); eff != "" && !strings.HasPrefix(raw, "$") {
		return config.MaskAPIKey(eff)
	}
	return config.MaskAPIKey(raw)
}

// DecisionConsumers lists the features that currently use the decision model.
func DecisionConsumers(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	var out []string
	if cfg.ModelAutoMode.Enabled {
		out = append(out, "model auto mode")
	}
	if cfg.Agents[config.AgentPersonaSelector].UseDecisionModel {
		out = append(out, "persona auto-select")
	}
	if cfg.Remembrances.ContextEnrichmentDecisionFilterEnabled {
		out = append(out, "context enrichment filter")
	}
	if cfg.Remembrances.MemoryContextDecisionFilterEnabled {
		out = append(out, "memory context filter")
	}
	return out
}

// RenderHealth formats a health verdict as one line plus the problems.
func RenderHealth(rep systemone.HealthReport) string {
	var sb strings.Builder
	if rep.OK {
		fmt.Fprintf(&sb, "healthy (%s, model %q, %d ms", rep.Kind, rep.Model, rep.LatencyMs)
		if rep.Version != "" {
			fmt.Fprintf(&sb, ", version %s", rep.Version)
		}
		sb.WriteString(")")
		return sb.String()
	}
	fmt.Fprintf(&sb, "NOT healthy (%s, model %q): reachable=%t authorized=%t model present=%t decision model=%t",
		rep.Kind, rep.Model, rep.Reachable, rep.Authorized, rep.ModelFound, rep.IsDecision)
	for _, p := range rep.Problems {
		sb.WriteString("\n  - " + p)
	}
	return sb.String()
}

// RenderStatus returns a text report of the saved decision model: provider,
// effective URL, masked key, model, timeout, consumers and, when withHealth is
// set, a live health probe. The API key is always masked.
func RenderStatus(ctx context.Context, withHealth bool) (string, error) {
	cfg := config.Get()
	if cfg == nil {
		return "", fmt.Errorf("configuration is not loaded")
	}
	dec := cfg.DecisionModel
	r := dec.Router
	var sb strings.Builder
	sb.WriteString("## Decision model\n\n")
	fmt.Fprintf(&sb, "- provider:  %s\n", r.EffectiveProvider())
	fmt.Fprintf(&sb, "- base URL:  %s\n", r.EffectiveBaseURL())
	key := MaskedAPIKey(r)
	if key == "" {
		key = "(none)"
	}
	fmt.Fprintf(&sb, "- API key:   %s\n", key)
	model := strings.TrimSpace(r.Model)
	if model == "" {
		model = "(not set)"
	}
	fmt.Fprintf(&sb, "- model:     %s\n", model)
	fmt.Fprintf(&sb, "- timeout:   %d ms\n", dec.EffectiveTimeout().Milliseconds())
	if r.KeepAlive != "" {
		fmt.Fprintf(&sb, "- keep alive: %s\n", r.KeepAlive)
	}
	if consumers := DecisionConsumers(cfg); len(consumers) > 0 {
		fmt.Fprintf(&sb, "- consumers: %s\n", strings.Join(consumers, ", "))
	} else {
		sb.WriteString("- consumers: none enabled\n")
	}
	if warnings := decisionWarnings(dec); len(warnings) > 0 {
		for _, w := range warnings {
			fmt.Fprintf(&sb, "- warning:   %s\n", w)
		}
	}
	if !withHealth {
		return sb.String(), nil
	}
	if model == "(not set)" {
		sb.WriteString("- health:    skipped (no model configured)\n")
		return sb.String(), nil
	}
	hctx, cancel := context.WithTimeout(ctx, statusCallTimeout)
	defer cancel()
	p, err := ProviderWithTimeout(r, statusCallTimeout)
	if err != nil {
		fmt.Fprintf(&sb, "- health:    error: %s\n", scrub(err.Error(), r))
		return sb.String(), nil
	}
	fmt.Fprintf(&sb, "- health:    %s\n", scrub(RenderHealth(p.Health(hctx, strings.TrimSpace(r.Model))), r))
	return sb.String(), nil
}

// RenderModels lists the decision models the provider reports.
func RenderModels(ctx context.Context, showAll bool) (string, error) {
	cfg := config.Get()
	if cfg == nil {
		return "", fmt.Errorf("configuration is not loaded")
	}
	r := cfg.DecisionModel.Router
	p, err := ProviderWithTimeout(r, statusCallTimeout)
	if err != nil {
		return "", fmt.Errorf("%s", scrub(err.Error(), r))
	}
	lctx, cancel := context.WithTimeout(ctx, statusCallTimeout)
	defer cancel()
	list, status, lerr := p.ListDecisionModels(lctx, showAll)
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Decision models (%s, %s)\n\n", r.EffectiveProvider(), status)
	if lerr != nil {
		fmt.Fprintf(&sb, "Could not list models: %s\nCheck the provider address and credentials, or set the model id directly.\n", scrub(lerr.Error(), r))
		return sb.String(), nil
	}
	if len(list) == 0 {
		switch {
		case p.Kind() == systemone.KindOllama:
			sb.WriteString("No decision model installed. Try: " + systemone.OllamaPullHint + "\n")
		case status == systemone.ListUnsupported:
			sb.WriteString("This provider cannot list models; set the model id directly.\n")
		default:
			sb.WriteString("No models reported.\n")
		}
		return sb.String(), nil
	}
	for _, m := range list {
		fmt.Fprintf(&sb, "- %s\n", m.ID)
	}
	if status == systemone.ListUnfiltered {
		sb.WriteString("\nThe provider does not mark decision models; showing the whole catalogue.\n")
	}
	return sb.String(), nil
}

func decisionWarnings(d config.DecisionModelConfig) []string {
	_, w := config.ValidateDecisionModel(d)
	return w
}

// scrub removes the router API key from text coming back from a provider.
func scrub(s string, r config.DecisionRouterConfig) string {
	if key := strings.TrimSpace(r.EffectiveAPIKey()); len(key) >= 4 {
		s = strings.ReplaceAll(s, key, "***")
	}
	return s
}
