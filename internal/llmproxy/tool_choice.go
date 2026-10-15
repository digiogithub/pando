package llmproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/logging"
)

// toolChoiceHeader tells the client how the upstream provider handled the
// request's tool_choice. It is only set when the request carried one:
//
//	native     the provider API received the equivalent parameter
//	forwarded  tool_choice was forwarded to an OpenAI-compatible server
//	           that may or may not implement it
//	emulated   the provider has no tool choice; Pando emulated it exactly
//	           (tool_choice "none" sends no tools, "auto" is the default)
//	degraded   the provider has no tool choice and the forced call could not
//	           be enforced ("required" sends all tools, a named function
//	           sends only that tool); the model may answer with text
const toolChoiceHeader = "X-Pando-Tool-Choice"

// parseOpenAIToolChoice decodes OpenAI's tool_choice: the strings "auto",
// "none" and "required", or {"type":"function","function":{"name":"..."}}.
// The flattened {"type":"function","name":"..."} of the Responses API is
// accepted too. An absent or null value returns nil.
func parseOpenAIToolChoice(raw json.RawMessage) (*provider.ToolChoice, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var mode string
	if err := json.Unmarshal(raw, &mode); err == nil {
		tc := &provider.ToolChoice{Mode: provider.ToolChoiceMode(mode)}
		if tc.Mode == provider.ToolChoiceFunction {
			return nil, fmt.Errorf("tool_choice %q must be an object naming the function", mode)
		}
		return tc, tc.Validate()
	}
	var obj struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("tool_choice must be a string or an object: %w", err)
	}
	if obj.Type != "function" {
		return nil, fmt.Errorf("unsupported tool_choice type %q", obj.Type)
	}
	name := obj.Function.Name
	if name == "" {
		name = obj.Name
	}
	tc := &provider.ToolChoice{Mode: provider.ToolChoiceFunction, Name: name}
	return tc, tc.Validate()
}

// parseAnthropicToolChoice decodes Anthropic's tool_choice object:
// {"type":"auto"|"any"|"none"} or {"type":"tool","name":"..."}.
func parseAnthropicToolChoice(raw json.RawMessage) (*provider.ToolChoice, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var obj struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("tool_choice must be an object: %w", err)
	}
	var tc *provider.ToolChoice
	switch obj.Type {
	case "auto":
		tc = &provider.ToolChoice{Mode: provider.ToolChoiceAuto}
	case "none":
		tc = &provider.ToolChoice{Mode: provider.ToolChoiceNone}
	case "any":
		tc = &provider.ToolChoice{Mode: provider.ToolChoiceRequired}
	case "tool":
		tc = &provider.ToolChoice{Mode: provider.ToolChoiceFunction, Name: obj.Name}
	default:
		return nil, fmt.Errorf("unsupported tool_choice type %q", obj.Type)
	}
	return tc, tc.Validate()
}

// toolChoicePlan is what a handler does with a request's tool choice.
type toolChoicePlan struct {
	// tools are the tools to send upstream (narrowed when emulated).
	tools []tools.BaseTool
	// opts carries provider.WithToolChoice when the provider receives it.
	opts []provider.ProviderClientOption
	// header is the toolChoiceHeader value; empty when no tool choice.
	header string
}

// planToolChoice validates a tool choice against the request's tools and
// decides, from the provider type, whether to pass it upstream or emulate it.
// A validation error is a client error (HTTP 400).
func planToolChoice(tc *provider.ToolChoice, reqTools []tools.BaseTool, providerType models.ModelProvider, model string) (toolChoicePlan, error) {
	plan := toolChoicePlan{tools: reqTools}
	if tc == nil {
		return plan, nil
	}
	if tc.Forces() && len(reqTools) == 0 {
		return plan, fmt.Errorf("tool_choice %q requires at least one tool", tc.String())
	}
	if tc.Mode == provider.ToolChoiceFunction && !hasTool(reqTools, tc.Name) {
		return plan, fmt.Errorf("tool_choice names function %q, which is not in tools", tc.Name)
	}

	switch support := provider.ToolChoiceSupportFor(providerType); support {
	case provider.ToolChoiceEmulated:
		var exact bool
		plan.tools, exact = provider.EmulateToolChoice(tc, reqTools, func(t tools.BaseTool) string { return t.Info().Name })
		if exact {
			plan.header = string(provider.ToolChoiceEmulated)
		} else {
			plan.header = "degraded"
			logging.Warn("llm-proxy: provider cannot enforce tool_choice; forced tool call degraded",
				"provider", providerType, "model", model, "tool_choice", tc.String())
		}
	default:
		plan.opts = append(plan.opts, provider.WithToolChoice(tc))
		plan.header = string(support)
		if support == provider.ToolChoiceForwarded {
			logging.Debug("llm-proxy: tool_choice forwarded to an OpenAI-compatible server that may ignore it",
				"provider", providerType, "model", model, "tool_choice", tc.String())
		}
	}
	return plan, nil
}

// setHeader writes the tool choice header when the request had a choice.
func (p toolChoicePlan) setHeader(w http.ResponseWriter) {
	if p.header != "" {
		w.Header().Set(toolChoiceHeader, p.header)
	}
}

func hasTool(ts []tools.BaseTool, name string) bool {
	for _, t := range ts {
		if t.Info().Name == name {
			return true
		}
	}
	return false
}
