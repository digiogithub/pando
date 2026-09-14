---
created_at: 2026-09-14T17:23:50.889473429Z
updated_at: 2026-09-14T17:23:50.889473429Z
tags:
    - fix
    - tests
    - config
    - isolation
    - llm-agent
---
# Fix: internal/llm/agent test-isolation leak (PANDO-T-0003)

Date: 2026-09-14

## Symptom

`go test ./internal/llm/agent` failed 4 tests only when the whole package ran together
(passed individually): `TestSetAndGetCavemanMode`, `TestCavemanActivatesTheSessionPolicyPath`,
`TestCavemanSessionPolicyInstructions`, `TestApplyToolDiscoveryWithoutManagerIsUnchanged`.
Six agents hit this independently on 2026-09-14 while working on unrelated stories (filed as
PANDO-T-0003).

## Root cause (named global)

The leaking global is **`internal/config.cfg`** (`var cfg *Config` at
`internal/config/config.go:1839`), the process-wide configuration singleton read by
`config.Get()` and written by `config.Load()` / `config.SetForTests()`.

`config.Load(workingDir, debug, ...)` (`internal/config/config.go:1863`) is a single-flight
singleton loader: `if cfg != nil { return cfg, nil }`, otherwise it assigns the package global
`cfg` and populates it from viper (which reads the real `$HOME`/`XDG_CONFIG_HOME` config unless
isolated). This is the same class of bug already documented for `internal/config` itself
(`pando/fixes/config_tests_home_leakage_and_template_drift.md`,
`pando/features/remote_telemetry_betterstack_phase6.md`).

`TestBuildSystemMessageUsesTemplatePromptBuilder` in
`internal/llm/agent/agent_provider_test.go` called `config.Load(tmpDir, false)` directly,
without isolating `$HOME` and without resetting the global config afterward — it only restored
the 3 struct fields (`WorkingDir`, `ContextPaths`, `MCPServers`) it happened to mutate on the
returned pointer, leaving the loaded `*Config` (with real-host defaults, notably
`ToolDiscovery.Enabled = true` from `viper.SetDefault("toolDiscovery.enabled", true)`, and
whatever `Caveman.DefaultMode` the host's config declared) as the process-wide `config.Get()`
result for every test that ran afterward in the same binary/process.

Downstream effects on the 4 failing tests:
- `caveman_session.go:configuredCavemanMode()` calls `config.Get()`; with the leaked non-nil
  cfg, `CavemanDefaultMode()` no longer resolved to off, breaking the "off by default" and
  "no active session policy by default" assertions in the caveman tests.
- `tool_discovery.go:ApplyToolDiscovery()` early-returns unchanged tools only when
  `config.Get() == nil`; with the leaked cfg (`ToolDiscovery.Enabled = true` by default), it
  took the real discovery path instead, changing the tool set
  `TestApplyToolDiscoveryWithoutManagerIsUnchanged` expected.

Confirmed by reproducing with `go test ./internal/llm/agent -run
'TestBuildSystemMessageUsesTemplatePromptBuilder|TestSetAndGetCavemanMode|...' -v`: the caveman
test failed with `expected off by default, got lite` right after the Load-based test ran.

## Route taken: reuse the existing isolate+reset seam (not a pure DI parameter, not a flag)

Per task direction, route 2 (parameterized config seam) was evaluated first.
`internal/config` does **not** offer a pure "load an independent `*Config` from a given path
without touching the global" constructor — `Load()`'s signature already takes a path
(`workingDir`), but by design it is a singleton loader (guarded by `if cfg != nil`) that both
mutates and returns the one process-wide `cfg`; production code across the tree depends on that
singleton behavior via `config.Get()`. Splitting it into a real DI seam would mean restructuring
`Load()` and every call site relying on the singleton — a much larger change than this task
warrants and not what the sibling `internal/config` fixes did.

What `internal/config` already offers, and what its own tests already use for exactly this
situation, is: `isolateGlobalConfig(t)` (`internal/config/config_test.go:20`, unexported —
`t.Setenv("HOME", t.TempDir())` + `t.Setenv("XDG_CONFIG_HOME", "")`) paired with the exported
`config.ResetForTests()` (`internal/config/config.go:4319`, already used by
`cmd/telemetry_test.go` per `pando/features/remote_telemetry_betterstack_phase6.md`). This is
the "same pattern already recorded for internal/config" the task pointed at. Since
`isolateGlobalConfig` itself is package-private to `internal/config` and this test lives in
`internal/llm/agent`, its two `t.Setenv` lines were replicated inline (same pattern, not a new
mechanism) rather than exporting a cross-package helper for a single call site.

No flag route was needed — the parameter-adjacent seam (isolate HOME + `config.ResetForTests()`)
was sufficient and already established elsewhere in the codebase.

## Change

File: `internal/llm/agent/agent_provider_test.go:TestBuildSystemMessageUsesTemplatePromptBuilder`

- Added `t.Setenv("HOME", t.TempDir())` + `t.Setenv("XDG_CONFIG_HOME", "")` before calling
  `config.Load`, so the real host/dev config is never read.
- Added `config.ResetForTests()` before `Load` (start from a known nil state) and
  `t.Cleanup(config.ResetForTests)` (leave `internal/config.cfg` nil for every later test in the
  binary), replacing the old cleanup that only restored 3 struct fields on the leaked pointer.

No production code changed — `internal/config` and `internal/llm/agent` non-test files were left
untouched; the defect was entirely in test hygiene.

## Verification

- `go test ./internal/llm/agent` — ok (whole package).
- `go test -count=2 ./internal/llm/agent` — ok.
- `go test ./internal/config/...` — ok.
- `go test ./internal/llm/agent ./internal/api` — ok.
- `go build ./...` — clean.
- `go test ./...` — clean, exit 0, no FAIL/panic lines anywhere in the log, including
  `internal/agui` (being edited concurrently by another agent; untouched here and passing on its
  own).

## Deliberately left alone

- `internal/agui/*` — out of scope per task instructions (another agent editing it
  concurrently); confirmed it still passes.
- `internal/rag/`, `internal/api/`, `internal/mesnada/`, `cmd/` — not touched; the fix did not
  require it.
- The other `config.SetForTests` call sites in `internal/llm/agent` tests
  (`agent_complete_test.go`, `caveman_session_test.go`, `desktop_tools_test.go`,
  `goal_runner_test.go`, `setup_bridge_model_test.go`, `tool_discovery_unified_test.go`) were
  reviewed and already save/restore correctly (or unconditionally reset to `nil` in cleanup,
  which is safe given no other test leaves a non-nil global behind after this fix) — they were
  not the source of the leak and were left as-is.
- `config.Load`'s singleton-with-cache design (`internal/config/config.go:1863`) itself was left
  unchanged; it is relied upon in production and by other tests, and restructuring it is out of
  proportion to this defect.
