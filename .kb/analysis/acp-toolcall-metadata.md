# Análisis: Cómo ACP envuelve tool calls y el problema en Pando

## Cómo funciona en acp-go-sdk (el estándar ACP)

### Estructura ToolCall en types_gen.go (línea 5633)
```go
type ToolCall struct {
    Meta      map[string]any    `json:"_meta,omitempty"`
    Content   []ToolCallContent `json:"content,omitempty"`
    Kind      ToolKind          `json:"kind,omitempty"`
    Locations []ToolCallLocation `json:"locations,omitempty"`
    RawInput  any               `json:"rawInput,omitempty"`   // ← parámetros de entrada crudos
    RawOutput any               `json:"rawOutput,omitempty"`
    Status    ToolCallStatus    `json:"status,omitempty"`
    Title     string            `json:"title"`                // ← título legible
    ToolCallId ToolCallId       `json:"toolCallId"`
}
```

### SessionUpdateToolCall (start, línea 4581)
Igual que ToolCall pero con `SessionUpdate string` adicional.

### Flujo de helpers.go:

1. **`StartToolCall(id, title, opts...)`** — Crea `SessionUpdate{ToolCall: &tc}` con opcionales:
   - `WithStartKind(kind)` → read/edit/execute/search...
   - `WithStartStatus(status)` → pending/in_progress/completed/failed
   - `WithStartRawInput(rawInput)` → los parámetros JSON parseados como objeto
   - `WithStartContent(content)` → diffs, text blocks, terminal refs
   - `WithStartLocations(locations)` → array de {path}

2. **`UpdateToolCall(id, opts...)`** — Crea `SessionUpdate{ToolCallUpdate: &tu}` con opcionales:
   - `WithUpdateStatus(status)`
   - `WithUpdateTitle(title)`
   - `WithUpdateKind(kind)`
   - `WithUpdateRawInput(rawInput)`
   - `WithUpdateRawOutput(rawOutput)`
   - `WithUpdateContent(content)`

### `toolDisplayTitle()` en tool_render.go (línea 78)
Deriva un título legible del rawInput:
- **bash**: extrae `rawInput["command"]` → muestra el comando real
- **view/read**: extrae `rawInput["file_path"]` → "Read <path>"
- **write**: extrae `rawInput["file_path"]` → "Write <path>"
- **edit**: extrae `rawInput["file_path"]` → "Edit <path>"
- **agent**: extrae primeros chars de `rawInput["prompt"]`
- **glob/grep**: extrae `rawInput["path"]` y `rawInput["pattern"]`

### `parseJSONInput()` (línea 15)
Convierte string JSON a `map[string]interface{}`. Si falla, devuelve el string original.
Si el string está vacío, devuelve `map[string]interface{}{}`.

## Cómo lo implementa Pando (server-side ACP)

### Archivos clave:
- `internal/mesnada/acp/prompt_handler.go` — streaming y non-streaming tool calls
- `internal/mesnada/acp/session_state.go` — history replay
- `internal/mesnada/acp/tool_render.go` — utilidades de render

### Flujo streaming (prompt_handler.go ~línea 132):
```
AgentEventTypeToolCall → tc.Name, tc.Input, tc.Finished
  ↓
kind = mapToolKind(tc.Name)
rawInput = parseJSONInput(tc.Input)    // ← si tc.Input está vacío → map[]{}
title = toolDisplayTitle(tc.Name, rawInput, workDir)
  ↓
StartToolCall(id, title, WithStartRawInput(rawInput), ...)
```

### Flujo non-streaming (prompt_handler.go ~línea 533):
```
msg.ToolCalls() → toolCall.Name, toolCall.Input
  ↓
rawInput = parseJSONInput(toolCall.Input)  // aquí Input sí tiene el JSON completo
title = toolDisplayTitle(toolCall.Name, rawInput, workDir)
  ↓
StartToolCall(id, title, WithStartRawInput(rawInput), ...)
```

## El problema: streaming path con rawInput={} y title="bash"

### Causa raíz

En `agent.go` líneas 844-869, el flujo de eventos es:

1. **EventToolUseStart** (línea 844): `event.ToolCall` tiene `Input: ""` (vacío, solo se conoce el nombre)
   → Se publica `AgentEventTypeToolCall` con `ToolCall.Input = ""` y `ToolCall.Finished = false`
   → `prompt_handler.go` recibe esto, llama a `parseJSONInput("")` → `map[string]interface{}{}`
   → `toolDisplayTitle("bash", map[]{}, workDir)`: no encuentra "command" en el map → devuelve "bash"
   → **`StartToolCall` se envía con `rawInput: {}` y `title: "bash"`**

2. **EventToolUseDelta** (línea 853): `assistantMsg.AppendToolCallInput(event.ToolCall.ID, event.ToolCall.Input)`
   → Solo acumula input, no publica evento al frontend

3. **EventToolUseStop** (línea 856): Se publica `AgentEventTypeToolCall` con el ToolCall completo (Finished=true, Input=JSON completo)
   → `prompt_handler.go` recibe esto con `tc.Finished = true`
   → Ahora `rawInput` tiene los datos reales
   → Envía un **UpdateToolCall** con `WithStartRawInput(rawInput)` correcto y title correcto

### El fallo

Cuando el cliente ACP recibe el primer `StartToolCall` con `title="bash"` y `rawInput={}`, ya muestra eso. Luego el `UpdateToolCall` correctivo llega, **pero el cliente puede o no actualizar la UI** dependiendo de su implementación. Algunos clientes (Zed, VS Code) muestran el `title` inicial y no lo reemplazan con el del update.

### Solución posible

En `prompt_handler.go`, en el streaming path (`AgentEventTypeToolCall`), cuando `tc.Finished = true`:

1. Enviar `StartToolCall` con status `in_progress` (en vez de `pending`)
2. **Asegurar que los parámetros correctivos incluyan `WithUpdateRawInput(rawInput)`** con los datos reales
3. Incluir siempre `WithUpdateTitle(title)` en el update para que el cliente actualice

Esto ya está implementado parcialmente en prompt_handler.go líneas 203-231:
```go
if !started {
    sendStart(acpsdk.ToolCallStatusInProgress)  // con rawInput={}
}
inProgressOpts := []acpsdk.ToolCallUpdateOpt{
    WithUpdateStatus(acpsdk.ToolCallStatusInProgress),
    WithUpdateKind(kind),
    WithUpdateTitle(title),       // ← título real con comando
    WithUpdateRawInput(rawInput), // ← rawInput real con command
    WithUpdateContent(content),
}
```

Pero a veces el `ToolUseStop` se pierde (buffer de 256 slots lleno), y entonces el `processAgentResponse` envía el correctivo. Eso funciona, pero el primer `StartToolCall` ya mostró "bash" y `rawInput: {}`.

### Otra aproximación: `StartToolCallStreaming`

El SDK tiene `StartToolCallStreaming()` (helpers.go línea 385) que envía:
```go
func StartToolCallStreaming(id, title, kind, opts...) SessionUpdate {
    // status=pending, rawInput={}
}
```

Esto permite que el cliente muestre la tool inmediatamente pero con un placeholder. Luego cuando el input completo llega, el `UpdateToolCall` reemplaza la info.

### Mejora concreta

El problema principal es que algunos clientes ACP no actualizan el título cuando reciben un `tool_call_update`. La solución es:

1. **En el streaming start**: usar el tool name como título provisional (ya se hace)
2. **En el streaming stop**: si el comando es bash, usar `toolDisplayTitle()` para poner el comando real, y enviar `WithUpdateTitle(command)`  
3. **En `session_state.go` (history replay)**: igual que prompt_handler, asegurar que rawInput lleva el comando