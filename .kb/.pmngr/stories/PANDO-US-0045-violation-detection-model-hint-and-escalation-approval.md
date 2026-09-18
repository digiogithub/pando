---
id: PANDO-US-0045
type: story
title: Violation detection, model hint and escalation approval
status: backlog
priority: high
parent: PANDO-EP-0009
labels: [security, sandbox]
estimate: 8
created: 2026-09-18T08:36:27Z
updated: 2026-09-18T08:36:27Z
---

## Description

**As a** developer **I want** blocked commands explained to the agent, and a way to approve an unsandboxed re-run **so that** the sandbox never becomes a dead end.

### Implementation

- `internal/sandbox/detect.go`: `Classify(exitCode, stderr, policy) (Denial{Kind: fs|net, Evidence})`, with patterns for EACCES, EPERM, read-only FS and network errors, plus path cross-checks.
- `bash.go`:
  - on a denial, append the `[sandbox]` hint and set `metadata.SandboxDenied`
  - new params `sandbox_permissions` (`"use_default"|"require_escalated"`) and `justification`
  - an escalated request calls `permissions.RequestWithContext` with `Action:"execute_unsandboxed"` and `RequireExplicitApproval:true`, then runs a one-shot unsandboxed `$SHELL -c` in the persistent shell's cwd
- Dialogs show the "Run outside sandbox" wording, the justification and a warning style:
  - TUI `internal/tui/components/dialog/permission.go`
  - WebUI `web-ui/src/components/chat/PermissionDialog.tsx`
  - ACP permission options, following the existing ACP handler path
  - AG-UI HITL
- "Allow for session" stores the grant (`GrantPersistant`) by command prefix. **Global auto-approve and goal/autopilot mode must never auto-grant `execute_unsandboxed`** unless `Sandbox.AllowAutoEscalation=true`.

## Acceptance Criteria

- [ ] A classifier table test covers at least 20 stderr samples (Linux, macOS, and false-positive cases).
- [ ] Agent test: a command fails under the sandbox; the model is shown the hint; an escalated call raises exactly one explicit-approval prompt; on deny it returns `permission denied`; on allow it runs unsandboxed.
- [ ] Auto-approve and goal mode still prompt for escalation.
- [ ] `go test ./internal/llm/agent ./internal/api` passes.

## Notes

Depends on: PANDO-US-0044.

Part of PANDO-EP-0009. Full design rationale and Grok Build citations: KB `pando/analysis/grok-build-sandbox-research.md`.
