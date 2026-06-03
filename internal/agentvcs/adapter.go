package agentvcs

import "context"

// Adapter bridges the session package's SnapshotCreator interface with the
// agent-vcs Service. It maps snapshot types to agent-vcs operations:
//   - "start" → NewChange (initial commit for the session)
//   - "end"   → Record (final delta commit)
//   - other   → Record (intermediate delta commit)
type Adapter struct {
	service Service
}

// NewAdapter creates an Adapter wrapping the given Service.
func NewAdapter(svc Service) *Adapter {
	return &Adapter{service: svc}
}

// CreateSessionSnapshot satisfies session.SnapshotCreator.
func (a *Adapter) CreateSessionSnapshot(ctx context.Context, sessionID, snapshotType, description string) error {
	switch snapshotType {
	case "start":
		_, err := a.service.NewChange(ctx, sessionID, description)
		return err
	default:
		_, err := a.service.Record(ctx, sessionID, description)
		return err
	}
}

// Service returns the underlying agent-vcs Service for direct access.
func (a *Adapter) Service() Service {
	return a.service
}
