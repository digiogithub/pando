---
created_at: 2026-10-19T13:02:52.308618428Z
updated_at: 2026-10-19T13:02:52.308618428Z
tags:
    - change
    - merge
---
# Merge into main: provider env refs, no config write on start, AG-UI profile system prompt

Date: 2026-10-19

## What
Three origin branches merged into `main` as sequential merge commits (repo style `merge: X into main`) and pushed to `origin` (digiogithub/pando). `upstream` untouched.

1. `fix/no-config-write-on-start` (49bf411a) -> merge 930adc91
   - fix(config): never write the config file during Load
2. `feat/provider-account-env-refs` (ddbe8319) -> merge a10c7572
   - feat(config): env references in provider account secrets; keep env providers beside accounts (86af9486)
   - fix(config): never write the config file during Load (bd0e249c, same patch as 49bf411a, only hunk offsets differ; merged cleanly)
   - fix(llmproxy): list models of keyless OpenAI-compatible accounts per account
3. `feat/agui-profile-system-prompt` (c2b67e52) -> merge 08b076fd (new `main`)
   - feat(agui): profiles can replace the base agent system prompt

All branches were based on previous main 085a1ce7; no conflicts.

## Verification
- `go build ./...` OK
- `go test ./internal/config/... ./internal/agui/... ./internal/llmproxy/... ./internal/llm/agent ./internal/api` all OK
- `main`, `main@git`, `main@origin` all at 08b076fd after `jj git push --remote origin --bookmark main`.

Related: [[fix_fresh_install_provider_agent_model_defaults]], [[analysis_copilotkit_agui_integration]], [[project_llm_proxy_plan]]
