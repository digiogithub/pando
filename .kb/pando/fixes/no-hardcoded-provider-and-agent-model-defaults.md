---
created_at: 2026-08-21T10:11:31.823252427Z
updated_at: 2026-08-21T10:29:20.381853774Z
tags:
    - fix
    - config
    - providers
    - models
    - webui
    - remembrances
---
# Fix: phantom Anthropic provider, hardcoded agent models, Remembrances settings pickers

Date: 2026-08-21. Related: [[fix_global_project_provider_accounts]], [[fix_fresh_machine_coder_model]],
[[feature_modelsdev_catalog]], [[plan_pando_setup_dynamic_model_switch]].

Four independent problems reported on a fresh install, fixed in one pass.

## 1. Anthropic provider always present on a new system

**Cause.** Both config templates (`internal/config/init.go` `DefaultConfigTemplate`
and the CLI template in `cmd/init.go`) wrote an *uncommented* `[Providers.anthropic]`
section with an empty `APIKey` and `UseOAuth = true`, while every other provider was
commented out. `migrateProvidersToAccounts` then turned that section into a real
`ProviderAccount`, so a user who never configured Anthropic saw an Anthropic account
and Anthropic models everywhere.

**Fix.**
- Both templates now comment the Anthropic block out; no provider is enabled by default.
- New `providerEntryHasCredentials(provider, Provider)` in `internal/config/config.go`.
  `migrateProvidersToAccounts` skips (and removes from `cfg.Providers`) any legacy
  section that requires an API key but has none in config, none in the environment and
  no stored OAuth session (`hasClaudeCredentials` / `hasCopilotCredentials` /
  `hasAWSCredentials` / `hasVertexAICredentials`). Providers that need no key
  (Ollama, llama.cpp, local) and entries explicitly `Disabled` are preserved, so an
  opt-out survives. This also repairs configs already written by the old template.

## 2. Hardcoded (and stale) agent models — gpt-4o for summarizer/title/task

**Cause.** `setProviderDefaults` set `agents.{coder,summarizer,task,title}.model` to
hardcoded IDs per provider (`models.CopilotGPT4o`, `models.Claude4Sonnet`,
`models.GPT41`, …). Those IDs went stale as providers retired models: on Copilot every
auxiliary agent was pinned to `copilot.gpt-4o`, which no longer exists.

**Fix — models resolved from the live registry, coder is the single source of truth.**
- The whole hardcoded block in `setProviderDefaults` is gone.
- `setDefaultModelForAgent` rewritten:
  - non-coder agents **inherit the coder model**, and stay empty when the coder has none;
  - the coder resolves `defaultCoderModel()` → `defaultProviderPreference()` (Copilot,
    Anthropic, OpenAI, Gemini, Groq, OpenRouter, XAI, Bedrock, Azure, VertexAI, Ollama,
    llama.cpp) filtered by `providerUsableForDefaults()`, then `bestModelForProvider()`
    picks from `models.SupportedModels()`: drops non-chat models
    (`nonChatModelMarkers`: embed/rerank/tts/whisper/image/…), prefers flagship over
    small variants (`smallModelMarkers`: mini/nano/flash/haiku/…), then the largest
    context window, then the more expensive output, then ID for determinism.
- New `propagateCoderModel(modelID, persist)` called from `setAgentModel` whenever the
  **coder** model changes: fills every `KnownAgentNames` entry that still has no model
  (summarizer, task, title, cli-assist, persona-selector, context-enricher), validates
  each, persists them in a single `updateCfgFile`, and leaves explicitly configured
  agents untouched. `ensureEvaluatorDefaultModel()` (self-improvement model) already
  inherited the coder model and still runs right after.
- `ensureAgentDefaults()` now resolves the coder first, then walks all
  `KnownAgentNames` (previously only 5 agents were covered).
- New helper `agentInheritingModel` keeps the manual `MaxTokens` override and drops
  reasoning-effort / thinking-mode, which `validateAgent` re-derives for the new model.

**Live refresh in both UIs.** `propagateCoderModel` publishes
`ConfigChangeEvent{Section: "agents", Source: "config"}`. The TUI settings page already
subscribes to `config.Bus` and only ignores `Source == "tui"`, so it repaints. The
Web UI receives it over `/api/v1/config/events`; `web-ui/src/stores/configEventsStore.ts`
now also calls `useAgentsStore.fetchAgents()` and `useProvidersStore.fetchProviders()`
(it previously refreshed only `useSettingsStore`), so a coder-model change made from the
Web UI itself is reflected in the agents section — the backend reports source
`"config"`, precisely so the `source === 'webui'` loop guard does not filter it.

## 3. WebUI Remembrances: Browse button for the KB folder

`GET /api/v1/fs/browse` and `web-ui/src/components/shared/DirBrowserDialog.tsx` already
existed (used by Projects). `RemembrancesSettings.tsx` now shows a `Browse…` button next
to the KB Path field that opens the same dialog; manual typing still works.

The dialog only understands absolute paths. The KB path is normally **project-relative**
(default `./.kb`), so for a relative value the picker opens at the instance working
directory — `useProjectStore.workspace.cwd`, from `GET /api/v1/project`, fetched on mount
when the store has not loaded it yet — and **not** at `$HOME`. It opens at the working
directory itself rather than at the joined KB folder, which may not exist yet and would
leave the dialog in an error state. An absolute or `~` value is used as-is.

## 4. WebUI Remembrances: embedding model pickers

New `GET /api/v1/remembrances/embedding-models?provider=&base_url=&api_key=` in
`internal/api/handlers_embedding_models.go` (route registered in `internal/api/routes.go`).
Query params let the UI list models for the values being edited before they are saved;
anything omitted falls back to the stored config via `resolveProviderCredentials`.

- **Ollama does distinguish embedding models, but not in `/api/tags`.** The capability
  lives in `POST /api/show`, whose `capabilities` array contains `"embedding"`. The
  handler lists `/api/tags`, then fans out `/api/show` (6 concurrent) and keeps the
  models flagged as embedders → `source: "api"`. Daemons too old to report
  `capabilities` fall back to name filtering (`embeddingNameMarkers`:
  embed/bge/gte-/e5-/minilm/nomic/mxbai/arctic-embed/qwen3-embedding) →
  `source: "heuristic"`.
- **openai / openai-compatible**: `GET {base}/models` filtered by name (neither reports
  per-model capabilities) → `"heuristic"`; on failure a known catalog → `"static"`.
- **anthropic (voyage)**: known catalog → `"static"`.

Front end: `EmbeddingModelPicker` in `RemembrancesSettings.tsx` renders a `<select>` of
the discovered models **plus** the free-text input (backed by a `<datalist>`) and a
`Refresh` button, used for both Document and Code embedding models. Free typing is
preserved on purpose: self-hosted endpoints can serve models no listing knows about.
A hint line reports how the list was obtained, or the error when it could not be fetched.

## Files touched

- `internal/config/config.go` — `setProviderDefaults`, `setDefaultModelForAgent`,
  `agentInheritingModel`, `defaultCoderModel`, `defaultProviderPreference`,
  `providerUsableForDefaults`, `bestModelForProvider`, `isNonChatModel`, `isSmallModel`,
  `propagateCoderModel`, `providerEntryHasCredentials`, `migrateProvidersToAccounts`,
  `ensureAgentDefaults`, `setAgentModel`
- `internal/config/init.go`, `cmd/init.go` — config templates
- `internal/api/handlers_embedding_models.go` (new), `internal/api/routes.go`
- `web-ui/src/components/settings/RemembrancesSettings.tsx`
- `web-ui/src/stores/configEventsStore.ts`
- `internal/config/agent_model_defaults_test.go` (new)

## Verification

- `go build ./...` clean; `gofmt -l` clean for the touched packages; `npx tsc --noEmit`
  clean for the web UI.
- `go test ./internal/config ./internal/api ./internal/llm/agent` — all pass.
- New tests: coder-model propagation fills only empty agents; non-coder agents stay
  empty while the coder has none and inherit afterwards; a credential-less Anthropic
  section is not migrated to an account; the template enables no provider.
- Smoke test loading a fresh config in a temp `HOME` with no API keys: no Anthropic
  account, no Anthropic provider entry, and every agent shares the single model resolved
  from the only usable provider (locally, auto-detected Ollama).
