// Package persona handles loading and managing persona definitions.
package persona

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"gopkg.in/yaml.v2"
)

// MaxDescriptionLen is the maximum length, in runes, of a persona description.
const MaxDescriptionLen = 500

// Manager handles persona loading and retrieval.
type Manager struct {
	personaPath      string
	personas         map[string]string // name -> content (front matter stripped)
	descriptions     map[string]string // name -> front matter description (may be absent)
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
		descriptions:     make(map[string]string),
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
		descriptions:     make(map[string]string),
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

		m.setPersonaLocked(personaName, string(content))
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

		m.setPersonaLocked(personaName, string(content))

		// Check for associated MCP config file: {persona-name}.mcp-config.json
		mcpConfigName := personaName + ".mcp-config.json"
		mcpConfigPath := filepath.Join(m.personaPath, mcpConfigName)
		if _, err := os.Stat(mcpConfigPath); err == nil {
			m.personaMCPConfig[personaName] = mcpConfigPath
		}
	}

	return nil
}

// setPersonaLocked stores a persona, splitting off its optional front matter.
// A later call for the same name replaces both content and description.
// The caller must hold m.mu for writing.
func (m *Manager) setPersonaLocked(name, raw string) {
	body, desc := splitFrontMatter(raw)
	m.personas[name] = body
	if desc != "" {
		m.descriptions[name] = desc
	} else {
		delete(m.descriptions, name)
	}
}

// frontMatterKeyRe matches a YAML "key:" line at the start of a line.
var frontMatterKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*[ \t]*:`)

// splitFrontMatter removes an optional YAML front matter block ("---" ... "---")
// from the top of raw and returns the remaining content plus the block's
// "description" field. A leading UTF-8 BOM is ignored for detection, and the
// delimiter lines may carry trailing spaces or tabs. A leading "---" block with
// no "key:" line (a Markdown horizontal rule) is not front matter and raw is
// returned unchanged. A block that is front matter but whose YAML does not
// parse is still stripped; its description is then recovered line by line.
func splitFrontMatter(raw string) (body, description string) {
	work := strings.TrimPrefix(raw, "\uFEFF")

	// Opening delimiter: the first line must be "---".
	first, rest, hasNL := strings.Cut(work, "\n")
	if !hasNL || strings.TrimRight(first, " \t\r") != "---" {
		return raw, ""
	}

	// Find the closing delimiter: a line consisting solely of "---".
	offset := 0
	for offset <= len(rest) {
		end := strings.IndexByte(rest[offset:], '\n')
		line := rest[offset:]
		if end >= 0 {
			line = rest[offset : offset+end]
		}
		if strings.TrimRight(line, " \t\r") == "---" {
			block := rest[:offset]
			if !looksLikeFrontMatter(block) {
				return raw, ""
			}
			next := offset + len(line)
			if end >= 0 {
				next++
			}
			body = strings.TrimLeft(rest[next:], "\r\n")
			var meta struct {
				Description string `yaml:"description"`
			}
			if err := yaml.Unmarshal([]byte(block), &meta); err != nil {
				return body, recoverDescription(block)
			}
			return body, meta.Description
		}
		if end < 0 {
			break
		}
		offset += end + 1
	}
	return raw, ""
}

// looksLikeFrontMatter reports whether block has at least one "key:" line.
func looksLikeFrontMatter(block string) bool {
	for _, line := range strings.Split(block, "\n") {
		if frontMatterKeyRe.MatchString(strings.TrimRight(line, "\r")) {
			return true
		}
	}
	return false
}

// recoverDescription extracts the top-level "description:" value from a block
// whose YAML failed to parse: the rest of the line with matching surrounding
// quotes removed.
func recoverDescription(block string) string {
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimRight(line, "\r")
		v, ok := strings.CutPrefix(line, "description:")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		return strings.TrimSpace(v)
	}
	return ""
}

// normalizeDescription collapses whitespace and truncates to MaxDescriptionLen runes.
func normalizeDescription(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > MaxDescriptionLen {
		s = strings.TrimSpace(string([]rune(s)[:MaxDescriptionLen]))
	}
	return s
}

// firstLineTitle returns the first non-empty line of content without leading
// heading markers.
func firstLineTitle(content string) string {
	content = strings.TrimPrefix(content, "\uFEFF")
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "# "))
		if line != "" && line != "---" {
			return line
		}
	}
	return ""
}

// Description returns a short natural-language description of when the persona
// applies. It uses the front matter "description" when present, otherwise the
// persona's first heading or non-empty line. The result has collapsed whitespace
// and is at most MaxDescriptionLen runes. Returns empty string for unknown personas.
func (m *Manager) Description(name string) string {
	if name == "" {
		return ""
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.descriptionLocked(name)
}

// descriptionLocked implements Description. The caller must hold m.mu.
func (m *Manager) descriptionLocked(name string) string {
	content, ok := m.personas[name]
	if !ok {
		return ""
	}
	desc := m.descriptions[name]
	if strings.TrimSpace(desc) == "" {
		desc = firstLineTitle(content)
	}
	return normalizeDescription(desc)
}

// Descriptions returns a copy of the name -> description map for all loaded personas.
func (m *Manager) Descriptions() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]string, len(m.personas))
	for name := range m.personas {
		out[name] = m.descriptionLocked(name)
	}
	return out
}

// GetPersona returns the content of a persona by name, without any front matter.
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
