# Phase 2 Implementation: ACP Message Standardization - rawOutput.error + filediff

## Date: 2026-05-29
## Implementation Summary

Successfully implemented Phase 2 of the ACP message standardization plan focusing on medium-priority improvements:

## Changes Made

### 1. Added Helper Function `buildRawOutput`
**File:** `internal/mesnada/acp/tool_render.go`

```go
// buildRawOutput constructs a rawOutput object matching the opencode standard.
// When isError is true, uses "error" key instead of "output" key.
func buildRawOutput(content string, metadata string, isError bool) map[string]interface{} {
    key := "output"
    if isError {
        key = "error"
    }
    out := map[string]interface{}{key: content}
    if metadata != "" {
        var m interface{}
        if json.Unmarshal([]byte(metadata), &m) == nil {
            out["metadata"] = m
        } else {
            out["metadata"] = metadata
        }
    }
    return out
}
```

### 2. Added Helper Function `countLines`
**File:** `internal/mesnada/acp/tool_render.go`

```go
// countLines counts the number of lines in a string.
func countLines(s string) int {
    if s == "" {
        return 0
    }
    return strings.Count(s, "\n") + 1
}
```

### 3. Updated rawOutput Construction in Three Locations

**a) prompt_handler.go (around line 395):**
- Replaced manual rawOutput construction with `buildRawOutput(tr.Content, tr.Metadata, tr.IsError)`
- Added filediff metadata for edit tools

**b) prompt_handler.go (around line 748):**
- Replaced manual rawOutput construction with `buildRawOutput(toolResult.Content, toolResult.Metadata, toolResult.IsError)`

**c) session_state.go (around line 504):**
- Replaced manual rawOutput construction with `buildRawOutput(tr.Content, tr.Metadata, tr.IsError)`
- Added filediff metadata for edit tools after storedInput is retrieved

### 4. Added Filediff Metadata for Edit Tools

For both `edit` and `write` tools, added structured filediff metadata:

**Edit tools:**
```go
if tr.Name == "edit" {
    meta["filediff"] = map[string]interface{}{
        "file":      ep.FilePath,
        "before":    ep.OldString,
        "after":     ep.NewString,
        "additions": countLines(ep.NewString),
        "deletions": countLines(ep.OldString),
    }
}
```

**Write tools:**
```go
if tr.Name == "write" {
    meta["filediff"] = map[string]interface{}{
        "file":      ep.FilePath,
        "additions": countLines(ep.Content),
    }
}
```

## Standards Compliance

This implementation addresses the following gaps from the plan:

- **G3**: `rawOutput.error` vs `rawOutput.output` - Now uses "error" key when `isError == true`
- **G4**: `rawOutput.metadata.filediff` for edit tools - Now includes structured filediff with file, before/after content, and line counts

## Testing

All existing tests pass:
- `go test ./internal/mesnada/acp/... -v` ✅
- Code formatting verified with `gofmt` ✅

## Files Modified

1. `internal/mesnada/acp/tool_render.go` - Added helper functions
2. `internal/mCP/Pando/pando/internal/mesnada/acp/prompt_handler.go` - Updated rawOutput construction and filediff logic
3. `internal/mesnada/acp/session_state.go` - Updated rawOutput construction and filediff logic

## Impact

This implementation ensures Pando's ACP messages now match the opencode standard for:
- Error handling consistency
- Edit tool metadata structure
- Better client compatibility (especially for Zed and other ACP clients)