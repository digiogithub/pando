package app

import (
	"context"
	"errors"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/project"
)

// fakeProjectService is a minimal in-memory project.Service for testing the
// project-reference resolver adapter (item B1). Only Get/GetByPath/List are
// meaningful; the mutating methods are no-ops.
type fakeProjectService struct {
	projects []project.Project
}

func (f *fakeProjectService) Create(ctx context.Context, name, path string) (*project.Project, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeProjectService) Get(ctx context.Context, id string) (*project.Project, error) {
	for i := range f.projects {
		if f.projects[i].ID == id {
			p := f.projects[i]
			return &p, nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeProjectService) GetByPath(ctx context.Context, path string) (*project.Project, error) {
	for i := range f.projects {
		if f.projects[i].Path == path {
			p := f.projects[i]
			return &p, nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeProjectService) List(ctx context.Context) ([]project.Project, error) {
	return f.projects, nil
}

func (f *fakeProjectService) UpdateStatus(ctx context.Context, id, status string, pid, port int) error {
	return nil
}
func (f *fakeProjectService) MarkInitialized(ctx context.Context, id string) error { return nil }
func (f *fakeProjectService) TouchLastOpened(ctx context.Context, id string) error { return nil }
func (f *fakeProjectService) Rename(ctx context.Context, id, name string) error    { return nil }
func (f *fakeProjectService) Delete(ctx context.Context, id string) error          { return nil }

func TestProjectRefResolverByID(t *testing.T) {
	dir := t.TempDir()
	svc := &fakeProjectService{projects: []project.Project{
		{ID: "proj-1", Name: "Alpha", Path: config.CanonicalProjectPath(dir)},
	}}
	r := makeProjectRefResolver(svc)
	if r == nil {
		t.Fatal("makeProjectRefResolver returned nil for non-nil service")
	}

	ref, ok := r.ResolveProjectRef(context.Background(), "proj-1")
	if !ok {
		t.Fatal("expected to resolve by id")
	}
	if ref.ID != "proj-1" || ref.Name != "Alpha" {
		t.Fatalf("got %+v, want id=proj-1 name=Alpha", ref)
	}
}

func TestProjectRefResolverByPath(t *testing.T) {
	dir := t.TempDir()
	canonical := config.CanonicalProjectPath(dir)
	svc := &fakeProjectService{projects: []project.Project{
		{ID: "proj-1", Name: "Alpha", Path: canonical},
	}}
	r := makeProjectRefResolver(svc)

	// Pass the raw dir; the adapter canonicalizes before GetByPath.
	ref, ok := r.ResolveProjectRef(context.Background(), dir)
	if !ok {
		t.Fatalf("expected to resolve by path %q", dir)
	}
	if ref.ID != "proj-1" {
		t.Fatalf("got id %q, want proj-1", ref.ID)
	}
}

func TestProjectRefResolverByNameCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	svc := &fakeProjectService{projects: []project.Project{
		{ID: "proj-1", Name: "Alpha", Path: config.CanonicalProjectPath(dir)},
	}}
	r := makeProjectRefResolver(svc)

	ref, ok := r.ResolveProjectRef(context.Background(), "  aLpHa  ")
	if !ok {
		t.Fatal("expected to resolve by case-insensitive trimmed name")
	}
	if ref.ID != "proj-1" {
		t.Fatalf("got id %q, want proj-1", ref.ID)
	}
}

func TestProjectRefResolverMiss(t *testing.T) {
	svc := &fakeProjectService{projects: []project.Project{
		{ID: "proj-1", Name: "Alpha", Path: "/tmp/alpha"},
	}}
	r := makeProjectRefResolver(svc)

	if _, ok := r.ResolveProjectRef(context.Background(), "nope"); ok {
		t.Fatal("expected miss for unknown reference")
	}
	if _, ok := r.ResolveProjectRef(context.Background(), ""); ok {
		t.Fatal("expected miss for empty reference")
	}

	refs := r.ListProjectRefs(context.Background())
	if len(refs) != 1 || refs[0].ID != "proj-1" {
		t.Fatalf("ListProjectRefs = %+v, want one proj-1", refs)
	}
}

func TestMakeProjectRefResolverNilService(t *testing.T) {
	if r := makeProjectRefResolver(nil); r != nil {
		t.Fatal("expected nil resolver for nil service")
	}
}
