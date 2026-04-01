package kb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("kb: write mirrored document %q: %w", targetPath, err)
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
