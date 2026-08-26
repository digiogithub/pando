// Package od carries OpenDesign's `od:` skill-frontmatter namespace, kept
// verbatim so a bundle written for either tool loads in the other. It is a leaf
// package on purpose: both the skills subsystem and the design subsystem read
// these types, and neither may depend on the other.
package od

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Metadata is the `od:` frontmatter block, OpenDesign's namespace kept
// verbatim so a bundle written for either tool loads in the other. The block is
// optional: a skill without it is an ordinary skill and every field below keeps
// its zero value, which is why the parser exposes it as a pointer.
type Metadata struct {
	// Mode separates bundles that produce an artifact ("template") from the
	// craft references a template pulls in ("reference"). An unknown or empty
	// mode is treated as a template when a surface is declared.
	Mode string `yaml:"mode"`
	// Surface is the design kind the template targets ("web", "deck").
	Surface string `yaml:"surface"`
	// Category and Scenario group the bundle in the gallery.
	Category string `yaml:"category"`
	Scenario string `yaml:"scenario"`
	// ExamplePrompt is the starter brief the gallery offers as "Try it".
	ExamplePrompt string       `yaml:"example_prompt"`
	Preview       Preview      `yaml:"preview"`
	DesignSystem  DesignSystem `yaml:"design_system"`
	Craft         Craft        `yaml:"craft"`
	Critique      Critique     `yaml:"critique"`
}

// Preview describes how the artifact wants to be previewed.
type Preview struct {
	// Type is the preview mode ("page", "deck").
	Type     string   `yaml:"type"`
	Viewport Viewport `yaml:"viewport"`
}

// Viewport is the preview viewport in CSS pixels. Zero means "use the
// configured default" rather than "collapse the preview".
type Viewport struct {
	Width  int `yaml:"width"`
	Height int `yaml:"height"`
}

// DesignSystem states whether the template needs a committed design system.
type DesignSystem struct {
	Requires bool `yaml:"requires"`
}

// Craft lists the craft references the template expects to read. Names are
// resolved inside the skill directory, never outside it.
type Craft struct {
	Requires []string `yaml:"requires"`
}

// Critique carries the per-skill critic policy consumed in P8.
type Critique struct {
	Policy string `yaml:"policy"`
}

// SplitFrontmatter separates the YAML frontmatter of a Markdown document from
// its body. It lives here because the design subsystem parses embedded skill
// bundles without going through the skills manager.
func SplitFrontmatter(content string) (string, string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.TrimPrefix(normalized, "\ufeff")

	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", fmt.Errorf("missing YAML frontmatter opening delimiter")
	}

	closingIndex := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closingIndex = i
			break
		}
	}
	if closingIndex == -1 {
		return "", "", fmt.Errorf("missing YAML frontmatter closing delimiter")
	}

	return strings.Join(lines[1:closingIndex], "\n"), strings.Join(lines[closingIndex+1:], "\n"), nil
}

// Strip removes the top-level `od:` key from a frontmatter document and
// re-encodes the rest. It reports false when the frontmatter is not a mapping
// or carries no od key. Callers use it to keep a skill whose od block they
// cannot understand: an unreadable extension must not make the skill invisible.
func Strip(frontmatter string) (string, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(frontmatter), &doc); err != nil {
		return "", false
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", false
	}
	mapping := doc.Content[0]
	kept := make([]*yaml.Node, 0, len(mapping.Content))
	removed := false
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "od" {
			removed = true
			continue
		}
		kept = append(kept, mapping.Content[i], mapping.Content[i+1])
	}
	if !removed {
		return "", false
	}
	mapping.Content = kept

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", false
	}
	return string(out), true
}
