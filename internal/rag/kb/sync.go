package kb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SyncDirectoryWithStats imports or syncs all markdown files from a directory.
// It recursively scans for .md files, upserts modified documents, and optionally
// deletes KB documents that no longer exist on disk for that directory source.
func (s *KBStore) SyncDirectoryWithStats(ctx context.Context, dirPath string, deleteMissing bool) (SyncStats, error) {
	var stats SyncStats

	baseDir, err := filepath.Abs(strings.TrimSpace(dirPath))
	if err != nil {
		return stats, fmt.Errorf("kb: resolve sync directory %q: %w", dirPath, err)
	}

	baseInfo, err := os.Stat(baseDir)
	if err != nil {
		return stats, fmt.Errorf("kb: stat sync directory %q: %w", baseDir, err)
	}
	if !baseInfo.IsDir() {
		return stats, fmt.Errorf("kb: sync path is not a directory: %s", baseDir)
	}

	seen := make(map[string]struct{})

	err = filepath.WalkDir(baseDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !isMarkdownFile(path) {
			return nil
		}

		stats.Scanned++
		relPath, relErr := filepath.Rel(baseDir, path)
		if relErr != nil {
			return fmt.Errorf("kb: relative path for %q: %w", path, relErr)
		}
		docPath := normalizeDocPath(relPath)
		seen[docPath] = struct{}{}

		contentBytes, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("kb: read file %s: %w", path, readErr)
		}

		fi, statErr := os.Stat(path)
		if statErr != nil {
			return fmt.Errorf("kb: stat file %s: %w", path, statErr)
		}

		meta := map[string]interface{}{
			"source_path":       path,
			"source_mtime_unix": fi.ModTime().Unix(),
		}

		existing, getErr := s.GetDocument(ctx, docPath)
		if getErr != nil {
			return fmt.Errorf("kb: check existing %s: %w", docPath, getErr)
		}

		content := string(contentBytes)
		if existing == nil {
			if addErr := s.AddDocument(ctx, docPath, content, meta); addErr != nil {
				return fmt.Errorf("kb: add %s: %w", docPath, addErr)
			}
			stats.Added++
			return nil
		}

		prevMTime := metadataInt64(existing.Metadata, "source_mtime_unix")
		if existing.Content == content && prevMTime == fi.ModTime().Unix() {
			stats.Unchanged++
			return nil
		}

		if updateErr := s.UpdateDocument(ctx, docPath, content, meta); updateErr != nil {
			return fmt.Errorf("kb: update %s: %w", docPath, updateErr)
		}
		stats.Updated++
		return nil
	})
	if err != nil {
		return stats, fmt.Errorf("kb: walk directory: %w", err)
	}

	if deleteMissing {
		offset := 0
		toDelete := make([]string, 0)
		for {
			docs, listErr := s.ListDocuments(ctx, 200, offset)
			if listErr != nil {
				return stats, fmt.Errorf("kb: list documents for delete-missing: %w", listErr)
			}
			if len(docs) == 0 {
				break
			}

			for i := range docs {
				doc := docs[i]
				sourcePath := strings.TrimSpace(metadataString(doc.Metadata, "source_path"))
				if sourcePath == "" {
					continue
				}
				absSource, absErr := filepath.Abs(sourcePath)
				if absErr != nil {
					continue
				}
				if !isPathWithinBase(absSource, baseDir) {
					continue
				}
				if _, ok := seen[doc.FilePath]; ok {
					continue
				}
				toDelete = append(toDelete, doc.FilePath)
			}

			offset += len(docs)
		}

		for _, docPath := range toDelete {
			if delErr := s.DeleteDocument(ctx, docPath); delErr != nil {
				return stats, fmt.Errorf("kb: delete missing doc %s: %w", docPath, delErr)
			}
			stats.Deleted++
		}
	}

	return stats, nil
}

func isMarkdownFile(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".md")
}

func normalizeDocPath(p string) string {
	if p == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(p))
}

func metadataString(meta map[string]interface{}, key string) string {
	if meta == nil {
		return ""
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return ""
	}
	switch typed := v.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func metadataInt64(meta map[string]interface{}, key string) int64 {
	if meta == nil {
		return 0
	}
	v, ok := meta[key]
	if !ok || v == nil {
		return 0
	}
	switch typed := v.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

func isPathWithinBase(path string, base string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
