---
tags:
    - agui
    - change
---
# AG-UI profiles can replace the base agent system prompt (2026-10-19)

## What changed
- New optional `SystemPrompt` key on `[AGUI.Profiles.<name>]` (`config.AGUIProfile.SystemPrompt`, json `systemPrompt`) and adapter-wide `[AGUI] SystemPrompt` (`config.AGUIConfig.SystemPrompt`), same fallback semantics as `Persona` (profile wins, else adapter-wide).
- Non-empty value REPLACES the Base agent's built-in prompt (the composed template sections and the project context file injection) for runs under that profile. Persona, profile `Prompt`, skills sections, memory block and non-interactive text still apply. Empty keeps today's prompt byte-for-byte.
- Config validation: `config.MaxAGUISystemPromptBytes` (32 KiB) cap for both locations in `validateAGUIProfiles`; `systemprompt` added to `aguiProfileKnownKeys`.

## Files and symbols
- `internal/config/config.go`: fields, `MaxAGUISystemPromptBytes`, `validateAGUIProfiles`.
- `internal/agui/deps.go`: `Config.SystemPrompt`, `Profile.SystemPrompt`, `ConfigFromApp` fallback.
- `internal/agui/runtime.go`: `applySessionOverrides` sets `SessionLLMOverrides.SystemPrompt` (independent of `PersonaScoped`).
- `internal/llm/agent/session_overrides.go`: `SessionLLMOverrides.SystemPrompt`, `isEmpty`, `storeSessionLLMOverrides`.
- `internal/llm/agent/agent.go`: `buildSystemMessage` skips `prompt.BuildPrompt` when the override is set; `composeOverrideSystemPrompt` appends skills.
- `docs/agui.md`: new Profiles section and table.

## Why
A host serving a non-coding assistant over AG-UI could not replace the "You are Pando, an interactive CLI tool ... software engineering" instructions.

## Verification
Tests: `TestBuildSystemMessageSessionSystemPromptReplacesBase`, `TestApplySessionOverrides_SystemPrompt`, `TestConfigFromApp_SystemPromptFallback`, `TestAGUISystemPromptLoadsAndCapIsEnforced`. `go vet ./...`; `go test ./internal/config/... ./internal/agui/... ./internal/llm/... ./cmd/...` pass except the environmental `TestBashRealSandboxEndToEnd` in /tmp checkouts.

## Notes
The Lua `hook_system_prompt` / `hook_prompt_compose` hooks run inside the template builder and therefore do not apply to an overridden prompt. The config API/WebUI does not enumerate AGUI profile fields, so no schema change there.
