---
id: PANDO-US-0077
type: story
title: "Config: `modelAutoMode` block with decision provider (Ollama/TypeSafe/custom), task routes (description, primary, up to 2 fallbacks), validation, persistence and REST"
status: done
priority: high
parent: PANDO-EP-0015
author: mcp
labels: [model-routing, config, api]
estimate: 5
created: 2026-09-30T19:34:53Z
updated: 2026-10-01T07:57:13Z
started: 2026-09-30T21:00:23Z
closed: 2026-10-01T07:57:13Z
---

## Description

As a user, I want to turn on model auto mode, choose which decision provider routes my prompts, and describe which kinds of tasks go to which model. The routing policy should be declarative and reviewable.

Add `Config.ModelAutoMode` next to `PersonaAutoSelect` (`internal/config/config.go:1281`). Follow the pattern of `PersonaAutoSelectConfig` (:844) and `UpdatePersonaAutoSelect` (:6187).

```go
type ModelAutoModeConfig struct {
    Enabled        bool             `json:"enabled"`
    DefaultAuto    bool             `json:"defaultAuto"`    // new sessions start in Auto (default true)
    Router         DecisionRouterConfig `json:"router"`
    Threshold      float64          `json:"threshold"`      // min p(choice), default 0.60
    MinConfidence  float64          `json:"minConfidence"`  // optional entropy guard, default 0 (off)
    TimeoutMs      int              `json:"timeoutMs"`      // default 1500 (ollama) / 3000 (remote)
    HistoryPrompts int              `json:"historyPrompts"` // previous user prompts in state, default 0
    Routes         []ModelAutoRoute `json:"routes"`
}

type DecisionProviderKind string // "ollama" | "typesafe" | "custom"

type DecisionRouterConfig struct {
    Provider     DecisionProviderKind `json:"provider"`          // default "ollama"
    BaseURL      string               `json:"baseURL,omitempty"` // root; client appends /v1/systemone, /v1/models
    APIKey       string               `json:"apiKey,omitempty"`  // encrypted at rest; "$ENV_VAR" allowed
    Model        string               `json:"model"`             // e.g. "tev1:0.8b", "jev-latest", "typesafe/jev-1.13"
    KeepAlive    string               `json:"keepAlive,omitempty"` // Ollama only, default "30m"
    Headers      map[string]string    `json:"headers,omitempty"`   // extra headers for gateways
}

type ModelAutoRoute struct {
    ID          string           `json:"id"`          // stable slug, used as the choice key
    Description string           `json:"description"` // natural language task description
    Model       models.ModelID   `json:"model"`
    Fallbacks   []models.ModelID `json:"fallbacks,omitempty"` // max 2
    Disabled    bool             `json:"disabled,omitempty"`
}
```

**Defaults per provider kind:**

| Kind | BaseURL | APIKey |
|---|---|---|
| `ollama` | Empty means the Ollama provider's raw base URL (`models.ResolveOllamaRawBaseURL`), or the auto-detected `http://localhost:11434` | Not used |
| `typesafe` | `https://api.typesafe.ai` | Required. Falls back to `$TYPESAFE_API_KEY` when empty |
| `custom` | Required | Optional |

The API key is stored encrypted, the same way provider keys are handled in `updateConfigFileAt`.

## Acceptance Criteria

- [ ] **Validation:**
  - Provider kind is one of `ollama`, `typesafe` or `custom`.
  - For `custom`, `baseURL` is required and must be a valid http(s) URL.
  - For `typesafe`, a key or the env fallback must be present. If neither is, this is a warning, not a load error.
  - `router.model` is required when `enabled`.
  - For `ollama`, the model must not end in `:cloud`.
  - At most 25 routes can be enabled.
  - Route IDs are unique and not blank. `none` is reserved.
  - Route description is required and at most 500 characters.
  - Route `Model` is required.
  - Each route has at most 2 fallbacks, with no duplicates and none equal to the primary.
  - `threshold` is in (0,1].
- [ ] Models the registry does not know produce warnings, not errors. At runtime, candidates with unknown models are skipped.
- [ ] `UpdateModelAutoMode(cfg)`:
  - persists through `updateCfgFile`
  - encrypts `router.apiKey`
  - reverts on error
  - respects `ErrIfLocked("modelAutoMode…")`
  - publishes on the config `Bus`
- [ ] Hot reload through `Reload()` works.
- [ ] Defaults exist in viper and in the config template (`internal/config/init.go:608`). `pando-schema.json` is updated.
- [ ] REST `GET/PUT /api/v1/config/model-auto-mode`:
  - `GET` never returns the plain API key. It returns `apiKeySet: true` plus a masked tail.
  - `PUT` with an empty key keeps the stored one.
  - Validation errors are reported per field.
- [ ] Unit tests cover the validation matrix, provider defaults, key encryption round-trip and persist/reload. Config tests call `isolateGlobalConfig(t)`.

## Notes

- The block is global. A project override follows the existing global/project merge rules.
- The coder model (`agents.coder.model`) remains the no-match fallback.
- Spec: "Model auto mode: configuration" (see the epic specs).
