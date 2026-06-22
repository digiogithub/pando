---
created_at: 2026-06-22T07:41:26.404625729Z
updated_at: 2026-06-22T07:41:26.404625729Z
tags:
    - fix
    - config
    - delegation
    - viper
---
# Fix A1 — viper nested-default shadowing for mesnada.delegation.*

Date: 2026-06-22. Backlog item **A1** (HIGH) from
`pando/plans/delegation_future_improvements.md`. Resolves the long-standing
pre-existing failure of `TestMesnadaDelegationDefaults` (internal/config).

## Symptom
`go test ./internal/config -run TestMesnadaDelegationDefaults` failed:
```
MaxResurrections = 0, want 4
MaxDepth = 0, want 3
MaxConcurrent = 0, want 8
ResurrectionTimeout = "", want 10m
```
Only reproduced via the full `config.Load()` path with a real config file that
sets an unrelated sibling key under `[Mesnada]` (e.g. `Enabled = true`) but no
`[Mesnada.Delegation]` table.

## Root cause
Well-known viper behaviour: once a config file introduces the `mesnada` node into
viper's config layer (via `mergeLocalConfig` → `viper.MergeConfigMap(local.AllSettings())`),
the nested `mesnada.delegation.*` **defaults** are dropped during
`viper.Unmarshal` / `AllSettings` (the `mesnada.delegation` subtree disappears
from the merged view). Empirically:
- The int/string caps (`maxResurrections`, `maxDepth`, `maxConcurrent`,
  `resurrectionTimeout`) come back as `0`/`""`.
- The boolean fields, including the default-true `autoStartWarmInstance`, DO
  survive unmarshal correctly (verified by probe and by the new regression test).
- When the user provides a `[Mesnada.Delegation]` table, unmarshal reads ALL the
  keys they list correctly (so `TestMesnadaDelegationUserValuesWin` always
  passed); only default-ONLY keys are shadowed.

`viper.IsSet(...)` and direct `viper.GetInt(...)` on the nested keys are ALSO
subject to the shadowing (IsSet returns true but Get returns 0), so reading back
from viper is not a usable workaround. The fix is therefore applied Go-side after
unmarshal, matching the existing `normalizeRemembrancesDefaults` pattern.

## Change (internal/config/config.go)
- New documented-default constants (single source of truth, shared by
  `setDefaults` and the Go-side fallback):
  `defaultDelegationMaxResurrections=4`, `defaultDelegationMaxDepth=3`,
  `defaultDelegationMaxConcurrent=8`, `defaultDelegationResurrectionTimeout="10m"`.
  The existing `viper.SetDefault("mesnada.delegation.*")` literals now reference
  these constants.
- New `normalizeMesnadaDelegationDefaults()` called from `applyDefaultValues()`
  (right after `normalizeRemembrancesDefaults()`): fills each int cap when `== 0`
  and the timeout when blank, from the constants. Booleans are left untouched so
  a user-set `AutoStartWarmInstance = false` is preserved (it survives unmarshal).

### Documented consequence
A zero value after unmarshal means "user did not set it" (a `[Mesnada.Delegation]`
table would have been read explicitly). Therefore an explicit cap of `0` is now
treated as "use the documented default" rather than "unlimited"; use a large
value for effectively-unlimited. This edge was already unreachable before the fix
(0 was indistinguishable from shadowed-0) and 0 was never a meaningful cap value.

## Tests (internal/config/mesnada_delegation_test.go)
- `TestMesnadaDelegationDefaults` — now PASSES.
- New `TestMesnadaDelegationWarmDefaultsUnderShadowing` — under the shadowing
  trigger (`[Mesnada]\nEnabled=true`), `AutoStartWarmInstance` stays true,
  `ReuseWarmInstances` stays false, `MaxConcurrent==8`.
- New `TestMesnadaDelegationAutoStartFalseWins` — user-set
  `AutoStartWarmInstance=false` (with `ReuseWarmInstances=true`) is preserved and
  not clobbered; unlisted caps still fall back to defaults.

## Verification
- `go test ./internal/config` green (all delegation + update + parseEnvBool).
- `go build ./...` green; `go vet ./internal/config` clean; gofmt clean.
- `go test ./internal/llm/agent ./internal/api` green (no consumer regression).

## Related
- Backlog: `pando/plans/delegation_future_improvements.md` (item A1 → DONE).
- Origin note: `pando/changes/delegation_phase7_3_warm_target_routing.md`
  ("Known pre-existing issue") and `pando/changes/delegation_phase0_foundations.md`.
- Feature: `pando/features/delegated_conclusions_resurrection.md`.
