package acp

import (
	"context"
	"errors"
	"testing"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

func newLearningTestSession(t *testing.T) (*PandoACPAgent, *mockAgentService, acpsdk.SessionId, *ACPServerSession) {
	t.Helper()

	agent := newTestPandoAgent()
	resp, err := agent.NewSession(context.Background(), acpsdk.NewSessionRequest{Cwd: "/tmp"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	acpSession, err := agent.getSession(resp.SessionId)
	if err != nil {
		t.Fatalf("getSession failed: %v", err)
	}
	return agent, agent.agentService.(*mockAgentService), resp.SessionId, acpSession
}

func TestParseLearningSlashCommands(t *testing.T) {
	tests := []struct {
		input     string
		wantKind  slashCommandKind
		wantFocus string
	}{
		{input: "/learning", wantKind: slashCommandLearning},
		{input: "/learning understand the KB graph", wantKind: slashCommandLearning, wantFocus: "understand the KB graph"},
		{input: "/learning-finish", wantKind: slashCommandLearningFinish},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			command, ok := parseSlashCommand(tt.input)
			if !ok {
				t.Fatalf("expected %q to parse as a slash command", tt.input)
			}
			if command.Kind != tt.wantKind {
				t.Errorf("kind = %q, want %q", command.Kind, tt.wantKind)
			}
			if command.Objective != tt.wantFocus {
				t.Errorf("focus = %q, want %q", command.Objective, tt.wantFocus)
			}
		})
	}
}

func TestLearningCommandsAreAdvertised(t *testing.T) {
	advertised := map[string]bool{}
	for _, command := range availableCommands() {
		advertised[command.Name] = true
	}
	for _, name := range []string{learningCommandToken, learningFinishCommandToken} {
		if !advertised[name] {
			t.Errorf("expected /%s to be advertised to ACP clients", name)
		}
	}
}

func TestHandleLearningActivation(t *testing.T) {
	agent, mockSvc, sessionID, acpSession := newLearningTestSession(t)
	ctx := context.Background()

	stopReason, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandLearning, Objective: "learn the indexer"})
	if err != nil {
		t.Fatalf("handleSlashCommand failed: %v", err)
	}
	if stopReason != acpsdk.StopReasonEndTurn {
		t.Errorf("stopReason = %q, want %q", stopReason, acpsdk.StopReasonEndTurn)
	}
	if !mockSvc.LearningMode(acpSession.PandoSessionID()) {
		t.Fatal("expected the mode to be enabled")
	}
	// Activation is a control command: it must not consume an agent turn.
	if mockSvc.runCalled {
		t.Error("expected activation not to run an agent turn")
	}
}

func TestHandleLearningActivationIsIdempotent(t *testing.T) {
	agent, mockSvc, sessionID, acpSession := newLearningTestSession(t)
	ctx := context.Background()

	for range 2 {
		if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandLearning}); err != nil {
			t.Fatalf("handleSlashCommand failed: %v", err)
		}
	}
	if !mockSvc.LearningMode(acpSession.PandoSessionID()) {
		t.Fatal("expected the mode to remain active after re-invoking /learning")
	}
}

func TestHandleLearningFinishWhenInactive(t *testing.T) {
	agent, mockSvc, sessionID, acpSession := newLearningTestSession(t)

	stopReason, err := agent.handleSlashCommand(context.Background(), sessionID, acpSession, slashCommand{Kind: slashCommandLearningFinish})
	if err != nil {
		t.Fatalf("handleSlashCommand failed: %v", err)
	}
	if stopReason != acpsdk.StopReasonEndTurn {
		t.Errorf("stopReason = %q, want %q", stopReason, acpsdk.StopReasonEndTurn)
	}
	if mockSvc.learningFinishCalled {
		t.Error("expected no closing turn when the mode was never enabled")
	}
}

func TestHandleLearningFinishSuccessClearsMode(t *testing.T) {
	agent, mockSvc, sessionID, acpSession := newLearningTestSession(t)
	ctx := context.Background()
	mockSvc.learningFinishSucceeds = true

	if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandLearning}); err != nil {
		t.Fatalf("activation failed: %v", err)
	}
	if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandLearningFinish}); err != nil {
		t.Fatalf("finish failed: %v", err)
	}

	if !mockSvc.learningFinishCalled {
		t.Fatal("expected the closing turn to run")
	}
	if mockSvc.LearningMode(acpSession.PandoSessionID()) {
		t.Error("expected a successful close to disable the mode")
	}
}

func TestHandleLearningFinishFailureRetainsMode(t *testing.T) {
	agent, mockSvc, sessionID, acpSession := newLearningTestSession(t)
	ctx := context.Background()
	mockSvc.learningFinishErr = errors.New("closing turn exploded")

	if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandLearning}); err != nil {
		t.Fatalf("activation failed: %v", err)
	}

	_, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandLearningFinish})
	if err == nil {
		t.Fatal("expected the finish command to surface the run error")
	}
	if !mockSvc.LearningMode(acpSession.PandoSessionID()) {
		t.Error("expected a failed close to keep learning active")
	}
}
