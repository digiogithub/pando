// Package sandbox confines the host processes Pando spawns on the agent's
// behalf (the bash tool's persistent shell first; ACP terminals, skill CLI
// tools, MCP servers and subagents as configured) to a filesystem, network
// and environment policy. Pando's own process is never confined: wrapping is
// per spawn, so the database, KB, WebUI and desktop keep working and the
// policy can change at run time.
//
// # Shape
//
//   - Policy (policy.go) is the platform-neutral description of what a child
//     may do. Resolve / ResolveConfig (resolve.go) build it from
//     config.SandboxConfig and the workspace; Policy.Hash identifies it so a
//     spawn site can tell when a long-lived child must be re-spawned.
//   - Wrapper (sandbox.go) rewrites an *exec.Cmd so the command runs under a
//     Policy, and reports a Capability: which backend is in use and whether it
//     is actually enforced on this machine. Default returns the per-OS wrapper.
//   - ScrubEnv (env.go) removes credential-looking variables from a child's
//     environment according to Policy.Env.
//   - Current / CurrentPolicyHash / Active / AutoAllowBash (sandbox.go) are the
//     accessors spawn sites and UIs use for the live configuration.
//   - Guarantees (guarantees.go) says whether an enforced sandbox is complete
//     (protected paths enforced, Pando's own ports blocked); AutoAllowBash
//     requires it. RegisterGuardedPort / GuardedPorts front the leaf package
//     internal/sandbox/portguard, where every Pando listener records its TCP
//     port so Policy.DenyConnectPorts keeps confined commands away from it.
//
// # Precedence
//
// enterprise lock on sandbox.* > PANDO_SANDBOX env > project config > global
// config > default (workspace-write, network allowed, bash auto-allowed while
// enforced). The project-only-tightens rule is applied by internal/config at
// load time; the env override and lock check are applied here, by Resolve.
//
// # Per-OS backends
//
// Each OS has exactly one file defining platformWrapper(), selected by the
// file-name build constraint, so the backend stories never touch each other's
// files or this package's shared code:
//
//   - wrapper_linux.go   — PANDO-US-0041: Landlock + seccomp via the
//     `pando __sandbox-exec` re-exec helper, optional bwrap. The helper
//     itself lives in the leaf package internal/sandbox/helper (stdlib and
//     x/sys only) and dispatches from its init(), before any other package
//     initialises; bwrap_linux.go builds the bwrap prefix.
//   - sbpl.go            — pure SBPL profile generator used by the darwin
//     backend, build-tag free so its golden tests run on every OS.
//   - wrapper_darwin.go  — PANDO-US-0042: /usr/bin/sandbox-exec with a
//     generated SBPL profile.
//   - wrapper_windows.go — PANDO-US-0043: not enforced; Job Object containment.
//   - wrapper_other.go   — every other GOOS (//go:build !linux && !darwin &&
//     !windows): always the not-enforced no-op wrapper.
//
// PANDO-US-0043 also adds two small helpers that cross every OS, used by the
// shell integration and status/log code rather than by Wrap itself:
//
//   - AttachProcessTree (jobobject_windows.go / jobobject_other.go) puts a
//     started child in a Windows Job Object with
//     JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE so the whole tree dies with Pando;
//     a no-op on every other OS, since those backends already clean up
//     their own children.
//   - IsWSL (wsl.go) reports Windows Subsystem for Linux for status labels
//     and logs; WSL runs the Linux backend (wrapper_linux.go) unchanged.
//
// Until a backend story lands, its file returns newNoopWrapper(reason), which
// leaves the command untouched and reports Capability{Backend: "none",
// Enforced: false}. Callers must therefore treat "policy enabled" and
// "sandbox enforced" as different things: fail open with a visible warning,
// and never auto-approve bash unless Active() is true.
package sandbox
