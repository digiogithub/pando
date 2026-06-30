package readmode

import (
	"fmt"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/rag/treesitter"
)

// maxDocSummaryLen bounds the inline docstring summary appended to a signature.
const maxDocSummaryLen = 80

// RenderSignatures renders every symbol's signature, grouped/indented by parent,
// with the real source line number (window StartLine + lineOffset) so the agent
// can jump to a full body via offset/limit. Returns "" when there is nothing to
// render (caller falls back to the raw window).
func RenderSignatures(symbols []*treesitter.CodeSymbol, lang treesitter.Language, lineOffset int) string {
	rendered := renderSymbolList(symbols, lineOffset, false)
	if rendered == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %d symbols (signatures only — use mode=full or offset=<line> for bodies)\n\n", lang, countSymbols(symbols))
	b.WriteString(rendered)
	return b.String()
}

// RenderMap renders a structural outline: an imports line plus top-level
// signatures only (no nested members). Returns "" when there is nothing to render.
func RenderMap(symbols []*treesitter.CodeSymbol, lang treesitter.Language, content string, lineOffset int) string {
	imports := extractImports(content, lang)
	list := renderSymbolList(symbols, lineOffset, true)
	if list == "" && len(imports) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s · structural map (use mode=full or offset=<line> for bodies)\n\n", lang)
	if len(imports) > 0 {
		fmt.Fprintf(&b, "imports: %s\n", strings.Join(imports, ", "))
		if list != "" {
			b.WriteByte('\n')
		}
	}
	b.WriteString(list)
	return b.String()
}

// renderSymbolList renders the symbols ordered by source line. When topLevelOnly
// is true only symbols without a parent are emitted (the map outline); otherwise
// nested symbols are indented under their parents (the signatures view).
func renderSymbolList(symbols []*treesitter.CodeSymbol, lineOffset int, topLevelOnly bool) string {
	if len(symbols) == 0 {
		return ""
	}

	byID := make(map[string]*treesitter.CodeSymbol, len(symbols))
	for _, s := range symbols {
		if s != nil {
			byID[s.ID] = s
		}
	}

	ordered := make([]*treesitter.CodeSymbol, 0, len(symbols))
	for _, s := range symbols {
		if s == nil {
			continue
		}
		if topLevelOnly && s.ParentID != nil {
			continue
		}
		ordered = append(ordered, s)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].StartLine < ordered[j].StartLine
	})

	var b strings.Builder
	for _, s := range ordered {
		depth := 0
		if !topLevelOnly {
			depth = symbolDepth(s, byID)
		}
		b.WriteString(renderEntry(s, lineOffset, depth))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderEntry renders one symbol line: an indented "L<line>  <signature>" with an
// optional trailing docstring summary.
func renderEntry(s *treesitter.CodeSymbol, lineOffset, depth int) string {
	indent := strings.Repeat("  ", depth+1)
	line := s.StartLine + lineOffset
	sig := displaySignature(s)
	entry := fmt.Sprintf("%sL%-5d %s", indent, line, sig)
	if doc := docSummary(s.DocString); doc != "" {
		entry += "  — " + doc
	}
	return entry
}

// displaySignature returns a single-line signature for the symbol, falling back
// to "<type> <name>" when no signature was extracted (e.g. structs, fields).
func displaySignature(s *treesitter.CodeSymbol) string {
	sig := collapseWS(s.Signature)
	if sig != "" {
		return sig
	}
	name := s.Name
	if name == "" {
		name = lastPathSegment(s.NamePath)
	}
	if name == "" {
		return string(s.SymbolType)
	}
	return fmt.Sprintf("%s %s", s.SymbolType, name)
}

// docSummary returns the first non-empty line of a docstring, bounded in length.
func docSummary(doc string) string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return ""
	}
	first := doc
	if idx := strings.IndexByte(doc, '\n'); idx >= 0 {
		first = doc[:idx]
	}
	first = strings.TrimSpace(strings.TrimLeft(first, "/#*- \t"))
	if len(first) > maxDocSummaryLen {
		first = first[:maxDocSummaryLen] + "…"
	}
	return first
}

// symbolDepth counts how many ancestors a symbol has via its ParentID chain,
// guarding against cycles.
func symbolDepth(s *treesitter.CodeSymbol, byID map[string]*treesitter.CodeSymbol) int {
	depth := 0
	seen := make(map[string]struct{})
	cur := s
	for cur != nil && cur.ParentID != nil {
		if _, dup := seen[cur.ID]; dup {
			break
		}
		seen[cur.ID] = struct{}{}
		parent, ok := byID[*cur.ParentID]
		if !ok {
			break
		}
		depth++
		if depth > 16 {
			break
		}
		cur = parent
	}
	return depth
}

func countSymbols(symbols []*treesitter.CodeSymbol) int {
	n := 0
	for _, s := range symbols {
		if s != nil {
			n++
		}
	}
	return n
}

// collapseWS replaces any run of whitespace (including newlines) with a single
// space and trims the result — multiline signatures become one tidy line.
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func lastPathSegment(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	if idx := strings.LastIndexByte(p, '/'); idx >= 0 {
		return p[idx+1:]
	}
	return p
}
