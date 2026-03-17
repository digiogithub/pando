package snapshot

import "context"

// Adapter implements session.SnapshotCreator using a snapshot Service.
// It bridges the session package (which uses the SnapshotCreator interface to
// avoid an import cycle) with the concrete snapshot.Service.
type Adapter struct {
	service Service
}

// NewAdapter wraps a Service in an Adapter that satisfies session.SnapshotCreator.
func NewAdapter(svc Service) *Adapter {
	return &Adapter{service: svc}
}

// CreateSessionSnapshot delegates to the underlying Service.Create method.
func (a *Adapter) CreateSessionSnapshot(ctx context.Context, sessionID, snapshotType, description string) error {
	_, err := a.service.Create(ctx, sessionID, snapshotType, description)
	return err
}
