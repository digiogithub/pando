# Comparative Analysis: Internal File Editing, Patching & Search Tools

> **Date**: 2026-03-15
> **Scope**: crush (Go), opencode (TypeScript), claude-code (Node.js/bundled), pando (Go)
> **Purpose**: Identify best patterns to guide improvements in Pando

---

## 1. Overview of Projects

| Project | Language | Type | Key Characteristics |
|---------|----------|------|---------------------|
| **crush** | Go | TUI AI assistant | Charmbracelet-based, forked from openproject, MIT license |
| **opencode** | TypeScript | Web/Desktop AI IDE | Bun/Effect runtime, LSP, snapshot system |
| **claude-code** | Node.js (bundled) | Terminal AI CLI | Anthropic proprietary, closed-source, distributed as minified JS |
| **pando** | Go | TUI AI assistant | Fork of crush, same base architecture |

> **Note on claude-code**: The source code is closed-source and distributed as a bundled/minified `cli.js`.
> The analysis here is based on reverse-engineering the installed binary at
> `/home/sevir/.local/lib/node_modules/@anthropic-ai/claude-code/cli.js` (v2.1.71).

---

## 2. Tool Inventory

### 2.1 File Editing Tools

| Tool | crush | opencode | claude-code | pando |
|------|-------|----------|-------------|-------|
| Edit (string replace) | ✅ `edit.go` | ✅ `tool/edit.ts` | ✅ (bundled) | ✅ `edit.go` |
| Write (full overwrite) | ✅ `write.go` | ✅ `tool/write.ts` | ✅ (bundled) | ✅ `write.go` |
| Multi-edit | ✅ `multiedit.go` | ✅ `tool/multiedit.ts` | ✅ (bundled) | ✅ (inherited) |
| Apply patch | ❌ | ✅ `tool/apply_patch.ts` | ❌ | ✅ `patch.go` |

### 2.2 Search Tools

| Tool | crush | opencode | claude-code | pando |
|------|-------|----------|-------------|-------|
| Grep (content search) | ✅ ripgrep + regex fallback | ✅ ripgrep wrapper | ✅ ripgrep bundled | ✅ ripgrep + regex fallback |
| Glob (file pattern) | ✅ ripgrep + doublestar | ✅ ripgrep + fuzzysort | ✅ native Node.js glob | ✅ ripgrep + doublestar |
| Code semantic search | ❌ | ✅ `codesearch.ts` (Exa MCP) | ❌ | ❌ |
| Fuzzy file search | ❌ | ✅ `fuzzysort` library | ❌ | ❌ |

### 2.3 Read/View Tools

| Tool | crush | opencode | claude-code | pando |
|------|-------|----------|-------------|-------|
| Read with line range | ✅ | ✅ | ✅ | ✅ |
| Binary detection | MIME (512B) | byte sampling (4096B) | ✅ (bundled) | MIME (512B) |
| Image/PDF support | ❌ | ✅ base64 | ✅ (images + PDF + Jupyter) | ❌ |
| Directory listing | ❌ | ✅ with typo suggestions | ✅ | ✅ |
| Jupyter notebooks | ❌ | ❌ | ✅ | ❌ |

---

## 3. Deep Comparison: Edit Tool Strategies

### 3.1 String Matching Approaches

This is the most critical difference between projects:

#### **crush / pando** — Simple exact matching
```go
index := strings.Index(oldContent, oldString)
lastIndex := strings.LastIndex(oldContent, oldString)
if index != lastIndex {
    return multipleMatchesError
}
newContent = oldContent[:index] + newString + oldContent[index+len(oldString):]
```
**Pros**: Simple, predictable, fast
**Cons**: Fails on any whitespace difference, very brittle with indentation

#### **claude-code** — Exact matching + smart quote normalization
```javascript
// Function P56(A, q): fuzzy quote matching
if (A.includes(q)) return q;  // exact match first

// If not found, normalize smart quotes and retry:
// " " → "  "  |  ' ' → ' '  |  « » → " "
// Function tv7() handles unicode quote variants
```
**Pros**: Handles the common case where AI generates "smart quotes" instead of ASCII
**Cons**: Only covers one class of mismatch (quotes); no whitespace/indentation tolerance

**Additional claude-code feature**: conflict detection in multi-edit — detects when `old_string` is a substring of a previous `new_string` within the same call, preventing cascading errors.

#### **opencode** — Cascading fuzzy matcher (9 stages)
```typescript
// Tries each replacer in order, stops on first success
const replacers = [
  SimpleReplacer,               // 1. Exact match
  LineTrimmedReplacer,          // 2. Per-line trim
  BlockAnchorReplacer,          // 3. Levenshtein on first/last lines
  WhitespaceNormalizedReplacer, // 4. Collapse whitespace
  IndentationFlexibleReplacer,  // 5. Remove common indentation
  EscapeNormalizedReplacer,     // 6. Unescape sequences
  TrimmedBoundaryReplacer,      // 7. Full-string trim
  ContextAwareReplacer,         // 8. ≥50% middle-line similarity
  MultiOccurrenceReplacer,      // 9. All occurrences (replaceAll)
]
```
**Pros**: Extremely resilient to whitespace/indentation variations, greatly reduces LLM retry loops
**Cons**: More complex code, could mask actual errors, non-deterministic in edge cases

**Levenshtein similarity used by opencode:**
```typescript
similarity = 1 - (levenshtein_distance / max_line_length)
// Single candidate: threshold = 0.0 (accept any)
// Multiple candidates: threshold = 0.3 (30% required)
```

### 3.2 CRLF / Line Ending Handling

| Project | Strategy |
|---------|----------|
| crush | Detect CRLF → convert to LF → edit → restore |
| opencode | Detect via `\r\n` presence → normalize → restore |
| claude-code | Handled internally in bundled code |
| pando | Same as crush (inherited) |

### 3.3 File Change Validation

All projects implement some form of "stale file" protection:

```go
// crush/pando: timestamp comparison
modTime := fileInfo.ModTime().Truncate(time.Second)
if modTime.After(lastRead) {
    return error("file modified externally since last read")
}
```

```typescript
// opencode: actual file locking (strongest)
FileTime.withLock(filePath, async () => {
    // Atomic operation inside lock
})
```

```javascript
// claude-code: timestamp comparison (similar to crush/pando)
// Also detects when file hasn't been read before editing
```

**opencode is strongest** with actual file locking vs timestamp comparison.

---

## 4. Deep Comparison: Patch Tool

### 4.1 Patch Format

**opencode / pando** share a compatible custom patch format:
```
*** Begin Patch
*** Add File: /path
+content
*** Update File: /path
*** Move to: /new/path
@@ context @@
 keep
-remove
+add
*** Delete File: /path
*** End Patch
```

**claude-code** does not expose a standalone patch tool — edits are done exclusively via the Edit tool or multi-edit.

**crush** has no patch tool.

### 4.2 Context Matching in Patches

**pando** — 3-tier whitespace fallback (fuzz level tracking):
```go
// Pass 1: exact (fuzz=0)
// Pass 2: trimRight (fuzz=1)
// Pass 3: trimSpace (fuzz=100)
// Rejects patches with fuzz > 3
```

**opencode** — 4-tier with Unicode normalization:
```typescript
// Pass 1: exact
// Pass 2: rstrip
// Pass 3: trim
// Pass 4: normalizeUnicode (smart quotes, dashes, ellipsis)
```

**Winner: opencode** — Unicode normalization prevents false failures when AI generates smart quotes.

### 4.3 Atomic Application

Both opencode and pando apply patches atomically (all files or none), with per-file permission requests.

---

## 5. Deep Comparison: Grep/Search

### 5.1 Ripgrep Integration

| Feature | crush | opencode | claude-code | pando |
|---------|-------|----------|-------------|-------|
| Ripgrep auto-download | ❌ | ✅ (v14.1.1) | ✅ (bundled in vendor/) | ❌ |
| Ignore files honored | `.gitignore` + `.crushignore` | `.gitignore` | `.gitignore` | `.gitignore` |
| Result limit | 100 | 100 | configurable | 100 |
| Sort by mtime | ✅ newest first | ✅ newest first | ❌ | ✅ newest first |
| JSON output parsing | ❌ (text) | ✅ typed Zod schema | ❌ (text) | ❌ (text) |
| Timeout handling | ❌ | ❌ | ✅ partial results | ❌ |
| Multithreaded retry | ❌ | ❌ | ✅ (`-j 1` on EAGAIN) | ❌ |
| Context lines (-C/-A/-B) | ❌ | ❌ | ✅ | ❌ |
| output_mode (files/count/content) | ❌ | ❌ | ✅ | ❌ |
| `--type` filtering | ❌ | ❌ | ✅ | ❌ |
| Multiline mode | ❌ | ❌ | ✅ | ❌ |
| Pagination (head_limit/offset) | ❌ | ❌ | ✅ | ❌ |

**claude-code has the richest Grep API** — it exposes most of ripgrep's power directly.

### 5.2 Regex Caching

**crush** implements thread-safe regex caching with session reset:
```go
var searchRegexCache = newRegexCache()  // csync.Map based
func ResetCache() { searchRegexCache.Reset(map[string]*regexp.Regexp{}) }
```

**claude-code** and **opencode** rely on their runtime's built-in regex caching (V8 JIT).
**pando** does not implement explicit regex caching.

### 5.3 Fuzzy File Search

Only **opencode** supports fuzzy file name search via `fuzzysort` library.

---

## 6. Deep Comparison: Read/View

### 6.1 Format Support

| Format | crush | opencode | claude-code | pando |
|--------|-------|----------|-------------|-------|
| Plain text + line range | ✅ | ✅ | ✅ | ✅ |
| Images (base64) | ❌ | ✅ | ✅ | ❌ |
| PDFs | ❌ | ✅ | ✅ (up to 20 pages) | ❌ |
| Jupyter notebooks | ❌ | ❌ | ✅ | ❌ |
| Binary detection | MIME 512B | byte sampling 4096B | ✅ | MIME 512B |

**claude-code** has the broadest format support (images, PDFs, Jupyter).

### 6.2 Context Injection (per-directory instructions)

Only **opencode** injects `.claude.md` instruction content into file read responses, enabling per-directory AI instructions.

### 6.3 Git Integration in Read

**opencode** integrates git diff directly in the Read tool output. Unique to opencode.

---

## 7. Unique Features by Project

### crush — Unique Strengths

1. **`.crushignore` support** — custom ignore file alongside `.gitignore`
2. **Bash security layer** — banned commands list (`curl`, `wget`, `sudo`, `apt`, etc.) with argument-level blocking
3. **Regex caching with session reset** — prevents memory leaks between sessions
4. **Partial success in MultiEdit** — failed edits tracked, successful ones applied, enabling targeted retry

### opencode — Unique Strengths

1. **9-stage cascading fuzzy edit matching** with Levenshtein distance
2. **Git-based snapshot system** — every edit tracked for full undo:
   ```typescript
   snapshot.track()       // capture state as git tree
   snapshot.diff(hash)    // unified diff from checkpoint
   snapshot.patch(hash)   // list changed files
   ```
3. **Ripgrep auto-download** — no system dependency required
4. **Unicode normalization in patch matching** — prevents smart quote / dash issues
5. **Code semantic search via Exa** — hybrid keyword + semantic search
6. **File locking** (`FileTime.withLock`) — prevents concurrent modification race conditions
7. **`.claude.md` instruction injection** — per-directory AI behavior customization
8. **Git diff in Read output** — see changes inline while reading
9. **Image/PDF read support** — base64 encoded responses

### claude-code — Unique Strengths

1. **Smart quote normalization in Edit** — specific fuzzy match for unicode quote variants (`"`, `"`, `'`, `'`)
2. **Richest Grep API** — exposes context lines (-C/-A/-B), output modes, `--type` filtering, multiline mode, pagination
3. **Ripgrep timeout handling** — returns partial results instead of failing, with auto-retry in single-threaded mode on EAGAIN
4. **Multi-edit conflict detection** — detects when `old_string` is substring of a previous `new_string` in the same call
5. **Jupyter notebook read support** — reads `.ipynb` files with cell/output rendering
6. **PDF read with page range** — reads specific pages from PDF files (max 20 pages per call)
7. **Bundled ripgrep** — ships in `vendor/ripgrep/<arch>-<platform>/rg`, no external dependency

### pando — Unique Strengths (vs crush base)

1. **ACP (Anthropic Client Protocol) integration** — remote file operations via client callbacks
2. **Full patch tool** — compatible with opencode patch format
3. **Intra-line diff highlighting** — character-level diffs using `diffmatchpatch`
4. **Syntax highlighting in diffs** — via Chroma lexer

---

## 8. Comparative Scoring

| Dimension | crush | opencode | claude-code | pando |
|-----------|-------|----------|-------------|-------|
| Edit resilience (fuzzy matching) | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐ |
| Patch capability | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐ | ⭐⭐⭐⭐ |
| Search power / API richness | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ |
| Concurrency / file locking safety | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ | ⭐⭐ |
| Undo / snapshot | ❌ | ⭐⭐⭐⭐⭐ | ❌ | ❌ |
| Portability (no system deps) | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ |
| Security (bash restrictions) | ⭐⭐⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐ |
| LSP integration | ⭐⭐⭐ | ⭐⭐⭐⭐ | ❌ | ⭐⭐⭐ |
| Format support (images/PDF) | ❌ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ❌ |
| Code simplicity | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | N/A (closed) | ⭐⭐⭐⭐ |

---

## 9. Priority Improvements for Pando

Based on the analysis, these are the highest-value improvements sorted by impact/effort ratio:

### 🔴 High Impact / Medium Effort

#### 9.1 Cascading Fuzzy Edit Matching
Port opencode's 9-stage cascading replacer to Go. This is the single most impactful improvement — dramatically reduces edit failures due to whitespace/indentation drift. Also incorporate claude-code's smart quote normalization as one of the stages.

**Minimum viable version** (stages 1-5 + quote normalization):
```go
type Replacer func(content, oldStr, newStr string) (string, bool)

var replacers = []Replacer{
    ExactReplacer,
    QuoteNormalizedReplacer,   // claude-code pattern: normalize " " ' ' etc.
    LineTrimmedReplacer,       // per-line strings.TrimSpace
    IndentationFlexReplacer,   // remove common indent prefix
    WhitespaceNormReplacer,    // regexp \s+ → " "
}
```

#### 9.2 Git-Based Snapshot System
Implement per-session git snapshots for undo capability (opencode approach):
```go
func Snapshot(projectPath string) (hash string, err error)
func Restore(projectPath string, hash string) error
func DiffFrom(projectPath string, hash string) (string, error)
```

#### 9.3 Unicode Normalization in Patch Matching
Add unicode normalization as 4th pass in pando's `seekSequence` (from opencode + claude-code):
```go
func normalizeUnicode(s string) string {
    replacer := strings.NewReplacer(
        "\u2018", "'", "\u2019", "'",   // smart single quotes
        "\u201C", "\"", "\u201D", "\"", // smart double quotes
        "\u2014", "-", "\u2013", "-",   // em/en dash
        "\u2026", "...",                // ellipsis
        "\u00A0", " ",                  // non-breaking space
    )
    return replacer.Replace(s)
}
```

### 🟡 High Impact / High Effort

#### 9.4 Richer Grep API
Adopt claude-code's grep parameter model — expose context lines, output modes, `--type` filtering, multiline, and pagination. This gives the LLM much more precise control over searches:
```go
type GrepParams struct {
    Pattern    string
    Path       string
    Include    string
    OutputMode string  // "content" | "files_with_matches" | "count"
    Context    int     // -C lines
    Before     int     // -B lines
    After      int     // -A lines
    Type       string  // --type (js, go, py...)
    Multiline  bool
    HeadLimit  int
    Offset     int
    LiteralText bool
}
```

#### 9.5 Ripgrep Timeout with Partial Results
From claude-code: handle ripgrep timeout gracefully — return partial results with a truncation flag instead of an error:
```go
type GrepResult struct {
    Matches   []GrepMatch
    Truncated bool
    Timeout   bool
}
```

#### 9.6 File Locking (Concurrent Write Safety)
Replace timestamp-comparison with actual file locking (opencode approach):
```go
var fileLocks sync.Map  // map[string]*sync.Mutex

func withFileLock(path string, fn func() error) error {
    mu, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
    mu.(*sync.Mutex).Lock()
    defer mu.(*sync.Mutex).Unlock()
    return fn()
}
```

### 🟢 Medium Impact / Low Effort

#### 9.7 Regex Cache with Session Reset
Port crush's `regexCache` (already available in crush codebase — same Go):
```go
var searchRegexCache = newRegexCache()
func ResetCache() { searchRegexCache.Reset(...) }
```

#### 9.8 `.pandoignore` Support
Add custom ignore file for grep/glob (crush's `.crushignore` pattern). Users can exclude generated files, caches, etc.

#### 9.9 Multi-edit Conflict Detection
Port claude-code's check that detects when `old_string` is a substring of a previous `new_string` in the same multi-edit call — prevents cascading replacement errors.

#### 9.10 Fuzzy File Name Search
Add fuzzy matching for file paths (Go: `github.com/lithammer/fuzzysearch`):
```go
matches := fuzzy.RankFind("srv/handler", allPaths)
```

### 🔵 Low Impact / Low Effort

#### 9.11 Binary File Detection Improvement
Upgrade from 512-byte MIME sampling to 4096-byte with null-byte detection (opencode approach):
```go
const sampleSize = 4096
// Null byte → binary
// >30% non-printable → binary
```

#### 9.12 Grep Result Metadata
Return structured metadata (already partially done in pando):
```go
type GrepMetadata struct {
    NumberOfMatches int  `json:"number_of_matches"`
    Truncated       bool `json:"truncated"`
}
```

---

## 10. Architecture Recommendations

### Edit Tool Redesign (Chain of Responsibility)
```go
type EditStrategy interface {
    Apply(content, oldString, newString string) (result string, ok bool)
    Name() string
}

type EditTool struct {
    strategies []EditStrategy
}

func (e *EditTool) Apply(content, oldString, newString string) (string, error) {
    for _, s := range e.strategies {
        if result, ok := s.Apply(content, oldString, newString); ok {
            log.Debug("edit matched via strategy", "strategy", s.Name())
            return result, nil
        }
    }
    return "", ErrOldStringNotFound
}
```

This allows adding/removing/reordering strategies without modifying core logic and enables easy testing of individual strategies.

### Snapshot Integration
The snapshot system should be a first-class feature at the session level, not per-tool. A session opens → initial snapshot taken. Any edit → tracked. Session ends → cleanup.

### Grep as First-Class Tool
Upgrade Grep to be as expressive as claude-code's implementation. The LLM uses grep extensively — giving it context lines, type filters, and output modes dramatically reduces the number of roundtrips needed.

---

## 11. References

- crush source: `/www/MCP/Pando/crush/internal/`
- opencode source: `/www/MCP/Pando/opencode/packages/opencode/src/`
- claude-code bundled: `/home/sevir/.local/lib/node_modules/@anthropic-ai/claude-code/cli.js` (v2.1.71)
- claude-code public repo: `/www/MCP/Pando/claude-code/` (plugins/hooks only, no source)
- pando source: `/www/MCP/Pando/pando/internal/llm/tools/`
- opencode edit tool: `packages/opencode/src/tool/edit.ts`
- opencode patch parser: `packages/opencode/src/patch/index.ts`
- pando diff system: `internal/diff/patch.go`, `internal/diff/diff.go`
- crush grep with cache: `internal/llm/tools/grep.go`
