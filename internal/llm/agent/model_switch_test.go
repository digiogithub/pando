package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/llm/provider"
	"github.com/digiogithub/pando/internal/llm/tools"
	"github.com/digiogithub/pando/internal/message"
	"github.com/digiogithub/pando/internal/pubsub"
)

func assistantWithReasoning() message.Message {
	return message.Message{
		ID:   "assistant-1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.ReasoningContent{Thinking: "internal deliberation"},
			message.TextContent{Text: "calling a tool"},
			message.ToolCall{ID: "call-1", Name: "view", Input: "{}", ThoughtSignature: []byte("sig")},
		},
	}
}

func TestSanitizeHistoryForModelSwitchStripsReasoningArtefacts(t *testing.T) {
	original := assistantWithReasoning()
	msgs := []message.Message{
		{ID: "user-1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}},
		original,
	}

	out := sanitizeHistoryForModelSwitch(msgs)

	if len(out) != len(msgs) {
		t.Fatalf("len = %d, want %d", len(out), len(msgs))
	}
	for _, part := range out[1].Parts {
		if _, isReasoning := part.(message.ReasoningContent); isReasoning {
			t.Fatal("reasoning content survived the switch")
		}
		if tc, ok := part.(message.ToolCall); ok && len(tc.ThoughtSignature) > 0 {
			t.Fatal("thought signature survived the switch")
		}
	}
	// The tool call itself must stay, or the following tool result is orphaned.
	if len(out[1].ToolCalls()) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(out[1].ToolCalls()))
	}
	if out[1].Content().Text != "calling a tool" {
		t.Fatalf("text content lost: %+v", out[1].Parts)
	}

	// The caller's messages (shared with what was loaded from the database) must
	// keep their reasoning: only what is sent to the new model is stripped.
	if msgs[1].ReasoningContent().Thinking != "internal deliberation" {
		t.Fatal("sanitization mutated the original message")
	}
	if tc := msgs[1].ToolCalls(); len(tc) != 1 || len(tc[0].ThoughtSignature) == 0 {
		t.Fatal("sanitization mutated the original tool call")
	}
}

func TestSanitizeHistoryForModelSwitchLeavesCleanHistoryAlone(t *testing.T) {
	msgs := []message.Message{
		{ID: "user-1", Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}},
		{ID: "assistant-1", Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "hello"}}},
	}

	out := sanitizeHistoryForModelSwitch(msgs)
	if out[1].Content().Text != "hello" || len(out[1].Parts) != 1 {
		t.Fatalf("clean history was altered: %+v", out[1].Parts)
	}
}

func TestHistoryHasImagesDetectsAttachmentsAndToolResults(t *testing.T) {
	text := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}},
	}
	if historyHasImages(text) {
		t.Fatal("text-only history reported as containing images")
	}

	attached := append(append([]message.Message{}, text...), message.Message{
		Role:  message.User,
		Parts: []message.ContentPart{message.BinaryContent{MIMEType: "image/png", Data: []byte{1}}},
	})
	if !historyHasImages(attached) {
		t.Fatal("user attachment not detected")
	}

	// Images produced mid-run by a tool count too: a screenshot makes the rest of
	// the conversation unusable for a text-only model.
	fromTool := append(append([]message.Message{}, text...), message.Message{
		Role: message.Tool,
		Parts: []message.ContentPart{message.ToolResult{
			ToolCallID: "call-1",
			Images:     []message.BinaryContent{{MIMEType: "image/png", Data: []byte{1}}},
		}},
	})
	if !historyHasImages(fromTool) {
		t.Fatal("tool-result image not detected")
	}
}

func TestSetupBridgeRefusesTextOnlyModelWhenHistoryHasImages(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "images")

	// The pricey model in the test catalogue has no attachment support.
	beginModelSwitchRun(sessionID, []message.Message{{
		Role:  message.User,
		Parts: []message.ContentPart{message.BinaryContent{MIMEType: "image/png", Data: []byte{1}}},
	}})
	t.Cleanup(func() { endModelSwitchRun(sessionID) })

	_, err := bridge.SetSessionModel(sessionID, string(testModelPricey), true)
	if err == nil {
		t.Fatal("expected a text-only model to be refused while images are in the history")
	}
	if !strings.Contains(err.Error(), "cannot read images") {
		t.Fatalf("error does not explain the refusal: %v", err)
	}
	if got := SessionLLMOverridesFor(sessionID).Model; got != "" {
		t.Fatalf("override = %q, want none", got)
	}
}

func TestSetupBridgeAllowsVisionModelWhenHistoryHasImages(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "images-ok")

	vision := models.ModelID("__mock.vision")
	models.SupportedModels[vision] = models.Model{
		ID: vision, Name: "Vision", Provider: models.ProviderMock,
		CostPer1MIn: 0.1, CostPer1MOut: 0.1, SupportsAttachments: true,
	}
	t.Cleanup(func() { delete(models.SupportedModels, vision) })

	beginModelSwitchRun(sessionID, []message.Message{{
		Role:  message.User,
		Parts: []message.ContentPart{message.BinaryContent{MIMEType: "image/png", Data: []byte{1}}},
	}})
	t.Cleanup(func() { endModelSwitchRun(sessionID) })

	switched, err := bridge.SetSessionModel(sessionID, string(vision), true)
	if err != nil {
		t.Fatalf("SetSessionModel: %v", err)
	}
	if !switched.Applied {
		t.Fatalf("switch = %+v, want it applied", switched)
	}
}

func TestSetupBridgeCapsSwitchesPerRun(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "cap")

	// Each alternative is cheaper than the previous one, so every step applies
	// unattended and the cap is what stops the sequence.
	ids := []models.ModelID{"__mock.alt1", "__mock.alt2", "__mock.alt3", "__mock.alt4"}
	for i, id := range ids {
		models.SupportedModels[id] = models.Model{
			ID: id, Name: string(id), Provider: models.ProviderMock,
			CostPer1MIn: 0.01 * float64(len(ids)-i), CostPer1MOut: 0.01 * float64(len(ids)-i),
		}
		t.Cleanup(func() { delete(models.SupportedModels, id) })
	}

	beginModelSwitchRun(sessionID, nil)
	t.Cleanup(func() { endModelSwitchRun(sessionID) })

	// Each step is cheaper than the cheap default (0.25/2), so all are unattended.
	for i := 0; i < defaultModelSwitchesPerRun; i++ {
		if _, err := bridge.SetSessionModel(sessionID, string(ids[i]), false); err != nil {
			t.Fatalf("switch %d: %v", i+1, err)
		}
	}

	_, err := bridge.SetSessionModel(sessionID, string(ids[len(ids)-1]), false)
	if err == nil {
		t.Fatal("expected the switch budget to be exhausted")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("error does not mention the limit: %v", err)
	}
	// The last successful switch stays in force.
	if got := SessionLLMOverridesFor(sessionID).Model; got != ids[defaultModelSwitchesPerRun-1] {
		t.Fatalf("override = %q, want %q", got, ids[defaultModelSwitchesPerRun-1])
	}
}

func TestSetupBridgeQuoteDoesNotConsumeSwitchBudget(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "cap-quote")

	beginModelSwitchRun(sessionID, nil)
	t.Cleanup(func() { endModelSwitchRun(sessionID) })

	// Quoting the same pricier switch repeatedly must not eat the budget, or the
	// user could never approve it.
	for i := 0; i < defaultModelSwitchesPerRun+2; i++ {
		quoted, err := bridge.SetSessionModel(sessionID, string(testModelPricey), false)
		if err != nil {
			t.Fatalf("quote %d: %v", i+1, err)
		}
		if quoted.Applied {
			t.Fatal("a quote must not apply the switch")
		}
	}

	confirmed, err := bridge.SetSessionModel(sessionID, string(testModelPricey), true)
	if err != nil {
		t.Fatalf("confirm after repeated quotes: %v", err)
	}
	if !confirmed.Applied {
		t.Fatal("the confirmed switch must apply: quotes should not have consumed the budget")
	}
}

func TestSetupBridgeSwitchBudgetIsPerRun(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "cap-reset")

	beginModelSwitchRun(sessionID, nil)
	state := modelSwitchRunStateFor(sessionID)
	if state == nil {
		t.Fatal("run state was not installed")
	}
	for i := 0; i < defaultModelSwitchesPerRun; i++ {
		state.reserveSwitch(defaultModelSwitchesPerRun)
	}
	if _, err := bridge.SetSessionModel(sessionID, string(testModelPricey), true); err == nil {
		t.Fatal("expected the exhausted budget to refuse the switch")
	}

	// A new run starts with a fresh budget.
	beginModelSwitchRun(sessionID, nil)
	t.Cleanup(func() { endModelSwitchRun(sessionID) })
	if _, err := bridge.SetSessionModel(sessionID, string(testModelPricey), false); err != nil {
		t.Fatalf("quote in the new run: %v", err)
	}
	if _, err := bridge.SetSessionModel(sessionID, string(testModelPricey), true); err != nil {
		t.Fatalf("switch in the new run: %v", err)
	}
}

func TestSetupBridgeGuardsInactiveOutsideARun(t *testing.T) {
	setupModelTestEnv(t, true)
	bridge, sessionID := newModelTestSession(t, "no-run")

	// With no run in flight there is no history to be incompatible with, so the
	// text-only model is accepted and no budget is charged.
	for i := 0; i < defaultModelSwitchesPerRun+1; i++ {
		if _, err := bridge.SetSessionModel(sessionID, string(testModelCheap), false); err != nil {
			t.Fatalf("switch %d outside a run: %v", i+1, err)
		}
		if _, err := bridge.SetSessionModel(sessionID, string(testModelUnknown), true); err != nil {
			// The unpriced model needs a confirmation, which the first call quotes;
			// only the refusal-free path matters here.
			continue
		}
	}
}

// switchStubProvider is a provider.Provider reporting a chosen model id, enough
// for applyPendingModelSwitch's comparison logic.
type switchStubProvider struct{ model models.Model }

func (p switchStubProvider) SendMessages(context.Context, []message.Message, []tools.BaseTool) (*provider.ProviderResponse, error) {
	return &provider.ProviderResponse{}, nil
}

func (p switchStubProvider) StreamResponse(context.Context, []message.Message, []tools.BaseTool) <-chan provider.ProviderEvent {
	ch := make(chan provider.ProviderEvent)
	close(ch)
	return ch
}

func (p switchStubProvider) Model() models.Model { return p.model }

func TestApplyPendingModelSwitchIsANoOpWithoutAnOverride(t *testing.T) {
	setupModelTestEnv(t, true)
	_, sessionID := newModelTestSession(t, "noop")

	a := &agent{agentName: config.AgentCoder, Broker: pubsub.NewBroker[AgentEvent]()}
	current := switchStubProvider{model: models.SupportedModels[testModelCheap]}
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}}}

	got, gotHistory := a.applyPendingModelSwitch(
		context.Background(), sessionID, current, "hi", "", history, nil)

	if got.Model().ID != testModelCheap {
		t.Fatalf("provider model = %q, want it untouched", got.Model().ID)
	}
	if len(gotHistory) != len(history) {
		t.Fatalf("history len = %d, want %d", len(gotHistory), len(history))
	}
}

func TestApplyPendingModelSwitchKeepsRunAliveWhenTheProviderCannotBeBuilt(t *testing.T) {
	setupModelTestEnv(t, true)
	_, sessionID := newModelTestSession(t, "rebuild-fails")

	// A model whose provider has no client implementation: building it fails with
	// "provider not supported".
	unbuildable := models.ModelID("__unbuildable.model")
	unbuildableProvider := models.ModelProvider("__unbuildable")
	models.SupportedModels[unbuildable] = models.Model{
		ID: unbuildable, Name: "Unbuildable", Provider: unbuildableProvider,
		CostPer1MIn: 0.1, CostPer1MOut: 0.1,
	}
	t.Cleanup(func() { delete(models.SupportedModels, unbuildable) })
	cfg := config.Get()
	cfg.ProviderAccounts = append(cfg.ProviderAccounts, config.ProviderAccount{
		ID: "unbuildable-1", Type: unbuildableProvider, APIKey: "key",
	})

	a := &agent{agentName: config.AgentCoder, Broker: pubsub.NewBroker[AgentEvent]()}
	current := switchStubProvider{model: models.SupportedModels[testModelCheap]}
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}}}

	SetSessionModelOverride(sessionID, unbuildable)
	events := make(chan AgentEvent, 4)

	got, _ := a.applyPendingModelSwitch(
		context.Background(), sessionID, current, "hi", "", history, events)

	if got.Model().ID != testModelCheap {
		t.Fatalf("provider model = %q, want the run to continue on the old model", got.Model().ID)
	}
	// The override must be dropped, or every following iteration retries it.
	if leftover := SessionLLMOverridesFor(sessionID).Model; leftover != "" {
		t.Fatalf("override = %q, want it cleared after the failure", leftover)
	}
	select {
	case event := <-events:
		if !strings.Contains(event.SystemMessage, "Could not switch") {
			t.Fatalf("event = %q, want the failure reported", event.SystemMessage)
		}
	default:
		t.Fatal("the failure was not reported to the user")
	}
}

func TestDescribeModelPriceStatesUnknownPrice(t *testing.T) {
	if got := describeModelPrice(models.Model{ID: "local.qwen"}); !strings.Contains(got, "unknown") {
		t.Fatalf("describeModelPrice = %q, want the price reported as unknown", got)
	}
	priced := describeModelPrice(models.Model{ID: "m", CostPer1MIn: 1.5, CostPer1MOut: 6})
	if !strings.Contains(priced, "$1.50 in / $6.00 out") {
		t.Fatalf("describeModelPrice = %q, want both prices", priced)
	}
}
