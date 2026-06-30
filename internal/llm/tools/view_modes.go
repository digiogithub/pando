package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools/readmode"
)

// displayPath renders a workspace-relative path when the file lives under the
// working directory, falling back to the absolute path. Used for compact dedup /
// compressed-read messages.
func displayPath(path string) string {
	wd := config.WorkingDirectory()
	if wd != "" {
		if rel, err := filepath.Rel(wd, path); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}

// dedupViewRead applies Phase 2 unchanged-re-read deduplication for the `view`
// tool. When the (path, window) was already delivered this session it returns a
// compact response (an "unchanged" stub or a "changed" diff) with handled=true;
// for a brand-new window it returns handled=false so the caller performs the
// normal read. It is a no-op (handled=false) when dedup is disabled or no session
// cache is attached to the context.
func dedupViewRead(ctx context.Context, filePath, content string, startLine, endLine int) (ToolResponse, bool) {
	if !config.Get().ReadDedupEnabled() {
		return ToolResponse{}, false
	}
	cache := GetSessionCache(ctx)
	if cache == nil {
		return ToolResponse{}, false
	}

	res := cache.RecordRead(filePath, startLine, endLine, content)
	switch res.Status {
	case ReadDedupUnchanged:
		msg := fmt.Sprintf(
			"[unchanged: %s lines %d-%d — content identical to earlier read %s this session; use mode=full or a different offset/limit to force a fresh read]",
			displayPath(filePath), startLine+1, endLine, res.Label,
		)
		return WithResponseMetadata(
			NewTextResponse(msg),
			ViewResponseMetadata{FilePath: filePath, Content: content},
		), true
	case ReadDedupChanged:
		diff := readmode.DiffWindows(res.PrevContent, content, startLine)
		var sb strings.Builder
		fmt.Fprintf(&sb, "<file path=%s lines=%d-%d changed-since=%s>\n",
			displayPath(filePath), startLine+1, endLine, res.Label)
		sb.WriteString(diff)
		sb.WriteString("\n</file>\n")
		return WithResponseMetadata(
			NewTextResponse(sb.String()),
			ViewResponseMetadata{FilePath: filePath, Content: content},
		), true
	default:
		return ToolResponse{}, false
	}
}

// renderViewMode applies Phase 1 compressed read modes to a window. requested is
// the explicit `mode` tool param ("" → use the configured default). It returns
// the rendered body and the concrete mode applied, or ok=false to fall back to
// the raw full window. The render is rejected (ok=false) when parsing fails, the
// listing is empty, or it is not actually smaller than the raw window (safeguard:
// a compressed read never costs more than the raw read).
func renderViewMode(ctx context.Context, filePath, content string, lineOffset, sizeBytes int, requested string, diagnosticsActive bool) (string, readmode.Mode, bool) {
	reqMode := readmode.Normalize(requested)
	if strings.TrimSpace(requested) == "" {
		reqMode = readmode.Normalize(config.Get().ResolveReadModeDefault())
	}

	concrete := readmode.Resolve(reqMode, readmode.ResolveInput{
		Path:              filePath,
		SizeBytes:         sizeBytes,
		DiagnosticsActive: diagnosticsActive,
	})
	if concrete == readmode.ModeFull {
		return "", readmode.ModeFull, false
	}

	symbols, lang, parsed := readmode.ParseSymbols(ctx, filePath, []byte(content))
	if !parsed {
		return "", readmode.ModeFull, false
	}

	var rendered string
	switch concrete {
	case readmode.ModeSignatures:
		rendered = readmode.RenderSignatures(symbols, lang, lineOffset)
	case readmode.ModeMap:
		rendered = readmode.RenderMap(symbols, lang, content, lineOffset)
	}

	if strings.TrimSpace(rendered) == "" {
		return "", readmode.ModeFull, false
	}
	// Safeguard mirroring lean-ctx's safeguard_ratio: if the compressed render is
	// not smaller than the raw window, prefer the raw window.
	if len(rendered) >= len(content) {
		return "", readmode.ModeFull, false
	}
	return rendered, concrete, true
}
