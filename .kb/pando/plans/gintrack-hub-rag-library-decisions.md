---
created_at: 2026-11-08T15:33:46.69241101Z
updated_at: 2026-11-08T15:33:46.69241101Z
tags:
    - plan
    - gintrack-hub
    - decision
---
# gintrack hub: decisiones finales de José (2026-11-08, vía Pips)

Complementa [[pando/plans/gintrack-hub-rag-library.md]] y [[pando/plans/gintrack-hub-rag-library-addendum.md]].

- **Q3**: memsync, en el Pando conectado al hub, se configura sin scopes `user/`.
- **Q4**: `alchemai-sdk/wire` se publica como módulo público con go.mod propio; el SDK privado lo reexporta. Esto desbloquea que gintrack (público) importe `wire/memory`.
- **Q5**: el hub NO rechaza secretos en servidor; la redacción es responsabilidad del cliente. El hueco de redacción de memsync pasa a ser una tarea del equipo de AlchemAI. Nota enviada a Pips (#39):
  - `memsync/worker.go:266` (`Enqueue`) y `memsync/pipeline.go` no redactan;
  - la redacción solo existe en `memorysync/redact.go`, y memorysync se aparta cuando memsync está activo (`memorysync/extension.go:166`);
  - fix propuesto: redactor en un paquete común, aplicado antes de `privacy.Prepare`, `ContentHash` y `spool.Append`, y también en `pipelinePayload`.
- **Q6**: OAuth MCP entra en la PoC del hub.
- El contrato RAG y el almacenamiento del hub quedan cerrados tal como están.
