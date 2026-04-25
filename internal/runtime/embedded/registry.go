package embedded

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// ImageRef identifies an OCI image by registry, repository, and tag or digest.
type ImageRef struct {
	Registry   string
	Repository string
	Tag        string
	Digest     string
}

// RegistryClient pulls OCI images into the local OCI layout cache.
type RegistryClient struct {
	CacheDir string
	Auth     authn.Keychain
}

// ParseImageRef normalizes a Docker/OCI image reference into structured fields.
func ParseImageRef(raw string) (ImageRef, error) {
	named, err := name.ParseReference(strings.TrimSpace(raw))
	if err != nil {
		return ImageRef{}, fmt.Errorf("parse image reference %q: %w", raw, err)
	}

	full := named.Name()
	out := ImageRef{}
	repoPart := full

	if base, digest, ok := strings.Cut(full, "@"); ok {
		repoPart = base
		out.Digest = digest
	}
	if out.Digest == "" {
		if idx := strings.LastIndex(repoPart, ":"); idx > strings.LastIndex(repoPart, "/") {
			out.Tag = repoPart[idx+1:]
			repoPart = repoPart[:idx]
		}
	}

	firstSlash := strings.Index(repoPart, "/")
	if firstSlash == -1 {
		out.Repository = repoPart
		return out, nil
	}

	out.Registry = repoPart[:firstSlash]
	out.Repository = repoPart[firstSlash+1:]
	return out, nil
}

func (r ImageRef) String() string {
	repository := strings.Trim(strings.TrimSpace(r.Repository), "/")
	if repository == "" {
		return ""
	}

	if registry := strings.Trim(strings.TrimSpace(r.Registry), "/"); registry != "" {
		repository = registry + "/" + repository
	}
	if digest := strings.TrimSpace(r.Digest); digest != "" {
		return repository + "@" + digest
	}

	tag := strings.TrimSpace(r.Tag)
	if tag == "" {
		tag = "latest"
	}
	return repository + ":" + tag
}

// ResolveCacheDir expands the configured cache dir or returns the default cache.
func ResolveCacheDir(cacheDir string) (string, error) {
	cacheDir = strings.TrimSpace(cacheDir)
	if cacheDir != "" {
		expanded, err := expandHome(cacheDir)
		if err != nil {
			return "", err
		}
		return filepath.Clean(expanded), nil
	}

	homeDir, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(homeDir) != "" {
		return filepath.Join(homeDir, ".pando", "image-cache"), nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve embedded image cache directory: %w", err)
	}
	return filepath.Join(wd, ".pando", "image-cache"), nil
}

// Pull downloads ref from the registry and persists it into the local OCI cache.
func (r *RegistryClient) Pull(ctx context.Context, ref ImageRef) (v1.Image, error) {
	refName := ref.String()
	named, err := name.ParseReference(refName)
	if err != nil {
		return nil, fmt.Errorf("parse image reference %q: %w", refName, err)
	}

	img, err := remote.Image(named, remote.WithAuthFromKeychain(r.keychain()), remote.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("pull image %q: %w", refName, err)
	}

	store, err := r.store()
	if err != nil {
		return nil, err
	}
	if err := store.Put(refName, img); err != nil {
		return nil, err
	}
	return img, nil
}

// IsLocal reports whether ref already exists in the local OCI cache.
func (r *RegistryClient) IsLocal(ref ImageRef) (bool, error) {
	store, err := r.store()
	if err != nil {
		return false, err
	}
	_, err = store.Get(ref.String())
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrImageNotCached):
		return false, nil
	default:
		return false, err
	}
}

// GC removes cached images outside the most recently used keepN entries.
func (r *RegistryClient) GC(ctx context.Context, keepN int) error {
	if keepN < 0 {
		return fmt.Errorf("keepN must be non-negative")
	}

	store, err := r.store()
	if err != nil {
		return err
	}

	entries, err := store.List()
	if err != nil {
		return err
	}

	sort.Slice(entries, func(i, j int) bool {
		left := lastTouched(entries[i])
		right := lastTouched(entries[j])
		if left.Equal(right) {
			return entries[i].Ref < entries[j].Ref
		}
		return left.After(right)
	})

	var errs []error
	for idx := keepN; idx < len(entries); idx++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := store.Delete(entries[idx].Ref); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *RegistryClient) keychain() authn.Keychain {
	if r != nil && r.Auth != nil {
		return r.Auth
	}
	return authn.DefaultKeychain
}

func (r *RegistryClient) store() (*ImageStore, error) {
	cacheDir, err := ResolveCacheDir(r.CacheDir)
	if err != nil {
		return nil, err
	}
	return &ImageStore{Root: cacheDir}, nil
}

func expandHome(path string) (string, error) {
	if path == "" || path[0] != '~' {
		return path, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if path == "~" {
		return homeDir, nil
	}
	return filepath.Join(homeDir, strings.TrimPrefix(path, "~/")), nil
}

func lastTouched(entry StoreEntry) time.Time {
	if !entry.AccessedAt.IsZero() {
		return entry.AccessedAt
	}
	return entry.StoredAt
}
