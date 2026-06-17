package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DiscoveryPaths returns the ordered list of skill search paths (highest precedence first).
func DiscoveryPaths(workDir string) []string {
	home, _ := os.UserHomeDir()

	return []string{
		filepath.Join(home, ".pando", "skills"),
		filepath.Join(workDir, ".pando", "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(workDir, ".claude", "skills"),
	}
}

// ConfiguredDiscoveryPaths returns the full ordered list of skill search paths
// (highest precedence first): the built-in discovery paths followed by any extra
// paths from configuration. Relative extra paths are resolved against workDir.
// This is the single source of truth shared by the agent's skill loader and the
// API/UI skill listing so they never diverge.
func ConfiguredDiscoveryPaths(workDir string, extraPaths []string) []string {
	paths := append([]string{}, DiscoveryPaths(workDir)...)
	for _, p := range extraPaths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(workDir, p)
		}
		paths = append(paths, filepath.Clean(p))
	}
	return paths
}

// DiscoverSkills scans all paths for SKILL.md files and returns skills ordered by precedence.
// Skills from higher-precedence paths override same-named skills from lower paths.
func DiscoverSkills(paths []string) ([]*Skill, error) {
	seen := make(map[string]*Skill, len(paths))

	for _, basePath := range paths {
		if _, err := os.Stat(basePath); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}

		if err := filepath.WalkDir(basePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() != SkillFileName {
				return nil
			}

			skill, err := ParseSkillFile(path)
			if err != nil {
				return nil
			}

			if _, exists := seen[skill.Metadata.Name]; !exists {
				seen[skill.Metadata.Name] = skill
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}

	result := make([]*Skill, 0, len(seen))
	for _, skill := range seen {
		result = append(result, skill)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Metadata.Name < result[j].Metadata.Name
	})

	return result, nil
}
