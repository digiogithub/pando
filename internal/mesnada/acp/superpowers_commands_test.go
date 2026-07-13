package acp

import (
	"context"
	"errors"
	"testing"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

func newSuperpowersTestSession(t *testing.T) (*PandoACPAgent, *mockAgentService, acpsdk.SessionId, *ACPServerSession) {
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

func TestParseSuperpowersSlashCommands(t *testing.T) {
	tests := []struct {
		input        string
		wantKind     slashCommandKind
		wantObjectiv string
	}{
		{input: "/superpowers", wantKind: slashCommandSuperpowers},
		{input: "/superpowers refactor the KB indexer", wantKind: slashCommandSuperpowers, wantObjectiv: "refactor the KB indexer"},
		{input: "/superpowers-finish", wantKind: slashCommandSuperpowersFinish},
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
			if command.Objective != tt.wantObjectiv {
				t.Errorf("objective = %q, want %q", command.Objective, tt.wantObjectiv)
			}
		})
	}
}

func TestSuperpowersCommandsAreAdvertised(t *testing.T) {
	advertised := map[string]bool{}
	for _, command := range availableCommands() {
		advertised[command.Name] = true
	}
	for _, name := range []string{superpowersCommandToken, superpowersFinishCommandToken} {
		if !advertised[name] {
			t.Errorf("expected /%s to be advertised to ACP clients", name)
		}
	}
}

func TestHandleSuperpowersActivation(t *testing.T) {
	agent, mockSvc, sessionID, acpSession := newSuperpowersTestSession(t)
	ctx := context.Background()

	stopReason, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandSuperpowers, Objective: "ship the parser"})
	if err != nil {
		t.Fatalf("handleSlashCommand failed: %v", err)
	}
	if stopReason != acpsdk.StopReasonEndTurn {
		t.Errorf("stopReason = %q, want %q", stopReason, acpsdk.StopReasonEndTurn)
	}
	if !mockSvc.SuperpowersMode(acpSession.PandoSessionID()) {
		t.Fatal("expected the mode to be enabled")
	}
	// Activation is a control command: it must not consume an agent turn.
	if mockSvc.runCalled {
		t.Error("expected activation not to run an agent turn")
	}
}

func TestHandleSuperpowersActivationIsIdempotent(t *testing.T) {
	agent, mockSvc, sessionID, acpSession := newSuperpowersTestSession(t)
	ctx := context.Background()

	for range 2 {
		if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandSuperpowers}); err != nil {
			t.Fatalf("handleSlashCommand failed: %v", err)
		}
	}
	if !mockSvc.SuperpowersMode(acpSession.PandoSessionID()) {
		t.Fatal("expected the mode to remain active after re-invoking /superpowers")
	}
}

func TestHandleSuperpowersFinishWhenInactive(t *testing.T) {
	agent, mockSvc, sessionID, acpSession := newSuperpowersTestSession(t)

	stopReason, err := agent.handleSlashCommand(context.Background(), sessionID, acpSession, slashCommand{Kind: slashCommandSuperpowersFinish})
	if err != nil {
		t.Fatalf("handleSlashCommand failed: %v", err)
	}
	if stopReason != acpsdk.StopReasonEndTurn {
		t.Errorf("stopReason = %q, want %q", stopReason, acpsdk.StopReasonEndTurn)
	}
	if mockSvc.superpowersFinishCalled {
		t.Error("expected no closing turn when the mode was never enabled")
	}
}

func TestHandleSuperpowersFinishSuccessClearsMode(t *testing.T) {
	agent, mockSvc, sessionID, acpSession := newSuperpowersTestSession(t)
	ctx := context.Background()
	mockSvc.superpowersFinishSucceeds = true

	if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandSuperpowers}); err != nil {
		t.Fatalf("activation failed: %v", err)
	}
	if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandSuperpowersFinish}); err != nil {
		t.Fatalf("finish failed: %v", err)
	}

	if !mockSvc.superpowersFinishCalled {
		t.Fatal("expected the closing turn to run")
	}
	if mockSvc.SuperpowersMode(acpSession.PandoSessionID()) {
		t.Error("expected a successful close to disable the mode")
	}
}

func TestHandleSuperpowersFinishFailureRetainsMode(t *testing.T) {
	agent, mockSvc, sessionID, acpSession := newSuperpowersTestSession(t)
	ctx := context.Background()
	mockSvc.superpowersFinishErr = errors.New("closing turn exploded")

	if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandSuperpowers}); err != nil {
		t.Fatalf("activation failed: %v", err)
	}

	_, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandSuperpowersFinish})
	if err == nil {
		t.Fatal("expected the finish command to surface the run error")
	}
	if !mockSvc.SuperpowersMode(acpSession.PandoSessionID()) {
		t.Error("expected a failed close to keep the workflow active")
	}
}
