# Execution Plan with Mesnada - TUI Enhancement

## Base Subagent Configuration
- **Engine**: `copilot`
- **Model**: `gpt-5.4`
- **Working dir**: `/www/MCP/Pando/pando`
- **Common tools**: remembrances (kb_search_documents, kb_get_document, code_hybrid_search, code_find_symbol, code_get_file_symbols)

## Dependency Graph

```
WAVE 1 (Parallel - No dependencies)
├── Agent-1A: File Tree Component
├── Agent-1B: Syntax Highlight Base (chroma)
├── Agent-4:  Command Palette Fuzzy Search
└── Agent-6:  Keybindings Refactoring

WAVE 2 (Depends on 1A + 1B)
├── Agent-1C: File Viewer Component (uses chroma from 1B)
└── Agent-1D: Tab System

WAVE 3 (Depends on 1C + 1D + 1A)
├── Agent-1E: Layout Integration (connects filetree + editor + tabs)
└── Agent-2:  DiffView Component (uses chroma from 1B)

WAVE 4 (Depends on previous waves)
├── Agent-3:  Mouse Support with Bubblezone
└── Agent-5:  Markdown Rendering Improvements
```

## Wave Justification

### Why these are parallel in Wave 1:
- **Agent-1A** (File Tree): Isolated component in `internal/tui/components/filetree/`, doesn't depend on anything new
- **Agent-1B** (Chroma): Utility highlighting package in `internal/tui/components/editor/highlight.go`, base for others
- **Agent-4** (Fuzzy): Modifies existing `dialog/commands.go` and `dialog/complete.go`, independent of the rest
- **Agent-6** (Keybindings): Refactor of `tui.go` keyMap to hierarchical struct, independent

### Why Wave 2 waits for Wave 1:
- **Agent-1C** (Viewer): Needs `highlight.go` from Agent-1B for syntax highlighting
- **Agent-1D** (Tabs): Needs to know the FileNode interface from Agent-1A to open files

### Why Wave 3 waits for Wave 2:
- **Agent-1E** (Layout): Needs all components (filetree, viewer, tabs) to integrate them into SplitPane
- **Agent-2** (DiffView): Reuses chroma from 1B, and the viewer pattern from 1C

### Why Wave 4 waits for Wave 3:
- **Agent-3** (Mouse): Needs all components to exist to add zone.Mark() to their View()
- **Agent-5** (Markdown): Improves code blocks with chroma and clickable links with bubblezone (needs Agent-3)

---

## Subagent Details

### WAVE 1

#### Agent-1A: File Tree Component
```
ID: tui-filetree
Dependencies: none
Files to create:
  - internal/tui/components/filetree/node.go
  - internal/tui/components/filetree/filetree.go
  - internal/tui/components/filetree/loader.go
  - internal/tui/components/filetree/keys.go
```

**Prompt**:
```
You are an expert in Go and bubbletea (charmbracelet). Your task is to implement a FileTree component for Pando's TUI.

## Context
Use the remembrances tools to obtain context:
1. `kb_get_document("pando/tui-enhancement/02-phase2-file-explorer.md")` - Complete file explorer specification
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Revised plan, section 1A
3. `kb_get_document("pando/tui-enhancement/08-pando-current-state-detailed.md")` - Current state of Pando
4. `kb_get_document("pando/tui-enhancement/11-external-libraries-reference.md")` - Available libraries

Use `code_hybrid_search` in the "pando" project to search for:
- The existing component structure in `internal/tui/components/`
- How other components (dialog, chat) are implemented as pattern references
- The existing theme and styles in `internal/tui/styles/` and `internal/tui/theme/`
- How `.gitignore` is used in the project

## Requirements
Create the `internal/tui/components/filetree/` package with:

1. **node.go**: FileNode struct with fields (Name, Path, IsDir, IsExpanded, Children, GitStatus, Depth)
2. **filetree.go**: bubbletea component with:
   - Tree view with expand/collapse
   - Navigation j/k (up/down), h/l (collapse/expand), enter (open)
   - Search with "/"
   - Update(tea.Msg) method that returns (tea.Model, tea.Cmd)
   - View() string method
   - SelectedFile() method to get the selected file
3. **loader.go**: Lazy loading of directories + git status integration
   - Respect .gitignore
   - Only load children on expand (lazy)
   - Git status icons using DiffAdded/DiffRemoved theme colors
4. **keys.go**: Specific keyMap for the file tree

## Patterns to follow
- Use lipgloss for styles, reuse existing theme colors
- The component should expose a clean interface (Init, Update, View, SetSize)
- Do NOT integrate with the main layout yet (that's Agent-1E's task)
```

#### Agent-1B: Syntax Highlighting Base (Chroma)
```
ID: tui-chroma-highlight
Dependencies: none
Files to create:
  - internal/tui/components/editor/highlight.go
```

**Prompt**:
```
You are an expert in Go. Your task is to create the base syntax highlighting module using chroma for Pando's TUI.

## Context
Use the remembrances tools to obtain context:
1. `kb_get_document("pando/tui-enhancement/03-phase3-editor-syntax-highlighting.md")` - Editor and highlighting specification
2. `kb_get_document("pando/tui-enhancement/10-crush-architecture-deep-dive.md")` - How crush implements highlighting
3. `kb_get_document("pando/tui-enhancement/11-external-libraries-reference.md")` - chroma reference

Use `code_hybrid_search` in the "pando" project to search for:
- The syntax colors in the theme: SyntaxComment, SyntaxKeyword, SyntaxString, etc.
- The go.mod to check if chroma is already a dependency
- The structure of `internal/tui/theme/` to understand the theme system

## Requirements
Create `internal/tui/components/editor/highlight.go` with:

1. **Highlighter struct**: Lexer and result cache
   - `Highlight(source, fileName string) (string, error)` - Detect lexer by extension, apply highlighting
   - `HighlightLine(line, fileName string) string` - Highlight individual line
   - LRU cache to avoid re-highlighting
2. Use `github.com/alecthomas/chroma/v2` with `terminal16m` formatter
3. Map Pando theme colors to chroma style
4. If chroma is not in go.mod, include instructions for `go get`

## Important
- This module will be reused by the File Viewer (Agent-1C) and DiffView (Agent-2)
- It must be an independent package without coupling to specific UI components
- Prioritize performance with aggressive caching
```

#### Agent-4: Command Palette with Fuzzy Search
```
ID: tui-fuzzy-palette
Dependencies: none
Files to modify:
  - internal/tui/components/dialog/commands.go
  - internal/tui/components/dialog/complete.go
```

**Prompt**:
```
You are an expert in Go and bubbletea. Your task is to improve Pando's existing Command Palette by adding fuzzy search.

## Context
Use the remembrances tools:
1. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Phase 4: Command Palette
2. `kb_get_document("pando/tui-enhancement/01-phase1-keybindings-commands.md")` - Commands specification

Use `code_hybrid_search` and `code_get_file_symbols` in the "pando" project to:
- Read `internal/tui/components/dialog/commands.go` - Current command dialog
- Read `internal/tui/components/dialog/complete.go` - Current completion dialog
- Search how commands are currently registered
- Search for fuzzy matching libraries in Go (sahilm/fuzzy or similar)

## Requirements
1. Add fuzzy matching to command filtering (use `sahilm/fuzzy` or implement basic scoring)
2. Categorize commands: General, Files, Sessions, Models, View
3. Show shortcut next to each command in the list
4. Keep opening with ctrl+k, add ctrl+p as alias
5. Ranking by usage frequency + fuzzy match score

## Important
- Do NOT break existing CommandDialog functionality
- Maintain compatibility with current overlay system
```

#### Agent-6: Keybindings Refactoring
```
ID: tui-keybindings
Dependencies: none
Files to create:
  - internal/tui/keys.go (extracted from tui.go)
Files to modify:
  - internal/tui/tui.go
```

**Prompt**:
```
You are an expert in Go and bubbletea. Your task is to refactor Pando's keybindings system.

## Context
Use the remembrances tools:
1. `kb_get_document("pando/tui-enhancement/01-phase1-keybindings-commands.md")` - Complete specification
2. `kb_get_document("pando/tui-enhancement/07-crush-vs-pando-comparison.md")` - Comparison with crush
3. `kb_get_document("pando/tui-enhancement/08-pando-current-state-detailed.md")` - Current state

Use `code_get_file_symbols` and `code_hybrid_search` in the "pando" project to:
- Read complete `internal/tui/tui.go` - Current keyMap and all bindings
- Search all key.Binding used in the project
- See how the help dialog displays shortcuts

## Requirements
1. Extract keyMap from tui.go to `internal/tui/keys.go`
2. Create hierarchical struct:
   ```go
   type KeyMap struct {
       Global   GlobalKeys   // Quit, Help, Logs
       Chat     ChatKeys     // Send, NewLine, Cancel, Scroll
       Editor   EditorKeys   // (prepare for future, empty for now)
       FileTree FileTreeKeys // (prepare for future, empty for now)
   }
   ```
3. Implement `help.KeyMap` interface for auto-generating help
4. Improve Help overlay to show shortcuts by context/category
5. Do NOT change current shortcuts, only reorganize

## Important
- This is a refactoring, it must NOT change behavior
- All existing tests must continue passing
```

---

### WAVE 2 (Wait for Wave 1 to finish)

#### Agent-1C: File Viewer Component
```
ID: tui-file-viewer
Dependencies: [tui-chroma-highlight]
Files to create:
  - internal/tui/components/editor/viewer.go
  - internal/tui/components/editor/keys.go
```

**Prompt**:
```
You are an expert in Go and bubbletea. Your task is to create the File Viewer component for Pando's TUI.

## Context
Use the remembrances tools:
1. `kb_get_document("pando/tui-enhancement/03-phase3-editor-syntax-highlighting.md")` - Complete specification
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Section 1B
3. `kb_get_document("pando/tui-enhancement/11-external-libraries-reference.md")` - bubbles viewport

Use `code_hybrid_search` and `code_get_file_symbols` in the "pando" project to:
- Read `internal/tui/components/editor/highlight.go` - The Highlighter previously created
- See how bubbles viewport is used in the project
- Search existing component patterns for consistency

## Requirements
1. **viewer.go**: Read-only component that displays files with:
   - Syntax highlighting via existing Highlighter in highlight.go
   - Line numbers using `viewport.LeftGutterFunc`
   - Vertical/horizontal scrolling
   - Search with `/` or ctrl+f using `viewport.SetHighlights()`
   - Current line highlight using `viewport.StyleLineFunc`
   - `OpenFile(path string) tea.Cmd` method to load files
   - `SetSize(w, h int)` method for responsive layout
2. **keys.go**: Viewer keyMap (j/k scroll, g/G top/bottom, / search, n/N next/prev match)

## Important
- Use bubbles viewport as base (already a dependency)
- Reuse the Highlighter from highlight.go, do NOT reimplement
- The viewer is read-only for now (editing is future)
```

#### Agent-1D: Tab System
```
ID: tui-tabs
Dependencies: [tui-filetree]
Files to create:
  - internal/tui/components/editor/tabs.go
```

**Prompt**:
```
You are an expert in Go and bubbletea/lipgloss. Your task is to create a tab system for open files.

## Context
Use the remembrances tools:
1. `kb_get_document("pando/tui-enhancement/03-phase3-editor-syntax-highlighting.md")` - Tabs section
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Section 1C

Use `code_hybrid_search` in the "pando" project to:
- Search for the FileNode struct in `internal/tui/components/filetree/node.go` (created by Agent-1A)
- See available icons in `internal/tui/styles/icons.go`
- See theme styles and colors

## Requirements
Create `internal/tui/components/editor/tabs.go` with:

1. **TabBar struct**: Tab bar with:
   - List of open tabs (path, name, dirty flag)
   - Active tab highlighted
   - Icon by file type + name + dirty indicator (dot)
   - Overflow with horizontal scrolling if many tabs
2. **Methods**:
   - `OpenTab(path string)` - Open or focus existing tab
   - `CloseTab(index int)` - Close tab
   - `ActiveTab() string` - Path of active tab
   - `SetSize(width int)` - Adapt to width
3. **Keybindings**: ctrl+w close, ctrl+tab/ctrl+shift+tab switch

## Important
- Tabs only manage state (which files are open)
- They do NOT render file content (that's the Viewer)
- Compact visual design, one line height
```

---

### WAVE 3 (Wait for Wave 2 to finish)

#### Agent-1E: Layout Integration
```
ID: tui-layout-integration
Dependencies: [tui-filetree, tui-file-viewer, tui-tabs, tui-keybindings]
Files to modify:
  - internal/tui/tui.go
  - internal/tui/page/chat.go (or create new page)
  - internal/tui/layout/ (possible changes)
```

**Prompt**:
```
You are an expert in Go and bubbletea. Your task is to integrate the new components (FileTree, Viewer, Tabs) into Pando's main layout.

## Context
Use the remembrances tools:
1. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Section 1D Layout
2. `kb_get_document("pando/tui-enhancement/08-pando-current-state-detailed.md")` - Current architecture
3. `kb_get_document("pando/tui-enhancement/10-crush-architecture-deep-dive.md")` - How crush handles layout

Use `code_hybrid_search` and `code_get_file_symbols` in the "pando" project to:
- Read `internal/tui/tui.go` - Main model
- Read `internal/tui/layout/` - Existing SplitPane
- Read `internal/tui/page/chat.go` - Current ChatPage
- Read the new components created:
  - `internal/tui/components/filetree/filetree.go`
  - `internal/tui/components/editor/viewer.go`
  - `internal/tui/components/editor/tabs.go`
  - `internal/tui/keys.go`

## Requirements
1. Add 3 layout modes to appModel:
   - **Chat only** (current, default)
   - **Sidebar + Chat** (filetree on the left)
   - **Sidebar + Editor** (filetree + viewer with tabs)
2. Toggle sidebar with ctrl+b
3. When file is selected in filetree → open in viewer
4. Key routing based on focus (filetree vs chat vs editor)
5. Use existing SplitPaneLayout, extend if necessary
6. Responsive: redistribute panels when terminal size changes

## Important
- Do NOT break existing chat functionality
- Chat must remain the default mode
- Smooth transitions between layouts
```

#### Agent-2: DiffView Component
```
ID: tui-diffview
Dependencies: [tui-chroma-highlight]
Files to create:
  - internal/tui/components/diff/diffview.go
  - internal/tui/components/diff/parser.go
  - internal/tui/components/diff/styles.go
```

**Prompt**:
```
You are an expert in Go and bubbletea. Your task is to implement a complete DiffView for Pando.

## Context
Use the remembrances tools:
1. `kb_get_document("pando/tui-enhancement/06-phase6-diff-viewer-changes.md")` - COMPLETE DiffView specification
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Phase 2
3. `kb_get_document("pando/tui-enhancement/10-crush-architecture-deep-dive.md")` - How crush implements diffs

Use `code_hybrid_search` in the "pando" project to:
- Search `internal/diff/` - Existing diff computation in Pando
- Read `internal/tui/components/editor/highlight.go` - Highlighter for syntax in diffs
- Search how the permission dialog currently shows diffs
- See DiffAdded/DiffRemoved colors in the theme

## Requirements
1. **parser.go**: Parse unified diffs into Hunk/DiffLine structures
2. **diffview.go**: bubbletea component with:
   - Unified mode (default) and split (side by side)
   - Toggle with `t` key
   - Syntax highlighting in content (reuse Highlighter)
   - Old/new line numbers
   - Hunk navigation with `]c` / `[c`
   - Scroll with j/k, page up/down
   - Configurable context lines (default 3)
3. **styles.go**: Styles using theme colors (DiffAdded, DiffRemoved, DiffContext)

## Important
- Reuse the Highlighter from `internal/tui/components/editor/highlight.go`
- Reuse `internal/diff/` if it has useful diff computation functions
- The component must be able to integrate as overlay AND as panel
```

---

### WAVE 4 (Wait for Wave 3 to finish)

#### Agent-3: Mouse Support with Bubblezone
```
ID: tui-mouse-support
Dependencies: [tui-filetree, tui-file-viewer, tui-tabs, tui-diffview, tui-layout-integration]
Files to create:
  - internal/tui/zone/zone.go
Files to modify:
  - internal/tui/tui.go (Init, View, Update for mouse)
  - All new components' View()
```

**Prompt**:
```
You are an expert in Go, bubbletea and bubblezone. Your task is to add complete mouse support to Pando.

## Context
Use the remembrances tools:
1. `kb_get_document("pando/tui-enhancement/05-phase5-mouse-support-bubblezone.md")` - Complete specification
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Phase 3
3. `kb_get_document("pando/tui-enhancement/11-external-libraries-reference.md")` - bubblezone API

Use `code_hybrid_search` in the "pando" project to:
- Search how bubblezone is currently imported (already a dependency)
- Read all View() of new components:
  - `internal/tui/components/filetree/filetree.go`
  - `internal/tui/components/editor/viewer.go`
  - `internal/tui/components/editor/tabs.go`
  - `internal/tui/components/diff/diffview.go`
- Read `internal/tui/tui.go` to understand Init() and routing

## Requirements
1. **zone.go**: Zone manager with constant IDs for each clickable zone
2. Activate `tea.EnableMouseCellMotion` in Init()
3. Add `zone.Mark()` in View() of:
   - File tree items (click = open/expand)
   - Tabs (click = activate, middle-click = close)
   - Sidebar items
   - Status bar elements
   - Dialog buttons
4. Mouse wheel scroll in viewports
5. `zone.Manager.Scan()` in tui.go's final View()
6. handleMouse(tea.MouseMsg) in main model's Update()

## Important
- bubblezone is ALREADY a dependency, do not add
- Do not break existing keyboard navigation
- Mouse is complement, not replacement
```

#### Agent-5: Markdown Rendering Improvements
```
ID: tui-markdown-improve
Dependencies: [tui-chroma-highlight, tui-mouse-support]
Files to modify:
  - internal/tui/components/chat/message.go (or similar)
  - internal/tui/styles/ (markdown styles)
```

**Prompt**:
```
You are an expert in Go, glamour and chroma. Your task is to improve markdown rendering in Pando's chat.

## Context
Use the remembrances tools:
1. `kb_get_document("pando/tui-enhancement/04-phase4-markdown-rendering.md")` - Complete specification
2. `kb_get_document("pando/tui-enhancement/09-revised-implementation-priorities.md")` - Phase 5
3. `kb_get_document("pando/tui-enhancement/08-pando-current-state-detailed.md")` - Current glamour state

Use `code_hybrid_search` in the "pando" project to:
- Search how glamour is currently used in the project
- Read `internal/tui/components/chat/` - Current message rendering
- Read `internal/tui/components/editor/highlight.go` - Highlighter for code blocks
- Search for MarkdownText, MarkdownHeading styles in the theme

## Requirements
1. Verify/improve that glamour styles use theme colors (MarkdownText, MarkdownHeading, etc.)
2. Code blocks with syntax highlighting via chroma (reuse Highlighter)
3. Streaming: debounce re-render during AI response streaming
4. Clickable links integrated with bubblezone (zone.Mark on URLs)
5. Tables and lists with better formatting

## Important
- glamour is ALREADY a dependency, do not add
- Incremental improvements on existing, do not rewrite
- Performance: cache markdown renders, only re-render on change
```

---

## Execution Summary

| Wave | Agents | Parallel | Est. Time | Blocks |
|------|--------|----------|-----------|--------|
| 1 | 1A, 1B, 4, 6 | 4 in parallel | - | Wave 2 |
| 2 | 1C, 1D | 2 in parallel | - | Wave 3 |
| 3 | 1E, 2 | 2 in parallel | - | Wave 4 |
| 4 | 3, 5 | 2 in parallel | - | End |

**Total**: 10 subagents, 4 waves, maximum 4 simultaneous agents

## Control Notes

### Validation between waves
Before launching the next wave, verify:
1. That created files compile (`go build ./...`)
2. That exposed interfaces are consistent between agents
3. That there are no conflicts in imports or package names

### Rollback
Each wave should be done on a separate git branch:
- `feat/tui-wave-1`, `feat/tui-wave-2`, etc.
- Merge to main only after validation

### Interface Coordination
Agents in Wave 2+ must use `code_get_file_symbols` to read files created by previous waves to know the actual interfaces, not assume.