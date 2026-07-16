---
created_at: 2026-07-16T23:19:10.840639414Z
updated_at: 2026-07-16T23:19:10.840639414Z
tags:
    - fix
    - copilot
    - models
    - provider
    - openrouter
    - endpoints
---
# Fix: Copilot model endpoint routing and capability detection from API metadata

Date: 2026-07-17

## Problem

Selecting `mai-code-1-flash` (real API id: `mai-code-1-flash-picker`) on any agent
failed with an API error. Root cause: pando decided the Copilot API route with a
regex over the *model name* instead of the metadata the API advertises.

`internal/llm/models/copilot.go` had:

```go
var responsesAPIModelRE = regexp.MustCompile(`(?i)^gpt-(\d+)`)
// major >= 5 && !HasPrefix("gpt-5-mini") -> Responses API
```

`mai-code-1-flash-picker` does not match `^gpt-`, so `IsCopilotResponsesAPIModel`
returned false and `copilotClient.send` used `/chat/completions`. But the model's
`supported_endpoints` is `["/responses"]` only — the request was rejected.

Verified live against `GET https://api.githubcopilot.com/models`:

| model | supported_endpoints | reasoning_effort |
|---|---|---|
| `mai-code-1-flash-picker` | `/responses` | low/medium/high |
| `gpt-5.6-luna`, `gpt-5.6-terra` | `/responses`, `ws:/responses` | yes (+xhigh, max) |
| `kimi-k2.7-code` | `/chat/completions` | no |
| `gpt-5.4` | `/responses`, `/chat/completions`, `ws:/responses` | yes |
| `gpt-5-mini` | `/chat/completions`, `/responses`, `ws:/responses` | yes |
| `claude-sonnet-5` | `/v1/messages`, `/chat/completions` | yes |

`gpt-5.6-*` and `kimi-k2.7-code` only worked *by luck* of their names matching (or
not matching) the regex.

Second bug (same class): `fetchCopilotModels` parsed only `capabilities.limits`,
discarding `capabilities.supports` and `supported_endpoints`. Both refresh paths
(`RefreshProviderModels` and `modelFromFetchedAccountModel`) then built `Model`
values with `CanReason`, `SupportsReasoningEffort` and `SupportsAttachments`
always false for any dynamic model without a static catalogue entry.
OpenRouter's fetcher had the same defect (discarded `supported_parameters` and
`architecture.input_modalities`).

## Reference: how the VS Code extension does it

`microsoft/vscode-copilot-chat`, `src/platform/endpoint/node/chatEndpoint.ts:233`:

```ts
protected get useResponsesApi(): boolean {
    if (this.modelMetadata.supported_endpoints
        && !this.modelMetadata.supported_endpoints.includes(ModelSupportedEndpoint.ChatCompletions)
        && this.modelMetadata.supported_endpoints.includes(ModelSupportedEndpoint.Responses)) {
        return true;
    }
    return !!this.modelMetadata.supported_endpoints?.includes(ModelSupportedEndpoint.Responses);
}
```

It has **no** name-based special-casing for mai-code, kimi or gpt-5.6 (`grep` over
`src/` finds zero hits). The family checks that do exist (`isGpt54()`,
`isGpt5PlusFamily()` in `chatModelCapabilities.ts`) select *tools*
(`apply_patch` vs `replace_string`), not the endpoint.

## Changes

- `internal/llm/models/fetcher.go`
  - `FetchedModel` gains `SupportedEndpoints []string`, `CanReason`,
    `SupportsReasoningEffort`, `SupportsAttachments`.
  - `fetchCopilotModels` parses `supported_endpoints`, `capabilities.type`,
    `capabilities.supports.{reasoning_effort,vision,tool_calls}`. Non-chat models
    (embeddings) are now filtered out.
  - `fetchOpenRouterModels` parses `supported_parameters` (`reasoning`,
    `include_reasoning`), `architecture.input_modalities` (`image`) and
    `top_provider.max_completion_tokens`.
- `internal/llm/models/models.go` — `Model.SupportedEndpoints []string`.
- `internal/llm/models/copilot.go` — new `CopilotModelUsesResponsesAPI(m Model)`,
  plus `CopilotEndpoint*` constants. Metadata-driven, mirroring the extension.
  `IsCopilotResponsesAPIModel` kept as the name-based fallback for static
  catalogue entries with no fetched metadata.
- `internal/llm/models/registry.go` — both `RefreshProviderModels` and
  `modelFromFetchedAccountModel` now carry the fetched capabilities into `Model`.
  The static-inherit branch still overrides `SupportedEndpoints` from the API,
  since the static catalogue never carries routing data.
- `internal/llm/provider/copilot.go` — `isResponsesAPIModel()` calls
  `models.CopilotModelUsesResponsesAPI(model)`.

### Routing rule

1. No `SupportedEndpoints` (static entry / older API) -> name heuristic.
2. No `/responses` advertised -> Chat Completions.
3. `/responses` but no `/chat/completions` -> Responses API. **(fixes mai-code)**
4. Both advertised -> keep the historical name heuristic, so `gpt-5-mini` stays
   on Chat Completions and `gpt-5.4` stays on Responses.

## Verification

- `go build ./...` clean; `go test ./internal/llm/models ./internal/llm/provider
  ./internal/llm/agent ./internal/api` all pass.
- New `internal/llm/models/copilot_endpoints_test.go`: routing table, Copilot
  capability/endpoint parsing + embeddings filtering, OpenRouter capability
  parsing, registry capability propagation.
- Live end-to-end through pando's own provider stack (temporary probe, removed
  afterwards) against the real Copilot API — every model returned `PONG`:

| model | endpoints | routes to /responses | result |
|---|---|---|---|
| `mai-code-1-flash-picker` | `/responses` | true | OK (was failing) |
| `kimi-k2.7-code` | `/chat/completions` | false | OK |
| `gpt-5.6-luna` | `/responses`, ws | true | OK |
| `gpt-5.6-terra` | `/responses`, ws | true | OK |
| `gpt-5-mini` | both | false | OK |
| `gpt-5.4` | both | true | OK |
| `claude-sonnet-5` | `/v1/messages`, `/chat/completions` | false | OK |

`claude-sonnet-5` now sends `reasoning_effort` over Chat Completions (the API
declares support) and is accepted — the old "Unrecognized request argument
supplied: reasoning_effort" note in `pando/plans/tui-fixes-2026-03-08.md` no
longer applies to current Copilot models.

## Operational note

`~/.pando_models.json` caches dynamic models. Stale entries (pre-fix) carry no
`supported_endpoints` and the old `128000` context-window fallback. A model
refresh is required for the fix to take effect on an existing install.

Related: [[copilot_context_window_fetch]]
