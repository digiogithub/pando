// Package readmode implements the smart file-read fidelity tiers used by the
// `view` tool (lean-ctx context-intelligence Phase 1) plus the helpers for the
// content-hash unchanged-re-read deduplication (Phase 2).
//
// The modes are strictly additive on top of the tool's existing pagination /
// line-numbering / streaming behavior: `full` is the default and reproduces the
// legacy raw window byte-for-byte, while `signatures` / `map` render a compact,
// line-numbered structural projection of the same window. `auto` deterministically
// picks a tier from the file path and size. Every compressed render is guarded so
// it never emits more bytes than the raw window (a parse failure or a render that
// is not actually smaller falls back to the raw window).
package readmode

import (
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/rag/treesitter"
)

// Mode is a file-read fidelity tier.
type Mode string

const (
	// ModeFull returns the raw paginated window (legacy behavior).
	ModeFull Mode = "full"
	// ModeSignatures renders every symbol's signature, grouped by parent.
	ModeSignatures Mode = "signatures"
	// ModeMap renders a structural outline: imports plus top-level signatures.
	ModeMap Mode = "map"
	// ModeAuto resolves to one of the concrete modes from path + size.
	ModeAuto Mode = "auto"
)

// Auto-resolution size thresholds (in bytes of the read window).
const (
	// smallAutoBytes and below always read in full — too small to bother compressing.
	smallAutoBytes = 1500
	// largeAutoBytes and above prefer signatures over the heavier map outline.
	largeAutoBytes = 12000
)

// Normalize parses a requested mode string. Unknown/empty values resolve to
// ModeFull so callers stay byte-identical to the legacy read.
func Normalize(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto":
		return ModeAuto
	case "signatures", "signature", "sig":
		return ModeSignatures
	case "map", "outline":
		return ModeMap
	default:
		return ModeFull
	}
}

// ResolveInput carries the deterministic signals the auto resolver uses.
type ResolveInput struct {
	// Path is the file path (absolute or relative) being read.
	Path string
	// SizeBytes is the size of the window being read (not necessarily the whole file).
	SizeBytes int
	// DiagnosticsActive indicates the file currently has LSP diagnostics — such
	// files are kept at full fidelity so the agent sees the exact offending lines.
	DiagnosticsActive bool
}

// Resolve turns a requested mode into a concrete mode (never ModeAuto). For
// ModeAuto it mirrors lean-ctx's resolve_inner: instruction files, non-code
// (config/data/text/markdown) files, small windows and diagnostic-active files
// stay full; medium code → map; large code → signatures.
func Resolve(requested Mode, in ResolveInput) Mode {
	if requested != ModeAuto {
		return requested
	}
	if in.DiagnosticsActive {
		return ModeFull
	}
	if isInstructionFile(in.Path) {
		return ModeFull
	}
	lang, ok := treesitter.DetectLanguage(in.Path)
	if !ok || !IsCodeLanguage(lang) {
		// Config/data/text/markdown: keep full in auto. (map mode can still render
		// these structurally when explicitly requested.)
		return ModeFull
	}
	if in.SizeBytes <= smallAutoBytes {
		return ModeFull
	}
	if in.SizeBytes >= largeAutoBytes {
		return ModeSignatures
	}
	return ModeMap
}

// IsCodeLanguage reports whether a tree-sitter language is "code" the auto
// resolver is willing to compress. Markdown / TOML are structured but excluded
// from auto compression (their full text is usually what the agent wants).
func IsCodeLanguage(lang treesitter.Language) bool {
	switch lang {
	case treesitter.LanguageGo,
		treesitter.LanguageTypeScript,
		treesitter.LanguageJavaScript,
		treesitter.LanguagePHP,
		treesitter.LanguageRust,
		treesitter.LanguageJava,
		treesitter.LanguageKotlin,
		treesitter.LanguageSwift,
		treesitter.LanguageC,
		treesitter.LanguageCPP,
		treesitter.LanguagePython,
		treesitter.LanguageLua,
		treesitter.LanguageVue,
		treesitter.LanguageSvelte:
		return true
	default:
		return false
	}
}

// isInstructionFile matches the agent-instruction files that must always be read
// in full (their prose is the payload, not their structure).
func isInstructionFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "agents.md", "claude.md", ".cursorrules", "copilot-instructions.md", "cursor.md":
		return true
	}
	if strings.HasSuffix(base, ".cursorrules") {
		return true
	}
	return false
}
