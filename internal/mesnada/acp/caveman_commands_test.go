package acp

import (
	"context"
	"strings"
	"testing"

	acpsdk "github.com/madeindigio/acp-go-sdk"
)

func newCavemanTestSession(t *testing.T) (*PandoACPAgent, *mockAgentService, acpsdk.SessionId, *ACPServerSession) {
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

func TestParseCavemanSlashCommands(t *testing.T) {
	tests := []struct {
		input     string
		wantKind  slashCommandKind
		wantLevel string
	}{
		{input: "/caveman", wantKind: slashCommandCaveman},
		{input: "/caveman ultra", wantKind: slashCommandCaveman, wantLevel: "ultra"},
		{input: "/caveman lite", wantKind: slashCommandCaveman, wantLevel: "lite"},
		{input: "/caveman-finish", wantKind: slashCommandCavemanFinish},
		// A stray argument must survive parsing so the handler can reject it
		// instead of silently disabling the mode.
		{input: "/caveman-finish ultra", wantKind: slashCommandCavemanFinish, wantLevel: "ultra"},
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
			if command.Objective != tt.wantLevel {
				t.Errorf("level = %q, want %q", command.Objective, tt.wantLevel)
			}
		})
	}
}

func TestCavemanCommandsAreAdvertised(t *testing.T) {
	advertised := map[string]*acpsdk.AvailableCommandInput{}
	for _, command := range availableCommands() {
		advertised[command.Name] = command.Input
	}

	input, ok := advertised[cavemanCommandToken]
	if !ok {
		t.Fatalf("expected /%s to be advertised to ACP clients", cavemanCommandToken)
	}
	if input == nil || input.Unstructured == nil || strings.TrimSpace(input.Unstructured.Hint) == "" {
		t.Errorf("expected /%s to advertise a level hint", cavemanCommandToken)
	}

	finishInput, ok := advertised[cavemanFinishCommandToken]
	if !ok {
		t.Fatalf("expected /%s to be advertised to ACP clients", cavemanFinishCommandToken)
	}
	if finishInput != nil {
		t.Errorf("expected /%s to advertise no input, got %#v", cavemanFinishCommandToken, finishInput)
	}
}

func TestPandoACPAgent_HandleSlashCavemanSetsAndClearsLevel(t *testing.T) {
	agent, mockAgent, sessionID, acpSession := newCavemanTestSession(t)
	ctx := context.Background()
	sid := acpSession.PandoSessionID()

	// /caveman ultra → sets ultra.
	stop, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandCaveman, Objective: "ultra"})
	if err != nil {
		t.Fatalf("handleSlashCommand(ultra) failed: %v", err)
	}
	if stop != acpsdk.StopReasonEndTurn {
		t.Fatalf("expected end turn, got %q", stop)
	}
	if got := mockAgent.cavemanModes[sid]; got != "ultra" {
		t.Fatalf("expected ultra stored, got %q", got)
	}

	// /caveman with no argument → defaults to full.
	if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandCaveman}); err != nil {
		t.Fatalf("handleSlashCommand(default) failed: %v", err)
	}
	if got := mockAgent.cavemanModes[sid]; got != "full" {
		t.Fatalf("expected full by default, got %q", got)
	}

	// /caveman ultra → the explicit level is accepted.
	if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandCaveman, Objective: "ultra"}); err != nil {
		t.Fatalf("handleSlashCommand(ultra) failed: %v", err)
	}
	if got := mockAgent.cavemanModes[sid]; got != "ultra" {
		t.Fatalf("expected ultra stored, got %q", got)
	}

	// /caveman-finish → clears.
	if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandCavemanFinish}); err != nil {
		t.Fatalf("handleSlashCommand(finish) failed: %v", err)
	}
	if _, ok := mockAgent.cavemanModes[sid]; ok {
		t.Fatal("expected the level to be cleared after /caveman-finish")
	}
}

func TestPandoACPAgent_HandleSlashCavemanRejectsUnknownLevel(t *testing.T) {
	agent, mockAgent, sessionID, acpSession := newCavemanTestSession(t)
	ctx := context.Background()

	stop, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandCaveman, Objective: "shouty"})
	if err != nil {
		t.Fatalf("handleSlashCommand(shouty) failed: %v", err)
	}
	if stop != acpsdk.StopReasonEndTurn {
		t.Fatalf("expected end turn, got %q", stop)
	}
	if _, ok := mockAgent.cavemanModes[acpSession.PandoSessionID()]; ok {
		t.Fatal("an unknown level must not change the session state")
	}
	if usage := slashCommandUsage(slashCommandCaveman); !strings.Contains(usage, "/caveman [lite|full|ultra]") {
		t.Errorf("expected /caveman to expose its usage, got %q", usage)
	}
}

// /caveman-finish takes no level: "/caveman-finish ultra" must not look like it
// switched levels when it would actually disable the mode.
func TestPandoACPAgent_HandleSlashCavemanFinishRejectsArgument(t *testing.T) {
	agent, mockAgent, sessionID, acpSession := newCavemanTestSession(t)
	ctx := context.Background()
	sid := acpSession.PandoSessionID()

	if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandCaveman, Objective: "lite"}); err != nil {
		t.Fatalf("handleSlashCommand(lite) failed: %v", err)
	}
	if _, err := agent.handleSlashCommand(ctx, sessionID, acpSession, slashCommand{Kind: slashCommandCavemanFinish, Objective: "ultra"}); err != nil {
		t.Fatalf("handleSlashCommand(finish ultra) failed: %v", err)
	}

	if got := mockAgent.cavemanModes[sid]; got != "lite" {
		t.Errorf("a rejected /caveman-finish must leave the level untouched, got %q", got)
	}
	if usage := slashCommandUsage(slashCommandCavemanFinish); !strings.Contains(usage, "It takes no level") {
		t.Errorf("expected /caveman-finish to expose its usage, got %q", usage)
	}
}
