package embedded

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/match"
	"github.com/google/go-containerregistry/pkg/v1/partial"
)

const metadataFileName = "pando-metadata.json"

// ErrImageNotCached is returned when an image is not present in the local store.
var ErrImageNotCached = errors.New("embedded image is not cached locally")

// StoreEntry describes one cached OCI image.
type StoreEntry struct {
	Ref        string    `json:"ref"`
	Digest     string    `json:"digest,omitempty"`
	Size       int64     `json:"size"`
	Path       string    `json:"path"`
	StoredAt   time.Time `json:"storedAt,omitempty"`
	AccessedAt time.Time `json:"accessedAt,omitempty"`
}

type imageMetadata struct {
	Ref        string    `json:"ref"`
	Digest     string    `json:"digest,omitempty"`
	Size       int64     `json:"size"`
	StoredAt   time.Time `json:"storedAt,omitempty"`
	AccessedAt time.Time `json:"accessedAt,omitempty"`
}

// ImageStore persists cached images using the OCI image layout spec.
type ImageStore struct {
	Root string
}

// Get returns a cached image by its normalized reference.
func (s *ImageStore) Get(ref string) (v1.Image, error) {
	ref, err := normalizeRefString(ref)
	if err != nil {
		return nil, err
	}

	root, err := s.ensureRoot()
	if err != nil {
		return nil, err
	}

	entryDir := filepath.Join(root, refCacheKey(ref))
	if _, err := os.Stat(entryDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrImageNotCached
		}
		return nil, fmt.Errorf("stat cached image %q: %w", ref, err)
	}

	path, err := layout.FromPath(entryDir)
	if err != nil {
		return nil, fmt.Errorf("open OCI layout for %q: %w", ref, err)
	}
	index, err := path.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("read OCI layout index for %q: %w", ref, err)
	}

	images, err := partial.FindImages(index, match.Name(ref))
	if err != nil {
		return nil, fmt.Errorf("locate cached image %q: %w", ref, err)
	}
	if len(images) == 0 {
		return nil, ErrImageNotCached
	}

	if err := s.touchMetadata(entryDir); err != nil {
		return nil, err
	}
	return images[0], nil
}

// Put stores img as a single-image OCI layout keyed by ref.
func (s *ImageStore) Put(ref string, img v1.Image) error {
	ref, err := normalizeRefString(ref)
	if err != nil {
		return err
	}

	root, err := s.ensureRoot()
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp(root, "write-*")
	if err != nil {
		return fmt.Errorf("create temporary OCI layout for %q: %w", ref, err)
	}

	finished := false
	defer func() {
		if !finished {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if _, err := layout.Write(tmpDir, empty.Index); err != nil {
		return fmt.Errorf("initialise OCI layout for %q: %w", ref, err)
	}
	layoutPath, err := layout.FromPath(tmpDir)
	if err != nil {
		return fmt.Errorf("open OCI layout for %q: %w", ref, err)
	}
	if err := layoutPath.AppendImage(img, layout.WithAnnotations(map[string]string{
		"org.opencontainers.image.ref.name": ref,
	})); err != nil {
		return fmt.Errorf("append OCI image %q: %w", ref, err)
	}

	digest, err := img.Digest()
	if err != nil {
		return fmt.Errorf("resolve image digest for %q: %w", ref, err)
	}
	size, err := dirSize(tmpDir)
	if err != nil {
		return fmt.Errorf("measure cached image %q: %w", ref, err)
	}

	meta := imageMetadata{
		Ref:        ref,
		Digest:     digest.String(),
		Size:       size,
		StoredAt:   time.Now(),
		AccessedAt: time.Now(),
	}
	if err := writeMetadata(tmpDir, meta); err != nil {
		return err
	}

	finalDir := filepath.Join(root, refCacheKey(ref))
	if err := os.RemoveAll(finalDir); err != nil {
		return fmt.Errorf("replace cached image %q: %w", ref, err)
	}
	if err := os.Rename(tmpDir, finalDir); err != nil {
		return fmt.Errorf("commit cached image %q: %w", ref, err)
	}

	finished = true
	return nil
}

// Delete removes a cached image reference from the local store.
func (s *ImageStore) Delete(ref string) error {
	ref, err := normalizeRefString(ref)
	if err != nil {
		return err
	}

	root, err := s.ensureRoot()
	if err != nil {
		return err
	}

	entryDir := filepath.Join(root, refCacheKey(ref))
	if err := os.RemoveAll(entryDir); err != nil {
		return fmt.Errorf("delete cached image %q: %w", ref, err)
	}
	return nil
}

// List returns all cached image entries sorted by most recent access.
func (s *ImageStore) List() ([]StoreEntry, error) {
	root, err := s.ensureRoot()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("list cached images: %w", err)
	}

	out := make([]StoreEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		item, err := s.readEntry(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}

	sort.Slice(out, func(i, j int) bool {
		left := lastTouched(out[i])
		right := lastTouched(out[j])
		if left.Equal(right) {
			return out[i].Ref < out[j].Ref
		}
		return left.After(right)
	})
	return out, nil
}

// TotalSize returns the total on-disk size of all cached images.
func (s *ImageStore) TotalSize() (int64, error) {
	entries, err := s.List()
	if err != nil {
		return 0, err
	}

	var total int64
	for _, entry := range entries {
		total += entry.Size
	}
	return total, nil
}

func (s *ImageStore) ensureRoot() (string, error) {
	root, err := ResolveCacheDir(s.Root)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create image cache root %q: %w", root, err)
	}
	return root, nil
}

func (s *ImageStore) readEntry(entryDir string) (StoreEntry, error) {
	meta, err := readMetadata(entryDir)
	if err != nil {
		return StoreEntry{}, err
	}

	if meta.Size == 0 {
		size, err := dirSize(entryDir)
		if err != nil {
			return StoreEntry{}, fmt.Errorf("measure cached image %q: %w", meta.Ref, err)
		}
		meta.Size = size
	}

	return StoreEntry{
		Ref:        meta.Ref,
		Digest:     meta.Digest,
		Size:       meta.Size,
		Path:       entryDir,
		StoredAt:   meta.StoredAt,
		AccessedAt: meta.AccessedAt,
	}, nil
}

func (s *ImageStore) touchMetadata(entryDir string) error {
	meta, err := readMetadata(entryDir)
	if err != nil {
		return err
	}

	meta.AccessedAt = time.Now()
	if meta.Size == 0 {
		size, err := dirSize(entryDir)
		if err != nil {
			return fmt.Errorf("measure cached image %q: %w", meta.Ref, err)
		}
		meta.Size = size
	}
	return writeMetadata(entryDir, meta)
}

func normalizeRefString(ref string) (string, error) {
	parsed, err := ParseImageRef(ref)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func readMetadata(entryDir string) (imageMetadata, error) {
	data, err := os.ReadFile(filepath.Join(entryDir, metadataFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return imageMetadata{}, fmt.Errorf("cached image metadata missing in %q: %w", entryDir, ErrImageNotCached)
		}
		return imageMetadata{}, fmt.Errorf("read cached image metadata in %q: %w", entryDir, err)
	}

	var meta imageMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return imageMetadata{}, fmt.Errorf("decode cached image metadata in %q: %w", entryDir, err)
	}
	return meta, nil
}

func writeMetadata(entryDir string, meta imageMetadata) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cached image metadata for %q: %w", meta.Ref, err)
	}

	if err := os.WriteFile(filepath.Join(entryDir, metadataFileName), data, 0o644); err != nil {
		return fmt.Errorf("write cached image metadata for %q: %w", meta.Ref, err)
	}
	return nil
}

func refCacheKey(ref string) string {
	sum := sha256.Sum256([]byte(ref))
	return hex.EncodeToString(sum[:])
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}
