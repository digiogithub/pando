package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/tools/readmode"
	"github.com/digiogithub/pando/internal/savings"
)

// recordSaving converts the baseline/actual character sizes of a token-reduction
// event into estimated tokens and records it in the Phase-5 savings ledger. It is
// a no-op when nothing was saved (actual ≥ baseline) so the ledger only reflects
// real wins.
func recordSaving(source savings.Source, detail, mode string, baselineChars, actualChars int) {
	if actualChars >= baselineChars {
		return
	}
	savings.Record(savings.Entry{
		Source:         source,
		Detail:         detail,
		Mode:           mode,
		BaselineTokens: savings.TokensFromChars(baselineChars),
		ActualTokens:   savings.TokensFromChars(actualChars),
	})
}

// bashSavingDetail trims a shell command to a short, single-line ledger detail.
func bashSavingDetail(command string) string {
	d := strings.TrimSpace(command)
	if i := strings.IndexByte(d, '\n'); i >= 0 {
		d = d[:i]
	}
	const max = 80
	if len(d) > max {
		d = d[:max] + "…"
	}
	return d
}

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

// resolveViewMode resolves the requested/default `view` mode to the concrete tier
// this read will use, applying the Phase-3 bounce escalation when the request is
// `auto`. requested is the explicit tool param ("" → configured default). The
// returned mode is never ModeAuto; ModeFull means "use the raw window".
func resolveViewMode(ctx context.Context, filePath string, sizeBytes int, requested string, diagnosticsActive bool) readmode.Mode {
	reqMode := readmode.Normalize(requested)
	if strings.TrimSpace(requested) == "" {
		reqMode = readmode.Normalize(config.Get().ResolveReadModeDefault())
	}

	concrete := readmode.Resolve(reqMode, readmode.ResolveInput{
		Path:              filePath,
		SizeBytes:         sizeBytes,
		DiagnosticsActive: diagnosticsActive,
	})

	// Phase 3: only auto-resolved compressed reads are escalated by bounce history;
	// an explicit signatures/map request is honored verbatim.
	if reqMode == readmode.ModeAuto && concrete != readmode.ModeFull {
		if bt := bounceTracker(ctx); bt != nil {
			concrete = bt.Upgrade(filePath, concrete, config.Get().ReadModeLearningEnabled())
		}
	}
	return concrete
}

// dedupViewRead applies Phase 2 unchanged-re-read deduplication for the `view`
// tool at the fidelity that is about to be delivered (nowMode). When the
// (path, window) was already delivered this session at a fidelity that already
// covers nowMode it returns a compact response (an "unchanged" stub or a
// "changed" diff) with handled=true; for a brand-new window, or when the caller
// now wants higher fidelity than was previously delivered, it returns
// handled=false so the caller performs the real read. It is a no-op
// (handled=false) when dedup is disabled or no session cache is attached.
func dedupViewRead(ctx context.Context, filePath, content string, startLine, endLine int, nowMode readmode.Mode) (ToolResponse, bool) {
	if !config.Get().ReadDedupEnabled() {
		return ToolResponse{}, false
	}
	cache := GetSessionCache(ctx)
	if cache == nil {
		return ToolResponse{}, false
	}

	res := cache.RecordRead(filePath, startLine, endLine, content, string(nowMode))
	switch res.Status {
	case ReadDedupUnchanged:
		msg := fmt.Sprintf(
			"[unchanged: %s lines %d-%d — content identical to earlier read %s this session; use mode=full or a different offset/limit to force a fresh read]",
			displayPath(filePath), startLine+1, endLine, res.Label,
		)
		recordSaving(savings.SourceDedup, displayPath(filePath), "unchanged", len(content), len(msg))
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
		recordSaving(savings.SourceDedup, displayPath(filePath), "diff", len(content), sb.Len())
		return WithResponseMetadata(
			NewTextResponse(sb.String()),
			ViewResponseMetadata{FilePath: filePath, Content: content},
		), true
	default:
		// ReadDedupNew / ReadDedupUpgraded: proceed with the real read.
		return ToolResponse{}, false
	}
}

// renderViewMode renders a window at an already-resolved concrete mode
// (signatures/map). It returns the rendered body, or ok=false to fall back to the
// raw full window. The render is rejected (ok=false) when the mode is full,
// parsing fails, the listing is empty, or it is not actually smaller than the raw
// window (safeguard: a compressed read never costs more than the raw read).
func renderViewMode(ctx context.Context, filePath, content string, lineOffset int, concrete readmode.Mode) (string, bool) {
	if concrete == readmode.ModeFull || concrete == readmode.ModeAuto {
		return "", false
	}

	symbols, lang, parsed := readmode.ParseSymbols(ctx, filePath, []byte(content))
	if !parsed {
		return "", false
	}

	var rendered string
	switch concrete {
	case readmode.ModeSignatures:
		rendered = readmode.RenderSignatures(symbols, lang, lineOffset)
	case readmode.ModeMap:
		rendered = readmode.RenderMap(symbols, lang, content, lineOffset)
	}

	if strings.TrimSpace(rendered) == "" {
		return "", false
	}
	// Safeguard mirroring lean-ctx's safeguard_ratio: if the compressed render is
	// not smaller than the raw window, prefer the raw window.
	if len(rendered) >= len(content) {
		return "", false
	}
	return rendered, true
}

// bounceTracker returns the session's Phase 3 bounce tracker, or nil when no
// session cache is attached to the context.
func bounceTracker(ctx context.Context) *readmode.BounceTracker {
	if cache := GetSessionCache(ctx); cache != nil {
		return cache.BounceTracker()
	}
	return nil
}

// recordCompressedRead arms a possible Phase-3 bounce for a compressed delivery.
func recordCompressedRead(ctx context.Context, filePath string, mode readmode.Mode, renderedBytes int) {
	if bt := bounceTracker(ctx); bt != nil {
		bt.RecordCompressed(filePath, mode, renderedBytes)
	}
}

// recordFullRead reports a full delivery to the bounce tracker; if a compressed
// read of the same path was pending this counts as a bounce that will escalate
// future auto reads of the path/extension.
func recordFullRead(ctx context.Context, filePath string) {
	if bt := bounceTracker(ctx); bt != nil {
		bt.RecordFull(filePath)
	}
}
