package prompt

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Prompt template variants are human-authored files that compete with the
// embedded template of a section in the evaluator's UCB selection:
//
//	<root>/<section>/<variant>.md.tpl        e.g. .pando/prompts/variants/base/workflow/terse.md.tpl
//
// The embedded (or overridden) template is the implicit "default" variant. A
// variant id is "<section>#<variant>" and is stable across restarts; the
// database only stores statistics keyed by it.

const (
	// DefaultVariant is the name of the implicit variant that is the regular
	// template of a section.
	DefaultVariant = "default"

	variantSuffix = ".md.tpl"
)

// VariantID returns the stable id of a variant of a section.
func VariantID(section, variant string) string {
	return section + "#" + variant
}

// VariantFile is a variant defined by a file on disk.
type VariantFile struct {
	Section string
	Name    string
	ID      string
	Path    string
}

// VariantRoots returns the directories searched for variants, highest
// priority first: the project (<workingDir>/.pando/prompts/variants) and the
// global config dir (~/.config/pando/prompts/variants).
func VariantRoots(workingDir string) []string {
	var roots []string
	if workingDir != "" {
		roots = append(roots, filepath.Join(workingDir, ".pando", "prompts", "variants"))
	}
	if home, err := homeDir(); err == nil && home != "" {
		roots = append(roots, filepath.Join(home, ".config", "pando", "prompts", "variants"))
	}
	return roots
}

// DiscoverVariants lists the variant files of a section found under roots,
// sorted by name. When the same variant name exists in several roots the
// earlier root wins. The reserved name "default" is ignored.
func DiscoverVariants(section string, roots []string) []VariantFile {
	if section == "" || strings.Contains(section, "..") {
		return nil
	}
	seen := make(map[string]bool)
	var out []VariantFile
	for _, root := range roots {
		dir := filepath.Join(root, filepath.FromSlash(section))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			fname := e.Name()
			if e.IsDir() || !strings.HasSuffix(fname, variantSuffix) {
				continue
			}
			name := strings.TrimSuffix(fname, variantSuffix)
			if name == "" || name == DefaultVariant || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, VariantFile{
				Section: section,
				Name:    name,
				ID:      VariantID(section, name),
				Path:    filepath.Join(dir, fname),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DiscoverAllVariants lists every variant file under roots (all sections),
// sorted by section then name. Project roots win over later roots on a clash.
func DiscoverAllVariants(roots []string) []VariantFile {
	sections := make(map[string]bool)
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), variantSuffix) {
				return nil
			}
			rel, rerr := filepath.Rel(root, filepath.Dir(path))
			if rerr == nil && rel != "." {
				sections[filepath.ToSlash(rel)] = true
			}
			return nil
		})
	}
	var out []VariantFile
	for section := range sections {
		out = append(out, DiscoverVariants(section, roots)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Section != out[j].Section {
			return out[i].Section < out[j].Section
		}
		return out[i].Name < out[j].Name
	})
	return out
}
