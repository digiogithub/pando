package design

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/snapshot"
)

// ErrNoProvider is returned by the tools when the design subsystem was never
// wired, which happens in stripped-down entry points that run without a
// database.
var ErrNoProvider = errors.New("design: the design subsystem is not available in this process")

// ErrSchemaOutdated reports a database created before the design migrations ran.
var ErrSchemaOutdated = errors.New("design: database schema is outdated; the design tables are missing; restart Pando so pending migrations run")

// Provider owns the process-wide pieces of the Design Studio — the database
// handle, the snapshot service and the single headless browser — and hands out
// cheap per-session Service values.
//
// The renderer is shared on purpose: starting a browser costs far more than a
// tool call should, and every design surface (tools, HTTP preview, CLI) renders
// through the same one.
type Provider struct {
	db    *sql.DB
	snaps Snapshotter

	mu       sync.Mutex
	renderer *Renderer
	mirror   SystemMirror
}

// NewProvider builds a provider over an open database. The snapshot service is
// created here because artifact versions are directory-scoped snapshots.
func NewProvider(db *sql.DB) (*Provider, error) {
	if db == nil {
		return nil, errors.New("design: nil database")
	}
	if err := ensureSchema(db); err != nil {
		return nil, err
	}
	snaps, err := snapshot.NewService()
	if err != nil {
		return nil, err
	}
	return &Provider{db: db, snaps: snaps}, nil
}

func ensureSchema(db *sql.DB) error {
	var exists int
	err := db.QueryRow(`SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'design_artifacts'`).Scan(&exists)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrSchemaOutdated
	case err != nil:
		return fmt.Errorf("design: check schema: %w", err)
	default:
		return nil
	}
}

// NewProviderWith builds a provider over an explicit snapshotter, which is what
// tests use.
func NewProviderWith(db *sql.DB, snaps Snapshotter) *Provider {
	return &Provider{db: db, snaps: snaps}
}

// SetMirror attaches the knowledge-base mirror handed to every service the
// provider creates. Wired at start-up when the knowledge base is available.
func (p *Provider) SetMirror(m SystemMirror) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mirror = m
}

// Service returns a design service bound to a session. The returned value is
// cheap: it shares the store, the snapshotter and the renderer.
func (p *Provider) Service(sessionID string) *Service {
	svc := NewServiceFromConfig(p.db, p.snaps, sessionID)
	svc = svc.WithRenderer(p.renderForConfig())
	p.mu.Lock()
	mirror := p.mirror
	p.mu.Unlock()
	if mirror != nil {
		svc = svc.WithMirror(mirror)
	}
	return svc
}

// renderForConfig lazily creates the shared renderer.
func (p *Provider) renderForConfig() *Renderer {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.renderer != nil {
		return p.renderer
	}
	cfg := config.Get()
	layout := NewLayout(cfg.WorkingDir, cfg.Design.OutputDir, cfg.Design.SystemDir)
	p.renderer = NewRenderer(layout, BrowserOptionsFromConfig())
	return p.renderer
}

// Close releases the shared browser.
func (p *Provider) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.renderer != nil {
		p.renderer.Close()
		p.renderer = nil
	}
}

// defaultProvider is set once during application start-up. The tools resolve it
// on every call rather than capturing a service, so a tool constructed before
// the provider exists still works.
var defaultProvider atomic.Pointer[Provider]

// SetDefaultProvider installs the process-wide provider.
func SetDefaultProvider(p *Provider) {
	defaultProvider.Store(p)
	logging.Debug("design: provider installed")
}

// DefaultProvider returns the process-wide provider, or nil when the design
// subsystem was never wired.
func DefaultProvider() *Provider { return defaultProvider.Load() }

// ServiceFor returns a session-bound service from the default provider.
func ServiceFor(sessionID string) (*Service, error) {
	p := DefaultProvider()
	if p == nil {
		return nil, ErrNoProvider
	}
	return p.Service(sessionID), nil
}

// CloseDefaultProvider shuts the shared browser down at application exit.
func CloseDefaultProvider() {
	if p := defaultProvider.Swap(nil); p != nil {
		p.Close()
	}
}
