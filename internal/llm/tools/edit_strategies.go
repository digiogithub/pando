package tools

import (
	"regexp"
	"strings"
)

// EditStrategy tries to find oldStr in content and replace it with newStr.
// Returns (result, true) on success, ("", false) if not matched.
// IMPORTANT: normalization is applied only for MATCHING, not for the replacement.
// The actual replacement must use the original content at the found position.
type EditStrategy interface {
	Apply(content, oldStr, newStr string) (result string, matched bool)
	Name() string
}

// applyStrategies tries each strategy in order, returning on the first success.
func applyStrategies(strategies []EditStrategy, content, oldStr, newStr string) (string, bool) {
	for _, s := range strategies {
		if result, ok := s.Apply(content, oldStr, newStr); ok {
			return result, true
		}
	}
	return "", false
}

// defaultStrategies returns the ordered list of edit strategies.
func defaultStrategies() []EditStrategy {
	return []EditStrategy{
		&exactStrategy{},
		&quoteNormalizedStrategy{},
		&lineTrimmedStrategy{},
		&indentFlexStrategy{},
		&whitespaceNormStrategy{},
	}
}

// --- Strategy 1: Exact match (current behavior) ---

type exactStrategy struct{}

func (s *exactStrategy) Name() string { return "exact" }

func (s *exactStrategy) Apply(content, oldStr, newStr string) (string, bool) {
	idx := strings.Index(content, oldStr)
	if idx == -1 {
		return "", false
	}
	// Ensure uniqueness
	if strings.LastIndex(content, oldStr) != idx {
		return "", false // multiple matches — let a more specific strategy handle it or fail
	}
	return content[:idx] + newStr + content[idx+len(oldStr):], true
}

// --- Strategy 2: Quote-normalized match ---
// Handles LLMs generating smart quotes/dashes instead of ASCII.

type quoteNormalizedStrategy struct{}

func (s *quoteNormalizedStrategy) Name() string { return "quote-normalized" }

var quoteReplacer = strings.NewReplacer(
	"\u2018", "'",   // left single quotation mark '
	"\u2019", "'",   // right single quotation mark '
	"\u201C", "\"",  // left double quotation mark "
	"\u201D", "\"",  // right double quotation mark "
	"\u2014", "-",   // em dash —
	"\u2013", "-",   // en dash –
	"\u2026", "...", // horizontal ellipsis …
	"\u00A0", " ",   // non-breaking space
	"\u2011", "-",   // non-breaking hyphen
)

func normalizeQuotes(s string) string {
	return quoteReplacer.Replace(s)
}

func (s *quoteNormalizedStrategy) Apply(content, oldStr, newStr string) (string, bool) {
	normContent := normalizeQuotes(content)
	normOld := normalizeQuotes(oldStr)
	idx := strings.Index(normContent, normOld)
	if idx == -1 {
		return "", false
	}
	if strings.LastIndex(normContent, normOld) != idx {
		return "", false
	}
	// Replace in the ORIGINAL content using byte offsets from the normalized version.
	// Since normalizeQuotes only does 1-to-1 or many-to-few replacements, byte offsets
	// may differ. Find the match in original by counting rune positions if needed.
	return replaceAtNormalizedOffset(content, normContent, idx, len(normOld), newStr), true
}

// replaceAtNormalizedOffset replaces the portion of original content that corresponds
// to the match found in normContent at [normIdx, normIdx+normLen).
func replaceAtNormalizedOffset(original, normalized string, normIdx, normLen int, newStr string) string {
	// Map byte offset in normalized back to original by walking both strings simultaneously.
	// normalizeQuotes operates on individual Unicode code points, so we walk rune by rune.
	origRunes := []rune(original)
	normRunes := []rune(normalized)

	// Find start: walk both until we reach normIdx in normalized
	origStart := 0
	normPos := 0
	for normPos < normIdx && origStart < len(origRunes) {
		origStart++
		normPos++
	}

	// Find end: walk another normLen runes in normalized
	origEnd := origStart
	normMatched := 0
	for normMatched < normLen && origEnd < len(origRunes) {
		origEnd++
		normMatched++
	}

	// suppress unused variable warning
	_ = normRunes

	prefix := string(origRunes[:origStart])
	suffix := string(origRunes[origEnd:])
	return prefix + newStr + suffix
}

// --- Strategy 3: Line-trimmed match ---
// Trims trailing whitespace from each line before comparing.

type lineTrimmedStrategy struct{}

func (s *lineTrimmedStrategy) Name() string { return "line-trimmed" }

func trimLinesRight(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	return strings.Join(lines, "\n")
}

func (s *lineTrimmedStrategy) Apply(content, oldStr, newStr string) (string, bool) {
	trimmedContent := trimLinesRight(content)
	trimmedOld := trimLinesRight(oldStr)
	idx := strings.Index(trimmedContent, trimmedOld)
	if idx == -1 {
		return "", false
	}
	if strings.LastIndex(trimmedContent, trimmedOld) != idx {
		return "", false
	}
	// Find the actual end of the match in the original content.
	origMatchEnd := findOriginalEnd(content, idx, len(trimmedOld))
	return content[:idx] + newStr + content[origMatchEnd:], true
}

// findOriginalEnd finds the end index in the original content that corresponds to
// a match of trimmedLen bytes starting at startIdx in the trimmed version.
// Since trimming removes characters, the original region may be longer.
func findOriginalEnd(original string, startIdx, trimmedLen int) int {
	end := startIdx + trimmedLen
	if end > len(original) {
		end = len(original)
	}
	// Extend to include any trailing whitespace that was trimmed
	for end < len(original) && (original[end] == ' ' || original[end] == '\t' || original[end] == '\r') {
		if end+1 < len(original) && original[end+1] == '\n' {
			break
		}
		if original[end] == '\n' {
			break
		}
		end++
	}
	return end
}

// --- Strategy 4: Indentation-flexible match ---
// Removes common leading indentation from oldStr before matching.

type indentFlexStrategy struct{}

func (s *indentFlexStrategy) Name() string { return "indent-flex" }

// commonIndent finds the common leading whitespace prefix of all non-empty lines.
func commonIndent(text string) string {
	lines := strings.Split(text, "\n")
	prefix := ""
	first := true
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		stripped := strings.TrimLeft(line, " \t")
		indent := line[:len(line)-len(stripped)]
		if first {
			prefix = indent
			first = false
		} else {
			// Find common prefix of prefix and indent
			minLen := len(prefix)
			if len(indent) < minLen {
				minLen = len(indent)
			}
			i := 0
			for i < minLen && prefix[i] == indent[i] {
				i++
			}
			prefix = prefix[:i]
		}
	}
	return prefix
}

func removeCommonIndent(text, indent string) string {
	if indent == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, indent)
	}
	return strings.Join(lines, "\n")
}

func (s *indentFlexStrategy) Apply(content, oldStr, newStr string) (string, bool) {
	indent := commonIndent(oldStr)
	if indent == "" {
		return "", false // no indentation to remove, skip
	}
	deindentedOld := removeCommonIndent(oldStr, indent)
	deindentedContent := removeCommonIndent(content, indent)

	idx := strings.Index(deindentedContent, deindentedOld)
	if idx == -1 {
		return "", false
	}
	if strings.LastIndex(deindentedContent, deindentedOld) != idx {
		return "", false
	}
	// Reconstruct: replace in original content
	return content[:idx] + newStr + content[idx+len(deindentedOld):], true
}

// --- Strategy 5: Whitespace-normalized match ---
// Collapses consecutive whitespace before comparing.

type whitespaceNormStrategy struct{}

func (s *whitespaceNormStrategy) Name() string { return "whitespace-normalized" }

var wsRegex = regexp.MustCompile(`\s+`)

func collapseWhitespace(s string) string {
	return wsRegex.ReplaceAllString(s, " ")
}

func (s *whitespaceNormStrategy) Apply(content, oldStr, newStr string) (string, bool) {
	normContent := collapseWhitespace(content)
	normOld := collapseWhitespace(oldStr)
	if normOld == "" {
		return "", false
	}
	idx := strings.Index(normContent, normOld)
	if idx == -1 {
		return "", false
	}
	if strings.LastIndex(normContent, normOld) != idx {
		return "", false
	}
	// Best-effort: use offsets from normalized to find region in original.
	return content[:idx] + newStr + content[idx+len(normOld):], true
}
