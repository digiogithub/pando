package design

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/digiogithub/pando/internal/ipc/dbproxy"
)

// ErrNotFound is returned when an artifact, version or node does not exist.
var ErrNotFound = errors.New("design: not found")

// ErrNoIndex reports that an artifact exists but carries no structure index for
// the requested version, which only a render can produce.
var ErrNoIndex = errors.New("design: no structure index")

// Store persists design metadata. The artifact files and their history live on
// disk (working tree + scoped snapshots); this only holds what is needed to
// list, resolve and navigate them.
//
// Reads use db directly. Writes go through w, which on an IPC secondary runs
// them directly first and forwards them to the primary on lock contention;
// multi-statement writes (AddVersion, ReplaceNodes) are sent as one batch and
// run in one transaction on whichever side executes them.
type Store struct {
	db *sql.DB
	w  *dbproxy.SQLWriter
}

// NewStore wraps an open database handle. Writes follow the IPC topology the
// pool is bound to (dbproxy.BindPool), if any.
func NewStore(db *sql.DB) *Store {
	return NewStoreWithProxy(db, nil)
}

// NewStoreWithProxy wraps db and routes writes through proxy (nil: direct, or
// the pool's bound proxy).
func NewStoreWithProxy(db *sql.DB, proxy *dbproxy.DBProxy) *Store {
	return &Store{db: db, w: dbproxy.NewSQLWriter(db, proxy)}
}

// Registered write statements (see dbproxy.RegisterStatement).
var (
	stmtInsertArtifact = dbproxy.RegisterStatement("design.insert_artifact", insertArtifactSQL)
	stmtUpdateArtifact = dbproxy.RegisterStatement("design.update_artifact", `
UPDATE design_artifacts
SET title = ?, kind = ?, skill_id = ?, design_system = ?, current_version = ?, updated_at = ?
WHERE id = ?`)
	stmtDeleteArtifact = dbproxy.RegisterStatement("design.delete_artifact",
		`DELETE FROM design_artifacts WHERE id = ?`)
	stmtInsertVersion = dbproxy.RegisterStatement("design.insert_version", `
INSERT INTO design_versions (artifact_id, number, snapshot_id, summary, created_at)
VALUES (?, ?, ?, ?, ?)`)
	stmtSetCurrentVersion = dbproxy.RegisterStatement("design.set_current_version", `
UPDATE design_artifacts SET current_version = ?, updated_at = ? WHERE id = ?`)
	stmtClearNodes = dbproxy.RegisterStatement("design.clear_nodes",
		`DELETE FROM design_nodes WHERE artifact_id = ? AND version = ?`)
	stmtInsertNode = dbproxy.RegisterStatement("design.insert_node", `
INSERT INTO design_nodes (
    artifact_id, version, node_id, parent_id, selector, role, text, slide,
    box_x, box_y, box_w, box_h, styles
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	stmtInsertCritique = dbproxy.RegisterStatement("design.insert_critique", `
INSERT INTO design_critiques (id, artifact_id, version, score, summary, issues, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`)
)

// --- artifacts ---

const insertArtifactSQL = `
INSERT INTO design_artifacts (
    id, session_id, project_id, title, slug, dir, kind, skill_id, design_system,
    current_version, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

// CreateArtifact inserts a new artifact row.
func (s *Store) CreateArtifact(ctx context.Context, a Artifact) (Artifact, error) {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	a.UpdatedAt = a.CreatedAt
	_, err := s.w.Exec(ctx, stmtInsertArtifact,
		a.ID, a.SessionID, a.ProjectID, a.Title, a.Slug, a.Dir, string(a.Kind),
		a.SkillID, a.DesignSystemID, a.CurrentVersion,
		a.CreatedAt.Unix(), a.UpdatedAt.Unix(),
	)
	if err != nil {
		return Artifact{}, fmt.Errorf("design: insert artifact %s: %w", a.ID, err)
	}
	return a, nil
}

const artifactColumns = `
id, session_id, project_id, title, slug, dir, kind, skill_id, design_system,
current_version, created_at, updated_at
`

func scanArtifact(sc interface{ Scan(...any) error }) (Artifact, error) {
	var (
		a       Artifact
		kind    string
		created int64
		updated int64
	)
	if err := sc.Scan(&a.ID, &a.SessionID, &a.ProjectID, &a.Title, &a.Slug, &a.Dir,
		&kind, &a.SkillID, &a.DesignSystemID, &a.CurrentVersion, &created, &updated); err != nil {
		return Artifact{}, err
	}
	a.Kind = Kind(kind)
	a.CreatedAt = time.Unix(created, 0)
	a.UpdatedAt = time.Unix(updated, 0)
	return a, nil
}

// GetArtifact returns one artifact by id.
func (s *Store) GetArtifact(ctx context.Context, id string) (Artifact, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+artifactColumns+` FROM design_artifacts WHERE id = ?`, id)
	a, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, fmt.Errorf("%w: artifact %s", ErrNotFound, id)
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("design: get artifact %s: %w", id, err)
	}
	return a, nil
}

// GetArtifactByDir returns the artifact stored at a project-relative directory.
func (s *Store) GetArtifactByDir(ctx context.Context, dir string) (Artifact, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+artifactColumns+` FROM design_artifacts WHERE dir = ?`, dir)
	a, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, fmt.Errorf("%w: artifact dir %s", ErrNotFound, dir)
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("design: get artifact by dir %s: %w", dir, err)
	}
	return a, nil
}

// ListArtifacts returns artifacts newest first. An empty sessionID lists all of
// them: artifacts outlive the session that created them.
func (s *Store) ListArtifacts(ctx context.Context, sessionID string) ([]Artifact, error) {
	query := `SELECT ` + artifactColumns + ` FROM design_artifacts`
	args := []any{}
	if sessionID != "" {
		query += ` WHERE session_id = ?`
		args = append(args, sessionID)
	}
	query += ` ORDER BY updated_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("design: list artifacts: %w", err)
	}
	defer rows.Close()

	artifacts := make([]Artifact, 0)
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, fmt.Errorf("design: scan artifact: %w", err)
		}
		artifacts = append(artifacts, a)
	}
	return artifacts, rows.Err()
}

// UpdateArtifact persists the mutable fields of an artifact and bumps
// updated_at.
func (s *Store) UpdateArtifact(ctx context.Context, a Artifact) error {
	a.UpdatedAt = time.Now()
	n, err := s.w.Exec(ctx, stmtUpdateArtifact,
		a.Title, string(a.Kind), a.SkillID, a.DesignSystemID, a.CurrentVersion, a.UpdatedAt.Unix(), a.ID)
	if err != nil {
		return fmt.Errorf("design: update artifact %s: %w", a.ID, err)
	}
	return requireAffected(n, "artifact "+a.ID)
}

// DeleteArtifact removes an artifact and, by cascade, its versions, nodes and
// critiques. The files on disk are never touched: they belong to the user.
func (s *Store) DeleteArtifact(ctx context.Context, id string) error {
	n, err := s.w.Exec(ctx, stmtDeleteArtifact, id)
	if err != nil {
		return fmt.Errorf("design: delete artifact %s: %w", id, err)
	}
	return requireAffected(n, "artifact "+id)
}

// --- versions ---

// AddVersion records an iteration and makes it the artifact's current version.
func (s *Store) AddVersion(ctx context.Context, v Version) error {
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Now()
	}
	// One transaction: the version row and the artifact's pointer to it.
	if _, err := s.w.ExecBatch(ctx,
		stmtInsertVersion.With(v.ArtifactID, v.Number, v.SnapshotID, v.Summary, v.CreatedAt.Unix()),
		stmtSetCurrentVersion.With(v.Number, time.Now().Unix(), v.ArtifactID),
	); err != nil {
		return fmt.Errorf("design: add version %s v%d: %w", v.ArtifactID, v.Number, err)
	}
	return nil
}

// GetVersion returns one version of an artifact.
func (s *Store) GetVersion(ctx context.Context, artifactID string, number int) (Version, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT artifact_id, number, snapshot_id, summary, created_at
FROM design_versions WHERE artifact_id = ? AND number = ?`, artifactID, number)

	var (
		v       Version
		created int64
	)
	err := row.Scan(&v.ArtifactID, &v.Number, &v.SnapshotID, &v.Summary, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, fmt.Errorf("%w: %s v%d", ErrNotFound, artifactID, number)
	}
	if err != nil {
		return Version{}, fmt.Errorf("design: get version %s v%d: %w", artifactID, number, err)
	}
	v.CreatedAt = time.Unix(created, 0)
	return v, nil
}

// ListVersions returns every version of an artifact, oldest first.
func (s *Store) ListVersions(ctx context.Context, artifactID string) ([]Version, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT artifact_id, number, snapshot_id, summary, created_at
FROM design_versions WHERE artifact_id = ? ORDER BY number ASC`, artifactID)
	if err != nil {
		return nil, fmt.Errorf("design: list versions %s: %w", artifactID, err)
	}
	defer rows.Close()

	versions := make([]Version, 0)
	for rows.Next() {
		var (
			v       Version
			created int64
		)
		if err := rows.Scan(&v.ArtifactID, &v.Number, &v.SnapshotID, &v.Summary, &created); err != nil {
			return nil, fmt.Errorf("design: scan version: %w", err)
		}
		v.CreatedAt = time.Unix(created, 0)
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// SetCurrentVersion points the artifact at an existing version, as a checkout
// does. It does not create history.
func (s *Store) SetCurrentVersion(ctx context.Context, artifactID string, number int) error {
	n, err := s.w.Exec(ctx, stmtSetCurrentVersion, number, time.Now().Unix(), artifactID)
	if err != nil {
		return fmt.Errorf("design: set current version %s: %w", artifactID, err)
	}
	return requireAffected(n, "artifact "+artifactID)
}

// --- nodes ---

// ReplaceNodes swaps the whole structure index of one artifact version. The
// index is a render product, so it is rebuilt wholesale rather than merged.
func (s *Store) ReplaceNodes(ctx context.Context, artifactID string, version int, nodes []Node) error {
	// One transaction: clear the version's index, then insert every node
	// (the writer prepares the repeated insert once).
	calls := make([]dbproxy.StmtCall, 0, len(nodes)+1)
	calls = append(calls, stmtClearNodes.With(artifactID, version))
	for _, n := range nodes {
		styles := "{}"
		if len(n.Styles) > 0 {
			encoded, err := json.Marshal(n.Styles)
			if err != nil {
				return fmt.Errorf("design: encode styles for node %s: %w", n.NodeID, err)
			}
			styles = string(encoded)
		}
		calls = append(calls, stmtInsertNode.With(
			artifactID, version, n.NodeID, n.ParentID, n.Selector, n.Role, n.Text, n.Slide,
			n.Box.X, n.Box.Y, n.Box.W, n.Box.H, styles))
	}
	if _, err := s.w.ExecBatch(ctx, calls...); err != nil {
		return fmt.Errorf("design: replace nodes %s v%d: %w", artifactID, version, err)
	}
	return nil
}

// ListNodes returns the structure index of one version. A slide >= 0 narrows
// the result to that slide; pass -1 for every node.
func (s *Store) ListNodes(ctx context.Context, artifactID string, version, slide int) ([]Node, error) {
	query := `
SELECT artifact_id, version, node_id, parent_id, selector, role, text, slide,
       box_x, box_y, box_w, box_h, styles
FROM design_nodes WHERE artifact_id = ? AND version = ?`
	args := []any{artifactID, version}
	if slide >= 0 {
		query += ` AND slide = ?`
		args = append(args, slide)
	}
	query += ` ORDER BY slide ASC, box_y ASC, box_x ASC, node_id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("design: list nodes %s v%d: %w", artifactID, version, err)
	}
	defer rows.Close()

	nodes := make([]Node, 0)
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// GetNode resolves a single node of a version, which is what a design://<id>
// selection turns into.
func (s *Store) GetNode(ctx context.Context, artifactID string, version int, nodeID string) (Node, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT artifact_id, version, node_id, parent_id, selector, role, text, slide,
       box_x, box_y, box_w, box_h, styles
FROM design_nodes WHERE artifact_id = ? AND version = ? AND node_id = ?`,
		artifactID, version, nodeID)

	n, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, fmt.Errorf("%w: node %s in %s v%d", ErrNotFound, nodeID, artifactID, version)
	}
	if err != nil {
		return Node{}, err
	}
	return n, nil
}

func scanNode(sc interface{ Scan(...any) error }) (Node, error) {
	var (
		n      Node
		styles string
	)
	if err := sc.Scan(&n.ArtifactID, &n.Version, &n.NodeID, &n.ParentID, &n.Selector,
		&n.Role, &n.Text, &n.Slide, &n.Box.X, &n.Box.Y, &n.Box.W, &n.Box.H, &styles); err != nil {
		return Node{}, err
	}
	if styles != "" && styles != "{}" {
		if err := json.Unmarshal([]byte(styles), &n.Styles); err != nil {
			return Node{}, fmt.Errorf("design: decode styles for node %s: %w", n.NodeID, err)
		}
	}
	return n, nil
}

// --- critiques ---

// AddCritique records a critic pass over a version.
func (s *Store) AddCritique(ctx context.Context, c Critique) (Critique, error) {
	if c.ID == "" {
		c.ID = NewCritiqueID()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	issues := "[]"
	if len(c.Issues) > 0 {
		encoded, err := json.Marshal(c.Issues)
		if err != nil {
			return Critique{}, fmt.Errorf("design: encode critique issues: %w", err)
		}
		issues = string(encoded)
	}
	if _, err := s.w.Exec(ctx, stmtInsertCritique,
		c.ID, c.ArtifactID, c.Version, c.Score, c.Summary, issues, c.CreatedAt.Unix()); err != nil {
		return Critique{}, fmt.Errorf("design: insert critique %s: %w", c.ID, err)
	}
	return c, nil
}

// LatestCritique returns the most recent critic pass over a version, or
// ErrNotFound when the version has never been critiqued.
func (s *Store) LatestCritique(ctx context.Context, artifactID string, version int) (Critique, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, artifact_id, version, score, summary, issues, created_at
FROM design_critiques WHERE artifact_id = ? AND version = ?
ORDER BY created_at DESC, rowid DESC LIMIT 1`, artifactID, version)

	var (
		c       Critique
		issues  string
		created int64
	)
	err := row.Scan(&c.ID, &c.ArtifactID, &c.Version, &c.Score, &c.Summary, &issues, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return Critique{}, fmt.Errorf("%w: critique for %s v%d", ErrNotFound, artifactID, version)
	}
	if err != nil {
		return Critique{}, fmt.Errorf("design: get critique %s v%d: %w", artifactID, version, err)
	}
	c.CreatedAt = time.Unix(created, 0)
	if issues != "" && issues != "[]" {
		if err := json.Unmarshal([]byte(issues), &c.Issues); err != nil {
			return Critique{}, fmt.Errorf("design: decode critique issues %s: %w", c.ID, err)
		}
	}
	return c, nil
}

// requireAffected turns a no-op UPDATE/DELETE into ErrNotFound.
func requireAffected(n int64, what string) error {
	if n == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, what)
	}
	return nil
}
