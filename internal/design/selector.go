package design

import (
	"fmt"
	"strconv"
	"strings"
)

// The design patch engine resolves the very selectors the renderer produced for
// its node index (see selectorFor in render_script.go): compound selectors made
// of a tag, an id, classes, attribute tests and :nth-of-type(), joined by the
// child (">") or descendant (whitespace) combinator. That grammar is
// intentionally small — anything richer is a sign the agent should be editing
// the source with the regular edit tool instead of patching a rendered node.

type selectorPart struct {
	tag       string // "" or "*" matches any tag
	id        string
	classes   []string
	attrs     []selectorAttr
	nthOfType int  // 0 when unset
	child     bool // true when joined to the previous part with ">"
}

type selectorAttr struct {
	name  string
	value string
	set   bool // true when the selector tested a value
}

type selector []selectorPart

// parseSelector parses the supported selector subset.
func parseSelector(s string) (selector, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("design: empty selector")
	}
	tokens, err := splitSelector(s)
	if err != nil {
		return nil, err
	}
	var sel selector
	child := false
	for _, tok := range tokens {
		if tok == ">" {
			if len(sel) == 0 {
				return nil, fmt.Errorf("design: selector %q starts with a combinator", s)
			}
			child = true
			continue
		}
		part, err := parseSelectorPart(tok)
		if err != nil {
			return nil, err
		}
		part.child = child
		child = false
		sel = append(sel, part)
	}
	if len(sel) == 0 {
		return nil, fmt.Errorf("design: selector %q has no components", s)
	}
	return sel, nil
}

// splitSelector splits on combinators without breaking bracketed or
// parenthesised sections.
func splitSelector(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	depthBracket, depthParen := 0, 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '[':
			depthBracket++
			cur.WriteByte(c)
		case c == ']':
			depthBracket--
			cur.WriteByte(c)
		case c == '(':
			depthParen++
			cur.WriteByte(c)
		case c == ')':
			depthParen--
			cur.WriteByte(c)
		case depthBracket > 0 || depthParen > 0:
			cur.WriteByte(c)
		case c == ' ' || c == '\t' || c == '\n':
			flush()
		case c == '>':
			flush()
			out = append(out, ">")
		case c == ',':
			return nil, fmt.Errorf("design: selector lists (\",\") are not supported")
		default:
			cur.WriteByte(c)
		}
	}
	if depthBracket != 0 || depthParen != 0 {
		return nil, fmt.Errorf("design: unbalanced selector %q", s)
	}
	flush()
	return out, nil
}

func parseSelectorPart(tok string) (selectorPart, error) {
	var p selectorPart
	i := 0
	// Leading type selector.
	start := i
	for i < len(tok) && !strings.ContainsRune("#.[:", rune(tok[i])) {
		i++
	}
	p.tag = strings.ToLower(tok[start:i])
	if p.tag == "*" {
		p.tag = ""
	}

	for i < len(tok) {
		switch tok[i] {
		case '#':
			i++
			start := i
			for i < len(tok) && !strings.ContainsRune("#.[:", rune(tok[i])) {
				i++
			}
			p.id = tok[start:i]
		case '.':
			i++
			start := i
			for i < len(tok) && !strings.ContainsRune("#.[:", rune(tok[i])) {
				i++
			}
			p.classes = append(p.classes, tok[start:i])
		case '[':
			end := strings.IndexByte(tok[i:], ']')
			if end < 0 {
				return p, fmt.Errorf("design: unterminated attribute selector in %q", tok)
			}
			body := tok[i+1 : i+end]
			i += end + 1
			a := selectorAttr{}
			if eq := strings.IndexByte(body, '='); eq >= 0 {
				a.name = strings.ToLower(strings.TrimSpace(body[:eq]))
				a.value = strings.Trim(strings.TrimSpace(body[eq+1:]), `"'`)
				a.set = true
			} else {
				a.name = strings.ToLower(strings.TrimSpace(body))
			}
			p.attrs = append(p.attrs, a)
		case ':':
			rest := tok[i:]
			const prefix = ":nth-of-type("
			if !strings.HasPrefix(rest, prefix) {
				return p, fmt.Errorf("design: unsupported pseudo-class in %q (only :nth-of-type(n) is supported)", tok)
			}
			end := strings.IndexByte(rest, ')')
			if end < 0 {
				return p, fmt.Errorf("design: unterminated :nth-of-type() in %q", tok)
			}
			n, err := strconv.Atoi(strings.TrimSpace(rest[len(prefix):end]))
			if err != nil || n < 1 {
				return p, fmt.Errorf("design: :nth-of-type() needs a positive integer in %q", tok)
			}
			p.nthOfType = n
			i += end + 1
		default:
			return p, fmt.Errorf("design: cannot parse selector component %q", tok)
		}
	}
	if p.tag == "" && p.id == "" && len(p.classes) == 0 && len(p.attrs) == 0 && p.nthOfType == 0 {
		return p, fmt.Errorf("design: selector component %q selects nothing", tok)
	}
	return p, nil
}

// matches reports whether a single element satisfies one compound part.
func (p selectorPart) matches(e *htmlElem) bool {
	if p.tag != "" && p.tag != e.tag {
		return false
	}
	if p.id != "" && p.id != e.id() {
		return false
	}
	for _, c := range p.classes {
		if !e.hasClass(c) {
			return false
		}
	}
	for _, a := range p.attrs {
		val, ok := e.attr(a.name)
		if !ok {
			return false
		}
		if a.set && a.value != htmlUnescape(val.val) {
			return false
		}
	}
	if p.nthOfType > 0 && p.nthOfType != e.typeIndex {
		return false
	}
	return true
}

// Match returns every element satisfying the selector, in document order.
func (d *htmlDoc) Match(sel selector) []*htmlElem {
	var out []*htmlElem
	for _, e := range d.all {
		if matchChain(e, sel) {
			out = append(out, e)
		}
	}
	return out
}

// matchChain evaluates the selector right-to-left against an element and its
// ancestors. The leading part is not anchored to the document root, because the
// renderer caps generated selectors at six components.
func matchChain(e *htmlElem, sel selector) bool {
	last := len(sel) - 1
	if !sel[last].matches(e) {
		return false
	}
	current := e
	for i := last; i > 0; i-- {
		part := sel[i]
		prev := sel[i-1]
		if part.child {
			if current.parent == nil || !prev.matches(current.parent) {
				return false
			}
			current = current.parent
			continue
		}
		// Descendant: walk up until an ancestor satisfies the previous part.
		found := false
		for anc := current.parent; anc != nil; anc = anc.parent {
			if prev.matches(anc) {
				current = anc
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
