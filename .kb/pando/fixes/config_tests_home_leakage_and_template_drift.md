---
created_at: 2026-07-13T15:53:45.803208086Z
updated_at: 2026-07-13T15:53:45.803208086Z
tags:
    - fix
    - tests
    - config
    - isolation
    - flaky
---
# Fix: `internal/config` failing tests — HOME leakage + template drift

Date: 2026-07-13
Context: surfaced while finishing [[kb_wiki_links_phase4_5_config_docs]] (both tests already
failed on HEAD, so they were not caused by that change).

## Diagnosis

Two failures, two different causes. **Neither was a production bug** — the code was right and
the tests were wrong, in two distinct ways.

### 1. `TestMesnadaDelegationWarmDefaultsUnderShadowing` — the test read the developer's own config

`Load()` searches for a global config in `$HOME` and `$XDG_CONFIG_HOME`
(`viper.AddConfigPath("$HOME")` etc. in `setDefaults`). `loadTempConfig` wrote a temp
`.pando.toml` and called `Load(tmpDir, false)` **without isolating HOME**, so the real
`~/.pando.toml` of whoever runs the suite was merged in and its values won.

This machine's `~/.pando.toml` has `AutoStartWarmInstance = false` (line 168), which is
exactly the assertion the test makes — so it failed here and would pass on a clean machine.
Verified by running the package with `HOME=$(mktemp -d)`: every delegation test passes at
HEAD, and a probe showed `AutoStartWarmInstance = true` under shadowing, i.e. **the A1
nested-default fallback works**; viper's shadowing does not affect that bool.

**Trap avoided:** the first hypothesis was a real regression (the Go-side fallback in
`normalizeMesnadaDelegationDefaults` restores the int/string caps but no boolean, and its
comment claims booleans "survive unmarshal correctly"). A `viper.GetBool` fallback was written
and **reverted** once the clean-HOME probe proved the comment right and the environment wrong.

### 2. `TestDefaultConfigTemplateEnablesPandoPreferredDefaults` — stale exact-string assertion

The test asserted substrings like `"[InternalTools]\nFetchEnabled = true"`, but the
`[InternalTools]` section of `DefaultConfigTemplate` column-aligns its `=`
(`FetchEnabled            = true`). The value is still `true`: pure formatting drift, the test
was never updated. `BrowserEnabled = true` was drifting the same way, hidden behind the
`Fatalf` on the earlier check.

## What was changed

- `internal/config/config_test.go`:
  - New helper `isolateGlobalConfig(t)` — `t.Setenv("HOME", t.TempDir())` +
    `XDG_CONFIG_HOME=""`, with a comment explaining that without it a test reads the config of
    whoever runs it.
  - `TestDefaultConfigTemplateEnablesPandoPreferredDefaults` rewritten: `templateSection`
    extracts a `[Section]` body (keys like `Enabled` exist in many sections) and each check is
    a `(section, key, value)` triple matched with a whitespace-tolerant regexp, so re-aligning
    the template can no longer break it. Also now asserts `[Remembrances] KBWikiLinks = true`.
- `internal/config/mesnada_delegation_test.go`: `loadTempConfig` calls `isolateGlobalConfig`.
- `internal/config/agent_model_persist_test.go`: calls `isolateGlobalConfig` **and** now
  declares `[Providers.copilot]` in its temp config — isolating HOME exposed that
  `TestAgentModelSurvivesReloadAfterResolve` was passing only because it borrowed the
  developer's Copilot credentials; with an empty home the agent's provider was unconfigured
  and the model got reverted to a default.

`provider_account_merge_test.go` and the legacy-global-config tests already isolated HOME
themselves and were left alone.

## Verification

`go test ./internal/config/` green **both** with the real `$HOME` and with
`HOME=$(mktemp -d)` (CI-like). `./internal/rag/kb`, `./internal/llm/tools`,
`./internal/llm/agent`, `./internal/api`, `./internal/agentsmd` still green.

## Lesson

Any config test that calls `Load()` must call `isolateGlobalConfig(t)`. A test that reads the
developer's home config is not just flaky — it can pass for the wrong reason (as
`TestAgentModelSurvivesReloadAfterResolve` did) and hide a genuine gap in what it claims to
cover.
