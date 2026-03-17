package snapshot

// DiffType describes the kind of change between two snapshots.
type DiffType string

const (
	DiffAdded    DiffType = "added"
	DiffModified DiffType = "modified"
	DiffDeleted  DiffType = "deleted"
)

// DiffEntry represents a single file-level change between two snapshots.
type DiffEntry struct {
	Path    string   `json:"path"`
	Type    DiffType `json:"type"`
	OldHash string   `json:"old_hash,omitempty"`
	NewHash string   `json:"new_hash,omitempty"`
	OldSize int64    `json:"old_size,omitempty"`
	NewSize int64    `json:"new_size,omitempty"`
}

// diffManifests compares two manifests and returns the set of changes needed
// to transform snapshot m1 into snapshot m2.
//
//   - Files present only in m2 are DiffAdded.
//   - Files present only in m1 are DiffDeleted.
//   - Files present in both but with a different hash are DiffModified.
//   - Directories and files with identical hashes are omitted.
func diffManifests(m1, m2 Manifest) []DiffEntry {
	// Index the files in each manifest by their relative path.
	index1 := indexFiles(m1.Files)
	index2 := indexFiles(m2.Files)

	var entries []DiffEntry

	// Detect deleted and modified files (files that were in m1).
	for path, f1 := range index1 {
		if f1.IsDir {
			continue
		}
		f2, exists := index2[path]
		if !exists {
			entries = append(entries, DiffEntry{
				Path:    path,
				Type:    DiffDeleted,
				OldHash: f1.Hash,
				OldSize: f1.Size,
			})
			continue
		}
		if f1.Hash != f2.Hash {
			entries = append(entries, DiffEntry{
				Path:    path,
				Type:    DiffModified,
				OldHash: f1.Hash,
				NewHash: f2.Hash,
				OldSize: f1.Size,
				NewSize: f2.Size,
			})
		}
	}

	// Detect added files (files that are only in m2).
	for path, f2 := range index2 {
		if f2.IsDir {
			continue
		}
		if _, exists := index1[path]; !exists {
			entries = append(entries, DiffEntry{
				Path:    path,
				Type:    DiffAdded,
				NewHash: f2.Hash,
				NewSize: f2.Size,
			})
		}
	}

	return entries
}

// indexFiles builds a map from relative file path to SnapshotFile.
func indexFiles(files []SnapshotFile) map[string]SnapshotFile {
	m := make(map[string]SnapshotFile, len(files))
	for _, f := range files {
		m[f.Path] = f
	}
	return m
}
