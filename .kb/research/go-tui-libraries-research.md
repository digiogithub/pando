# Research: Go TUI Libraries and Frameworks for IDE-like TUI

Date: 2026-03-07

---

## 1. charmbracelet/bubbles - TUI Components for Bubbletea

### General Description
Bubbles is the official reusable components library for Bubble Tea (TUI framework based on Elm architecture). All components implement the Model-Update-View pattern.

### Available Components

#### viewport - Scrollable content area
- **IDE Use**: Perfect for content panels, file preview, command output
- **Key API**:
  - `New(opts ...Option)` - constructor with options
  - `SetContent(s string)` / `SetContentLines(lines []string)` - set content
  - `GetContent()` - retrieve content
  - `ScrollUp(n)`, `ScrollDown(n)`, `PageUp()`, `PageDown()`, `HalfPageUp()`, `HalfPageDown()`
  - `GotoTop()`, `GotoBottom()` - extreme jumps
  - `SetYOffset(n)`, `SetXOffset(n)` - absolute position
  - `ScrollLeft(n)`, `ScrollRight(n)` - horizontal scroll
  - `AtTop()`, `AtBottom()`, `PastBottom()` - position states
  - `ScrollPercent()`, `HorizontalScrollPercent()` - progress (0-1)
  - `TotalLineCount()`, `VisibleLineCount()` - counts
  - `SetHighlights(matches [][]int)` - mark ranges for search
  - `HighlightNext()`, `HighlightPrevious()` - navigate highlights
  - `EnsureVisible(line, colstart, colend)` - ensure line visibility
  - `LeftGutterFunc GutterFunc` - for line numbers or indicators
  - `StyleLineFunc func(int) lipgloss.Style` - style per line
- **Struct fields**: width, height, yOffset, xOffset, SoftWrap, FillHeight, MouseWheelEnabled, MouseWheelDelta, Style
- **High Performance Mode**: YPosition field for rendering in alt-screen buffer

#### textinput - Single-line input field
- **IDE Use**: Search bars, command palette, name input
- **API**: `New()`, `SetValue(s)`, `Value()`, `Focus()`, `Blur()`, validation, placeholder

#### textarea - Multi-line input field
- **IDE Use**: Text editor, editing areas
- **API**: `New()`, `SetValue(s)`, `Value()`, `LineCount()`, vertical/horizontal scroll, Unicode

#### table - Tabular data
- **IDE Use**: File listings, search results, git status
- **API**: `New(columns, rows)`, `SetHeight(h)`, `SetWidth(w)`, `SelectedRow()`, navigation, selection

#### list - Navigable list with search
- **IDE Use**: File explorer, command palette, file search
- **Features**: Built-in pagination, fuzzy filtering, auto-generated help, spinner, status messages
- **API**: `New(items, delegate, width, height)`, `SetItems(items)`, `SelectedItem()`, filtering

#### filepicker - File selector
- **IDE Use**: Open files, navigate filesystem
- **API**: `New()`, `SelectedFile()`, `AllowedTypes(types)`, directory navigation

#### progress - Progress bar
- **IDE Use**: Loading indicators, operation progress
- **API**: `New()`, `SetPercent(float64)`, solid/gradient fills, Harmonica animation

#### spinner - Activity indicator
- **IDE Use**: Async operations, file loading
- **API**: `New()`, multiple predefined styles, customizable frames

#### help - Help view
- **IDE Use**: Keyboard shortcut bar
- **API**: `New()`, `View(width)`, single-line and multi-line mode, automatic truncation

#### key - Keybindings management
- **IDE Use**: Configurable keyboard shortcut system
- **API**: `NewBinding(opts)`, `Matches(msg, bindings)`, remapping, contextual help text

#### paginator - Pagination logic
- Dots style (iOS) and page numbering

#### timer / stopwatch - Timers
- Countdown and count-up

#### cursor - Cursor management
- Cursor position and style

#### runeutil - Rune utilities
- Key message processing

### Composition Patterns
- Each component is a `Model` with `Update()` and `View()`
- They are embedded as fields in a parent model
- The parent model delegates messages to the corresponding component based on focus state
- They compose visually with lipgloss.JoinHorizontal/JoinVertical

---

## 2. yorukot/superfile - TUI File Manager (Architecture Reference)

### Technology
- Go (88%), built on Bubble Tea + Lipgloss
- Binary: `spf`

### Main Model Architecture

```
model (bubbletea root)
├── fileModel (multi-panel manager)
│   ├── FilePanels[] (individual panels array)
│   ├── FocusedPanelIndex (active panel index)
│   ├── SinglePanelWidth (dynamically calculated width)
│   ├── MaxFilePanel (maximum panel count)
│   └── FilePreview (content preview)
├── sidebarModel (sidebar with directories, renaming)
├── fileMetaData (metadata via exiftool, with cache)
├── processBarModel (background tasks)
├── clipboard
└── Overlapping modals:
    ├── helpMenu (centered overlay)
    ├── promptModal (SPF commands)
    ├── zoxideModal (quick navigation)
    ├── sortModal (sorting)
    ├── notifyModel (confirmations)
    ├── typingModal (text input)
    └── warnModel (confirmation)
```

### Multiple Panels System
- Dynamic array `FilePanels[]` with focus index `FocusedPanelIndex`
- Each panel has: Location, SearchBar, Rename, PanelMode, IsFocused
- Width dynamically calculated: `fileModelWidth = fullWidth - sidebarWidth - border`
- Functions: `getFocusedFilePanel()`, toggle between panels

### Focus System
- States: sidebarFocus, processBarFocus, metadataFocus, nonePanelFocus
- Toggle pattern: activating one deactivates the previous
- IsFocused property in each file panel

### Navigation
- `parentDirectory()` - go up to parent directory
- `enterPanel()` - enter directory or open file
- `sidebarSelectDirectory()` - change directory from sidebar
- Integration with Zoxide for quick navigation

### Rendering (model_render.go)
- Components rendered independently: `sidebarRender()`, `processBarRender()`
- Layout composed with lipgloss
- Minimum terminal size validation: `terminalSizeWarnRender()`
- Dimensions: `mainPanelHeight = fullHeight - 2(border) - footerHeight`
- Modals overlay on top of main interface

### Code Structure
```
src/internal/
├── model.go                    - Main model
├── model_msg.go                - Messages
├── model_render.go             - Rendering
├── handle_panel_movement.go    - Panel movement
├── handle_panel_navigation.go  - Panel navigation
├── handle_file_operations.go   - File operations
├── handle_modal.go             - Modal management
├── file_operations.go          - General operations
├── file_operations_compress.go - Compression
├── file_operations_extract.go  - Extraction
├── key_function.go             - Keyboard functions
├── wheel_function.go           - Mouse wheel functions
├── function.go                 - Main functions
├── config_function.go          - Configuration
├── default_config.go           - Default config
├── type.go / type_utils.go     - Types
├── validation.go               - Validations
├── backend/                    - Backend logic
├── common/                     - Shared code
└── ui/                         - User interface
```

### Lessons for an IDE-like TUI
1. Separate handlers by responsibility (panel_movement, panel_navigation, file_operations)
2. Focus system with enum states
3. Dynamic dimension calculation based on terminal size
4. Modals as overlay on the main interface
5. Cache for expensive metadata (exiftool)

---

## 3. charmbracelet/glow - Markdown Renderer in Terminal

### Architecture
- CLI + TUI in Go, uses Bubble Tea for the interactive interface
- Uses **Glamour** internally for markdown rendering

### Operation Modes
- **TUI**: `glow` without arguments - interactive interface to navigate and search markdown
- **CLI**: `glow file.md` - direct rendering

### Content Sources
- Local files
- Standard input (stdin)
- HTTP/HTTPS URLs
- GitHub/GitLab repositories

### Rendering
- Automatic style detection (dark/light) based on terminal background
- Formatted ANSI output
- Pagination via `less -r`
- Configurable width (`-w`)

### Configuration (glow.yml)
- Visual style
- Rendering width
- Mouse support
- Line numbers

### Structure
```
ui/           - TUI components (bubbletea)
utils/        - utilities
main.go       - entry point
config_cmd.go - configuration
style.go      - styles
github.go     - GitHub integration
gitlab.go     - GitLab integration
```

### Integration with Bubbletea
- The TUI model uses viewport for scrolling rendered markdown
- Glamour renders markdown to ANSI string
- The string is passed to viewport.SetContent()
- User navigates with keyboard/mouse

---

## 4. lrstanley/bubblezone - Mouse Support for Bubbletea

### Problem It Solves
Bubbletea provides basic mouse events (MouseButtonLeft, etc.) but determining WHICH component was clicked in multi-component interfaces is complex. BubbleZone abstracts this.

### Core Concept
Uses **zero-printable-width ANSI markers** that:
1. Are invisible and don't affect `lipgloss.Width()`
2. Are inserted around components with `Mark()`
3. Are scanned with `Scan()` to register positions
4. Are removed from final output before rendering

### Complete API

#### Initialization
```go
zone.NewGlobal()           // Global manager (accessible via package functions)
manager := zone.New()      // Local manager (injectable)
defer zone.Close()         // Stop workers
```

#### Zone Marking
```go
zone.Mark(id string, content string) string
// Wraps content with markers identified by id
// Example: zone.Mark("save-btn", saveButton)
```

#### Scanning (only in root model)
```go
zone.Scan(content string) string
// Scans entire view, registers zone positions, removes markers
// MUST be called ONLY in the root model's View()
```

#### Zone Query
```go
info := zone.Get(id string) *ZoneInfo
// Returns zone info (nil if unknown)

info.InBounds(msg tea.MouseMsg) bool
// Checks if mouse event is within the zone

info.Pos(msg tea.MouseMsg) (x, y int)
// Relative coordinates within zone (0,0 = top-left)
// Returns (-1,-1) if out of bounds

info.IsZero() bool
// True if zone is not yet known
```

#### ZoneInfo Struct
```go
type ZoneInfo struct {
    StartX int  // x top-left (0-based)
    StartY int  // y top-left (0-based)
    EndX   int  // x bottom-right (0-based)
    EndY   int  // y bottom-right (0-based)
}
```

#### Helper Functions
```go
zone.AnyInBounds(model, mouse)                           // Sends MsgZoneInBounds for each zone in bounds
zone.AnyInBoundsAndUpdate(model, mouse) (Model, Cmd)     // Same but returns updated model/cmd
zone.NewPrefix() string                                   // Unique prefix to avoid ID collisions
zone.SetEnabled(bool) / zone.Enabled() bool               // Enable/disable
zone.Clear(id string)                                     // Clear zone data
```

### Complete Usage Pattern

```go
// main.go
func main() {
    zone.NewGlobal()
    defer zone.Close()
    p := tea.NewProgram(model{}, tea.WithAltScreen(), tea.WithMouseCellMotion())
    p.Run()
}

// View() - ROOT model
func (m model) View() string {
    buttons := lipgloss.JoinHorizontal(lipgloss.Top,
        zone.Mark("confirm", okButton),
        zone.Mark("cancel", cancelButton),
    )
    return zone.Scan(m.style.Render(buttons))  // Scan only here
}

// Update()
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.MouseMsg:
        if msg.Button == tea.MouseButtonLeft {
            if zone.Get("confirm").InBounds(msg) {
                m.active = "confirm"
            }
        }
    }
    return m, nil
}
```

### For Nested Components (avoid ID collisions)
```go
type childModel struct {
    id string  // unique prefix
}

func NewChild() childModel {
    return childModel{id: zone.NewPrefix()}
}

func (c childModel) View() string {
    return zone.Mark(c.id+"item-1", item1)  // Prefixed ID
}
```

### Best Practices
1. `Scan()` ONLY in the root model
2. Use `lipgloss.Width()` (not `len()`) - markers are transparent to Width
3. Avoid `MaxHeight/MaxWidth` from lipgloss (they truncate and break zones)
4. Bounds are rectangular (bounding box)
5. For lists: mark each item with unique ID
6. `NewPrefix()` for reusable components

### Requirements
- Alt-screen enabled: `tea.WithAltScreen()`
- Mouse tracking: `tea.WithMouseCellMotion()`

---

## 5. charmbracelet/glamour - Markdown Rendering in Terminal

### Description
Go library for rendering Markdown with styles in ANSI terminals. "Stylesheet-based markdown rendering for CLI apps."

### Main API

#### Simple Rendering
```go
out, err := glamour.Render(in, "dark")                    // With specific style
out, err := glamour.RenderWithEnvironmentConfig(in)        // Uses GLAMOUR_STYLE env
outBytes, err := glamour.RenderBytes(inBytes, "dark")      // Bytes version
```

#### TermRenderer (customizable)
```go
r, err := glamour.NewTermRenderer(
    glamour.WithAutoStyle(),          // Detects light/dark background
    glamour.WithWordWrap(80),         // Wrapping width
    glamour.WithEmoji(),              // Render emojis
)
out, err := r.Render(markdownString)

// Also implements io.ReadWriteCloser
r.Write(bytes)
r.Read(buf)
r.Close()
```

#### Available Options (TermRendererOption)
```go
glamour.WithAutoStyle()                    // Auto-detect theme
glamour.WithStandardStyle("dark")          // Standard style
glamour.WithStylePath("/path/style.json")  // Style from file
glamour.WithEnvironmentConfig()            // From env GLAMOUR_STYLE
glamour.WithStyles(ansi.StyleConfig{})     // Direct styles
glamour.WithStylesFromJSONFile("f.json")   // From JSON
glamour.WithStylesFromJSONBytes(json)      // From JSON bytes
glamour.WithWordWrap(80)                   // Wrapping width
glamour.WithBaseURL("https://...")         // Base URL for relative links
glamour.WithColorProfile(termenv.TrueColor) // Color profile
glamour.WithEmoji()                        // Emojis
glamour.WithPreservedNewLines()            // Preserve newlines
glamour.WithTableWrap(true)               // Table wrapping
glamour.WithInlineTableLinks(true)        // Inline links in tables
glamour.WithChromaFormatter("terminal256") // Chroma formatter
glamour.WithOptions(opt1, opt2...)        // Group options
```

### Predefined Styles
- `"dark"` - Dark theme (default)
- `"light"` - Light theme
- `"auto"` - Auto-detect
- `"notty"` - For non-terminal output
- `"dracula"` - Dracula scheme

### Chroma Integration
- Glamour uses Chroma internally for syntax highlighting in code blocks
- Configurable with `WithChromaFormatter()`: "terminal256", "terminal16m", etc.
- Automatic for ``` blocks with specified language

### Style Customization
- Via `ansi.StyleConfig` struct
- Via JSON files
- Via environment variable `GLAMOUR_STYLE`

### Bubbletea Integration (typical pattern)
```go
// In Init() or Update():
renderer, _ := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(width))
renderedMarkdown, _ := renderer.Render(rawMarkdown)
m.viewport.SetContent(renderedMarkdown)

// In View():
return m.viewport.View()
```

---

## 6. alecthomas/chroma - Syntax Highlighting in Go

### Description
Pure Go library for syntax highlighting. Converts source code to colored HTML, ANSI terminal text, etc. Inspired by Pygments.

### Architecture: 3 Components
1. **Lexers** - Convert text to token streams (200+ languages)
2. **Formatters** - Transform tokens to formatted output
3. **Styles** - Map token types to colors/styles

### Supported Languages (200+)
Go, Python, Java, JavaScript, TypeScript, Rust, C/C++, C#, Ruby, PHP, Kotlin, SQL, HTML, XML, JSON, YAML, Markdown, Docker, Terraform, Bash, PowerShell, GraphQL, Haskell, and many more.

### Terminal Formatters (relevant for TUI)
- `"terminal16"` - 8/16 ANSI colors
- `"terminal256"` - 256 ANSI colors
- `"terminal16m"` - True color (24-bit RGB)
- Also: HTML, noop, tokens (debugging)

### Quick Usage
```go
import "github.com/alecthomas/chroma/v2/quick"

err := quick.Highlight(os.Stdout, sourceCode, "go", "terminal256", "monokai")
```

### Detailed Programmatic Usage
```go
import (
    "github.com/alecthomas/chroma/v2"
    "github.com/alecthomas/chroma/v2/lexers"
    "github.com/alecthomas/chroma/v2/formatters"
    "github.com/alecthomas/chroma/v2/styles"
)

// 1. Identify language
lexer := lexers.Match("foo.go")          // By filename
lexer := lexers.Get("go")                // By name
lexer := lexers.Analyse(sourceCode)      // By content
if lexer == nil { lexer = lexers.Fallback }

// 2. Optimize (merge identical adjacent tokens)
lexer = chroma.Coalesce(lexer)

// 3. Select style
style := styles.Get("monokai")
if style == nil { style = styles.Fallback }

// 4. Select formatter
formatter := formatters.Get("terminal256")
if formatter == nil { formatter = formatters.Fallback }

// 5. Tokenize
iterator, err := lexer.Tokenise(nil, sourceCode)

// 6. Format to a writer (can be bytes.Buffer)
var buf bytes.Buffer
err = formatter.Format(&buf, style, iterator)
highlightedCode := buf.String()
```

### Popular Styles/Themes
- `"monokai"` - Dark, popular
- `"github"` - Light
- `"dracula"` - Dark
- All Pygments styles converted
- Case-insensitive

### TUI Integration (IDE pattern)
```go
// For file preview with syntax highlighting:
func highlightFile(content, filename string, width int) string {
    lexer := lexers.Match(filename)
    if lexer == nil { lexer = lexers.Fallback }
    lexer = chroma.Coalesce(lexer)
    
    style := styles.Get("dracula")
    formatter := formatters.Get("terminal256")
    
    iterator, _ := lexer.Tokenise(nil, content)
    var buf bytes.Buffer
    formatter.Format(&buf, style, iterator)
    return buf.String()
}

// Then in bubbletea:
highlighted := highlightFile(fileContent, "main.go", m.width)
m.viewport.SetContent(highlighted)
```

### Additional Features
- Automatic language detection by filename, extension or content
- Token hierarchy: if CommentSpecial is not defined, it inherits from Comment
- `chroma.Coalesce()` improves performance by merging adjacent tokens
- less integration: `LESSOPEN` for colorized preview

---

## Integration Summary for IDE-like TUI

### Recommended Stack
```
bubbletea          → Base framework (Model-Update-View)
lipgloss           → Styles and layout (borders, colors, join)
bubbles/viewport   → Scrollable content panels
bubbles/textinput  → Command palette, search bar
bubbles/textarea   → Text editor
bubbles/list       → File explorer, fuzzy search
bubbles/table      → Git status, results
bubbles/help       → Shortcuts bar
bubbles/key        → Keybindings system
bubblezone         → Mouse support with clickable zones
glamour            → Markdown rendering (README, docs)
chroma             → Code syntax highlighting
```

### Main Composition Pattern
```go
type IDEModel struct {
    // Layout
    sidebar     SidebarModel      // File tree (bubbles/list)
    editor      EditorModel       // Main editor (bubbles/textarea or viewport+chroma)
    preview     viewport.Model    // Preview panel (viewport)
    terminal    viewport.Model    // Terminal output
    commandBar  textinput.Model   // Command palette
    statusBar   StatusBarModel    // Status bar
    help        help.Model        // Help bar
    
    // State
    focusedPanel FocusState       // Which panel has focus
    keys         KeyMap           // Keybindings (bubbles/key)
    width, height int             // Terminal dimensions
}

func (m IDEModel) View() string {
    left := m.sidebar.View()
    center := m.editor.View()
    right := m.preview.View()
    
    main := lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
    bottom := m.statusBar.View()
    
    full := lipgloss.JoinVertical(lipgloss.Left, main, bottom)
    return zone.Scan(full)  // Mouse support
}
```

### Lessons from Superfile
1. **Separate handlers**: handle_panel_movement.go, handle_panel_navigation.go
2. **Focus as enum**: sidebarFocus, editorFocus, previewFocus, etc.
3. **Dynamic dimensions**: Recalculate on WindowSizeMsg
4. **Modals as overlay**: Render on top of main content
5. **Cache**: For expensive operations (metadata, highlighting)
