# config: Load must not write the user's config file (evaluator seed stays in memory)

## Problem
Two processes sharing one config file (two containers on one volume) broke:
`pando agui-serve` rewrote the shared `.pando.toml` at startup (full config
dump, plaintext MCP header re-encrypted as `age1:` with an identity local to
that process, although `AgeKeys = ''`), so the other process (`llm-proxy`)
could no longer decrypt it ("no identity matched any of the recipients").

## Root cause
`config.Load` -> `applyDefaultValues` -> `ensureEvaluatorDefaultModel()` seeded
`evaluator.model` from the coder agent's model when none was set and persisted
it through `updateCfgFile` -> `updateConfigFileAt`, which re-marshals the whole
file, runs `migrateProvidersToAccounts` on the parsed file and
`encryptSensitiveConfigFields`. Any file with `[Agents.coder] Model` and no
`[Evaluator] Model` was rewritten on every first start. Present on `main`
too; independent of env providers (the "migrated legacy providers" log line is
the in-memory migration inside that same write path, a symptom, not the cause).

## Change
`internal/config/config.go`: `ensureEvaluatorDefaultModel(persist bool)`.
`Load` passes `false` (in-memory seed, derived again on each load);
`UpdateAgentModel` with persist (explicit user selection of the coder model)
passes `true`, unchanged behaviour. Load now performs no config write.

## Verification
- New test `internal/config/load_nowrite_test.go` (TestLoadDoesNotRewriteConfigFile): env provider + coder model + plaintext MCP header, file byte-identical after Load; fails without the fix.
- `go vet` and `go test ./internal/config/... ./internal/llmproxy/... ./cmd/...` pass.
- Manual: `agui-serve` against such a file, before: file rewritten with `age1:`; after: byte-identical.
