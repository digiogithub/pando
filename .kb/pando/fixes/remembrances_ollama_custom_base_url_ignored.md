---
created_at: 2026-10-22T15:36:59.110714693Z
updated_at: 2026-10-22T15:36:59.110714693Z
tags:
    - fix
    - remembrances
    - embeddings
    - ollama
    - config
---
# Remembrances: custom Ollama base URL ignored after restart (2026-10-22)

## Symptom
Settings > Remembrances > Document embeddings: provider `ollama`, Base URL `http://localhost:11435`. "Test connection" reports OK, but after restarting Pando the embedder goes back to the default `http://localhost:11434`.

## Root cause
The value was saved correctly (`DocumentEmbeddingBaseURL`), but `internal/rag/service.go` `resolveEmbedderCredentials` only used the custom base URL for `openai-compatible`. For `ollama` it always used the Ollama provider URL (or `OLLAMA_BASE_URL`, then the localhost:11434 default). The settings test (`internal/api/handlers_remembrances.go` `testEmbedder`) and the TUI test both applied the override, which is why the test passed while the running service used the default.

## Change
- `internal/rag/service.go`: `resolveEmbedderCredentials` uses a non-empty custom base URL for `ollama` (normalised with `models.ResolveOllamaRawBaseURL`). This affects both the document and the code embedder (code inherits the document URL when `UseSameModel`).
- `internal/api/handlers_setup.go`: `setupOllamaBaseURL` (first-run setup assistant: Ollama status and model pulls) prefers the Remembrances document base URL when its provider is ollama.
- `internal/config/config.go`: comments on `DocumentEmbeddingBaseURL` and `CodeEmbeddingBaseURL` now describe the ollama override.
- New `internal/rag/service_test.go`.

## Verification
`go build ./...`; `go test ./internal/rag ./internal/api ./internal/config` pass.

Related: [[pando/fixes/default-code-embedding-model-repo-gone.md]]
