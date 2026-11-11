package kb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConfigureFilesystemMirror sets a base directory where KB documents are mirrored
// as markdown files on add/update/delete operations.
func (s *KBStore) ConfigureFilesystemMirror(dirPath string) error {
	trimmed := strings.TrimSpace(dirPath)
	if trimmed == "" {
		s.fsMu.Lock()
		s.fsMirrorPath = ""
		s.fsMu.Unlock()
		return nil
	}

	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return fmt.Errorf("kb: resolve mirror path %q: %w", dirPath, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return fmt.Errorf("kb: create mirror path %q: %w", abs, err)
	}

	s.fsMu.Lock()
	s.fsMirrorPath = abs
	s.fsMu.Unlock()
	return nil
}

// FilesystemMirrorPath returns the configured filesystem mirror path.
func (s *KBStore) FilesystemMirrorPath() string {
	s.fsMu.RLock()
	defer s.fsMu.RUnlock()
	return s.fsMirrorPath
}

// WriteDocumentToFilesystem writes a KB document to the configured mirror path.
// If no mirror path is configured, this is a no-op.
func (s *KBStore) WriteDocumentToFilesystem(filePath, content string) error {
	base := s.FilesystemMirrorPath()
	if strings.TrimSpace(base) == "" {
		return nil
	}

	targetPath, err := resolveMirrorDocumentPath(base, filePath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("kb: create mirror parent for %q: %w", targetPath, err)
	}

	// Record the self-write before os.WriteFile returns, so the fsnotify event
	// this write is about to generate can never race ahead of the watcher's
	// suppression check (PANDO-US-0004). The predicted mtime (now) covers that
	// race window; it is corrected below to the real on-disk mtime immediately
	// after the write, well before the watcher's 250ms debounce elapses.
	s.recordSelfWrite(targetPath, time.Now().Unix())

	if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("kb: write mirrored document %q: %w", targetPath, err)
	}

	if fi, statErr := os.Stat(targetPath); statErr == nil {
		s.recordSelfWrite(targetPath, fi.ModTime().Unix())
	}
	return nil
}

// DeleteDocumentFromFilesystem removes a mirrored KB document from disk.
// If no mirror path is configured, this is a no-op.
func (s *KBStore) DeleteDocumentFromFilesystem(filePath string) error {
	base := s.FilesystemMirrorPath()
	if strings.TrimSpace(base) == "" {
		return nil
	}

	targetPath, err := resolveMirrorDocumentPath(base, filePath)
	if err != nil {
		return err
	}

	// Record before the removal happens, for the same race-avoidance reason
	// WriteDocumentToFilesystem does above (PANDO-US-0004).
	s.recordSelfDelete(targetPath)

	err = os.Remove(targetPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("kb: remove mirrored document %q: %w", targetPath, err)
	}
	return nil
}

func resolveMirrorDocumentPath(basePath, docPath string) (string, error) {
	cleanRel := filepath.Clean(filepath.FromSlash(strings.TrimSpace(docPath)))
	if cleanRel == "." || cleanRel == "" {
		return "", fmt.Errorf("kb: invalid document path %q", docPath)
	}
	if filepath.IsAbs(cleanRel) {
		return "", fmt.Errorf("kb: document path must be relative: %q", docPath)
	}
	if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("kb: document path escapes mirror base: %q", docPath)
	}

	full := filepath.Join(basePath, cleanRel)
	fullClean := filepath.Clean(full)
	if !isPathWithinBase(fullClean, basePath) {
		return "", fmt.Errorf("kb: resolved path escapes mirror base: %q", docPath)
	}
	return fullClean, nil
}

// ReadMirroredFrontMatter reads the front matter of filePath's file in the
// filesystem mirror, so a tool rewriting the file can keep what it records
// (created_at, aliases, host-defined keys) instead of rebuilding it from the
// index alone. extra holds the keys the typed FrontMatter does not serialize.
// ok is false when no mirror is configured, the file does not exist, or it has
// no front matter that parses.
func (s *KBStore) ReadMirroredFrontMatter(filePath string) (fm FrontMatter, extra map[string]interface{}, ok bool, err error) {
	base := s.FilesystemMirrorPath()
	if strings.TrimSpace(base) == "" {
		return FrontMatter{}, nil, false, nil
	}
	targetPath, err := resolveMirrorDocumentPath(base, filePath)
	if err != nil {
		return FrontMatter{}, nil, false, err
	}
	raw, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return FrontMatter{}, nil, false, nil
		}
		return FrontMatter{}, nil, false, fmt.Errorf("kb: read mirrored document %q: %w", targetPath, err)
	}
	parsed, rawFM, _, parseErr := ParseFrontMatterWithRaw(string(raw))
	if parseErr != nil || rawFM == nil {
		return FrontMatter{}, nil, false, nil
	}
	return parsed, ExtraFrontMatterKeys(rawFM), true, nil
}

// MirrorSourceMetadata returns the source_* metadata the directory sync records
// for filePath's mirrored file (source_path, source_mtime_unix, source_format),
// read after the file was written. Storing it with the document lets the next
// startup sync see the file as unchanged instead of re-importing it.
//
// It returns nil when no mirror is configured, the file cannot be stat'd, or
// the path is not a markdown file: the sync only indexes markdown (and
// converted documents) by extension, and a source_path on a document whose
// file the sync never visits would make its delete-missing pass remove it.
func (s *KBStore) MirrorSourceMetadata(filePath string) map[string]interface{} {
	base := s.FilesystemMirrorPath()
	if strings.TrimSpace(base) == "" || !isMarkdownFile(filePath) {
		return nil
	}
	targetPath, err := resolveMirrorDocumentPath(base, filePath)
	if err != nil {
		return nil
	}
	fi, err := os.Stat(targetPath)
	if err != nil || fi.IsDir() {
		return nil
	}
	return map[string]interface{}{
		"source_path":       targetPath,
		"source_mtime_unix": fi.ModTime().Unix(),
		"source_format":     fileFormat(targetPath),
	}
}
