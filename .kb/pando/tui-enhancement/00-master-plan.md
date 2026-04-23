# Pando TUI Enhancement - Plan Maestro de Implementación

## Visión General
Transformar Pando de un asistente AI conversacional básico a un IDE TUI completo, inspirado en la arquitectura de crush, con soporte para:
- Navegación de proyectos con panel lateral de archivos
- Editor de código con syntax highlighting
- Visualización de markdown estilo glow
- Diff viewer para cambios del agente AI
- Soporte completo de mouse con bubblezone
- Sistema de keybindings y command palette avanzado

## Estado Actual de Pando (TUI)

### Arquitectura Existente
- **Modelo principal**: `internal/tui/tui.go` - `appModel` struct con sistema de páginas y diálogos
- **Páginas**: `internal/tui/page/` - ChatPage, LogsPage
- **Componentes**: `internal/tui/components/` - chat/, dialog/, logs/, util/
- **Layout**: `internal/tui/layout/` - SplitPane, overlay, containers
- **Estilos**: `internal/tui/styles/` - icons, theme
- **Diálogos existentes**: Permission, Session, Command, Model, Init, Filepicker, Theme, MultiArguments, Completion, Help, Quit

### Keybindings Actuales (tui.go)
```go
keyMap struct {
    Logs, Quit, SwitchSession, Filepicker key.Binding
    // + helpEsc, returnKey
}
```

### Dependencias Charmbracelet Actuales
- bubbletea, bubbles, lipgloss, glamour (verificar en go.mod)

## Análisis de Crush (Referencia)

### Arquitectura UI de Crush
```
internal/ui/
├── AGENTS.md          # UI development guidelines
├── model/
│   ├── ui.go          # Main model (message routing, focus, layout, dialogs)
│   ├── keys.go        # KeyMap struct (Editor, Chat, Initialize, Global)
│   └── chat.go        # Chat logic with full mouse support
├── chat/              # Chat message item types and renderers
│   ├── messages.go    # MessageItem interfaces, caching, highlighting
│   ├── assistant.go   # AssistantMessageItem with KeyEventHandler
│   └── mcp.go         # MCP resource rendering
├── dialog/
│   ├── dialog.go      # Overlay system (stack-based dialogs)
│   ├── actions.go     # Action types (SelectSession, SelectModel, NewSession, etc.)
│   ├── sessions.go    # Session dialog (list, select, rename, delete)
│   ├── models.go      # Model selection dialog (large/small, tab switch)
│   ├── permissions.go # Permission dialog with diff view
│   ├── quit.go        # Quit confirmation
│   ├── filepicker.go  # File picker dialog
│   ├── oauth.go       # OAuth dialog
│   └── api_key_input.go
├── completions/
│   ├── completions.go # Command/file completion popup
│   └── keys.go        # Completion keybindings
├── diffview/
│   ├── diffview.go    # Full DiffView with split/unified modes
│   ├── split.go       # Split view rendering
│   └── style.go       # Diff styling with chroma
├── list/              # Generic list component with lazy rendering
│   ├── list.go
│   ├── item.go        # List item interfaces
│   └── highlight.go   # Content highlighting
├── common/
│   ├── markdown.go    # MarkdownRenderer using glamour
│   ├── highlight.go   # SyntaxHighlight using chroma
│   └── capabilities.go
├── styles/
│   └── styles.go      # All style definitions with semantic colors
├── anim/              # Animation system
└── logo/              # Logo rendering
```

### Patrones Clave de Crush
1. **Overlay System**: Stack de diálogos (`dialog.Overlay`) - último diálogo recibe eventos
2. **Action Pattern**: Diálogos retornan `Action` types que el modelo principal procesa
3. **KeyMap Jerárquico**: Editor > Chat > Initialize > Global bindings
4. **Components Dumb**: No manejan tea.Msg directamente, exponen métodos
5. **Mouse Support Nativo**: HandleMouseDown/Up/Drag en Chat con selección de texto
6. **DiffView Completo**: Split/Unified con syntax highlighting via chroma
7. **Markdown**: glamour.TermRenderer con estilos custom
8. **Syntax Highlighting**: chroma con cache de lexers

## Fases de Implementación

### Fase 1: Sistema de Keybindings y Command Palette (Prioridad: ALTA)
- Expandir KeyMap con estructura jerárquica (Editor, Chat, Global)
- Implementar command palette mejorado (tipo crush)
- Shortcuts globales: Ctrl+P (commands), Ctrl+M (models), Ctrl+S (sessions)
- Help overlay con lista de shortcuts

### Fase 2: Panel Lateral File Explorer (Prioridad: ALTA)
- Componente FileTree con árbol de directorios
- Toggle con keybinding (Ctrl+B o similar)
- Integración con SplitPane existente
- Navegación con teclado y mouse
- Indicadores de archivos modificados (git status)

### Fase 3: Editor TUI con Syntax Highlighting (Prioridad: MEDIA)
- Viewer/Editor de archivos con chroma syntax highlighting
- Números de línea, scroll, búsqueda
- Tab system para múltiples archivos abiertos
- Integración con el panel lateral

### Fase 4: Markdown Rendering Mejorado (Prioridad: MEDIA)
- Integrar glamour para renderizar respuestas AI
- Code blocks con syntax highlighting
- Soporte para tablas, listas, enlaces
- Estilo personalizable (tema oscuro/claro)

### Fase 5: Mouse Support con Bubblezone (Prioridad: MEDIA)
- Integrar bubblezone para zonas clickeables
- Clicks en panel lateral para abrir archivos
- Clicks en tabs del editor
- Scroll con mouse wheel
- Selección de texto en chat (como crush)

### Fase 6: Diff Viewer y Gestión de Cambios (Prioridad: ALTA)
- Portar DiffView de crush (split/unified modes)
- Panel de archivos modificados por el agente AI
- Vista de diff inline en el chat (permisos)
- Navegación entre cambios
- Git status integrado

## Dependencias a Añadir
```
github.com/charmbracelet/glamour     # Markdown rendering
github.com/alecthomas/chroma/v2      # Syntax highlighting
github.com/lrstanley/bubblezone      # Mouse zones
```

## Consideraciones Arquitectónicas
1. **Mantener compatibilidad** con la estructura actual de páginas/diálogos
2. **SplitPane existente** se puede reutilizar para panel lateral
3. **Overlay system** de Pando ya funciona, solo necesita expansión
4. **Componentes deben ser lazy** - no renderizar lo que no se ve
5. **Cache agresivo** de syntax highlighting (como crush)
6. **Responsive layout** - adaptar paneles al tamaño del terminal
