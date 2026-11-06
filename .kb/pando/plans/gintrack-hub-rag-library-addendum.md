---
created_at: 2026-11-08T13:00:55.808120706Z
updated_at: 2026-11-08T13:00:55.808120706Z
tags:
    - plan
    - gintrack-hub
---
# gintrack hub: anexo al cierre del contrato (2026-11-08)

Complementa [[pando/plans/gintrack-hub-rag-library.md]].

- Almacenamiento de memoria en el hub:
  - un fichero markdown por key en git;
  - un commit síncrono por proyecto y lote antes de responder 200 (at-least-once exige que lo aceptado sea durable);
  - `version` monotónico por proyecto, guardado en el frontmatter para que los cursores sobrevivan a una reconstrucción del índice;
  - `Record.id` estable (id de gintrack); memsync empareja por `local_id`.
- Bloqueante pendiente de José: alchemai-sdk es privado y git-in-track público. Importar `wire/memory` rompe `go mod download` a terceros. Propuesta: publicar `wire/memory` como módulo público (solo stdlib).
- Bucle de duplicados: un Pando que indexa el repo gintrack produce eventos de origen `sync`/`watcher`. Se evita con la configuración existente de core `[Extensions.Memory] Origins = ["tool","api"]` (y `Paths` si hace falta), sin cambios de código. `OriginRemote` ya se bloquea siempre (`internal/extensions/memory.go:217`). El hub cuenta además los repetidos como `duplicate`, como red de seguridad.
