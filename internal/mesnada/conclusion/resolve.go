package conclusion

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ArtifactResolver enriches an artifact reference string for display.
// It receives the raw ref (a file path, URL, or any opaque token) and returns
// a richer display string. It must return the input unchanged when enrichment
// is not possible or the ref is unrecognized.
type ArtifactResolver func(ref string) string

// MemoryRefResolver enriches a memory-ref identifier for display.
// It receives the raw ref (a KB path, memory key, or any opaque token) and
// returns a richer display string. It must return the input unchanged when
// enrichment is not possible.
type MemoryRefResolver func(ref string) string

// ResolveOptions carries optional resolvers that FormatForParent uses to enrich
// artifact and memory-ref entries. A nil *ResolveOptions or a nil resolver
// field means no enrichment is applied for that category — the output is
// byte-for-byte identical to the pre-resolver behavior.
type ResolveOptions struct {
	// Artifacts, when non-nil, is called for each artifact entry. When nil,
	// artifacts are rendered as a plain comma-joined list (legacy behavior).
	Artifacts ArtifactResolver

	// Memory, when non-nil, is called for each memory_ref entry. When nil,
	// memory_refs are rendered as a plain comma-joined list (legacy behavior).
	Memory MemoryRefResolver
}

// NewFilesystemArtifactResolver returns an ArtifactResolver that stats each
// artifact path on the local filesystem and appends a human-readable size or
// "(missing)" suffix. If the ref is a relative path, it is resolved against
// baseDir. A blank baseDir or any stat error returns the bare ref unchanged.
func NewFilesystemArtifactResolver(baseDir string) ArtifactResolver {
	return func(ref string) string {
		if strings.TrimSpace(ref) == "" {
			return ref
		}
		path := ref
		if baseDir != "" && !filepath.IsAbs(ref) {
			path = filepath.Join(baseDir, ref)
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				return ref + " (missing)"
			}
			// Any other error (permissions, etc.): return bare ref.
			return ref
		}
		return fmt.Sprintf("%s (%s)", ref, humanBytes(info.Size()))
	}
}

// humanBytes formats n bytes as a compact human-readable string (B/KB/MB).
func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		kb := float64(n) / 1024
		return fmt.Sprintf("%.1f KB", kb)
	default:
		mb := float64(n) / (1024 * 1024)
		return fmt.Sprintf("%.1f MB", mb)
	}
}
