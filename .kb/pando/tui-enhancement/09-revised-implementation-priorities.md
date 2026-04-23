# Plan Revisado de Implementación - Prioridades Corregidas

## Contexto
Tras el análisis exhaustivo, Pando ya tiene:
- glamour y bubblezone como dependencias
- Sidebar con archivos modificados y diff stats
- 18+ keybindings definidos
- Markdown rendering básico con glamour
- 9 temas con colores para diff, markdown y syntax
- Overlay system básico

## Fases Revisadas (Ordenadas por Impacto Real)

### Fase 1: File Tree Navigator + Editor Viewer (MAYOR IMPACTO)
**Prioridad: CRÍTICA** - Es lo que más diferencia un chat de un IDE

**1A. File Tree Component** (nuevo componente, no modificar sidebar existente)
- Directorio: `internal/tui/components/filetree/`
- Tree view con expand/collapse
- Git status icons (usa colores de DiffAdded/DiffRemoved del tema)
- Keybindings: j/k, enter, h/l, /
- Respeta .gitignore
- Lazy loading por directorio

**1B. File Viewer Component**
- Directorio: `internal/tui/components/editor/`
- Viewer read-only con syntax highlighting (chroma)
- Números de línea, scroll
- Búsqueda con `/` o ctrl+f
- **Dependencia nueva**: `github.com/alecthomas/chroma/v2`
- Usar colores SyntaxComment/SyntaxKeyword/etc del tema existente

**1C. Tab System**
- Múltiples archivos abiertos
- Tab bar con icono + nombre + dirty indicator
- ctrl+w para cerrar, ctrl+tab para cambiar

**1D. Layout Integration**
- Toggle sidebar con ctrl+b
- Usar SplitPaneLayout existente con 3 layouts:
  - Chat only (actual)
  - Sidebar + Chat
  - Sidebar + Editor (o Sidebar + Editor + Chat en 3 paneles)

**Archivos clave a crear**:
```
internal/tui/components/filetree/
  ├── node.go       # FileNode struct
  ├── filetree.go   # Componente principal
  ├── loader.go     # Carga de dirs + git status
  └── keys.go       # Keybindings

internal/tui/components/editor/
  ├── viewer.go     # File viewer
  ├── tabs.go       # Tab system
  ├── highlight.go  # chroma integration
  └── keys.go       # Keybindings
```

### Fase 2: DiffView Completo (ALTO IMPACTO)
**Prioridad: ALTA** - El sidebar ya muestra stats, falta el viewer completo

- Portar concepto de DiffView de crush
- Modos: unified y split
- Syntax highlighting en diff (reutilizar chroma de Fase 1)
- Integrar en:
  - Permission dialog (ya muestra diffs básicos)
  - Changes panel (sidebar existente -> click para ver diff completo)
  - Como página standalone o overlay
- Navegación entre hunks con ]c / [c

**Archivos a crear**:
```
internal/tui/components/diff/
  ├── diffview.go     # DiffView unified + split
  ├── parser.go       # Parseo de diffs
  └── styles.go       # Estilos (usar DiffAdded/DiffRemoved del tema)
```

### Fase 3: Mouse Support Activo (MEDIO IMPACTO)
**Prioridad: MEDIA** - bubblezone ya importado, solo falta usarlo

- Activar `tea.EnableMouseCellMotion` en Init()
- Crear zone manager (`internal/tui/zone/`)
- Hacer clickeable:
  - File tree items (abrir/expandir)
  - Tabs del editor (cambiar/cerrar)
  - Sidebar items (ver diff)
  - Status bar elements (modelo, sesión)
  - Botones de diálogos
- Mouse wheel scroll en todos los viewports
- `zone.Manager.Scan()` en View() final

**Archivos a crear/modificar**:
```
internal/tui/zone/zone.go  # Zone manager + IDs
# Modificar todos los View() para Mark() zones
# Modificar tui.go para Scan() y handleMouse()
```

### Fase 4: Command Palette con Fuzzy Search (MEDIO IMPACTO)
**Prioridad: MEDIA** - CommandDialog existe pero es básico

- Añadir fuzzy matching al CompletionDialog existente
- Categorías de comandos (General, Files, Sessions, Models, View)
- Mostrar shortcut junto a cada comando
- Integrar con todos los comandos registrados
- Abrir con ctrl+k (ya existe) o ctrl+p

**Archivos a modificar**:
```
internal/tui/components/dialog/commands.go   # Añadir fuzzy
internal/tui/components/dialog/complete.go   # Reutilizar para fuzzy
```

### Fase 5: Mejoras de Markdown Rendering (BAJO IMPACTO)
**Prioridad: BAJA** - glamour ya funciona, solo mejorar

- Verificar que los estilos de glamour usen MarkdownText/MarkdownHeading/etc del tema
- Mejorar rendering de code blocks con syntax highlighting chroma
- Streaming markdown parcial con debounce
- Links clickeables (integrar con bubblezone de Fase 3)

**Archivos a modificar**:
```
internal/tui/styles/markdown.go      # Mapear colores del tema
internal/tui/components/chat/message.go  # Mejorar rendering
```

### Fase 6: Refinamiento de Keybindings (BAJO IMPACTO)
**Prioridad: BAJA** - Ya tiene 18+ shortcuts, solo organizar mejor

- Extraer KeyMap a struct jerárquico en archivo separado
- Mejorar Help overlay para mostrar todos los shortcuts por contexto
- Añadir nuevos shortcuts para las features nuevas (Fase 1-3)
- Documentar todos los shortcuts en el help

## Dependencias Nuevas Reales
```
github.com/alecthomas/chroma/v2  # ÚNICA dependencia nueva necesaria
```

## Orden de Ejecución
```
Fase 1 (File Tree + Editor) ─── requiere chroma
         │
         ├──→ Fase 2 (DiffView) ─── reutiliza chroma
         │
         └──→ Fase 3 (Mouse) ─── reutiliza bubblezone ya importado
                  │
                  └──→ Fase 5 (Markdown mejoras)

Fase 4 (Fuzzy Search) ─── independiente
Fase 6 (Keybindings) ─── independiente, se hace incrementalmente
```

## Estimación de Esfuerzo (Revisada)
| Fase | Esfuerzo | Archivos Nuevos | Archivos Modificados |
|------|----------|-----------------|---------------------|
| 1 (File Tree + Editor) | Alto | ~8 | ~3 |
| 2 (DiffView) | Medio | ~3 | ~3 |
| 3 (Mouse) | Medio | ~1 | ~8 |
| 4 (Fuzzy) | Bajo | 0 | ~2 |
| 5 (Markdown) | Bajo | 0 | ~2 |
| 6 (Keybindings) | Bajo | ~1 | ~2 |
