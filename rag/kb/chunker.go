package kb

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/digiogithub/pando/rag/embeddings"
)

// lineChunk is a piece of a document body with its 1-based, inclusive line
// range in the source document (front matter included in the numbering).
type lineChunk struct {
	Text      string
	StartLine int
	EndLine   int
}

// docFields are the front-matter fields indexed for full-text search next to
// the body chunks (kb_doc_fields_fts).
type docFields struct {
	Title   string
	Labels  string
	Aliases string
}

func (f docFields) empty() bool { return f.Title == "" && f.Labels == "" && f.Aliases == "" }

// parsedDoc is a document split into its front matter and its body.
type parsedDoc struct {
	FM       FrontMatter
	Raw      map[string]interface{}
	Body     string
	BodyLine int // 1-based line of the body's first line in the source
	Fields   docFields
}

// parseDocument strips the YAML front matter from content and extracts the
// fields indexed for search. Invalid front matter is not an error: the whole
// content is then the body, as in ParseFrontMatter.
func parseDocument(content string) parsedDoc {
	fm, raw, body, err := ParseFrontMatterWithRaw(content)
	if err != nil {
		fm, raw, body = FrontMatter{}, nil, content
	}
	p := parsedDoc{FM: fm, Raw: raw, Body: body, BodyLine: 1}
	if body != content {
		// ParseFrontMatterWithRaw trims surrounding whitespace, so the body
		// is a suffix of content with its trailing whitespace removed.
		end := len(strings.TrimRightFunc(content, unicode.IsSpace))
		trimmedBody := strings.TrimRightFunc(body, unicode.IsSpace)
		if strings.HasSuffix(content[:end], trimmedBody) {
			p.BodyLine = strings.Count(content[:end-len(trimmedBody)], "\n") + 1
		}
	}
	p.Fields = docFields{
		Title:   firstString(raw["title"]),
		Labels:  strings.Join(stringList(raw["labels"]), " "),
		Aliases: strings.Join(fm.Aliases, " "),
	}
	if p.Fields.Title == "" {
		p.Fields.Title = firstHeading(body)
	}
	return p
}

// specBlockRe matches a requirement heading such as "### SPEC-0007.R3" or
// "### AUTH.R12 Login throttling". Such a block is never split across chunks.
var specBlockRe = regexp.MustCompile(`^###\s+[A-Za-z0-9_-]+\.R\d+\b`)

// headingLevel returns the ATX heading level of line (1-6), or 0.
func headingLevel(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0
	}
	if n < len(line) && line[n] != ' ' && line[n] != '\t' {
		return 0
	}
	return n
}

// block is a run of source lines [start, end) chunkLines keeps together when
// it can; an atomic block is never split, even when larger than a chunk.
type block struct {
	start, end int
	atomic     bool
}

// splitBlocks groups lines into paragraphs (separated by blank lines), with
// a heading always starting a new block, fenced code kept whole, and each
// "### <ID>.R<n>" requirement block kept whole up to the next heading of
// level 3 or higher.
func splitBlocks(lines []string) []block {
	var blocks []block
	cur := block{start: -1}
	flush := func(end int) {
		if cur.start >= 0 && end > cur.start {
			cur.end = end
			blocks = append(blocks, cur)
		}
		cur = block{start: -1}
	}
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if fenceMarker(trimmed) == fence {
				fence = ""
			}
			continue
		}
		if f := fenceMarker(trimmed); f != "" {
			if cur.start < 0 {
				cur.start = i
			}
			fence = f
			continue
		}
		level := headingLevel(trimmed)
		if cur.atomic {
			// A requirement block runs until the next heading of level <= 3.
			if level >= 1 && level <= 3 {
				flush(i)
			} else {
				continue
			}
		}
		switch {
		case level > 0:
			flush(i)
			cur = block{start: i, atomic: level == 3 && specBlockRe.MatchString(trimmed)}
		case trimmed == "":
			flush(i)
		default:
			if cur.start < 0 {
				cur.start = i
			}
		}
	}
	flush(len(lines))
	return blocks
}

// chunkLines splits a document body into chunks of at most size bytes that
// follow block boundaries (splitBlocks), each with its line range; firstLine
// is the source line of body's first line. A block larger than size is split
// on line boundaries unless it is atomic, and a single line larger than size
// is split with embeddings.ChunkText using overlap. Blank chunks are dropped.
func chunkLines(body string, firstLine, size, overlap int) []lineChunk {
	if size <= 0 {
		size = embeddings.DefaultChunkSize
	}
	lines := strings.Split(body, "\n")
	var out []lineChunk
	emit := func(start, end int) { // lines [start, end)
		for start < end && strings.TrimSpace(lines[start]) == "" {
			start++
		}
		for end > start && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		if start >= end {
			return
		}
		out = append(out, lineChunk{
			Text:      strings.Join(lines[start:end], "\n"),
			StartLine: firstLine + start,
			EndLine:   firstLine + end - 1,
		})
	}
	span := func(start, end int) int { // bytes of lines [start, end) joined
		n := 0
		for _, l := range lines[start:end] {
			n += len(l) + 1
		}
		return n - 1
	}

	curStart, curEnd := -1, -1
	flush := func() {
		if curStart >= 0 {
			emit(curStart, curEnd)
		}
		curStart, curEnd = -1, -1
	}
	for _, b := range splitBlocks(lines) {
		if curStart >= 0 && span(curStart, b.end) <= size {
			curEnd = b.end
			continue
		}
		flush()
		if b.atomic || span(b.start, b.end) <= size {
			curStart, curEnd = b.start, b.end
			continue
		}
		// Oversized, splittable block: pack whole lines; a single line that
		// alone exceeds size is cut by characters.
		ls := -1
		for i := b.start; i < b.end; i++ {
			if len(lines[i]) > size {
				if ls >= 0 {
					emit(ls, i)
					ls = -1
				}
				for _, piece := range embeddings.ChunkText(lines[i], size, overlap) {
					if strings.TrimSpace(piece) != "" {
						out = append(out, lineChunk{Text: piece, StartLine: firstLine + i, EndLine: firstLine + i})
					}
				}
				continue
			}
			if ls >= 0 && span(ls, i+1) > size {
				emit(ls, i)
				ls = -1
			}
			if ls < 0 {
				ls = i
			}
		}
		if ls >= 0 {
			emit(ls, b.end)
		}
	}
	flush()
	return out
}

func firstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if headingLevel(t) == 1 {
			return strings.TrimSpace(t[1:])
		}
	}
	return ""
}

func firstString(v interface{}) string {
	if l := stringList(v); len(l) > 0 {
		return l[0]
	}
	return ""
}

// stringList reads a front-matter value that may be a string or a list.
func stringList(v interface{}) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		if s := strings.TrimSpace(t); s != "" {
			return []string{s}
		}
		return nil
	case []string:
		return t
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if e == nil {
				continue
			}
			if s := strings.TrimSpace(fmt.Sprint(e)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return []string{fmt.Sprint(t)}
	}
}
