# Plan: Estandarización de mensajes ACP en Pando (basado en opencode)

> Fecha: 2026-05-29  
> Fuente de referencia: `.kb/research/acp-opencode-message-structure.md`  
> Estado: PLANIFICADO

---

## 1. Análisis comparativo: opencode vs pando

### 1.1 Lo que opencode hace (estándar de referencia)

| Mensaje ACP | Campo clave | Comportamiento opencode |
|-------------|-------------|------------------------|
| `agent_message_chunk` | `messageId` | Incluye `messageId` (UUID) en cada chunk para agrupar fragmentos del mismo mensaje |
| `agent_thought_chunk` | `messageId` | Incluye `messageId` compartido con el mensaje del mismo turno |
| `user_message_chunk` | `messageId` | Emitido al recibir el prompt del usuario |
| `tool_call` | `rawInput` | Siempre estructurado como objeto JSON (igual que Pando) |
| `tool_call_update` (failed) | `rawOutput.error` | Usa `rawOutput.error` para fallos, no `rawOutput.output` |
| `edit` tool | `rawOutput.metadata.filediff` | Incluye `filediff` con `before`/`after`/`additions`/`deletions` |
| `bash` tool | título | Usa el comando real como título: `input.command ? input.command : "Terminal"` |
| `grep`/`glob` | `locations` | `[{ "path": input.path }]` cuando path está disponible |

**Nota importante:** Opencode sí usa `rawInput` estructurado como objeto JSON, igual que Pando. El problema en Zed que menciona ("no muestra los datos de input de una tool") sugiere que hay otros factores que afectan la renderización en Zed (posiblemente related al lifecycle, timing, o cómo se muestran los datos en la UI).

### 1.2 Estado actual de Pando

**Lo que Pando ya hace bien:**
- ✅ `rawInput` estructurado como objeto (igual que opencode)
- ✅ `rawOutput: { output, metadata }` en completados
- ✅ `tool_call` antes de cualquier `tool_call_update`
- ✅ Lifecycle: `pending` → `in_progress` → `completed/failed`
- ✅ `ToolDiffContent` para `edit`/`write`
- ✅ Terminal `_meta` (terminal_info, terminal_output, terminal_exit) para bash
- ✅ `UpdatePlan` en lugar de `StartToolCall` para `TodoWrite`
- ✅ `sendCurrentModeUpdate("plan")` antes del `UpdatePlan`
- ✅ Replay de historial con `streamSessionHistory`
- ✅ Síntesis de `StartToolCall` cuando el start fue perdido

**Gaps identificados vs opencode:**

| Gap | Severidad | Archivo(s) afectado(s) |
|-----|-----------|------------------------|
| **G1**: `messageId` ausente en `agent_message_chunk` / `agent_thought_chunk` | Alta | `prompt_handler.go`, `session_state.go` |
| **G2**: `user_message_chunk` no se emite al recibir el prompt | Alta | `prompt_handler.go` |
| **G3**: `rawOutput.error` no se usa en fallos de bash/tools; usa `output` siempre | Media | `prompt_handler.go`, `session_state.go` |
| **G4**: `rawOutput.metadata.filediff` ausente en `edit` completado | Media | `prompt_handler.go`, `session_state.go` |
| **G5**: Título de bash usa nombre de herramienta genérico, no el comando | Media | `tool_render.go` |
| **G6**: Terminal metadata no está capability-gated de forma consistente en replay | Baja | `session_state.go` |
| **G7**: `user_message_chunk` no se emite en replay de historial | Baja | `session_state.go` |
| **G8**: `agent_message_chunk`/`agent_thought_chunk` carecen de `messageId` en replay | Baja | `session_state.go` |

---

## 2. Descripción detallada de cada gap

### G1: messageId ausente en agent_message_chunk / agent_thought_chunk

**Opencode behavior:**
```json
{
  "sessionUpdate": "agent_message_chunk",
  "messageId": "msg_asst_001",
  "content": { "type": "text", "text": "..." }
}
```

**Pando actual:**
```go
acpsdk.UpdateAgentMessageText(event.Delta)  // sin messageId
```

El ACP SDK Go (`SessionUpdateAgentMessageChunk`) tiene campo `MessageId *string` (UNSTABLE pero implementado). Sin él, los clientes que agrupan chunks por messageId (como Zed) pueden tener problemas al unir fragmentos de distintos mensajes.

**Fix:** Usar `msg.ID` (de `message.Message`) como `messageId` al emitir chunks de contenido en modo streaming. Crear helper `UpdateAgentMessageTextWithID(text, msgID string)`.

### G2: user_message_chunk no se emite al recibir el prompt

**Opencode behavior:** Cuando llega el prompt del usuario, emite `user_message_chunk` antes de procesar.

**Pando actual:** El prompt del usuario se procesa directamente sin emitir `user_message_chunk`. En `streamSessionHistory` sí se replayan mensajes de usuario, pero en live no se emite este update al recibir el prompt.

**Fix:** En `HandlePrompt` / `processPromptWithAgent`, antes de llamar al agente, emitir `acpsdk.UpdateUserMessageText(promptText)` con el `messageId` de la sesión actual.

### G3: rawOutput.error en fallos

**Opencode behavior (failed):**
```json
{
  "rawOutput": {
    "error": "Command 'nonexistent' not found.",
    "metadata": {}
  }
}
```

**Pando actual:**
```go
rawOutput := map[string]interface{}{
    "output": tr.Content,  // siempre "output", incluso en errores
}
```

**Fix:** Cuando `tr.IsError == true`, usar `"error"` como clave en lugar de `"output"` para alinear con el estándar opencode.

### G4: rawOutput.metadata.filediff en edit completado

**Opencode behavior:**
```json
{
  "rawOutput": {
    "output": "File edited successfully",
    "metadata": {
      "filediff": {
        "file": "src/component.tsx",
        "before": "  gap: 12px",
        "after": "  gap: 18px",
        "additions": 1,
        "deletions": 1
      }
    }
  }
}
```

**Pando actual:** Solo usa `ToolDiffContent` en el campo `content`, pero `rawOutput.metadata` no contiene el `filediff`. El diff visual existe pero no en el formato estándar de rawOutput.

**Fix:** Añadir `rawOutput.metadata.filediff` con `{ file, before, after, additions, deletions }` para herramientas `edit`. Para `write`, puede incluir `{ file, additions: linesCount }`.

### G5: Título de bash usa nombre genérico

**Opencode / claude-agent-acp behavior:**
```
title: "bun test --filter session"  // el comando real
```

**Pando actual (`tool_render.go:toolDisplayTitle`):**
```
title: "bash"  // o "Bash" — título genérico
```

**Fix:** En `toolDisplayTitle` para bash tools, usar el campo `command` del rawInput como título cuando está disponible y es suficientemente corto (ej. max 80 chars).

### G6 & G7: Replay - user_message_chunk y terminal capability gating

**G6:** En `streamSessionHistory`, los mensajes de usuario (role=User) ya emiten `UpdateUserMessageText`, pero el campo `messageId` no se pobla.

**G7:** Terminal metadata en el replay no tiene mecanismo de capability gating consistente (el campo `a.terminalOutputEnabled()` sí existe, pero debe verificarse que funcione también en el contexto de replay).

### G8: messageId en replay

En `streamSessionHistory`, los `UpdateAgentMessageText` y `UpdateAgentThoughtText` no incluyen `messageId`. Deben usar `msg.ID` como `messageId`.

---

## 3. Plan de implementación por fases

### Fase 1 — Alta prioridad: messageId + user_message_chunk (estándar básico)

**Archivos:** `prompt_handler.go`, `session_state.go`, posiblemente nuevo helper en `tool_render.go`

#### 1a. Añadir messageId a agent_message_chunk y agent_thought_chunk

En `processAgentEventStream`:
```go
// En AgentEventTypeContentDelta:
update := SessionUpdate{AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{
    Content:   acpsdk.TextBlock(event.Delta),
    MessageId: &currentMessageID,  // nuevo campo
}}
```

Donde `currentMessageID` se actualiza en `AgentEventTypeResponse` con `event.Message.ID`.

En `processAgentResponse`:
```go
// Pasar el msgID al enviar content/reasoning completo
update := SessionUpdate{AgentMessageChunk: &acpsdk.SessionUpdateAgentMessageChunk{
    Content:   acpsdk.TextBlock(content.String()),
    MessageId: &msg.ID,
}}
```

#### 1b. Emitir user_message_chunk al recibir prompt

En `HandlePrompt` o `processPromptWithAgent`, antes de llamar al agente:
```go
userMsgID := generateMessageID()  // o recuperar de la sesión pando
acpSession.SendUpdate(SessionUpdate{UserMessageChunk: &acpsdk.SessionUpdateUserMessageChunk{
    Content:   acpsdk.TextBlock(promptText),
    MessageId: &userMsgID,
}})
```

#### 1c. messageId en streamSessionHistory

Para cada mensaje de usuario/asistente en el replay, usar `msg.ID` como `MessageId`.

---

### Fase 2 — Media prioridad: rawOutput.error + filediff

**Archivos:** `prompt_handler.go`, `session_state.go`

#### 2a. rawOutput key error vs output

Crear helper:
```go
func buildRawOutput(content string, metadata string, isError bool) map[string]interface{} {
    key := "output"
    if isError {
        key = "error"
    }
    out := map[string]interface{}{key: content}
    if metadata != "" {
        var m interface{}
        if json.Unmarshal([]byte(metadata), &m) == nil {
            out["metadata"] = m
        } else {
            out["metadata"] = metadata
        }
    }
    return out
}
```

Sustituir los dos bloques `rawOutput` duplicados en `processAgentEventStream` y `processAgentResponse`.

#### 2b. rawOutput.metadata.filediff para edit

En la sección de `ToolResult` para edit tools:
```go
if isEditTool(tr.Name) && !tr.IsError && storedInput != "" {
    var ep editToolInput
    if json.Unmarshal([]byte(storedInput), &ep) == nil && ep.FilePath != "" {
        if tr.Name == "edit" {
            // Contar líneas para additions/deletions
            rawOutput["metadata"] = map[string]interface{}{
                "filediff": map[string]interface{}{
                    "file":      ep.FilePath,
                    "before":    ep.OldString,
                    "after":     ep.NewString,
                    "additions": countLines(ep.NewString),
                    "deletions": countLines(ep.OldString),
                },
            }
        }
    }
}
```

---

### Fase 3 — Media prioridad: bash title + corrección de títulos

**Archivos:** `tool_render.go`

#### 3a. Título de bash como comando real

En `toolDisplayTitle`:
```go
case "bash", "execute_command":
    if m, ok := rawInput.(map[string]interface{}); ok {
        if cmd, ok := m["command"].(string); ok && cmd != "" {
            if len(cmd) > 80 {
                cmd = cmd[:77] + "..."
            }
            return cmd
        }
    }
    return "Terminal"
```

---

### Fase 4 — Baja prioridad: Unificación live/replay + tests

**Archivos:** `prompt_handler.go`, `session_state.go`, `agent_pando_test.go`

#### 4a. Extraer helper compartido para rawOutput

Factorizar `buildRawOutput` en `tool_render.go` para que tanto `prompt_handler.go` como `session_state.go` lo usen.

#### 4b. Tests de lifecycle ACP con messageId

Añadir tests en `agent_pando_test.go`:
- `TestAgentMessageChunkHasMessageId`: verifica que cada chunk tiene `messageId`
- `TestUserMessageChunkEmittedOnPrompt`: verifica que se emite `user_message_chunk`
- `TestRawOutputErrorKeyOnFailure`: verifica `rawOutput.error` vs `rawOutput.output`
- `TestEditToolRawOutputFilediff`: verifica `rawOutput.metadata.filediff`
- `TestBashTitleUsesCommand`: verifica que el título de bash usa el comando

---

## 4. Resumen de cambios por archivo

| Archivo | Cambios |
|---------|---------|
| `internal/mesnada/acp/prompt_handler.go` | G1 (messageId en streaming), G2 (user_message_chunk), G3 (rawOutput error key) |
| `internal/mesnada/acp/session_state.go` | G1 (messageId en replay), G7 (terminal gating replay), G8 (messageId replay) |
| `internal/mesnada/acp/tool_render.go` | G3 (buildRawOutput helper), G4 (filediff), G5 (bash title) |
| `internal/mesnada/acp/agent_pando_test.go` | Tests para G1, G2, G3, G4, G5 |

---

## 5. Orden de ejecución recomendado

```
Fase 1 → Fase 2 → Fase 3 → Fase 4
```

Cada fase es independiente y desplegable por separado. La Fase 1 es la de mayor impacto de compatibilidad con clientes ACP estándar (Zed, etc.).

---

## 6. Checklist de cumplimiento ACP post-implementación

- [ ] `agent_message_chunk` incluye `messageId` (UUID del mensaje)
- [ ] `agent_thought_chunk` incluye `messageId` compartido
- [ ] `user_message_chunk` se emite al recibir prompt (live)
- [ ] `user_message_chunk` se emite en replay de historial
- [ ] `rawOutput.error` se usa cuando `isError == true`
- [ ] `rawOutput.metadata.filediff` presente para herramienta `edit`
- [ ] Título de bash usa el comando real cuando está disponible
- [ ] Todos los cambios tienen tests en `agent_pando_test.go`
- [ ] Live y replay producen la misma forma de payload (paridad)

---

## 7. Referencias

- Documento fuente: `.kb/research/acp-opencode-message-structure.md`
- Análisis previo: `.kb/pando/analysis/acp-tool-call-compatibility-improvements-from-opencode-and-claude-agent-acp-2026-05-25.md`
- ACP SDK Go: `/www/MCP/Pando/acp-go-sdk/helpers.go`, `types_gen.go`
- Implementación actual: `internal/mesnada/acp/prompt_handler.go`, `session_state.go`, `tool_render.go`