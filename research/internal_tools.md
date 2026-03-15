# Comparative Analysis: Internal File Editing, Patching & Search Tools

> **Date**: 2026-03-15
> **Scope**: crush (Go), opencode (TypeScript), pando/claude-code (Go)
> **Purpose**: Identify best patterns to guide improvements in Pando

---

## 1. Overview of Projects

| Project | Language | Type | Key Characteristics |
|---------|----------|------|---------------------|
| **crush** | Go | TUI AI assistant | Charmbracelet-based, forked from openproject, MIT license |
| **opencode** | TypeScript | Web/Desktop AI IDE | Bun/Effect runtime, LSP, snapshot system |
| **pando** | Go | TUI AI assistant | Fork of crush, same base architecture |

---

## 2. Tool Inventory

### 2.1 File Editing Tools

| Tool | crush | opencode | pando |
|------|-------|----------|-------|
| Edit (string replace) | ✅ `edit.go` | ✅ `tool/edit.ts` | ✅ `edit.go` |
| Write (full overwrite) | ✅ `write.go` | ✅ `tool/write.ts` | ✅ `write.go` |
| Multi-edit | ✅ `multiedit.go` | ✅ `tool/multiedit.ts` | ✅ (inherited) |
| Apply patch | ❌ | ✅ `tool/apply_patch.ts` | ✅ `patch.go` |

### 2.2 Search Tools

| Tool | crush | opencode | pando |
|------|-------|----------|-------|
| Grep (content search) | ✅ ripgrep + regex fallback | ✅ ripgrep wrapper | ✅ ripgrep + regex fallback |
| Glob (file pattern) | ✅ ripgrep + doublestar | ✅ ripgrep + fuzzysort | ✅ ripgrep + doublestar |
| Code semantic search | ❌ | ✅ `codesearch.ts` (Exa MCP) | ❌ |
| Fuzzy file search | ❌ | ✅ `fuzzysort` library | ❌ |

### 2.3 Read/View Tools

| Tool | crush | opencode | pando |
|------|-------|----------|-------|
| Read with line range | ✅ | ✅ | ✅ |
| Binary detection | Basic (extension only) | ✅ byte sampling (4096B) | ✅ |
| Image/PDF support | ❌ | ✅ base64 | ❌ |
| Directory listing | ❌ | ✅ with typo suggestions | ✅ |

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

#### **opencode** — Cascading fuzzy matcher (9 stages)
```typescript
// Tries each replacer in order, stops on first success
const replacers = [
  SimpleReplacer,           // 1. Exact match
  LineTrimmedReplacer,      // 2. Per-line trim
  BlockAnchorReplacer,      // 3. Levenshtein on first/last lines
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
| pando | Same as crush (inherited) |

All three handle this correctly, but the detection implementation differs slightly.

### 3.3 File Change Validation

All three projects implement "must read before edit" to prevent stale writes:

```go
// crush/pando pattern
modTime := fileInfo.ModTime().Truncate(time.Second)
if modTime.After(lastRead) {
    return error("file modified externally since last read")
}
```

```typescript
// opencode uses FileTime locking (stronger)
FileTime.withLock(filePath, async () => {
    // Atomic operation inside lock
})
```

**opencode is stronger** here with actual file locking, not just timestamp comparison.

---

## 4. Deep Comparison: Patch Tool

### 4.1 Patch Format

**opencode** has a custom patch format with heredoc support:
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

**pando** implements the same format (compatible with opencode):
```go
// diff/patch.go implements identical parsing
func TextToPatch(text string, orig map[string]string) (Patch, int, error)
```

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

All three use ripgrep as primary backend. Key differences:

| Feature | crush | opencode | pando |
|---------|-------|----------|-------|
| Ripgrep auto-download | ❌ | ✅ (v14.1.1, all platforms) | ❌ |
| Ignore files honored | `.gitignore` + `.crushignore` | `.gitignore` (implicit) | `.gitignore` |
| Result limit | 100 | 100 | 100 |
| Sort by mtime | ✅ newest first | ✅ newest first | ✅ newest first |
| JSON output parsing | ❌ (text) | ✅ typed Zod schema | ❌ (text) |

**Key advantage of opencode**: Ripgrep auto-download makes it portable on any platform without requiring system-level ripgrep install.

### 5.2 Regex Caching

**crush** implements thread-safe regex caching with session reset:
```go
var searchRegexCache = newRegexCache()  // csync.Map based

func ResetCache() {
    searchRegexCache.Reset(map[string]*regexp.Regexp{})
}
```

**opencode** relies on V8's built-in regex JIT caching.
**pando** does not implement explicit regex caching.

### 5.3 Fuzzy File Search

Only **opencode** supports fuzzy file name search via `fuzzysort` library, which is very useful for "find me a file similar to X" queries without exact names.

---

## 6. Deep Comparison: Read/View

### 6.1 Binary Detection

**opencode** has the most sophisticated approach:
```typescript
// 1. Fast path: extension check
// 2. Byte sampling: read first 4096 bytes
// 3. Null byte = binary
// 4. >30% non-printable chars = binary
```

**crush/pando** use simpler MIME detection from first 512 bytes via `http.DetectContentType`.

### 6.2 Context Injection

Only **opencode** injects `.claude.md` instruction content into file read responses, enabling per-directory AI instructions — a powerful feature.

### 6.3 Git Integration

**opencode** integrates git diff directly in the Read tool:
```typescript
// Shows git diff alongside file content
const gitDiff = await run("git diff HEAD -- " + file)
```

This is unique to opencode and very valuable for review workflows.

---

## 7. Unique Features by Project

### crush — Unique Strengths

1. **`.crushignore` support** — custom ignore file alongside `.gitignore`
2. **Bash security layer** — banned commands list (`curl`, `wget`, `sudo`, `apt`, etc.) with argument-level blocking
3. **Regex caching with session reset** — prevents memory leaks between sessions
4. **Partial success in MultiEdit** — failed edits tracked, successful ones applied, enabling targeted retry

### opencode — Unique Strengths

1. **9-stage cascading fuzzy edit matching** with Levenshtein distance — dramatically reduces edit failures
2. **Git-based snapshot system** — every edit is tracked in isolated git repo for full undo:
   ```typescript
   snapshot.track()  // writes current state as git tree
   snapshot.diff(hash)  // unified diff from any checkpoint
   snapshot.patch(hash) // list of changed files
   ```
3. **Ripgrep auto-download** — no system dependency required
4. **Unicode normalization in patch matching** — prevents smart quote / dash issues
5. **Code semantic search via Exa** — hybrid keyword + semantic search
6. **File locking** (`FileTime.withLock`) — prevents concurrent modification race conditions
7. **`.claude.md` instruction injection** — per-directory AI behavior customization
8. **Git diff in Read output** — see changes inline while reading
9. **Image/PDF read support** — base64 encoded responses

### pando — Unique Strengths (vs crush base)

1. **ACP (Anthropic Client Protocol) integration** — remote file operations via client callbacks
2. **Full patch tool** — inherited from opencode-compatible format
3. **Intra-line diff highlighting** — character-level diffs using `diffmatchpatch`
4. **Syntax highlighting in diffs** — via Chroma lexer

---

## 8. Comparative Scoring

| Dimension | crush | opencode | pando |
|-----------|-------|----------|-------|
| Edit resilience (fuzzy matching) | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ |
| Patch capability | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ |
| Search power | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |
| Concurrency safety | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ |
| Undo / snapshot | ❌ | ⭐⭐⭐⭐⭐ | ❌ |
| Portability | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ |
| Security (bash) | ⭐⭐⭐⭐⭐ | ⭐⭐ | ⭐⭐⭐ |
| LSP integration | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |
| Code simplicity | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |

---

## 9. Priority Improvements for Pando

Based on the analysis, these are the highest-value improvements sorted by impact/effort ratio:

### 🔴 High Impact / Medium Effort

#### 9.1 Cascading Fuzzy Edit Matching
Port opencode's 9-stage cascading replacer to Go. This is the single most impactful improvement — it will dramatically reduce edit failures due to whitespace/indentation drift.

**Minimum viable version** (stages 1-4):
```go
type Replacer func(content, oldStr, newStr string) (string, bool)

var replacers = []Replacer{
    ExactReplacer,
    LineTrimmedReplacer,       // per-line strings.TrimSpace
    IndentationFlexReplacer,   // remove common indent prefix
    WhitespaceNormReplacer,    // regexp \s+ → " "
}
```

#### 9.2 Git-Based Snapshot System
Implement per-session git snapshots for undo capability. Use an isolated bare git repo per project:
```go
// Create snapshot before any edit
func Snapshot(projectPath string) (hash string, err error)
// Restore from snapshot
func Restore(projectPath string, hash string) error
// Diff from snapshot
func DiffFrom(projectPath string, hash string) (string, error)
```

#### 9.3 Unicode Normalization in Patch Matching
Add unicode normalization as 4th pass in pando's `seekSequence`:
```go
func normalizeUnicode(s string) string {
    replacer := strings.NewReplacer(
        "\u2018", "'", "\u2019", "'",   // smart quotes
        "\u201C", "\"", "\u201D", "\"", // double smart quotes
        "\u2014", "-", "\u2013", "-",   // em/en dash
        "\u2026", "...",                // ellipsis
        "\u00A0", " ",                  // non-breaking space
    )
    return replacer.Replace(s)
}
```

### 🟡 High Impact / High Effort

#### 9.4 File Locking (Concurrent Write Safety)
Replace timestamp-comparison with actual file locking using `sync.Mutex` per file path:
```go
var fileLocks sync.Map  // map[string]*sync.Mutex

func withFileLock(path string, fn func() error) error {
    mu, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
    mu.(*sync.Mutex).Lock()
    defer mu.(*sync.Mutex).Unlock()
    return fn()
}
```

#### 9.5 Ripgrep Auto-Download
Make ripgrep a bundled dependency using platform-specific downloads (same approach as opencode's `ripgrep.ts`). Eliminates the system dependency.

### 🟢 Medium Impact / Low Effort

#### 9.6 Regex Cache with Session Reset
Port crush's `regexCache` (already available in crush codebase — same Go):
```go
// Copy directly from crush/internal/llm/tools/grep.go
var searchRegexCache = newRegexCache()
func ResetCache() { searchRegexCache.Reset(...) }
```

#### 9.7 `.pandoignore` Support
Add custom ignore file for grep/glob, similar to crush's `.crushignore`. Users can exclude generated files, caches, etc. from AI tool results.

#### 9.8 Fuzzy File Name Search
Add `fuzzysort`-equivalent fuzzy matching for file paths (Go: use `github.com/lithammer/fuzzysearch`):
```go
// Match "srv/handler" against all paths
matches := fuzzy.RankFind("srv/handler", allPaths)
```

### 🔵 Low Impact / Low Effort

#### 9.9 Result Metadata in Grep
Return structured metadata like crush/opencode (already partially done in pando):
```go
type GrepMetadata struct {
    NumberOfMatches int  `json:"number_of_matches"`
    Truncated       bool `json:"truncated"`
}
```

#### 9.10 Binary File Detection Improvement
Upgrade from 512-byte MIME sampling to 4096-byte with null-byte detection (opencode approach):
```go
const sampleSize = 4096
// Check for null bytes → binary
// Check for >30% non-printable → binary
```

---

## 10. Architecture Recommendations

### Edit Tool Redesign
The single most impactful change is restructuring the Edit tool to use a strategy/chain-of-responsibility pattern:

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
The snapshot system should be a first-class feature integrated at the session level, not per-tool. A session opens → initial snapshot taken. Any edit → tracked. Session ends → cleanup.

---

## 11. References

- crush source: `/www/MCP/Pando/crush/internal/`
- opencode source: `/www/MCP/Pando/opencode/packages/opencode/src/`
- pando source: `/www/MCP/Pando/pando/internal/llm/tools/`
- opencode edit tool: `packages/opencode/src/tool/edit.ts`
- opencode patch parser: `packages/opencode/src/patch/index.ts`
- pando diff system: `internal/diff/patch.go`, `internal/diff/diff.go`
- crush grep with cache: `internal/llm/tools/grep.go`
