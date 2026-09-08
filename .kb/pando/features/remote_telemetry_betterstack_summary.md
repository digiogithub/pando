---
created_at: 2026-09-10T21:50:10.264873676Z
updated_at: 2026-09-10T21:50:10.264873676Z
tags:
    - feature
    - telemetry
    - summary
---
# Remote telemetry to Better Stack: implementation summary (2026-09-11)

Status: COMPLETE (P0-P6 + review fixes).

- Plan: [[pando/plans/remote_telemetry_betterstack_plan.md]].
- Phase notes:
  - [[pando/features/remote_telemetry_betterstack_phase0_1.md]]
  - [[pando/features/remote_telemetry_betterstack.md]] (P2)
  - [[pando/features/remote_telemetry_betterstack_phase3.md]]
  - [[pando/features/remote_telemetry_betterstack_phase4.md]]
  - [[pando/features/remote_telemetry_betterstack_phase5_tui.md]]
  - [[pando/features/remote_telemetry_betterstack_phase6.md]]
- Review fixes: [[pando/fixes/remote_telemetry_review_fixes.md]].

## What exists now
- **Opt-in, off by default.** Toggle lives in TUI Settings > General, WebUI Settings > General (Desktop reuses the WebUI), the `pando telemetry` CLI and the `pando_setup telemetry` tool.
- **Debug ID.** 16 random digits stored raw in the GLOBAL config `[Telemetry]` section. It is displayed and shipped dashed: `1234-5678-9012-3456`.
- **Token.** Injected at link time via `-X github.com/digiogithub/pando/internal/telemetry.sourceToken`:
  - Makefile: `PANDO_BETTERSTACK_TOKEN`
  - goreleaser: `envOrDefault`
  - release.yml: `secrets.PANDO_BETTERSTACK_TOKEN`
  - The repo secret was set on 2026-09-11 with `kvage get pando_betterstack_token | gh secret set PANDO_BETTERSTACK_TOKEN -R digiogithub/pando`.
- **Custom endpoint.** `PANDO_TELEMETRY_ENDPOINT` requires `PANDO_TELEMETRY_TOKEN` and never uses the built-in token. It must be https unless loopback.
- **Privacy.**
  - Info-level agent tool results are summarized; full content is logged at Debug only.
  - Attrs are JSON round-tripped, redacted (keys, values, `$HOME` becomes `~`) and truncated: 2 KiB per string, 16 KiB per record.
  - Per-session request/response dumps are never shipped.
  - Debug records ship only when the app Debug flag is on.

## Pitfalls learned
- **GitHub push protection.** It blocked the push because of a fake Slack `xox…` token in `internal/redact/patterns_test.go`.
  - Provider-shaped fixtures must be split at runtime with `fakeSecret(parts...)`. Never write contiguous token literals.
  - A blocked push leaves nothing remote: jj auto-amends `@`, then push again.
- **Config singleton.** `config.Load` is a process singleton, so tests must use `config.ResetForTests()` plus `isolateGlobalConfig(t)`.
- **Global writes.** `updateCfgFile` prefers the project `.toml`. Telemetry uses `updateGlobalCfgFile`.

## Verification
- **Go:**
  - `go build ./...` and `go vet ./...` are clean.
  - `go test -race` passes on telemetry/redact/logging/config/app/api.
  - tui, llm/tools, llm/provider and cmd tests pass.
  - `internal/llm/agent` has 4 pre-existing HOME-leak failures, unrelated.
- **WebUI:** typecheck and build pass.
- **Python E2E:** `python3 -m unittest tests.test_telemetry_cli` passes 7/7 against a local mock ingest.
- **Not verified:** real Better Stack ingest from this session. The environment blocked the outbound request that carries the token. The user must run it manually:

  ```
  PANDO_BETTERSTACK_TOKEN=$(kvage get pando_betterstack_token) make build-fast
  pando telemetry enable
  pando serve
  ```

  Then filter by debug_id in Better Stack.
