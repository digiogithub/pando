package project

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/digiogithub/pando/internal/db"
	"github.com/google/uuid"
)

// Service defines operations for managing registered projects.
type Service interface {
	// Create registers a new project directory.
	// name defaults to filepath.Base(path) if empty.
	Create(ctx context.Context, name, path string) (*Project, error)

	// Get retrieves a project by ID.
	Get(ctx context.Context, id string) (*Project, error)

	// GetByPath retrieves a project by its directory path.
	GetByPath(ctx context.Context, path string) (*Project, error)

	// List returns all registered projects, newest first.
	List(ctx context.Context) ([]Project, error)

	// UpdateStatus updates the running state and optional process info.
	// pid and port should be 0 when not applicable.
	UpdateStatus(ctx context.Context, id, status string, pid, port int) error

	// MarkInitialized marks the project's config as having been initialized.
	MarkInitialized(ctx context.Context, id string) error

	// TouchLastOpened records the current time as last_opened for the project.
	TouchLastOpened(ctx context.Context, id string) error

	// Rename updates the display name of a project.
	Rename(ctx context.Context, id, name string) error

	// Delete removes a project from the registry (does NOT delete files).
	Delete(ctx context.Context, id string) error
}

// NewService creates a new project service backed by the given DB queries.
func NewService(q *db.Queries) Service {
	return &service{q: q}
}

type service struct {
	q *db.Queries
}

// Create registers a new project directory.
func (s *service) Create(ctx context.Context, name, path string) (*Project, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	if name == "" {
		name = filepath.Base(absPath)
	}

	row, err := s.q.CreateProject(ctx, db.CreateProjectParams{
		ID:          uuid.New().String(),
		Name:        name,
		Path:        absPath,
		Status:      StatusStopped,
		Initialized: 0,
		AcpPid:      sql.NullInt64{},
		AcpPort:     sql.NullInt64{},
		LastOpened:  sql.NullInt64{},
	})
	if err != nil {
		return nil, err
	}

	p := fromDB(row)
	return &p, nil
}

// Get retrieves a project by ID.
func (s *service) Get(ctx context.Context, id string) (*Project, error) {
	row, err := s.q.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	p := fromDB(row)
	return &p, nil
}

// GetByPath retrieves a project by its directory path.
func (s *service) GetByPath(ctx context.Context, path string) (*Project, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	row, err := s.q.GetProjectByPath(ctx, absPath)
	if err != nil {
		return nil, err
	}
	p := fromDB(row)
	return &p, nil
}

// List returns all registered projects ordered by last_opened, then created_at.
func (s *service) List(ctx context.Context) ([]Project, error) {
	rows, err := s.q.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	projects := make([]Project, len(rows))
	for i, row := range rows {
		projects[i] = fromDB(row)
	}
	return projects, nil
}

// UpdateStatus updates the running state and optional process info.
func (s *service) UpdateStatus(ctx context.Context, id, status string, pid, port int) error {
	var acpPid, acpPort sql.NullInt64
	if pid != 0 {
		acpPid = sql.NullInt64{Int64: int64(pid), Valid: true}
	}
	if port != 0 {
		acpPort = sql.NullInt64{Int64: int64(port), Valid: true}
	}
	return s.q.UpdateProjectStatus(ctx, db.UpdateProjectStatusParams{
		ID:      id,
		Status:  status,
		AcpPid:  acpPid,
		AcpPort: acpPort,
	})
}

// MarkInitialized marks the project's config as having been initialized.
func (s *service) MarkInitialized(ctx context.Context, id string) error {
	return s.q.MarkProjectInitialized(ctx, id)
}

// TouchLastOpened records the current time as last_opened for the project.
func (s *service) TouchLastOpened(ctx context.Context, id string) error {
	return s.q.UpdateProjectLastOpened(ctx, db.UpdateProjectLastOpenedParams{
		ID:         id,
		LastOpened: sql.NullInt64{Int64: time.Now().Unix(), Valid: true},
	})
}

// Rename updates the display name of a project.
func (s *service) Rename(ctx context.Context, id, name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	return s.q.UpdateProjectName(ctx, id, name)
}

// Delete removes a project from the registry (does NOT delete files on disk).
func (s *service) Delete(ctx context.Context, id string) error {
	return s.q.DeleteProject(ctx, id)
}

// fromDB converts a db.Project row to a domain Project struct.
func fromDB(row db.Project) Project {
	p := Project{
		ID:          row.ID,
		Name:        row.Name,
		Path:        row.Path,
		Status:      row.Status,
		Initialized: row.Initialized != 0,
		CreatedAt:   time.Unix(row.CreatedAt, 0),
		UpdatedAt:   time.Unix(row.UpdatedAt, 0),
	}
	if row.AcpPid.Valid {
		p.ACPPID = int(row.AcpPid.Int64)
	}
	if row.AcpPort.Valid {
		p.ACPPort = int(row.AcpPort.Int64)
	}
	if row.LastOpened.Valid {
		t := time.Unix(row.LastOpened.Int64, 0)
		p.LastOpened = &t
	}
	return p
}
