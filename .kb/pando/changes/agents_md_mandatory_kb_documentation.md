---
created_at: 2026-06-19T05:29:18.053594602Z
updated_at: 2026-06-19T05:29:18.053594602Z
tags:
    - change
    - documentation
    - process
---
# Cambio: reforzar en AGENTS.md/CLAUDE.md la documentación obligatoria en KB

Fecha: 2026-06-19

## Qué se cambió
Se reforzó la política de documentación activa del proyecto añadiendo una regla
explícita y obligatoria de registrar un resumen con `kb_add_document` tras
cualquier tarea de modificación o implementación de software.

## Archivos tocados
- `AGENTS.md` (y `CLAUDE.md`, que se mantiene sincronizado con el mismo contenido).
  - Nueva sección `### MANDATORY: document after any modification or implementation task`,
    situada justo después de `### MANDATORY: search before any task that needs prior context`
    para darle el mismo peso. Establece:
    - Obligatorio guardar resumen con `kb_add_document` tras cualquier
      modificación/implementación/fix/refactor, incluso cambios de una línea.
    - El resumen debe capturar: qué se cambió, ficheros/símbolos tocados, motivo y
      cómo se verificó.
    - `file_path` claro (`pando/changes/…`, `pando/fixes/…`, `pando/features/…`);
      actualizar documentos existentes en vez de duplicar; `remember` no sustituye
      al resumen.
    - Nota de plan mode: durante el plan mode del harness solo se puede editar el
      fichero de plan, así que se difiere la escritura del KB hasta salir del modo,
      pero no se omite.
  - Refuerzo en la línea `Implementation` del Development Workflow para remitir a la
    nueva sección MANDATORY.

## Motivo
Surgió a raíz de una observación del usuario: en una tarea previa (doble Ctrl+C)
el plan se guardó con el mecanismo del harness (plan mode) y no se documentó en el
KB tras salir del plan mode. El hueco estaba en que la guía no obligaba
explícitamente a documentar el resultado de cada cambio ni cubría el caso del plan
mode. Esta regla cierra ese hueco para seguir generando documentación viva.

## Verificación
- Cambio de documentación (Markdown); sin build/tests asociados.
- Revisión visual de la estructura de `AGENTS.md`.