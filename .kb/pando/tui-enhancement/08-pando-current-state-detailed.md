# Pando TUI - Estado Actual Detallado

## Dependencias Charmbracelet YA Existentes
```
github.com/charmbracelet/bubbles v0.21.0
github.com/charmbracelet/bubbletea v1.3.5
github.com/charmbracelet/lipgloss v1.1.0
github.com/charmbracelet/glamour v0.9.1        # YA EXISTE
github.com/charmbracelet/x/ansi v0.8.0
github.com/lrstanley/bubblezone v0.0.0-...     # YA EXISTE
```

**IMPORTANTE**: glamour y bubblezone ya son dependencias. Las fases 4 y 5 no necesitan añadirlas.

## Keybindings Actuales (más de lo esperado)
| Shortcut | Acción | Contexto |
|----------|--------|----------|
| `ctrl+l` | Logs page | Global |
| `ctrl+m` | Orchestrator page | Global |
| `ctrl+c` | Quit dialog | Global |
| `ctrl+_`/`ctrl+h` | Toggle help | Global |
| `ctrl+g` | Settings page | Global |
| `ctrl+s` | Switch session | ChatPage |
| `ctrl+k` | Commands dialog | ChatPage |
| `ctrl+f` | File picker | Global |
| `ctrl+o` | Model selection | Global |
| `ctrl+t` | Theme switcher | Global |
| `ctrl+n` | Nueva sesión | ChatPage |
| `ctrl+e` | Editor externo | Editor |
| `ctrl+r` | Delete mode adjuntos | Editor |
| `@` | Completion dialog | Editor |
| `esc` | Cerrar diálogos | Global |
| `?` | Help | Global |
| `enter`/`ctrl+s` | Enviar mensaje | Editor |

## Páginas (4 páginas completas)
1. **ChatPage** - Chat principal con split pane (messages + editor + sidebar)
2. **LogsPage** - Tabla de logs + detalles
3. **SettingsPage** - Configuración con secciones dinámicas
4. **OrchestratorPage** - Dashboard de tareas Mesnada

## Diálogos (12 diálogos)
1. Permission, Session, Command, Model, Init
2. Filepicker, Theme, MultiArguments, Completion
3. Help, Quit, CustomCommands

## Sidebar YA EXISTENTE (chat/sidebar.go - 379 líneas)
- Información de sesión
- Configuración LSP
- **Archivos modificados con estadísticas de cambios (+/-)**
- Cálculo de diffs entre versión inicial y actual
- Tracking de adiciones/remociones por archivo

## Sistema de Temas (9 temas, 77 métodos de color)
Temas: OneDark, TokyoNight, Flexoki, Tron, Gruvbox, Monokai, Catppuccin, Dracula, OpenCode

Colores ya definidos para:
- **Diff**: DiffAdded, DiffRemoved, DiffContext, etc. (10 colores)
- **Markdown**: MarkdownText, MarkdownHeading, MarkdownLink, etc. (14 colores)
- **Syntax**: SyntaxComment, SyntaxKeyword, SyntaxFunction, etc. (8 colores)

## Markdown Rendering
- `internal/tui/styles/markdown.go` - Ya existe rendering con glamour
- Integrado en `message.go` para mensajes del chat

## Componentes de Chat
- **Editor** (319 líneas): TextArea, adjuntos, editor externo
- **Lista de Mensajes** (488 líneas): Viewport, spinner, cache
- **Mensajes** (660 líneas): Renderización de user/assistant/tools, diffs inline para Edit
- **Sidebar** (379 líneas): Archivos modificados con diff stats

## Layout
- **SplitPaneLayout**: Left (messages) + Right (sidebar) + Bottom (editor)
- **Container**: Padding, bordes, estilos dinámicos
- **Overlay**: Para diálogos modales centrados

## Lo que REALMENTE Falta vs Crush

| Feature | Estado | Prioridad |
|---------|--------|-----------|
| File Tree navigator (expandible, no solo lista) | NO EXISTE | Alta |
| Editor/Viewer de archivos con syntax highlighting | NO EXISTE | Alta |
| Tab system para archivos | NO EXISTE | Alta |
| DiffView completo (split/unified con scroll) | PARCIAL (inline en messages) | Alta |
| Mouse clicks en sidebar/chat | NO EXISTE (bubblezone importado pero no usado) | Media |
| Command palette con fuzzy search | BÁSICO (CommandDialog sin fuzzy) | Media |
| KeyMap jerárquico | PARCIAL (hay shortcuts pero no estructura jerárquica) | Baja |
| Markdown con glamour | YA EXISTE | - |
| chroma syntax highlighting | NO EXISTE | Alta |
| Overlay stack de diálogos | PARCIAL (booleans, no stack) | Baja |

## Conclusión
Pando está más avanzado de lo esperado. Las principales brechas son:
1. **File Tree navigator** interactivo (no el sidebar actual de archivos modificados)
2. **Editor/Viewer** integrado con syntax highlighting (chroma)
3. **DiffView** completo como componente standalone
4. **Mouse interaction** real (bubblezone está importado pero no se usa activamente)
5. **Fuzzy search** en command palette
