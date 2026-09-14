//go:build agui_fixture_agent

// Package agent — PANDO-T-0002 deterministic HITL fixture agent.
//
// This file exists ONLY in a binary built with `-tags agui_fixture_agent`.
// cmd/pando's default `go build ./...` (and every release build) never
// passes that tag, so this code is entirely absent from a production
// binary: there is no `AgentFixtureHITL` entry in config.KnownAgentNames
// (internal/config/fixture_agent.go, gated by the same tag), so
// agui.ConfigFromApp would drop the name even if someone tried to select
// it, and createAgentProvider's hook into this file
// (agent.go's `maybeFixtureProvider` call) resolves to the always-declining
// stub in fixture_hitl_agent_stub.go instead of this implementation.
//
// Even in a binary that WAS built with the tag, maybeFixtureProvider below
// still requires PANDO_AGUI_FIXTURE_AGENT=1 in the process environment
// before it does anything — a second, independent gate so a tagged
// debug/test binary does not silently behave differently from production
// unless a harness deliberately opts in.
//
// Purpose (PANDO-T-0002): PANDO-US-0008/0010 need a permission request and
// an AskUserQuestion prompt raised by a REAL `agui-serve`, answered through
// the real `approve`/`deny`/`answerQuestion` wire format
// (internal/agui/hitl.go), with the run resuming — all without an LLM call
// or credentials. This file fakes only the model side of that: everything
// else in the path (agent.Service, the real tool set built by
// agent.CoderAgentToolsWithMesnada, permission.Service, the
// userinput/hitlQuestionTool substitution, the run lifecycle, the SSE
// writer) is exactly what a real model-driven run uses.
package agent

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
	"github.com/google/uuid"
)

// fixtureResponseDelay is a deliberate, small artificial latency before this
// fake model answers each turn. It exists to close a real race PANDO-T-0002
// found while building this fixture: internal/agui/server.go's
// beginResumeSegment resolves the blocked permission/question tool call
// (waking the agent goroutine) and only THEN does handleExistingThreadRun
// call streamAttach -> attachRun -> run.subscribe(), on the same handler
// goroutine but strictly after. A real LLM's network latency always keeps
// that window open long enough for the resuming client's HTTP response to
// subscribe before the run produces its next events; a zero-latency fake
// model does not, and can race the run to full completion before the
// resuming client's own attach ever registers. attachRun's replay then stops
// at the FIRST segment boundary in the buffer (the earlier interrupt's
// RUN_FINISHED), never reaching the new segment's events even though they
// already happened -- so the resuming client observes a stale "still
// interrupted" replay of a run that actually already finished successfully
// server-side. This delay is comfortably larger than the handler's own
// synchronous work, so it reliably loses that race on purpose instead of
// occasionally winning it. This is a test-harness-only workaround, not a fix
// to internal/agui: the underlying attach/resume race is a genuine finding
// worth its own look, reported alongside this task rather than fixed here.
const fixtureResponseDelay = 150 * time.Millisecond

// FixtureHITLAgentName is this file's copy of the agent name literal.
// internal/config/fixture_agent.go declares the matching config.AgentName
// constant independently (config cannot import this package, which already
// imports config) — both are gated behind the same `agui_fixture_agent`
// build tag, so they can never drift into only one existing.
const FixtureHITLAgentName config.AgentName = "fixture-hitl"

// fixtureAgentEnvVar is the second, independent gate: even in a binary
// built with the tag, the fixture provider only activates when this is set
// to "1". A test harness (sdk/typescript's integration suite) sets it
// explicitly on the child process it spawns.
const fixtureAgentEnvVar = "PANDO_AGUI_FIXTURE_AGENT"

// maybeFixtureProvider is createAgentProvider's hook into this file. It
// returns ok=false for every agent name except the fixture one, and even
// then only when the env var opt-in is present — see the file doc comment
// for the full gating story.
func maybeFixtureProvider(agentName config.AgentName) (provider.Provider, bool) {
	if agentName != FixtureHITLAgentName {
		return nil, false
	}
	if os.Getenv(fixtureAgentEnvVar) != "1" {
		return nil, false
	}
	return &fixtureHITLProvider{}, true
}

// fixtureHITLProvider is a fake model, not a fake adapter: it never touches
// the network and never calls a real LLM. Turn 1 of a run inspects the
// user's message and deterministically "decides" to call either the real
// "write" tool (which requires permission, exercising the synthetic
// pando_permission_request suspend/resume path) or the real "AskUserQuestion"
// tool (which the AG-UI adapter substitutes with hitlQuestionTool when
// HumanInTheLoop is on, exercising the question suspend/resume path). Every
// following turn in the same run just ends it with a short text response —
// the fixture only needs to prove the round trip, not simulate a real
// conversation.
type fixtureHITLProvider struct{}

var _ provider.Provider = (*fixtureHITLProvider)(nil)

func (p *fixtureHITLProvider) Model() models.Model {
	return models.Model{
		ID:               "fixture-hitl",
		Name:             "PANDO-T-0002 fixture HITL agent (no LLM call)",
		Provider:         models.ModelProvider("fixture"),
		APIModel:         "fixture-hitl",
		ContextWindow:    200_000,
		DefaultMaxTokens: 8_192,
	}
}

func (p *fixtureHITLProvider) SendMessages(_ context.Context, messages []message.Message, _ []tools.BaseTool) (*provider.ProviderResponse, error) {
	resp, _ := p.decide(messages)
	return resp, nil
}

func (p *fixtureHITLProvider) StreamResponse(_ context.Context, messages []message.Message, _ []tools.BaseTool) <-chan provider.ProviderEvent {
	out := make(chan provider.ProviderEvent, 8)
	go func() {
		defer close(out)
		time.Sleep(fixtureResponseDelay)
		resp, call := p.decide(messages)
		if call != nil {
			out <- provider.ProviderEvent{Type: provider.EventToolUseStart, ToolCall: &message.ToolCall{ID: call.ID, Name: call.Name, Type: call.Type}}
			out <- provider.ProviderEvent{Type: provider.EventToolUseDelta, ToolCall: &message.ToolCall{ID: call.ID, Input: call.Input}}
			out <- provider.ProviderEvent{Type: provider.EventToolUseStop, ToolCall: &message.ToolCall{ID: call.ID}}
		} else if resp.Content != "" {
			out <- provider.ProviderEvent{Type: provider.EventContentDelta, Content: resp.Content}
		}
		out <- provider.ProviderEvent{Type: provider.EventComplete, Response: resp}
	}()
	return out
}

// decide inspects the conversation so far and returns this turn's response.
// When it "calls a tool", the same call is both the returned *message.ToolCall
// (used by StreamResponse to emit the streamed start/delta/stop events, the
// same shape a real streaming provider would) and resp.ToolCalls' sole entry
// (what resolveToolCallsOnComplete treats as authoritative).
func (p *fixtureHITLProvider) decide(history []message.Message) (*provider.ProviderResponse, *message.ToolCall) {
	if len(history) > 0 && history[len(history)-1].Role == message.Tool {
		// A tool already ran earlier in this same run (the permission or
		// question prompt was answered, denied or cancelled) -- always just
		// end the turn. The fixture only has to prove the run resumes
		// cleanly, not carry on a conversation.
		return &provider.ProviderResponse{
			Content:      "Understood, thank you.",
			FinishReason: message.FinishReasonEndTurn,
		}, nil
	}

	prompt := strings.ToLower(lastUserText(history))
	switch {
	case strings.Contains(prompt, "askuserquestion"):
		call := fixtureQuestionCall()
		return &provider.ProviderResponse{
			ToolCalls:    []message.ToolCall{*call},
			FinishReason: message.FinishReasonToolUse,
		}, call
	case strings.Contains(prompt, "write tool"):
		call := fixtureWriteCall(lastUserText(history))
		return &provider.ProviderResponse{
			ToolCalls:    []message.ToolCall{*call},
			FinishReason: message.FinishReasonToolUse,
		}, call
	default:
		return &provider.ProviderResponse{
			Content:      "No fixture scenario matched this prompt; nothing to do.",
			FinishReason: message.FinishReasonEndTurn,
		}, nil
	}
}

// lastUserText returns the text of the most recent user message, or "".
func lastUserText(history []message.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == message.User {
			return history[i].Content().Text
		}
	}
	return ""
}

var (
	fixtureWriteFileRe = regexp.MustCompile(`named (\S+)`)
	fixtureWriteBodyRe = regexp.MustCompile(`contents '([^']*)'`)
)

// fixtureWriteCall builds a deterministic call to the real "write" tool
// (internal/llm/tools/write.go), which requires permission -- exactly the
// path installPermissionPolicy/askClientForApproval (internal/agui/hitl.go)
// suspends on. file_path/content are parsed out of the prompt when it
// matches the "named <path> ... contents '<text>'" shape the integration
// suite's prompts use, falling back to a fixed name otherwise so the tool
// call is always well-formed.
func fixtureWriteCall(prompt string) *message.ToolCall {
	fileName := "fixture-write-test.txt"
	if m := fixtureWriteFileRe.FindStringSubmatch(prompt); len(m) == 2 {
		fileName = m[1]
	}
	content := "fixture-write-ok"
	if m := fixtureWriteBodyRe.FindStringSubmatch(prompt); len(m) == 2 {
		content = m[1]
	}
	input, _ := json.Marshal(tools.WriteParams{FilePath: fileName, Content: content})
	return &message.ToolCall{
		ID:       "fixture-" + uuid.NewString(),
		Name:     tools.WriteToolName,
		Input:    string(input),
		Type:     "function",
		Finished: true,
	}
}

// fixtureQuestionCall builds a deterministic call to the real
// "AskUserQuestion" tool (internal/llm/tools/ask_user_question.go). The AG-UI
// adapter substitutes it with hitlQuestionTool when HumanInTheLoop is on
// (internal/agui/agentpool.go), so this reaches the client exactly like a
// model-issued call would. Two questions are asked on purpose: q1 is
// single-select, q2 has multiSelect:true, so a client can exercise both the
// multi-select and (client-side, always available) "Other" free-text paths
// answering either one.
func fixtureQuestionCall() *message.ToolCall {
	params := tools.AskUserQuestionParams{
		Questions: []tools.AskUserQuestionParamQuestion{
			{
				Question: "Which environment should this change target?",
				Header:   "Environment",
				Options: []tools.AskUserQuestionParamOption{
					{Label: "Staging", Description: "The shared staging environment."},
					{Label: "Production", Description: "The live production environment."},
				},
			},
			{
				Question:    "Which frameworks should be supported?",
				Header:      "Frameworks",
				MultiSelect: true,
				Options: []tools.AskUserQuestionParamOption{
					{Label: "React", Description: "React / Next.js."},
					{Label: "Vue", Description: "Vue / Nuxt."},
					{Label: "Svelte", Description: "Svelte / SvelteKit."},
				},
			},
		},
	}
	input, _ := json.Marshal(params)
	return &message.ToolCall{
		ID:       "fixture-" + uuid.NewString(),
		Name:     tools.AskUserQuestionToolName,
		Input:    string(input),
		Type:     "function",
		Finished: true,
	}
}
