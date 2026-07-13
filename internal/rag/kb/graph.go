package kb

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Relatedness weights. An outgoing link is the strongest signal (the author of
// this document pointed at that one on purpose), a backlink is almost as strong
// but was not authored here, and shared tags are a weak topical hint.
const (
	weightOutgoing   = 1.0
	weightBacklink   = 0.9
	weightSharedTags = 0.3
)

// defaultRelatedLimit bounds RelatedDocuments when the caller passes no limit.
const defaultRelatedLimit = 10

// LinkTarget is a wiki link together with the document it resolves to, if any.
// Unresolved targets are kept on purpose: they are concepts the author referred
// to but nobody has documented yet.
type LinkTarget struct {
	WikiLink
	// FilePath of the document the link points at, empty when unresolved.
	FilePath string
	// Resolved reports whether the target slug matched a document.
	Resolved bool
}

// Backlink is an incoming link: another document pointing at this one.
type Backlink struct {
	// FilePath of the document that holds the link.
	FilePath string
	// Slug is the target the source wrote, which may be an alias or a bare
	// basename rather than this document's full path.
	Slug string
	// Label is the [[target|label]] text, empty when the link had none.
	Label string
}

// RelatedDocument is a neighbour in the document graph, scored by how it is
// connected.
type RelatedDocument struct {
	FilePath string
	Score    float64
	// Reasons explains the score, e.g. "links to it", "links here",
	// "shared tags: kb, wiki".
	Reasons []string
}

// WantedConcept is a link target that no document defines yet: the wiki
// equivalent of a red link. Frequently wanted concepts are the best candidates
// for the next document to write.
type WantedConcept struct {
	// Slug is the normalized target.
	Slug string
	// Raw is the target as an author first wrote it, which reads better than the
	// slug when suggesting a title.
	Raw string
	// Count is how many documents point at it.
	Count int
	// Sources are the documents that point at it.
	Sources []string
}

// docEntry is one document as seen by the graph queries: everything they need
// except the content, which is never loaded here.
type docEntry struct {
	id       int64
	filePath string
	tags     []string
	aliases  []string
}

// linkResolver maps link slugs to documents. Resolution happens at query time
// rather than at write time, which is what lets a link point at a document that
// does not exist yet and lets a renamed document be found again through its
// path, basename or aliases without rewriting stored rows.
type linkResolver struct {
	docs   []docEntry
	bySlug map[string]docEntry
}

// newLinkResolver builds the slug index. Precedence is full path first, then
// basename, then declared aliases: a link that spells out the path always wins,
// and a shorter form only resolves to a document that nothing more specific
// claimed. Within one precedence level the first document in path order wins, so
// resolution is deterministic when two documents share a basename.
func (s *KBStore) newLinkResolver(ctx context.Context) (*linkResolver, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, file_path, metadata
		FROM kb_documents
		ORDER BY file_path`)
	if err != nil {
		return nil, fmt.Errorf("kb: list documents for link resolution: %w", err)
	}
	defer rows.Close()

	r := &linkResolver{bySlug: make(map[string]docEntry)}
	for rows.Next() {
		var (
			e       docEntry
			rawMeta string
		)
		if err := rows.Scan(&e.id, &e.filePath, &rawMeta); err != nil {
			return nil, fmt.Errorf("kb: scan document for link resolution: %w", err)
		}
		if rawMeta != "" {
			var meta map[string]interface{}
			if err := json.Unmarshal([]byte(rawMeta), &meta); err == nil {
				e.tags = ExtractTagsFromMetadata(meta)
				e.aliases = ExtractAliasesFromMetadata(meta)
			}
		}
		r.docs = append(r.docs, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("kb: iterate documents for link resolution: %w", err)
	}

	claim := func(slug string, e docEntry) {
		if slug == "" {
			return
		}
		if _, taken := r.bySlug[slug]; taken {
			return
		}
		r.bySlug[slug] = e
	}
	for _, e := range r.docs {
		claim(NormalizeSlug(e.filePath), e)
	}
	for _, e := range r.docs {
		claim(baseSlug(e.filePath), e)
	}
	for _, e := range r.docs {
		for _, alias := range e.aliases {
			claim(NormalizeSlug(alias), e)
		}
	}

	return r, nil
}

// resolve returns the document a slug points at.
func (r *linkResolver) resolve(slug string) (docEntry, bool) {
	e, ok := r.bySlug[slug]
	return e, ok
}

// entry returns the indexed document for a file path.
func (r *linkResolver) entry(filePath string) (docEntry, bool) {
	for _, e := range r.docs {
		if e.filePath == filePath {
			return e, true
		}
	}
	return docEntry{}, false
}

// slugsOf returns every slug a document answers to.
func (r *linkResolver) slugsOf(e docEntry) []string {
	return DocumentSlugs(e.filePath, e.aliases)
}

// baseSlug is the slug of a document's file name alone, so that
// "pando/plans/foo.md" can also be reached as [[foo]].
func baseSlug(filePath string) string {
	slug := NormalizeSlug(filePath)
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		return slug[i+1:]
	}
	return slug
}

// OutgoingLinks returns the wiki links a document declares, each resolved
// against the current documents. A document with no links yields an empty slice
// and no error, which is the normal state of a knowledge base that does not use
// the syntax.
func (s *KBStore) OutgoingLinks(ctx context.Context, filePath string) ([]LinkTarget, error) {
	links, err := s.LinksFrom(ctx, filePath)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, nil
	}

	resolver, err := s.newLinkResolver(ctx)
	if err != nil {
		return nil, err
	}

	targets := make([]LinkTarget, 0, len(links))
	for _, l := range links {
		t := LinkTarget{WikiLink: l}
		if e, ok := resolver.resolve(l.Slug); ok {
			t.Resolved, t.FilePath = true, e.filePath
		}
		targets = append(targets, t)
	}
	return targets, nil
}

// Backlinks returns the documents that link to this one, through its path, its
// basename or any of its aliases.
//
// A link only counts when it actually resolves here: two documents may share a
// basename, and the shorter form belongs to whichever one the resolver awards it
// to, so a link to "[[foo]]" is not a backlink of every document named foo.md.
func (s *KBStore) Backlinks(ctx context.Context, filePath string) ([]Backlink, error) {
	resolver, err := s.newLinkResolver(ctx)
	if err != nil {
		return nil, err
	}
	self, ok := resolver.entry(filePath)
	if !ok {
		return nil, nil
	}
	return s.backlinksOf(ctx, resolver, self)
}

// backlinksOf is the shared implementation, taking a resolver the caller already
// built.
func (s *KBStore) backlinksOf(ctx context.Context, resolver *linkResolver, self docEntry) ([]Backlink, error) {
	slugs := resolver.slugsOf(self)
	if len(slugs) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(slugs)), ",")
	args := make([]any, 0, len(slugs)+1)
	for _, s := range slugs {
		args = append(args, s)
	}
	args = append(args, self.id)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT d.file_path, l.target_slug, l.label
		FROM kb_links l
		JOIN kb_documents d ON d.id = l.source_document_id
		WHERE l.target_slug IN (%s)
		  AND l.source_document_id != ?
		ORDER BY d.file_path, l.position`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("kb: backlinks of %s: %w", self.filePath, err)
	}
	defer rows.Close()

	var out []Backlink
	for rows.Next() {
		var b Backlink
		if err := rows.Scan(&b.FilePath, &b.Slug, &b.Label); err != nil {
			return nil, fmt.Errorf("kb: scan backlink: %w", err)
		}
		// The slug must belong to this document, not to another one that won it.
		if e, ok := resolver.resolve(b.Slug); !ok || e.id != self.id {
			continue
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// RelatedDocuments returns the neighbours of a document in the graph, ordered by
// how strongly they are connected: documents it links to, documents that link to
// it, and — weakly — documents sharing tags with it. limit <= 0 uses a default.
func (s *KBStore) RelatedDocuments(ctx context.Context, filePath string, limit int) ([]RelatedDocument, error) {
	if limit <= 0 {
		limit = defaultRelatedLimit
	}

	resolver, err := s.newLinkResolver(ctx)
	if err != nil {
		return nil, err
	}
	self, ok := resolver.entry(filePath)
	if !ok {
		return nil, nil
	}

	scores := make(map[string]*RelatedDocument)
	// A neighbour is scored once per kind of connection: two documents linked
	// both by path and by basename are still one link, not two.
	counted := make(map[string]struct{})
	add := func(path, kind string, weight float64, reason string) {
		if path == "" || path == filePath {
			return
		}
		if _, dup := counted[kind+"\x00"+path]; dup {
			return
		}
		counted[kind+"\x00"+path] = struct{}{}

		r, ok := scores[path]
		if !ok {
			r = &RelatedDocument{FilePath: path}
			scores[path] = r
		}
		r.Score += weight
		r.Reasons = append(r.Reasons, reason)
	}

	links, err := s.LinksFrom(ctx, filePath)
	if err != nil {
		return nil, err
	}
	for _, l := range links {
		if e, ok := resolver.resolve(l.Slug); ok {
			add(e.filePath, "outgoing", weightOutgoing, "links to it")
		}
	}

	backlinks, err := s.backlinksOf(ctx, resolver, self)
	if err != nil {
		return nil, err
	}
	for _, b := range backlinks {
		add(b.FilePath, "backlink", weightBacklink, "links here")
	}

	for _, e := range resolver.docs {
		if e.id == self.id {
			continue
		}
		if shared := sharedTags(self.tags, e.tags); len(shared) > 0 {
			add(e.filePath, "tags", weightSharedTags, "shared tags: "+strings.Join(shared, ", "))
		}
	}

	out := make([]RelatedDocument, 0, len(scores))
	for _, r := range scores {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].FilePath < out[j].FilePath
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// sharedTags returns the tags two documents have in common, compared
// case-insensitively.
func sharedTags(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	inA := make(map[string]string, len(a))
	for _, t := range a {
		inA[strings.ToLower(strings.TrimSpace(t))] = t
	}

	var shared []string
	seen := make(map[string]struct{})
	for _, t := range b {
		key := strings.ToLower(strings.TrimSpace(t))
		orig, ok := inA[key]
		if !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		shared = append(shared, orig)
	}
	return shared
}

// WantedConcepts returns the link targets that no document defines, most linked
// first: the concepts the knowledge base refers to but never explains. limit <= 0
// returns them all.
func (s *KBStore) WantedConcepts(ctx context.Context, limit int) ([]WantedConcept, error) {
	resolver, err := s.newLinkResolver(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT l.target_slug, l.target_raw, d.file_path
		FROM kb_links l
		JOIN kb_documents d ON d.id = l.source_document_id
		ORDER BY l.id`)
	if err != nil {
		return nil, fmt.Errorf("kb: list links for wanted concepts: %w", err)
	}
	defer rows.Close()

	wanted := make(map[string]*WantedConcept)
	var order []string
	for rows.Next() {
		var slug, raw, source string
		if err := rows.Scan(&slug, &raw, &source); err != nil {
			return nil, fmt.Errorf("kb: scan link: %w", err)
		}
		if _, resolved := resolver.resolve(slug); resolved {
			continue
		}
		w, ok := wanted[slug]
		if !ok {
			// The first spelling seen wins: it is the one to suggest as a title.
			w = &WantedConcept{Slug: slug, Raw: raw}
			wanted[slug] = w
			order = append(order, slug)
		}
		w.Count++
		w.Sources = append(w.Sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("kb: iterate links: %w", err)
	}

	out := make([]WantedConcept, 0, len(wanted))
	for _, slug := range order {
		out = append(out, *wanted[slug])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Slug < out[j].Slug
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
