---
id: PANDO-US-0041
type: story
title: Linux backend with Landlock + seccomp helper re-exec
status: done
priority: high
parent: PANDO-EP-0009
labels: [security, sandbox]
estimate: 8
created: 2026-09-18T08:36:25Z
updated: 2026-09-18T11:01:10Z
started: 2026-09-18T09:23:51Z
closed: 2026-09-18T11:01:10Z
---

## Description

**As a** Linux user **I want** agent commands confined by the kernel **so that** a bad or injected command cannot damage files outside my project.

### Implementation

- Add deps `github.com/landlock-lsm/go-landlock` and `github.com/elastic/go-seccomp-bpf` (or a hand-rolled BPF using `golang.org/x/net/bpf`). Both are pure Go.
- `internal/sandbox/linux.go`: `Wrap(cmd, p)` rewrites the command to `os.Executable() __sandbox-exec -- <orig argv>`, passes the policy as JSON on `ExtraFiles[0]` (fd 3), and sets `SysProcAttr.Setpgid=true`.
- `internal/sandbox/helper.go`: `RunHelper(args)`, dispatched from `main.go` **before** cobra, config and logging init. Steps:
  1. `LockOSThread`
  2. `PR_SET_NO_NEW_PRIVS`
  3. go-landlock `BestEffort().RestrictPaths(...)` with a device allowlist and the `/dev/tty` ENXIO skip
  4. the namespace-lockdown seccomp filter
  5. optionally the network seccomp filter (syscall set from Grok `child_net.rs:178-199`)
  6. `unix.Exec`
- When `UseBwrap=auto|always` and the `bwrap` probe succeeds, prefix `bwrap --cap-drop ALL --bind / / --ro-bind <protected>... --dev-bind /dev /dev --proc /proc --` (following Grok `lib.rs:300-368`). Otherwise use the Landlock-only fallback, which grants RW per top-level workspace entry excluding protected ones.
- Capability probe via `landlock.ABI`/`landlock_create_ruleset(NULL,0,VERSION)` and `bwrap --version`, cached.

## Acceptance Criteria

- [ ] The e2e matrix passes on a Landlock kernel: workspace write allowed, `$HOME` write denied, `.pando.toml` write denied (bwrap and fallback), `unshare -Ur` denied, network denied only when `restricted`.
- [ ] A BPF interpreter unit test covers the syscall sets, wrong arch and x32.
- [ ] On a kernel without Landlock, `Capability.Enforced=false` with a reason, and commands still run.
- [ ] The helper never loads config or DB, verified by a startup-time test under 20 ms.

## Notes

Depends on: PANDO-US-0040.

Part of PANDO-EP-0009. Full design rationale and Grok Build citations: KB `pando/analysis/grok-build-sandbox-research.md`.
