---
id: PANDO-US-0040
type: story
title: Sandbox core package, policy model and config
status: done
priority: high
parent: PANDO-EP-0009
labels: [security, sandbox]
estimate: 5
created: 2026-09-18T08:36:24Z
updated: 2026-09-18T11:01:10Z
started: 2026-09-18T09:08:28Z
closed: 2026-09-18T11:01:10Z
---

## Description

**As a** Pando maintainer **I want** a platform-neutral `internal/sandbox` package with a policy model and config section **so that** every spawn site and UI shares one definition of "sandboxed".

### Implementation

- New `internal/sandbox/{policy.go,sandbox.go,detect_caps.go,other.go}`:
  - `Policy`, `Mode` (`workspace-write|read-only|strict|off`), `Network`
  - `Resolve(cfg *config.Config, workspace string) Policy`
  - `Capability{Backend, Version, Enforced, Reason}` and `Default() Wrapper`
- Writable roots: the workspace (from `config.WorkingDirectory()`), temp dirs (following Grok `paths.rs:45-68`), and cache allowlist dirs.
- Protected paths: `<ws>/.pando`, `<ws>/.pando.toml`, `$HOME/.pando.toml`, the global config dir, `<ws>/.git/hooks` and `<ws>/.git/config`.
- `internal/config/config.go`: add `Sandbox SandboxConfig` to `Config` (around `:1091`). Zero value means ON; `Disabled` is a bool, the rest are strings where empty means default. Add `UpdateSandbox(SandboxConfig) error` following `UpdateContainer` (`:5530`).
- Project-config tightening only: the merge logic in config load ignores `Disabled=true` and broader `WritableRoots` coming from a project-level file.
- Env override `PANDO_SANDBOX`.
- Honour `overlay.LockedKeys()` for `sandbox.*`.
- Update `pando-schema.json` and `schema/`.

## Acceptance Criteria

- [ ] `Resolve` unit tests cover each mode, precedence (lock > env > project > global > default) and the project-cannot-loosen rule.
- [ ] An empty config resolves to `workspace-write`, network allowed and auto-allow on.
- [ ] `config` tests call `isolateGlobalConfig(t)` (repo pitfall).

## Notes

Depends on: none.

Part of PANDO-EP-0009. Full design rationale and Grok Build citations: KB `pando/analysis/grok-build-sandbox-research.md`.
