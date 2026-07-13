package kb

import (
	"context"
	"reflect"
	"testing"
)

func TestExtractWikiLinks(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []WikiLink
	}{
		{
			name: "no links",
			body: "# Title\n\nPlain prose with [a markdown link](foo.md).",
			want: nil,
		},
		{
			name: "simple and labelled links",
			body: "See [[code-edges]] and [[pando/plans/foo.md|the plan]].",
			want: []WikiLink{
				{Raw: "code-edges", Slug: "code-edges", Position: 0},
				{Raw: "pando/plans/foo.md", Slug: "pando/plans/foo", Label: "the plan", Position: 1},
			},
		},
		{
			name: "deduplicated by slug, first occurrence wins",
			body: "[[Foo Bar]] then [[foo_bar]] then [[foo-bar]].",
			want: []WikiLink{
				{Raw: "Foo Bar", Slug: "foo-bar", Position: 0},
			},
		},
		{
			name: "fenced code block is ignored",
			body: "Real [[alpha]].\n\n```md\n[[not-a-link]]\n```\n\nAlso [[beta]].",
			want: []WikiLink{
				{Raw: "alpha", Slug: "alpha", Position: 0},
				{Raw: "beta", Slug: "beta", Position: 1},
			},
		},
		{
			name: "tilde fence is ignored",
			body: "~~~\n[[fenced]]\n~~~\n[[real]]",
			want: []WikiLink{
				{Raw: "real", Slug: "real", Position: 0},
			},
		},
		{
			name: "inline code span is ignored",
			body: "Write links as `[[concept]]` — for example [[actual-concept]].",
			want: []WikiLink{
				{Raw: "actual-concept", Slug: "actual-concept", Position: 0},
			},
		},
		{
			name: "unterminated inline code does not swallow links",
			body: "A stray ` backtick and [[still-linked]].",
			want: []WikiLink{
				{Raw: "still-linked", Slug: "still-linked", Position: 0},
			},
		},
		{
			name: "empty and whitespace targets are dropped",
			body: "[[]] and [[   ]] and [[ok]]",
			want: []WikiLink{
				{Raw: "ok", Slug: "ok", Position: 0},
			},
		},
		{
			name: "unicode target",
			body: "[[diseño de módulos]]",
			want: []WikiLink{
				{Raw: "diseño de módulos", Slug: "diseño-de-módulos", Position: 0},
			},
		},
		{
			name: "empty label keeps the link",
			body: "[[target|]]",
			want: []WikiLink{
				{Raw: "target", Slug: "target", Position: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractWikiLinks(tt.body)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractWikiLinks() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizeSlug(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Foo Bar", "foo-bar"},
		{"foo_bar", "foo-bar"},
		{"pando/features/Foo Bar.md", "pando/features/foo-bar"},
		{"/pando/plans/x.markdown/", "pando/plans/x"},
		{"  spaced   out  ", "spaced-out"},
		{"already-normalized", "already-normalized"},
		{"", ""},
		{"---", ""},
	}

	for _, tt := range tests {
		if got := NormalizeSlug(tt.in); got != tt.want {
			t.Errorf("NormalizeSlug(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDocumentSlugs(t *testing.T) {
	got := DocumentSlugs("pando/features/kb_wiki_links.md", []string{"Wiki Links", "kb-wiki-links"})
	want := []string{"pando/features/kb-wiki-links", "kb-wiki-links", "wiki-links"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DocumentSlugs() = %#v, want %#v", got, want)
	}
}

// fakeEmbedder returns a fixed-size vector per text so store writes work without
// a real embedding provider.
type fakeEmbedder struct{}

func (fakeEmbedder) EmbedDocuments(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}

func (fakeEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func (fakeEmbedder) Dimension() int { return 3 }

func TestDocumentLinksLifecycle(t *testing.T) {
	db := openTestKBDB(t)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	const path = "pando/changes/alpha.md"

	if err := store.AddDocument(ctx, path, "Alpha refers to [[beta]] and [[pando/features/gamma.md|Gamma]].", nil); err != nil {
		t.Fatalf("AddDocument() error = %v", err)
	}

	links, err := store.LinksFrom(ctx, path)
	if err != nil {
		t.Fatalf("LinksFrom() error = %v", err)
	}
	want := []WikiLink{
		{Raw: "beta", Slug: "beta", Position: 0},
		{Raw: "pando/features/gamma.md", Slug: "pando/features/gamma", Label: "Gamma", Position: 1},
	}
	if !reflect.DeepEqual(links, want) {
		t.Fatalf("LinksFrom() after add = %#v, want %#v", links, want)
	}

	// Update replaces the link set (UpdateDocument = delete + add).
	if err := store.UpdateDocument(ctx, path, "Now only [[delta]] matters.", nil); err != nil {
		t.Fatalf("UpdateDocument() error = %v", err)
	}
	links, err = store.LinksFrom(ctx, path)
	if err != nil {
		t.Fatalf("LinksFrom() after update error = %v", err)
	}
	if len(links) != 1 || links[0].Slug != "delta" {
		t.Fatalf("LinksFrom() after update = %#v, want single delta link", links)
	}

	// Deleting the document removes its links.
	if err := store.DeleteDocument(ctx, path); err != nil {
		t.Fatalf("DeleteDocument() error = %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_links`).Scan(&count); err != nil {
		t.Fatalf("count kb_links error = %v", err)
	}
	if count != 0 {
		t.Fatalf("kb_links rows after delete = %d, want 0", count)
	}
}

func TestAddDocumentWithEmbeddingsIndexesLinks(t *testing.T) {
	db := openTestKBDB(t)
	store := NewKBStore(db, fakeEmbedder{}, 0, 0)
	ctx := context.Background()

	const path = "pando/changes/proxied.md"
	body := "Forwarded write linking [[epsilon]]."

	// Mirrors what the primary does for an IPC-forwarded KB write.
	if err := store.AddDocumentWithEmbeddings(ctx, path, body, nil,
		[]string{body}, [][]float32{{1, 0, 0}}); err != nil {
		t.Fatalf("AddDocumentWithEmbeddings() error = %v", err)
	}

	links, err := store.LinksFrom(ctx, path)
	if err != nil {
		t.Fatalf("LinksFrom() error = %v", err)
	}
	if len(links) != 1 || links[0].Slug != "epsilon" {
		t.Fatalf("LinksFrom() = %#v, want single epsilon link", links)
	}
}

func TestUpsertMemoryByKeyIndexesLinks(t *testing.T) {
	db := openTestKBDB(t)
	store := NewKBStore(db, nil, 0, 0)
	ctx := context.Background()

	opts := MemoryUpsertOptions{
		FilePath: "memory/project/notes.md",
		Key:      "pando.links.test",
		Content:  "Remember that [[zeta]] supersedes [[eta]].",
		Tags:     []string{"memory"},
	}
	if _, err := store.UpsertMemory(ctx, opts); err != nil {
		t.Fatalf("UpsertMemory() error = %v", err)
	}

	links, err := store.LinksFrom(ctx, opts.FilePath)
	if err != nil {
		t.Fatalf("LinksFrom() error = %v", err)
	}
	if len(links) != 2 || links[0].Slug != "zeta" || links[1].Slug != "eta" {
		t.Fatalf("LinksFrom() = %#v, want zeta + eta", links)
	}

	// A second upsert must not duplicate rows.
	opts.Content = "Only [[zeta]] now."
	if _, err := store.UpsertMemory(ctx, opts); err != nil {
		t.Fatalf("second UpsertMemory() error = %v", err)
	}
	links, err = store.LinksFrom(ctx, opts.FilePath)
	if err != nil {
		t.Fatalf("LinksFrom() after re-upsert error = %v", err)
	}
	if len(links) != 1 || links[0].Slug != "zeta" {
		t.Fatalf("LinksFrom() after re-upsert = %#v, want single zeta link", links)
	}
}
