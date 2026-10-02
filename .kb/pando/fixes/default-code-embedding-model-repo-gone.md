---
created_at: 2026-10-02T20:18:17.217384731Z
updated_at: 2026-10-02T20:18:17.217384731Z
tags:
    - fix
    - embeddings
    - setup
    - ollama
---
# Fix: default code embedding model pointed to a removed Hugging Face repo

Date: 2026-10-02.

## Problem
`hf.co/limcheekin/CodeRankEmbed-GGUF:Q4_K_M` was the default code embedding model of the first-run setup assistant and of the generated config templates. The repository now answers HTTP 401 on Hugging Face (removed or private), so `ollama pull` fails on new installs. Found while writing [[pando/changes/pando-docs-agentvcs-decision-model-code-embeddings.md]].

## Change
Replaced by the equivalent quantisation of the same model that is still published: `hf.co/brandtcormorant/CodeRankEmbed-Q4_K_M-GGUF:Q4_K_M` (90 MB, 768 dims, MIT). Verified pulled and embedding in Ollama 0.35.

Files: `internal/config/setup.go` (`DefaultSetupCodeEmbeddingModel`), `internal/config/init.go` and `cmd/init.go` (config templates), `internal/ollamasetup/ollamasetup_test.go`, `.pando.toml`, `.ai/remembrances.config.yaml`, `.kb/pando/features/webui-first-run-setup-assistant.md`.

Same model and vector size, so existing code indexes stay valid; users who already have the old tag locally keep working with it.

## Verification
`go build ./...`; `go test ./internal/config ./internal/ollamasetup ./internal/api` pass.
