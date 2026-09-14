package kb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/logging"
)

// frontMatterRepairMarkerPath is the file_path of an internal, synthetic KB
// document used to record that RepairFrontMatterMetadata has already run for
// a given KB source directory, so a repaired (or already-healthy) corpus is
// not re-scanned on every startup. It carries no "source_path" metadata, so
// it can never be picked up by SyncDirectoryWithStats or the watcher, and the
// repair pass itself skips it for the same reason (it only looks at
// documents with a source_path).
const frontMatterRepairMarkerPath = "__pando_kb__/frontmatter_repair_marker"

// RepairFrontMatterMetadata re-indexes documents whose stored metadata is
// missing front-matter-derived keys (tags, aliases, or any other host-defined
// key) that their source file on disk still declares. It repairs the damage
// the pre-fix watcher inflicted: every edit made through the watcher replaced
// a document's metadata with bare source_* fields, discarding tags and any
// other front-matter key, and recorded a fresh source_mtime_unix so the sync
// path never noticed the file "changed" again and so never repaired it on its
// own.
//
// The pass ignores the mtime-skip semantics the regular sync uses: it
// re-parses every filesystem-backed document's current source file
// regardless of what source_mtime_unix says, because that is exactly the
// field the bug left in a false "unchanged" state.
//
// The repair is bounded and one-shot per dirPath: on success it records a
// marker document and returns immediately without scanning on a later call
// for the same directory. A corpus already indexed by the fixed sync/watcher
// code has nothing to repair, so the pass still scans it once but writes
// nothing (RepairStats.Repaired stays 0).
func (s *KBStore) RepairFrontMatterMetadata(ctx context.Context, dirPath string) (RepairStats, error) {
	var stats RepairStats

	already, err := s.frontMatterRepairAlreadyRan(ctx, dirPath)
	if err != nil {
		return stats, err
	}
	if already {
		return stats, nil
	}

	conv := s.documentConverter()

	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return stats, err
		}

		items, err := s.listDocumentMetadata(ctx, 500, offset)
		if err != nil {
			return stats, fmt.Errorf("kb: repair list metadata: %w", err)
		}
		if len(items) == 0 {
			break
		}
		offset += len(items)

		for _, item := range items {
			if err := ctx.Err(); err != nil {
				return stats, err
			}

			sourcePath := strings.TrimSpace(metadataString(item.Metadata, "source_path"))
			if sourcePath == "" {
				continue // synthetic or memory document, not filesystem-backed.
			}
			if _, ok := item.Metadata["converted"]; ok {
				continue // converted documents carry no front matter to repair.
			}

			stats.Scanned++

			repaired, repairErr := s.repairDocumentFrontMatter(ctx, item, sourcePath, conv)
			if repairErr != nil {
				logging.Warn("kb repair: front matter repair failed",
					"doc_path", item.FilePath,
					"error", repairErr,
				)
				continue
			}
			if repaired {
				stats.Repaired++
			}
		}
	}

	if err := s.markFrontMatterRepairDone(ctx, dirPath, stats); err != nil {
		return stats, err
	}

	return stats, nil
}

// repairDocumentFrontMatter re-parses item's source file and, if the parsed
// front matter declares a metadata key the stored document does not have,
// rebuilds the document's metadata and body through the exact helper the
// sync/watcher paths use and re-indexes it.
func (s *KBStore) repairDocumentFrontMatter(ctx context.Context, item documentMetadata, sourcePath string, conv DocumentConverter) (bool, error) {
	fi, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // Missing file; the regular sync/watcher delete path handles that.
		}
		return false, err
	}
	if fi.IsDir() {
		return false, nil
	}

	content, format, converted, err := loadDocumentBody(sourcePath, conv)
	if err != nil {
		return false, err
	}
	if converted {
		return false, nil // No front matter to lose.
	}

	meta, body := buildDocumentMetadata(sourcePath, item.FilePath, fi.ModTime().Unix(), loadResult{
		content:   content,
		format:    format,
		converted: converted,
	})

	if !frontMatterMetadataMissing(item.Metadata, meta) {
		return false, nil
	}

	if err := s.UpdateDocument(ctx, item.FilePath, body, meta); err != nil {
		return false, err
	}
	return true, nil
}

// frontMatterMetadataMissing reports whether freshly-parsed metadata declares
// a front-matter-derived key (tags, aliases, or any other unreserved
// front-matter key) that stored does not have. Sync-owned fields
// (source_path/source_mtime_unix/source_format/converted) are always present
// on both sides and are not what this repair is about, so they are ignored.
func frontMatterMetadataMissing(stored, fresh map[string]interface{}) bool {
	for k := range fresh {
		switch k {
		case "source_path", "source_mtime_unix", "source_format", "converted":
			continue
		}
		if _, ok := stored[k]; !ok {
			return true
		}
	}
	return false
}

func (s *KBStore) frontMatterRepairAlreadyRan(ctx context.Context, dirPath string) (bool, error) {
	meta, err := s.getDocumentMetadata(ctx, frontMatterRepairMarkerPath)
	if err != nil {
		return false, fmt.Errorf("kb: repair marker lookup: %w", err)
	}
	if meta == nil {
		return false, nil
	}
	return metadataString(meta.Metadata, "dir_path") == dirPath, nil
}

// markFrontMatterRepairDone records (adding or updating) the marker document
// that makes the repair one-shot per directory. The write is made under
// WithoutWriteObserver: this is internal bookkeeping, not a document a host
// asked to store, and must not surface as a user-visible write event.
func (s *KBStore) markFrontMatterRepairDone(ctx context.Context, dirPath string, stats RepairStats) error {
	ctx = WithoutWriteObserver(ctx)

	meta := map[string]interface{}{
		"dir_path": dirPath,
		"scanned":  stats.Scanned,
		"repaired": stats.Repaired,
		"ran_at":   time.Now().UTC().Format(time.RFC3339),
	}
	content := "Internal marker recording that the KB front-matter metadata repair " +
		"(PANDO-US-0003) has run for this source directory. Safe to delete: the " +
		"repair will simply run again on the next start."

	existing, err := s.getDocumentMetadata(ctx, frontMatterRepairMarkerPath)
	if err != nil {
		return fmt.Errorf("kb: repair marker lookup before write: %w", err)
	}
	if existing == nil {
		return s.AddDocument(ctx, frontMatterRepairMarkerPath, content, meta)
	}
	return s.UpdateDocument(ctx, frontMatterRepairMarkerPath, content, meta)
}
