package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

const (
	PandoSetupToolName = "pando_setup"

	pandoSetupDescription = `Pando's own control panel: inspect configuration and runtime state, discover
available models, read session usage, switch session modes, and — when the user
asks for it and the capability is enabled — change this session's model.

It works like a CLI: pick a command and pass CLI-style arguments. Run it with
command="help" to list every command, or pass "--help" as args to any command to
see its own usage. Configuration access is read-only and secrets stay masked.`

	pandoSetupCommandParamDescription = `Command to run. Use "help" to list all available commands.`

	pandoSetupArgsParamDescription = `CLI-style arguments for the command (e.g. "--provider copilot --detail").
Pass "--help" to print the command's own usage.`
)

// ---------------------------------------------------------------------------
// Bridge
// ---------------------------------------------------------------------------

// SetupSessionInfo is the usage snapshot of the current session. The token
// counters describe the most recent turn — the session record overwrites them on
// every request — while Cost accumulates over the whole session.
type SetupSessionInfo struct {
	ID                  string  `json:"id"`
	Title               string  `json:"title,omitempty"`
	Model               string  `json:"model,omitempty"`
	MessageCount        int64   `json:"messageCount"`
	PromptTokens        int64   `json:"promptTokens"`
	CompletionTokens    int64   `json:"completionTokens"`
	CacheReadTokens     int64   `json:"cacheReadTokens"`
	CacheCreationTokens int64   `json:"cacheCreationTokens"`
	ReasoningTokens     int64   `json:"reasoningTokens"`
	ContextWindow       int64   `json:"contextWindow,omitempty"`
	Cost                float64 `json:"cost"`
}

// SetupMode reports the state of one session mode (caveman, ponytail, ...).
type SetupMode struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

// SetupRunnableCommand describes a slash command the agent may activate itself.
type SetupRunnableCommand struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Args    string `json:"args,omitempty"`
	// Reason is set only when the command is listed but not runnable by the
	// agent; it explains why the user has to trigger it.
	Reason string `json:"reason,omitempty"`
}

// Runnable reports whether the agent may activate this command itself.
func (c SetupRunnableCommand) Runnable() bool { return c.Reason == "" }

// SetupModelState describes the model a session runs on, together with the price
// metadata used to decide whether a switch needs the user's confirmation.
type SetupModelState struct {
	ID            string  `json:"id"`
	Name          string  `json:"name,omitempty"`
	Provider      string  `json:"provider,omitempty"`
	Account       string  `json:"account,omitempty"`
	ContextWindow int64   `json:"contextWindow,omitempty"`
	CostIn        float64 `json:"costIn"`
	CostOut       float64 `json:"costOut"`
	// CostKnown is false when the catalogue has no price for the model (local
	// models, mock providers, entries models.dev could not enrich). It is not the
	// same as a price of zero and must not be reported as "free".
	CostKnown bool `json:"costKnown"`
	// Overridden reports that the model comes from a session-scoped override
	// rather than from the agent's configured default.
	Overridden bool `json:"overridden"`
}

// SetupModelSwitch is the outcome of a model-switch request.
type SetupModelSwitch struct {
	// Applied is false when the switch was only quoted and still needs the user's
	// confirmation; in that case nothing changed.
	Applied  bool            `json:"applied"`
	Previous SetupModelState `json:"previous"`
	Target   SetupModelState `json:"target"`
	// ConfirmationReason is set only when Applied is false: why the user has to
	// approve (the target costs more, or a price is missing on either side).
	ConfirmationReason string `json:"confirmationReason,omitempty"`
	// Cleared reports that the session override was removed and the agent's
	// configured default is back in use.
	Cleared bool `json:"cleared,omitempty"`
}

// SetupBridge exposes the session-scoped state that pando_setup needs. It is
// implemented outside this package (the agent owns session modes and the
// session store, and both already import this package), so the tool stays
// dependency-free and testable with a fake.
type SetupBridge interface {
	// SessionInfo returns the usage snapshot for a session.
	SessionInfo(ctx context.Context, sessionID string) (SetupSessionInfo, error)
	// SessionModes lists the session modes and their current state.
	SessionModes(sessionID string) []SetupMode
	// Commands lists the slash commands, runnable or not.
	Commands() []SetupRunnableCommand
	// RunCommand activates a slash command for the session and returns the text
	// the agent should read (a confirmation, or the command's own instructions).
	RunCommand(ctx context.Context, sessionID, name, args string) (string, error)
	// CurrentModel reports the model the session runs on.
	CurrentModel(sessionID string) (SetupModelState, error)
	// SetSessionModel switches the model for this session only. An empty modelID
	// clears the override. When the target costs more than the current model — or
	// either price is unknown — the switch is only applied if confirmed is true;
	// otherwise it returns a quote with Applied false and changes nothing.
	SetSessionModel(sessionID, modelID string, confirmed bool) (SetupModelSwitch, error)
}

// ---------------------------------------------------------------------------
// Tool
// ---------------------------------------------------------------------------

type pandoSetupTool struct {
	bridge SetupBridge
	lsp    LSPProvider
}

// NewPandoSetupTool returns the always-on tool that lets the agent inspect and
// steer its own Pando instance. Both dependencies may be nil: the configuration
// and model commands still work, while the session, command and lsp commands
// report unavailable.
func NewPandoSetupTool(bridge SetupBridge, lspProvider LSPProvider) BaseTool {
	return &pandoSetupTool{bridge: bridge, lsp: lspProvider}
}

func (t *pandoSetupTool) Info() ToolInfo {
	return ToolInfo{
		Name:        PandoSetupToolName,
		Description: pandoSetupDescription,
		Parameters: map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": pandoSetupCommandParamDescription,
			},
			"args": map[string]any{
				"type":        "string",
				"description": pandoSetupArgsParamDescription,
			},
		},
		Required: []string{"command"},
	}
}

func (t *pandoSetupTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params struct {
		Command string `json:"command"`
		Args    string `json:"args"`
	}
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("invalid parameters: " + err.Error()), nil
	}

	name := strings.ToLower(strings.TrimSpace(params.Command))
	if name == "" {
		return NewTextResponse(renderSetupHelp("")), nil
	}

	cmd, ok := lookupSetupCommand(name)
	if !ok {
		return NewTextErrorResponse(fmt.Sprintf(
			"unknown command %q. Run pando_setup with command=\"help\" to list the available commands.", name)), nil
	}

	args := parseSetupArgs(params.Args)
	if args.Bool("help") || args.Bool("h") {
		return NewTextResponse(cmd.Usage), nil
	}

	out, err := cmd.Run(ctx, t, args)
	if err != nil {
		return NewTextErrorResponse(err.Error()), nil
	}
	return NewTextResponse(out), nil
}

// ---------------------------------------------------------------------------
// Command table
// ---------------------------------------------------------------------------

type setupCommand struct {
	Name    string
	Summary string
	Usage   string
	Run     func(ctx context.Context, t *pandoSetupTool, args setupArgs) (string, error)
}

func setupCommands() []setupCommand {
	return []setupCommand{
		{
			Name:    "help",
			Summary: "List the commands, or show the usage of one of them",
			Usage: `Usage: help [command]
Without an argument it lists every command with a one-line summary.`,
			Run: func(_ context.Context, _ *pandoSetupTool, args setupArgs) (string, error) {
				return renderSetupHelp(args.Positional(0)), nil
			},
		},
		{
			Name:    "config",
			Summary: "Read the active configuration (read-only, secrets masked)",
			Usage: `Usage: config [section] [--search TERM]
Without a section it lists the sections and their size.
  section    Section to dump as JSON (e.g. mcpServers, agents, lsp, remembrances).
  --search   Only keep entries whose key or value contains TERM (case-insensitive).
Values are read-only here; API keys, tokens and headers are always masked.`,
			Run: runSetupConfig,
		},
		{
			Name:    "providers",
			Summary: "List the configured provider accounts and their credential state",
			Usage: `Usage: providers [--all]
Lists enabled provider accounts: id, type, credentials, base URL and model count.
  --all   Include accounts that are disabled.`,
			Run: runSetupProviders,
		},
		{
			Name:    "models",
			Summary: "List selectable models with their canonical id (e.g. copilot.gpt-5.4)",
			Usage: `Usage: models [--provider TYPE] [--account ID] [--search TERM] [--detail] [--limit N]
Prints the model ids accepted anywhere a model must be chosen, subagents included.
  --provider  Keep one provider type only (anthropic, copilot, openai, ...).
  --account   Keep one provider account id only.
  --search    Match id, name or description (case-insensitive).
  --detail    Add cost per 1M tokens, context window, capabilities and description.
  --limit     Maximum rows to print (default 60, 0 = no limit).`,
			Run: runSetupModels,
		},
		{
			Name:    "model",
			Summary: "Show the model this session runs on, or switch it",
			Usage: `Usage: model [<model-id>] [--confirm] [--clear]
Without arguments it prints the active model, its price and where it comes from.
  <model-id>  Switch this session to that model (run "models" for the ids).
  --confirm   Approve a switch the user has just accepted (see below).
  --clear     Drop the session override and go back to the configured model.
The switch is scoped to this session, lives in memory only and is lost when Pando
restarts. It takes effect on the very next request, so a switch made mid-task
answers the rest of the task on the new model.
Switching to a model that costs more — or to or from a model with no published
price — is NOT applied on the first call: you get a quote to show the user, and
only after the user accepts do you repeat the call with --confirm.
Never confirm on your own behalf: the confirmation must come from the user.`,
			Run: runSetupModel,
		},
		{
			Name:    "lsp",
			Summary: "List the language servers: installed, installable by Pando, or manual",
			Usage: `Usage: lsp [name] [--all] [--missing]
Without arguments it lists the servers that are configured, running or already
installed, plus the global on-demand activation settings.
  name        Show one server in detail (files handled, install command, state).
  --all       List the whole built-in catalogue, including opt-in servers.
  --missing   List only the servers that need a manual install.
Servers marked "installable" are installed by Pando with bun or npm the first
time a matching file is touched; "not installed" ones print the command to run.`,
			Run: runSetupLSP,
		},
		{
			Name:    "session",
			Summary: "Show this session's token usage, cost and active modes",
			Usage: `Usage: session
Reports the message count, the last turn's prompt/completion/cache/reasoning tokens,
the cost accumulated so far, the model in use and the state of every session mode.`,
			Run: runSetupSession,
		},
		{
			Name:    "commands",
			Summary: "List the slash commands, marking the ones this tool can activate",
			Usage: `Usage: commands [--all]
Lists the slash commands runnable with "run". Commands that only the user can
trigger are listed with the reason.
  --all   Also list the user/project custom commands.`,
			Run: runSetupCommands,
		},
		{
			Name:    "run",
			Summary: "Activate a slash command for this session",
			Usage: `Usage: run <command> [arguments]
Activates a slash command without the user typing it, e.g. "run caveman ultra".
Mode commands apply from the next turn on. Commands that carry instructions
return them as text: follow them immediately in the current turn.
Run "commands" first to see what is available.`,
			Run: runSetupRun,
		},
	}
}

func lookupSetupCommand(name string) (setupCommand, bool) {
	for _, cmd := range setupCommands() {
		if cmd.Name == name {
			return cmd, true
		}
	}
	return setupCommand{}, false
}

func renderSetupHelp(topic string) string {
	topic = strings.ToLower(strings.TrimSpace(topic))
	if topic != "" {
		if cmd, ok := lookupSetupCommand(topic); ok {
			return cmd.Usage
		}
		return fmt.Sprintf("unknown command %q.\n\n%s", topic, renderSetupHelp(""))
	}

	var sb strings.Builder
	sb.WriteString("pando_setup — inspect and steer this Pando instance.\n\n")
	sb.WriteString("Commands:\n")
	for _, cmd := range setupCommands() {
		sb.WriteString(fmt.Sprintf("  %-10s %s\n", cmd.Name, cmd.Summary))
	}
	sb.WriteString("\nPass args=\"--help\" to any command for its own usage.\n")
	return sb.String()
}

// ---------------------------------------------------------------------------
// config
// ---------------------------------------------------------------------------

func runSetupConfig(_ context.Context, _ *pandoSetupTool, args setupArgs) (string, error) {
	cfg := config.Get()
	if cfg == nil {
		return "", fmt.Errorf("configuration is not loaded")
	}

	tree, err := buildSetupConfigTree(cfg)
	if err != nil {
		return "", err
	}

	search := strings.ToLower(strings.TrimSpace(args.String("search")))
	section := strings.TrimSpace(args.Positional(0))

	if section == "" {
		return renderSetupConfigIndex(tree, search), nil
	}

	value, ok := lookupSetupSection(tree, section)
	if !ok {
		return "", fmt.Errorf("unknown configuration section %q. Run \"config\" with no argument to list the sections", section)
	}
	if search != "" {
		value = filterSetupValue(value, search)
		if value == nil {
			return fmt.Sprintf("No entry in section %q matches %q.", section, search), nil
		}
	}

	pretty, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to render section %q: %w", section, err)
	}
	return fmt.Sprintf("## config.%s (read-only, secrets masked)\n\n```json\n%s\n```\n", section, pretty), nil
}

// buildSetupConfigTree turns the live configuration into a generic, redacted
// tree. Going through JSON rather than a hand-written view is what keeps the
// tool in sync with the settings panels: a new configuration section shows up
// here the moment it is added to the struct.
func buildSetupConfigTree(cfg *config.Config) (map[string]any, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration: %w", err)
	}
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("failed to read configuration: %w", err)
	}
	redactSetupValue(tree)
	return tree, nil
}

func renderSetupConfigIndex(tree map[string]any, search string) string {
	keys := make([]string, 0, len(tree))
	for k := range tree {
		if search != "" && !strings.Contains(strings.ToLower(k), search) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("## Configuration sections (read-only, secrets masked)\n\n")
	if len(keys) == 0 {
		sb.WriteString("No section matches the filter.\n")
		return sb.String()
	}
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("- %-20s %s\n", k, summarizeSetupValue(tree[k])))
	}
	sb.WriteString("\nUse: config <section> — e.g. config mcpServers\n")
	return sb.String()
}

// lookupSetupSection resolves a section name case-insensitively.
func lookupSetupSection(tree map[string]any, section string) (any, bool) {
	if v, ok := tree[section]; ok {
		return v, true
	}
	lower := strings.ToLower(section)
	for k, v := range tree {
		if strings.ToLower(k) == lower {
			return v, true
		}
	}
	return nil, false
}

// summarizeSetupValue renders a one-line preview of a configuration value.
func summarizeSetupValue(v any) string {
	switch value := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for k := range value {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 6 {
			return fmt.Sprintf("object, %d keys (%s, ...)", len(keys), strings.Join(keys[:6], ", "))
		}
		return fmt.Sprintf("object, %d keys (%s)", len(keys), strings.Join(keys, ", "))
	case []any:
		return fmt.Sprintf("list, %d items", len(value))
	case string:
		if value == "" {
			return `""`
		}
		if len(value) > 60 {
			return strconv.Quote(value[:60] + "…")
		}
		return strconv.Quote(value)
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", value)
	}
}

// filterSetupValue keeps only the entries whose key or rendered value contains
// term. It returns nil when nothing matches.
func filterSetupValue(v any, term string) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any)
		for k, child := range value {
			if strings.Contains(strings.ToLower(k), term) {
				out[k] = child
				continue
			}
			if kept := filterSetupValue(child, term); kept != nil {
				out[k] = kept
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		var out []any
		for _, child := range value {
			if kept := filterSetupValue(child, term); kept != nil {
				out = append(out, kept)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		if strings.Contains(strings.ToLower(value), term) {
			return value
		}
		return nil
	default:
		return nil
	}
}

// ---------------------------------------------------------------------------
// Secret redaction
// ---------------------------------------------------------------------------

// setupSecretKeySuffixes matches configuration keys whose value must never be
// printed. Matching is on the lowercased key: "token" (singular) catches
// sourcegraphToken and oauthAccessToken while leaving promptTokens,
// maxOutputTokens and tokenOptimization untouched.
var setupSecretKeySuffixes = []string{
	"apikey", "api_key", "token", "secret", "password", "passwd",
	"credential", "credentials", "privatekey", "agekeys", "codeverifier",
	"oauthstate",
}

// setupHeaderKeys are maps whose values are credentials even though the header
// names themselves are safe to show.
var setupHeaderKeys = map[string]bool{"headers": true, "extraheaders": true}

// redactSetupValue walks a decoded configuration tree in place and masks every
// secret it finds.
func redactSetupValue(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		if list, isList := v.([]any); isList {
			for _, child := range list {
				redactSetupValue(child)
			}
		}
		return
	}

	for k, child := range m {
		lower := strings.ToLower(k)
		switch {
		case isSetupSecretKey(lower):
			m[k] = maskSetupSecret(child)
		case setupHeaderKeys[lower]:
			m[k] = maskSetupStringMap(child)
		case lower == "env":
			m[k] = maskSetupEnv(child)
		default:
			redactSetupValue(child)
		}
	}
}

func isSetupSecretKey(lower string) bool {
	for _, suffix := range setupSecretKeySuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// maskSetupSecret replaces a secret with a placeholder that still tells the
// agent whether a value is configured, and how it ends.
func maskSetupSecret(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	if strings.TrimSpace(s) == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return "****" + s[len(s)-4:]
}

func maskSetupStringMap(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		out[k] = maskSetupSecret(val)
	}
	return out
}

// maskSetupEnv masks the value half of "NAME=value" entries: MCP servers pass
// credentials through the environment.
func maskSetupEnv(v any) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, 0, len(list))
	for _, item := range list {
		s, isString := item.(string)
		if !isString {
			out = append(out, item)
			continue
		}
		name, value, found := strings.Cut(s, "=")
		if !found || value == "" {
			out = append(out, s)
			continue
		}
		out = append(out, name+"="+fmt.Sprintf("%v", maskSetupSecret(value)))
	}
	return out
}

// ---------------------------------------------------------------------------
// providers
// ---------------------------------------------------------------------------

func runSetupProviders(_ context.Context, _ *pandoSetupTool, args setupArgs) (string, error) {
	accounts := config.GetProviderAccounts()
	includeDisabled := args.Bool("all")

	modelCount := make(map[string]int)
	typeCount := make(map[string]int)
	for _, m := range models.GetAllModels() {
		if m.AccountID != "" {
			modelCount[m.AccountID]++
		}
		typeCount[string(m.Provider)]++
	}

	var sb strings.Builder
	sb.WriteString("## Provider accounts\n\n")

	shown := 0
	for _, acc := range accounts {
		if acc.Disabled && !includeDisabled {
			continue
		}
		shown++

		label := strings.TrimSpace(acc.DisplayName)
		if label == "" {
			label = acc.ID
		}
		// Models are only account-scoped when several accounts share a provider
		// type; otherwise they carry no account id and the type total is the
		// account's own catalogue.
		count := modelCount[acc.ID]
		if count == 0 {
			count = typeCount[string(acc.Type)]
		}

		state := "enabled"
		if acc.Disabled {
			state = "disabled"
		}
		if acc.ReauthRequired {
			state += ", reauth required"
		}

		sb.WriteString(fmt.Sprintf("- %s (%s) — %s\n", acc.ID, acc.Type, label))
		sb.WriteString(fmt.Sprintf("  state: %s | credentials: %s | models: %d\n",
			state, describeSetupCredentials(acc), count))
		if acc.BaseURL != "" {
			sb.WriteString(fmt.Sprintf("  baseUrl: %s\n", acc.BaseURL))
		}
	}

	if shown == 0 {
		if len(accounts) == 0 {
			sb.WriteString("No provider account is configured.\n")
		} else {
			sb.WriteString("Every provider account is disabled. Re-run with --all to list them.\n")
		}
		return sb.String(), nil
	}

	sb.WriteString("\nUse: models --account <id> — to list the model ids of one account.\n")
	return sb.String(), nil
}

// describeSetupCredentials reports which credentials an account holds without
// revealing any of them.
func describeSetupCredentials(acc config.ProviderAccount) string {
	var kinds []string
	if strings.TrimSpace(acc.APIKey) != "" {
		kinds = append(kinds, "api key")
	}
	if strings.TrimSpace(acc.OAuthAccessToken) != "" || strings.TrimSpace(acc.OAuthRefreshToken) != "" || acc.UseOAuth {
		kinds = append(kinds, "oauth")
	}
	if len(kinds) == 0 {
		return "none"
	}
	return strings.Join(kinds, " + ")
}

// ---------------------------------------------------------------------------
// models
// ---------------------------------------------------------------------------

func runSetupModels(_ context.Context, _ *pandoSetupTool, args setupArgs) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(args.String("provider")))
	account := strings.TrimSpace(args.String("account"))
	search := strings.ToLower(strings.TrimSpace(args.String("search")))
	detail := args.Bool("detail")

	limit := 60
	if raw := strings.TrimSpace(args.String("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			return "", fmt.Errorf("--limit expects a non-negative integer, got %q", raw)
		}
		limit = parsed
	}

	all := models.GetAllModels()
	matched := make([]models.Model, 0, len(all))
	for _, m := range all {
		if provider != "" && strings.ToLower(string(m.Provider)) != provider {
			continue
		}
		if account != "" && m.AccountID != account {
			continue
		}
		if search != "" && !setupModelMatches(m, search) {
			continue
		}
		matched = append(matched, m)
	}

	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })

	var sb strings.Builder
	sb.WriteString("## Available models\n\n")
	if len(matched) == 0 {
		sb.WriteString("No model matches the filters. Run \"providers\" to check the configured accounts.\n")
		return sb.String(), nil
	}

	truncated := 0
	if limit > 0 && len(matched) > limit {
		truncated = len(matched) - limit
		matched = matched[:limit]
	}

	for _, m := range matched {
		if detail {
			sb.WriteString(renderSetupModelDetail(m))
			continue
		}
		sb.WriteString(fmt.Sprintf("- %s — %s (%s)\n", m.ID, m.Name, m.Provider))
	}

	if truncated > 0 {
		sb.WriteString(fmt.Sprintf("\n… %d more hidden. Narrow with --provider/--search or raise --limit.\n", truncated))
	}
	if !detail {
		sb.WriteString("\nIds are usable as-is wherever a model is selected (subagents included). Add --detail for cost and limits.\n")
	}
	return sb.String(), nil
}

func setupModelMatches(m models.Model, search string) bool {
	return strings.Contains(strings.ToLower(string(m.ID)), search) ||
		strings.Contains(strings.ToLower(m.Name), search) ||
		strings.Contains(strings.ToLower(m.Description), search)
}

func renderSetupModelDetail(m models.Model) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("- %s — %s (%s)\n", m.ID, m.Name, m.Provider))

	facts := []string{}
	if m.ContextWindow > 0 {
		facts = append(facts, fmt.Sprintf("context %s", formatSetupTokens(m.ContextWindow)))
	}
	if m.DefaultMaxTokens > 0 {
		facts = append(facts, fmt.Sprintf("max output %s", formatSetupTokens(m.DefaultMaxTokens)))
	}
	if m.CostPer1MIn > 0 || m.CostPer1MOut > 0 {
		facts = append(facts, fmt.Sprintf("cost $%.2f in / $%.2f out per 1M", m.CostPer1MIn, m.CostPer1MOut))
	} else {
		facts = append(facts, "cost unknown")
	}
	if m.CanReason {
		facts = append(facts, "reasoning")
	}
	if m.SupportsAttachments {
		facts = append(facts, "attachments")
	}
	sb.WriteString("  " + strings.Join(facts, " | ") + "\n")

	var meta []string
	if m.Knowledge != "" {
		meta = append(meta, "knowledge "+m.Knowledge)
	}
	if m.ReleaseDate != "" {
		meta = append(meta, "released "+m.ReleaseDate)
	}
	if m.AccountID != "" {
		meta = append(meta, "account "+m.AccountID)
	}
	if len(meta) > 0 {
		sb.WriteString("  " + strings.Join(meta, " | ") + "\n")
	}
	if m.Description != "" {
		sb.WriteString("  " + collapseSetupWhitespace(m.Description) + "\n")
	}
	return sb.String()
}

func formatSetupTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	default:
		return strconv.FormatInt(n, 10)
	}
}

func collapseSetupWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ---------------------------------------------------------------------------
// model
// ---------------------------------------------------------------------------

func runSetupModel(ctx context.Context, t *pandoSetupTool, args setupArgs) (string, error) {
	if t.bridge == nil {
		return "", fmt.Errorf("model selection is unavailable in this runtime")
	}
	sessionID, _ := GetContextValues(ctx)
	if sessionID == "" {
		return "", fmt.Errorf("no session is attached to this call")
	}

	target := strings.TrimSpace(args.Positional(0))
	clear := args.Bool("clear")
	if clear && target != "" {
		return "", fmt.Errorf("--clear takes no model id: it restores the configured model")
	}

	if target == "" && !clear {
		state, err := t.bridge.CurrentModel(sessionID)
		if err != nil {
			return "", err
		}
		return renderSetupCurrentModel(state), nil
	}

	// Clearing goes back to a model the user already chose, so it never needs a
	// cost confirmation.
	switched, err := t.bridge.SetSessionModel(sessionID, target, clear || args.Bool("confirm"))
	if err != nil {
		return "", err
	}
	return renderSetupModelSwitch(switched), nil
}

func renderSetupCurrentModel(state SetupModelState) string {
	var sb strings.Builder
	sb.WriteString("## Session model\n\n")
	sb.WriteString("- " + describeSetupModelState(state) + "\n")
	if state.Overridden {
		sb.WriteString("- source: session override (this session only, lost on restart)\n")
		sb.WriteString("- restore the configured model with: model --clear\n")
	} else {
		sb.WriteString("- source: the agent's configured model\n")
	}
	sb.WriteString("\nRun \"models\" for the ids you can switch to.\n")
	return sb.String()
}

func renderSetupModelSwitch(s SetupModelSwitch) string {
	var sb strings.Builder

	if !s.Applied {
		sb.WriteString("## Confirmation required — the model was NOT changed\n\n")
		sb.WriteString(fmt.Sprintf("- current: %s\n", describeSetupModelState(s.Previous)))
		sb.WriteString(fmt.Sprintf("- target:  %s\n", describeSetupModelState(s.Target)))
		sb.WriteString("\n" + s.ConfirmationReason + "\n")
		sb.WriteString("\nAsk the user whether to switch. Only if the user accepts, repeat this call as:\n")
		sb.WriteString(fmt.Sprintf("  pando_setup model %s --confirm\n", s.Target.ID))
		sb.WriteString("\nDo not confirm on the user's behalf, and do not report the model as changed:\n")
		sb.WriteString("this session is still running on " + s.Previous.ID + ".\n")
		return sb.String()
	}

	if s.Cleared {
		sb.WriteString("## Session model override cleared\n\n")
		sb.WriteString(fmt.Sprintf("- back to the configured model: %s\n", describeSetupModelState(s.Target)))
		sb.WriteString("\nIt takes effect on your next request.\n")
		return sb.String()
	}

	sb.WriteString("## Session model changed\n\n")
	sb.WriteString(fmt.Sprintf("- from: %s\n", describeSetupModelState(s.Previous)))
	sb.WriteString(fmt.Sprintf("- to:   %s\n", describeSetupModelState(s.Target)))
	sb.WriteString("\nIt takes effect on your next request and applies to this session only; the\n")
	sb.WriteString("configured model is untouched and the override is lost when Pando restarts\n")
	sb.WriteString("(editors connected over ACP restore it with the session).\n")
	sb.WriteString("Restore the configured model with: model --clear\n")
	return sb.String()
}

// describeSetupModelState renders one model as a single line: id, name, provider
// and price. Unknown prices are said to be unknown, never printed as zero.
func describeSetupModelState(state SetupModelState) string {
	if state.ID == "" {
		return "none"
	}
	line := state.ID
	if state.Name != "" {
		line += " — " + state.Name
	}
	if state.Provider != "" {
		line += " (" + state.Provider + ")"
	}
	if state.CostKnown {
		line += fmt.Sprintf(" | $%.2f in / $%.2f out per 1M", state.CostIn, state.CostOut)
	} else {
		line += " | price unknown"
	}
	if state.ContextWindow > 0 {
		line += " | context " + formatSetupTokens(state.ContextWindow)
	}
	return line
}

// ---------------------------------------------------------------------------
// lsp
// ---------------------------------------------------------------------------

func runSetupLSP(_ context.Context, t *pandoSetupTool, args setupArgs) (string, error) {
	catalog, ok := t.lsp.(SetupLSPCatalog)
	if t.lsp == nil || !ok {
		return "", fmt.Errorf("language server state is unavailable in this runtime")
	}
	servers := catalog.LSPCatalog()
	if len(servers) == 0 {
		return "", fmt.Errorf("no language server is known to this instance")
	}

	if name := strings.TrimSpace(args.Positional(0)); name != "" {
		for _, s := range servers {
			if strings.EqualFold(s.Name, name) {
				return renderSetupLSPDetail(s), nil
			}
		}
		return "", fmt.Errorf("unknown language server %q. Run \"lsp --all\" to list the catalogue", name)
	}

	all := args.Bool("all")
	missingOnly := args.Bool("missing")

	var sb strings.Builder
	sb.WriteString("## Language servers\n\n")
	sb.WriteString(renderSetupLSPActivation())
	sb.WriteString("\n")

	shown := 0
	hidden := 0
	for _, s := range servers {
		if missingOnly && s.Availability != "manual" {
			continue
		}
		// The default view is what matters right now: servers the user
		// configured, servers already running, and servers ready to start.
		interesting := s.Configured || s.RunState != "stopped" || s.Availability == "installed"
		if !all && !missingOnly && !interesting {
			hidden++
			continue
		}
		sb.WriteString(renderSetupLSPLine(s))
		shown++
	}

	if shown == 0 {
		sb.WriteString("No language server matches the filters.\n")
	}
	if hidden > 0 {
		sb.WriteString(fmt.Sprintf("\n… %d more in the catalogue (run \"lsp --all\").\n", hidden))
	}
	return sb.String(), nil
}

// renderSetupLSPActivation summarizes the global on-demand knobs.
func renderSetupLSPActivation() string {
	a := config.Get().LSPActivationSettings()
	state := "on-demand activation: " + a.ActivateOn
	if !a.AutoActivate {
		state = "on-demand activation: off (LSPAutoActivate = false)"
	}
	install := "auto-install: on"
	if !a.AutoInstall {
		install = "auto-install: off"
	}
	return fmt.Sprintf("%s · %s · runner: %s · startup wait %s · install wait %s\n",
		state, install, a.Runner, a.StartupTimeout, a.InstallTimeout)
}

func renderSetupLSPLine(s LSPCatalogEntry) string {
	labels := []string{s.AvailabilityLabel}
	if s.Installing {
		labels = append(labels, "installing")
	} else if s.RunState != "stopped" {
		labels = append(labels, s.RunState)
	}
	if s.Configured {
		labels = append(labels, "configured")
	}
	if s.OptIn {
		labels = append(labels, "opt-in")
	}
	if s.Disabled {
		labels = append(labels, "disabled")
	}

	line := fmt.Sprintf("- %s [%s]", s.Name, strings.Join(labels, ", "))
	if files := setupLSPFiles(s); files != "" {
		line += " — " + files
	}
	if s.Availability == "manual" && s.Hint != "" {
		line += fmt.Sprintf("\n    install: %s", s.Hint)
	}
	return line + "\n"
}

func renderSetupLSPDetail(s LSPCatalogEntry) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s\n\n", s.Name))
	if s.Description != "" {
		sb.WriteString(s.Description + "\n\n")
	}
	sb.WriteString(fmt.Sprintf("- command: %s\n", s.Command))
	if files := setupLSPFiles(s); files != "" {
		sb.WriteString(fmt.Sprintf("- handles: %s\n", files))
	}
	sb.WriteString(fmt.Sprintf("- availability: %s\n", s.AvailabilityLabel))
	sb.WriteString(fmt.Sprintf("- state: %s\n", s.RunState))
	if s.Installing {
		sb.WriteString("- an install is in flight\n")
	}
	sb.WriteString(fmt.Sprintf("- configured explicitly: %t\n", s.Configured))
	if s.OptIn {
		sb.WriteString("- opt-in: add it under [LSP." + s.Name + "] to enable it\n")
	}
	if s.Disabled {
		sb.WriteString("- disabled in the configuration\n")
	}
	if s.Reason != "" {
		sb.WriteString(fmt.Sprintf("- %s\n", s.Reason))
	}
	if s.Hint != "" {
		sb.WriteString(fmt.Sprintf("- manual install: %s\n", s.Hint))
	}
	return sb.String()
}

func setupLSPFiles(s LSPCatalogEntry) string {
	parts := append([]string{}, s.Languages...)
	parts = append(parts, s.Filenames...)
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// session
// ---------------------------------------------------------------------------

func runSetupSession(ctx context.Context, t *pandoSetupTool, _ setupArgs) (string, error) {
	if t.bridge == nil {
		return "", fmt.Errorf("session state is unavailable in this runtime")
	}
	sessionID, _ := GetContextValues(ctx)
	if sessionID == "" {
		return "", fmt.Errorf("no session is attached to this call")
	}

	info, err := t.bridge.SessionInfo(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to read session usage: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("## Session\n\n")
	if info.Title != "" {
		sb.WriteString(fmt.Sprintf("- title: %s\n", info.Title))
	}
	sb.WriteString(fmt.Sprintf("- id: %s\n", info.ID))
	if info.Model != "" {
		sb.WriteString(fmt.Sprintf("- model: %s\n", info.Model))
	}
	sb.WriteString(fmt.Sprintf("- messages: %d\n", info.MessageCount))

	// The session record keeps the last turn's counters, not a running total, so
	// label them as such: reporting them as session totals would be a lie.
	sb.WriteString("\n### Tokens (last turn)\n\n")
	sb.WriteString(fmt.Sprintf("- prompt: %d (input + cache writes)\n", info.PromptTokens))
	sb.WriteString(fmt.Sprintf("- completion: %d (output + cache reads)\n", info.CompletionTokens))
	sb.WriteString(fmt.Sprintf("- cache read: %d\n", info.CacheReadTokens))
	sb.WriteString(fmt.Sprintf("- cache write: %d\n", info.CacheCreationTokens))
	if info.ReasoningTokens > 0 {
		sb.WriteString(fmt.Sprintf("- reasoning: %d\n", info.ReasoningTokens))
	}
	if info.ContextWindow > 0 {
		sb.WriteString(fmt.Sprintf("- context window: %s (~%d used, %.0f%%)\n",
			formatSetupTokens(info.ContextWindow), info.PromptTokens,
			float64(info.PromptTokens)/float64(info.ContextWindow)*100))
	}
	sb.WriteString(fmt.Sprintf("\n- cost so far: $%.4f (accumulated over the session)\n", info.Cost))

	modes := t.bridge.SessionModes(sessionID)
	if len(modes) > 0 {
		sb.WriteString("\n### Modes\n\n")
		for _, m := range modes {
			sb.WriteString(fmt.Sprintf("- %-12s %s\n", m.Name, m.State))
		}
	}
	return sb.String(), nil
}

// ---------------------------------------------------------------------------
// commands / run
// ---------------------------------------------------------------------------

func runSetupCommands(_ context.Context, t *pandoSetupTool, args setupArgs) (string, error) {
	if t.bridge == nil {
		return "", fmt.Errorf("slash commands are unavailable in this runtime")
	}

	all := t.bridge.Commands()
	includeCustom := args.Bool("all")

	var runnable, blocked, custom []SetupRunnableCommand
	for _, c := range all {
		switch {
		case strings.Contains(c.Name, ":"):
			custom = append(custom, c)
		case c.Runnable():
			runnable = append(runnable, c)
		default:
			blocked = append(blocked, c)
		}
	}

	var sb strings.Builder
	sb.WriteString("## Slash commands\n\n")
	sb.WriteString("### Runnable with \"run\"\n\n")
	for _, c := range runnable {
		sb.WriteString(fmt.Sprintf("- %s", c.Name))
		if c.Args != "" {
			sb.WriteString(" " + c.Args)
		}
		sb.WriteString(" — " + c.Summary + "\n")
	}

	if includeCustom {
		sb.WriteString("\n### Custom (user/project)\n\n")
		if len(custom) == 0 {
			sb.WriteString("None defined.\n")
		}
		for _, c := range custom {
			sb.WriteString(fmt.Sprintf("- %s — %s\n", c.Name, c.Summary))
		}
	} else if len(custom) > 0 {
		sb.WriteString(fmt.Sprintf("\n%d custom user/project commands available — list them with --all.\n", len(custom)))
	}

	if len(blocked) > 0 {
		sb.WriteString("\n### User-only\n\n")
		for _, c := range blocked {
			sb.WriteString(fmt.Sprintf("- %s — %s (%s)\n", c.Name, c.Summary, c.Reason))
		}
	}
	return sb.String(), nil
}

func runSetupRun(ctx context.Context, t *pandoSetupTool, args setupArgs) (string, error) {
	if t.bridge == nil {
		return "", fmt.Errorf("slash commands are unavailable in this runtime")
	}

	name := strings.TrimSpace(args.Positional(0))
	if name == "" {
		return "", fmt.Errorf("run expects a command name. Run \"commands\" to list them")
	}
	name = strings.TrimPrefix(name, "/")

	sessionID, _ := GetContextValues(ctx)
	if sessionID == "" {
		return "", fmt.Errorf("no session is attached to this call")
	}

	commandArgs := strings.TrimSpace(strings.Join(args.PositionalsFrom(1), " "))
	out, err := t.bridge.RunCommand(ctx, sessionID, name, commandArgs)
	if err != nil {
		return "", err
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Argument parsing
// ---------------------------------------------------------------------------

// setupArgs is a parsed CLI-style argument string: "--flag value" / "--flag=value"
// pairs plus positional words. A flag with no value is a boolean.
type setupArgs struct {
	flags       map[string]string
	positionals []string
}

func (a setupArgs) String(name string) string { return a.flags[name] }

func (a setupArgs) Bool(name string) bool {
	v, ok := a.flags[name]
	if !ok {
		return false
	}
	return v == "" || strings.EqualFold(v, "true") || v == "1"
}

func (a setupArgs) Positional(i int) string {
	if i < 0 || i >= len(a.positionals) {
		return ""
	}
	return a.positionals[i]
}

func (a setupArgs) PositionalsFrom(i int) []string {
	if i < 0 || i >= len(a.positionals) {
		return nil
	}
	return a.positionals[i:]
}

// setupBooleanFlags are the flags that never take a value. Declaring them is
// what makes "--detail gopls" parse as a boolean plus a positional instead of
// swallowing the word as the flag's value.
var setupBooleanFlags = map[string]bool{
	"help": true, "h": true, "detail": true, "all": true,
	"confirm": true, "clear": true,
}

// parseSetupArgs tokenizes a CLI-style argument string. Quotes group words, and
// "--flag value" is a pair unless the flag is boolean or the value itself starts
// with "-".
func parseSetupArgs(raw string) setupArgs {
	args := setupArgs{flags: map[string]string{}}
	tokens := tokenizeSetupArgs(raw)

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if !strings.HasPrefix(token, "-") || token == "-" || token == "--" {
			args.positionals = append(args.positionals, token)
			continue
		}

		name := strings.TrimLeft(token, "-")
		if name, value, found := strings.Cut(name, "="); found {
			args.flags[strings.ToLower(name)] = value
			continue
		}

		name = strings.ToLower(name)
		if !setupBooleanFlags[name] && i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
			args.flags[name] = tokens[i+1]
			i++
			continue
		}
		args.flags[name] = ""
	}
	return args
}

// tokenizeSetupArgs splits on whitespace, honouring single and double quotes.
func tokenizeSetupArgs(raw string) []string {
	var (
		tokens  []string
		current strings.Builder
		quote   rune
	)
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	for _, r := range raw {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return tokens
}
