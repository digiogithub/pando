package kb

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
)

// WikiLink is a [[target]] or [[target|label]] reference found in a document body.
//
// Slug is the normalized form used for storage and resolution; Raw preserves the
// target exactly as the author wrote it. A link is not required to resolve to an
// existing document: unresolved targets are kept on purpose, since they record
// concepts that are worth documenting later.
type WikiLink struct {
	Raw      string
	Slug     string
	Label    string
	Position int
}

// wikiLinkRe matches [[target]] and [[target|label]] on a single line.
var wikiLinkRe = regexp.MustCompile(`\[\[([^\[\]|\n]+)(?:\|([^\[\]\n]*))?\]\]`)

// markdownExtensions are stripped from link targets and document paths so that
// "pando/features/foo.md", "pando/features/foo" and "[[pando/features/foo]]"
// all normalize to the same slug.
var markdownExtensions = []string{".md", ".markdown", ".mdx"}

// ExtractWikiLinks returns the wiki links found in a document body, in order of
// appearance and deduplicated by slug (the first occurrence wins). Links inside
// fenced code blocks and inline code spans are ignored, so documentation that
// merely shows the syntax does not pollute the graph.
func ExtractWikiLinks(body string) []WikiLink {
	if !strings.Contains(body, "[[") {
		return nil
	}

	var links []WikiLink
	seen := make(map[string]struct{})

	inFence := false
	fenceChar := ""

	for _, line := range strings.Split(body, "\n") {
		if marker := fenceMarker(strings.TrimSpace(line)); marker != "" {
			switch {
			case !inFence:
				inFence, fenceChar = true, marker
			case marker == fenceChar:
				inFence, fenceChar = false, ""
			}
			continue
		}
		if inFence {
			continue
		}

		for _, m := range wikiLinkRe.FindAllStringSubmatch(stripInlineCode(line), -1) {
			raw := strings.TrimSpace(m[1])
			slug := NormalizeSlug(raw)
			if slug == "" {
				continue
			}
			if _, dup := seen[slug]; dup {
				continue
			}
			seen[slug] = struct{}{}
			links = append(links, WikiLink{
				Raw:      raw,
				Slug:     slug,
				Label:    strings.TrimSpace(m[2]),
				Position: len(links),
			})
		}
	}

	return links
}

// fenceMarker reports the fence character of a code-fence line ("`" or "~"),
// or "" when the line does not open/close a fence.
func fenceMarker(trimmed string) string {
	switch {
	case strings.HasPrefix(trimmed, "```"):
		return "`"
	case strings.HasPrefix(trimmed, "~~~"):
		return "~"
	}
	return ""
}

// stripInlineCode removes backtick-delimited code spans from a line so that
// `[[example]]` written as inline code is not indexed as a link.
func stripInlineCode(line string) string {
	if !strings.Contains(line, "`") {
		return line
	}

	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] != '`' {
			b.WriteByte(line[i])
			i++
			continue
		}

		run := 0
		for i+run < len(line) && line[i+run] == '`' {
			run++
		}
		closeIdx := indexBacktickRun(line, i+run, run)
		if closeIdx < 0 {
			// Unterminated span: the backticks are literal text.
			b.WriteString(line[i:])
			break
		}
		i = closeIdx + run
	}
	return b.String()
}

// indexBacktickRun returns the start index of the next run of exactly n
// backticks at or after from, or -1 when there is none.
func indexBacktickRun(s string, from, n int) int {
	for i := from; i < len(s); i++ {
		if s[i] != '`' {
			continue
		}
		j := i
		for j < len(s) && s[j] == '`' {
			j++
		}
		if j-i == n {
			return i
		}
		i = j - 1
	}
	return -1
}

// NormalizeSlug converts a link target or document path into its canonical slug:
// lower-cased, markdown extension stripped, spaces/underscores collapsed into
// single dashes, path separators preserved.
//
//	"pando/features/Foo Bar.md" -> "pando/features/foo-bar"
//	"Feature_Foo"               -> "feature-foo"
func NormalizeSlug(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.Trim(s, "/")
	for _, ext := range markdownExtensions {
		if strings.HasSuffix(s, ext) {
			s = strings.TrimSuffix(s, ext)
			break
		}
	}

	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r == '/':
			b.WriteRune('/')
			prevDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		default:
			b.WriteRune(r)
			prevDash = false
		}
	}

	out := b.String()
	out = strings.ReplaceAll(out, "/-", "/")
	out = strings.ReplaceAll(out, "-/", "/")
	return strings.Trim(out, "-/")
}

// DocumentSlugs returns every slug a document can be addressed by: its full
// path, its basename, and any declared aliases. Link resolution matches a
// target slug against this set.
func DocumentSlugs(filePath string, aliases []string) []string {
	slugs := make([]string, 0, 2+len(aliases))
	seen := make(map[string]struct{})

	add := func(s string) {
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		slugs = append(slugs, s)
	}

	add(NormalizeSlug(filePath))
	add(NormalizeSlug(path.Base(filePath)))
	for _, a := range aliases {
		add(NormalizeSlug(a))
	}

	return slugs
}

// sqlExecer is satisfied by both *sql.DB and *sql.Tx.
type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// indexDocumentLinks refreshes the kb_links rows of a document unless the wiki
// link graph is disabled, in which case it writes nothing. Every write path goes
// through it, so the toggle is enforced in one place.
func (s *KBStore) indexDocumentLinks(ctx context.Context, ex sqlExecer, docID int64, filePath, body string) (int, error) {
	if !s.WikiLinksEnabled() {
		return 0, nil
	}
	return replaceDocumentLinks(ctx, ex, docID, filePath, body)
}

// countIndexedLinks reports how many links a body contributes to the graph,
// which is zero when the graph is off. The sync uses it to report LinksIndexed
// without threading a count back out of the store's write methods; the re-scan is
// cheap (it bails out immediately on a body with no "[[").
func (s *KBStore) countIndexedLinks(body string) int {
	if !s.WikiLinksEnabled() {
		return 0
	}
	return len(ExtractWikiLinks(body))
}

// replaceDocumentLinks refreshes the kb_links rows of a document, returning how
// many links were indexed. Callers pass the transaction that inserted the
// document so links land atomically with it.
func replaceDocumentLinks(ctx context.Context, ex sqlExecer, docID int64, filePath, body string) (int, error) {
	if _, err := ex.ExecContext(ctx, `DELETE FROM kb_links WHERE source_document_id = ?`, docID); err != nil {
		return 0, fmt.Errorf("kb: delete links: %w", err)
	}
	return insertDocumentLinks(ctx, ex, docID, filePath, ExtractWikiLinks(body))
}

// insertDocumentLinks writes the given links for a document, assuming its
// previous rows are already gone. Callers that know the document has no rows
// (the backfill) use it to skip a pointless DELETE.
func insertDocumentLinks(ctx context.Context, ex sqlExecer, docID int64, filePath string, links []WikiLink) (int, error) {
	for _, l := range links {
		if _, err := ex.ExecContext(ctx, `
			INSERT INTO kb_links (source_document_id, source_path, target_slug, target_raw, label, position)
			VALUES (?, ?, ?, ?, ?, ?)`,
			docID, filePath, l.Slug, l.Raw, l.Label, l.Position,
		); err != nil {
			return 0, fmt.Errorf("kb: insert link %q: %w", l.Slug, err)
		}
	}

	return len(links), nil
}

// LinksFrom returns the wiki links stored for a document, ordered by position.
// Targets are returned unresolved; resolution against real documents is done by
// the graph queries.
func (s *KBStore) LinksFrom(ctx context.Context, filePath string) ([]WikiLink, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.target_raw, l.target_slug, l.label, l.position
		FROM kb_links l
		JOIN kb_documents d ON d.id = l.source_document_id
		WHERE d.file_path = ?
		ORDER BY l.position`,
		filePath,
	)
	if err != nil {
		return nil, fmt.Errorf("kb: links from %s: %w", filePath, err)
	}
	defer rows.Close()

	var links []WikiLink
	for rows.Next() {
		var l WikiLink
		if err := rows.Scan(&l.Raw, &l.Slug, &l.Label, &l.Position); err != nil {
			return nil, fmt.Errorf("kb: scan link: %w", err)
		}
		links = append(links, l)
	}

	return links, rows.Err()
}
