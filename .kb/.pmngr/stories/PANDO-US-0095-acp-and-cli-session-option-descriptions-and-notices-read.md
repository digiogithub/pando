---
id: PANDO-US-0095
type: story
title: "ACP and CLI: session option descriptions and notices read `decisionModel`; `pando_setup decision-model` subcommand (show/set/test) and ACP `available_commands` entry; `pando init` template; ACP docs"
status: in_review
priority: medium
parent: PANDO-EP-0018
author: mcp
labels: [decision-model, acp, cli, pando-setup]
estimate: 5
created: 2026-10-01T16:18:26Z
updated: 2026-10-01T16:56:53Z
started: 2026-10-01T16:56:53Z
---

## Description

As an ACP (Zed, Xcode, VS Code) or CLI user with no WebUI, I want to configure and inspect the decision model and to see when context was filtered.

- `internal/mesnada/acp/session_state.go:554-600`: `autoModelDescription()` and the persona "Auto" option description read `cfg.DecisionModel.Router`; `autoModeEnabled()` unchanged. A `Context filter: kept n/m` notice arrives through the existing session notice path (verify with the ACP fake client used in `acp` tests; Xcode grouping must not swallow it).
- `pando_setup` internal tool: `decision-model show` (provider, URL, masked key, model, health, consumers on), `decision-model set --provider --base-url --api-key --model --timeout-ms`, `decision-model test`, `decision-model models` (discovery), `decision-model clear-key`. Reuses `config.UpdateDecisionModel`, `modelrouter.RouterHealth`, provider `ListDecisionModels`. Gate behind the same `pando_setup` enablement.
- ACP slash command `/decision-model` listed in `available_commands_update` (deferred post-response, see `fix_acp_slash_commands_post_response_ordering`) mapping to the tool's `show`/`test`.
- `pando init` and `internal/config/init.go` template write `[DecisionModel.Router]` instead of `[ModelAutoMode.Router]`; `pando doctor` consumed from the observability story.
- Docs: `docs/acp.md` and `docs/pando-setup.md` sections.

## Acceptance Criteria

- [ ] ACP session option descriptions show `router: ollama/tev1:0.8b` from the new block (test in `internal/mesnada/acp`).
- [ ] ACP client receives the context-filter notice as a session update when `Dropped > 0`; none otherwise (test with the fake client).
- [ ] `pando_setup decision-model …` subcommands covered by tests under `internal/llm/tools` (set persists encrypted key, show masks it, test reports health verdict via `systemonetest`).
- [ ] `/decision-model` appears in ACP `available_commands` after the first response; invoking it returns the `show` output.
- [ ] Fresh `pando init` config contains `[DecisionModel.Router]` and no `[ModelAutoMode.Router]` (golden test).
- [ ] `go test ./internal/mesnada/acp ./internal/llm/tools ./cmd` green.

## Notes

- Anchors: `session_state.go:554-600`, `internal/llm/tools/pando_setup*.go`, `internal/config/init.go:624`, `cmd/init.go`, `docs/acp.md`, `docs/pando-setup.md`.
- Spec: "Decision model: settings surfaces (WebUI/TUI/ACP)".
