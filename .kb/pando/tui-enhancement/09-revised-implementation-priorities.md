# Revised Implementation Plan - Corrected Priorities

## Context
After thorough analysis, Pando already has:
- glamour and bubblezone as dependencies
- Sidebar with modified files and diff stats
- 18+ keybindings defined
- Basic markdown rendering with glamour
- 9 themes with colors for diff, markdown, and syntax
- Basic overlay system

## Revised Phases (Ordered by Real Impact)

### Phase 1: File Tree Navigator + Editor Viewer (HIGHEST IMPACT)
**Priority: CRITICAL** - This is what differentiates a chat from an IDE

**1A. File Tree Component** (new component, don't modify existing sidebar)
- Directory: `internal/tui/components/filetree/`
- Tree view with expand/collapse
- Git status icons (uses DiffAdded/DiffRemoved theme colors)
- Keybindings: j/k, enter, h/l, /
- Respects .gitignore
- Lazy loading per directory

**1B. File Viewer Component**
- Directory: `internal/tui/components/editor/`
- Read-only viewer with syntax highlighting (chroma)
- Line numbers, scrolling
- Search with `/` or ctrl+f
- **New dependency**: `github.com/alecthomas/chroma/v2`
- Use SyntaxComment/SyntaxKeyword/etc. colors from existing theme

**1C. Tab System**
- Multiple open files
- Tab bar with icon + name + dirty indicator
- ctrl+w to close, ctrl+tab to switch

**1D. Layout Integration**
- Toggle sidebar with ctrl+b
- Use existing SplitPaneLayout with 3 layouts:
  - Chat only (current)
  - Sidebar + Chat
  - Sidebar + Editor (or Sidebar + Editor + Chat in 3 panels)

**Key files to create**:
```
internal/tui/components/filetree/
  ├── node.go       # FileNode struct
  ├── filetree.go   # Main component
  ├── loader.go     # Dir loading + git status
  └── keys.go       # Keybindings

internal/tui/components/editor/
  ├── viewer.go     # File viewer
  ├── tabs.go       # Tab system
  ├── highlight.go  # chroma integration
  └── keys.go       # Keybindings
```

### Phase 2: Complete DiffView (HIGH IMPACT)
**Priority: HIGH** - Sidebar already shows stats, the complete viewer is missing

- Port DiffView concept from crush
- Modes: unified and split
- Syntax highlighting in diff (reuse chroma from Phase 1)
- Integrate into:
  - Permission dialog (already shows basic diffs)
  - Changes panel (existing sidebar -> click to view complete diff)
  - As standalone page or overlay
- Navigate between hunks with ]c / [c

**Files to create**:
```
internal/tui/components/diff/
  ├── diffview.go     # DiffView unified + split
  ├── parser.go       # Diff parsing
  └── styles.go       # Styles (use DiffAdded/DiffRemoved from theme)
```

### Phase 3: Active Mouse Support (MEDIUM IMPACT)
**Priority: MEDIUM** - bubblezone already imported, just need to use it

- Enable `tea.EnableMouseCellMotion` in Init()
- Create zone manager (`internal/tui/zone/`)
- Make clickable:
  - File tree items (open/expand)
  - Editor tabs (switch/close)
  - Sidebar items (view diff)
  - Status bar elements (model, session)
  - Dialog buttons
- Mouse wheel scroll in all viewports
- `zone.Manager.Scan()` in final View()

**Files to create/modify**:
```
internal/tui/zone/zone.go  # Zone manager + IDs
# Modify all View() to Mark() zones
# Modify tui.go for Scan() and handleMouse()
```

### Phase 4: Command Palette with Fuzzy Search (MEDIUM IMPACT)
**Priority: MEDIUM** - CommandDialog exists but is basic

- Add fuzzy matching to existing CompletionDialog
- Command categories (General, Files, Sessions, Models, View)
- Show shortcut next to each command
- Integrate with all registered commands
- Open with ctrl+k (already exists) or ctrl+p

**Files to modify**:
```
internal/tui/components/dialog/commands.go   # Add fuzzy
internal/tui/components/dialog/complete.go   # Reuse for fuzzy
```

### Phase 5: Markdown Rendering Improvements (LOW IMPACT)
**Priority: LOW** - glamour already works, just improve

- Verify glamour styles use MarkdownText/MarkdownHeading/etc. from theme
- Improve code block rendering with chroma syntax highlighting
- Partial streaming markdown with debounce
- Clickable links (integrate with Phase 3 bubblezone)

**Files to modify**:
```
internal/tui/styles/markdown.go      # Map theme colors
internal/tui/components/chat/message.go  # Improve rendering
```

### Phase 6: Keybinding Refinement (LOW IMPACT)
**Priority: LOW** - Already has 18+ shortcuts, just organize better

- Extract KeyMap to hierarchical struct in separate file
- Improve Help overlay to show all shortcuts by context
- Add new shortcuts for new features (Phases 1-3)
- Document all shortcuts in help

## Actual New Dependencies
```
github.com/alecthomas/chroma/v2  # ONLY new dependency needed
```

## Execution Order
```
Phase 1 (File Tree + Editor) ─── requires chroma
         │
         ├──→ Phase 2 (DiffView) ─── reuses chroma
         │
         └──→ Phase 3 (Mouse) ─── reuses already imported bubblezone
                   │
                   └──→ Phase 5 (Markdown improvements)

Phase 4 (Fuzzy Search) ─── independent
Phase 6 (Keybindings) ─── independent, done incrementally
```

## Effort Estimation (Revised)
| Phase | Effort | New Files | Modified Files |
|-------|--------|-----------|----------------|
| 1 (File Tree + Editor) | High | ~8 | ~3 |
| 2 (DiffView) | Medium | ~3 | ~3 |
| 3 (Mouse) | Medium | ~1 | ~8 |
| 4 (Fuzzy) | Low | 0 | ~2 |
| 5 (Markdown) | Low | 0 | ~2 |
| 6 (Keybindings) | Low | ~1 | ~2 |
