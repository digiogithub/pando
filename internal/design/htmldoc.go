package design

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// htmlDoc is a byte-offset-preserving view of an HTML source file.
//
// The design patch engine deliberately does NOT reserialise documents with
// html.Render: artifact files live in the user's repository and are hand-edited
// and committed, so a patch must leave every byte it did not target untouched.
// Instead the tokenizer walks the source once, recording the exact ranges of
// every element, its start tag and each attribute, and a patch becomes a set of
// splices into the original bytes.
type htmlDoc struct {
	src   []byte
	roots []*htmlElem
	all   []*htmlElem
}

// htmlElem is one element with the source ranges needed to splice it.
type htmlElem struct {
	tag      string
	attrs    []htmlAttr
	parent   *htmlElem
	children []*htmlElem

	// outerStart..outerEnd spans "<tag ...>...</tag>".
	outerStart, outerEnd int
	// startTagStart..startTagEnd spans "<tag ...>".
	startTagStart, startTagEnd int
	// innerStart..innerEnd spans the children of a non-void element; both are
	// -1 for void and self-closing elements.
	innerStart, innerEnd int

	void      bool
	typeIndex int // 1-based position among same-tag element siblings
	depth     int
}

// htmlAttr is one attribute with the ranges required to rewrite just its value.
type htmlAttr struct {
	key string
	val string
	// start..end spans `key="value"` (or a bare `key`).
	start, end int
	// valStart..valEnd spans the raw value inside the quotes; both are -1 when
	// the attribute has no value.
	valStart, valEnd int
	quote            byte // '"', '\'' or 0 for unquoted
}

// voidElements never have children or a closing tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// autoClosing lists tags whose open element is implicitly closed by a new start
// tag of the same name. Browsers apply the full HTML5 tree-construction rules;
// the patch engine only needs the handful that authors actually leave unclosed,
// so that a source tree still matches the DOM the renderer indexed.
var autoClosing = map[string]bool{
	"li": true, "p": true, "dt": true, "dd": true, "option": true,
	"tr": true, "td": true, "th": true, "thead": true, "tbody": true,
}

// parseHTMLDoc builds the offset-preserving tree of src.
func parseHTMLDoc(src []byte) *htmlDoc {
	doc := &htmlDoc{src: src}
	z := html.NewTokenizer(strings.NewReader(string(src)))
	var stack []*htmlElem
	offset := 0

	closeTop := func(end int) {
		if len(stack) == 0 {
			return
		}
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		top.innerEnd = top.startTagEnd
		if end > top.startTagEnd {
			top.innerEnd = end
		}
		top.outerEnd = end
	}

	for {
		tt := z.Next()
		raw := z.Raw()
		start := offset
		offset += len(raw)
		end := offset

		switch tt {
		case html.ErrorToken:
			// Anything still open runs to the end of the file.
			for len(stack) > 0 {
				closeTop(len(src))
			}
			doc.finalise()
			return doc

		case html.StartTagToken, html.SelfClosingTagToken:
			tag, attrs, selfClosing := parseStartTag(raw, start)
			if autoClosing[tag] && len(stack) > 0 && stack[len(stack)-1].tag == tag {
				closeTop(start)
			}
			el := &htmlElem{
				tag:           tag,
				attrs:         attrs,
				outerStart:    start,
				startTagStart: start,
				startTagEnd:   end,
				innerStart:    end,
				innerEnd:      end,
				outerEnd:      end,
			}
			if len(stack) > 0 {
				el.parent = stack[len(stack)-1]
				el.parent.children = append(el.parent.children, el)
			} else {
				doc.roots = append(doc.roots, el)
			}
			doc.all = append(doc.all, el)

			if selfClosing || voidElements[tag] || tt == html.SelfClosingTagToken {
				el.void = true
				el.innerStart, el.innerEnd = -1, -1
				continue
			}
			stack = append(stack, el)

		case html.EndTagToken:
			name, _ := z.TagName()
			tag := string(name)
			// Close back to the matching open element; stray end tags are ignored.
			idx := -1
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].tag == tag {
					idx = i
					break
				}
			}
			if idx < 0 {
				continue
			}
			for len(stack) > idx+1 {
				closeTop(start)
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			top.innerEnd = start
			top.outerEnd = end
		}
	}
}

// finalise computes per-element depth and nth-of-type indices.
func (d *htmlDoc) finalise() {
	var walk func(parent *htmlElem, children []*htmlElem, depth int)
	walk = func(parent *htmlElem, children []*htmlElem, depth int) {
		counts := make(map[string]int, len(children))
		for _, c := range children {
			counts[c.tag]++
			c.typeIndex = counts[c.tag]
			c.depth = depth
			walk(c, c.children, depth+1)
		}
	}
	walk(nil, d.roots, 0)
}

// attr returns an attribute by (case-insensitive) name.
func (e *htmlElem) attr(name string) (htmlAttr, bool) {
	for _, a := range e.attrs {
		if a.key == strings.ToLower(name) {
			return a, true
		}
	}
	return htmlAttr{}, false
}

// attrValue returns the unescaped value of an attribute.
func (e *htmlElem) attrValue(name string) string {
	a, ok := e.attr(name)
	if !ok {
		return ""
	}
	return html.UnescapeString(a.val)
}

// id returns the element id.
func (e *htmlElem) id() string { return e.attrValue("id") }

// classes returns the element class list.
func (e *htmlElem) classes() []string { return strings.Fields(e.attrValue("class")) }

// hasClass reports whether the element carries a class.
func (e *htmlElem) hasClass(name string) bool {
	for _, c := range e.classes() {
		if c == name {
			return true
		}
	}
	return false
}

// sameTypeSiblings counts the element siblings sharing this element's tag.
func (e *htmlElem) sameTypeSiblings() int {
	siblings := 0
	list := []*htmlElem(nil)
	if e.parent != nil {
		list = e.parent.children
	}
	for _, s := range list {
		if s.tag == e.tag {
			siblings++
		}
	}
	if siblings == 0 {
		siblings = 1
	}
	return siblings
}

// parseStartTag scans the raw bytes of a start tag, recording the exact ranges
// of the tag name and of every attribute. base is the offset of raw within the
// source document.
func parseStartTag(raw []byte, base int) (tag string, attrs []htmlAttr, selfClosing bool) {
	i := 0
	n := len(raw)
	if i < n && raw[i] == '<' {
		i++
	}
	nameStart := i
	for i < n && !isTagBreak(raw[i]) {
		i++
	}
	tag = strings.ToLower(string(raw[nameStart:i]))

	for i < n {
		for i < n && isHTMLSpace(raw[i]) {
			i++
		}
		if i >= n {
			break
		}
		if raw[i] == '>' {
			break
		}
		if raw[i] == '/' {
			selfClosing = true
			i++
			continue
		}
		attrStart := i
		for i < n && !isHTMLSpace(raw[i]) && raw[i] != '=' && raw[i] != '>' && raw[i] != '/' {
			i++
		}
		key := strings.ToLower(string(raw[attrStart:i]))
		if key == "" {
			i++
			continue
		}
		a := htmlAttr{key: key, start: base + attrStart, end: base + i, valStart: -1, valEnd: -1}

		save := i
		for i < n && isHTMLSpace(raw[i]) {
			i++
		}
		if i < n && raw[i] == '=' {
			i++
			for i < n && isHTMLSpace(raw[i]) {
				i++
			}
			if i < n && (raw[i] == '"' || raw[i] == '\'') {
				quote := raw[i]
				i++
				valStart := i
				for i < n && raw[i] != quote {
					i++
				}
				a.quote = quote
				a.val = string(raw[valStart:i])
				a.valStart = base + valStart
				a.valEnd = base + i
				if i < n {
					i++ // closing quote
				}
			} else {
				valStart := i
				for i < n && !isHTMLSpace(raw[i]) && raw[i] != '>' {
					i++
				}
				a.val = string(raw[valStart:i])
				a.valStart = base + valStart
				a.valEnd = base + i
			}
			a.end = base + i
		} else {
			i = save
		}
		attrs = append(attrs, a)
	}
	return tag, attrs, selfClosing
}

func isHTMLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

func isTagBreak(b byte) bool {
	return isHTMLSpace(b) || b == '>' || b == '/'
}

// insertPoint returns the offset inside the start tag where a new attribute can
// be spliced in: right after the last attribute, or after the tag name.
func (e *htmlElem) insertPoint() (int, error) {
	if e.startTagEnd <= e.startTagStart {
		return 0, fmt.Errorf("design: element <%s> has no usable start tag", e.tag)
	}
	if len(e.attrs) > 0 {
		return e.attrs[len(e.attrs)-1].end, nil
	}
	return e.startTagStart + 1 + len(e.tag), nil
}

// escapeAttr escapes a value for use inside a double-quoted attribute.
func escapeAttr(v string) string {
	r := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")
	return r.Replace(v)
}

// escapeText escapes a value for use as element text content.
func escapeText(v string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(v)
}

// htmlUnescape resolves character references in an attribute value so that
// selector comparisons work on the decoded text.
func htmlUnescape(s string) string { return html.UnescapeString(s) }
