package acp

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
	acpsdk "github.com/madeindigio/acp-go-sdk"
)

// enableAutoModeForTest turns model auto mode on in the global config for the
// duration of the test.
func enableAutoModeForTest(t *testing.T, enabled bool) {
	t.Helper()
	prev := config.Get()
	config.SetForTests(&config.Config{ModelAutoMode: config.ModelAutoModeConfig{
		Enabled: enabled,
		Router:  config.DecisionRouterConfig{Provider: config.DecisionProviderOllama, Model: "tev1:0.8b"},
	}})
	t.Cleanup(func() { config.SetForTests(prev) })
}

func TestAutoModelOptionFirstAndDefault(t *testing.T) {
	enableAutoModeForTest(t, true)
	svc := &mockAgentService{
		currentModel:    "coder-model",
		availableModels: []ACPModelInfo{{ID: "coder-model", Name: "Coder"}, {ID: "other", Name: "Other"}},
		defaultAuto:     true,
	}
	session := NewACPServerSession(acpsdk.SessionId("acp-1"), "/tmp", nil, "pando-1")

	// Config option: auto first, and current for a new session under DefaultAuto.
	opt := sessionConfigOptionByID(t, buildSessionConfigOptions(svc, session), sessionConfigModelID)
	if got := string(opt.Select.CurrentValue); got != "auto" {
		t.Fatalf("current model = %q, want auto", got)
	}
	vals := opt.Select.Options.Ungrouped
	if vals == nil || len(*vals) != 3 || string((*vals)[0].Value) != "auto" || (*vals)[0].Name != "Auto" {
		t.Fatalf("auto must be the first option, got %+v", vals)
	}
	if desc := (*vals)[0].Description; desc == nil || *desc == "" {
		t.Fatalf("auto option needs a description")
	}

	// Model state: same ordering and current value.
	state := buildSessionModelState(svc, sessionModelValue(svc, session))
	if len(state.AvailableModels) != 3 || string(state.AvailableModels[0].ModelId) != "auto" {
		t.Fatalf("auto must be first in model state, got %+v", state.AvailableModels)
	}
	if string(state.CurrentModelId) != "auto" {
		t.Fatalf("model state current = %q, want auto", state.CurrentModelId)
	}

	// Session without DefaultAuto starts on the coder model.
	svc.defaultAuto = false
	opt = sessionConfigOptionByID(t, buildSessionConfigOptions(svc, session), sessionConfigModelID)
	if got := string(opt.Select.CurrentValue); got != "coder-model" {
		t.Fatalf("current model = %q, want coder-model", got)
	}

	// Disabled: no auto entry, and an auto session behaves as coder.
	enableAutoModeForTest(t, false)
	session.SetModel("auto")
	opt = sessionConfigOptionByID(t, buildSessionConfigOptions(svc, session), sessionConfigModelID)
	if got := string(opt.Select.CurrentValue); got != "coder-model" {
		t.Fatalf("disabled: current model = %q, want coder-model", got)
	}
	if vals := opt.Select.Options.Ungrouped; vals == nil || len(*vals) != 2 {
		t.Fatalf("disabled: expected no auto option, got %+v", vals)
	}
	if st := buildSessionModelState(svc, "auto"); len(st.AvailableModels) != 2 || string(st.CurrentModelId) != "coder-model" {
		t.Fatalf("disabled: unexpected model state %+v", st)
	}
}
