---
created_at: 2026-06-16T12:32:14.730502476Z
updated_at: 2026-06-16T12:32:14.730502476Z
tags:
    - plan
    - tool
    - tui
    - webui
    - acp
    - feedback
    - ask-user-question
    - architecture
---
# Plan: Tool `AskUserQuestion` (feedback interactivo del usuario)

Created: 2026-06-16. Status: **COMPLETO — Fases 1-6 implementadas (2026-06-16).**

### Progreso
- **Fase 1 ✅** `internal/userinput/userinput.go` + `userinput_test.go` (Ask bloqueante, Respond, Cancel, PendingRequests; tests en verde).
- **Fase 2 ✅** `internal/llm/tools/ask_user_question.go` + test (modo ACP devuelve texto, modo UI bloquea con metadata estructurada, validación). Registrada en `CoderAgentTools`/`CoderAgentToolsWithMesnada` y en `alwaysIncludedTools` (`tools.AskUserQuestionToolName`).
- **Fase 3 ✅** `internal/app/app.go`: campo `App.UserInput`, init `userinput.NewService()`, propagado a `CoderAgentToolsWithMesnada`.
- **Fase 4 ✅** TUI: nuevo `internal/tui/components/dialog/ask_question.go` (`AskQuestionDialogCmp`, navegación ↑/↓, single/multi-select, "Other" con `textinput`, pantalla de resumen, `QuestionResponseMsg`). Wiring en `internal/tui/tui.go` (campos `showAskQuestion`/`askQuestion`, init, `pubsub.Event[userinput.QuestionRequest]` → overlay, `QuestionResponseMsg` → `Respond`/`Cancel`, bloqueo de teclas, help section, `PlaceOverlay`, guard de ratón). Subscriber `userinput` en `cmd/root.go`. Committed en `777aec74`.
- **Fase 5 ✅** Web UI. Backend: `internal/api/handlers_chat.go` (`streamSessionEvents` suscribe `UserInput`, replay `PendingRequests`, nuevo `writeQuestionRequest` → SSE `question_request`); nuevo `internal/api/handlers_questions.go` (`POST /api/v1/questions/respond`, body `{id, sessionId, answers[], cancelled}` → `Respond`/`Cancel`); ruta en `routes.go`. Frontend: tipos `QuestionRequest`/`QuestionItem`/`QuestionOption`/`QuestionAnswer` + evento `question_request` en `types/index.ts`; parser en `services/sse.ts`; store `pendingQuestions`/`addQuestionRequest`/`respondQuestion`/`cancelQuestion` en `stores/sessionStore.ts`; handler en `hooks/useChat.ts`; nuevo `components/chat/QuestionDialog.tsx` (navegación multi-pregunta, single/multi, "Other", resumen, confirmar/cancelar) montado en `MainLayout.tsx`. Bundle embebido reconstruido (`bun run build:embedded` → `internal/api/webui/dist`).
- **Fase 6 ✅** ACP + cierre. Hallazgo: `tools.ACPClientConnContextKey` **nunca se setea en producción** (`NewACPClientConnection` no se llama), así que en ACP la tool caería al camino bloqueante `userInput.Ask()` y se colgaría (sin frontend ACP suscrito). Como `tools→acp` es dependencia unidireccional (acp NO puede importar tools → ciclo), se define `acp.ACPModeContextKey{}` (exportada en `internal/mesnada/acp/prompt_handler.go`) y se setea en `processPromptWithAgent` antes de `agentService.Run` (propaga vía `genCtx` de agent.go a cada tool). La tool detecta ACP con `ctx.Value(acp.ACPModeContextKey{}) != nil || ctx.Value(ACPClientConnContextKey) != nil`. Config opcional `[InternalTools] AskUserQuestionDisabled` (default false = habilitado) gateando el registro vía `maybeAskUserQuestionTool`/`askUserQuestionEnabled` en `tools.go`. Tests añadidos: `TestAskUserQuestion_ACPModeKey_ReturnsText`. Doc de feature en KB `pando/features/ask_user_question.md`.
- Verificado: `go build ./...`, `go vet ./internal/llm/tools ./internal/llm/agent ./internal/mesnada/acp ./internal/config`, `go test ./internal/userinput ./internal/llm/tools ./internal/llm/agent ./internal/api ./internal/mesnada/acp`, `tsc --noEmit` (web-ui).

## Context

Pando necesita que el agente pueda **hacer preguntas al usuario y esperar su respuesta** a mitad
de una tarea (igual que la tool `AskUserQuestion` de Claude Code). Hoy el agente solo puede pedir
aprobaciones binarias (allow/deny) vía el sistema de permisos; no hay forma de que pregunte
"¿qué enfoque prefieres?" con un menú de opciones seleccionables.

Objetivo: una tool llamada **`AskUserQuestion`** (mismo nombre que Claude Code) que:
- Presenta una o varias preguntas, cada una con opciones seleccionables tipo menú.
- En **TUI** y **Web UI**: overlay encima del chat usando componentes de selección de `bubbles`;
  el usuario navega pregunta a pregunta, selecciona, y al final ve un **resumen** para confirmar o
  volver a editar. La tool **bloquea** hasta recibir la respuesta.
- En **ACP** (sin tipo de mensaje de selección): la tool **formatea las preguntas/opciones como
  texto** y termina el turno; el usuario responde por escrito en el siguiente mensaje (decisión
  confirmada con el usuario: "devolver texto y terminar turno").
- Paridad completa con Claude Code: `multiSelect` por pregunta, `header` corto, `description` por
  opción, y opción **"Other"** automática para texto libre.

El patrón arquitectónico de referencia es el **sistema de permisos** (`internal/permission`): una
tool bloquea en un canal, publica un evento pubsub, el frontend muestra un overlay y responde,
desbloqueando la tool. La única diferencia es que `AskUserQuestion` devuelve **datos
estructurados** (opciones elegidas) en lugar de un `bool`. Relacionado con la feature reciente
"Interactive Agent Loop Steering" (pando/features/agent_loop_steering.md).

## Arquitectura (espejo del flujo de permisos)

```
Tool.Run --Ask()--> userinput.Service --Publish--> pubsub broker
   ^ (bloquea en chan AskResponse)                      |
   |                                          TUI overlay / Web SSE
   |                                                     |
   +------------- Respond(id, answers) <----- QuestionResponseMsg / POST
```

ACP no participa en el bloqueo: la tool detecta el contexto ACP y devuelve texto directamente.

---

## Fase 1 — Servicio `internal/userinput` (núcleo, bloqueante)

Nuevo paquete `internal/userinput/userinput.go`, modelado sobre `internal/permission/permission.go`.

Tipos:
- `Option { Label, Description string }`
- `Question { ID, Header, Question string; MultiSelect bool; Options []Option }`
- `QuestionRequest { ID, SessionID string; Questions []Question }` — payload del evento pubsub.
- `Answer { QuestionID string; Selected []string; OtherText string }`
- `AskResponse { Answers []Answer; Cancelled bool }`
- `CreateAskRequest { SessionID string; Questions []Question }`

Interface `Service` (paralela a `permission.Service`):
- `pubsub.Suscriber[QuestionRequest]`
- `Ask(req CreateAskRequest) AskResponse` — genera ID (uuid), `respCh := make(chan AskResponse, 1)`,
  lo guarda en `sync.Map`, `trackPending`, `Publish(pubsub.CreatedEvent, ...)`, bloquea en `<-respCh`
  (patrón de `permissionService.Request`, permission.go:180-193).
- `Respond(id string, resp AskResponse)` — envía por canal y limpia (espejo de `Grant`/`Deny`).
- `Cancel(id string)` — `Respond(id, AskResponse{Cancelled: true})`.
- `PendingRequests(sessionID string) []QuestionRequest` — replay para reconexión Web.
- `NewService() Service` — `pubsub.NewBroker[QuestionRequest]()`.

Tests `internal/userinput/userinput_test.go`: bloqueo+respond, cancel, pending replay
(basados en `internal/permission/permission_test.go`).

---

## Fase 2 — Tool `AskUserQuestion`

Nuevo `internal/llm/tools/ask_user_question.go` implementando `tools.BaseTool`
(interface en `internal/llm/tools/tools.go:79`).

- Const `AskUserQuestionToolName = "AskUserQuestion"`.
- `askUserQuestionTool struct { userInput userinput.Service }` + `NewAskUserQuestionTool(userInput)`.
- `Info()`: schema con paridad Claude Code:
  - `questions`: array (required). Cada item: `question` (string, req), `header` (string ≤12 chars, req),
    `multiSelect` (bool, default false), `options` (array 2-4 de `{label, description}`).
- `Run(ctx, call)`:
  1. Parsea params; valida (1-4 preguntas, 2-4 opciones).
  2. `sessionID, _ := tools.GetContextValues(ctx)` (tools.go:94).
  3. Modo ACP: si `ctx.Value(tools.ACPClientConnContextKey) != nil` (patrón usado en bash.go:318,
     write.go:116, view.go:125) -> formatea preguntas+opciones como markdown numerado y devuelve
     `NewTextResponse(...)` pidiendo respuesta escrita. No bloquea, termina el turno.
  4. Modo UI (resto): `resp := t.userInput.Ask(CreateAskRequest{SessionID, Questions})` (bloquea).
     Si `resp.Cancelled` -> `NewTextErrorResponse("user cancelled")`. Si no, formatea las selecciones
     (incluyendo `OtherText`) como texto estructurado/JSON, vía `WithResponseMetadata`.

Registro en `internal/llm/agent/tools.go`:
- Añadir `tools.NewAskUserQuestionTool(userInput)` en `CoderAgentTools` (≈ línea 147) y en
  `CoderAgentToolsWithMesnada` (≈ línea 180). Requiere pasar `userinput.Service` a estas funciones.
- Añadir `"AskUserQuestion": true` a `alwaysIncludedTools` (tools.go:44) para que el context-trimmer
  no la elimine.

---

## Fase 3 — Wiring en `internal/app/app.go`

- Añadir campo `UserInput userinput.Service` al struct `App` (junto a `Permissions`, app.go:67).
- Inicializar `UserInput: userinput.NewService()` (junto a `Permissions:`, app.go:192).
- Pasar `app.UserInput` a `CoderAgentToolsWithMesnada(...)` (app.go:565) y propagar el nuevo
  parámetro en las firmas afectadas (solo CoderAgent lo necesita; TaskAgent no).

---

## Fase 4 — TUI

Nuevo componente `internal/tui/components/dialog/ask_question.go` (modelado sobre `dialog/permission.go`):
- Interface `AskQuestionDialogCmp` (tea.Model, layout.Bindings, `SetRequest(userinput.QuestionRequest) tea.Cmd`).
- Estado: `request`, `currentIdx`, `selections map[int]map[int]bool`, `otherText map[int]string`,
  `mode` (`asking` | `summary`), `textinput.Model` para "Other", y `list.Model` (bubbles/list) o
  selección por índice para las opciones.
- Navegación: `↑/↓` mover, `espacio` alternar (multi) / seleccionar (single), `enter`/`→`/`tab`
  siguiente pregunta, `←` anterior. Última pregunta -> `mode = summary`.
- Resumen: lista preguntas+selecciones; `enter` confirma, `e`/`←` vuelve a editar.
- "Other": al seleccionarla muestra `textinput` para texto libre.
- Emite `QuestionResponseMsg { RequestID string; Answers []userinput.Answer; Cancelled bool }`
  (espejo de `PermissionResponseMsg`, permission.go:31). `esc` -> Cancelled.

`internal/tui/tui.go` (espejo del flujo de permisos):
- Campos `askQuestion` + `showAskQuestion bool`; init en `NewModel` (≈ tui.go:2343).
- `case pubsub.Event[userinput.QuestionRequest]:` -> show + `SetRequest` (espejo tui.go:538).
- `case dialog.QuestionResponseMsg:` -> `a.app.UserInput.Respond(...)`/`Cancel`; hide (espejo tui.go:541).
- Render overlay en `View()` con `layout.PlaceOverlay` (espejo tui.go:1913-1926).
- Propagar `WindowSizeMsg`/bindings como permission (tui.go:303, 1227, 1810).

`cmd/root.go`: añadir `setupSubscriber(ctx, &wg, "userinput", app.UserInput.Subscribe, ch)` en
`setupSubscriptions` (junto a root.go:510) para convertir eventos del broker en `tea.Msg`.

---

## Fase 5 — Web UI

Backend (`internal/api`):
- `handlers_chat.go` `streamSessionEvents` (≈ línea 276): suscribir `s.app.UserInput.Subscribe(clientCtx)`,
  replay `PendingRequests`, emitir SSE `question_request`. Nuevo helper `writeQuestionRequest`
  (espejo de `writePermissionRequest`, handlers_chat.go:319).
- Nuevo `handlers_questions.go`: `POST /api/v1/questions/respond` con body
  `{ id, sessionId, answers:[{questionId, selected[], otherText}], cancelled }` -> `Respond(...)`
  (espejo de `handlePermissionRespond`).
- `routes.go`: registrar ruta (junto a routes.go:20).

Frontend (`web-ui/` fuente React; recompila a `internal/api/webui/dist`):
- Componente panel de preguntas encima del chat (similar al de permisos): navegación, single/multi,
  "Other" con input, resumen + confirmar.
- Escuchar SSE `question_request` (store junto a `pendingPermissions`).
- `POST /api/v1/questions/respond` al confirmar/cancelar.
- Nota: requiere rebuild del bundle (`dist` está compilado).

---

## Fase 6 — ACP + cierre

- ACP: cubierto por la detección de contexto en la tool (Fase 2.3). Verificar que el agente ACP
  propaga `tools.ACPClientConnContextKey` al ctx de ejecución de tools (`internal/mesnada/acp/agent.go`);
  si no, añadirlo al ctx antes de ejecutar tools.
- (Opcional) Config `[Tools] AskUserQuestionEnabled` (default true).
- Docs: guardar feature en KB (`pando/features/ask_user_question.md`) y memoria del proyecto.

---

## Verificación

1. Tests Go: `go test ./internal/userinput`; `go test ./internal/llm/agent ./internal/api`
   (comando verificado); test de la tool en modo ACP (texto) y modo UI (con `Respond` en goroutine).
2. TUI manual: pedir al agente usar `AskUserQuestion`; comprobar overlay, navegación multi-pregunta,
   multiselect, "Other", resumen, confirmación; verificar que el resultado llega al LLM.
3. Web UI: misma prueba vía navegador; SSE `question_request`, panel, POST respond; probar reconexión
   (replay de `PendingRequests`).
4. ACP (Zed/VS Code): la tool emite texto de preguntas/opciones y el turno termina; la respuesta
   escrita continúa la tarea.

## Archivos clave
- Nuevos: `internal/userinput/userinput.go` (+test), `internal/llm/tools/ask_user_question.go` (+test),
  `internal/tui/components/dialog/ask_question.go`, `internal/api/handlers_questions.go`, componente React en `web-ui/`.
- Modificados: `internal/llm/agent/tools.go`, `internal/app/app.go`, `internal/tui/tui.go`,
  `cmd/root.go`, `internal/api/handlers_chat.go`, `internal/api/routes.go`,
  (verificación) `internal/mesnada/acp/agent.go`.

## Decisiones del usuario (2026-06-16)
- ACP: "Devolver texto y terminar turno" (sin estado bloqueante en ACP).
- Opciones: "Paridad completa" con Claude Code (multiSelect, header, description, "Other" automático).
