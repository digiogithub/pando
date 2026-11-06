---
created_at: 2026-11-08T18:14:02.959803457Z
updated_at: 2026-11-08T18:14:02.959803457Z
tags:
    - change
    - refactor
    - rag
    - gintrack-hub
---
# Fase 1: extracción del módulo `github.com/digiogithub/pando/rag` (2026-11-08)

Plan: [[pando/plans/gintrack-hub-rag-library.md]], decisiones en [[pando/plans/gintrack-hub-rag-library-decisions.md]]. Bookmark jj `rag-module`, workspace `/www/MCP/Pando/pando-rag`, desde main d6d104ea. Sin push.

## Qué cambió
Pando consume un módulo Go anidado `rag/` (go.mod propio) mediante `replace github.com/digiogithub/pando/rag => ./rag`.

- `rag/sqlx`: `RunTx`, `RunTxOpts` y `RetryLoop` con un clasificador `Retryable` como parámetro. `DefaultRetryable` reconoce por texto `SQLITE_BUSY`, `SQLITE_LOCKED` y "database is locked". `internal/db.RunTx` y `retryLoop` delegan en él pasando `db.IsRetryable` (que entiende los errores del motor multiwriter).
- `rag/embeddings` (antes `internal/rag/embeddings`) y `rag/convert` (antes `internal/convert`): movidos tal cual. El paquete mantiene el nombre `embeddings`; `embed` chocaría con el `embed` de la stdlib.
- `rag/kb` (antes `internal/rag/kb`):
  - Ya no importa `internal/logging` ni `internal/db`. Usa `*slog.Logger` (`SetLogger`/`Options.Logger`, nil = `slog.Default()`) y `sqlx` con `Options.Retryable`.
  - `logging.SourceLogger()` (nuevo, `internal/logging/source_handler.go`) conserva el atributo `source=file:line` en Debug/Info, igual que `logging.Debug/Info`.
- `kb.Open(db, Options{Embedder, EmbeddingModel, ChunkSize, ChunkOverlap, Workers, DisableWikiLinks, Converter, Logger, Retryable})`. `NewKBStore` queda deprecated.
  - Pando usa `Open` en `internal/rag/service.go` y en `cmd/kb.go`. `config.Get()` se sigue resolviendo en Pando.
  - `Workers: max(IndexWorkers, 1)` mantiene que un `IndexWorkers` sin valor signifique un worker, como hacía antes `SetSyncWorkers`.
- `rag/migrate`: las 5 migraciones de kb y events, con los mismos nombres y versiones.
  - `migrate.Up` y `migrate.Pending` usan su propia tabla `rag_goose_version`, para consumidores nuevos.
  - `migrate.Merge(base, dir)` fusiona en un FS y rechaza nombres o versiones duplicados.
  - `internal/db.FS` es ahora el FS fusionado, así que la historia de goose en `goose_db_version` no cambia.
- El paquete `internal/rag/events` se queda en Pando; su migración pasa al módulo.

## Verificación
- El repo no tiene `.sluice.toml`, así que sluice verify no tiene gates. Se verificó con `go build ./...`, `go vet` y `go test` de `rag/...`, `internal/db`, `internal/rag/...`, `internal/llm/tools`, `internal/api`, `internal/app`, `internal/extensions`, `internal/session`, `internal/logging` y `cmd/...`. Todo en verde, igual que la línea base.
- `TestMergedMigrationsKeepVersionsAndOrder` fija la lista fusionada (26 versiones, mismo orden, sin colisiones). `fstest.TestFS` valida el FS fusionado.
- goose sobre copias de BD reales:
  - pando (2.4 GB) y oflama: 0 pendientes antes y después;
  - hyper-mcp: aplica 10 pendientes, incluida la `kb_chunk_embedding_meta` movida, y termina con 0;
  - las copias se borraron.
- Las versiones de dependencias del módulo son iguales o inferiores a las de Pando, así que el build list de Pando no cambia (`go.mod`/`go.sum` raíz sin cambios salvo require y replace).

## Deuda anotada
- watcher (fsnotify), selfwrite y el mirror de filesystem viven en `rag/kb` porque son métodos de `KBStore`. Separarlos (el hub reindexa por commit, no por inotify) es un cambio de diseño pendiente.
- Contrato frente a implementación: `Options.DisableWikiLinks` en lugar de `WikiLinks` (el valor cero mantiene los links activados); paquete `embeddings` en lugar de `embed`.
