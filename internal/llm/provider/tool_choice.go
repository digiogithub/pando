package provider

import (
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
	"google.golang.org/genai"

	"github.com/digiogithub/pando/internal/llm/models"
)

// ToolChoiceMode says how the model may use the tools of a request. It is the
// provider-neutral form of OpenAI's tool_choice, Anthropic's tool_choice and
// Gemini's function calling mode.
type ToolChoiceMode string

const (
	// ToolChoiceAuto lets the model decide between text and tool calls. It is
	// also what every provider does when no tool choice is given.
	ToolChoiceAuto ToolChoiceMode = "auto"
	// ToolChoiceNone forbids tool calls even though tools are declared.
	ToolChoiceNone ToolChoiceMode = "none"
	// ToolChoiceRequired forces at least one call to any of the tools.
	ToolChoiceRequired ToolChoiceMode = "required"
	// ToolChoiceFunction forces a call to the tool named in ToolChoice.Name.
	ToolChoiceFunction ToolChoiceMode = "function"
)

// ToolChoice is a provider-neutral tool choice for one request. A nil
// *ToolChoice means "not specified": the provider default (auto) applies and
// nothing is sent upstream.
type ToolChoice struct {
	Mode ToolChoiceMode
	// Name is the tool to call; only meaningful for ToolChoiceFunction.
	Name string
}

// Forces reports whether the choice obliges the model to call a tool.
func (tc *ToolChoice) Forces() bool {
	return tc != nil && (tc.Mode == ToolChoiceRequired || tc.Mode == ToolChoiceFunction)
}

// String renders the choice for logs and headers.
func (tc *ToolChoice) String() string {
	if tc == nil {
		return "unset"
	}
	if tc.Mode == ToolChoiceFunction {
		return "function:" + tc.Name
	}
	return string(tc.Mode)
}

// Validate checks the choice is well formed.
func (tc *ToolChoice) Validate() error {
	if tc == nil {
		return nil
	}
	switch tc.Mode {
	case ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired:
		return nil
	case ToolChoiceFunction:
		if strings.TrimSpace(tc.Name) == "" {
			return fmt.Errorf("tool_choice of type function needs a function name")
		}
		return nil
	}
	return fmt.Errorf("unsupported tool_choice %q", tc.Mode)
}

// WithToolChoice sets the tool choice the client sends with every request
// that declares tools. A nil choice keeps the provider default.
func WithToolChoice(tc *ToolChoice) ProviderClientOption {
	return func(options *providerClientOptions) {
		options.toolChoice = tc
	}
}

// ToolChoiceSupport says how a provider honours a tool choice.
type ToolChoiceSupport string

const (
	// ToolChoiceNative means the provider API has a tool choice parameter
	// and Pando maps every mode to it.
	ToolChoiceNative ToolChoiceSupport = "native"
	// ToolChoiceForwarded means the OpenAI tool_choice field is forwarded to
	// an OpenAI-compatible server whose support Pando cannot know (custom
	// endpoints, llama.cpp, LOCAL_ENDPOINT). It is honoured only if the
	// server implements it.
	ToolChoiceForwarded ToolChoiceSupport = "forwarded"
	// ToolChoiceEmulated means the provider has no tool choice parameter:
	// "none" is emulated exactly by not sending tools, a named function by
	// sending only that tool, and "required" cannot be enforced at all.
	ToolChoiceEmulated ToolChoiceSupport = "emulated"
)

// ToolChoiceSupportFor returns how the given provider type honours a tool
// choice. Callers use it to decide between passing WithToolChoice and
// emulating the choice (see EmulateToolChoice).
func ToolChoiceSupportFor(p models.ModelProvider) ToolChoiceSupport {
	switch p {
	case models.ProviderOpenAI, models.ProviderAzure, models.ProviderCopilot,
		models.ProviderAnthropic, models.ProviderBedrock,
		models.ProviderGemini, models.ProviderVertexAI,
		models.ProviderGROQ, models.ProviderOpenRouter, models.ProviderXAI:
		return ToolChoiceNative
	case models.ProviderOllama:
		// Ollama's OpenAI compatibility layer documents tool_choice as not
		// supported and ignores it.
		return ToolChoiceEmulated
	default:
		return ToolChoiceForwarded
	}
}

// EmulateToolChoice applies a tool choice to the tool list for a provider
// that cannot receive it. It returns the tools to send and whether the
// choice is honoured exactly: "none" drops every tool (exact), a named
// function keeps only that tool (the model may still answer with text), and
// "required" keeps all tools (cannot be enforced).
func EmulateToolChoice[T any](tc *ToolChoice, tools []T, name func(T) string) ([]T, bool) {
	if tc == nil {
		return tools, true
	}
	switch tc.Mode {
	case ToolChoiceNone:
		return nil, true
	case ToolChoiceFunction:
		var kept []T
		for _, t := range tools {
			if name(t) == tc.Name {
				kept = append(kept, t)
			}
		}
		return kept, false
	case ToolChoiceRequired:
		return tools, false
	}
	return tools, true
}

// openAIToolChoiceParam maps a tool choice to the Chat Completions union.
// The zero value (omitted) is returned for a nil choice.
func openAIToolChoiceParam(tc *ToolChoice) openai.ChatCompletionToolChoiceOptionUnionParam {
	if tc == nil {
		return openai.ChatCompletionToolChoiceOptionUnionParam{}
	}
	if tc.Mode == ToolChoiceFunction {
		return openai.ChatCompletionToolChoiceOptionParamOfChatCompletionNamedToolChoice(
			openai.ChatCompletionNamedToolChoiceFunctionParam{Name: tc.Name},
		)
	}
	return openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String(string(tc.Mode))}
}

// responsesToolChoiceParam maps a tool choice to the Responses API union.
func responsesToolChoiceParam(tc *ToolChoice) responses.ResponseNewParamsToolChoiceUnion {
	if tc == nil {
		return responses.ResponseNewParamsToolChoiceUnion{}
	}
	if tc.Mode == ToolChoiceFunction {
		return responses.ResponseNewParamsToolChoiceUnion{
			OfFunctionTool: &responses.ToolChoiceFunctionParam{Name: tc.Name},
		}
	}
	return responses.ResponseNewParamsToolChoiceUnion{
		OfToolChoiceMode: openai.Opt(responses.ToolChoiceOptions(tc.Mode)),
	}
}

// anthropicToolChoiceParam maps a tool choice to Anthropic's union:
// required becomes {type:"any"} and a named function {type:"tool",name}.
func anthropicToolChoiceParam(tc *ToolChoice) anthropic.ToolChoiceUnionParam {
	if tc == nil {
		return anthropic.ToolChoiceUnionParam{}
	}
	switch tc.Mode {
	case ToolChoiceNone:
		return anthropic.ToolChoiceUnionParam{OfNone: &anthropic.ToolChoiceNoneParam{}}
	case ToolChoiceRequired:
		return anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}}
	case ToolChoiceFunction:
		return anthropic.ToolChoiceUnionParam{OfTool: &anthropic.ToolChoiceToolParam{Name: tc.Name}}
	}
	return anthropic.ToolChoiceUnionParam{OfAuto: &anthropic.ToolChoiceAutoParam{}}
}

// anthropicBetaToolChoiceParam is anthropicToolChoiceParam for the beta API.
func anthropicBetaToolChoiceParam(tc *ToolChoice) anthropic.BetaToolChoiceUnionParam {
	if tc == nil {
		return anthropic.BetaToolChoiceUnionParam{}
	}
	switch tc.Mode {
	case ToolChoiceNone:
		return anthropic.BetaToolChoiceUnionParam{OfNone: &anthropic.BetaToolChoiceNoneParam{}}
	case ToolChoiceRequired:
		return anthropic.BetaToolChoiceUnionParam{OfAny: &anthropic.BetaToolChoiceAnyParam{}}
	case ToolChoiceFunction:
		return anthropic.BetaToolChoiceUnionParam{OfTool: &anthropic.BetaToolChoiceToolParam{Name: tc.Name}}
	}
	return anthropic.BetaToolChoiceUnionParam{OfAuto: &anthropic.BetaToolChoiceAutoParam{}}
}

// geminiToolConfig maps a tool choice to Gemini's function calling config:
// required becomes mode ANY, a named function mode ANY restricted to that
// name. Nil is returned for a nil choice.
func geminiToolConfig(tc *ToolChoice) *genai.ToolConfig {
	if tc == nil {
		return nil
	}
	fc := &genai.FunctionCallingConfig{}
	switch tc.Mode {
	case ToolChoiceNone:
		fc.Mode = genai.FunctionCallingConfigModeNone
	case ToolChoiceRequired:
		fc.Mode = genai.FunctionCallingConfigModeAny
	case ToolChoiceFunction:
		fc.Mode = genai.FunctionCallingConfigModeAny
		fc.AllowedFunctionNames = []string{tc.Name}
	default:
		fc.Mode = genai.FunctionCallingConfigModeAuto
	}
	return &genai.ToolConfig{FunctionCallingConfig: fc}
}
