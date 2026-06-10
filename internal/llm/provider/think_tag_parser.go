package provider

import "strings"

const (
	thinkOpenTag  = "<think>"
	thinkCloseTag = "</think>"
)

// thinkTagParser extracts <think>...</think> content from a streaming text,
// routing thinking tokens and regular content into separate buckets.
// It handles tags that are split across multiple streaming chunks.
//
// Used for Ollama and similar OpenAI-compat providers that embed reasoning
// in content rather than a dedicated field (e.g. reasoning_content).
type thinkTagParser struct {
	inThinking bool
	pending    string // suffix from previous chunk that may be the start of a tag
}

// feed processes a new content chunk and returns separated thinking and
// regular content. Partial tags at chunk boundaries are held in an internal
// buffer and resolved on the next call.
func (p *thinkTagParser) feed(chunk string) (thinking, content string) {
	text := p.pending + chunk
	p.pending = ""

	for len(text) > 0 {
		if p.inThinking {
			idx := strings.Index(text, thinkCloseTag)
			if idx >= 0 {
				thinking += text[:idx]
				text = text[idx+len(thinkCloseTag):]
				p.inThinking = false
				continue
			}
			// Check for partial closing tag at the end of text.
			suffix := longestTagPrefix(text, thinkCloseTag)
			thinking += text[:len(text)-len(suffix)]
			p.pending = suffix
			return
		}

		idx := strings.Index(text, thinkOpenTag)
		if idx >= 0 {
			content += text[:idx]
			text = text[idx+len(thinkOpenTag):]
			p.inThinking = true
			continue
		}
		// Check for partial opening tag at the end of text.
		suffix := longestTagPrefix(text, thinkOpenTag)
		content += text[:len(text)-len(suffix)]
		p.pending = suffix
		return
	}
	return
}

// flush returns any content still held in the internal buffer after the
// stream ends. A partial tag that never completed is treated as literal text.
func (p *thinkTagParser) flush() (thinking, content string) {
	if p.pending == "" {
		return
	}
	if p.inThinking {
		thinking = p.pending
	} else {
		content = p.pending
	}
	p.pending = ""
	return
}

// longestTagPrefix returns the longest suffix of s that equals a prefix of tag.
// Used to detect partial tags at chunk boundaries.
func longestTagPrefix(s, tag string) string {
	maxLen := len(tag) - 1
	if maxLen > len(s) {
		maxLen = len(s)
	}
	for i := maxLen; i > 0; i-- {
		if strings.HasSuffix(s, tag[:i]) {
			return s[len(s)-i:]
		}
	}
	return ""
}

// extractThinkTags splits a complete (non-streaming) string into its thinking
// and regular content parts.
func extractThinkTags(text string) (thinking, content string) {
	p := &thinkTagParser{}
	thinking, content = p.feed(text)
	t2, c2 := p.flush()
	return thinking + t2, content + c2
}

// filterImageTags strips multimodal image placeholder tokens that some local
// models (e.g. Ollama/LLaVA) emit as literal text in their output.  These
// tokens carry no textual meaning and would otherwise appear verbatim in the
// TUI chat window.
func filterImageTags(s string) string {
	s = strings.ReplaceAll(s, "<image/>", "")
	s = strings.ReplaceAll(s, "<image />", "")
	s = strings.ReplaceAll(s, "</image>", "")
	s = strings.ReplaceAll(s, "<image>", "")
	return s
}
