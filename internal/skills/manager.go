package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type SkillManager struct {
	skills       map[string]*Skill
	loadOrder    []string
	maxCacheSize int
	mu           sync.RWMutex
}

func NewSkillManager(maxCache int) *SkillManager {
	return &SkillManager{
		skills:       make(map[string]*Skill),
		maxCacheSize: maxCache,
	}
}

func (m *SkillManager) LoadAll(paths []string) error {
	loaded := make(map[string]*Skill, len(paths))
	for _, path := range paths {
		skillPath, err := resolveSkillPath(path)
		if err != nil {
			return err
		}

		skill, err := ParseSkillFile(skillPath)
		if err != nil {
			return err
		}

		if _, exists := loaded[skill.Metadata.Name]; exists {
			return fmt.Errorf("duplicate skill name %q", skill.Metadata.Name)
		}

		skill.Instructions = ""
		skill.Resources = nil
		skill.LoadedLevel = LevelMetadata
		loaded[skill.Metadata.Name] = skill
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for name := range loaded {
		if _, exists := m.skills[name]; exists {
			return fmt.Errorf("duplicate skill name %q", name)
		}
	}

	for name, skill := range loaded {
		m.skills[name] = skill
	}

	return nil
}

func (m *SkillManager) GetAllMetadata() []SkillMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metadata := make([]SkillMetadata, 0, len(m.skills))
	for _, skill := range m.skills {
		metadata = append(metadata, skill.Metadata)
	}

	sort.Slice(metadata, func(i, j int) bool {
		return metadata[i].Name < metadata[j].Name
	})

	return metadata
}

func (m *SkillManager) IsLoaded(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	skill, ok := m.skills[name]
	if !ok {
		return false
	}

	return skill.LoadedLevel >= LevelInstructions && strings.TrimSpace(skill.Instructions) != ""
}

func (m *SkillManager) SetLoaded(name string, loaded bool) error {
	if loaded {
		_, err := m.GetInstructions(name)
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}

	skill.Instructions = ""
	skill.Resources = nil
	skill.LoadedLevel = LevelMetadata

	for i, existing := range m.loadOrder {
		if existing == name {
			m.loadOrder = append(m.loadOrder[:i], m.loadOrder[i+1:]...)
			break
		}
	}

	return nil
}

func (m *SkillManager) GetInstructions(name string) (string, error) {
	m.mu.Lock()
	skill, ok := m.skills[name]
	if !ok {
		m.mu.Unlock()
		return "", fmt.Errorf("skill %q not found", name)
	}
	if skill.LoadedLevel >= LevelInstructions && skill.Instructions != "" {
		skill.LastAccessed = time.Now()
		m.touchLRULocked(name)
		instructions := skill.Instructions
		m.mu.Unlock()
		return instructions, nil
	}
	sourcePath := skill.SourcePath
	m.mu.Unlock()

	parsed, err := ParseSkillFile(sourcePath)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok = m.skills[name]
	if !ok {
		return "", fmt.Errorf("skill %q not found", name)
	}

	skill.Metadata = parsed.Metadata
	skill.Instructions = parsed.Instructions
	if skill.LoadedLevel < LevelInstructions {
		skill.LoadedLevel = LevelInstructions
	}
	skill.LastAccessed = time.Now()
	m.touchLRULocked(name)
	m.enforceCacheLimitLocked()

	return skill.Instructions, nil
}

func (m *SkillManager) GetResource(name, resourcePath string) ([]byte, error) {
	if _, err := m.GetInstructions(name); err != nil {
		return nil, err
	}

	m.mu.Lock()
	skill, ok := m.skills[name]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("skill %q not found", name)
	}

	cleanResourcePath := filepath.Clean(resourcePath)
	for _, resource := range skill.Resources {
		if resource.Path == cleanResourcePath {
			skill.LastAccessed = time.Now()
			content := append([]byte(nil), resource.Content...)
			m.mu.Unlock()
			return content, nil
		}
	}
	sourcePath := skill.SourcePath
	m.mu.Unlock()

	fullPath, normalizedPath, err := resolveResourceLocation(sourcePath, resourcePath)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("read resource %q for skill %q: %w", resourcePath, name, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	skill, ok = m.skills[name]
	if !ok {
		return nil, fmt.Errorf("skill %q not found", name)
	}

	for _, resource := range skill.Resources {
		if resource.Path == normalizedPath {
			return append([]byte(nil), resource.Content...), nil
		}
	}

	skill.Resources = append(skill.Resources, SkillResource{Path: normalizedPath, Content: append([]byte(nil), content...)})
	skill.LoadedLevel = LevelResources
	skill.LastAccessed = time.Now()

	return append([]byte(nil), content...), nil
}

func (m *SkillManager) EvictLRU() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evictLRULocked()
}

func (m *SkillManager) Recall(name string) error {
	_, err := m.GetInstructions(name)
	return err
}

func (m *SkillManager) EstimateTokens(text string) int {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}
	return (len(trimmed) + 3) / 4
}

func (m *SkillManager) enforceCacheLimitLocked() {
	if m.maxCacheSize <= 0 {
		return
	}
	for m.loadedInstructionCountLocked() > m.maxCacheSize {
		m.evictLRULocked()
	}
}

func (m *SkillManager) loadedInstructionCountLocked() int {
	count := 0
	for _, skill := range m.skills {
		if skill.LoadedLevel >= LevelInstructions && skill.Instructions != "" {
			count++
		}
	}
	return count
}

func (m *SkillManager) evictLRULocked() {
	for len(m.loadOrder) > 0 {
		name := m.loadOrder[0]
		m.loadOrder = m.loadOrder[1:]
		skill, ok := m.skills[name]
		if !ok || skill.Instructions == "" {
			continue
		}

		skill.Instructions = ""
		skill.Resources = nil
		skill.LoadedLevel = LevelMetadata
		return
	}
}

func (m *SkillManager) touchLRULocked(name string) {
	for i, existing := range m.loadOrder {
		if existing == name {
			m.loadOrder = append(m.loadOrder[:i], m.loadOrder[i+1:]...)
			break
		}
	}
	m.loadOrder = append(m.loadOrder, name)
}

func resolveSkillPath(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat skill path %q: %w", path, err)
	}
	if info.IsDir() {
		path = filepath.Join(path, SkillFileName)
	}
	return filepath.Clean(path), nil
}

func resolveResourceLocation(skillPath, resourcePath string) (string, string, error) {
	if strings.TrimSpace(resourcePath) == "" {
		return "", "", fmt.Errorf("resource path cannot be empty")
	}

	skillDir := filepath.Dir(skillPath)
	cleanPath := filepath.Clean(resourcePath)
	if filepath.IsAbs(cleanPath) {
		return "", "", fmt.Errorf("resource path %q must be relative", resourcePath)
	}

	fullPath := filepath.Join(skillDir, cleanPath)
	relPath, err := filepath.Rel(skillDir, fullPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve resource path %q: %w", resourcePath, err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("resource path %q escapes skill directory", resourcePath)
	}

	return fullPath, relPath, nil
}
