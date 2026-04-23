# Plan de Ejecución con Mesnada - TUI Enhancement

## Configuración Base de Subagentes
- **Engine**: `copilot`
- **Model**: `gpt-5.4`
- **Working dir**: `/www/MCP/Pando/pando`
- **Tools comunes**: remembrances (kb_search_documents, kb_get_document, code_hybrid_search, code_find_symbol, code_get_file_symbols)

## Grafo de Dependencias

```
OLEADA 1 (Paralelo - Sin dependencias)
├── Agent-1A: File Tree Component
├── Agent-1B: Syntax Highlight Base (chroma)
├── Agent-4:  Command Palette Fuzzy Search
└── Agent-6:  Keybindings Refactoring

OLEADA 2 (Depende de 1A + 1B)
├── Agent-1C: File Viewer Component (usa chroma de 1B)
└── Agent-1D: Tab System

OLEADA 3 (Depende de 1C + 1D + 1A)
├── Agent-1E: Layout Integration (conecta filetree + editor + tabs)
└── Agent-2:  DiffView Component (usa chroma de 1B)

OLEADA 4 (Depende de oleadas anteriores)
├── Agent-3:  Mouse Support con Bubblezone
└── Agent-5:  Markdown Rendering Mejoras
```

## Justificación de Oleadas

### Por qué estos son paralelos en Oleada 1:
- **Agent-1A** (File Tree): Componente aislado en `internal/tui/components/filetree/`, no depende de nada nuevo
- **Agent-1B** (Chroma): Paquete utilitario de highlighting en `internal/tui/components/editor/highlight.go`, base para otros
- **Agent-4** (Fuzzy): Modifica `dialog/commands.go` y `dialog/complete.go` existentes, independiente del resto
- **Agent-6** (Keybindings): Refactor de `tui.go` keyMap a struct jerárquico, independiente

### Por qué Oleada 2 espera a Oleada 1:
- **Agent-1C** (Viewer): Necesita `highlight.go` de Agent-1B para syntax highlighting
- **Agent-1D** (Tabs): Necesita saber la interfaz de FileNode de Agent-1A para abrir archivos

### Por qué Oleada 3 espera a Oleada 2:
- **Agent-1E** (Layout): Necesita todos los componentes (filetree, viewer, tabs) para integrarlos en SplitPane
- **Agent-2** (DiffView): Reutiliza chroma de 1B, y el patrón de viewer de 1C

### Por qué Oleada 4 espera a Oleada 3:
- **Agent-3** (Mouse): Necesita que todos los componentes existan para añadir zone.Mark() a sus View()
- **Agent-5** (Markdown): Mejora code blocks con chroma y links clickeables con bubblezone (necesita Agent-3)

---

## Detalle de Subagentes

### OLEADA 1

#### Agent-1A: File Tree Component
```
ID: tui-filetree
Dependencias: ninguna
Archivos a crear:
  - internal/tui/components/filetree/node.go
  - internal/tui/components/filetree/filetree.go
  - internal/tui/components/filetree/loader.go
  - internal/tui/components/filetree/keys.go
```

**Prompt**:
```
Eres un experto en Go y bubbletea (charmbracelet). Tu tarea es implementar un componente FileTree para la TUI de Pando.

## Contexto
Usa las tools de remembrances para obtener contexto:
1. `kb_get_document("pando/tui-enhancement/02-phase2-file-explorer.md")` - Especificación completa del file explorer
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Plan revisado, sección 1A
3. `kb_get_document("pando/tui-enhancement/08-pando-current-state-detailed.md")` - Estado actual de Pando
4. `kb_get_document("pando/tui-enhancement/11-external-libraries-reference.md")` - Librerías disponibles

Usa `code_hybrid_search` en el proyecto "pando" para buscar:
- La estructura existente de componentes en `internal/tui/components/`
- Cómo se implementan otros componentes (dialog, chat) como referencia de patrones
- El tema y estilos existentes en `internal/tui/styles/` y `internal/tui/theme/`
- Cómo se usa `.gitignore` en el proyecto

## Requisitos
Crea el paquete `internal/tui/components/filetree/` con:

1. **node.go**: FileNode struct con campos (Name, Path, IsDir, IsExpanded, Children, GitStatus, Depth)
2. **filetree.go**: Componente bubbletea con:
   - Tree view con expand/collapse
   - Navegación j/k (arriba/abajo), h/l (colapsar/expandir), enter (abrir)
   - Búsqueda con "/"
   - Método Update(tea.Msg) que retorna (tea.Model, tea.Cmd)
   - Método View() string
   - Método SelectedFile() para obtener el archivo seleccionado
3. **loader.go**: Carga lazy de directorios + integración con git status
   - Respetar .gitignore
   - Solo cargar hijos al expandir (lazy)
   - Iconos de git status usando colores DiffAdded/DiffRemoved del tema
4. **keys.go**: KeyMap específico del file tree

## Patrones a seguir
- Usa lipgloss para estilos, reutiliza colores del tema existente
- El componente debe exponer una interfaz limpia (Init, Update, View, SetSize)
- NO integrar aún con el layout principal (eso es tarea de Agent-1E)
```

#### Agent-1B: Syntax Highlighting Base (Chroma)
```
ID: tui-chroma-highlight
Dependencias: ninguna
Archivos a crear:
  - internal/tui/components/editor/highlight.go
```

**Prompt**:
```
Eres un experto en Go. Tu tarea es crear el módulo base de syntax highlighting usando chroma para la TUI de Pando.

## Contexto
Usa las tools de remembrances para obtener contexto:
1. `kb_get_document("pando/tui-enhancement/03-phase3-editor-syntax-highlighting.md")` - Especificación del editor y highlighting
2. `kb_get_document("pando/tui-enhancement/10-crush-architecture-deep-dive.md")` - Cómo crush implementa highlighting
3. `kb_get_document("pando/tui-enhancement/11-external-libraries-reference.md")` - Referencia de chroma

Usa `code_hybrid_search` en el proyecto "pando" para buscar:
- Los colores de syntax en el tema: SyntaxComment, SyntaxKeyword, SyntaxString, etc.
- El go.mod para verificar si chroma ya está como dependencia
- La estructura de `internal/tui/theme/` para entender el sistema de temas

## Requisitos
Crea `internal/tui/components/editor/highlight.go` con:

1. **Highlighter struct**: Cache de lexers y resultados
   - `Highlight(source, fileName string) (string, error)` - Detecta lexer por extensión, aplica highlighting
   - `HighlightLine(line, fileName string) string` - Highlight de una línea individual
   - Cache LRU para evitar re-highlighting
2. Usar `github.com/alecthomas/chroma/v2` con formatter `terminal16m`
3. Mapear colores del tema de Pando al estilo de chroma
4. Si chroma no está en go.mod, incluir instrucciones para `go get`

## Importante
- Este módulo será reutilizado por el File Viewer (Agent-1C) y el DiffView (Agent-2)
- Debe ser un paquete independiente sin acoplamiento a componentes UI específicos
- Priorizar performance con cache agresivo
```

#### Agent-4: Command Palette con Fuzzy Search
```
ID: tui-fuzzy-palette
Dependencias: ninguna
Archivos a modificar:
  - internal/tui/components/dialog/commands.go
  - internal/tui/components/dialog/complete.go
```

**Prompt**:
```
Eres un experto en Go y bubbletea. Tu tarea es mejorar el Command Palette existente de Pando añadiendo fuzzy search.

## Contexto
Usa las tools de remembrances:
1. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Fase 4: Command Palette
2. `kb_get_document("pando/tui-enhancement/01-phase1-keybindings-commands.md")` - Especificación de commands

Usa `code_hybrid_search` y `code_get_file_symbols` en el proyecto "pando" para:
- Leer `internal/tui/components/dialog/commands.go` - El dialog de comandos actual
- Leer `internal/tui/components/dialog/complete.go` - El completion dialog actual
- Buscar cómo se registran los comandos actualmente
- Buscar fuzzy matching libraries en Go (sahilm/fuzzy o similar)

## Requisitos
1. Añadir fuzzy matching al filtrado de comandos (usar `sahilm/fuzzy` o implementar scoring básico)
2. Categorizar comandos: General, Files, Sessions, Models, View
3. Mostrar shortcut junto a cada comando en la lista
4. Mantener apertura con ctrl+k, añadir ctrl+p como alias
5. Ranking por frecuencia de uso + score de fuzzy match

## Importante
- NO romper la funcionalidad existente del CommandDialog
- Mantener compatibilidad con el overlay system actual
```

#### Agent-6: Keybindings Refactoring
```
ID: tui-keybindings
Dependencias: ninguna
Archivos a crear:
  - internal/tui/keys.go (extraído de tui.go)
Archivos a modificar:
  - internal/tui/tui.go
```

**Prompt**:
```
Eres un experto en Go y bubbletea. Tu tarea es refactorizar el sistema de keybindings de Pando.

## Contexto
Usa las tools de remembrances:
1. `kb_get_document("pando/tui-enhancement/01-phase1-keybindings-commands.md")` - Especificación completa
2. `kb_get_document("pando/tui-enhancement/07-crush-vs-pando-comparison.md")` - Comparativa con crush
3. `kb_get_document("pando/tui-enhancement/08-pando-current-state-detailed.md")` - Estado actual

Usa `code_get_file_symbols` y `code_hybrid_search` en el proyecto "pando" para:
- Leer `internal/tui/tui.go` completo - keyMap actual y todos los bindings
- Buscar todos los key.Binding usados en el proyecto
- Ver cómo el help dialog muestra shortcuts

## Requisitos
1. Extraer keyMap de tui.go a `internal/tui/keys.go`
2. Crear struct jerárquico:
   ```go
   type KeyMap struct {
       Global   GlobalKeys   // Quit, Help, Logs
       Chat     ChatKeys     // Send, NewLine, Cancel, Scroll
       Editor   EditorKeys   // (preparar para futuro, vacío por ahora)
       FileTree FileTreeKeys // (preparar para futuro, vacío por ahora)
   }
   ```
3. Implementar `help.KeyMap` interface para auto-generar help
4. Mejorar Help overlay para mostrar shortcuts por contexto/categoría
5. NO cambiar los shortcuts actuales, solo reorganizar

## Importante
- Este es un refactor, NO debe cambiar comportamiento
- Todos los tests existentes deben seguir pasando
```

---

### OLEADA 2 (Esperar a que Oleada 1 termine)

#### Agent-1C: File Viewer Component
```
ID: tui-file-viewer
Dependencias: [tui-chroma-highlight]
Archivos a crear:
  - internal/tui/components/editor/viewer.go
  - internal/tui/components/editor/keys.go
```

**Prompt**:
```
Eres un experto en Go y bubbletea. Tu tarea es crear el File Viewer component para la TUI de Pando.

## Contexto
Usa las tools de remembrances:
1. `kb_get_document("pando/tui-enhancement/03-phase3-editor-syntax-highlighting.md")` - Especificación completa
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Sección 1B
3. `kb_get_document("pando/tui-enhancement/11-external-libraries-reference.md")` - viewport de bubbles

Usa `code_hybrid_search` y `code_get_file_symbols` en el proyecto "pando" para:
- Leer `internal/tui/components/editor/highlight.go` - El Highlighter creado previamente
- Ver cómo se usa viewport de bubbles en el proyecto
- Buscar patrones de componentes existentes para mantener consistencia

## Requisitos
1. **viewer.go**: Componente read-only que muestra archivos con:
   - Syntax highlighting via el Highlighter existente en highlight.go
   - Números de línea usando `viewport.LeftGutterFunc`
   - Scroll vertical/horizontal
   - Búsqueda con `/` o ctrl+f usando `viewport.SetHighlights()`
   - Current line highlight usando `viewport.StyleLineFunc`
   - Método `OpenFile(path string) tea.Cmd` para cargar archivos
   - Método `SetSize(w, h int)` para responsive layout
2. **keys.go**: KeyMap del viewer (j/k scroll, g/G top/bottom, / search, n/N next/prev match)

## Importante
- Usar viewport de bubbles como base (ya es dependencia)
- Reutilizar el Highlighter de highlight.go, NO reimplementar
- El viewer es read-only por ahora (edición es futuro)
```

#### Agent-1D: Tab System
```
ID: tui-tabs
Dependencias: [tui-filetree]
Archivos a crear:
  - internal/tui/components/editor/tabs.go
```

**Prompt**:
```
Eres un experto en Go y bubbletea/lipgloss. Tu tarea es crear un sistema de tabs para archivos abiertos.

## Contexto
Usa las tools de remembrances:
1. `kb_get_document("pando/tui-enhancement/03-phase3-editor-syntax-highlighting.md")` - Sección de tabs
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Sección 1C

Usa `code_hybrid_search` en el proyecto "pando" para:
- Buscar el FileNode struct en `internal/tui/components/filetree/node.go` (creado por Agent-1A)
- Ver los iconos disponibles en `internal/tui/styles/icons.go`
- Ver los estilos y colores del tema

## Requisitos
Crea `internal/tui/components/editor/tabs.go` con:

1. **TabBar struct**: Barra de tabs con:
   - Lista de tabs abiertos (path, nombre, dirty flag)
   - Tab activo highlighted
   - Icono por tipo de archivo + nombre + indicador dirty (punto)
   - Overflow con scroll horizontal si hay muchos tabs
2. **Métodos**:
   - `OpenTab(path string)` - Abre o enfoca tab existente
   - `CloseTab(index int)` - Cierra tab
   - `ActiveTab() string` - Path del tab activo
   - `SetSize(width int)` - Adaptar al ancho
3. **Keybindings**: ctrl+w cerrar, ctrl+tab/ctrl+shift+tab cambiar

## Importante
- Los tabs solo gestionan estado (qué archivos están abiertos)
- NO renderizan el contenido del archivo (eso es el Viewer)
- Diseño visual compacto, una línea de alto
```

---

### OLEADA 3 (Esperar a que Oleada 2 termine)

#### Agent-1E: Layout Integration
```
ID: tui-layout-integration
Dependencias: [tui-filetree, tui-file-viewer, tui-tabs, tui-keybindings]
Archivos a modificar:
  - internal/tui/tui.go
  - internal/tui/page/chat.go (o crear nueva página)
  - internal/tui/layout/ (posibles cambios)
```

**Prompt**:
```
Eres un experto en Go y bubbletea. Tu tarea es integrar los nuevos componentes (FileTree, Viewer, Tabs) en el layout principal de Pando.

## Contexto
Usa las tools de remembrances:
1. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Sección 1D Layout
2. `kb_get_document("pando/tui-enhancement/08-pando-current-state-detailed.md")` - Arquitectura actual
3. `kb_get_document("pando/tui-enhancement/10-crush-architecture-deep-dive.md")` - Cómo crush maneja layout

Usa `code_hybrid_search` y `code_get_file_symbols` en el proyecto "pando" para:
- Leer `internal/tui/tui.go` - Modelo principal
- Leer `internal/tui/layout/` - SplitPane existente
- Leer `internal/tui/page/chat.go` - ChatPage actual
- Leer los nuevos componentes creados:
  - `internal/tui/components/filetree/filetree.go`
  - `internal/tui/components/editor/viewer.go`
  - `internal/tui/components/editor/tabs.go`
  - `internal/tui/keys.go`

## Requisitos
1. Añadir 3 modos de layout al appModel:
   - **Chat only** (actual, default)
   - **Sidebar + Chat** (filetree a la izquierda)
   - **Sidebar + Editor** (filetree + viewer con tabs)
2. Toggle sidebar con ctrl+b
3. Cuando se selecciona archivo en filetree → abrir en viewer
4. Routing de teclas según focus (filetree vs chat vs editor)
5. Usar SplitPaneLayout existente, extenderlo si es necesario
6. Responsive: redistribuir paneles al cambiar tamaño del terminal

## Importante
- NO romper la funcionalidad del chat existente
- El chat debe seguir siendo el modo por defecto
- Transiciones suaves entre layouts
```

#### Agent-2: DiffView Component
```
ID: tui-diffview
Dependencias: [tui-chroma-highlight]
Archivos a crear:
  - internal/tui/components/diff/diffview.go
  - internal/tui/components/diff/parser.go
  - internal/tui/components/diff/styles.go
```

**Prompt**:
```
Eres un experto en Go y bubbletea. Tu tarea es implementar un DiffView completo para Pando.

## Contexto
Usa las tools de remembrances:
1. `kb_get_document("pando/tui-enhancement/06-phase6-diff-viewer-changes.md")` - Especificación COMPLETA del DiffView
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Fase 2
3. `kb_get_document("pando/tui-enhancement/10-crush-architecture-deep-dive.md")` - Cómo crush implementa diffs

Usa `code_hybrid_search` en el proyecto "pando" para:
- Buscar `internal/diff/` - Diff computation existente en Pando
- Leer `internal/tui/components/editor/highlight.go` - Highlighter para syntax en diffs
- Buscar cómo el permission dialog muestra diffs actualmente
- Ver colores DiffAdded/DiffRemoved en el tema

## Requisitos
1. **parser.go**: Parsear unified diffs en estructuras Hunk/DiffLine
2. **diffview.go**: Componente bubbletea con:
   - Modo unified (default) y split (lado a lado)
   - Toggle con tecla `t`
   - Syntax highlighting en contenido (reutilizar Highlighter)
   - Números de línea old/new
   - Navegación entre hunks con `]c` / `[c`
   - Scroll con j/k, page up/down
   - Context lines configurable (default 3)
3. **styles.go**: Estilos usando colores del tema (DiffAdded, DiffRemoved, DiffContext)

## Importante
- Reutilizar el Highlighter de `internal/tui/components/editor/highlight.go`
- Reutilizar `internal/diff/` si tiene funciones útiles de cómputo de diffs
- El componente debe poder integrarse como overlay Y como panel
```

---

### OLEADA 4 (Esperar a que Oleada 3 termine)

#### Agent-3: Mouse Support con Bubblezone
```
ID: tui-mouse-support
Dependencias: [tui-filetree, tui-file-viewer, tui-tabs, tui-diffview, tui-layout-integration]
Archivos a crear:
  - internal/tui/zone/zone.go
Archivos a modificar:
  - internal/tui/tui.go (Init, View, Update para mouse)
  - Todos los View() de componentes nuevos
```

**Prompt**:
```
Eres un experto en Go, bubbletea y bubblezone. Tu tarea es añadir soporte completo de mouse a Pando.

## Contexto
Usa las tools de remembrances:
1. `kb_get_document("pando/tui-enhancement/05-phase5-mouse-support-bubblezone.md")` - Especificación completa
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Fase 3
3. `kb_get_document("pando/tui-enhancement/11-external-libraries-reference.md")` - bubblezone API

Usa `code_hybrid_search` en el proyecto "pando" para:
- Buscar cómo se importa bubblezone actualmente (ya es dependencia)
- Leer todos los View() de los nuevos componentes:
  - `internal/tui/components/filetree/filetree.go`
  - `internal/tui/components/editor/viewer.go`
  - `internal/tui/components/editor/tabs.go`
  - `internal/tui/components/diff/diffview.go`
- Leer `internal/tui/tui.go` para entender el Init() y routing

## Requisitos
1. **zone.go**: Zone manager con IDs constantes para cada zona clickeable
2. Activar `tea.EnableMouseCellMotion` en Init()
3. Añadir `zone.Mark()` en View() de:
   - File tree items (click = abrir/expandir)
   - Tabs (click = activar, middle-click = cerrar)
   - Sidebar items
   - Status bar elements
   - Botones de diálogos
4. Mouse wheel scroll en viewports
5. `zone.Manager.Scan()` en el View() final de tui.go
6. handleMouse(tea.MouseMsg) en Update() del modelo principal

## Importante
- bubblezone YA es dependencia, no añadir
- No romper navegación por teclado existente
- Mouse es complemento, no reemplazo
```

#### Agent-5: Markdown Rendering Mejoras
```
ID: tui-markdown-improve
Dependencias: [tui-chroma-highlight, tui-mouse-support]
Archivos a modificar:
  - internal/tui/components/chat/message.go (o similar)
  - internal/tui/styles/ (markdown styles)
```

**Prompt**:
```
Eres un experto en Go, glamour y chroma. Tu tarea es mejorar el rendering de markdown en el chat de Pando.

## Contexto
Usa las tools de remembrances:
1. `kb_get_document("pando/tui-enhancement/04-phase4-markdown-rendering.md")` - Especificación completa
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Fase 5
3. `kb_get_document("pando/tui-enhancement/08-pando-current-state-detailed.md")` - Estado actual de glamour

Usa `code_hybrid_search` en el proyecto "pando" para:
- Buscar cómo se usa glamour actualmente en el proyecto
- Leer `internal/tui/components/chat/` - Rendering actual de mensajes
- Leer `internal/tui/components/editor/highlight.go` - Highlighter para code blocks
- Buscar estilos MarkdownText, MarkdownHeading en el tema

## Requisitos
1. Verificar/mejorar que estilos de glamour usen colores del tema (MarkdownText, MarkdownHeading, etc.)
2. Code blocks con syntax highlighting via chroma (reutilizar Highlighter)
3. Streaming: debounce de re-render durante streaming de respuestas AI
4. Links clickeables integrados con bubblezone (zone.Mark en URLs)
5. Tablas y listas con mejor formato

## Importante
- glamour YA es dependencia, no añadir
- Mejoras incrementales sobre lo existente, no reescribir
- Performance: cache de renders de markdown, solo re-render en cambio
```

---

## Resumen de Ejecución

| Oleada | Agentes | Paralelos | Tiempo Est. | Bloquea |
|--------|---------|-----------|-------------|---------|
| 1 | 1A, 1B, 4, 6 | 4 en paralelo | - | Oleada 2 |
| 2 | 1C, 1D | 2 en paralelo | - | Oleada 3 |
| 3 | 1E, 2 | 2 en paralelo | - | Oleada 4 |
| 4 | 3, 5 | 2 en paralelo | - | Fin |

**Total**: 10 subagentes, 4 oleadas, máximo 4 agentes simultáneos

## Notas de Control

### Validación entre oleadas
Antes de lanzar la siguiente oleada, verificar:
1. Que los archivos creados compilen (`go build ./...`)
2. Que las interfaces expuestas sean consistentes entre agentes
3. Que no haya conflictos en imports o nombres de paquetes

### Rollback
Cada oleada debe hacerse en una rama git separada:
- `feat/tui-wave-1`, `feat/tui-wave-2`, etc.
- Merge a main solo tras validación

### Coordinación de interfaces
Los agentes de Oleada 2+ deben leer con `code_get_file_symbols` los archivos creados por oleadas anteriores para conocer las interfaces reales, no asumir.
