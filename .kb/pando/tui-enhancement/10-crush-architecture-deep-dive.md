# Crush - Análisis Arquitectónico Profundo

## Ultraviolet (UV) Rendering Framework
Crush NO usa el patrón tradicional de bubbletea (View() → string). Usa **Ultraviolet**:

```go
// Canvas-based rendering
canvas := uv.NewScreenBuffer(width, height)
m.Draw(canvas, canvas.Bounds())
v.Content = canvas.Render()

// En componentes
uv.NewStyledString(view).Draw(scr, area)
```

**Diferencia con Pando**: Pando usa el patrón tradicional View() → string con lipgloss. No necesita migrar a UV, pero es bueno saber que el rendering de crush es más avanzado.

## UI States Machine
```
uiOnboarding → uiInitialize → uiLanding → uiChat
```
Cada estado tiene su propio set de keybindings y rendering.

## Focus States
```go
const (
    uiFocusNone   // Sin focus (landing, onboarding)
    uiFocusEditor // Focus en textarea
    uiFocusMain   // Focus en chat (scroll, select)
)
```

## Key Event Routing (handleKeyPressMsg)
Orden de prioridad:
1. `Ctrl+C` → siempre Quit dialog
2. Diálogos abiertos → router a dialog.HandleMsg()
3. `Esc` → cancelar agente / limpiar queue
4. Según estado UI:
   - **Editor focus**: Completions → Attachments → Editor keys → Global
   - **Chat focus**: Chat navigation keys
5. **`@` detection**: trigger completions en tiempo real (línea 1664)
6. **`/` detection**: cuando textarea vacío, abre command palette (línea 1649)

## Commands Dialog (3 tabs)
```
┌─ Commands ──────────────────────┐
│ [System] [User] [MCP Prompts]   │
│ ┌─────────────────────────────┐ │
│ │ 🔍 Filter...               │ │
│ │ ─────────────────────────── │ │
│ │ New Session      Ctrl+N    │ │
│ │ Toggle Help      Ctrl+G    │ │
│ │ Toggle Compact   Ctrl+T    │ │
│ │ External Editor  Ctrl+O    │ │
│ │ ...                        │ │
│ └─────────────────────────────┘ │
└─────────────────────────────────┘
```
- Tab/Shift+Tab entre System/User/MCP
- Búsqueda fuzzy en tiempo real
- Spinner durante carga async de User/MCP commands

## Pills (Todo/Queue)
Sección entre chat y editor:
```
│ Chat messages...                    │
├─────────────────────────────────────┤
│ • ⋯ To-Do 2/5  Current task...     │
│   ✓ Completed item                 │
│   → In-progress item               │
│ • ▶▶▶▶ 3 Queued                    │
│   → First queued item              │
├─────────────────────────────────────┤
│ Editor (input area)                 │
```
- Toggle con Ctrl+T
- pillsExpanded boolean
- pillSectionTodos / pillSectionQueue

## Sidebar de Crush
```
├── Logo (small o large según altura)
├── Session Title
├── Current Working Directory
├── Model Info (nombre, provider, reasoning)
├── Files Section (dinámico - máx 10)
├── LSP Status Section (máx 8)
└── MCP Status Section (máx 8)
```
- `getDynamicHeightLimits()` distribuye altura entre secciones
- Modo compact: sidebar desaparece, header compacto + overlay con Ctrl+D

## Chat Message Types
```go
UserMessageItem       // Mensajes del usuario (con attachments)
AssistantMessageItem  // Respuestas (markdown rendered)
ToolCallItem          // Herramientas ejecutadas
TodoItem              // Items de todo (status + nombre)
ReferencesItem        // Referencias a archivos
SearchItem            // Búsquedas realizadas
DiagnosticsItem       // Errores LSP
```

Cada uno:
- Implementa `MessageItem` interface
- Cache de render por ancho
- Puede ser animado (solo si visible)
- Soporta highlight (selección de texto)
- Soporta focus

## Lista Lazy-Loaded (list/list.go)
```go
type List struct {
    items []Item
    offsetIdx, offsetLine int   // Virtualización
    selectedIdx int
    renderCallbacks []func()    // Hooks pre-render
}
```
- Solo renderiza items visibles
- Callbacks de render para aplicar focus/highlight
- Soporta gaps entre items
- FilterableList con fuzzy search (sahilm/fuzzy)

## Estilos con Gradientes
- Usa `charmtone` para gradientes de color
- Aplicados en Pills, herramientas, etc.
- Estilos semánticos: Primary, Secondary, Tertiary, BgBase, FgBase, etc.

## Patterns a Portar a Pando

### 1. Action Pattern (RECOMENDADO)
Los diálogos retornan Action types tipados, el modelo principal los procesa.
Pando actualmente usa tea.Msg genéricos - podría beneficiarse de Actions.

### 2. Lazy List con Callbacks (RECOMENDADO)
La lista de crush solo renderiza lo visible y usa callbacks para transformar items.
Pando usa viewport de bubbles - podría mejorar performance con lazy rendering.

### 3. @ y / Detection (PARCIALMENTE IMPLEMENTADO)
Pando ya tiene @ para completions. Podría mejorar / para slash commands.

### 4. Pills Todo/Queue (OPCIONAL)
Podría ser útil para mostrar progreso del agente de forma más visual.

### 5. Dynamic Sidebar Heights (RECOMENDADO)
La distribución dinámica de altura entre secciones del sidebar es elegante.
