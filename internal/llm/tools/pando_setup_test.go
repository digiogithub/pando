package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// fakeSetupBridge is a scripted SetupBridge for the session/commands/run paths.
type fakeSetupBridge struct {
	info     SetupSessionInfo
	infoErr  error
	modes    []SetupMode
	commands []SetupRunnableCommand

	ranName string
	ranArgs string
	runOut  string
	runErr  error
}

func (f *fakeSetupBridge) SessionInfo(context.Context, string) (SetupSessionInfo, error) {
	return f.info, f.infoErr
}

func (f *fakeSetupBridge) SessionModes(string) []SetupMode { return f.modes }

func (f *fakeSetupBridge) Commands() []SetupRunnableCommand { return f.commands }

func (f *fakeSetupBridge) RunCommand(_ context.Context, _, name, args string) (string, error) {
	f.ranName, f.ranArgs = name, args
	return f.runOut, f.runErr
}

func runSetupTool(t *testing.T, tool BaseTool, ctx context.Context, command, args string) ToolResponse {
	t.Helper()
	input, err := json.Marshal(map[string]string{"command": command, "args": args})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	resp, err := tool.Run(ctx, ToolCall{Name: PandoSetupToolName, Input: string(input)})
	if err != nil {
		t.Fatalf("tool returned a transport error: %v", err)
	}
	return resp
}

func sessionCtx(sessionID string) context.Context {
	return context.WithValue(context.Background(), SessionIDContextKey, sessionID)
}

func TestPandoSetupInfoIsSelfDescribing(t *testing.T) {
	info := NewPandoSetupTool(nil).Info()

	if info.Name != PandoSetupToolName {
		t.Fatalf("name = %q, want %q", info.Name, PandoSetupToolName)
	}
	if len(info.Required) != 1 || info.Required[0] != "command" {
		t.Fatalf("required = %v, want [command]", info.Required)
	}
	for _, param := range []string{"command", "args"} {
		if _, ok := info.Parameters[param]; !ok {
			t.Fatalf("parameter %q missing from schema", param)
		}
	}
	// The description must point at help rather than enumerate the commands:
	// self-discovery is what keeps the always-loaded schema cheap.
	if !strings.Contains(info.Description, `command="help"`) {
		t.Fatalf("description does not advertise the help command:\n%s", info.Description)
	}
	if len(info.Description) > 600 {
		t.Fatalf("description is %d chars; keep it short and let help carry the detail", len(info.Description))
	}
	for _, cmd := range setupCommands() {
		if strings.Contains(info.Description, cmd.Usage) {
			t.Fatalf("description inlines the usage of %q; keep it discoverable via help", cmd.Name)
		}
	}
}

func TestPandoSetupHelp(t *testing.T) {
	tool := NewPandoSetupTool(nil)

	resp := runSetupTool(t, tool, context.Background(), "help", "")
	if resp.IsError {
		t.Fatalf("help errored: %s", resp.Content)
	}
	for _, cmd := range setupCommands() {
		if !strings.Contains(resp.Content, cmd.Name) {
			t.Fatalf("help omits command %q:\n%s", cmd.Name, resp.Content)
		}
	}

	// Both "help <command>" and "<command> --help" reach the same usage text.
	viaHelp := runSetupTool(t, tool, context.Background(), "help", "models")
	viaFlag := runSetupTool(t, tool, context.Background(), "models", "--help")
	if viaHelp.Content != viaFlag.Content {
		t.Fatalf("help models and models --help differ:\n%s\n---\n%s", viaHelp.Content, viaFlag.Content)
	}
	if !strings.Contains(viaFlag.Content, "--provider") {
		t.Fatalf("models usage does not document its flags:\n%s", viaFlag.Content)
	}
}

func TestPandoSetupEmptyCommandFallsBackToHelp(t *testing.T) {
	resp := runSetupTool(t, NewPandoSetupTool(nil), context.Background(), "  ", "")
	if resp.IsError {
		t.Fatalf("empty command errored: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "Commands:") {
		t.Fatalf("empty command did not print help:\n%s", resp.Content)
	}
}

func TestPandoSetupUnknownCommand(t *testing.T) {
	resp := runSetupTool(t, NewPandoSetupTool(nil), context.Background(), "nope", "")
	if !resp.IsError {
		t.Fatalf("unknown command should be an error, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "help") {
		t.Fatalf("unknown-command error should point at help:\n%s", resp.Content)
	}
}

func TestParseSetupArgs(t *testing.T) {
	args := parseSetupArgs(`--provider copilot --search "gpt 5" --detail sub arg --limit=10`)

	if got := args.String("provider"); got != "copilot" {
		t.Fatalf("provider = %q, want copilot", got)
	}
	if got := args.String("search"); got != "gpt 5" {
		t.Fatalf("search = %q, want %q", got, "gpt 5")
	}
	if got := args.String("limit"); got != "10" {
		t.Fatalf("limit = %q, want 10", got)
	}
	if !args.Bool("detail") {
		t.Fatal("detail should be a boolean flag")
	}
	if args.Bool("missing") {
		t.Fatal("absent flag must not read as true")
	}
	if got := args.Positional(0); got != "sub" {
		t.Fatalf("positional 0 = %q, want sub", got)
	}
	if got := strings.Join(args.PositionalsFrom(1), " "); got != "arg" {
		t.Fatalf("positionals from 1 = %q, want arg", got)
	}
	if got := args.Positional(9); got != "" {
		t.Fatalf("out-of-range positional = %q, want empty", got)
	}
}

func TestParseSetupArgsFlagFollowedByFlag(t *testing.T) {
	// "--all --provider x": --all must stay boolean instead of swallowing --provider.
	args := parseSetupArgs("--all --provider x")
	if !args.Bool("all") {
		t.Fatal("--all should be boolean")
	}
	if got := args.String("provider"); got != "x" {
		t.Fatalf("provider = %q, want x", got)
	}
}

func TestRedactSetupValueMasksSecretsOnly(t *testing.T) {
	tree := map[string]any{
		"providerAccounts": []any{
			map[string]any{
				"id":                "copilot",
				"apiKey":            "sk-super-secret-1234",
				"oauthAccessToken":  "tok-abcd",
				"oauthCodeVerifier": "verifier-value",
				"baseUrl":           "https://api.example.com",
				"extraHeaders":      map[string]any{"Authorization": "Bearer zzzz9999"},
			},
		},
		"mcpServers": map[string]any{
			"pando": map[string]any{
				"env": []any{"PANDO_API_KEY=abcdef123456", "DEBUG"},
			},
		},
		"tokenOptimization": map[string]any{"enabled": true},
		"session":           map[string]any{"promptTokens": float64(1200)},
		"ageKeys":           "AGE-SECRET-KEY-ZZZZ",
	}

	redactSetupValue(tree)

	account := tree["providerAccounts"].([]any)[0].(map[string]any)
	if got := account["apiKey"]; got != "****1234" {
		t.Fatalf("apiKey = %v, want ****1234", got)
	}
	if got := account["oauthAccessToken"]; got != "****abcd" {
		t.Fatalf("oauthAccessToken = %v, want ****abcd", got)
	}
	if got := account["oauthCodeVerifier"]; got != "****alue" {
		t.Fatalf("oauthCodeVerifier = %v, want ****alue", got)
	}
	if got := account["baseUrl"]; got != "https://api.example.com" {
		t.Fatalf("baseUrl was altered: %v", got)
	}
	headers := account["extraHeaders"].(map[string]any)
	if got := headers["Authorization"]; got != "****9999" {
		t.Fatalf("header value = %v, want ****9999", got)
	}

	env := tree["mcpServers"].(map[string]any)["pando"].(map[string]any)["env"].([]any)
	if got := env[0]; got != "PANDO_API_KEY=****3456" {
		t.Fatalf("env secret = %v, want PANDO_API_KEY=****3456", got)
	}
	if got := env[1]; got != "DEBUG" {
		t.Fatalf("valueless env entry was altered: %v", got)
	}

	// Plural "tokens" keys and "tokenOptimization" are counters, not secrets.
	if got := tree["session"].(map[string]any)["promptTokens"]; got != float64(1200) {
		t.Fatalf("promptTokens was masked: %v", got)
	}
	if got := tree["tokenOptimization"].(map[string]any)["enabled"]; got != true {
		t.Fatalf("tokenOptimization was masked: %v", got)
	}
	if got := tree["ageKeys"]; got != "****ZZZZ" {
		t.Fatalf("ageKeys = %v, want ****ZZZZ", got)
	}
}

func TestRedactSetupValueEmptySecretStaysEmpty(t *testing.T) {
	tree := map[string]any{"apiKey": "", "sourcegraphToken": "ab"}
	redactSetupValue(tree)

	if got := tree["apiKey"]; got != "" {
		t.Fatalf("empty secret = %v, want empty so the agent can tell it is unset", got)
	}
	if got := tree["sourcegraphToken"]; got != "****" {
		t.Fatalf("short secret = %v, want ****", got)
	}
}

func TestFilterSetupValue(t *testing.T) {
	tree := map[string]any{
		"lsp": map[string]any{
			"gopls":        map[string]any{"command": "gopls"},
			"typescript":   map[string]any{"command": "typescript-language-server"},
			"unrelatedKey": map[string]any{"command": "rust-analyzer"},
		},
	}

	kept := filterSetupValue(tree, "gopls")
	lsp := kept.(map[string]any)["lsp"].(map[string]any)
	if _, ok := lsp["gopls"]; !ok {
		t.Fatalf("gopls entry dropped: %v", lsp)
	}
	if _, ok := lsp["typescript"]; ok {
		t.Fatalf("non-matching entry kept: %v", lsp)
	}

	if got := filterSetupValue(tree, "nothing-matches-this"); got != nil {
		t.Fatalf("no match should return nil, got %v", got)
	}
}

func TestSummarizeSetupValue(t *testing.T) {
	cases := []struct {
		value any
		want  string
	}{
		{map[string]any{"a": 1, "b": 2}, "object, 2 keys (a, b)"},
		{[]any{1, 2, 3}, "list, 3 items"},
		{"", `""`},
		{nil, "null"},
		{true, "true"},
	}
	for _, tc := range cases {
		if got := summarizeSetupValue(tc.value); got != tc.want {
			t.Fatalf("summarizeSetupValue(%v) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestPandoSetupModels(t *testing.T) {
	const id models.ModelID = "pandosetuptest.demo-model"
	models.RegisterDynamicModel(models.Model{
		ID:               id,
		Name:             "Demo Model",
		Provider:         "pandosetuptest",
		APIModel:         "demo-model",
		ContextWindow:    1_000_000,
		DefaultMaxTokens: 64_000,
		CostPer1MIn:      1.25,
		CostPer1MOut:     10,
		CanReason:        true,
		Description:      "A demo model registered by the pando_setup tests.",
		Knowledge:        "2026-01",
	})
	t.Cleanup(func() { delete(models.SupportedModels, id) })

	tool := NewPandoSetupTool(nil)

	resp := runSetupTool(t, tool, context.Background(), "models", "--provider pandosetuptest")
	if resp.IsError {
		t.Fatalf("models errored: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, string(id)) {
		t.Fatalf("canonical model id missing:\n%s", resp.Content)
	}

	detail := runSetupTool(t, tool, context.Background(), "models", "--provider pandosetuptest --detail")
	for _, want := range []string{"context 1.0M", "max output 64K", "$1.25 in / $10.00 out", "reasoning", "knowledge 2026-01", "demo model registered"} {
		if !strings.Contains(detail.Content, want) {
			t.Fatalf("detail output missing %q:\n%s", want, detail.Content)
		}
	}

	empty := runSetupTool(t, tool, context.Background(), "models", "--provider does-not-exist")
	if empty.IsError {
		t.Fatalf("empty result should not be an error: %s", empty.Content)
	}
	if !strings.Contains(empty.Content, "No model matches") {
		t.Fatalf("empty result not reported:\n%s", empty.Content)
	}

	bad := runSetupTool(t, tool, context.Background(), "models", "--limit abc")
	if !bad.IsError {
		t.Fatalf("invalid --limit should error, got: %s", bad.Content)
	}
}

func TestPandoSetupModelsLimitTruncates(t *testing.T) {
	for i := 0; i < 3; i++ {
		id := models.ModelID(fmt.Sprintf("pandosetuplimit.model-%d", i))
		models.RegisterDynamicModel(models.Model{
			ID: id, Name: "Limit Model", Provider: "pandosetuplimit", APIModel: "m",
		})
		t.Cleanup(func() { delete(models.SupportedModels, id) })
	}

	resp := runSetupTool(t, NewPandoSetupTool(nil), context.Background(), "models", "--provider pandosetuplimit --limit 2")
	if !strings.Contains(resp.Content, "1 more hidden") {
		t.Fatalf("truncation not reported:\n%s", resp.Content)
	}
}

func TestPandoSetupSession(t *testing.T) {
	bridge := &fakeSetupBridge{
		info: SetupSessionInfo{
			ID: "sess-1", Title: "Demo", Model: "copilot.gpt-5.4",
			MessageCount: 12, PromptTokens: 1000, CompletionTokens: 250,
			CacheReadTokens: 40, CacheCreationTokens: 60, ReasoningTokens: 7,
			ContextWindow: 200_000, Cost: 0.1234,
		},
		modes: []SetupMode{{Name: "caveman", State: "full"}, {Name: "ponytail", State: "off"}},
	}
	tool := NewPandoSetupTool(bridge)

	resp := runSetupTool(t, tool, sessionCtx("sess-1"), "session", "")
	if resp.IsError {
		t.Fatalf("session errored: %s", resp.Content)
	}
	for _, want := range []string{"copilot.gpt-5.4", "prompt: 1000", "completion: 250", "reasoning: 7", "$0.1234", "caveman", "full"} {
		if !strings.Contains(resp.Content, want) {
			t.Fatalf("session output missing %q:\n%s", want, resp.Content)
		}
	}

	// Without a session in context there is nothing to report.
	noSession := runSetupTool(t, tool, context.Background(), "session", "")
	if !noSession.IsError {
		t.Fatalf("session without a session id should error, got: %s", noSession.Content)
	}
}

func TestPandoSetupWithoutBridge(t *testing.T) {
	tool := NewPandoSetupTool(nil)
	for _, command := range []string{"session", "commands", "run"} {
		resp := runSetupTool(t, tool, sessionCtx("sess-1"), command, "caveman")
		if !resp.IsError {
			t.Fatalf("%s without a bridge should error, got: %s", command, resp.Content)
		}
	}
}

func TestPandoSetupCommands(t *testing.T) {
	bridge := &fakeSetupBridge{
		commands: []SetupRunnableCommand{
			{Name: "caveman", Args: "[lite|full|ultra]", Summary: "Shorter answers"},
			{Name: "compact", Summary: "Compact the session", Reason: "cannot run inside an active turn"},
			{Name: "project:review", Summary: "Custom command"},
		},
	}
	tool := NewPandoSetupTool(bridge)

	resp := runSetupTool(t, tool, sessionCtx("sess-1"), "commands", "")
	if !strings.Contains(resp.Content, "caveman [lite|full|ultra]") {
		t.Fatalf("runnable command missing its argument hint:\n%s", resp.Content)
	}
	if !strings.Contains(resp.Content, "cannot run inside an active turn") {
		t.Fatalf("user-only reason missing:\n%s", resp.Content)
	}
	if strings.Contains(resp.Content, "project:review") {
		t.Fatalf("custom commands should stay behind --all:\n%s", resp.Content)
	}

	all := runSetupTool(t, tool, sessionCtx("sess-1"), "commands", "--all")
	if !strings.Contains(all.Content, "project:review") {
		t.Fatalf("--all did not list custom commands:\n%s", all.Content)
	}
}

func TestPandoSetupRun(t *testing.T) {
	bridge := &fakeSetupBridge{runOut: "Caveman mode enabled."}
	tool := NewPandoSetupTool(bridge)

	resp := runSetupTool(t, tool, sessionCtx("sess-1"), "run", "/caveman ultra now")
	if resp.IsError {
		t.Fatalf("run errored: %s", resp.Content)
	}
	if bridge.ranName != "caveman" {
		t.Fatalf("command name = %q, want caveman (leading slash must be stripped)", bridge.ranName)
	}
	if bridge.ranArgs != "ultra now" {
		t.Fatalf("command args = %q, want %q", bridge.ranArgs, "ultra now")
	}
	if resp.Content != "Caveman mode enabled." {
		t.Fatalf("run output = %q", resp.Content)
	}

	missing := runSetupTool(t, tool, sessionCtx("sess-1"), "run", "")
	if !missing.IsError {
		t.Fatalf("run without a command should error, got: %s", missing.Content)
	}
}

func TestPandoSetupRunPropagatesBridgeError(t *testing.T) {
	bridge := &fakeSetupBridge{runErr: fmt.Errorf("unknown slash command \"nope\"")}
	resp := runSetupTool(t, NewPandoSetupTool(bridge), sessionCtx("sess-1"), "run", "nope")
	if !resp.IsError {
		t.Fatalf("bridge error should surface as a tool error, got: %s", resp.Content)
	}
	if !strings.Contains(resp.Content, "nope") {
		t.Fatalf("bridge error text lost: %s", resp.Content)
	}
}

func TestBuildSetupConfigTreeRedactsRealConfig(t *testing.T) {
	cfg := &config.Config{
		WorkingDir: "/tmp/project",
		AgeKeys:    "AGE-SECRET-KEY-WXYZ",
		ProviderAccounts: []config.ProviderAccount{{
			ID:               "copilot",
			Type:             models.ProviderCopilot,
			APIKey:           "sk-live-000011112222",
			OAuthAccessToken: "gho-aaaabbbbcccc",
			BaseURL:          "https://api.githubcopilot.com",
			ExtraHeaders:     map[string]string{"Authorization": "Bearer 55556666"},
		}},
		MCPServers: map[string]config.MCPServer{
			"pando": {Command: "pando", Env: []string{"PANDO_TOKEN=zzzz88889999"}},
		},
	}

	tree, err := buildSetupConfigTree(cfg)
	if err != nil {
		t.Fatalf("buildSetupConfigTree: %v", err)
	}

	rendered, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshal tree: %v", err)
	}
	for _, secret := range []string{"sk-live-000011112222", "gho-aaaabbbbcccc", "Bearer 55556666", "zzzz88889999", "AGE-SECRET-KEY-WXYZ"} {
		if strings.Contains(string(rendered), secret) {
			t.Fatalf("secret %q leaked into the config dump:\n%s", secret, rendered)
		}
	}
	// Non-secret settings must survive so the dump is still useful.
	for _, want := range []string{"https://api.githubcopilot.com", "/tmp/project", "copilot"} {
		if !strings.Contains(string(rendered), want) {
			t.Fatalf("config dump lost %q:\n%s", want, rendered)
		}
	}
	if _, ok := tree["providerAccounts"]; !ok {
		t.Fatalf("providerAccounts section missing: %v", tree)
	}
}

func TestRenderSetupConfigIndex(t *testing.T) {
	tree := map[string]any{
		"mcpServers": map[string]any{"pando": map[string]any{}},
		"agents":     map[string]any{"coder": map[string]any{}},
		"debug":      false,
	}

	out := renderSetupConfigIndex(tree, "")
	for _, want := range []string{"mcpServers", "agents", "debug", "config <section>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("index missing %q:\n%s", want, out)
		}
	}

	filtered := renderSetupConfigIndex(tree, "mcp")
	if strings.Contains(filtered, "agents") {
		t.Fatalf("--search did not narrow the index:\n%s", filtered)
	}

	empty := renderSetupConfigIndex(tree, "zzz")
	if !strings.Contains(empty, "No section matches") {
		t.Fatalf("empty filter result not reported:\n%s", empty)
	}
}

func TestLookupSetupSectionIsCaseInsensitive(t *testing.T) {
	tree := map[string]any{"mcpServers": "value"}

	if _, ok := lookupSetupSection(tree, "mcpServers"); !ok {
		t.Fatal("exact section name not found")
	}
	if _, ok := lookupSetupSection(tree, "mcpservers"); !ok {
		t.Fatal("section lookup should be case-insensitive")
	}
	if _, ok := lookupSetupSection(tree, "nope"); ok {
		t.Fatal("unknown section should not resolve")
	}
}

func TestPandoSetupIsBuiltin(t *testing.T) {
	if !IsBuiltinTool(PandoSetupToolName) {
		t.Fatal("pando_setup must be registered as a built-in so the MCP gateway never claims it")
	}
}
