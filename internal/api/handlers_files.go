package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// isSafePath checks that the resolved path is within the working directory.
func (s *Server) isSafePath(relativePath string) (string, bool) {
	if strings.Contains(relativePath, "..") {
		return "", false
	}
	cwd := s.config.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", false
		}
	}
	fullPath := filepath.Join(cwd, filepath.Clean(relativePath))
	// Verify the resolved path stays within cwd
	rel, err := filepath.Rel(cwd, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", false
	}
	return fullPath, true
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListFiles(w, r)
	case http.MethodPost:
		s.handleCreateFile(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	relativePath := r.URL.Query().Get("path")
	if relativePath == "" {
		relativePath = "."
	}

	fullPath, safe := s.isSafePath(relativePath)
	if !safe {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "directory not found")
		return
	}

	files := make([]map[string]interface{}, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && name != "." {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, map[string]interface{}{
			"name":  name,
			"isDir": entry.IsDir(),
			"size":  info.Size(),
			"path":  filepath.Join(relativePath, name),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":  relativePath,
		"files": files,
	})
}

func (s *Server) handleFileByPath(w http.ResponseWriter, r *http.Request) {
	relativePath := strings.TrimPrefix(r.URL.Path, "/api/v1/files/")
	if relativePath == "" {
		writeError(w, http.StatusBadRequest, "file path required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleReadFile(w, r, relativePath)
	case http.MethodPut:
		s.handleUpdateFile(w, r, relativePath)
	case http.MethodDelete:
		s.handleDeleteFile(w, r, relativePath)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request, relativePath string) {
	fullPath, safe := s.isSafePath(relativePath)
	if !safe {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "path is a directory, use /api/v1/files endpoint")
		return
	}

	file, err := os.Open(fullPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to open file")
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":    relativePath,
		"content": string(content),
		"size":    len(content),
	})
}

func (s *Server) handleUpdateFile(w http.ResponseWriter, r *http.Request, relativePath string) {
	fullPath, safe := s.isSafePath(relativePath)
	if !safe {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create parent directory")
		return
	}

	if err := os.WriteFile(fullPath, []byte(body.Content), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":    relativePath,
		"message": "file updated",
	})
}

func (s *Server) handleCreateFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path    string  `json:"path"`
		Content *string `json:"content"`
		IsDir   bool    `json:"isDir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	fullPath, safe := s.isSafePath(body.Path)
	if !safe {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}

	if body.IsDir {
		if err := os.MkdirAll(fullPath, 0755); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create directory")
			return
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"path":    body.Path,
			"isDir":   true,
			"message": "directory created",
		})
		return
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create parent directory")
		return
	}

	content := ""
	if body.Content != nil {
		content = *body.Content
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create file")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"path":    body.Path,
		"message": "file created",
	})
}

func (s *Server) handleRawFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	relativePath := strings.TrimPrefix(r.URL.Path, "/api/v1/files/raw/")
	if relativePath == "" {
		writeError(w, http.StatusBadRequest, "file path required")
		return
	}
	fullPath, safe := s.isSafePath(relativePath)
	if !safe {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	f, err := os.Open(fullPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to open file")
		return
	}
	defer f.Close()
	// ServeContent detects MIME type from filename and handles range requests
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request, relativePath string) {
	fullPath, safe := s.isSafePath(relativePath)
	if !safe {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	if err := os.RemoveAll(fullPath); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":    relativePath,
		"message": "deleted",
	})
}

func (s *Server) handleRenameFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var body struct {
		OldPath string `json:"oldPath"`
		NewPath string `json:"newPath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.OldPath == "" || body.NewPath == "" {
		writeError(w, http.StatusBadRequest, "oldPath and newPath are required")
		return
	}

	oldFull, oldSafe := s.isSafePath(body.OldPath)
	newFull, newSafe := s.isSafePath(body.NewPath)
	if !oldSafe || !newSafe {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}

	if _, err := os.Stat(oldFull); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "source file not found")
		return
	}

	// Ensure parent of destination exists
	if err := os.MkdirAll(filepath.Dir(newFull), 0755); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create destination directory")
		return
	}

	if err := os.Rename(oldFull, newFull); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rename file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"oldPath": body.OldPath,
		"newPath": body.NewPath,
		"message": "renamed",
	})
}

// handleFSBrowse handles GET /api/v1/fs/browse?path=...
// Returns subdirectories at the given absolute path (expands ~).
func (s *Server) handleFSBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/"
		}
		path = home
	}

	// Expand ~ to home directory
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}

	path = filepath.Clean(path)

	// Reject non-absolute paths after cleaning to prevent traversal via relative segments.
	if !filepath.IsAbs(path) {
		writeError(w, http.StatusBadRequest, "path must be absolute")
		return
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read directory: "+err.Error())
		return
	}

	dirs := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}

	parent := filepath.Dir(path)
	if parent == path {
		parent = ""
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":   path,
		"parent": parent,
		"dirs":   dirs,
	})
}

func (s *Server) handleSearchFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	query := strings.ToLower(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	cwd := s.config.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get working directory")
			return
		}
	}

	var results []map[string]interface{}
	err := filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()
		// Skip hidden files and directories
		if strings.HasPrefix(name, ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip common large directories
		if info.IsDir() && (name == "node_modules" || name == "vendor" || name == ".git") {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.Contains(strings.ToLower(name), query) {
			rel, _ := filepath.Rel(cwd, path)
			results = append(results, map[string]interface{}{
				"name":  name,
				"path":  rel,
				"isDir": false,
				"size":  info.Size(),
			})
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":   query,
		"results": results,
	})
}
