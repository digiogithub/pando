package acp

import (
	"context"
	"log"
	"testing"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

func TestResumeKeepsAuto(t *testing.T) {
	enableAutoModeForTest(t, true)
	ctx := context.Background()
	sessions := newMockSessionService()
	models := []ACPModelInfo{{ID: "coder-model", Name: "Coder"}}
	newAgent := func() (*PandoACPAgent, *mockAgentService) {
		svc := &mockAgentService{currentModel: "coder-model", availableModels: models}
		return NewPandoACPAgent("1.0.0-test", "/tmp", log.Default(), svc, sessions, nil), svc
	}

	agent, _ := newAgent()
	resp, err := agent.NewSession(ctx, acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := agent.validateModel("auto"); err != nil {
		t.Fatalf("validateModel(auto): %v", err)
	}
	if _, err := agent.SetSessionModel(ctx, acpsdk.SetSessionModelRequest{SessionId: resp.SessionId, ModelId: "auto"}); err != nil {
		t.Fatalf("SetSessionModel(auto): %v", err)
	}

	// A fresh agent (process restart) resumes from the persisted state.
	agent2, svc2 := newAgent()
	res, err := agent2.ResumeSession(ctx, acpsdk.ResumeSessionRequest{SessionId: resp.SessionId, Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	if res.Models == nil || string(res.Models.CurrentModelId) != "auto" {
		t.Fatalf("resume models = %+v, want current auto", res.Models)
	}
	opt := sessionConfigOptionByID(t, buildSessionConfigOptions(svc2, mustACPSession(t, agent2, resp.SessionId)), sessionConfigModelID)
	if got := string(opt.Select.CurrentValue); got != "auto" {
		t.Fatalf("resume config model = %q, want auto", got)
	}

	// The next prompt carries the explicit Auto flag to the agent.
	acpSession := mustACPSession(t, agent2, resp.SessionId)
	ov := sessionLLMOverridesFor(acpSession)
	if ov.Model != "" || ov.AutoMode == nil || !*ov.AutoMode {
		t.Fatalf("overrides = %+v, want AutoMode=true and no model", ov)
	}

	// Load (the Xcode/Zed path) restores it as well.
	agent3, _ := newAgent()
	loaded, err := agent3.LoadSession(ctx, acpsdk.LoadSessionRequest{SessionId: resp.SessionId, Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded.Models == nil || string(loaded.Models.CurrentModelId) != "auto" {
		t.Fatalf("load models = %+v, want current auto", loaded.Models)
	}

	// Picking a concrete model turns Auto off and persists it.
	if _, err := agent2.SetSessionModel(ctx, acpsdk.SetSessionModelRequest{SessionId: resp.SessionId, ModelId: "coder-model"}); err != nil {
		t.Fatalf("SetSessionModel(concrete): %v", err)
	}
	if auto := svc2.autoModes[acpSession.PandoSessionID()]; auto {
		t.Fatalf("expected AutoMode=false after a concrete pick")
	}
	agent4, _ := newAgent()
	res, err = agent4.ResumeSession(ctx, acpsdk.ResumeSessionRequest{SessionId: resp.SessionId, Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("ResumeSession(concrete): %v", err)
	}
	if string(res.Models.CurrentModelId) != "coder-model" {
		t.Fatalf("resume models = %+v, want coder-model", res.Models)
	}

	// With Auto disabled a persisted auto session behaves as coder.
	if _, err := agent2.SetSessionModel(ctx, acpsdk.SetSessionModelRequest{SessionId: resp.SessionId, ModelId: "auto"}); err != nil {
		t.Fatalf("SetSessionModel(auto): %v", err)
	}
	enableAutoModeForTest(t, false)
	agent5, _ := newAgent()
	res, err = agent5.ResumeSession(ctx, acpsdk.ResumeSessionRequest{SessionId: resp.SessionId, Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("ResumeSession(disabled): %v", err)
	}
	if string(res.Models.CurrentModelId) != "coder-model" {
		t.Fatalf("disabled resume models = %+v, want coder-model", res.Models)
	}
}

func mustACPSession(t *testing.T, a *PandoACPAgent, id acpsdk.SessionId) *ACPServerSession {
	t.Helper()
	s, err := a.getSession(id)
	if err != nil {
		t.Fatalf("getSession: %v", err)
	}
	return s
}
