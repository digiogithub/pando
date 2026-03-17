// Package snapshot provides a service for capturing and comparing point-in-time
// snapshots of the working directory file system.
package snapshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/pubsub"
)

// Snapshot type constants.
const (
	SnapshotTypeStart  = "start"  // Created at session start
	SnapshotTypeEnd    = "end"    // Created at session end
	SnapshotTypeManual = "manual" // User-triggered
)

// Snapshot represents a point-in-time capture of the working directory.
type Snapshot struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	Type        string `json:"type"` // start, end, manual
	Description string `json:"description"`
	WorkingDir  string `json:"working_dir"`
	FileCount   int    `json:"file_count"`
	TotalSize   int64  `json:"total_size"`
	CreatedAt   int64  `json:"created_at"`
}

// SnapshotFile represents a single file captured within a snapshot.
type SnapshotFile struct {
	Path    string `json:"path"`
	Hash    string `json:"hash"`     // SHA256 hex digest
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
	IsDir   bool   `json:"is_dir"`
}

// Manifest combines a Snapshot header with its full file list.
type Manifest struct {
	Snapshot Snapshot       `json:"snapshot"`
	Files    []SnapshotFile `json:"files"`
}

// Service defines the public interface for the snapshot service.
type Service interface {
	pubsub.Suscriber[Snapshot]
	Create(ctx context.Context, sessionID, snapshotType, description string) (Snapshot, error)
	Get(ctx context.Context, id string) (Snapshot, error)
	GetManifest(ctx context.Context, id string) (Manifest, error)
	List(ctx context.Context) ([]Snapshot, error)
	ListBySession(ctx context.Context, sessionID string) ([]Snapshot, error)
	Delete(ctx context.Context, id string) error
	GetFileContent(ctx context.Context, snapshotID, fileHash string) ([]byte, error)
	Compare(ctx context.Context, snapshotID1, snapshotID2 string) ([]DiffEntry, error)
	Cleanup(ctx context.Context, maxAge int, maxCount int) error
}

// service is the concrete implementation of Service.
type service struct {
	*pubsub.Broker[Snapshot]
	storage *storage
	scanner *scanner
}

// NewService creates and returns a new snapshot Service.
// It reads the data directory from the application config and initialises
// both the file-based storage and the file-system scanner.
func NewService() (Service, error) {
	dataDir := config.Get().Data.Directory
	snapshotsDir := filepath.Join(dataDir, "snapshots")

	if err := os.MkdirAll(snapshotsDir, 0o755); err != nil {
		return nil, fmt.Errorf("snapshot: create snapshots dir: %w", err)
	}

	stor, err := newStorage(snapshotsDir)
	if err != nil {
		return nil, fmt.Errorf("snapshot: init storage: %w", err)
	}

	scan := newScanner()

	svc := &service{
		Broker:  pubsub.NewBroker[Snapshot](),
		storage: stor,
		scanner: scan,
	}

	logging.Debug("snapshot service initialised", "dir", snapshotsDir)
	return svc, nil
}

// Create scans the current working directory and persists a new snapshot.
func (s *service) Create(ctx context.Context, sessionID, snapshotType, description string) (Snapshot, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot: get working dir: %w", err)
	}

	logging.Debug("creating snapshot", "session_id", sessionID, "type", snapshotType, "dir", workingDir)

	files, err := s.scanner.Scan(workingDir)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot: scan working dir: %w", err)
	}

	var totalSize int64
	for _, f := range files {
		totalSize += f.Size
	}

	snap := Snapshot{
		ID:          uuid.New().String(),
		SessionID:   sessionID,
		Type:        snapshotType,
		Description: description,
		WorkingDir:  workingDir,
		FileCount:   len(files),
		TotalSize:   totalSize,
		CreatedAt:   time.Now().Unix(),
	}

	manifest := Manifest{
		Snapshot: snap,
		Files:    files,
	}

	if err := s.storage.SaveSnapshot(manifest); err != nil {
		return Snapshot{}, fmt.Errorf("snapshot: save snapshot: %w", err)
	}

	s.Publish(pubsub.CreatedEvent, snap)
	logging.Info("snapshot created", "id", snap.ID, "files", snap.FileCount, "size", snap.TotalSize)
	return snap, nil
}

// Get retrieves a snapshot header by its ID.
func (s *service) Get(_ context.Context, id string) (Snapshot, error) {
	manifest, err := s.storage.LoadManifest(id)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot: get %s: %w", id, err)
	}
	return manifest.Snapshot, nil
}

// GetManifest retrieves the full manifest (header + file list) for a snapshot.
func (s *service) GetManifest(_ context.Context, id string) (Manifest, error) {
	manifest, err := s.storage.LoadManifest(id)
	if err != nil {
		return Manifest{}, fmt.Errorf("snapshot: get manifest %s: %w", id, err)
	}
	return manifest, nil
}

// List returns headers for all stored snapshots.
func (s *service) List(_ context.Context) ([]Snapshot, error) {
	ids, err := s.storage.ListSnapshots()
	if err != nil {
		return nil, fmt.Errorf("snapshot: list: %w", err)
	}

	snapshots := make([]Snapshot, 0, len(ids))
	for _, id := range ids {
		m, err := s.storage.LoadManifest(id)
		if err != nil {
			logging.Error("snapshot: failed to load manifest during list", "id", id, "error", err)
			continue
		}
		snapshots = append(snapshots, m.Snapshot)
	}
	return snapshots, nil
}

// ListBySession returns all snapshot headers belonging to a specific session.
func (s *service) ListBySession(ctx context.Context, sessionID string) ([]Snapshot, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	var result []Snapshot
	for _, snap := range all {
		if snap.SessionID == sessionID {
			result = append(result, snap)
		}
	}
	return result, nil
}

// Delete removes a snapshot and publishes a DeletedEvent. Blobs are shared and
// are only removed by CleanOrphanBlobs.
func (s *service) Delete(_ context.Context, id string) error {
	manifest, err := s.storage.LoadManifest(id)
	if err != nil {
		return fmt.Errorf("snapshot: delete %s: load manifest: %w", id, err)
	}

	if err := s.storage.DeleteSnapshot(id); err != nil {
		return fmt.Errorf("snapshot: delete %s: %w", id, err)
	}

	s.Publish(pubsub.DeletedEvent, manifest.Snapshot)
	logging.Info("snapshot deleted", "id", id)
	return nil
}

// GetFileContent retrieves the raw (decompressed) content of a blob by its hash.
// The snapshotID is accepted for interface symmetry but the content-addressable
// store only needs the hash.
func (s *service) GetFileContent(_ context.Context, _ string, fileHash string) ([]byte, error) {
	data, err := s.storage.LoadBlob(fileHash)
	if err != nil {
		return nil, fmt.Errorf("snapshot: get file content (hash=%s): %w", fileHash, err)
	}
	return data, nil
}

// Compare produces a diff between two snapshots, listing added, modified, and
// deleted files.
func (s *service) Compare(_ context.Context, snapshotID1, snapshotID2 string) ([]DiffEntry, error) {
	m1, err := s.storage.LoadManifest(snapshotID1)
	if err != nil {
		return nil, fmt.Errorf("snapshot: compare load first (%s): %w", snapshotID1, err)
	}

	m2, err := s.storage.LoadManifest(snapshotID2)
	if err != nil {
		return nil, fmt.Errorf("snapshot: compare load second (%s): %w", snapshotID2, err)
	}

	return diffManifests(m1, m2), nil
}

// Cleanup removes old snapshots according to age (days) and count limits.
// If maxAge <= 0 the age limit is ignored; if maxCount <= 0 the count limit is
// ignored. Orphan blobs are collected afterwards.
func (s *service) Cleanup(ctx context.Context, maxAge int, maxCount int) error {
	all, err := s.List(ctx)
	if err != nil {
		return fmt.Errorf("snapshot: cleanup list: %w", err)
	}

	now := time.Now().Unix()
	var toDelete []string

	// Age-based pruning.
	if maxAge > 0 {
		cutoff := now - int64(maxAge)*86400
		for _, snap := range all {
			if snap.CreatedAt < cutoff {
				toDelete = append(toDelete, snap.ID)
			}
		}
	}

	// Count-based pruning: keep the newest maxCount entries.
	if maxCount > 0 && len(all) > maxCount {
		// all is in storage order; sort descending by CreatedAt to keep newest.
		sorted := make([]Snapshot, len(all))
		copy(sorted, all)
		sortSnapshotsByAge(sorted) // newest first
		for _, snap := range sorted[maxCount:] {
			toDelete = append(toDelete, snap.ID)
		}
	}

	// Deduplicate IDs.
	seen := make(map[string]struct{}, len(toDelete))
	for _, id := range toDelete {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if err := s.Delete(ctx, id); err != nil {
			logging.Error("snapshot: cleanup failed to delete", "id", id, "error", err)
		}
	}

	// Remove blobs that are no longer referenced by any manifest.
	if err := s.storage.CleanOrphanBlobs(); err != nil {
		logging.Error("snapshot: cleanup orphan blobs", "error", err)
	}

	logging.Info("snapshot cleanup complete", "removed", len(seen))
	return nil
}

// sortSnapshotsByAge sorts snapshots in descending CreatedAt order (newest first).
func sortSnapshotsByAge(snaps []Snapshot) {
	for i := 0; i < len(snaps)-1; i++ {
		for j := i + 1; j < len(snaps); j++ {
			if snaps[j].CreatedAt > snaps[i].CreatedAt {
				snaps[i], snaps[j] = snaps[j], snaps[i]
			}
		}
	}
}
