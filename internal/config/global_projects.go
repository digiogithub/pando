package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// GlobalConfigDir returns the user-level Pando configuration directory.
// Follows XDG Base Directory spec: $XDG_CONFIG_HOME/pando or ~/.config/pando.
func GlobalConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "pando")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "pando")
}

// GlobalProjectEntry represents a known Pando project in the global registry.
type GlobalProjectEntry struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type globalProjectsRegistry struct {
	Version  int                  `json:"version"`
	Projects []GlobalProjectEntry `json:"projects"`
}

func globalProjectsFilePath() string {
	dir := GlobalConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "projects.json")
}

// LoadGlobalProjects reads the global projects registry.
// Returns an empty list if the file does not exist.
func LoadGlobalProjects() ([]GlobalProjectEntry, error) {
	path := globalProjectsFilePath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var reg globalProjectsRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	return reg.Projects, nil
}

// saveGlobalProjects writes the global projects registry atomically.
func saveGlobalProjects(projects []GlobalProjectEntry) error {
	path := globalProjectsFilePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	reg := globalProjectsRegistry{Version: 1, Projects: projects}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	// Write to temp file then rename for atomicity.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RegisterSelfAsGlobalProject upserts the given absolute path (name = folder
// basename by default) into the global projects registry. Existing entries
// are not renamed – user-set names are preserved.
// No-op when absPath is empty or the registry directory is unavailable.
func RegisterSelfAsGlobalProject(absPath, name string) error {
	if absPath == "" {
		return nil
	}
	projects, _ := LoadGlobalProjects() // ignore read error, start fresh

	for _, p := range projects {
		if p.Path == absPath {
			return nil // already registered
		}
	}
	projects = append(projects, GlobalProjectEntry{Path: absPath, Name: name})
	return saveGlobalProjects(projects)
}

// UpdateGlobalProjectName updates the display name for a path in the global
// registry. Silently succeeds if the path is not found.
func UpdateGlobalProjectName(absPath, newName string) error {
	projects, err := LoadGlobalProjects()
	if err != nil {
		return err
	}
	for i, p := range projects {
		if p.Path == absPath {
			projects[i].Name = newName
			return saveGlobalProjects(projects)
		}
	}
	return nil
}
