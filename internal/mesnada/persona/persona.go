// Package persona handles loading and managing persona definitions.
package persona

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Manager handles persona loading and retrieval.
type Manager struct {
	personaPath      string
	personas         map[string]string // name -> content
	personaMCPConfig map[string]string // name -> mcp-config.json path
	activePersona    string            // currently selected persona name (empty = auto/none)
	mu               sync.RWMutex
}

// NewManager creates a new persona manager.
// If personaPath is empty, creates an empty manager.
func NewManager(personaPath string) (*Manager, error) {
	m := &Manager{
		personaPath:      personaPath,
		personas:         make(map[string]string),
		personaMCPConfig: make(map[string]string),
	}

	if personaPath != "" {
		if err := m.loadPersonas(); err != nil {
			return nil, fmt.Errorf("failed to load personas: %w", err)
		}
	}

	return m, nil
}

// NewManagerWithBuiltins creates a persona manager that first loads built-in personas
// from the provided embed.FS, then overlays any user-defined personas from personaPath.
// personaPath may be empty (only built-ins will be loaded).
func NewManagerWithBuiltins(builtinFS fs.ReadDirFS, personaPath string) (*Manager, error) {
	m := &Manager{
		personaPath:      personaPath,
		personas:         make(map[string]string),
		personaMCPConfig: make(map[string]string),
	}

	// Load built-in personas first.
	if err := m.loadBuiltinPersonas(builtinFS); err != nil {
		return nil, fmt.Errorf("failed to load built-in personas: %w", err)
	}

	// Overlay with user-defined personas (may override built-ins).
	if personaPath != "" {
		if err := m.loadPersonas(); err != nil {
			return nil, fmt.Errorf("failed to load user personas: %w", err)
		}
	}

	return m, nil
}

// loadBuiltinPersonas reads all .md files from the given embed.FS.
func (m *Manager) loadBuiltinPersonas(fsys fs.ReadDirFS) error {
	entries, err := fsys.ReadDir(".")
	if err != nil {
		return fmt.Errorf("failed to read built-in persona directory: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}

		personaName := strings.TrimSuffix(name, filepath.Ext(name))

		content, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("failed to read built-in persona %s: %w", name, err)
		}

		m.personas[personaName] = string(content)
	}

	return nil
}

// loadPersonas reads all .md files from the persona directory.
func (m *Manager) loadPersonas() error {
	// Check if directory exists
	info, err := os.Stat(m.personaPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist, just return empty (not an error)
			return nil
		}
		return err
	}

	if !info.IsDir() {
		return fmt.Errorf("persona_path is not a directory: %s", m.personaPath)
	}

	// Read all .md files
	entries, err := os.ReadDir(m.personaPath)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}

		// Remove .md extension to get persona name
		personaName := strings.TrimSuffix(name, filepath.Ext(name))

		// Read file content
		filePath := filepath.Join(m.personaPath, name)
		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read persona file %s: %w", name, err)
		}

		m.personas[personaName] = string(content)

		// Check for associated MCP config file: {persona-name}.mcp-config.json
		mcpConfigName := personaName + ".mcp-config.json"
		mcpConfigPath := filepath.Join(m.personaPath, mcpConfigName)
		if _, err := os.Stat(mcpConfigPath); err == nil {
			m.personaMCPConfig[personaName] = mcpConfigPath
		}
	}

	return nil
}

// GetPersona returns the content of a persona by name.
// Returns empty string if persona not found.
func (m *Manager) GetPersona(name string) string {
	if name == "" {
		return ""
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	content, _ := m.personas[name]
	return content
}

// ListPersonas returns a sorted list of available persona names.
func (m *Manager) ListPersonas() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.personas))
	for name := range m.personas {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// HasPersona checks if a persona exists.
func (m *Manager) HasPersona(name string) bool {
	if name == "" {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.personas[name]
	return exists
}

// GetPersonaMCPConfig returns the path to the persona's associated MCP config file.
// Returns empty string if the persona has no associated MCP config.
func (m *Manager) GetPersonaMCPConfig(name string) string {
	if name == "" {
		return ""
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	path, _ := m.personaMCPConfig[name]
	return path
}

// ApplyPersona prepends persona content to the given prompt.
// If persona is empty or not found, returns the original prompt.
func (m *Manager) ApplyPersona(personaName, prompt string) string {
	if personaName == "" {
		return prompt
	}

	content := m.GetPersona(personaName)
	if content == "" {
		return prompt
	}

	// Prepend persona content + blank line + original prompt
	return content + "\n\n" + prompt
}

// SetActivePersona sets the manually selected persona.
// Pass empty string to clear the active persona (revert to auto-select or none).
func (m *Manager) SetActivePersona(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activePersona = name
}

// GetActivePersona returns the currently active persona name.
// Empty string means no persona is manually set (auto-select or none).
func (m *Manager) GetActivePersona() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activePersona
}
