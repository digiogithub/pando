package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools/shell"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mesnada/acp"
	"github.com/digiogithub/pando/internal/permission"
	"github.com/digiogithub/pando/internal/runtime"
	"github.com/digiogithub/pando/internal/safety"
	"github.com/digiogithub/pando/internal/sandbox"
	"github.com/digiogithub/pando/internal/savings"
)

// getRuntimeResolver extracts a RuntimeResolver from the context if one was
// injected (Phase 2+). Returns nil when running on the default host path.
func getRuntimeResolver(ctx context.Context) runtime.RuntimeResolver {
	if v := ctx.Value(RuntimeResolverContextKey); v != nil {
		if r, ok := v.(runtime.RuntimeResolver); ok {
			return r
		}
	}
	return runtime.NewResolver()
}

type BashParams struct {
	Command   string `json:"command"`
	Timeout   int    `json:"timeout"`
	HeadLimit int    `json:"head_limit"` // Max output lines to return (0 = no limit)
	TailLines int    `json:"tail_lines"` // Only return last N lines (0 = no limit)
	// SandboxPermissions is SandboxPermissionsDefault (or empty) or
	// SandboxPermissionsEscalated to ask the user to run this one command
	// outside the host sandbox. A no-op when the sandbox is not active.
	SandboxPermissions string `json:"sandbox_permissions,omitempty"`
	// Justification explains an escalation; required with
	// SandboxPermissionsEscalated while the sandbox is active.
	Justification string `json:"justification,omitempty"`
}

// Values of BashParams.SandboxPermissions.
const (
	SandboxPermissionsDefault   = "use_default"
	SandboxPermissionsEscalated = "require_escalated"
)

type BashPermissionsParams struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
	// Justification is the agent's reason for an escalation request.
	Justification string `json:"justification,omitempty"`
	// Unsandboxed is true for an escalation request: the command would run
	// outside the host sandbox.
	Unsandboxed bool `json:"unsandboxed,omitempty"`
}

type BashResponseMetadata struct {
	StartTime  int64 `json:"start_time"`
	EndTime    int64 `json:"end_time"`
	TotalLines int   `json:"total_lines"`
	Truncated  bool  `json:"truncated"`
	// OutputFilter is the name of the RTK-style filter or native parser that
	// compressed the command output, empty when no compression was applied.
	OutputFilter string `json:"output_filter,omitempty"`
	// OutputFilterCharsBefore / OutputFilterCharsAfter record the byte sizes
	// before and after compression so the UI can show the token/char savings.
	OutputFilterCharsBefore int `json:"output_filter_chars_before,omitempty"`
	OutputFilterCharsAfter  int `json:"output_filter_chars_after,omitempty"`
	// SandboxBackend is the host sandbox backend that confined the shell
	// (e.g. "landlock+seccomp", "seatbelt"); empty when the command did not
	// run in the host shell or the sandbox policy is off.
	SandboxBackend string `json:"sandbox_backend,omitempty"`
	// SandboxMode is the sandbox mode of the shell's policy
	// (workspace-write, read-only, strict); empty when off or not applicable.
	SandboxMode string `json:"sandbox_mode,omitempty"`
	// ApprovedBySandbox is true when the permission prompt was skipped
	// because the command ran confined by an enforced sandbox.
	ApprovedBySandbox bool `json:"approved_by_sandbox,omitempty"`
	// SandboxDenied is true when the command failed and its output looks like
	// the sandbox blocked it (see sandbox.Classify); the tool output then
	// carries a "[sandbox]" hint for the model.
	SandboxDenied bool `json:"sandbox_denied,omitempty"`
	// SandboxDenialKind is "fs" or "net" when SandboxDenied.
	SandboxDenialKind string `json:"sandbox_denial_kind,omitempty"`
	// SandboxDenialPath is the blocked path, when one was identified.
	SandboxDenialPath string `json:"sandbox_denial_path,omitempty"`
	// Unsandboxed is true when the user approved an escalation and the
	// command ran once outside the sandbox.
	Unsandboxed bool `json:"unsandboxed,omitempty"`
}
type bashTool struct {
	permissions permission.Service
}

const (
	BashToolName = "bash"

	DefaultTimeout  = 1 * 60 * 1000  // 1 minutes in milliseconds
	MaxTimeout      = 10 * 60 * 1000 // 10 minutes in milliseconds
	MaxOutputLength = 30000
)

var defaultBannedCommands = []string{
	"alias", "curl", "curlie", "wget", "axel", "aria2c",
	"nc", "telnet", "lynx", "w3m", "links", "httpie", "xh",
	"http-prompt", "chrome", "firefox", "safari",
}

// effectiveBannedCommands returns the banned commands list, taking into account
// the user configuration: BannedCommands replaces the default list entirely if set,
// and AllowedCommands removes specific entries from the default list.
func effectiveBannedCommands() []string {
	cfg := config.Get()
	if cfg == nil {
		return defaultBannedCommands
	}

	// If the user explicitly set a custom banned list, use it as-is.
	if len(cfg.Bash.BannedCommands) > 0 {
		return cfg.Bash.BannedCommands
	}

	// Otherwise start from the default list and remove anything the user allowed.
	if len(cfg.Bash.AllowedCommands) == 0 {
		return defaultBannedCommands
	}

	allowed := make(map[string]struct{}, len(cfg.Bash.AllowedCommands))
	for _, cmd := range cfg.Bash.AllowedCommands {
		allowed[strings.ToLower(cmd)] = struct{}{}
	}

	result := make([]string, 0, len(defaultBannedCommands))
	for _, cmd := range defaultBannedCommands {
		if _, ok := allowed[strings.ToLower(cmd)]; !ok {
			result = append(result, cmd)
		}
	}
	return result
}

// bannedCommands is kept for backward compatibility with bashDescription.
var bannedCommands = defaultBannedCommands

var safeReadOnlyCommands = []string{
	"ls", "echo", "pwd", "date", "cal", "uptime", "whoami", "id", "groups", "env", "printenv", "set", "unset", "which", "type", "whereis",
	"whatis", "uname", "hostname", "df", "du", "free", "top", "ps", "kill", "killall", "nice", "nohup", "time", "timeout",

	"git status", "git log", "git diff", "git show", "git branch", "git tag", "git remote", "git ls-files", "git ls-remote",
	"git rev-parse", "git config --get", "git config --list", "git describe", "git blame", "git grep", "git shortlog",

	"go version", "go help", "go list", "go env", "go doc", "go vet", "go fmt", "go mod", "go test", "go build", "go run", "go install", "go clean",
}

func bashDescription() string {
	bannedCommandsStr := strings.Join(effectiveBannedCommands(), ", ")
	return fmt.Sprintf(`Executes a given bash command in a persistent shell session with optional timeout, ensuring proper handling and security measures.

Before executing the command, please follow these steps:

1. Directory Verification:
 - If the command will create new directories or files, first use the LS tool to verify the parent directory exists and is the correct location
 - For example, before running "mkdir foo/bar", first use LS to check that "foo" exists and is the intended parent directory

2. Security Check:
 - For security and to limit the threat of a prompt injection attack, some commands are limited or banned. If you use a disallowed command, you will receive an error message explaining the restriction. Explain the error to the User.
 - Verify that the command is not one of the banned commands: %s.
%s
3. Command Execution:
 - After ensuring proper quoting, execute the command.
 - Capture the output of the command.

4. Output Processing:
 - If the output exceeds %d characters, output will be truncated before being returned to you.
 - Prepare the output for display to the user.

5. Return Result:
 - Provide the processed output of the command.
 - If any errors occurred during execution, include those in the output.

OUTPUT CONTROL:
- head_limit: Return only the first N lines of output (e.g., head_limit=50)
- tail_lines: Return only the last N lines — ideal for logs (e.g., tail_lines=20)
- Large outputs (>300 lines or >15000 chars) are automatically cached in session memory
- When cached, you will see a [Response cached: N lines → cache_id: "XXXX"] header
- Use the cache_read tool to access additional pages of cached output

Usage notes:
- The command argument is required.
- You can specify an optional timeout in milliseconds (up to 600000ms / 10 minutes). If not specified, commands will timeout after 30 minutes.
- VERY IMPORTANT: You MUST avoid using search commands like 'find' and 'grep'. Instead use Grep, Glob, code search tools, or mesnada_spawn_agent for delegated exploration. You MUST avoid read tools like 'cat', 'head', 'tail', and 'ls', and use the View and LS tools to inspect files.
- When issuing multiple commands, use the ';' or '&&' operator to separate them. DO NOT use newlines (newlines are ok in quoted strings).
- IMPORTANT: All commands share the same shell session. Shell state (environment variables, virtual environments, current directory, etc.) persist between commands. For example, if you set an environment variable as part of a command, the environment variable will persist for subsequent commands.
- Try to maintain your current working directory throughout the session by using absolute paths and avoiding usage of 'cd'. You may use 'cd' if the User explicitly requests it.
<good-example>
pytest /foo/bar/tests
</good-example>
<bad-example>
cd /foo/bar && pytest tests
</bad-example>

# Committing changes with git

When the user asks you to create a new git commit, follow these steps carefully:

1. Start with a single message that contains exactly three tool_use blocks that do the following (it is VERY IMPORTANT that you send these tool_use blocks in a single message, otherwise it will feel slow to the user!):
 - Run a git status command to see all untracked files.
 - Run a git diff command to see both staged and unstaged changes that will be committed.
 - Run a git log command to see recent commit messages, so that you can follow this repository's commit message style.

2. Use the git context at the start of this conversation to determine which files are relevant to your commit. Add relevant untracked files to the staging area. Do not commit files that were already modified at the start of this conversation, if they are not relevant to your commit.

3. Analyze all staged changes (both previously staged and newly added) and draft a commit message. Wrap your analysis process in <commit_analysis> tags:

<commit_analysis>
- List the files that have been changed or added
- Summarize the nature of the changes (eg. new feature, enhancement to an existing feature, bug fix, refactoring, test, docs, etc.)
- Brainstorm the purpose or motivation behind these changes
- Do not use tools to explore code, beyond what is available in the git context
- Assess the impact of these changes on the overall project
- Check for any sensitive information that shouldn't be committed
- Draft a concise (1-2 sentences) commit message that focuses on the "why" rather than the "what"
- Ensure your language is clear, concise, and to the point
- Ensure the message accurately reflects the changes and their purpose (i.e. "add" means a wholly new feature, "update" means an enhancement to an existing feature, "fix" means a bug fix, etc.)
- Ensure the message is not generic (avoid words like "Update" or "Fix" without context)
- Review the draft message to ensure it accurately reflects the changes and their purpose
</commit_analysis>

4. Create the commit with a message ending with:
🤖 Generated with pando
Co-Authored-By: pando <noreply@github.com/digiogithub/pando>

- In order to ensure good formatting, ALWAYS pass the commit message via a HEREDOC, a la this example:
<example>
git commit -m "$(cat <<'EOF'
 Commit message here.

 🤖 Generated with pando
 Co-Authored-By: pando <noreply@github.com/digiogithub/pando>
 EOF
 )"
</example>

5. If the commit fails due to pre-commit hook changes, retry the commit ONCE to include these automated changes. If it fails again, it usually means a pre-commit hook is preventing the commit. If the commit succeeds but you notice that files were modified by the pre-commit hook, you MUST amend your commit to include them.

6. Finally, run git status to make sure the commit succeeded.

Important notes:
- When possible, combine the "git add" and "git commit" commands into a single "git commit -am" command, to speed things up
- However, be careful not to stage files (e.g. with 'git add .') for commits that aren't part of the change, they may have untracked files they want to keep around, but not commit.
- NEVER update the git config
- DO NOT push to the remote repository
- IMPORTANT: Never use git commands with the -i flag (like git rebase -i or git add -i) since they require interactive input which is not supported.
- If there are no changes to commit (i.e., no untracked files and no modifications), do not create an empty commit
- Ensure your commit message is meaningful and concise. It should explain the purpose of the changes, not just describe them.
- Return an empty response - the user will see the git output directly

# Creating pull requests
Use the gh command via the Bash tool for ALL GitHub-related tasks including working with issues, pull requests, checks, and releases. If given a Github URL use the gh command to get the information needed.

IMPORTANT: When the user asks you to create a pull request, follow these steps carefully:

1. Understand the current state of the branch. Remember to send a single message that contains multiple tool_use blocks (it is VERY IMPORTANT that you do this in a single message, otherwise it will feel slow to the user!):
 - Run a git status command to see all untracked files.
 - Run a git diff command to see both staged and unstaged changes that will be committed.
 - Check if the current branch tracks a remote branch and is up to date with the remote, so you know if you need to push to the remote
 - Run a git log command and 'git diff main...HEAD' to understand the full commit history for the current branch (from the time it diverged from the 'main' branch.)

2. Create new branch if needed

3. Commit changes if needed

4. Push to remote with -u flag if needed

5. Analyze all changes that will be included in the pull request, making sure to look at all relevant commits (not just the latest commit, but all commits that will be included in the pull request!), and draft a pull request summary. Wrap your analysis process in <pr_analysis> tags:

<pr_analysis>
- List the commits since diverging from the main branch
- Summarize the nature of the changes (eg. new feature, enhancement to an existing feature, bug fix, refactoring, test, docs, etc.)
- Brainstorm the purpose or motivation behind these changes
- Assess the impact of these changes on the overall project
- Do not use tools to explore code, beyond what is available in the git context
- Check for any sensitive information that shouldn't be committed
- Draft a concise (1-2 bullet points) pull request summary that focuses on the "why" rather than the "what"
- Ensure the summary accurately reflects all changes since diverging from the main branch
- Ensure your language is clear, concise, and to the point
- Ensure the summary accurately reflects the changes and their purpose (ie. "add" means a wholly new feature, "update" means an enhancement to an existing feature, "fix" means a bug fix, etc.)
- Ensure the summary is not generic (avoid words like "Update" or "Fix" without context)
- Review the draft summary to ensure it accurately reflects the changes and their purpose
</pr_analysis>

6. Create PR using gh pr create with the format below. Use a HEREDOC to pass the body to ensure correct formatting.
<example>
gh pr create --title "the pr title" --body "$(cat <<'EOF'
## Summary
<1-3 bullet points>

## Test plan
[Checklist of TODOs for testing the pull request...]

🤖 Generated with pando
EOF
)"
</example>

Important:
- Return an empty response - the user will see the gh output directly
- Never update git config`, bannedCommandsStr, bashSandboxDescription(), MaxOutputLength)
}

// usesHostShell reports whether bash commands run in the persistent host
// shell, the only execution path the host sandbox confines. Docker and Podman
// isolate commands themselves, and "auto"/"embedded" may resolve to them, so
// they are not considered sandboxed here (the embedded runtime wraps its own
// commands through sandbox.WrapCmd, but without the auto-approval shortcut).
func usesHostShell(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	rt := strings.TrimSpace(strings.ToLower(cfg.Container.Runtime))
	return rt == "" || rt == string(runtime.RuntimeHost)
}

// bashSandboxDescription is the Security Check bullet describing the host
// sandbox; empty unless commands really run confined (host shell and an
// enforced backend). In ACP mode commands run in the client's terminal and
// the client's own rules apply.
func bashSandboxDescription() string {
	if !usesHostShell(config.Get()) || !sandbox.Active() {
		return ""
	}
	p := sandbox.Current()
	var limits string
	switch p.Mode {
	case sandbox.ModeReadOnly:
		limits = "writes are limited to temp dirs"
	case sandbox.ModeStrict:
		limits = "writes are limited to the workspace and temp dirs, and reads to the workspace and system/toolchain dirs"
	default:
		limits = "writes are limited to the workspace, temp dirs and package caches"
	}
	desc := fmt.Sprintf(" - Commands run inside Pando's sandbox (mode %s): %s; Pando's config/data and git hooks/config stay read-only; credential environment variables (API keys, tokens) are removed", p.Mode, limits)
	if p.RestrictsNetwork() {
		desc += "; network access is blocked"
	}
	return desc + ". A \"Permission denied\", \"Operation not permitted\" or \"Read-only file system\" error outside those limits comes from the sandbox (the output then ends with a [sandbox] note): do not look for workarounds. Prefer a path inside the workspace or a temp dir; if access outside the sandbox is truly required, call bash again with sandbox_permissions: \"require_escalated\" and a one-line justification. The User must approve that, and the command then runs once outside the sandbox, in the current directory but without the persistent shell's state (exports, functions).\n"
}

func NewBashTool(permission permission.Service) BaseTool {
	return &bashTool{
		permissions: permission,
	}
}

func (b *bashTool) Info() ToolInfo {
	return ToolInfo{
		Name:        BashToolName,
		Description: bashDescription(),
		Parameters: map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The command to execute",
			},
			"timeout": map[string]any{
				"type":        "number",
				"description": "Optional timeout in milliseconds (max 600000)",
			},
			"head_limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of output lines to return (0 = no limit, subject to 30000 char cap)",
			},
			"tail_lines": map[string]any{
				"type":        "integer",
				"description": "Return only the last N lines of output (useful for log tails). Overrides head_limit.",
			},
			"sandbox_permissions": map[string]any{
				"type":        "string",
				"enum":        []string{SandboxPermissionsDefault, SandboxPermissionsEscalated},
				"description": "Only meaningful while Pando's host sandbox is active (ignored otherwise). \"require_escalated\" asks the user to run this command once outside the sandbox; use it only after a command failed because of the sandbox and the access is truly needed. Requires justification.",
			},
			"justification": map[string]any{
				"type":        "string",
				"description": "One line explaining why the command must run outside the sandbox. Required with sandbox_permissions \"require_escalated\"; shown to the user in the approval prompt.",
			},
		},
		Required: []string{"command"},
	}
}

func (b *bashTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params BashParams
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse("invalid parameters"), nil
	}

	if params.Timeout > MaxTimeout {
		params.Timeout = MaxTimeout
	} else if params.Timeout <= 0 {
		params.Timeout = DefaultTimeout
	}

	if params.Command == "" {
		return NewTextErrorResponse("missing command"), nil
	}

	escalate := false
	switch strings.TrimSpace(params.SandboxPermissions) {
	case "", SandboxPermissionsDefault:
	case SandboxPermissionsEscalated:
		escalate = true
	default:
		return NewTextErrorResponse(fmt.Sprintf("invalid sandbox_permissions %q: use %q or %q", params.SandboxPermissions, SandboxPermissionsDefault, SandboxPermissionsEscalated)), nil
	}

	logging.Debug("bash tool called", "command", params.Command, "timeout", params.Timeout)

	// Check if we're in ACP context and should use client callbacks
	if acpConn := ctx.Value(ACPClientConnContextKey); acpConn != nil {
		return b.runWithACP(ctx, params, acpConn)
	}

	baseCmd := strings.Fields(params.Command)[0]
	for _, banned := range effectiveBannedCommands() {
		if strings.EqualFold(baseCmd, banned) {
			return NewTextErrorResponse(fmt.Sprintf("command '%s' is not allowed", baseCmd)), nil
		}
	}

	isSafeReadOnly := false
	cmdLower := strings.ToLower(params.Command)

	for _, safe := range safeReadOnlyCommands {
		if strings.HasPrefix(cmdLower, strings.ToLower(safe)) {
			if len(cmdLower) == len(safe) || cmdLower[len(safe)] == ' ' || cmdLower[len(safe)] == '-' {
				isSafeReadOnly = true
				break
			}
		}
	}

	sessionID, messageID := GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a new file")
	}
	isDangerous := false
	if cfg := config.Get(); cfg != nil {
		isDangerous = safety.IsDangerousShellCommand(params.Command, cfg.Goal.DangerousPatterns)
	} else {
		isDangerous = safety.IsDangerousShellCommand(params.Command, nil)
	}
	// The persistent host shell is resolved before the permission check so
	// the auto-approval below is based on the confinement of the very shell
	// that will run the command.
	cfg := config.Get()
	var hostShell *shell.PersistentShell
	var sbx shell.SandboxInfo
	if usesHostShell(cfg) {
		hostShell = shell.GetPersistentShell(config.WorkingDirectory())
		sbx = hostShell.Sandbox()
	}
	// An escalation only means something while the shell is really confined;
	// otherwise the command takes the regular path below.
	if escalate && sbx.Active() {
		return b.runEscalated(ctx, sessionID, params, hostShell, sbx, isDangerous)
	}
	// An enforced sandbox replaces the prompt for ordinary commands; the
	// floor stays: dangerous commands always ask (with explicit approval), and
	// banned commands were rejected above.
	approvedBySandbox := !isSafeReadOnly && !isDangerous && sbx.AutoAllowBash()
	if !isSafeReadOnly && !approvedBySandbox {
		p := b.permissions.Request(
			permission.CreatePermissionRequest{
				SessionID:               sessionID,
				Path:                    config.WorkingDirectory(),
				ToolName:                BashToolName,
				Action:                  "execute",
				Description:             fmt.Sprintf("Execute command: %s", params.Command),
				RequireExplicitApproval: isDangerous,
				Params: BashPermissionsParams{
					Command: params.Command,
				},
			},
		)
		if !p {
			return ToolResponse{}, permission.ErrorPermissionDenied
		}
	}
	logging.Debug("bash executing", "command", params.Command, "isSafeReadOnly", isSafeReadOnly, "approvedBySandbox", approvedBySandbox)
	startTime := time.Now()
	stdout, stderr, exitCode, interrupted, err := b.executeCommand(ctx, sessionID, params, hostShell)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("error executing command: %w", err)
	}

	metadata := BashResponseMetadata{ApprovedBySandbox: approvedBySandbox}
	if sbx.Policy.Covers(sandbox.PurposeBash) {
		metadata.SandboxMode = string(sbx.Policy.Mode)
		metadata.SandboxBackend = sbx.Capability.Backend
	}
	// Only a run that was really confined can be blamed on the sandbox.
	var hint string
	if hostShell != nil && sbx.Active() && exitCode != 0 && !interrupted {
		if d, ok := sandbox.ClassifyAt(exitCode, stdout+"\n"+stderr, sbx.Policy, hostShell.Cwd()); ok {
			metadata.SandboxDenied = true
			metadata.SandboxDenialKind = string(d.Kind)
			metadata.SandboxDenialPath = d.Path
			hint = sandboxDenialHint(d, sbx)
			sandbox.Emit(ctx, sandbox.Event{
				Type:      sandbox.EventDenied,
				SessionID: sessionID,
				Backend:   sbx.Capability.Backend,
				Mode:      string(sbx.Policy.Mode),
				Kind:      string(d.Kind),
				Op:        d.Op,
				Path:      d.Path,
				Command:   params.Command,
			})
		}
	}
	return b.buildResult(params, stdout, stderr, exitCode, interrupted, startTime, metadata, hint), nil
}

// buildResult turns a finished command into the tool response: output
// filtering and truncation, line limits, the error/exit-code trailer and the
// optional [sandbox] hint. metadata carries the caller's sandbox fields; the
// timing and output fields are filled here.
func (b *bashTool) buildResult(params BashParams, stdout, stderr string, exitCode int, interrupted bool, startTime time.Time, metadata BashResponseMetadata, hint string) ToolResponse {
	// RTK-style output compression: map the command to a declarative filter
	// and strip noise before truncation/caching. Fail-safe (returns raw on any
	// issue) and never touches the exit code or stderr handling below.
	filterResult := applyOutputFilter(params.Command, stdout)
	stdout = filterResult.Output
	if filterResult.Name != "" {
		recordSaving(savings.SourceBash, bashSavingDetail(params.Command), filterResult.Name, filterResult.Before, filterResult.After)
	}

	stdout = truncateOutput(stdout)
	stderr = truncateOutput(stderr)

	// Apply line-based limits if requested
	totalLines := countLines(stdout)
	truncated := false
	if params.TailLines > 0 {
		newStdout := tailLines(stdout, params.TailLines)
		if newStdout != stdout {
			truncated = true
		}
		stdout = newStdout
	} else if params.HeadLimit > 0 {
		newStdout := headLines(stdout, params.HeadLimit)
		if newStdout != stdout {
			truncated = true
		}
		stdout = newStdout
	}

	logging.Debug("bash completed", "command", params.Command, "exitCode", exitCode, "interrupted", interrupted, "stdoutLen", len(stdout), "stderrLen", len(stderr))

	errorMessage := stderr
	if interrupted {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += "Command was aborted before completion"
	} else if exitCode != 0 {
		if errorMessage != "" {
			errorMessage += "\n"
		}
		errorMessage += fmt.Sprintf("Exit code %d", exitCode)
	}

	hasBothOutputs := stdout != "" && stderr != ""

	if hasBothOutputs {
		stdout += "\n"
	}

	if errorMessage != "" {
		stdout += "\n" + errorMessage
	}
	if hint != "" {
		stdout += "\n\n" + hint
	}

	metadata.StartTime = startTime.UnixMilli()
	metadata.EndTime = time.Now().UnixMilli()
	metadata.TotalLines = totalLines
	metadata.Truncated = truncated
	metadata.OutputFilter = filterResult.Name
	metadata.OutputFilterCharsBefore = filterResult.Before
	metadata.OutputFilterCharsAfter = filterResult.After
	if stdout == "" {
		return WithResponseMetadata(NewTextResponse("no output"), metadata)
	}
	return WithResponseMetadata(NewTextResponse(stdout), metadata)
}

// runEscalated handles sandbox_permissions "require_escalated" while the
// shell is confined: it asks for an execute_unsandboxed permission and, when
// granted, runs the command once outside the sandbox in the shell's current
// directory (not in the persistent shell, whose state is not shared).
//
// Unless the policy allows auto-escalation (and the command is not
// dangerous), the request is NeverAutoApprove
// with RequireExplicitApproval: per-session and global auto-approve (auto,
// yolo, goal/autopilot, headless modes) never grant it, and an "allow for
// session" answer is stored scoped to escalationGrantKey (the command prefix),
// so it only covers later escalations of the same command prefix.
func (b *bashTool) runEscalated(ctx context.Context, sessionID string, params BashParams, hostShell *shell.PersistentShell, sbx shell.SandboxInfo, isDangerous bool) (ToolResponse, error) {
	justification := strings.TrimSpace(params.Justification)
	if justification == "" {
		return NewTextErrorResponse(`justification is required with sandbox_permissions "require_escalated": explain in one line why the command must run outside the sandbox`), nil
	}
	// Dangerous commands keep the floor even with auto-escalation.
	auto := sbx.Policy.AllowAutoEscalation && !isDangerous
	grantKey := escalationGrantKey(params.Command)

	sandbox.Emit(ctx, sandbox.Event{
		Type:        sandbox.EventEscalationRequested,
		SessionID:   sessionID,
		Backend:     sbx.Capability.Backend,
		Mode:        string(sbx.Policy.Mode),
		Command:     params.Command,
		Reason:      justification,
		AutoAllowed: auto,
	})
	granted := b.permissions.RequestWithContext(ctx, permission.CreatePermissionRequest{
		SessionID:               sessionID,
		Path:                    config.WorkingDirectory(),
		ToolName:                BashToolName,
		Action:                  permission.ActionExecuteUnsandboxed,
		Description:             escalationDescription(params.Command, justification, grantKey),
		RequireExplicitApproval: !auto,
		NeverAutoApprove:        !auto,
		Justification:           justification,
		GrantKey:                grantKey,
		Params: BashPermissionsParams{
			Command:       params.Command,
			Timeout:       params.Timeout,
			Justification: justification,
			Unsandboxed:   true,
		},
	})
	if !granted {
		sandbox.Emit(ctx, sandbox.Event{
			Type:      sandbox.EventEscalationDenied,
			SessionID: sessionID,
			Backend:   sbx.Capability.Backend,
			Command:   params.Command,
		})
		return ToolResponse{}, permission.ErrorPermissionDenied
	}
	sandbox.Emit(ctx, sandbox.Event{
		Type:      sandbox.EventEscalationGranted,
		SessionID: sessionID,
		Backend:   sbx.Capability.Backend,
		Command:   params.Command,
	})

	cwd := hostShell.Cwd()
	if cwd == "" {
		cwd = config.WorkingDirectory()
	}
	startTime := time.Now()
	stdout, stderr, exitCode, interrupted, err := shell.ExecUnsandboxed(ctx, cwd, params.Command, params.Timeout)
	if err != nil {
		return ToolResponse{}, fmt.Errorf("error executing command: %w", err)
	}
	return b.buildResult(params, stdout, stderr, exitCode, interrupted, startTime, BashResponseMetadata{Unsandboxed: true}, ""), nil
}

// escalationDescription is the permission prompt text of an escalation.
func escalationDescription(command, justification, grantKey string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Run outside sandbox: %s\nJustification: %s\n", command, justification)
	sb.WriteString("The command runs once without the sandbox, with full access to your files, network and environment (credentials included).")
	if prefix, ok := strings.CutPrefix(grantKey, grantKeyPrefix); ok {
		fmt.Fprintf(&sb, "\n\"Allow for session\" covers later escalations of commands starting with: %s", prefix)
	} else {
		sb.WriteString("\n\"Allow for session\" covers only this exact command.")
	}
	return sb.String()
}

// Grant key tags of escalationGrantKey.
const (
	grantKeyPrefix = "prefix:"
	grantKeyExact  = "exact:"
)

// escalationGrantKey scopes an "allow for session" escalation grant. A simple
// command (plain words, no shell syntax) is keyed by its first two words
// ("npm install", "go test"), or its only word; anything with shell syntax,
// quoting, globs, variables, redirections or a flag in the second position is
// keyed by the exact command. A request only matches a grant with the same
// key, so a prefix grant never covers a command with shell syntax.
func escalationGrantKey(command string) string {
	c := strings.TrimSpace(command)
	if strings.ContainsAny(c, ";&|<>`$(){}[]*?!#~=\\'\"\n\r\t") {
		return grantKeyExact + c
	}
	fields := strings.Fields(c)
	if len(fields) >= 2 && strings.HasPrefix(fields[1], "-") {
		return grantKeyExact + strings.Join(fields, " ")
	}
	if len(fields) > 2 {
		fields = fields[:2]
	}
	return grantKeyPrefix + strings.Join(fields, " ")
}

// sandboxDenialHint is the "[sandbox]" note appended to the output of a
// command the sandbox most likely blocked.
func sandboxDenialHint(d sandbox.Denial, sbx shell.SandboxInfo) string {
	var what string
	switch {
	case d.Kind == sandbox.DenialNet:
		what = "network access (network: restricted)"
	case d.Op == sandbox.OpRead && d.Path != "":
		what = "reading " + d.Path
	case d.Path != "":
		what = "a write to " + d.Path
	case d.Op == sandbox.OpRead:
		what = "a read outside the readable directories"
	default:
		what = "a write outside the writable directories"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[sandbox] This command likely failed because Pando's sandbox (mode %s, backend %s) blocked %s. Evidence: %s", sbx.Policy.Mode, sbx.Capability.Backend, what, d.Evidence)
	if d.WorkspaceRootEntry && sbx.Capability.Backend == sandbox.BackendLandlock {
		sb.WriteString("\nWith the Landlock-only backend (bubblewrap is not available), new files and directories cannot be created directly in the workspace root, because it contains protected entries (.pando, .pando.toml). Create it in a subdirectory instead, or ask the user to install bubblewrap (bwrap).")
	}
	if d.Kind == sandbox.DenialNet {
		sb.WriteString("\nNetwork access is blocked for commands in this sandbox mode.")
	} else {
		sb.WriteString("\nIf possible, use a path inside the workspace or a temp dir instead.")
	}
	sb.WriteString(" If access outside the sandbox is truly needed, call bash again with sandbox_permissions: \"require_escalated\" and a one-line justification; the user will be asked to approve running the command once outside the sandbox.")
	return sb.String()
}

// executeCommand runs the command in hostShell when given (the host runtime),
// or through the configured container runtime otherwise.
func (b *bashTool) executeCommand(ctx context.Context, sessionID string, params BashParams, hostShell *shell.PersistentShell) (string, string, int, bool, error) {
	cfg := config.Get()
	if cfg == nil {
		return "", "", 0, false, fmt.Errorf("config not loaded")
	}

	containerCfg := cfg.Container
	if usesHostShell(cfg) {
		if hostShell == nil {
			hostShell = shell.GetPersistentShell(config.WorkingDirectory())
		}
		return hostShell.Exec(ctx, params.Command, params.Timeout)
	}

	resolver := getRuntimeResolver(ctx)
	if resolver == nil {
		return "", "", 0, false, fmt.Errorf("runtime resolver not available")
	}

	executionRuntime, _, err := resolver.Resolve(containerCfg)
	if err != nil {
		return "", "", 0, false, err
	}

	entry, err := runtime.DefaultSessionManager().GetOrCreate(ctx, sessionID, config.WorkingDirectory(), executionRuntime)
	if err != nil {
		runtime.DefaultSessionManager().RecordEvent(runtime.ContainerEvent{
			SessionID:   sessionID,
			RuntimeType: executionRuntime.Type(),
			Event:       "error",
			Details:     err.Error(),
		})
		return "", "", 0, false, err
	}

	result, err := entry.Runtime.Exec(ctx, sessionID, params.Command, nil)
	if err != nil {
		runtime.DefaultSessionManager().RecordEvent(runtime.ContainerEvent{
			SessionID:   sessionID,
			RuntimeType: entry.RuntimeType,
			ContainerID: entry.ContainerID,
			Event:       "error",
			Details:     err.Error(),
		})
		return "", "", 0, false, err
	}
	runtime.DefaultSessionManager().RecordEvent(runtime.ContainerEvent{
		SessionID:   sessionID,
		RuntimeType: entry.RuntimeType,
		ContainerID: entry.ContainerID,
		Event:       "exec",
		Details:     params.Command,
	})
	return result.Stdout, result.Stderr, result.ExitCode, result.Interrupted, nil
}

// headLines returns the first n lines of s.
func headLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n >= len(lines) {
		return s
	}
	return strings.Join(lines[:n], "\n") + fmt.Sprintf("\n\n... [output truncated at %d lines, %d more lines omitted] ...", n, len(lines)-n)
}

// tailLines returns the last n lines of s.
func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n >= len(lines) {
		return s
	}
	omitted := len(lines) - n
	return fmt.Sprintf("... [first %d lines omitted, showing last %d lines] ...\n\n", omitted, n) + strings.Join(lines[len(lines)-n:], "\n")
}

func truncateOutput(content string) string {
	if len(content) <= MaxOutputLength {
		return content
	}

	halfLength := MaxOutputLength / 2
	start := content[:halfLength]
	end := content[len(content)-halfLength:]

	truncatedLinesCount := countLines(content[halfLength : len(content)-halfLength])
	return fmt.Sprintf("%s\n\n... [%d lines truncated] ...\n\n%s", start, truncatedLinesCount, end)
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

// runWithACP handles command execution via ACP client callback. The command
// runs in the client's terminal (Zed, VS Code, ...), so Pando's host sandbox
// does not apply: the client owns that host and its own rules.
func (b *bashTool) runWithACP(ctx context.Context, params BashParams, acpConnInterface interface{}) (ToolResponse, error) {
	acpConn, ok := acpConnInterface.(*acp.ACPClientConnection)
	if !ok {
		return ToolResponse{}, fmt.Errorf("invalid ACP client connection type")
	}

	logging.Debug("bash tool using ACP callback", "command", params.Command)

	// Parse command into command and args
	// Simple parsing - split by spaces (this could be improved for quoted args)
	parts := strings.Fields(params.Command)
	if len(parts) == 0 {
		return NewTextErrorResponse("empty command"), nil
	}

	command := parts[0]
	args := parts[1:]

	// Use the working directory from the connection
	cwd := acpConn.GetWorkDir()

	startTime := time.Now()

	// Create terminal on client
	terminalID, err := acpConn.CreateTerminal(ctx, command, args, cwd)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("Failed to create terminal: %s", err)), nil
	}

	logging.Debug("bash ACP terminal created", "terminalID", terminalID)

	// Wait for terminal to exit (with timeout from params)
	timeoutDuration := time.Duration(params.Timeout) * time.Millisecond
	timeoutCtx, cancel := context.WithTimeout(ctx, timeoutDuration)
	defer cancel()

	exitCode, err := acpConn.WaitForTerminalExit(timeoutCtx, terminalID)
	if err != nil {
		// If timeout, try to get output anyway
		if err == context.DeadlineExceeded {
			output, outputErr := acpConn.TerminalOutput(ctx, terminalID)
			if outputErr == nil {
				output = truncateOutput(output)
				output += "\n\nCommand was aborted before completion (timeout)"

				metadata := BashResponseMetadata{
					StartTime: startTime.UnixMilli(),
					EndTime:   time.Now().UnixMilli(),
				}
				return WithResponseMetadata(NewTextResponse(output), metadata), nil
			}
		}
		return NewTextErrorResponse(fmt.Sprintf("Failed to wait for terminal: %s", err)), nil
	}

	// Get the output
	output, err := acpConn.TerminalOutput(ctx, terminalID)
	if err != nil {
		return NewTextErrorResponse(fmt.Sprintf("Failed to get terminal output: %s", err)), nil
	}

	// RTK-style output compression (see Run): fail-safe, applied before truncation.
	filterResult := applyOutputFilter(params.Command, output)
	output = filterResult.Output
	if filterResult.Name != "" {
		recordSaving(savings.SourceBash, bashSavingDetail(params.Command), filterResult.Name, filterResult.Before, filterResult.After)
	}

	// Truncate if needed
	output = truncateOutput(output)

	// Apply line-based limits if requested
	acpTotalLines := countLines(output)
	acpTruncated := false
	if params.TailLines > 0 {
		newOutput := tailLines(output, params.TailLines)
		if newOutput != output {
			acpTruncated = true
		}
		output = newOutput
	} else if params.HeadLimit > 0 {
		newOutput := headLines(output, params.HeadLimit)
		if newOutput != output {
			acpTruncated = true
		}
		output = newOutput
	}

	// Add exit code info if non-zero
	if exitCode != nil && *exitCode != 0 {
		if output != "" {
			output += "\n"
		}
		output += fmt.Sprintf("Exit code %d", *exitCode)
	}

	logging.Debug("bash ACP completed", "command", params.Command, "exitCode", exitCode, "outputLen", len(output))

	metadata := BashResponseMetadata{
		StartTime:               startTime.UnixMilli(),
		EndTime:                 time.Now().UnixMilli(),
		TotalLines:              acpTotalLines,
		Truncated:               acpTruncated,
		OutputFilter:            filterResult.Name,
		OutputFilterCharsBefore: filterResult.Before,
		OutputFilterCharsAfter:  filterResult.After,
	}

	if output == "" {
		return WithResponseMetadata(NewTextResponse("no output"), metadata), nil
	}
	return WithResponseMetadata(NewTextResponse(output), metadata), nil
}
