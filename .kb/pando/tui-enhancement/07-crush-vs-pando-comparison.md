# Comparativa Crush vs Pando - Análisis de Features TUI

## Tabla Comparativa

| Feature | Crush | Pando | Gap |
|---------|-------|-------|-----|
| **Keybindings** | KeyMap jerárquico (Editor/Chat/Init/Global) | keyMap plano (4 bindings) | Alto |
| **Command Palette** | Completions con fuzzy, files + MCP resources | CompletionDialog básico | Medio |
| **Sessions** | Dialog completo (list, select, rename, delete) | SessionDialog básico | Bajo |
| **Models** | Dialog con Large/Small, tab switch, API key | ModelDialog existente | Bajo |
| **Overlay System** | Stack de diálogos (`dialog.Overlay`) | show* booleans individuales | Medio |
| **DiffView** | Split/Unified con syntax highlight + cache | No existe | Alto |
| **Markdown** | glamour con estilos custom | Básico o inexistente | Alto |
| **Syntax Highlight** | chroma con cache de lexers | No existe en TUI | Alto |
| **Mouse Chat** | HandleMouseDown/Up/Drag, selectWord/Line | No | Alto |
| **File Explorer** | FilePicker dialog (no sidebar permanente) | FilePicker dialog | Medio |
| **Sidebar** | Solo logo (`sidebarLogo`) | No | Alto |
| **Editor** | No (usa editor externo) | No | Alto |
| **Animation** | Sistema de animaciones (`anim/`) | No | Bajo |
| **Permission Diff** | Inline diff en diálogo de permisos | permissionDialogCmp básico | Medio |
| **Help** | Help overlay con ShortHelp/FullHelp | HelpCmp existente | Bajo |
| **Themes** | Estilos semánticos, chroma themes | ThemeDialog existente | Bajo |
| **Layout** | Custom layout con `uv.Screen` | SplitPane, overlay, containers | Bajo |
| **Status Bar** | StatusLine con info de sesión/modelo | StatusCmp existente | Bajo |

## Features Únicas que Pando Debe Tener (Más allá de Crush)

1. **Sidebar de archivos permanente** - Crush no tiene, Pando sí lo necesita
2. **Editor integrado** - Crush usa editor externo, Pando tendrá editor TUI
3. **Bubblezone mouse** - Crush hace mouse nativo, Pando usará bubblezone
4. **Panel de cambios** - Vista dedicada para ver todos los cambios del agente
5. **Git status visual** - Integrado en el file tree con iconos

## Patrones de Crush a Adoptar

### 1. Action Pattern
Los diálogos retornan `Action` types en vez de tea.Msg genéricos:
```go
ActionOpenDialog{DialogID: "sessions"}
ActionSelectSession{Session: s}
ActionSelectModel{ModelType: "large"}
ActionNewSession{}
ActionToggleCompactMode{}
ActionRunCustomCommand{Content: "/help"}
```
**Beneficio**: Desacoplamiento claro entre diálogos y modelo principal.

### 2. Dialog Interface
```go
type Dialog interface {
    HandleMsg(msg tea.Msg) Action
    Render(width int) string
    ShortHelp() []key.Binding
    Cursor() *tea.Cursor
}
```
**Beneficio**: Todos los diálogos son intercambiables en el overlay.

### 3. Components Should Be Dumb
- No manejan `tea.Msg` directamente
- Exponen métodos para cambios de estado
- Retornan `tea.Cmd` cuando necesitan side effects
- Renderizan via `Render(width int) string`

### 4. Cached Message Items
```go
type cachedMessageItem struct {
    cache    string
    cacheW   int
    isDirty  bool
}
```
**Beneficio**: Evita re-rendering costoso de mensajes que no cambian.

### 5. List with Lazy Rendering
- Solo renderiza items visibles
- Scroll virtual
- Filtrado fuzzy integrado

## Orden de Prioridad Recomendado

1. **Fase 1** (Keybindings) + **Fase 6** (Diff) → Funcionalidad core
2. **Fase 4** (Markdown) → Mejora visual inmediata del chat
3. **Fase 2** (File Explorer) → Navegación de proyecto
4. **Fase 3** (Editor) → Capacidad de edición
5. **Fase 5** (Mouse) → Polish y UX

## Dependencias Entre Fases

```
Fase 1 (Keybindings) ──→ Fase 2 (File Explorer) ──→ Fase 3 (Editor)
                                     │                      │
                                     └──→ Fase 5 (Mouse) ←──┘
                                     
Fase 4 (Markdown) ──→ Fase 6 (Diff Viewer)
        │                    │
        └──→ Fase 5 (Mouse) ←┘
```

- Fase 1 es prerequisito de todas (establece el sistema de keybindings)
- Fase 2 y 4 pueden hacerse en paralelo
- Fase 3 depende de Fase 2 (necesita sidebar para abrir archivos)
- Fase 5 se beneficia de todas las anteriores pero puede integrarse incrementalmente
- Fase 6 puede empezarse en paralelo con Fase 2
