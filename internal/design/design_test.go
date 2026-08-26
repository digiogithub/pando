package design

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"github.com/digiogithub/pando/internal/snapshot"
)

// --- helpers ---

// newTestStore opens an in-memory database carrying the design schema. The
// schema is read from the real migration so the test fails when the two drift.
func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := db.Exec(designSchema(t)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return NewStore(db)
}

// designSchema extracts the Up section of the design migration.
func designSchema(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "db", "migrations", "20260826000001_add_design.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	body := string(data)
	start := strings.Index(body, "-- +goose StatementBegin")
	end := strings.Index(body, "-- +goose Down")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("migration %s has no parseable Up section", path)
	}
	up := body[start:end]
	up = regexp.MustCompile(`(?m)^-- \+goose.*$`).ReplaceAllString(up, "")
	return up
}

// fakeSnapshotter keeps scoped snapshots in memory with the same contract as
// the real one: it only ever reads and restores files under the snapshot root.
type fakeSnapshotter struct {
	roots   map[string]string
	content map[string]map[string]string
	next    int
}

func newFakeSnapshotter() *fakeSnapshotter {
	return &fakeSnapshotter{roots: map[string]string{}, content: map[string]map[string]string{}}
}

func (f *fakeSnapshotter) CreateScoped(_ context.Context, sessionID, description, rootDir string) (snapshot.Snapshot, error) {
	f.next++
	id := "snap-" + string(rune('a'+f.next-1))
	files := map[string]string{}
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	f.roots[id] = rootDir
	f.content[id] = files
	return snapshot.Snapshot{
		ID:          id,
		SessionID:   sessionID,
		Type:        snapshot.SnapshotTypeScoped,
		Description: description,
		WorkingDir:  rootDir,
		FileCount:   len(files),
	}, nil
}

func (f *fakeSnapshotter) RevertScoped(_ context.Context, id string) error {
	root, ok := f.roots[id]
	if !ok {
		return errors.New("unknown snapshot " + id)
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	for rel, content := range f.content[id] {
		target := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeSnapshotter) Compare(_ context.Context, from, to string) ([]snapshot.DiffEntry, error) {
	fromFiles, toFiles := f.content[from], f.content[to]
	var entries []snapshot.DiffEntry
	for path, oldContent := range fromFiles {
		newContent, ok := toFiles[path]
		switch {
		case !ok:
			entries = append(entries, snapshot.DiffEntry{Path: path, Type: snapshot.DiffDeleted})
		case newContent != oldContent:
			entries = append(entries, snapshot.DiffEntry{Path: path, Type: snapshot.DiffModified})
		}
	}
	for path := range toFiles {
		if _, ok := fromFiles[path]; !ok {
			entries = append(entries, snapshot.DiffEntry{Path: path, Type: snapshot.DiffAdded})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// newTestService wires a service over a temp project directory.
func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	project := t.TempDir()
	layout := NewLayout(project, "designer", "_system")
	svc := NewService(newTestStore(t), newFakeSnapshotter(), layout, "session-1")
	return svc, project
}

// --- layout ---

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Landing Page":        "landing-page",
		"  Café  Brûlée  ":    "caf-br-l-e",
		"a/b/../c":            "a-b-c",
		"_system":             "_system-artifact",
		"...":                 "",
		"Q4 Deck — Revenue!!": "q4-deck-revenue",
	}
	for input, want := range cases {
		if got := Slugify(input); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestLayoutAbsDirRejectsEscapes(t *testing.T) {
	layout := NewLayout("/tmp/project", "designer", "_system")
	for _, bad := range []string{"../secrets", "designer/../../etc", "/etc/passwd", "other/dir"} {
		if _, err := layout.AbsDir(bad); err == nil {
			t.Errorf("AbsDir(%q) accepted a path outside the design root", bad)
		}
	}
	got, err := layout.AbsDir("designer/landing")
	if err != nil {
		t.Fatalf("AbsDir: %v", err)
	}
	if want := filepath.Join("/tmp/project", "designer", "landing"); got != want {
		t.Fatalf("AbsDir = %q, want %q", got, want)
	}
}

// --- manifest ---

func TestManifestRoundTripAndNormalize(t *testing.T) {
	dir := t.TempDir()
	m := NewManifest("dsg_1", KindDeck, "Q4 Deck")
	if err := WriteManifest(dir, m); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	got, err := ReadManifest(dir)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if got.ID != "dsg_1" || got.Kind != KindDeck || got.Entry != "index.html" {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if got.Deck == nil {
		t.Fatal("deck manifest has no deck block")
	}
	if got.Preview.Viewport != DefaultViewport {
		t.Fatalf("viewport = %+v, want %+v", got.Preview.Viewport, DefaultViewport)
	}

	// A hand-edited manifest with an unknown kind falls back to web and drops
	// the deck block instead of failing the artifact.
	broken := Manifest{ID: "dsg_2", Kind: "hologram", Deck: &DeckSpec{Slides: 3}}
	broken.Normalize()
	if broken.Kind != KindWeb || broken.Deck != nil || broken.Version != 1 {
		t.Fatalf("Normalize did not repair the manifest: %+v", broken)
	}
}

// --- service ---

func TestCreateArtifactMaterialisesDirectoryAndVersionOne(t *testing.T) {
	svc, project := newTestService(t)
	ctx := context.Background()

	artifact, err := svc.Create(ctx, CreateParams{Title: "Landing Page", Kind: KindWeb})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if artifact.Dir != "designer/landing-page" {
		t.Fatalf("dir = %q, want designer/landing-page", artifact.Dir)
	}
	if artifact.CurrentVersion != 1 {
		t.Fatalf("current version = %d, want 1", artifact.CurrentVersion)
	}

	absDir := filepath.Join(project, "designer", "landing-page")
	for _, name := range []string{"index.html", ManifestName} {
		if _, err := os.Stat(filepath.Join(absDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}

	stored, err := svc.Get(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.CurrentVersion != 1 || stored.Title != "Landing Page" {
		t.Fatalf("stored artifact = %+v", stored)
	}
}

func TestCreateRejectsUnsupportedKindAndDuplicateDir(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, CreateParams{Title: "Phone App", Kind: "mobile"}); err == nil {
		t.Fatal("Create accepted a kind that v1 does not support")
	}
	if _, err := svc.Create(ctx, CreateParams{Title: "Landing", Slug: "landing"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Create(ctx, CreateParams{Title: "Landing again", Slug: "landing"}); err == nil {
		t.Fatal("Create overwrote an existing artifact directory")
	}
}

func TestCreateRejectsSeedFilesEscapingTheArtifact(t *testing.T) {
	svc, project := newTestService(t)
	_, err := svc.Create(context.Background(), CreateParams{
		Title: "Evil",
		Files: map[string]string{"../../escaped.html": "<h1>nope</h1>"},
	})
	if err == nil {
		t.Fatal("Create accepted a seed file outside the artifact directory")
	}
	if _, statErr := os.Stat(filepath.Join(project, "escaped.html")); !os.IsNotExist(statErr) {
		t.Fatalf("seed file escaped the artifact directory (err=%v)", statErr)
	}
}

func TestVersionHistoryAndCheckout(t *testing.T) {
	svc, project := newTestService(t)
	ctx := context.Background()

	artifact, err := svc.Create(ctx, CreateParams{
		Title: "Landing",
		Files: map[string]string{"index.html": "<h1>v1</h1>"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	entry := filepath.Join(project, "designer", "landing", "index.html")

	if err := os.WriteFile(entry, []byte("<h1>v2</h1>"), 0o644); err != nil {
		t.Fatalf("edit artifact: %v", err)
	}
	v2, err := svc.CommitVersion(ctx, artifact.ID, "hero rewrite")
	if err != nil {
		t.Fatalf("CommitVersion: %v", err)
	}
	if v2.Number != 2 {
		t.Fatalf("version number = %d, want 2", v2.Number)
	}

	versions, err := svc.Versions(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) != 2 || versions[0].Number != 1 || versions[1].Number != 2 {
		t.Fatalf("unexpected history: %+v", versions)
	}

	diff, err := svc.Diff(ctx, artifact.ID, 1, 2)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(diff) == 0 {
		t.Fatal("Diff between two different versions reported no changes")
	}

	if err := svc.Checkout(ctx, artifact.ID, 1); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	data, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if string(data) != "<h1>v1</h1>" {
		t.Fatalf("checkout did not restore v1: %q", data)
	}

	restored, err := svc.Get(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if restored.CurrentVersion != 1 {
		t.Fatalf("current version after checkout = %d, want 1", restored.CurrentVersion)
	}
	manifest, err := ReadManifest(filepath.Join(project, "designer", "landing"))
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("manifest version after checkout = %d, want 1", manifest.Version)
	}
}

// --- store ---

func TestNodeIndexReplaceAndQuery(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateArtifact(ctx, Artifact{ID: "dsg_1", Slug: "deck", Dir: "designer/deck", Kind: KindDeck}); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	nodes := []Node{
		{NodeID: "n1", Selector: "#title", Slide: 0, Box: Rect{Y: 10}, Styles: map[string]string{"color": "red"}},
		{NodeID: "n2", Selector: "#sub", Slide: 1, Box: Rect{Y: 20}},
	}
	if err := store.ReplaceNodes(ctx, "dsg_1", 1, nodes); err != nil {
		t.Fatalf("ReplaceNodes: %v", err)
	}

	all, err := store.ListNodes(ctx, "dsg_1", 1, -1)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d nodes, want 2", len(all))
	}

	slide1, err := store.ListNodes(ctx, "dsg_1", 1, 1)
	if err != nil {
		t.Fatalf("ListNodes(slide=1): %v", err)
	}
	if len(slide1) != 1 || slide1[0].NodeID != "n2" {
		t.Fatalf("slide filter returned %+v", slide1)
	}

	n1, err := store.GetNode(ctx, "dsg_1", 1, "n1")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if n1.Styles["color"] != "red" {
		t.Fatalf("styles lost in round trip: %+v", n1.Styles)
	}

	// A re-render replaces the whole index rather than merging into it.
	if err := store.ReplaceNodes(ctx, "dsg_1", 1, nodes[:1]); err != nil {
		t.Fatalf("ReplaceNodes (rerender): %v", err)
	}
	all, err = store.ListNodes(ctx, "dsg_1", 1, -1)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("stale nodes survived the re-render: %+v", all)
	}
}

func TestCritiqueRoundTripAndAttachment(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	artifact, err := svc.Create(ctx, CreateParams{Title: "Landing"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := svc.Store().AddCritique(ctx, Critique{
		ArtifactID: artifact.ID,
		Version:    1,
		Score:      7.5,
		Summary:    "contrast too low",
		Issues: []Issue{
			{Severity: SeverityWarning, NodeID: "n1", Message: "low contrast", Fix: "darken the text"},
		},
	}); err != nil {
		t.Fatalf("AddCritique: %v", err)
	}

	versions, err := svc.Versions(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if versions[0].Critique == nil {
		t.Fatal("version 1 has no critique attached")
	}
	if versions[0].Critique.Score != 7.5 || len(versions[0].Critique.Issues) != 1 {
		t.Fatalf("critique round trip: %+v", versions[0].Critique)
	}
	if versions[0].Critique.Issues[0].NodeID != "n1" {
		t.Fatalf("issue lost its node anchor: %+v", versions[0].Critique.Issues[0])
	}
}

func TestMissingRecordsReportNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.GetArtifact(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetArtifact error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetVersion(ctx, "nope", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetVersion error = %v, want ErrNotFound", err)
	}
	if err := store.SetCurrentVersion(ctx, "nope", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetCurrentVersion error = %v, want ErrNotFound", err)
	}
	if _, err := store.LatestCritique(ctx, "nope", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LatestCritique error = %v, want ErrNotFound", err)
	}
}

func TestDeleteArtifactCascadesButKeepsFiles(t *testing.T) {
	svc, project := newTestService(t)
	ctx := context.Background()

	artifact, err := svc.Create(ctx, CreateParams{Title: "Landing"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Delete(ctx, artifact.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(ctx, artifact.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("artifact still present: %v", err)
	}
	versions, err := svc.Store().ListVersions(ctx, artifact.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("versions survived the cascade: %+v", versions)
	}
	if _, err := os.Stat(filepath.Join(project, "designer", "landing", "index.html")); err != nil {
		t.Fatalf("deleting the metadata removed the user's files: %v", err)
	}
}
