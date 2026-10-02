---
created_at: 2026-10-02T20:12:33.368647717Z
updated_at: 2026-10-02T20:12:33.368647717Z
tags:
    - change
    - pando-docs
    - documentation
    - embeddings
    - agent-vcs
    - decision-model
---
# pando-docs: Agent-VCS recovery, decision model and code embedding model pages

Date: 2026-10-02. Repo: `/www/MCP/Pando/pando-docs` (uncommitted, on top of [[pando/changes/pando-docs-features-vs-guides-rework.md]]). Same split as the rework: features explain, guides teach in the Web UI, reference lists. Plain voice with similes, en + es.

## Gaps found
- Agent-VCS: feature and guide existed but were wrong. `Settings > Snapshots > Enabled` is the switch of Agent-VCS (`internal/app/app.go`: `agentvcs.NewService()` only when `cfg.Snapshots.Enabled`), default `false`, read at startup (restart needed). Docs described snapshots as a separate "whole-project photo" feature. One commit at session start (BASELINE) and one per agent turn, not per edit. Button is **Revert All** (dialog title "Revert to commit"), per-file revert icon, "Revert N selected". The published screenshot was an empty view.
- Decision model: no feature page, only steps inside the auto mode guide. Three consumers: auto mode routing, persona selector (`Settings > Agents > Persona Selector > Use decision model`, `useDecisionModel` on agent `persona-selector`, own model becomes fallback), context relevance filter (`Settings > Remembrances > Decision model relevance filter`). The settings page can pull suggested models (`tev1:0.8b`, `tev1`, `nimble`).
- Code embeddings: nothing explained why a separate model for code.

## Files
- New features: `docs/features/decision-model.md`, `docs/features/code-embeddings.md` (en/es); rewritten `docs/features/agent-vcs.md`.
- New guides: `guides/decision-model.md` (weight 14), `guides/code-search.md` (weight 11); rewritten `guides/review-and-undo.md`. Roots guide weights renumbered: remembrances 10, code-search 11, mesnada 12, goal-mode 13, decision-model 14, model-auto-mode 15, working-modes 16, self-improvement 17, review-and-undo 18.
- New reference: `docs/configuration/embedding-models.md`. Edited `configuration/auto-mode.md` (who uses the decision model, Pull button, test report), `configuration/modes.md` (Enabled default and meaning), `configuration/remembrances.md`, `configuration/_index.md`.
- `data/features.yaml` and `docs/features/_index.md`: slugs `decision-model` (roots.pando) and `code-embeddings` (roots.remembrances). Cross-links in context-enrichment, model-auto-mode, guides remembrances and model-auto-mode.
- Captures (pando-test, port 18767, playwright): `pando-webui-agent-vcs-commit.jpg`, `pando-webui-agent-vcs-diff.jpg`, `pando-webui-chat-modified-files.jpg`, `pando-webui-settings-agents-persona-selector.jpg`, replaced `pando-webui-settings-snapshots.jpg` (enabled, 2 snapshots).

## Embedding model research (Ollama 0.35, RTX 4000 SFF Ada)
Script `scratchpad/embbench.py`: 6 tasks x 8 languages (python, go, typescript, java, rust, php, csharp, shell), neutral function names, recall@8 of a plain-English query; speed on 120 chunks of 1500 chars from the pando repo.

| Model | recall@8 | chunks/s | notes |
|---|---|---|---|
| nomic-embed-text | 0.67 | ~93 | go 0/6, shell 1/6 |
| hf.co/brandtcormorant/CodeRankEmbed-Q4_K_M-GGUF:Q4_K_M | 0.875 (0.917 with query prefix) | ~100 | shell 1/6 |
| hf.co/brandtcormorant/CodeRankEmbed-Q8_0-GGUF:Q8_0 | 0.875 | ~104 | same as Q4 |
| hf.co/ggml-org/jina-embeddings-v2-base-code-Q8_0-GGUF:Q8_0 | 1.0 | ~84 | 172 MB, 30 languages, Apache 2.0 |
| qwen3-embedding:0.6b | 1.0 | ~24 | 639 MB, 1024 dims |
| hf.co/jinaai/jina-code-embeddings-0.5b-GGUF:Q8_0 | fails | - | Ollama `501 Not Implemented` on /api/embed (needs last-token pooling); CC-BY-NC |

## Product issues found (not fixed, code untouched)
- `hf.co/limcheekin/CodeRankEmbed-GGUF` returns HTTP 401 on Hugging Face (repo gone or private). It is `DefaultSetupCodeEmbeddingModel` (`internal/config/setup.go:23`) and appears in `internal/config/init.go:456`, `cmd/init.go:271`, `.pando.toml`, `.ai/remembrances.config.yaml`. New installs cannot pull it. Working replacement: `hf.co/brandtcormorant/CodeRankEmbed-Q4_K_M-GGUF:Q4_K_M`.
- Pando sends no query prefix to embedders (`internal/rag/embeddings`); CodeRankEmbed expects `Represent this query for searching relevant code: ` on queries.
- Web UI Snapshots toggle gives no hint that it controls Agent VCS nor that a restart is needed.

## Verification
`hugo --quiet --baseURL /pando-docs/` clean; link checker 29706 refs, 0 broken; visual check of es guides review-and-undo, decision-model and the es embedding-models reference.
Not confirmed in the running app: persona actually switching with Auto + decision model; the Pull button (model was already installed); restart requirement inferred from source.
