package llmproxy

import (
	"context"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/tools"
)

// resolvedModel bundles a model with the provider account that should serve it.
type resolvedModel struct {
	model   models.Model
	account config.ProviderAccount
}

// activeAccounts returns all configured, non-disabled provider accounts.
func activeAccounts() []config.ProviderAccount {
	all := config.GetProviderAccounts()
	out := make([]config.ProviderAccount, 0, len(all))
	for _, a := range all {
		if a.Disabled {
			continue
		}
		out = append(out, a)
	}
	return out
}

// accountForModel finds the provider account that should serve the given model.
// It prefers the exact account the model was registered under (AccountID), then
// falls back to the first account matching the model's provider type.
func accountForModel(accounts []config.ProviderAccount, m models.Model) (config.ProviderAccount, bool) {
	if m.AccountID != "" {
		for _, a := range accounts {
			if a.ID == m.AccountID {
				return a, true
			}
		}
	}
	for _, a := range accounts {
		if a.Type == m.Provider {
			return a, true
		}
	}
	return config.ProviderAccount{}, false
}

// stripAccountPrefix removes a provider-type or account-ID prefix (e.g.
// "openai." or "myaccount.") from a requested model ID. It reports whether a
// prefix matching the given account was found.
func stripAccountPrefix(requested string, acc config.ProviderAccount) (string, bool) {
	if p := string(acc.Type) + "."; strings.HasPrefix(requested, p) {
		return strings.TrimPrefix(requested, p), true
	}
	if acc.ID != "" {
		if p := acc.ID + "."; strings.HasPrefix(requested, p) {
			return strings.TrimPrefix(requested, p), true
		}
	}
	return requested, false
}

// synthModel builds an on-the-fly model definition for a provider account when
// the requested model is not yet present in the registry. This lets clients use
// any model the upstream provider supports, even before the background refresh
// loop discovers it.
func synthModel(acc config.ProviderAccount, apiModel string) models.Model {
	id := models.ModelID(string(acc.Type) + "." + apiModel)
	if existing, ok := models.SupportedModels[id]; ok {
		return existing
	}
	return models.Model{
		ID:               id,
		Name:             apiModel,
		Provider:         acc.Type,
		APIModel:         apiModel,
		ContextWindow:    128_000,
		DefaultMaxTokens: 4096,
		AccountID:        acc.ID,
	}
}

// resolveModel resolves a requested model ID (as sent to /v1/chat/completions or
// /v1/messages) to a concrete model plus the provider account that should serve
// it. It accepts:
//   - registry IDs (e.g. "openai.gpt-4o", "copilot.gpt-5")
//   - raw upstream model IDs / APIModels (e.g. "gpt-4o")
//   - Anthropic shorthand aliases (e.g. "sonnet", "opus")
//
// This is what makes models listed by GET /v1/models actually usable: the list
// and chat handlers no longer have to agree on a single ID spelling.
func resolveModel(requested string) (resolvedModel, bool) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return resolvedModel{}, false
	}
	accounts := activeAccounts()

	// 1. Registry lookup via normalization (covers static + dynamically registered
	//    models, plus shorthand aliases).
	if m, ok := models.SupportedModels[models.NormalizeModelID(requested)]; ok {
		if acc, ok := accountForModel(accounts, m); ok {
			return resolvedModel{model: m, account: acc}, true
		}
	}
	// 1b. Direct raw key lookup (in case normalization rewrote an existing key).
	if m, ok := models.SupportedModels[models.ModelID(requested)]; ok {
		if acc, ok := accountForModel(accounts, m); ok {
			return resolvedModel{model: m, account: acc}, true
		}
	}

	// 2. Reverse lookup by APIModel scoped to a configured account. Handles raw
	//    upstream IDs (e.g. "gpt-4o") that were registered as "openai.gpt-4o".
	for _, acc := range accounts {
		for _, m := range models.SupportedModels {
			if m.Provider != acc.Type {
				continue
			}
			if m.AccountID != "" && m.AccountID != acc.ID {
				continue
			}
			if m.APIModel == requested {
				return resolvedModel{model: m, account: acc}, true
			}
		}
	}

	// 3. Provider/account-prefixed but not yet registered: synthesize a model so
	//    the request can still be served by the matching account.
	for _, acc := range accounts {
		if apiModel, matched := stripAccountPrefix(requested, acc); matched {
			return resolvedModel{model: synthModel(acc, apiModel), account: acc}, true
		}
	}

	// 4. Single-account fallback: if exactly one account is configured, treat the
	//    requested ID as an upstream APIModel for it.
	if len(accounts) == 1 {
		return resolvedModel{model: synthModel(accounts[0], requested), account: accounts[0]}, true
	}

	return resolvedModel{}, false
}

// proxyTool adapts an inbound tool/function definition to the tools.BaseTool
// interface so it can be advertised to the upstream provider. The proxy never
// executes tools itself — it relays the model's tool calls back to the client —
// so Run is intentionally a no-op error.
type proxyTool struct {
	name        string
	description string
	properties  map[string]any
	required    []string
}

func (t proxyTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name:        t.name,
		Description: t.description,
		Parameters:  t.properties,
		Required:    t.required,
	}
}

func (t proxyTool) Run(_ context.Context, _ tools.ToolCall) (tools.ToolResponse, error) {
	return tools.ToolResponse{}, fmt.Errorf("tool %q must be executed by the client, not the proxy", t.name)
}

// schemaToProxyTool converts a JSON-Schema parameters/input_schema object into a
// proxyTool, splitting out "properties" and "required" the way the providers
// expect them.
func schemaToProxyTool(name, description string, schema interface{}) proxyTool {
	t := proxyTool{name: name, description: description}
	obj, ok := schema.(map[string]interface{})
	if !ok {
		return t
	}
	if props, ok := obj["properties"].(map[string]interface{}); ok {
		t.properties = props
	}
	if req, ok := obj["required"].([]interface{}); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				t.required = append(t.required, s)
			}
		}
	}
	return t
}
