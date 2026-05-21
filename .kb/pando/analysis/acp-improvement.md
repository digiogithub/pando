# Plan: Igualar la visualización de tool calls en tiempo real ACP con la del histórico de sesión

## Problema

Cuando se usa Pando por ACP en modo operación en tiempo real (streaming), las tool calls se muestran con metadatos incompletos: `title: "bash"`, `rawInput: {}`, sin `locations`, sin `content`. Sin embargo, cuando se carga el histórico de una sesión ACP, las mismas tool calls se renderizan con datos enriquecidos: `title: "ls -la /tmp"`, `rawInput: {"command": "ls -la /tmp"}`, con `locations` y `content` correctos.

## Análisis de Causa Raíz

### Flujo 1: Streaming en tiempo real (`processPromptWithAgent` → `prompt_handler.go`)

| Evento LLM | ToolCall.Input | Acción ACP | Estado visualizado |
|---|---|---|---|
| `EventToolUseStart` | `""` (vacío) | `StartToolCall` con `rawInput: {}`, `title: "bash"` | ❌ Sin enriquecer |
| `EventToolUseDelta` | se acumula en memoria | **Sin evento al frontend** | — |
| `EventToolUseStop` | JSON completo | `UpdateToolCall` con datos reales | ✅ Enriquecido |

**Problemas identificados:**
1. El `StartToolCall` inicial tiene `rawInput: {}` y `title: "bash"` porque se envía antes de
   que el input esté disponible, y el cliente ACP lo renderiza tal cual.
2. `EventToolUseDelta` no emite eventos al cliente ACP, por lo que durante la acumulación
   del input el cliente no ve ninguna actualización.
3. Si el buffer de 256 slots se llena y `EventToolUseStop` se pierde, el correctivo llega
   después en `processAgentResponse`, pero hay una ventana visible con datos incompletos.

### Flujo 2: Non-streaming (`processAgentResponse` en `prompt_handler.go` líneas 521-638)

Cuando el proveedor no emite ToolUseStart/Stop (Copilot, OpenAI, Gemini), el código
corrige enviando `StartToolCall` con los datos completos. Este path funciona bien
porque ya tiene el input completo.

### Flujo 3: Histórico de sesión (`session_state.go` líneas 238-283)

En el replay del histórico, los `message.ToolCall` ya tienen `p.Input` con el JSON
completo almacenado en BD:

| Campo | Valor en histórico | Por qué funciona |
|---|---|---|
| `rawInput` | `parseJSONInput(p.Input)` → parsed JSON | Input completo desde BD |
| `title` | `toolDisplayTitle(p.Name, rawInput, workDir)` | Calculado con datos reales |
| `locations` | `toLocations(p.Name, p.Input)` | Extraído del JSON completo |
| `content` | `toolCallContent(p.Name, rawInput)` | Extraído del JSON completo |
| `status` | `ToolCallStatusInProgress` | Correcto para replay |

Este path **no tiene el problema de timing** porque lee datos persistidos donde cada
tool call ya tiene el input completo.

## Diferencias concretas por campo

| Campo | Streaming start | Streaming in_progress | History replay |
|---|---|---|---|
| `rawInput` | `{}` (vacío) | actualizado (pero solo en stop, no en in_progress intermedio) | JSON parseado |
| `title` | nombre de la tool | actualizado con `toolDisplayTitle` | calculado con `toolDisplayTitle` |
| `kind` | ✅ correcto | ✅ correcto | ✅ correcto |
| `status` | `in_progress` | `in_progress` | `in_progress` |
| `locations` | ❌ no enviado | ✅ enviado | ✅ enviado |
| `content` | ✅ terminal_ref o texto | ✅ enviado | ✅ enviado |
| `_meta` | ✅ `pando.toolName` | ✅ actualizado | ✅ completo |

### Gap específico en el streaming `in_progress`:

En `prompt_handler.go` línea 214-231, el `UpdateToolCall` in_progress sí envía:
- `WithUpdateStatus`, `WithUpdateKind`, `WithUpdateTitle`, `WithUpdateRawInput`, `WithUpdateContent`, `WithUpdateLocations`

Pero este solo se envía en el evento **Finished** (`tc.Finished = true`), es decir,
cuando el tool call está completo. Durante la acumulación no hay ningún update intermedio.

## Plan de Implementación

### Fase 1: Emitir `AgentEventTypeToolCall` con input acumulado desde `EventToolUseDelta`

**Archivo:** `internal/llm/agent/agent.go` (~línea 881)

**Cambio:** Publicar un evento `AgentEventTypeToolCall` en cada `EventToolUseDelta` pero
con un debounce (mínimo 100ms entre eventos) para no saturar el canal. Esto permite que
el `prompt_handler.go` reciba actualizaciones del input mientras se acumula y pueda enviar
`UpdateToolCall` correctivo al cliente ACP.

Alternativa (más simple): Publicar un evento **una sola vez** cuando el primer chunk
de input llega tras el ToolUseStart. Esto evita la saturación pero aún da la primera
actualización enriquecida al cliente.

### Fase 2: Reforzar el correctivo en `processAgentResponse` con `WithUpdateLocations`

**Archivo:** `internal/mesnada/acp/prompt_handler.go` (~líneas 582-592)

El bloque correctivo (`hadEmptyInput && toolCall.Input != ""`, líneas 571-602) actualmente
envía: `WithUpdateKind`, `WithUpdateTitle`, `WithUpdateRawInput`.

**Añadir:** `WithUpdateLocations(locations)` al correctivo update. Actualmente se calcula
`locations` en la línea 576 pero NO se incluye en los `correctiveOpts`.

```go
correctiveOpts := []acpsdk.ToolCallUpdateOpt{
    acpsdk.WithUpdateKind(kind),
    acpsdk.WithUpdateTitle(title),
    acpsdk.WithUpdateRawInput(rawInput),
    acpsdk.WithUpdateLocations(locations),  // ← AÑADIR
}
if len(content) > 0 {
    correctiveOpts = append(correctiveOpts, acpsdk.WithUpdateContent(content))
}
```

### Fase 3: Enviar `StartToolCall` con `locations` incluso con input vacío

**Archivo:** `internal/mesnada/acp/prompt_handler.go` (~línea 168-185)

El `sendStart` callback ya intenta añadir locations, pero `toLocations(tc.Name, tc.Input)`
retorna nil cuando `tc.Input` está vacío. Para herramientas como `view`, `read`, `edit`
el location no puede extraerse sin el input, pero para `bash` sí podemos enviar el
terminal_ref en content.

**Cambio:** Para el bash tool, asegurar que el `StartToolCall` siempre lleva el
`ToolTerminalRef` en content (ya se hace en líneas 164-166). Verificar que esto funciona.

### Fase 4: Asegurar paridad en `rawOutput` entre paths

**Archivo:** `internal/mesnada/acp/prompt_handler.go` (líneas 193-232)

En el streaming path, cuando `tc.Finished = true` y `started = true` (el `StartToolCall`
ya se envió), se envía el `inProgressOpts` update. Este NO incluye `rawOutput` porque
aún no hay resultado. Esto es correcto.

Pero cuando `tc.Finished = true` y `!started` (el StartToolCall nunca se envió), se envía
un `StartToolCall` sintético (líneas 205-212). Este ya incluye `rawInput`, `kind`,
`locations`, `content`. ✅ Correcto.

### Fase 5: Paridad `_meta` entre streaming y history replay

**Archivo:** `internal/mesnada/acp/prompt_handler.go`

Verificar que todos los paths incluyen `_meta` consistentemente:
- Streaming start: ✅ (línea 161-166, inyectado en línea 182-184)
- Streaming in_progress: ✅ (línea 226-228)
- Streaming synthetic start: ✅ (línea 280-299)
- Streaming tool result: ✅ (línea 341-409)
- Non-streaming start: ✅ (línea 612-635)
- Non-streaming result: ✅ (línea 690-747)

**History replay (`session_state.go`):**
- ToolCall start: ✅ (líneas 270-282)
- ToolResult: ✅ (líneas 349-406)

### Fase 6: Tests

**Archivos:** `internal/mesnada/acp/agent_pando_test.go`

Añadir tests que verifiquen:
1. Que el correctivo `UpdateToolCall` incluye `locations` para herramientas con path
2. Que el streaming `StartToolCall` para bash incluye `terminal_ref` en content
3. Que el non-streaming `StartToolCall` siempre lleva `rawInput` completo
4. Paridad de `_meta` entre todos los paths

Comparar con los tests existentes `TestToolDisplayTitle` para referencia.

### Fase 7: Verificación

1. `go test ./internal/mesnada/acp ./internal/llm/agent`
2. `go vet ./internal/mesnada/acp ./internal/llm/agent`
3. Build: `go build ./cmd/pando`
4. Prueba manual conectando un cliente ACP (Zed) y observando la visualización

## Archivos a modificar

| Archivo | Cambio |
|---|---|
| `internal/llm/agent/agent.go` | Fase 1: Emitir evento en ToolUseDelta |
| `internal/mesnada/acp/prompt_handler.go` | Fase 2: Añadir locations al correctivo |
| `internal/mesnada/acp/agent_pando_test.go` | Fase 6: Tests de paridad |

## Criterios de éxito

- [ ] Las tool calls en streaming realtime muestran `rawInput` con el comando/parámetros reales
- [ ] El `title` muestra la misma info enriquecida que en el histórico (ej: "ls -la /tmp" vs "bash")
- [ ] Los `locations` aparecen en las tool calls de ficheros (view, read, edit) en tiempo real
- [ ] El `_meta` es consistente entre streaming y replay de histórico
- [ ] No hay regresión en el comportamiento de tool calls ya funcionales
- [ ] Los tests existentes siguen pasando
