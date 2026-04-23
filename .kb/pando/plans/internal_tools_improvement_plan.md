# Pando Internal Tools Improvement Plan (v2)

> **Date**: 2026-03-15 (updated: ripgrep binary eliminated)
> **Based on**: `.kb/research/internal_tools.md` + analysis of ripgrep (Rust) source + pando go.mod
> **Decision**: Replace ripgrep binary with native Go implementation using existing go.mod dependencies
> **Fact keys**: Each phase stored as remembrance fact under user_id=`pando`

---

## Key Decision: No External Binary Dependency

**ripgrep is a Rust project** — its code cannot be used in Go directly.
Instead, we implement an equivalent `internal/search/` package in pure Go.

**All required Go capabilities already exist in go.mod:**
| Capability | Go package | Status |
|-----------|-----------|--------|
| Glob / gitignore patterns | `github.com/bmatcuk/doublestar/v4` | ✅ already used |
| Binary file detection | `github.com/gabriel-vasile/mimetype` | ✅ already in go.mod |
| Parallel workers | `golang.org/x/sync` (errgroup) | ✅ already in go.mod |
| Structured concurrency | `github.com/sourcegraph/conc` | ✅ already indirect dep |
| Regex (RE2) | `regexp` stdlib | ✅ stdlib |
| Fuzzy matching | `github.com/sahilm/fuzzy` | ✅ already used |

---

## Summary Table

| Phase | Focus | Effort | Impact | Fact Key |
|-------|-------|--------|--------|----------|
| **Phase 0** | Native Go search engine (replace rg binary) | Medium | Critical | `improvement_plan_phase0` |
| **Phase 1** | Zero-risk quick wins (unicode, binary detection, multi-edit) | Low | Medium-High | `improvement_plan_phase1` |
| **Phase 2** | Edit tool cascading fuzzy matching | Medium | Very High | `improvement_plan_phase2` |
| **Phase 3** | Grep API enrichment (native engine API) | Low* | High | `improvement_plan_phase3` |
| **Phase 4** | File locking + .pandoignore + regex cache | Low-Medium | Medium | `improvement_plan_phase4` |

*Low effort because Phase 0 built the engine; Phase 3 just exposes its knobs.

---

## Phase 0 — Native Go Search Engine
**Fact**: `improvement_plan_phase0`

Replace all `exec.Command("rg", ...)` calls with a pure-Go engine.

### New package: `internal/search/`

#### `ignore.go` — Gitignore parser (mirrors ripgrep's `ignore/gitignore.rs`)
- Parse `.gitignore` + `.pandoignore` line by line
- Rules: `#` = comment, `!` prefix = negate, `/` suffix = dir-only, `/` in middle = anchored
- Convert to `doublestar.Match`-compatible patterns  
- Walk up directory tree collecting matchers (like git does)
- `LoadIgnoreFiles(rootPath string) → *IgnoreMatcher`

#### `walker.go` — Concurrent file walker (mirrors ripgrep's `ignore/walk.rs Worker`)
- Producer/consumer: `fs.WalkDir` → channel → N goroutines
- Each worker applies: IgnoreMatcher + TypeFilter + IncludeGlob
- Binary check in worker: first 4096 bytes, null-byte = skip (mimetype available)
- Collects results → sorts by mtime

#### `searcher.go` — Content search (mirrors ripgrep's `searcher` crate)
- `bufio.Scanner` with 64KB buffer
- Binary skip: check first 4096 bytes before reading lines
- Before-context: circular buffer of last N lines
- After-context: countdown after match
- Output modes: `files_with_matches` | `content` | `count`

#### `types.go` — File type mapping (port of ripgrep's `ignore/default_types.rs`)
- Static `map[string][]string` for ~35 most common dev types
- `go→[*.go]`, `js→[*.js,*.mjs]`, `ts→[*.ts,*.tsx]`, `py→[*.py]`, `rust→[*.rs]`, etc.

### Files modified:
- `internal/fileutil/fileutil.go` — remove `rgPath`/`GetRgCmd`, use `search.WalkFiles`
- `internal/llm/tools/grep.go` — replace `searchWithRipgrep` with `search.SearchFiles`
- `internal/llm/tools/glob.go` — use native walker with ignore support

---

## Phase 1 — Zero-Risk Quick Wins
**Fact**: `improvement_plan_phase1`

Self-contained changes, each a separate commit, no architecture changes.

### 1.1 Unicode Normalization in Patch Matching
- `internal/diff/patch.go` → `seekSequence` function
- Add 4th pass: normalize `"→"`, `'→'`, `—→-`, `–→-`, `…→...`, `\u00A0→ ` before line compare

### 1.2 Unicode/Smart Quote Normalization in Edit Tool
- `internal/llm/tools/edit.go` → `replaceContent` / `deleteContent`
- Retry with normalized quotes before returning "old_string not found"

### 1.3 Multi-Edit Conflict Detection
- Check if any `old_string` is substring of a previous `new_string` in same call
- Return error before applying to prevent cascading replacements

### 1.4 Binary File Detection Improvement
- Increase sample 512 → 4096 bytes, add null-byte check, >30% non-printable = binary
- (Phase 0 already adds `mimetype`-based detection to the search engine; replicate in view.go)

---

## Phase 2 — Edit Tool Resilience: Cascading Fuzzy Matching
**Fact**: `improvement_plan_phase2`

New file: `internal/llm/tools/edit_strategies.go`

```go
type EditStrategy interface {
    Apply(content, oldStr, newStr string) (result string, matched bool)
    Name() string
}
```

### Chain (5 strategies, in order):
1. `ExactReplacer` — `strings.Index` (current behavior)
2. `QuoteNormalizedReplacer` — smart quotes/dashes normalization
3. `LineTrimmedReplacer` — `strings.TrimRight` per line
4. `IndentationFlexReplacer` — remove common indent prefix, find de-indented match
5. `WhitespaceNormReplacer` — collapse `\s+` → single space

**Constraint**: normalization only for matching; replacement on original content at found offset.
**Log**: `logging.Debug("edit matched via strategy", "strategy", s.Name())`

---

## Phase 3 — Grep API Enrichment
**Fact**: `improvement_plan_phase3`

Expose the native engine's capabilities to the LLM. Low effort after Phase 0.

### New GrepParams fields:
```go
OutputMode  string  // "content" | "files_with_matches" (default) | "count"
Context     int     // -C (before + after)
Before      int     // -B
After       int     // -A
Type        string  // "go", "js", "py"... → expands via types.go DefaultTypes map
Multiline   bool    // match across line boundaries
HeadLimit   int     // cap results
Offset      int     // skip first N (pagination)
```

**Note**: No ripgrep timeout/EAGAIN (those were binary quirks). Native engine uses `context.Context` cancellation.

---

## Phase 4 — Infrastructure Safety
**Fact**: `improvement_plan_phase4`

### 4.1 File Locking
- `sync.Map` of `*sync.Mutex` per file path
- Wrap `replaceContent`, `deleteContent`, `createNewFile` in `withFileLock`
- Keep timestamp check INSIDE the lock

### 4.2 .pandoignore Support
- Phase 0 already implements `.pandoignore` in the search engine's `LoadIgnoreFiles`
- Phase 4 ensures it's also applied in glob.go and any remaining direct walk code

### 4.3 Regex Cache with Session Reset
```go
var regexCache sync.Map  // pattern → *regexp.Regexp
func getOrCompileRegex(pattern string) (*regexp.Regexp, error)
func ResetRegexCache()  // called on session end
```

---

## Implementation Order

```
Phase 0  (new internal/search/ package — enables everything else)
  ↓
Phase 1  (independent quick wins, parallel with Phase 0 if resources allow)
  ↓
Phase 2  (edit resilience — high LLM UX impact)
  ↓
Phase 3  (expose Phase 0 engine knobs in grep tool — low effort)
  ↓
Phase 4  (infrastructure: locking, cache — can interleave with Phase 2/3)
```

---

## Out of Scope

- **Git-based snapshot system** — significant session-level architecture, deferred
- **Fuzzy file name search** — lower ROI, deferred (fuzzy lib already in go.mod if needed later)
- **Ripgrep auto-download** — eliminated by design (Phase 0 removes this need entirely)

---

## References

- ripgrep ignore crate: `/www/MCP/Pando/ripgrep/crates/ignore/src/`
  - `gitignore.rs` — gitignore pattern parsing algorithm
  - `walk.rs` — Worker struct: work-stealing parallel traversal
  - `types.rs` + `default_types.rs` — type→extension mapping
- ripgrep searcher crate: `/www/MCP/Pando/ripgrep/crates/searcher/src/`
  - `line_buffer.rs` — 64KB buffer, null-byte binary detection, BinaryDetection enum
- pando existing code:
  - `internal/fileutil/fileutil.go` — current rg usage + doublestar glob
  - `internal/llm/tools/grep.go` — current ripgrep + fallback regex search
  - `internal/llm/tools/glob.go` — current glob implementation
