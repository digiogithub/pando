---
id: PANDO-US-0044
type: story
title: Bash tool and persistent shell integration
status: done
priority: high
parent: PANDO-EP-0009
labels: [security, sandbox]
estimate: 5
created: 2026-09-18T08:36:26Z
updated: 2026-09-18T11:01:12Z
started: 2026-09-18T09:36:26Z
closed: 2026-09-18T11:01:12Z
---

## Description

**As a** developer **I want** the bash tool's persistent shell to start inside the sandbox and restart when the policy changes **so that** the default is protected and toggles apply immediately.

### Implementation

- `internal/llm/tools/shell/shell.go` `newPersistentShell`:
  - apply env scrubbing (replacing `os.Environ()` at `:94`), then `sandbox.Default().Wrap(cmd, policy)`
  - store `policyHash`
  - `GetPersistentShell` re-spawns when `sandbox.CurrentPolicyHash() != shellInstance.policyHash`
  - `killChildren` (`:248`) switches to killing the process group, because a bwrap parent sits in between
- `internal/runtime/host.go`: no change beyond using the same shell. `embedded_runtime.go:258`: wrap its `exec.CommandContext`.
- `internal/llm/tools/bash.go`:
  - when `sandbox.Active() && policy.AutoAllowBash && !isDangerous`, skip `permissions.Request` (`:359`)
  - add `SandboxBackend`, `SandboxMode` and `ApprovedBySandbox` to `BashResponseMetadata`
  - update `bashDescription()` (`:119`)
- Skip wrapping when `Container.Runtime` resolves to docker or podman.

## Acceptance Criteria

- [ ] `go test ./internal/llm/tools/...` covers wrapping, the auto-allow floor (dangerous commands still prompt) and re-spawn on policy change.
- [ ] Manual: toggling Off in settings, the next `touch ~/x` succeeds; toggling On again, it fails.
- [ ] Provider API keys are absent from `env` inside the sandboxed shell by default.

## Notes

Depends on: PANDO-US-0041 and/or PANDO-US-0042, PANDO-US-0040.

Part of PANDO-EP-0009. Full design rationale and Grok Build citations: KB `pando/analysis/grok-build-sandbox-research.md`.
