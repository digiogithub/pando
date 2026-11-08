---
created_at: 2026-11-08T12:58:04.214880423Z
updated_at: 2026-11-08T12:58:04.214880423Z
tags:
    - plan
    - architecture
    - gintrack-hub
    - rag
---
# gintrack hub: rol de Pando (librería RAG + cliente memsync) — 2026-11-08

Análisis y diseño (sin cambios de código) pedido por José vía Pips (agentd) y coordinado con claude@gintrack. Relacionado: [[pando/features/extension-memory-capability]], alchemai-agent memsync.

## Decisiones
- gintrack hub es una PoC que avanza en paralelo a AlchemAI Mem. Vive en el repo de gintrack (`gintrack hub`).
- Pando exporta su RAG como **módulo Go anidado `github.com/digiogithub/pando/rag`**, con go.mod propio, en lugar de `pkg/` del módulo raíz. Así se evita arrastrar todo el go.mod de Pando por MVS. Usa el driver modernc.
- El hub usa **una SQLite por proyecto**; la búsqueda de organización fusiona los proyectos con RRF mediante `kb.Fuse`.
- El hub implementa el **lado servidor del protocolo AlchemAI Mem `alchemai.memory.v2`** (batch, search, changes, enrich). Pando se conecta con el cliente memsync de alchemai-agent sin extensión nueva: solo configura endpoint y token.
- El hub es estado; mesnada y la ejecución de agentes se quedan en local.

## Contrato RAG acordado (resumen)
- `kb.Open(db, Options)` y `Close`; `Put`, `Get`, `Delete` y `DeletePrefix`, con el path como id.
- `SyncTree(fs.FS)`. `ApplyChanges(fsys, []Change{Op,Path,OldPath,Blob,Content}, watermark)`, que guarda la marca de agua por commit en la misma transacción. `Watermark()`.
- `Search(q, Query{PathPrefixes, ExcludePrefixes, Scopes, Kinds, Statuses (default active), Tags, Authors, Mode})`, con los filtros aplicados en SQL.
- `Hit{Path, Chunk, StartLine, EndLine, Score, Kind, Status, Scope, Author, Rev, GitBlob, Meta}`.
- La memoria son documentos con `kind=memory`; `UpsertMemory` queda como fachada para Pando. El chunking quita el frontmatter y no parte los bloques `### SPEC.Rn`.
- `Embedder` añade `Model()`, guardado por chunk.
- Migración aditiva: columnas `author`, `rev`, `git_blob`, `kind`, `status`, `meta`; `start_line`, `end_line` y `embedding_model` en chunks; tabla `kb_state`.
- Vectores: `kb.VectorIndex` lineal por defecto. sqlite-vec no es viable con modernc; cuando haga falta, HNSW a partir de unos 50k chunks por proyecto.

## Cambios necesarios en Pando (pendientes)
- Mover `internal/rag/kb` (salvo watcher, selfwrite y filesystem mirror), `internal/rag/embeddings`, `internal/convert` y las migraciones kb/events/memory al módulo.
- Cortar las dependencias de `internal/logging` (pasar a slog), `internal/db.RunTx` (copiarla) y `config.Get()` (se queda en `rag/service.go`).
- Aplicar el filtro de scope en SQL. Hoy `filterByScope` (kb.go:713) se aplica después de fusionar y devuelve de menos.
- Rellenar `ProjectID` en `RemembranceQuery` (`internal/extensions/memory_search.go:61`).
- Esfuerzo: fase 1 (extraer el módulo) 4–5 d; fase 2 (API del contrato) 4–5 d; configuración de memsync ~1 d; ANN diferible, 3–5 d.

## Extensiones aditivas propuestas al protocolo Mem (en el hub, sin tocar SDK ni Mem)
- Propuestas: `Record.status`, `BatchResponse.items`, `/v1/memory/proposals` y perm `propose`.
- Concurrencia optimista: `base_version` y razón de rechazo `stale_revision`.
- Binarios vía `asset_id`/`MediaAsset`.
- Descubrimiento con `GET /v1/memory/capabilities`; Mem devuelve 404.
- Las vistas de gintrack quedan fuera del protocolo.

## Hallazgo
En alchemai-agent, cuando memsync está activo, memorysync se aparta (`memorysync/extension.go:166`) y memsync no aplica redacción (`memsync/worker.go:266`). El contenido sale sin redactar. Avisado a Pips.

## Deuda técnica (Fase 1)
- `watcher.go` (fsnotify), `selfwrite.go` y `filesystem.go` (mirror) se han movido a `rag/kb` porque son métodos de `KBStore`. El hub no los necesita: reindexa por commit con `ApplyChanges`. Separarlos en un paquete aparte o devolverlos a Pando es un cambio de diseño pendiente.

## Deuda técnica (Fase 2)
- `ApplyChanges` (y `SyncTree` sin `Include`) solo indexan `.md`. Vale para el PoC. Para indexar los pdf/docx del hub hay que habilitar `Include` y el `Converter` en `ApplyChanges`. Hoy el converter trabaja con rutas de disco, no con `fs.FS`, así que hay que adaptarlo (leer el blob a un fichero temporal o pasar el `io.Reader`).
- Las memorias del hub (ADR-044) se indexan como ficheros normales: `type: memory` pasa a kind=memory y `status:` pasa a status. Se buscan con `Search(Kinds: [memory], Statuses: [active])` sin pasar por `UpsertMemory`, que queda solo como fachada de Pando.
