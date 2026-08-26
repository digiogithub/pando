package design

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/snapshot"
)

// Snapshotter is the slice of the snapshot service the design package needs.
// Only the scoped operations are used: an artifact's history must never be able
// to restore or delete a file outside its own directory.
type Snapshotter interface {
	CreateScoped(ctx context.Context, sessionID, description, rootDir string) (snapshot.Snapshot, error)
	RevertScoped(ctx context.Context, snapshotID string) error
	Compare(ctx context.Context, snapshotID1, snapshotID2 string) ([]snapshot.DiffEntry, error)
}

// Service is the artifact and version API the design tools, the HTTP surface
// and the CLI all share.
type Service struct {
	store     *Store
	snaps     Snapshotter
	layout    Layout
	sessionID string
	renderer  *Renderer
	mirror    SystemMirror
}

// NewServiceFromConfig builds a design service from the loaded configuration,
// which is how the tools, the HTTP surface and the CLI all obtain one.
func NewServiceFromConfig(db *sql.DB, snaps Snapshotter, sessionID string) *Service {
	cfg := config.Get()
	layout := NewLayout(cfg.WorkingDir, cfg.Design.OutputDir, cfg.Design.SystemDir)
	return NewService(NewStore(db), snaps, layout, sessionID)
}

// NewService builds a design service. sessionID is recorded on artifacts and
// on the snapshots they take; it may be empty for CLI use.
func NewService(store *Store, snaps Snapshotter, layout Layout, sessionID string) *Service {
	return &Service{store: store, snaps: snaps, layout: layout, sessionID: sessionID}
}

// Layout exposes the resolved directory layout.
func (s *Service) Layout() Layout { return s.layout }

// WithMirror attaches the knowledge-base mirror used by the design system. It
// is optional: every surface works without one, they just do not publish what
// they extract.
func (s *Service) WithMirror(m SystemMirror) *Service {
	s.mirror = m
	return s
}

// CreateParams describes a new artifact. Only Title is mandatory.
type CreateParams struct {
	Title        string
	Kind         Kind
	Slug         string
	SkillID      string
	DesignSystem string
	ProjectID    string
	// Files seeds the artifact directory: project-relative-to-the-artifact
	// paths mapped to their content. A scaffold normally provides at least the
	// entry document; when it is empty a minimal placeholder entry is written
	// so the artifact is renderable from version 1.
	Files map[string]string
	// Entry overrides the manifest entry document (default "index.html").
	Entry string
}

// Create materialises a new artifact: directory, seed files, manifest, metadata
// row and version 1 (a scoped snapshot of the fresh directory).
func (s *Service) Create(ctx context.Context, p CreateParams) (Artifact, error) {
	if p.Title == "" {
		return Artifact{}, errors.New("design: artifact title is required")
	}
	// A template both picks the kind and seeds the files, so it is resolved
	// before the kind is defaulted: creating a deck template as a web artifact
	// would silently drop its print styles.
	template, hasTemplate := BundledTemplate(p.SkillID)
	if hasTemplate && p.Kind == "" {
		p.Kind = template.Kind
	}
	if p.Kind == "" {
		p.Kind = KindWeb
	}
	if !ValidKind(p.Kind) {
		return Artifact{}, fmt.Errorf("design: unsupported artifact kind %q (v1 supports web, deck)", p.Kind)
	}
	if err := s.layout.EnsureRoot(); err != nil {
		return Artifact{}, err
	}

	slug := Slugify(p.Slug)
	if slug == "" {
		var err error
		if slug, err = s.layout.AvailableSlug(p.Title); err != nil {
			return Artifact{}, err
		}
	}

	relDir := s.layout.RelDir(slug)
	absDir, err := s.layout.AbsDir(relDir)
	if err != nil {
		return Artifact{}, err
	}
	if _, err := os.Stat(absDir); err == nil {
		return Artifact{}, fmt.Errorf("design: directory %s already exists", relDir)
	} else if !os.IsNotExist(err) {
		return Artifact{}, fmt.Errorf("design: stat %s: %w", relDir, err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("design: create %s: %w", relDir, err)
	}

	id := NewArtifactID()
	manifest := NewManifest(id, p.Kind, p.Title)
	manifest.Skill = p.SkillID
	manifest.DesignSystem = p.DesignSystem
	if p.Entry != "" {
		manifest.Entry = p.Entry
	}

	if hasTemplate {
		if template.Viewport.W > 0 && template.Viewport.H > 0 {
			manifest.Preview.Viewport = template.Viewport
		}
	}

	files := p.Files
	if len(files) == 0 && hasTemplate {
		scaffold, err := Scaffold(p.SkillID, p.Title)
		if err != nil {
			return Artifact{}, err
		}
		if len(scaffold) > 0 {
			files = scaffold
		}
	}
	if len(files) == 0 {
		files = map[string]string{manifest.Entry: placeholderEntry(p.Kind, p.Title)}
	}
	if _, ok := files[manifest.Entry]; !ok {
		// A scaffold that does not carry the manifest entry would produce an
		// artifact nothing can render, so the placeholder fills the gap rather
		// than the creation failing.
		files[manifest.Entry] = placeholderEntry(p.Kind, p.Title)
	}
	if err := writeSeedFiles(absDir, files); err != nil {
		return Artifact{}, err
	}
	if err := WriteManifest(absDir, manifest); err != nil {
		return Artifact{}, err
	}

	artifact := Artifact{
		ID:             id,
		SessionID:      s.sessionID,
		ProjectID:      p.ProjectID,
		Title:          p.Title,
		Slug:           slug,
		Dir:            relDir,
		Kind:           p.Kind,
		SkillID:        p.SkillID,
		DesignSystemID: p.DesignSystem,
		CreatedAt:      time.Now(),
	}
	if artifact, err = s.store.CreateArtifact(ctx, artifact); err != nil {
		return Artifact{}, err
	}

	if _, err := s.CommitVersion(ctx, artifact.ID, "initial version"); err != nil {
		return Artifact{}, err
	}
	artifact.CurrentVersion = 1
	s.publish(EventCreated, Event{
		ArtifactID:   artifact.ID,
		Title:        artifact.Title,
		Slug:         artifact.Slug,
		ArtifactKind: artifact.Kind,
		Version:      1,
	})
	return artifact, nil
}

// Get returns one artifact.
func (s *Service) Get(ctx context.Context, id string) (Artifact, error) {
	return s.store.GetArtifact(ctx, id)
}

// List returns artifacts newest first. Pass sessionOnly to restrict the result
// to the current session.
func (s *Service) List(ctx context.Context, sessionOnly bool) ([]Artifact, error) {
	sessionID := ""
	if sessionOnly {
		sessionID = s.sessionID
	}
	return s.store.ListArtifacts(ctx, sessionID)
}

// AbsDir returns the absolute directory of an artifact.
func (s *Service) AbsDir(a Artifact) (string, error) {
	return s.layout.AbsDir(a.Dir)
}

// CommitVersion snapshots the artifact directory and records it as the next
// version. The manifest is rewritten so a checkout of the directory alone still
// reports the right version number.
func (s *Service) CommitVersion(ctx context.Context, artifactID, summary string) (Version, error) {
	artifact, err := s.store.GetArtifact(ctx, artifactID)
	if err != nil {
		return Version{}, err
	}
	absDir, err := s.layout.AbsDir(artifact.Dir)
	if err != nil {
		return Version{}, err
	}

	number := artifact.CurrentVersion + 1
	snap, err := s.snaps.CreateScoped(ctx, s.sessionID,
		fmt.Sprintf("design %s v%d: %s", artifact.Slug, number, summary), absDir)
	if err != nil {
		return Version{}, fmt.Errorf("design: snapshot %s v%d: %w", artifact.Slug, number, err)
	}

	version := Version{
		ArtifactID: artifactID,
		Number:     number,
		SnapshotID: snap.ID,
		Summary:    summary,
		CreatedAt:  time.Now(),
	}
	if err := s.store.AddVersion(ctx, version); err != nil {
		return Version{}, err
	}
	if err := s.syncManifestVersion(absDir, number); err != nil {
		return Version{}, err
	}
	s.publish(EventVersion, Event{
		ArtifactID:   artifact.ID,
		Title:        artifact.Title,
		Slug:         artifact.Slug,
		ArtifactKind: artifact.Kind,
		Version:      number,
		Summary:      summary,
	})
	return version, nil
}

// Versions returns the full history of an artifact, each entry carrying its
// last critique when there is one.
func (s *Service) Versions(ctx context.Context, artifactID string) ([]Version, error) {
	// An unknown artifact must be reported as missing, not as an artifact with
	// no history: an empty list is a legitimate answer and would hide the typo.
	if _, err := s.store.GetArtifact(ctx, artifactID); err != nil {
		return nil, err
	}
	versions, err := s.store.ListVersions(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	for i := range versions {
		critique, err := s.store.LatestCritique(ctx, artifactID, versions[i].Number)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		versions[i].Critique = &critique
	}
	return versions, nil
}

// Checkout restores the artifact directory to a previous version. The revert is
// scoped to that directory, so unrelated files are never touched, and the
// current state is snapshotted first by RevertScoped.
func (s *Service) Checkout(ctx context.Context, artifactID string, number int) error {
	version, err := s.store.GetVersion(ctx, artifactID, number)
	if err != nil {
		return err
	}
	if version.SnapshotID == "" {
		return fmt.Errorf("design: version %s v%d has no snapshot to restore", artifactID, number)
	}
	if err := s.snaps.RevertScoped(ctx, version.SnapshotID); err != nil {
		return fmt.Errorf("design: checkout %s v%d: %w", artifactID, number, err)
	}
	if err := s.store.SetCurrentVersion(ctx, artifactID, number); err != nil {
		return err
	}

	artifact, err := s.store.GetArtifact(ctx, artifactID)
	if err != nil {
		return err
	}
	absDir, err := s.layout.AbsDir(artifact.Dir)
	if err != nil {
		return err
	}
	return s.syncManifestVersion(absDir, number)
}

// Diff compares two versions of an artifact.
func (s *Service) Diff(ctx context.Context, artifactID string, from, to int) ([]snapshot.DiffEntry, error) {
	fromVersion, err := s.store.GetVersion(ctx, artifactID, from)
	if err != nil {
		return nil, err
	}
	toVersion, err := s.store.GetVersion(ctx, artifactID, to)
	if err != nil {
		return nil, err
	}
	return s.snaps.Compare(ctx, fromVersion.SnapshotID, toVersion.SnapshotID)
}

// Delete drops the metadata of an artifact. The files stay on disk: they belong
// to the user's repository, and removing them is an explicit, separate act.
func (s *Service) Delete(ctx context.Context, artifactID string) error {
	return s.store.DeleteArtifact(ctx, artifactID)
}

// Store exposes the metadata store for the node index and critiques, which the
// renderer and the critic loop own.
func (s *Service) Store() *Store { return s.store }

// syncManifestVersion keeps pando-design.json in step with the recorded
// version. A missing manifest is not fatal: the artifact directory is the
// user's, and they may have removed it.
func (s *Service) syncManifestVersion(absDir string, number int) error {
	manifest, err := ReadManifest(absDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if manifest.Version == number {
		return nil
	}
	manifest.Version = number
	return WriteManifest(absDir, manifest)
}

// writeSeedFiles writes the scaffold of a new artifact, rejecting any path that
// escapes the artifact directory.
func writeSeedFiles(absDir string, files map[string]string) error {
	for name, content := range files {
		cleaned := filepath.Clean(filepath.FromSlash(name))
		if filepath.IsAbs(cleaned) || cleaned == ".." || hasParentPrefix(cleaned) {
			return fmt.Errorf("design: seed file %q escapes the artifact directory", name)
		}
		target := filepath.Join(absDir, cleaned)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("design: create dir for %s: %w", name, err)
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return fmt.Errorf("design: write seed file %s: %w", name, err)
		}
	}
	return nil
}

func hasParentPrefix(cleaned string) bool {
	return len(cleaned) >= 3 && cleaned[:3] == ".."+string(filepath.Separator)
}
