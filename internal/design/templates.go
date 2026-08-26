package design

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/digiogithub/pando/internal/skills/od"
	"gopkg.in/yaml.v3"
)

// bundleFS holds the Pando-authored design bundles: the templates a user starts
// an artifact from, and the craft references those templates read. They are
// embedded rather than installed at build time so a fresh checkout has a
// gallery without touching the user's skills roots.
//
//go:embed bundles/templates/*/SKILL.md bundles/templates/*/scaffold/* bundles/craft/*.md
var bundleFS embed.FS

const (
	bundleTemplateRoot = "bundles/templates"
	bundleCraftRoot    = "bundles/craft"
	// craftDirName is where a template's craft references land inside the
	// installed skill directory. They are copied in rather than shared,
	// because the skill manager refuses to read a resource outside the skill
	// directory — and rightly so: a bundle that reaches outside itself breaks
	// the moment someone copies it elsewhere.
	craftDirName = "craft"
	// titlePlaceholder is substituted in scaffold files at creation time.
	titlePlaceholder = "{{TITLE}}"
)

// TemplateSource says where a gallery entry came from.
type TemplateSource string

const (
	// SourceBundled is a Pando-authored bundle embedded in the binary.
	SourceBundled TemplateSource = "bundled"
	// SourceInstalled is a bundle found in one of the skill discovery roots.
	SourceInstalled TemplateSource = "installed"
)

// Template is one gallery entry: a design skill described by its `od:` block.
// Third-party bundles reach the gallery through the same struct — Pando speaks
// the format, it does not ship anyone else's content.
type Template struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Category      string   `json:"category"`
	Scenario      string   `json:"scenario"`
	ExamplePrompt string   `json:"example_prompt,omitempty"`
	Mode          string   `json:"mode,omitempty"`
	Kind          Kind     `json:"kind,omitempty"`
	Preview       string   `json:"preview,omitempty"`
	Viewport      Viewport `json:"viewport,omitempty"`
	// RequiresSystem is od.design_system.requires: the template expects a
	// committed design system and will otherwise produce a look nobody chose.
	RequiresSystem bool     `json:"requires_system"`
	Craft          []string `json:"craft,omitempty"`
	CritiquePolicy string   `json:"critique_policy,omitempty"`
	// Startable reports whether an artifact can be created from this entry. A
	// craft reference or a workflow bundle is real, useful, and not startable.
	Startable bool `json:"startable"`
	// Source is where the entry came from; Installed reports whether it is
	// present in a skills root, which is what makes it visible to the agent.
	Source    TemplateSource `json:"source"`
	Installed bool           `json:"installed"`
	// SourcePath is set for installed entries so a user can find the file.
	SourcePath string `json:"source_path,omitempty"`
}

// TemplateFromSkill converts a discovered skill into a gallery entry. It
// reports false for a skill with no `od:` block: an ordinary Claude Code skill
// is not a design template and must not appear as one. Callers pass the fields
// rather than the skill itself because the design package must not depend on
// the skills subsystem, which depends on the tool layer, which depends on this
// package.
func TemplateFromSkill(name, description string, meta *od.Metadata) (Template, bool) {
	if meta == nil {
		return Template{}, false
	}
	t := Template{
		Name:           name,
		Description:    description,
		Category:       strings.TrimSpace(meta.Category),
		Scenario:       strings.TrimSpace(meta.Scenario),
		ExamplePrompt:  strings.TrimSpace(meta.ExamplePrompt),
		Mode:           strings.TrimSpace(meta.Mode),
		Preview:        strings.TrimSpace(meta.Preview.Type),
		Viewport:       Viewport{W: meta.Preview.Viewport.Width, H: meta.Preview.Viewport.Height},
		RequiresSystem: meta.DesignSystem.Requires,
		Craft:          append([]string(nil), meta.Craft.Requires...),
		CritiquePolicy: strings.TrimSpace(meta.Critique.Policy),
		Startable:      isTemplateMode(meta),
	}
	if surface := Kind(strings.ToLower(strings.TrimSpace(meta.Surface))); ValidKind(surface) {
		t.Kind = surface
	} else if t.Startable {
		// A surface this build does not support yet is not something we can
		// scaffold, so the entry stays listed but not startable rather than
		// silently creating a web artifact from a deck template.
		t.Startable = false
	}
	if t.Category == "" {
		t.Category = "uncategorised"
	}
	return t, true
}

// BundledTemplates returns the Pando-authored bundles, sorted by name.
func BundledTemplates() ([]Template, error) {
	entries, err := fs.ReadDir(bundleFS, bundleTemplateRoot)
	if err != nil {
		return nil, fmt.Errorf("design: read bundled templates: %w", err)
	}
	out := make([]Template, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		t, err := bundledTemplate(entry.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// BundledTemplate returns one Pando-authored bundle by name.
func BundledTemplate(name string) (Template, bool) {
	if !validBundleName(name) {
		return Template{}, false
	}
	t, err := bundledTemplate(name)
	if err != nil {
		return Template{}, false
	}
	return t, true
}

func bundledTemplate(name string) (Template, error) {
	body, err := bundleFS.ReadFile(path.Join(bundleTemplateRoot, name, skillFileName))
	if err != nil {
		return Template{}, fmt.Errorf("design: read bundled template %q: %w", name, err)
	}
	skillName, description, meta, err := parseBundleFrontmatter(string(body), name)
	if err != nil {
		return Template{}, fmt.Errorf("design: parse bundled template %q: %w", name, err)
	}
	t, ok := TemplateFromSkill(skillName, description, meta)
	if !ok {
		return Template{}, fmt.Errorf("design: bundled template %q has no od: block", name)
	}
	t.Source = SourceBundled
	return t, nil
}

// BundledTemplateContent returns the raw SKILL.md of a bundled template, which
// is what the installer writes and what the gallery shows as "read the skill".
func BundledTemplateContent(name string) (string, bool) {
	if !validBundleName(name) {
		return "", false
	}
	body, err := bundleFS.ReadFile(path.Join(bundleTemplateRoot, name, skillFileName))
	if err != nil {
		return "", false
	}
	return string(body), true
}

// CraftReferenceNames lists the bundled craft references.
func CraftReferenceNames() []string {
	entries, err := fs.ReadDir(bundleFS, bundleCraftRoot)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, strings.TrimSuffix(entry.Name(), ".md"))
	}
	sort.Strings(names)
	return names
}

// CraftReference returns one craft reference by name ("typography").
func CraftReference(name string) (string, bool) {
	if !validBundleName(name) {
		return "", false
	}
	body, err := bundleFS.ReadFile(path.Join(bundleCraftRoot, name+".md"))
	if err != nil {
		return "", false
	}
	return string(body), true
}

// Scaffold returns the seed files of a bundled template with the artifact title
// substituted, keyed by their path inside the artifact directory. A template
// with no scaffold returns an empty map and the caller falls back to the
// placeholder entry, which is still renderable.
func Scaffold(name, title string) (map[string]string, error) {
	if !validBundleName(name) {
		return nil, fmt.Errorf("design: invalid template name %q", name)
	}
	root := path.Join(bundleTemplateRoot, name, "scaffold")
	entries, err := fs.ReadDir(bundleFS, root)
	if err != nil {
		// Not every template ships a scaffold; that is a design decision, not
		// an error.
		return map[string]string{}, nil
	}
	files := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := bundleFS.ReadFile(path.Join(root, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("design: read scaffold %s/%s: %w", name, entry.Name(), err)
		}
		files[entry.Name()] = strings.ReplaceAll(string(body), titlePlaceholder, title)
	}
	return files, nil
}

// ErrBundleInstalled reports that a skill of that name is already present.
var ErrBundleInstalled = errors.New("design: template already installed")

// InstallBundle writes a bundled template into a skills root as
// {targetDir}/{name}/SKILL.md plus the craft references it declares. It returns
// the paths written. Installing is what makes the bundle visible to the agent's
// skill loader; the gallery works without it.
//
// It refuses to overwrite an existing skill unless force is set: the installed
// copy is the user's to edit, and silently replacing their edits with ours is
// the one thing an install must never do.
func InstallBundle(name, targetDir string, force bool) ([]string, error) {
	content, ok := BundledTemplateContent(name)
	if !ok {
		return nil, fmt.Errorf("design: %q is not a bundled template (%s)", name, strings.Join(bundledNames(), ", "))
	}
	t, err := bundledTemplate(name)
	if err != nil {
		return nil, err
	}

	skillDir := filepath.Join(targetDir, name)
	skillFile := filepath.Join(skillDir, skillFileName)
	if !force {
		if _, err := os.Stat(skillFile); err == nil {
			return nil, fmt.Errorf("%w: %s (pass force to replace it)", ErrBundleInstalled, skillFile)
		}
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return nil, fmt.Errorf("design: create %s: %w", skillDir, err)
	}

	written := make([]string, 0, len(t.Craft)+1)
	if err := os.WriteFile(skillFile, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("design: write %s: %w", skillFile, err)
	}
	written = append(written, skillFile)

	for _, craft := range t.Craft {
		body, ok := CraftReference(craft)
		if !ok {
			// A template naming a craft reference we do not ship is a bug in
			// the bundle, and a silent install would hide it.
			return written, fmt.Errorf("design: template %q requires unknown craft reference %q", name, craft)
		}
		craftDir := filepath.Join(skillDir, craftDirName)
		if err := os.MkdirAll(craftDir, 0o755); err != nil {
			return written, fmt.Errorf("design: create %s: %w", craftDir, err)
		}
		target := filepath.Join(craftDir, craft+".md")
		if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
			return written, fmt.Errorf("design: write %s: %w", target, err)
		}
		written = append(written, target)
	}
	return written, nil
}

// Gallery merges the bundled templates with the design bundles already present
// in the skill discovery roots. An installed bundle of the same name wins: the
// user's copy is the one the agent actually reads, so listing ours would show a
// description that is not in force.
func Gallery(discovered []Template) []Template {
	byName := make(map[string]Template)
	order := make([]string, 0, len(discovered))

	for _, t := range discovered {
		t.Source = SourceInstalled
		t.Installed = true
		if _, exists := byName[t.Name]; !exists {
			order = append(order, t.Name)
		}
		byName[t.Name] = t
	}

	bundled, err := BundledTemplates()
	if err != nil {
		bundled = nil
	}
	for _, t := range bundled {
		if existing, ok := byName[t.Name]; ok {
			// Keep the installed copy but remember it is one of ours, so the
			// gallery can offer "reinstall" rather than "install".
			existing.Source = SourceInstalled
			byName[t.Name] = existing
			continue
		}
		order = append(order, t.Name)
		byName[t.Name] = t
	}

	out := make([]Template, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Startable != out[j].Startable {
			return out[i].Startable
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func bundledNames() []string {
	entries, err := fs.ReadDir(bundleFS, bundleTemplateRoot)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

// validBundleName rejects anything that could address a file outside the
// embedded bundle tree.
func validBundleName(name string) bool {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return false
	}
	return true
}

// skillFileName is the Claude Code skill document name. It is duplicated from
// the skills subsystem rather than imported, because that subsystem depends on
// the tool layer, which depends on this package.
const skillFileName = "SKILL.md"

// isTemplateMode mirrors skills.SkillMetadata.IsDesignTemplate for an od block
// read straight from a bundle.
func isTemplateMode(meta *od.Metadata) bool {
	if meta == nil || strings.EqualFold(meta.Mode, "reference") {
		return false
	}
	return strings.TrimSpace(meta.Surface) != ""
}

// bundleFrontmatter is the slice of a SKILL.md header the gallery needs.
type bundleFrontmatter struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	OD          *od.Metadata `yaml:"od"`
}

func parseBundleFrontmatter(content, defaultName string) (string, string, *od.Metadata, error) {
	frontmatter, _, err := od.SplitFrontmatter(content)
	if err != nil {
		return "", "", nil, err
	}
	var header bundleFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &header); err != nil {
		return "", "", nil, err
	}
	if header.Name == "" {
		header.Name = defaultName
	}
	return header.Name, header.Description, header.OD, nil
}
