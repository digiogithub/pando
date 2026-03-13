package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	relativePath := r.URL.Query().Get("path")
	if relativePath == "" {
		relativePath = "."
	}

	fullPath := filepath.Join(s.config.CWD, relativePath)

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
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	relativePath := strings.TrimPrefix(r.URL.Path, "/api/v1/files/")
	if relativePath == "" {
		writeError(w, http.StatusBadRequest, "file path required")
		return
	}

	fullPath := filepath.Join(s.config.CWD, relativePath)

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
